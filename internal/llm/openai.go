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
	Content    any           `json:"content,omitempty"` // string 或 []oaiContentPart
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

// oaiContentPart 是 OpenAI 多模态 content 数组里的一项。
type oaiContentPart struct {
	Type     string       `json:"type"` // "text" | "image_url"
	Text     string       `json:"text,omitempty"`
	ImageURL *oaiImageURL `json:"image_url,omitempty"`
}

type oaiImageURL struct {
	URL string `json:"url"`
}

// buildOAIContent 把一条消息转成 OpenAI 的 content 字段。
// 无图时返回字符串（有图时返回 []oaiContentPart）。
// 顺序：文本在前，图片在后——多数模型对"先读图再答题"的顺序更友好。
func buildOAIContent(m Message) any {
	if len(m.Images) == 0 {
		return m.Text
	}
	parts := make([]oaiContentPart, 0, len(m.Images)+1)
	if m.Text != "" {
		parts = append(parts, oaiContentPart{Type: "text", Text: m.Text})
	}
	for _, im := range m.Images {
		if im.Data == "" {
			continue
		}
		parts = append(parts, oaiContentPart{
			Type:     "image_url",
			ImageURL: &oaiImageURL{URL: im.DataURL()},
		})
	}
	return parts
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

// oaiContentText 从 content 字段（string 或多模态数组）里取出文本部分。
func oaiContentText(c any) string {
	switch v := c.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		// 反序列化后 Content 是 []any，需要重新解析成 oaiContentPart
		var b strings.Builder
		for _, it := range v {
			raw, _ := json.Marshal(it)
			var p oaiContentPart
			if err := json.Unmarshal(raw, &p); err != nil {
				continue
			}
			if p.Type == "text" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return ""
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
			// 有图时 content 必须是数组：[{type:text},{type:image_url},...]
			content := buildOAIContent(m)
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
				// assistant 带 tool_calls 时 content 必须是字符串（规范要求），丢弃图片
				messages = append(messages, oaiMessage{Role: "assistant", Content: m.Text, ToolCalls: tcs})
			} else {
				messages = append(messages, oaiMessage{Role: string(m.Role), Content: content})
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
			// tool 角色消息不能带图（OpenAI 规范），附带的图片另起一条 user 消息
			if len(m.Images) > 0 {
				messages = append(messages, oaiMessage{
					Role:    "user",
					Content: buildOAIContent(Message{Text: "以上工具产出的图片：", Images: m.Images}),
				})
			}
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
		// 流式模式下 usage 默认不下发，必须显式请求，否则用量统计全是 0
		"stream_options": map[string]any{"include_usage": true},
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

	// 带重试地发起请求（瞬时故障最多重试 10 次、每次间隔 5 秒）。
	// 为什么要重试：中转网关/免费模型（如 openrouter 的 :free 档、agnes）渠道池浅，
	// 偶发返回 500「上游失败」/ 503「无可用渠道」/ 502 / 429，单次失败不代表模型不可用。
	// 只对可重试的状态码重试；4xx（如 401 key 失效）立刻返回，避免掩盖真实配置错误。
	resp, err := p.doWithRetry(ctx, body, onEvent)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		err := p.httpError(resp)
		resp.Body.Close()
		return nil, err
	}

	// 空响应重试：上游 200 却什么都没回（文本/思考/工具全空）也视为瞬时故障——
	// 免费档/中转网关偶发这种「假完成」，不重试的话用户只会看到一句空兜底。
	// 沿用同一组重试参数；重试期间照常发 retry 事件，界面显示「重试 n/10」。
	emitted := false
	track := func(ev StreamEvent) error {
		if ev.Type == EventText || ev.Type == EventReasoning || ev.Type == EventToolCall {
			emitted = true
		}
		if onEvent == nil {
			return nil
		}
		return onEvent(ev)
	}
	for attempt := 0; ; attempt++ {
		out, serr := readOpenAIStream(ctx, resp.Body, track)
		if serr != nil {
			resp.Body.Close()
			return nil, serr
		}
		if out.Content != "" || len(out.ToolCalls) > 0 || emitted {
			resp.Body.Close()
			return out, nil
		}
		resp.Body.Close()
		if attempt >= maxRetries {
			// 一直空：原样返回，交给上层「模型没有返回任何内容」兜底
			return out, nil
		}
		notifyRetry(onEvent, attempt+1, maxRetries)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryDelay):
		}
		resp, err = p.doOnce(ctx, body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			err := p.httpError(resp)
			resp.Body.Close()
			return nil, err
		}
	}
}

// retryableStatus 可重试的 HTTP 状态码：网关/限流类瞬时错误。
// 500 也算——中转网关（如 agnes）上游故障时回「请求上游失败，请稍后重试」，属瞬时故障。
// 刻意不含 4xx（如 400/401/403/404）—— 那是请求或配置本身有问题，重试没意义。
func retryableStatus(code int) bool {
	switch code {
	case http.StatusInternalServerError, // 500 上游瞬时故障
		http.StatusTooManyRequests,    // 429 限流
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	}
	return false
}

// 重试策略：最多 10 次重试（含首次共 11 次请求），每次重试前固定等 5 秒。
// 网关/免费模型渠道池浅，偶发 500/503/429，单次失败不代表模型不可用。
const (
	maxRetries = 10
	retryDelay = 5 * time.Second
)

// doWithRetry 发送请求，遇可重试状态码或网络错误按固定间隔重试。
// 重试之间会通过 onEvent 发一条 reasoning 提示，让用户知道正在重试（否则界面像卡死）。
func (p *OpenAIProvider) doWithRetry(ctx context.Context, body []byte, onEvent func(StreamEvent) error) (*http.Response, error) {
	maxAttempts := maxRetries + 1
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			// 每次重试前等 5 秒（受 ctx 取消控制）
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay):
			}
		}

		resp, err := p.doOnce(ctx, body)
		if err != nil {
			// 网络层错误（EOF/超时）也重试；最后一次才抛
			lastErr = fmt.Errorf("请求 %s 失败: %w", p.baseURL, err)
			if attempt == maxAttempts {
				return nil, lastErr
			}
			notifyRetry(onEvent, attempt, maxRetries)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		if !retryableStatus(resp.StatusCode) || attempt == maxAttempts {
			return resp, nil // 交给调用方读 body 报错
		}
		// 可重试且还有次数：排干并关闭 body 继续下一轮
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		lastErr = fmt.Errorf("provider 返回 %d", resp.StatusCode)
		notifyRetry(onEvent, attempt, maxRetries)
	}

	return nil, lastErr
}

// doOnce 发起一次请求（无重试逻辑；重试由 doWithRetry 与 Chat 的空响应重试负责）。
func (p *OpenAIProvider) doOnce(ctx context.Context, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	return p.httpClient().Do(httpReq)
}

// notifyRetry 在重试前给前端一条独立的重试事件（不混进模型思考流），
// 让界面在「处理中」位置显示重试进度，避免长等待时看起来像卡死。
func notifyRetry(onEvent func(StreamEvent) error, attempt, max int) {
	if onEvent == nil {
		return
	}
	_ = onEvent(StreamEvent{
		Type:  EventRetry,
		Retry: &RetryInfo{Attempt: attempt, Max: max},
	})
}

// httpError 把非 200 响应转成可读错误，并针对 401/403 给出定位提示。
func (p *OpenAIProvider) httpError(resp *http.Response) error {
	var oe oaiErrorBody
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	_ = json.Unmarshal(data, &oe)
	msg := oe.Error.Message
	if msg == "" {
		msg = strings.TrimSpace(string(data))
	}
	// 中转网关常见文案不含 provider 名，补上便于定位是哪条配置
	// 上下文超限（400 context length）单独透传，提醒新建对话/缩短历史，避免被当成"换个模型"
	if resp.StatusCode == http.StatusBadRequest && msg != "" {
		lower := strings.ToLower(msg)
		if strings.Contains(lower, "context") && (strings.Contains(lower, "length") || strings.Contains(lower, "exceed")) {
			return fmt.Errorf("provider %s(%s) 返回 400: %s（对话上下文过长，请新建对话或缩短历史后重试）", p.model, p.baseURL, msg)
		}
	}
	return fmt.Errorf("provider %s(%s) 返回 %d: %s", p.model, p.baseURL, resp.StatusCode, msg)
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
				// content 可能是 string 或多模态数组，这里统一取出文本部分
				if txt := oaiContentText(ch.Message.Content); txt != "" {
					out.Content += txt
					if onEvent != nil {
						if err := onEvent(StreamEvent{Type: EventText, Text: txt}); err != nil {
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
