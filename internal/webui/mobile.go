package webui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"omni/internal/config"
)

// 移动端远程访问。
//
// 桌面端左下角手机图标打开开关后，本机在局域网额外挂一个监听器，
// 与桌面窗口共用同一套路由和会话数据（项目、任务、聊天、设置完全同步），
// 手机浏览器打开后就是一个可用的 omni 工作台。
//
// 门禁是「设备唯一 token」而不是账号登录：二维码里的 URL 自带 token，
// 首次访问校验通过后换成 HttpOnly Cookie 并 302 到干净地址，之后按 Cookie 校验。
// 走 Cookie 而不是 Authorization 头，是因为 /api/events 是 EventSource，
// 浏览器不允许给 SSE 带自定义请求头。
//
// 安全边界（重要）：打开后，同一局域网内拿到 token 的人可以驱动本机 Agent，
// 包括 bash 执行和文件读写。所以默认关闭、token 128 位随机、每个请求都校验
// （不只首页），并且可以随时一键关停、二维码 URL 随 token 轮换一起失效。

const (
	mobileCookieName = "omni_mobile"
	mobileDefaultPort = 8792
	// mobileTokenBytes = 32 字节 = 256 位熵，base64url 后 43 字符。
	mobileTokenBytes = 32
	// mobilePathPrefix 是扫码链接的路径前缀：/device/<token>
	mobilePathPrefix = "/device/"
)

// mobileFile 是 token、固定端口与开关状态的落盘位置。
// 开关持久化，重启后仍保持上次的连接状态（用户不必每次手动打开）。
type mobileFile struct {
	Token   string `json:"token"`
	Port    int    `json:"port"`
	Enabled bool   `json:"enabled"`
	// PublicURL 是对外可达的基址，例如部署到线上后的 https://omni.example.com。
	// 为空时回退到本机局域网 IPv4，也就是现在「内网 IP:端口」的形态。
	PublicURL string `json:"public_url,omitempty"`
}

// mobile 是局域网监听器状态，零值不可用，须先 load。
type mobile struct {
	mu      sync.Mutex
	token   string
	port    int
	enabled bool
	// publicURL 非空时二维码用域名而不是内网 IP，见 mobileURLsLocked。
	publicURL string
	// persistEnabled 是上次退出时的开关状态，用于重启后自动恢复监听。
	persistEnabled bool
	srv            *http.Server
}

// newMobile 读取设备 token 与端口；文件缺失或损坏时生成新的。
// 写不了盘（只读目录等）直接返回错误：宁可开不了移动端，也不给一个弱 token。
func newMobile() (*mobile, error) {
	m := &mobile{}
	dir := config.DefaultDataDir()
	path := filepath.Join(dir, "mobile.json")
	var f mobileFile
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	changed := false
	if !validToken(f.Token) {
		tok, err := randomToken()
		if err != nil {
			return nil, err
		}
		f.Token = tok
		changed = true
	}
	if f.Port <= 0 {
		f.Port = mobileDefaultPort
		changed = true
	}
	if changed {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录失败: %w", err)
		}
		if err := m.write(f); err != nil {
			return nil, err
		}
	}
	m.token = f.Token
	m.port = f.Port
	m.publicURL = strings.TrimRight(strings.TrimSpace(f.PublicURL), "/")
	m.persistEnabled = f.Enabled
	return m, nil
}

// tokenPattern 校验落盘的 token：base64url 字符集，16~128 位。
//
// 这里只校验「可放进 URL 且不是明显随手写的弱值」，不再强制等于
// randomToken() 的 43 位长度。之前卡死长度带来一个很难查的坑：
// 用户（或任何工具）往 mobile.json 里写了个自定义 token，只要不是正好
// 43 位，下次启动就被当成损坏数据悄悄换成新 token——二维码还在手机上，
// 链接却已经失效，表现为「URL 莫名其妙又打不开了」。
func validToken(s string) bool {
	if len(s) < 16 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// write 落盘 token / 端口 / 开关。
func (m *mobile) write(f mobileFile) error {
	b, _ := json.Marshal(f)
	if err := os.WriteFile(filepath.Join(config.DefaultDataDir(), "mobile.json"), b, 0o600); err != nil {
		return fmt.Errorf("写入设备 token 失败: %w", err)
	}
	return nil
}

// randomToken 生成 256 位随机 token（URL 安全字符）。crypto/rand 失败时返回错误而不是降级到弱随机。
func randomToken() (string, error) {
	b := make([]byte, mobileTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成设备 token 失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// guard 是局域网监听器的门禁中间件：未开启一律 404（不暴露端口存在）。
func (m *mobile) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		on, tok := m.enabled, m.token
		m.mu.Unlock()
		if !on {
			writeErr(w, "移动端访问未开启", http.StatusNotFound)
			return
		}
		// 首次扫码：路径里带 token（/device/<token>），校验后换成 Cookie 并
		// 302 到干净的 /，避免 token 长期留在地址栏、浏览器历史和截图里。
		if strings.HasPrefix(r.URL.Path, mobilePathPrefix) {
			got := strings.TrimPrefix(r.URL.Path, mobilePathPrefix)
			if subtle.ConstantTimeCompare([]byte(got), []byte(tok)) != 1 {
				writeErr(w, "链接无效", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     mobileCookieName,
				Value:    tok,
				Path:     "/",
				MaxAge:   30 * 24 * 3600,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if c, err := r.Cookie(mobileCookieName); err == nil &&
			subtle.ConstantTimeCompare([]byte(c.Value), []byte(tok)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		denyMobile(w, r)
	})
}

// denyMobile 是凭证无效时的响应。
//
// 文档导航（浏览器地址栏、扫码）给人话 HTML 页面，XHR/SSE 仍回 JSON。
// 换发 token 之后旧链接会失效，手机上此时正停在已打开的页面里，
// 下一次请求就被拒——这时必须明确告诉用户「重新扫码」，而不是甩一段
// {"error":...} 的 JSON 让用户对着原始数据猜。
func denyMobile(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(mobileDeniedHTML))
		return
	}
	writeErr(w, "请用桌面端「移动端」里的二维码进入", http.StatusUnauthorized)
}

// mobileDeniedHTML 是凭证失效时的落地页，刻意内联样式、不引外部资源，
// 断网也能正常显示。
const mobileDeniedHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>需要重新扫码</title>
<style>
  body{margin:0;min-height:100dvh;display:flex;align-items:center;justify-content:center;
       background:#f6f6f7;color:#18181b;padding:24px;
       font-family:system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif}
  @media (prefers-color-scheme:dark){body{background:#18181b;color:#fafafa}}
  .card{max-width:22rem;width:100%;text-align:center}
  h1{font-size:1.125rem;margin:0 0 .75rem}
  p{margin:.4rem 0;font-size:.875rem;line-height:1.6;opacity:.75}
  a{display:inline-block;margin-top:1.25rem;padding:.5rem 1.25rem;border-radius:.5rem;
    background:#18181b;color:#fafafa;text-decoration:none;font-size:.875rem;border:0;
    font-family:inherit;cursor:pointer}
  @media (prefers-color-scheme:dark){a{background:#fafafa;color:#18181b}}
</style></head>
<body><div class="card">
  <h1>访问凭证已失效</h1>
  <p>这个二维码或链接已经不能用了——它可能来自一次二维码换发，<br>或者这台设备还没在电脑端开启「移动端」。</p>
  <p>请回到电脑，在左下角手机图标里重新扫码进入。</p>
  <a onclick="location.reload()">我扫了新码，再试一次</a>
</div></body></html>`

// start 开启局域网监听（重复调用复用已有实例），返回第一个可扫码 URL。
func (s *server) mobileStart() (string, error) {
	m := s.mobileState()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.srv == nil {
		ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", m.port))
		if err != nil {
			return "", fmt.Errorf("局域网端口 %d 无法监听: %w", m.port, err)
		}
		srv := &http.Server{Handler: m.guard(s.routes())}
		go func() { _ = srv.Serve(ln) }()
		m.srv = srv
	}
	m.enabled = true
	m.persistEnabled = true
	_ = m.write(m.mobileFileLocked())
	// 已在持锁状态，直接调 Locked 版本（sync.Mutex 不可重入）
	urls := s.mobileURLsLocked()
	if len(urls) == 0 {
		return "", fmt.Errorf("未找到局域网 IPv4 地址，请确认电脑已连上 Wi-Fi 或网线")
	}
	return urls[0], nil
}

// stop 关停监听器并断开所有移动端连接。
func (s *server) mobileStop() {
	m := s.mobileState()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.srv != nil {
		_ = m.srv.Close()
		m.srv = nil
	}
	m.enabled = false
	m.persistEnabled = false
	_ = m.write(m.mobileFileLocked())
}

// mobileFileLocked 把当前状态打包成可落盘的结构，调用方必须已持有 m.mu。
func (m *mobile) mobileFileLocked() mobileFile {
	return mobileFile{Token: m.token, Port: m.port, Enabled: m.enabled, PublicURL: m.publicURL}
}

// rotateToken 换发新 token。
//
// 旧 token 立刻作废，不需要维护黑名单：guard 每个请求都拿「当前 token」做比对，
// 所以已经用旧 token 换到 Cookie 的手机也会在下一次请求时被拒，重新扫码即可。
func (m *mobile) rotateToken() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tok, err := randomToken()
	if err != nil {
		return err
	}
	m.token = tok
	return m.write(m.mobileFileLocked())
}

// mobileState 懒初始化，保证测试里直接构造 &server{} 也不会空指针。
// 初始化失败时退回零值 mobile：token 为空、enabled 为 false，门禁一律拒绝。
func (s *server) mobileState() *mobile {
	s.mobileOnce.Do(func() {
		if s.mobile == nil {
			if m, err := newMobile(); err == nil {
				s.mobile = m
			} else {
				s.mobile = &mobile{}
			}
		}
	})
	return s.mobile
}

// mobileURLsLocked 生成扫码 URL。优先用配置的对外域名（部署到线上时），
// 没配就退回每张网卡一个内网地址：http://<内网IP>:<端口>/device/<token>。
// 调用方必须已持有 m.mu，且 m 已由 mobileState() 初始化过。
func (s *server) mobileURLsLocked() []string {
	if s.mobile.publicURL != "" {
		return []string{s.mobile.publicURL + mobilePathPrefix + s.mobile.token}
	}
	ips := lanIPs()
	urls := make([]string, 0, len(ips))
	for _, ip := range ips {
		urls = append(urls, "http://"+ip+":"+strconv.Itoa(s.mobile.port)+mobilePathPrefix+s.mobile.token)
	}
	return urls
}

// mobileInfo 是前端面板要的完整状态。
func (s *server) mobileInfo() map[string]any {
	m := s.mobileState()
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{
		"enabled": m.enabled,
		"port":    m.port,
		"urls":    s.mobileURLsLocked(),
	}
}

// lanIPs 列出手机能连到的局域网 IPv4：排除回环、未就绪和 169.254 链路本地地址。
func lanIPs() []string {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, itf := range ifs {
		if itf.Flags&net.FlagUp == 0 || itf.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := itf.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, ip.String())
		}
	}
	sort.Strings(out)
	return out
}

// ---------- HTTP 处理器（只挂在桌面端的 127.0.0.1 服务上）----------

// handleMobileInfo 返回开关状态与扫码 URL，供左下角面板渲染二维码。
func (s *server) handleMobileInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.mobileInfo())
}

// handleMobileToggle 开关移动端访问。
func (s *server) handleMobileToggle(w http.ResponseWriter, r *http.Request) {
	body, err := decode[struct {
		Enabled bool `json:"enabled"`
	}](r)
	if err != nil {
		writeErr(w, "参数错误", http.StatusBadRequest)
		return
	}
	if body.Enabled {
		if _, err := s.mobileStart(); err != nil {
			writeErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		s.mobileStop()
	}
	writeJSON(w, s.mobileInfo())
}

// handleMobileRotate 换发设备 token。
//
// 这是一次安全操作：旧二维码、旧的扫码链接、以及按旧 token 换到的 Cookie
// 会全部立刻失效，已经连着的手机必须重新扫新码。所以界面上要先二次确认再调这里。
func (s *server) handleMobileRotate(w http.ResponseWriter, r *http.Request) {
	if err := s.mobileState().rotateToken(); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.mobileInfo())
}
