package webui

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omni/internal/config"
)

func TestValidToken(t *testing.T) {
	tok, err := randomToken()
	if err != nil {
		t.Fatalf("randomToken: %v", err)
	}
	if !validToken(tok) {
		t.Fatalf("randomToken 生成的不合法: %q", tok)
	}
	// 自定义 token（手写、非 43 位）必须被接受，否则重启会被悄悄换掉
	for _, s := range []string{
		strings.Repeat("a", 16),
		"ht9zkSNOQTDtNFNOwPbNOAOUv8MCM-rXhpa_cIvAQiI", // 42 位
		"my-own-token-for-testing",
		strings.Repeat("A", 128),
	} {
		if !validToken(s) {
			t.Errorf("应接受 %q（长度 %d）", s, len(s))
		}
	}
	// 空、太短、超长、含非 base64url 字符
	for _, s := range []string{
		"",
		"short",
		strings.Repeat("a", 15),
		strings.Repeat("a", 129),
		"has space in it xx",
		"has/slash/in/it",
		"中文token不算数",
	} {
		if validToken(s) {
			t.Errorf("应拒绝 %q", s)
		}
	}
}

// 自定义 token 写进 mobile.json 后，重启（新Mobile）必须原样沿用，
// 不能被当成损坏数据换掉。
func TestNewMobileKeepsCustomToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	// config.DefaultDataDir 走用户主目录；用一次真实的 load 验证不变性
	if got := config.DefaultDataDir(); !strings.HasPrefix(got, dir) {
		t.Skipf("DefaultDataDir=%s 不在临时目录内，跳过", got)
	}
	path := filepath.Join(config.DefaultDataDir(), "mobile.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const custom = "ht9zkSNOQTDtNFNOwPbNOAOUv8MCM-rXhpa_cIvAQiI" // 42 位
	if err := os.WriteFile(path, []byte(`{"token":"`+custom+`","port":8792,"enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	m1, err := newMobile()
	if err != nil {
		t.Fatal(err)
	}
	if m1.token != custom {
		t.Fatalf("首次加载把 token 换了: %q -> %q", custom, m1.token)
	}
	m2, err := newMobile()
	if err != nil {
		t.Fatal(err)
	}
	if m2.token != custom {
		t.Fatalf("二次加载把 token 换了: %q -> %q", custom, m2.token)
	}
}

// 不跟随 302：/device/<token> 会重定向到 /，跟过去就丢掉了要检查的 302。
var noRedirect = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// 换发后旧 token 与旧 Cookie 立即失效，新 token 可用。
func TestGuardAfterRotate(t *testing.T) {
	m := &mobile{token: "old-token-0123456789", enabled: true}
	srv := httptest.NewServer(m.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer srv.Close()

	// 旧 token 换 Cookie
	resp, err := noRedirect.Get(srv.URL + mobilePathPrefix + "old-token-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("旧 token 应 302, got %d", resp.StatusCode)
	}
	var oldCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == mobileCookieName {
			oldCookie = c
		}
	}
	if oldCookie == nil {
		t.Fatal("没有下发 Cookie")
	}

	if err := m.rotateToken(); err != nil {
		t.Fatal(err)
	}

	// 旧 Cookie 被拒
	req, _ := http.NewRequest("GET", srv.URL+"/api/tasks", nil)
	req.AddCookie(oldCookie)
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("换发后旧 Cookie 应 401, got %d", r2.StatusCode)
	}

	// 新 token 可用
	r3, err := noRedirect.Get(srv.URL + mobilePathPrefix + m.token)
	if err != nil {
		t.Fatal(err)
	}
	r3.Body.Close()
	if r3.StatusCode != http.StatusFound {
		t.Fatalf("新 token 应 302, got %d", r3.StatusCode)
	}
}

// 文档导航拿到人话 HTML，XHR 拿到 JSON。
func TestDenyMobileContentNegotiation(t *testing.T) {
	m := &mobile{token: "old-token-0123456789", enabled: true}
	srv := httptest.NewServer(m.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("文档导航应返回 HTML, got %q", ct)
	}
	b := make([]byte, 4096)
	n, _ := resp.Body.Read(b)
	if !strings.Contains(string(b[:n]), "重新扫码") {
		t.Fatalf("HTML 页面缺少引导文案: %q", string(b[:n]))
	}
	if strings.Contains(string(b[:n]), `"error"`) {
		t.Fatalf("不应再出现原始 JSON 字段: %q", string(b[:n]))
	}

	// XHR 仍要 JSON，前端才能解析出 error 字段
	req2, _ := http.NewRequest("GET", srv.URL+"/api/tasks", nil)
	req2.Header.Set("Accept", "*/*")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if ct := resp2.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("XHR 应返回 JSON, got %q", ct)
	}
}

func TestRandomTokenLength(t *testing.T) {
	tok, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	if want := base64.RawURLEncoding.EncodedLen(mobileTokenBytes); len(tok) != want {
		t.Fatalf("randomToken 长度 %d, 期望 %d", len(tok), want)
	}
}

// 配置了 public_url（部署到线上）时，二维码基址用域名而不是内网 IP。
func TestMobileURLsPrefersPublicURL(t *testing.T) {
	s := &server{}
	m := &mobile{token: "tok-0123456789abcdef", port: 8792, publicURL: "https://omni.example.com"}
	s.mobile = m
	s.mobileOnce.Do(func() {})

	got := s.mobileURLsLocked()
	if len(got) != 1 {
		t.Fatalf("配了 public_url 应只返回一个 URL, got %v", got)
	}
	want := "https://omni.example.com" + mobilePathPrefix + "tok-0123456789abcdef"
	if got[0] != want {
		t.Fatalf("got %q, want %q", got[0], want)
	}
}
