package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func init() {
	Register("gemini", func(cfg ConfigP) (Provider, error) {
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com/v1beta"
		}
		baseURL = strings.TrimRight(baseURL, "/")
		return &GeminiProvider{
			baseURL: baseURL,
			apiKey:  cfg.APIKey,
			model:   cfg.Model,
		}, nil
	})
}

// GeminiProvider 实现 Gemini streamGenerateContent API。
type GeminiProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func (p *GeminiProvider) Name() string { return "gemini" }

// ListModels 走 Gemini 的 GET {base}/models?key=...（name 形如 "models/gemini-2.5-pro"，去掉前缀）。
func (p *GeminiProvider) ListModels(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	endpoint := fmt.Sprintf("%s/models?key=%s", p.baseURL, url.QueryEscape(p.apiKey))
	if err := fetchJSON(ctx, p.httpClient(), endpoint, nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Models))
	for _, mdl := range out.Models {
		n := strings.TrimPrefix(mdl.Name, "models/")
		if n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("供应商返回空模型列表")
	}
	return names, nil
}

func (p *GeminiProvider) Models() []string {
	if p.model != "" {
		return []string{p.model}
	}
	return nil
}

type gemPart struct {
	Text             string       `json:"text,omitempty"`
	Thought          bool         `json:"thought,omitempty"` // true = 思考/推理内容
	FunctionCall     *gemFuncCall `json:"functionCall,omitempty"`
	FunctionResponse *gemFuncResp `json:"functionResponse,omitempty"`
}

type gemFuncCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type gemFuncResp struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type gemContent struct {
	Role  string    `json:"role,omitempty"` // user | model | function
	Parts []gemPart `json:"parts"`
}

type gemToolDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type gemTool struct {
	FunctionDeclarations []gemToolDecl `json:"functionDeclarations"`
}

type gemResponseChunk struct {
	Candidates []struct {
		Content      gemContent `json:"content"`
		FinishReason string     `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *GeminiProvider) httpClient() *http.Client {
	if p.client == nil {
		p.client = &http.Client{Timeout: 600 * time.Second}
	}
	return p.client
}

func (p *GeminiProvider) Chat(ctx context.Context, req ChatRequest, onEvent func(StreamEvent) error) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, fmt.Errorf("未指定模型，请通过配置或 /model 设置")
	}

	var contents []gemContent
	if req.System != "" {
		contents = append(contents, gemContent{Role: "user", Parts: []gemPart{{Text: "System instruction: " + req.System}}})
	}

	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem:
			contents = append(contents, gemContent{Role: "user", Parts: []gemPart{{Text: "System instruction: " + m.Text}}})
		case RoleUser, RoleAssistant:
			role := "user"
			if m.Role == RoleAssistant {
				role = "model"
			}
			var parts []gemPart
			if m.Text != "" {
				parts = append(parts, gemPart{Text: m.Text})
			}
			for _, tc := range m.ToolCalls {
				args := map[string]any{}
				if err := json.Unmarshal(mustMarshalRaw(tc.Args), &args); err != nil {
					args = tc.Args
				}
				b, _ := json.Marshal(args)
				parts = append(parts, gemPart{FunctionCall: &gemFuncCall{Name: tc.Name, Args: b}})
			}
			contents = append(contents, gemContent{Role: role, Parts: parts})
		case RoleTool:
			if m.ToolResult != nil {
				parts := []gemPart{{
					FunctionResponse: &gemFuncResp{
						Name: m.ToolResult.CallID,
						Response: map[string]any{
							"result":  m.ToolResult.Content,
							"isError": m.ToolResult.IsError,
						},
					},
				}}
				contents = append(contents, gemContent{Role: "function", Parts: parts})
			}
		}
	}

	tools := make([]gemTool, 0, 1)
	if len(req.Tools) > 0 {
		decls := make([]gemToolDecl, 0, len(req.Tools))
		for _, t := range req.Tools {
			decls = append(decls, gemToolDecl{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
		}
		tools = append(tools, gemTool{FunctionDeclarations: decls})
	}

	payload := map[string]any{"contents": contents}
	if len(tools) > 0 {
		payload["tools"] = tools
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", p.baseURL, url.PathEscape(model), url.QueryEscape(p.apiKey))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求 %s 失败: %w", p.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider 返回 %d: %s", resp.StatusCode, data)
	}

	return readGeminiStream(ctx, resp.Body, onEvent)
}

func readGeminiStream(ctx context.Context, body io.Reader, onEvent func(StreamEvent) error) (*ChatResponse, error) {
	out := &ChatResponse{}
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
		var chunk gemResponseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		// 流式尾包的 usageMetadata 是累计值：直接覆盖记录，流结束时取最后一次
		if chunk.UsageMetadata != nil {
			out.Usage = &Usage{
				InputTokens:  chunk.UsageMetadata.PromptTokenCount,
				OutputTokens: chunk.UsageMetadata.CandidatesTokenCount,
			}
			if onEvent != nil {
				_ = onEvent(StreamEvent{Type: EventUsage, Usage: out.Usage})
			}
		}
		if chunk.Error != nil {
			if onEvent != nil {
				_ = onEvent(StreamEvent{Type: EventError, Err: fmt.Errorf("%s", chunk.Error.Message)})
			}
			return out, fmt.Errorf("gemini error: %s", chunk.Error.Message)
		}
		for _, cand := range chunk.Candidates {
			for _, part := range cand.Content.Parts {
				if part.Text != "" && part.Thought {
					// 思考/推理增量：仅供 UI 展示，不进正文与历史
					if onEvent != nil {
						if err := onEvent(StreamEvent{Type: EventReasoning, Text: part.Text}); err != nil {
							return nil, err
						}
					}
					continue
				}
				if part.Text != "" {
					out.Content += part.Text
					if onEvent != nil {
						if err := onEvent(StreamEvent{Type: EventText, Text: part.Text}); err != nil {
							return nil, err
						}
					}
				}
				if part.FunctionCall != nil {
					fc := part.FunctionCall
					tc := &ToolCall{ID: fc.Name, Name: fc.Name, Args: parseArgs(string(fc.Args))}
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
	if onEvent != nil {
		_ = onEvent(StreamEvent{Type: EventDone})
	}
	return out, nil
}
