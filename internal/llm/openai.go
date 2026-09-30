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
	Register("openai", func(cfg ConfigP) (Provider, error) {
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		baseURL = strings.TrimRight(baseURL, "/")
		return &OpenAIProvider{
			baseURL: baseURL,
			apiKey:  cfg.APIKey,
			model:   cfg.Model,
		}, nil
	})
}

// OpenAIProvider 实现 OpenAI 兼容的 chat/completions 流式调用。
type OpenAIProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Models() []string {
	if p.model != "" {
		return []string{p.model}
	}
	return nil
}

// ListModels 走 OpenAI 兼容的 GET {base}/models（ollama 同端点）。
func (p *OpenAIProvider) ListModels(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	// ollama 不校验 key；其余按 Bearer 传
	headers := map[string]string{}
	if p.apiKey != "" {
		headers["Authorization"] = "Bearer " + p.apiKey
	}
	if err := fetchJSON(ctx, p.httpClient(), p.baseURL+"/models", headers, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Data))
	seen := map[string]bool{}
	for _, d := range out.Data {
		if d.ID == "" || seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		names = append(names, d.ID)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("供应商返回空模型列表")
	}
	return names, nil
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

type oaiTool struct {
	Type     string          `json:"type"`
	Function oaiToolFunction `json:"function"`
}

type oaiToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type oaiChoice struct {
	Delta        *oaiDelta   `json:"delta,omitempty"`
	Message      *oaiMessage `json:"message,omitempty"`
	FinishReason string      `json:"finish_reason"`
}

type oaiDelta struct {
	Content   *string       `json:"content,omitempty"`
	ToolCalls []oaiToolCall `json:"tool_calls,omitempty"`
	// 思考/推理增量：reasoning_content 为 GLM/DeepSeek 风格，reasoning 为 OpenRouter 风格
	ReasoningContent *string `json:"reasoning_content,omitempty"`
	Reasoning        *string `json:"reasoning,omitempty"`
}

type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type oaiSseChunk struct {
	Choices []oaiChoice `json:"choices"`
	Usage   *oaiUsage   `json:"usage,omitempty"`
}

type oaiErrorBody struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *OpenAIProvider) httpClient() *http.Client {
	if p.client == nil {
		p.client = &http.Client{Timeout: 600 * time.Second}
	}
	return p.client
}

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest, onEvent func(StreamEvent) error) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, fmt.Errorf("未指定模型，请通过配置或 /model 设置")
	}

	messages := make([]oaiMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, oaiMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem:
			messages = append(messages, oaiMessage{Role: "system", Content: m.Text})
		case RoleUser, RoleAssistant:
			if len(m.ToolCalls) > 0 {
				tcs := make([]oaiToolCall, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					var t oaiToolCall
					t.ID = tc.ID
					t.Type = "function"
					t.Function.Name = tc.Name
					b, _ := json.Marshal(tc.Args)
					t.Function.Arguments = string(b)
					tcs = append(tcs, t)
				}
				messages = append(messages, oaiMessage{Role: "assistant", Content: m.Text, ToolCalls: tcs})
			} else {
				messages = append(messages, oaiMessage{Role: string(m.Role), Content: m.Text})
			}
		case RoleTool:
			if m.ToolResult == nil {
				return nil, fmt.Errorf("tool 消息缺少结果")
			}
			messages = append(messages, oaiMessage{
				Role:       "tool",
				ToolCallID: m.ToolResult.CallID,
				Content:    m.ToolResult.Content,
			})
		}
	}

	tools := make([]oaiTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, oaiTool{
			Type: "function",
			Function: oaiToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	payload := map[string]any{
		"model":       model,
		"messages":    messages,
		"stream":      true,
		"temperature": 0.2,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求 %s 失败: %w", p.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var oe oaiErrorBody
		data, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(data, &oe)
		msg := oe.Error.Message
		if msg == "" {
			msg = string(data)
		}
		return nil, fmt.Errorf("provider 返回 %d: %s", resp.StatusCode, msg)
	}

	return readOpenAIStream(ctx, resp.Body, onEvent)
}

// toolCallState 累积单个工具调用的流式字段。
type toolCallState struct {
	id      string
	name    string
	argsStr string
}

func readOpenAIStream(ctx context.Context, body io.Reader, onEvent func(StreamEvent) error) (*ChatResponse, error) {
	out := &ChatResponse{}
	pending := map[int]*toolCallState{}
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
		var chunk oaiSseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			out.Usage = &Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}
			if onEvent != nil {
				_ = onEvent(StreamEvent{Type: EventUsage, Usage: out.Usage})
			}
		}
		for _, ch := range chunk.Choices {
			if ch.Delta != nil {
				if ch.Delta.ReasoningContent != nil && *ch.Delta.ReasoningContent != "" {
					if onEvent != nil {
						if err := onEvent(StreamEvent{Type: EventReasoning, Text: *ch.Delta.ReasoningContent}); err != nil {
							return nil, err
						}
					}
				}
				if ch.Delta.Reasoning != nil && *ch.Delta.Reasoning != "" {
					if onEvent != nil {
						if err := onEvent(StreamEvent{Type: EventReasoning, Text: *ch.Delta.Reasoning}); err != nil {
							return nil, err
						}
					}
				}
				if ch.Delta.Content != nil && *ch.Delta.Content != "" {
					out.Content += *ch.Delta.Content
					if onEvent != nil {
						if err := onEvent(StreamEvent{Type: EventText, Text: *ch.Delta.Content}); err != nil {
							return nil, err
						}
					}
				}
				for _, td := range ch.Delta.ToolCalls {
					st, ok := pending[td.Index]
					if !ok {
						st = &toolCallState{}
						pending[td.Index] = st
					}
					if td.ID != "" {
						st.id = td.ID
					}
					if td.Function.Name != "" {
						st.name = td.Function.Name
					}
					st.argsStr += td.Function.Arguments
				}
			}
			if ch.Message != nil {
				if ch.Message.Content != "" {
					out.Content += ch.Message.Content
					if onEvent != nil {
						if err := onEvent(StreamEvent{Type: EventText, Text: ch.Message.Content}); err != nil {
							return nil, err
						}
					}
				}
				for _, mct := range ch.Message.ToolCalls {
					tc := &ToolCall{ID: mct.ID, Name: mct.Function.Name, Args: parseArgs(mct.Function.Arguments)}
					out.ToolCalls = append(out.ToolCalls, *tc)
					if onEvent != nil {
						if err := onEvent(StreamEvent{Type: EventToolCall, Tool: tc}); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return out, fmt.Errorf("读取流失败: %w", err)
	}

	for i := 0; i < len(pending); i++ {
		st := pending[i]
		if st == nil {
			continue
		}
		tc := &ToolCall{
			ID:   st.id,
			Name: st.name,
			Args: parseArgs(st.argsStr),
		}
		if tc.ID == "" {
			tc.ID = fmt.Sprintf("call_%d", len(out.ToolCalls))
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

func parseArgs(s string) map[string]any {
	args := map[string]any{}
	if strings.TrimSpace(s) == "" {
		return args
	}
	if err := json.Unmarshal([]byte(s), &args); err != nil {
		args["_raw"] = s
	}
	return args
}
