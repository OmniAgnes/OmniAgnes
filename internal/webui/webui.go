// Package webui 提供网页版聊天界面：本地 HTTP 服务 + SSE 流式输出，
// 与终端 TUI 共用同一个 Agent 实例。
package webui

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"omni/internal/agent"
	"omni/internal/llm"
)

//go:embed page.html
var pageHTML string

var (
	mu     sync.Mutex
	srvURL string
)

// Start 启动网页版服务（仅监听 127.0.0.1，重复调用复用已有服务），返回访问地址。
func Start(a *agent.Agent) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if srvURL != "" {
		return srvURL, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(pageHTML))
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"model":      a.Model(),
			"mode":       string(a.Mode()),
			"configured": a.HasProvider(),
		})
	})
	mux.HandleFunc("POST /api/chat", func(w http.ResponseWriter, r *http.Request) {
		handleChat(a, w, r)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	srvURL = "http://" + ln.Addr().String()
	return srvURL, nil
}

// Launch 启动服务并尝试用系统浏览器打开，返回 URL。
func Launch(a *agent.Agent) (string, error) {
	url, err := Start(a)
	if err != nil {
		return "", err
	}
	_ = openBrowser(url)
	return url, nil
}

// handleChat 以 SSE 流式返回一轮问答。
func handleChat(a *agent.Agent, w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		http.Error(w, `请求体需为 {"text": "..."}`, http.StatusBadRequest)
		return
	}
	if !a.HasProvider() {
		http.Error(w, "尚未配置模型：请先在终端 TUI 中运行 /config 配置", http.StatusServiceUnavailable)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "当前连接不支持流式输出", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")

	send := func(payload map[string]any) {
		b, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	_ = a.Run(r.Context(), req.Text, func(ev agent.Event) {
		switch ev.Type {
		case agent.EventText:
			send(map[string]any{"type": "text", "text": ev.Text})
		case agent.EventToolStart:
			send(map[string]any{"type": "tool_start", "name": ev.Tool.Name, "args": llm.MarshalToolArgs(ev.Tool.Args)})
		case agent.EventToolOutput:
			send(map[string]any{"type": "tool_output", "name": ev.Tool.Name, "output": ev.Output, "is_error": ev.IsError})
		case agent.EventError:
			send(map[string]any{"type": "error", "error": ev.Err.Error()})
		case agent.EventDone:
			send(map[string]any{"type": "done"})
		}
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
