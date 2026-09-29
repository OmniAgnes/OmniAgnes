package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

// Role 是消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall 是一次工具调用请求。
type ToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"arguments"`
}

// ToolResult 是一次工具执行结果。
type ToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// Message 是多模态消息：文本或工具调用/结果的组合。
type Message struct {
	Role       Role        `json:"role"`
	Text       string      `json:"text,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
}

// NewTextMessage 便捷构造。
func NewTextMessage(role Role, text string) Message {
	return Message{Role: role, Text: text}
}

// ToolDef 描述一个可供模型调用的工具（JSON Schema 风格）。
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// StreamEventType 流式事件类型。
type StreamEventType string

const (
	EventText     StreamEventType = "text"      // 文本增量
	EventToolCall StreamEventType = "tool_call" // 工具调用开始（含完整参数）
	EventToolArgs StreamEventType = "tool_args" // 工具参数增量（可选）
	EventDone     StreamEventType = "done"
	EventError    StreamEventType = "error"
	EventUsage    StreamEventType = "usage" // 令牌用量
)

// Usage 记录令牌统计。
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// StreamEvent 是流式回调事件。
type StreamEvent struct {
	Type  StreamEventType
	Text  string
	Tool  *ToolCall
	Usage *Usage
	Err   error
}

// ChatRequest 是 Chat 的入参。
type ChatRequest struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
}

// ChatResponse 是 Chat 的返回。
type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
	Usage     *Usage
}

// Provider 是 LLM 提供方抽象。
type Provider interface {
	// Name 返回类型名，如 openai / anthropic / gemini。
	Name() string
	// Models 返回可用的模型列表（可能为空）。
	Models() []string
	// Chat 流式调用。onEvent 收到增量；返回最终结构化结果。
	Chat(ctx context.Context, req ChatRequest, onEvent func(StreamEvent) error) (*ChatResponse, error)
}

// Registry 是 provider 工厂注册表。
type Registry struct {
	factories map[string]Factory
}

// Factory 按 provider 类型构造实例。
type Factory func(cfg ConfigP) (Provider, error)

// ConfigP 描述 provider 通用实例化参数。
type ConfigP struct {
	Type    string
	Model   string
	BaseURL string
	APIKey  string
}

var globalRegistry = &Registry{factories: map[string]Factory{}}

// Register 注册 provider 工厂（通常在各实现 init 中调用）。
func Register(name string, f Factory) {
	globalRegistry.factories[name] = f
}

// New 通过注册表构造 provider。
func New(cfg ConfigP) (Provider, error) {
	f, ok := globalRegistry.factories[cfg.Type]
	if !ok {
		// ollama 复用 openai 实现
		if cfg.Type == "ollama" {
			f, ok = globalRegistry.factories["openai"]
		}
	}
	if !ok {
		return nil, fmt.Errorf("未知 provider 类型: %s", cfg.Type)
	}
	return f(cfg)
}

// MarshalToolArgs 将参数 map 压成 JSON 字符串。
func MarshalToolArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	b, err := json.MarshalIndent(args, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}
