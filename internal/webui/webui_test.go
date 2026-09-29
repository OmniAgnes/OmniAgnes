package webui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"omni/internal/agent"
	"omni/internal/llm"
	"omni/internal/tools"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }

func (fakeProvider) Models() []string { return []string{"fake-model"} }

func (fakeProvider) Chat(ctx context.Context, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (*llm.ChatResponse, error) {
	_ = onEvent(llm.StreamEvent{Type: llm.EventText, Text: "你好"})
	return &llm.ChatResponse{Content: "你好"}, nil
}

func newTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	reg := tools.NewRegistry(&tools.Env{Cwd: t.TempDir()})
	return agent.New(fakeProvider{}, reg, agent.Options{Model: "fake-model", Mode: agent.ModeBuild, Cwd: t.TempDir()})
}

func TestWebUIEndToEnd(t *testing.T) {
	a := newTestAgent(t)
	url, err := Start(a)
	if err != nil {
		t.Fatal(err)
	}

	// 页面
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(page), "omni") {
		t.Fatalf("页面异常: %d %s", resp.StatusCode, string(page[:min(100, len(page))]))
	}

	// 状态
	resp, err = http.Get(url + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	state, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(state), `"configured":true`) || !strings.Contains(string(state), "fake-model") {
		t.Fatalf("state 异常: %s", state)
	}

	// 聊天 SSE
	resp, err = http.Post(url+"/api/chat", "application/json", strings.NewReader(`{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	s := string(body)
	if !strings.Contains(s, `"text":"你好"`) || !strings.Contains(s, `"type":"done"`) {
		t.Fatalf("SSE 异常: %s", s)
	}

	// 重复 Start 复用同一地址
	url2, err := Start(a)
	if err != nil || url2 != url {
		t.Fatalf("Start 应复用: %s vs %s (%v)", url, url2, err)
	}
}

func TestWebUIUnconfigured(t *testing.T) {
	reg := tools.NewRegistry(&tools.Env{Cwd: t.TempDir()})
	a := agent.New(nil, reg, agent.Options{Cwd: t.TempDir()})
	// 直接挂 handleChat，避开 Start 的单例复用
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/chat", func(w http.ResponseWriter, r *http.Request) {
		handleChat(a, w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/chat", "application/json", strings.NewReader(`{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("未配置应返回 503，实际 %d", resp.StatusCode)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
