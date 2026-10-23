package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// OpenCode 网关缺少 x-opencode-session 会直接 400：
// 同一对话内必须稳定，用户显式配置时以用户为准，其他网关不受影响。
func TestOpenCodeSessionHeader(t *testing.T) {
	p := &OpenAIProvider{baseURL: "https://opencode.ai/inference/go/openai/v1", session: sessionState{id: "instance"}}
	h1 := p.requestHeaders("conv-1")
	h2 := p.requestHeaders("conv-1")
	if h1["x-opencode-session"] != "conv-1" || h2["x-opencode-session"] != "conv-1" {
		t.Fatalf("会话头不稳定: %v / %v", h1, h2)
	}
	// 没传会话 ID（如连通性测试之外的低层调用）时退回 provider 自己的稳定标识
	if got := p.requestHeaders("")["x-opencode-session"]; got != "instance" {
		t.Fatalf("fallback session = %q", got)
	}

	// 用户配置了同名头：不覆盖（大小写不敏感）
	p2 := &OpenAIProvider{
		baseURL: "https://opencode.ai/inference/go/openai/v1",
		session: sessionState{id: "instance"},
		headers: map[string]string{"X-OpenCode-Session": "fixed"},
	}
	if got := p2.requestHeaders("conv-2")["X-OpenCode-Session"]; got != "fixed" {
		t.Fatalf("用户配置应优先: %q", got)
	}
	if _, ok := p2.requestHeaders("conv-2")["x-opencode-session"]; ok {
		t.Fatal("不应再注入第二份会话头")
	}

	// 非 opencode 网关不发送该头
	p3 := &OpenAIProvider{baseURL: "https://api.example.com/v1", session: sessionState{id: "instance"}}
	if _, ok := p3.requestHeaders("conv")["x-opencode-session"]; ok {
		t.Fatal("普通网关不应注入 x-opencode-session")
	}

	// 配了空值等同没配：照常补会话头，避免空头继续被网关拒绝
	p4 := &OpenAIProvider{
		baseURL: "https://opencode.ai/inference/go/openai/v1",
		session: sessionState{id: "instance"},
		headers: map[string]string{"x-opencode-session": "  "},
	}
	if got := p4.requestHeaders("conv-4")["x-opencode-session"]; got != "conv-4" {
		t.Fatalf("空会话头应被替换: %q", got)
	}
}

// 自定义头要真正进到请求里，且能覆盖默认的 Authorization。
func TestOpenAIProviderCustomHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	p := &OpenAIProvider{
		baseURL: srv.URL,
		apiKey:  "k",
		model:   "m",
		headers: map[string]string{
			"X-Tenant":      "acme",
			"Authorization": "Bearer custom-token",
		},
		session: sessionState{id: "s"},
	}
	if _, err := p.Chat(context.Background(), ChatRequest{
		Messages:  []Message{NewTextMessage(RoleUser, "hi")},
		SessionID: "conv",
	}, nil); err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if got.Get("X-Tenant") != "acme" {
		t.Fatalf("缺少自定义头: %v", got)
	}
	if got.Get("Authorization") != "Bearer custom-token" {
		t.Fatalf("自定义 Authorization 应覆盖默认值: %q", got.Get("Authorization"))
	}
}
