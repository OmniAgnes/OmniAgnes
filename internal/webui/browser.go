// 内置浏览器桥：desktop 壳（Electron）里的 WebContentsView 由内核的 browser 工具驱动。
//
// 通道设计与 approval/question 对称（内核→前端有 SSE，前端→内核有 REST）：
//
//	· browser 工具 Execute → 造一个 op 入队，阻塞等结果（最长 3 分钟）；
//	· 前端 BrowserPane 长轮询 GET /api/browser/next 取走 op，经 preload IPC
//	  交给 Electron 主进程执行（CDP 级输入事件 / executeJavaScript）；
//	· 执行完 POST /api/browser/result 回填，工具拿到页面文本/元素树返回给模型。
//
// 长轮询连接本身就是「桌面壳在线」的心跳：纯浏览器（非 Electron）没有 poller，
// 工具会快速失败并提示，不会让模型干等。
package webui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"omni/internal/tools"
)

// browserOp 是一次待前端执行的浏览器操作。
type browserOp struct {
	ID         string         `json:"id"`
	Op         string         `json:"op"`
	Args       map[string]any `json:"args"`
	NoAutoOpen bool           `json:"noAutoOpen,omitempty"` // 目标是 JSON：前端照常执行但不自动展开面板
	ch         chan browserRes
	issued     time.Time // 发给前端的时间（零值=还在队列）
}

// browserRes 是前端回传的执行结果。
type browserRes struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

// BrowserBridge 保存待执行队列与在线 poller 状态。
type BrowserBridge struct {
	mu       sync.Mutex
	pending  []*browserOp          // 未取走的 op（FIFO）
	inflight map[string]*browserOp // 已发给前端、等待回填
	nextWake chan struct{}         // 唤醒挂起的 long-poll
	pollers  int                   // 当前挂起的 /next 长轮询数
	lastSeen time.Time             // 最近一次 /next 进出时间（两次轮询的间隙兜底）
	seq      int
}

func newBrowserBridge() *BrowserBridge {
	return &BrowserBridge{
		inflight: map[string]*browserOp{},
		nextWake: make(chan struct{}, 1),
	}
}

// desktopOnline 判断桌面壳是否连着：有长轮询挂起，或 5 秒内有过轮询活动
// （一次 /next 返回到下一次发起之间有毫秒级间隙，用 lastSeen 兜底防误判）。
func (b *BrowserBridge) desktopOnline() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pollers > 0 || time.Since(b.lastSeen) < 5*time.Second
}

// dispatch 入队一个 op 并等待结果。ctx 取消（用户中断/会话结束）立即返回错误。
func (b *BrowserBridge) dispatch(ctx context.Context, op string, args map[string]any) (string, error) {
	b.mu.Lock()
	b.seq++
	o := &browserOp{
		ID:   fmt.Sprintf("bw-%d", b.seq),
		Op:   op,
		Args: args,
		ch:   make(chan browserRes, 1),
	}
	b.mu.Unlock()
	// open/navigate 先探一下目标是不是 JSON：裸 JSON 没有浏览意义，自动展开
	// 只会弹出一屏原始数据。标记后前端照常（隐藏）执行，面板保持收起。
	// 放在锁外——探测是网络 IO，最坏几秒，不能堵住其他 op 的入队。
	if op == "open" || op == "navigate" {
		if u, _ := args["url"].(string); u != "" && probeJSON(ctx, u) {
			o.NoAutoOpen = true
		}
	}
	b.mu.Lock()
	b.pending = append(b.pending, o)
	b.mu.Unlock()
	select {
	case b.nextWake <- struct{}{}:
	default:
	}

	select {
	case res := <-o.ch:
		if !res.OK {
			msg := res.Error
			if msg == "" {
				msg = "浏览器操作失败"
			}
			return "", fmt.Errorf("%s", msg)
		}
		return res.Output, nil
	case <-ctx.Done():
		b.drop(o.ID)
		return "", ctx.Err()
	case <-time.After(3 * time.Minute):
		b.drop(o.ID)
		return "", fmt.Errorf("浏览器操作超时（桌面端无响应）")
	}
}

// drop 从队列/inflight 移除未完成的操作。
func (b *BrowserBridge) drop(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, o := range b.pending {
		if o.ID == id {
			b.pending = append(b.pending[:i], b.pending[i+1:]...)
			return
		}
	}
	delete(b.inflight, id)
}

// handleBrowserNext 长轮询取下一条浏览器操作；60 秒无操作返回空对象。
// 挂起期间 pollers++（桌面壳在线标记），返回时 pollers--。
func (s *server) handleBrowserNext(w http.ResponseWriter, r *http.Request) {
	b := s.browser
	if b == nil {
		writeErr(w, "浏览器桥未启用", http.StatusNotFound)
		return
	}
	b.mu.Lock()
	b.pollers++
	b.lastSeen = time.Now()
	var got *browserOp
	if len(b.pending) > 0 {
		got = b.pending[0]
		b.pending = b.pending[1:]
		got.issued = time.Now()
		b.inflight[got.ID] = got
	}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.pollers--
		b.lastSeen = time.Now()
		b.mu.Unlock()
	}()

	if got == nil {
		// 等唤醒或超时
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		for {
			select {
			case <-b.nextWake:
			case <-ctx.Done():
				writeJSON(w, map[string]any{"op": nil})
				return
			}
			b.mu.Lock()
			b.lastSeen = time.Now()
			if len(b.pending) > 0 {
				got = b.pending[0]
				b.pending = b.pending[1:]
				got.issued = time.Now()
				b.inflight[got.ID] = got
			}
			b.mu.Unlock()
			if got != nil {
				break
			}
		}
	}
	// 顺带清理超期未回填的 op（前端崩溃/关页防泄漏）
	b.gcStale()
	writeJSON(w, map[string]any{"op": map[string]any{
		"id":         got.ID,
		"op":         got.Op,
		"args":       got.Args,
		"noAutoOpen": got.NoAutoOpen,
	}})
}

// gcStale 清理发出去超过 60 秒仍未回填的操作（前端崩溃/关页时防泄漏）。
func (b *BrowserBridge) gcStale() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, o := range b.inflight {
		if time.Since(o.issued) > 60*time.Second {
			select {
			case o.ch <- browserRes{Error: "桌面端浏览器无响应（可能已关闭）"}:
			default:
			}
			delete(b.inflight, id)
		}
	}
}

// handleBrowserResult 前端回填执行结果。
func (s *server) handleBrowserResult(w http.ResponseWriter, r *http.Request) {
	if s.browser == nil {
		writeErr(w, "浏览器桥未启用", http.StatusNotFound)
		return
	}
	req, err := decode[struct {
		ID     string `json:"id"`
		OK     *bool  `json:"ok"`
		Output string `json:"output"`
		Error  string `json:"error"`
	}](r)
	if err != nil || req.ID == "" {
		writeErr(w, "请求体需为 {\"id\":..., \"ok\":bool, \"output\":..., \"error\":...}", http.StatusBadRequest)
		return
	}
	s.browser.mu.Lock()
	o := s.browser.inflight[req.ID]
	delete(s.browser.inflight, req.ID)
	s.browser.mu.Unlock()
	if o == nil {
		writeErr(w, "操作不存在或已超时: "+req.ID, http.StatusNotFound)
		return
	}
	res := browserRes{OK: req.OK == nil || *req.OK, Output: req.Output, Error: req.Error}
	select {
	case o.ch <- res:
	default:
	}
	writeJSON(w, map[string]any{"ok": true})
}

// probeJSON 探测 URL 是否返回 JSON：路径以 .json 结尾，或响应 Content-Type 含
// json（application/json、application/*+json 都命中）。探测失败（超时/网络错误/
// 非 2xx 的 HEAD）一律按非 JSON 处理——宁可多弹一次面板，不能让工具卡住或失灵。
func probeJSON(ctx context.Context, rawURL string) bool {
	if u, err := url.Parse(rawURL); err == nil && strings.EqualFold(path.Ext(u.Path), ".json") {
		return true
	}
	// 相对写法（如 example.com/x）与主进程 runBrowserOp 的补齐规则一致
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	client := &http.Client{Timeout: 3 * time.Second}
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		ct := resp.Header.Get("Content-Type")
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		resp.Body.Close()
		if strings.Contains(ct, "json") {
			return true
		}
		if resp.StatusCode < 400 || method == http.MethodGet {
			return false // 拿得到正常响应就信它；GET 是最后一次尝试
		}
		// HEAD 被拒（4xx/5xx）退化 GET 再试一次
	}
	return false
}

// browserTool 构造 browser 内置工具。op 语义由桌面端实现，这里只做参数校验与转发。
func (s *server) browserTool() *tools.Tool {
	return &tools.Tool{
		Name: "browser",
		Description: "驱动桌面版内置浏览器（仅 Electron 桌面端可用；网页版会直接报错）。" +
			"open {url} 打开页面并等加载完成；navigate {url} / reload / back / forward；" +
			"click {x,y} 点击视口坐标（doubleClick 可选）；type {text} 在焦点输入框输入文本；" +
			"key {key} 按键（Enter/Tab/Escape/Backspace/ArrowDown…）；scroll {x,y,amount} 滚动（负值向上）；" +
			"wait {ms} 等待；extract 提取可见交互元素（含坐标）与正文文本——模型靠它“看”页面；" +
			"eval {code} 在页面执行 JS 表达式并返回结果；state 返回 url/标题/加载状态；show / hide / close 控制面板。" +
			"标准流程：open → extract 看元素坐标 → click/type → extract 验证。",
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"op"},
			"properties": map[string]any{
				"op": map[string]any{
					"type":        "string",
					"description": "操作名：open|navigate|reload|back|forward|click|type|key|scroll|wait|extract|eval|state|show|hide|close",
				},
				"url":         map[string]any{"type": "string", "description": "open/navigate 的目标地址"},
				"x":           map[string]any{"type": "number", "description": "click/scroll 的视口 X 坐标"},
				"y":           map[string]any{"type": "number", "description": "click/scroll 的视口 Y 坐标"},
				"text":        map[string]any{"type": "string", "description": "type 的文本"},
				"key":         map[string]any{"type": "string", "description": "key 的按键名"},
				"amount":      map[string]any{"type": "number", "description": "scroll 滚动量（像素，负值向上）"},
				"ms":          map[string]any{"type": "integer", "description": "wait 毫秒数"},
				"code":        map[string]any{"type": "string", "description": "eval 的 JS 表达式"},
				"doubleClick": map[string]any{"type": "boolean", "description": "click 是否双击"},
			},
		},
		ReadOnly: false,
		Execute: func(ctx context.Context, args map[string]any, env *tools.Env) (string, error) {
			opName, _ := args["op"].(string)
			if opName == "" {
				return "", fmt.Errorf("缺少 op 参数")
			}
			if s.browser == nil || !s.browser.desktopOnline() {
				return "", fmt.Errorf("内置浏览器不可用：当前不是桌面版（browser 工具仅在 Electron 桌面壳中工作），请改用 read/bash 等工具或提示用户在桌面版中操作")
			}
			return s.browser.dispatch(ctx, opName, args)
		},
	}
}
