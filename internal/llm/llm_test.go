package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockSSEServer 返回一个发送原始 SSE 内容的测试服务器，并记录收到的请求体。
func mockSSEServer(t *testing.T, body string) (*httptest.Server, *json.RawMessage) {
	t.Helper()
	var got json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, 0, 4096)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			raw = append(raw, buf[:n]...)
			if err != nil {
				break
			}
		}
		got = json.RawMessage(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func sseChunk(namespace ...string) string {
	// 组装多个 data 行
	return strings.Join(namespace, "\n") + "\n\n"
}

func TestOpenAIStreamText(t *testing.T) {
	body := sseChunk(
		`data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"你"},"finish_reason":null}]}`,
	) + sseChunk(
		`data: {"id":"2","choices":[{"index":0,"delta":{"content":"好"},"finish_reason":null}]}`,
	) + sseChunk(
		`data: {"id":"3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	) + "data: [DONE]\n\n"

	srv, _ := mockSSEServer(t, body)
	p := &OpenAIProvider{baseURL: srv.URL, apiKey: "k", model: "gpt-4o"}

	var texts []string
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{NewTextMessage(RoleUser, "hi")},
	}, func(ev StreamEvent) error {
		if ev.Type == EventText {
			texts = append(texts, ev.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "你好" {
		t.Fatalf("Content = %q", resp.Content)
	}
	if len(texts) != 2 {
		t.Fatalf("events = %d", len(texts))
	}
}

// 思考开关：on/off 写进 thinking.type；未配置时不发该字段。
// 推理强度：配置了才发 reasoning_effort（仅部分模型支持）。
func TestOpenAIReasoningPayload(t *testing.T) {
	body := sseChunk(`data: {"choices":[{"index":0,"delta":{"content":"好"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n"
	cases := []struct {
		reasoning string
		effort    string
		want      string // 期望 payload 包含的片段
	}{
		{reasoning: "on", want: `"thinking":{"type":"enabled"}`},
		{reasoning: "off", want: `"thinking":{"type":"disabled"}`},
		{effort: "high", want: `"reasoning_effort":"high"`},
	}
	for _, c := range cases {
		srv, got := mockSSEServer(t, body)
		p := &OpenAIProvider{baseURL: srv.URL, apiKey: "k", model: "gpt-4o"}
		if _, err := p.Chat(context.Background(), ChatRequest{
			Messages:        []Message{NewTextMessage(RoleUser, "hi")},
			Reasoning:       c.reasoning,
			ReasoningEffort: c.effort,
		}, nil); err != nil {
			t.Fatalf("Chat(%q,%q) error: %v", c.reasoning, c.effort, err)
		}
		if !strings.Contains(string(*got), c.want) {
			t.Fatalf("payload 缺少 %s: %s", c.want, string(*got))
		}
	}
	// 未配置：payload 里不能出现 thinking / reasoning_effort
	srv, got := mockSSEServer(t, body)
	p := &OpenAIProvider{baseURL: srv.URL, apiKey: "k", model: "gpt-4o"}
	if _, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{NewTextMessage(RoleUser, "hi")},
	}, nil); err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if s := string(*got); strings.Contains(s, "thinking") || strings.Contains(s, "reasoning_effort") {
		t.Fatalf("未配置时不应发送思考参数: %s", s)
	}
}

func TestOpenAIStreamToolCall(t *testing.T) {
	args := `{"path":"a.txt"}`
	body := sseChunk(
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\""}}]}}]}`,
	) + sseChunk(
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"a.txt\"}"}}]}}]}`,
	) + sseChunk(
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	) + "data: [DONE]\n\n"

	srv, _ := mockSSEServer(t, body)
	p := &OpenAIProvider{baseURL: srv.URL, apiKey: "k", model: "gpt-4o"}
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{NewTextMessage(RoleUser, "read a.txt")},
	}, nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "read" {
		t.Fatalf("unexpected tool call: %+v", tc)
	}
	b, _ := json.Marshal(tc.Args)
	if string(b) != args {
		t.Fatalf("args = %s", b)
	}
}

// 思考/推理增量（reasoning_content / reasoning 两种风格）必须以 EventReasoning
// 透传，且不混进正文。
func TestOpenAIStreamReasoning(t *testing.T) {
	body := sseChunk(`data: {"choices":[{"index":0,"delta":{"reasoning_content":"让我想"}}]}`) + sseChunk(
		`data: {"choices":[{"index":0,"delta":{"reasoning":"一下"}}]}`,
	) + sseChunk(
		`data: {"choices":[{"index":0,"delta":{"content":"答案"}}]}`,
	) + "data: [DONE]\n\n"

	srv, _ := mockSSEServer(t, body)
	p := &OpenAIProvider{baseURL: srv.URL, apiKey: "k", model: "m"}
	var reasoning, text []string
	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{NewTextMessage(RoleUser, "hi")},
	}, func(ev StreamEvent) error {
		switch ev.Type {
		case EventReasoning:
			reasoning = append(reasoning, ev.Text)
		case EventText:
			text = append(text, ev.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if strings.Join(reasoning, "") != "让我想一下" {
		t.Fatalf("reasoning = %q", strings.Join(reasoning, ""))
	}
	if strings.Join(text, "") != "答案" {
		t.Fatalf("text = %q", strings.Join(text, ""))
	}
}

func TestOpenAIMessagesIncludeToolResult(t *testing.T) {
	body := sseChunk(`data: {"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n"
	srv, got := mockSSEServer(t, body)
	p := &OpenAIProvider{baseURL: srv.URL, apiKey: "k", model: "m"}

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: RoleAssistant, Text: "", ToolCalls: []ToolCall{{ID: "c1", Name: "bash", Args: map[string]any{"command": "ls"}}}},
			{Role: RoleTool, ToolResult: &ToolResult{CallID: "c1", Content: "out"}},
		},
	}, nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	payload := string(*got)
	if !strings.Contains(payload, `"tool_call_id":"c1"`) {
		t.Fatalf("missing tool_call_id: %s", payload)
	}
	if !strings.Contains(payload, `"role":"tool"`) {
		t.Fatalf("missing tool role: %s", payload)
	}
}

func TestOpenAIErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()
	p := &OpenAIProvider{baseURL: srv.URL, apiKey: "k", model: "m"}
	_, err := p.Chat(context.Background(), ChatRequest{Messages: []Message{}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnthropicStreamToolUse(t *testing.T) {
	contentBlockStart := `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"glob","input":{}}}`
	jsonStart := `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"pattern\":"}}`
	jsonMid := `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"**/*.go\"}"}}`
	stop := `{"type":"content_block_stop","index":0}`
	msgStop := `{"type":"message_stop"}`
	lines := []string{contentBlockStart, jsonStart, jsonMid, stop, msgStop}
	body := ""
	for _, l := range lines {
		body += "data: " + l + "\n\n"
	}

	srv, _ := mockSSEServer(t, body)
	p := &AnthropicProvider{baseURL: srv.URL, apiKey: "k", model: "claude"}
	resp, err := p.Chat(context.Background(), ChatRequest{Messages: []Message{NewTextMessage(RoleUser, "l")}}, nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.Name != "glob" {
		t.Fatalf("name = %q", tc.Name)
	}
	if fmt.Sprint(tc.Args["pattern"]) != "**/*.go" {
		t.Fatalf("args = %v", tc.Args)
	}
}

func TestGeminiStreamToolCall(t *testing.T) {
	chunk := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"read","args":{"path":"main.go"}}}]}}]}`
	body := "data: " + chunk + "\n\n"
	srv, _ := mockSSEServer(t, body)
	p := &GeminiProvider{baseURL: srv.URL, apiKey: "k", model: "gemini"}
	resp, err := p.Chat(context.Background(), ChatRequest{Messages: []Message{NewTextMessage(RoleUser, "read")}}, nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "read" {
		t.Fatalf("unexpected tool calls: %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Args["path"] != "main.go" {
		t.Fatalf("args = %v", resp.ToolCalls[0].Args)
	}
}

// 网关对 max_tokens 有上限（如 GLM 系 [1, 65536]），超限报 400。钳位重试前先准确识别这类错。
func TestClampMaxTokens(t *testing.T) {
	body := []byte(`{"model":"glm-4","max_tokens":200000,"messages":[{"role":"user","content":"hi"}]}`)
	over := fmt.Errorf("provider glm(https://x/v1) 返回 400: field MaxTokens invalid, should be in [1, 65536]")
	fixed, ok := clampMaxTokens(body, over)
	if !ok {
		t.Fatal("应识别 max_tokens 超限")
	}
	var payload map[string]any
	if err := json.Unmarshal(fixed, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["max_tokens"].(float64) != 65536 {
		t.Fatalf("max_tokens = %v, want 65536", payload["max_tokens"])
	}
	// 解析不出上限数字时兜底删除 max_tokens，用网关默认值
	noRange := fmt.Errorf("provider glm(https://x/v1) 返回 400: MaxTokens invalid")
	fixed2, ok := clampMaxTokens(body, noRange)
	if !ok {
		t.Fatal("应兜底删除 max_tokens")
	}
	if strings.Contains(string(fixed2), "max_tokens") {
		t.Fatalf("max_tokens 应被删除: %s", fixed2)
	}
	// 与 max_tokens 无关的错误不触发
	if _, ok := clampMaxTokens(body, fmt.Errorf("provider 返回 400: invalid api key")); ok {
		t.Fatal("无关错误不应触发")
	}
	// 未超限的错误也不动
	small := []byte(`{"model":"glm-4","max_tokens":1024,"messages":[]}`)
	if _, ok := clampMaxTokens(small, over); ok {
		t.Fatal("未超限不应触发")
	}
}
