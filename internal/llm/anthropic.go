package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	Register("anthropic", func(cfg ConfigP) (Provider, error) {
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "https://api.anthropic.com/v1"
		}
		baseURL = strings.TrimRight(baseURL, "/")
		return &AnthropicProvider{
			baseURL: baseURL,
			apiKey:  cfg.APIKey,
			model:   cfg.Model,
		}, nil
	})
}

// AnthropicProvider 实现 Anthropic Messages API。
type AnthropicProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Models() []string {
	if p.model != "" {
		return []string{p.model}
	}
	return nil
}

type ancContentBlock struct {
	Type string `json:"type"` // text | tool_use | tool_result | thinking
	Text string `json:"text,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// 工具入参（tool_use 的 input 是对象）
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type ancMessage struct {
	Role    string            `json:"role"` // user | assistant
	Content []ancContentBlock `json:"content"`
}

type ancToolInputSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
	Required   []string       `json:"required,omitempty"`
}

type ancTool struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	InputSchema ancToolInputSchema `json:"input_schema"`
}

type ancResponse struct {
	Content []ancContentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StopReason string `json:"stop_reason"`
}

type ancEvent struct {
	Type         string           `json:"type"`
	Delta        *ancDelta        `json:"delta,omitempty"`
	Index        int              `json:"index"`
	ContentBlock *ancContentBlock `json:"content_block,omitempty"`
	Message      *ancResponse     `json:"message,omitempty"`
}

type ancDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// input_json_delta
	PartialJSON string `json:"partial_json,omitempty"`
}

func (p *AnthropicProvider) httpClient() *http.Client {
	if p.client == nil {
		p.client = &http.Client{Timeout: 600 * time.Second}
	}
	return p.client
}

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest, onEvent func(StreamEvent) error) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, fmt.Errorf("未指定模型，请通过配置或 /model 设置")
	}

	var messages []ancMessage
	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem:
			// system 通过 system 字段传，这里跳过
		case RoleUser, RoleAssistant:
			var blocks []ancContentBlock
			if m.Text != "" {
				blocks = append(blocks, ancContentBlock{Type: "text", Text: m.Text})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, ancContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: mustMarshalRaw(tc.Args),
				})
			}
			messages = append(messages, ancMessage{Role: string(m.Role), Content: blocks})
		case RoleTool:
			if m.ToolResult != nil {
				msg := ancMessage{Role: "user", Content: []ancContentBlock{{
					Type:      "tool_result",
					ToolUseID: m.ToolResult.CallID,
					Text:      m.ToolResult.Content,
					IsError:   m.ToolResult.IsError,
				}}}
				// 合并相邻 user 消息（Anthropic 要求交替）
				if len(messages) > 0 && messages[len(messages)-1].Role == "user" {
					messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, msg.Content...)
				} else {
					messages = append(messages, msg)
				}
			}
		}
	}

	system := req.System

	tools := make([]ancTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		schema := t.Parameters
		props, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]interface{})
		var reqStrs []string
		for _, r := range required {
			if s, ok := r.(string); ok {
				reqStrs = append(reqStrs, s)
			}
		}
		tools = append(tools, ancTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: ancToolInputSchema{Type: "object", Properties: props, Required: reqStrs},
		})
	}

	payload := map[string]any{
		"model":      model,
		"max_tokens": 8192,
		"stream":     true,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if system != "" {
		payload["system"] = system
	}
	if len(messages) > 0 {
		payload["messages"] = messages
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("anthropic-dangerous-direct-browser-access", "true")

	resp, err := p.httpClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求 %s 失败: %w", p.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider 返回 %d: %s", resp.StatusCode, string(data))
	}

	return readAnthropicStream(ctx, resp.Body, onEvent)
}

func readAnthropicStream(ctx context.Context, body io.Reader, onEvent func(StreamEvent) error) (*ChatResponse, error) {
	out := &ChatResponse{}
	// 流式 content 块：index -> {id, name, argsStr}
	blocks := map[int]*toolCallState{}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		var ev ancEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			if ev.Message != nil {
				out.Usage = &Usage{InputTokens: ev.Message.Usage.InputTokens, OutputTokens: ev.Message.Usage.OutputTokens}
			}
		case "content_block_start":
			if cb := ev.ContentBlock; cb != nil && ev.Index >= 0 {
				st := &toolCallState{id: cb.ID, name: cb.Name, argsStr: ""}
				if cb.Input != nil && len(cb.Input) > 2 {
					st.argsStr = string(cb.Input)
				}
				blocks[ev.Index] = st
			}
		case "content_block_delta":
			if cb, ok := blocks[ev.Index]; ok {
				if ev.Delta != nil {
					cb.argsStr += ev.Delta.PartialJSON
				}
			} else if ev.Delta != nil && ev.Delta.Type == "text_delta" {
				token := ev.Delta.Text
				out.Content += token
				if onEvent != nil {
					if err := onEvent(StreamEvent{Type: EventText, Text: token}); err != nil {
						return nil, err
					}
				}
			}
		case "content_block_stop", "message_delta", "message_stop":
			// 忽略状态事件
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return out, fmt.Errorf("读取流失败: %w", err)
	}

	for i := 0; i < len(blocks); i++ {
		st := blocks[i]
		if st == nil || st.name == "" {
			continue
		}
		tc := &ToolCall{ID: st.id, Name: st.name, Args: parseArgs(st.argsStr)}
		if tc.ID == "" {
			tc.ID = fmt.Sprintf("toolu_%d", len(out.ToolCalls))
		}
		out.ToolCalls = append(out.ToolCalls, *tc)
		if onEvent != nil {
			if err := onEvent(StreamEvent{Type: EventToolCall, Tool: tc}); err != nil {
				return out, err
			}
		}
	}
	if onEvent != nil {
		_ = onEvent(StreamEvent{Type: EventDone})
	}
	return out, nil
}

func mustMarshalRaw(v map[string]any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}
