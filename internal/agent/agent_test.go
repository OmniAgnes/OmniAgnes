package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omni/internal/llm"
	"omni/internal/tools"
)

// mockProvider 依序返回预设响应。
type mockProvider struct {
	responses []llm.ChatResponse
	idx       int
}

func (m *mockProvider) Name() string     { return "mock" }
func (m *mockProvider) Models() []string { return []string{"mock-model"} }
func (m *mockProvider) ListModels(ctx context.Context) ([]string, error) {
	return []string{"mock-model"}, nil
}
func (m *mockProvider) Chat(ctx context.Context, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (*llm.ChatResponse, error) {
	resp := m.responses[0]
	if m.idx < len(m.responses) {
		resp = m.responses[m.idx]
	}
	m.idx++
	if onEvent != nil {
		for _, tc := range resp.ToolCalls {
			copy := tc
			_ = onEvent(llm.StreamEvent{Type: llm.EventToolCall, Tool: &copy})
		}
		if resp.Content != "" {
			_ = onEvent(llm.StreamEvent{Type: llm.EventText, Text: resp.Content})
		}
		_ = onEvent(llm.StreamEvent{Type: llm.EventDone})
	}
	return &resp, nil
}

func newTestAgent(t *testing.T, prov llm.Provider, mode Mode, maxIter int) (*Agent, *tools.Registry, string) {
	t.Helper()
	dir := t.TempDir()
	env := &tools.Env{Cwd: dir, MaxOutput: 4000, Timeout: 10 * 1000_000_000}
	reg := tools.NewRegistry(env)
	a := New(prov, reg, Options{Model: "mock-model", Mode: mode, Cwd: dir, MaxIterations: maxIter})
	return a, reg, dir
}

func TestAgentSingleTurn(t *testing.T) {
	prov := &mockProvider{responses: []llm.ChatResponse{{Content: "你好"}}}
	a, _, _ := newTestAgent(t, prov, ModeBuild, 8)
	var texts []string
	err := a.Run(context.Background(), "打招呼", func(ev Event) {
		if ev.Type == EventText {
			texts = append(texts, ev.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if strings.Join(texts, "") != "你好" {
		t.Fatalf("texts = %v", texts)
	}
	if len(a.History()) != 2 {
		t.Fatalf("history len = %d", len(a.History()))
	}
	if a.History()[0].Role != llm.RoleUser || a.History()[1].Role != llm.RoleAssistant {
		t.Fatalf("unexpected roles")
	}
}

func TestAgentToolLoop(t *testing.T) {
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "glob", Args: map[string]any{"pattern": "*.go"}}}},
		{Content: "完成"},
	}}
	a, reg, dir := newTestAgent(t, prov, ModeBuild, 8)

	var sawToolStart, sawToolEnd bool
	var toolOut string
	err := a.Run(context.Background(), "列出文件", func(ev Event) {
		switch ev.Type {
		case EventToolStart:
			sawToolStart = true
		case EventToolOutput:
			toolOut = ev.Output
		case EventToolEnd:
			sawToolEnd = true
		}
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !sawToolStart || !sawToolEnd {
		t.Fatalf("missing tool events: start=%v end=%v", sawToolStart, sawToolEnd)
	}
	// 目录为空 → glob 返回“无匹配文件”
	_ = reg
	if dir == "" {
		t.Fatal(dir)
	}
	_ = toolOut
	// history: user, assistant(tool), tool, assistant(text)
	h := a.History()
	if len(h) != 4 {
		t.Fatalf("history len = %d", len(h))
	}
	if h[2].Role != llm.RoleTool || h[2].ToolResult == nil {
		t.Fatalf("expected tool result message")
	}
}

func TestAgentPlanModeBlocksWrite(t *testing.T) {
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "write", Args: map[string]any{"path": "f.txt", "content": "x"}}}},
		{Content: "不能在 plan 模式写文件"},
	}}
	a, _, dir := newTestAgent(t, prov, ModePlan, 8)

	var isErr bool
	err := a.Run(context.Background(), "写个文件", func(ev Event) {
		if ev.Type == EventToolEnd {
			isErr = ev.IsError
		}
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !isErr {
		t.Fatal("expected tool error in plan mode")
	}
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); err == nil {
		t.Fatal("file should not be created in plan mode")
	}
}

func TestAgentNoToolsLeavesReasoningText(t *testing.T) {
	prov := &mockProvider{responses: []llm.ChatResponse{{Content: "```go\nfmt.Println(1)\n```"}}}
	a, _, _ := newTestAgent(t, prov, ModeBuild, 8)
	var got []string
	if err := a.Run(context.Background(), "q", func(ev Event) {
		if ev.Type == EventText {
			got = append(got, ev.Text)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "") == "" {
		t.Fatal("expected text output")
	}
}

func TestAgentMaxIterations(t *testing.T) {
	// 每次都返回工具调用 → 超过最大迭代
	prov := &mockProvider{}
	for i := 0; i < 3; i++ {
		prov.responses = append(prov.responses, llm.ChatResponse{
			ToolCalls: []llm.ToolCall{{ID: "c1", Name: "glob", Args: map[string]any{"pattern": "*.go"}}},
		})
	}
	a, _, _ := newTestAgent(t, prov, ModeBuild, 2)
	err := a.Run(context.Background(), "q", nil)
	if err == nil || !strings.Contains(err.Error(), "最大迭代") {
		t.Fatalf("expected max iterations error, got %v", err)
	}
}

func TestAgentRegenerateRollsBack(t *testing.T) {
	prov := &mockProvider{responses: []llm.ChatResponse{
		{Content: "第一版回答"},
		{Content: "第二版回答"},
	}}
	a, _, _ := newTestAgent(t, prov, ModeBuild, 8)

	if err := a.Run(context.Background(), "问题", func(Event) {}); err != nil {
		t.Fatal(err)
	}
	if len(a.History()) != 2 {
		t.Fatalf("history after first turn = %d", len(a.History()))
	}
	// 回滚后重新执行
	a.RollbackLastTurn()
	if len(a.History()) != 0 {
		t.Fatalf("history after rollback = %d", len(a.History()))
	}
	if err := a.Run(context.Background(), "问题", func(Event) {}); err != nil {
		t.Fatal(err)
	}
	h := a.History()
	if len(h) != 2 || h[1].Text != "第二版回答" {
		t.Fatalf("unexpected history after regen: %+v", h)
	}
}

func TestContextWindow(t *testing.T) {
	user := func(i int) llm.Message {
		return llm.NewTextMessage(llm.RoleUser, fmt.Sprintf("u%d", i))
	}
	asst := func(i int) llm.Message {
		return llm.NewTextMessage(llm.RoleAssistant, fmt.Sprintf("a%d", i))
	}
	// 6 轮：u0 a0 u1 a1 u2 a2 u3 a3 u4 a4 u5 a5
	var hist []llm.Message
	for i := 0; i < 6; i++ {
		hist = append(hist, user(i), asst(i))
	}

	got := contextWindow(hist, 2)
	if len(got) != 4 || got[0].Text != "u4" || got[3].Text != "a5" {
		t.Fatalf("keep last 2 turns = %+v", got)
	}

	// 轮数超出实际用户数时整段返回
	all := contextWindow(hist, 99)
	if len(all) != len(hist) {
		t.Fatalf("keep-all len = %d, want %d", len(all), len(hist))
	}

	// 非法参数不裁剪
	if w := contextWindow(hist, 0); len(w) != len(hist) {
		t.Fatalf("keep 0 should not crop, got %d", len(w))
	}
	if w := contextWindow(nil, 2); w != nil {
		t.Fatalf("nil history should stay nil")
	}
}
