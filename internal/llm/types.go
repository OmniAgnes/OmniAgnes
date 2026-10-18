package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	// Attachments 是工具产出的多媒体（截图、生成的图片/视频/音频），
	// 供前端渲染；图片类同时会作为多模态输入送进模型。
	Attachments []Attachment `json:"attachments,omitempty"`
}

// AttachmentKind 是多媒体附件类型。
type AttachmentKind string

const (
	KindImage AttachmentKind = "image"
	KindVideo AttachmentKind = "video"
	KindAudio AttachmentKind = "audio"
	KindFile  AttachmentKind = "file" // 生成类文档（application/pdf 等），前端以文件卡片/预览展示，不进模型上下文
)

// Attachment 是一条多媒体产出（图片/视频/音频）。
// Data / URL / Path 三选一：Data 为 base64（小图内联），URL 为远程地址，
// Path 为本地文件（大文件走 /api/attachment 端点按需读取，避免撑爆历史 JSON）。
type Attachment struct {
	Kind      AttachmentKind `json:"kind"`
	MediaType string         `json:"media_type,omitempty"`
	Data      string         `json:"data,omitempty"`
	URL       string         `json:"url,omitempty"`
	Path      string         `json:"path,omitempty"`
	Caption   string         `json:"caption,omitempty"`
	// ForModel 标记这张图需要送进模型（如截图/读图，供视觉分析）。
	// 生成类工具（生图/生视频/TTS）的产出只给 UI 展示，不置此标记，省 token。
	ForModel bool `json:"for_model,omitempty"`
}

// DataURL 拼 data URL（仅当有 Data 时有效）；无 Data 时回落到 URL。
func (a Attachment) DataURL() string {
	if a.Data == "" {
		return a.URL
	}
	mt := a.MediaType
	if mt == "" {
		mt = string(a.Kind) + "/*"
	}
	return "data:" + mt + ";base64," + a.Data
}

// ByteSize 估算 base64 解码后的字节数。
func (a Attachment) ByteSize() int {
	if a.Data == "" {
		return 0
	}
	n := len(a.Data) * 3 / 4
	if len(a.Data)%4 != 0 {
		n++
	}
	return n
}

// IsImage 报告是否为图片类附件。
func (a Attachment) IsImage() bool { return a.Kind == KindImage }

// Image 是一张随消息携带的图片（多模态）。
// 只保存 data URL 形如 "data:image/png;base64,...."，由各 provider 在序列化时
// 转成自家格式（OpenAI image_url / Anthropic source / Gemini inlineData）。
type Image struct {
	// MediaType 如 image/png、image/jpeg（不含 data: 前缀）
	MediaType string `json:"media_type"`
	// Data 是 base64 编码的原始字节（不含 data: URL 前缀）
	Data string `json:"data"`
}

// DataURL 拼出完整 data URL。
func (im Image) DataURL() string {
	mt := im.MediaType
	if mt == "" {
		mt = "image/png"
	}
	return "data:" + mt + ";base64," + im.Data
}

// 估算字节数（base64 解码后的原始大小），用于超限判断。
func (im Image) ByteSize() int {
	if im.Data == "" {
		return 0
	}
	// base64：4 字符 = 3 字节，减去可能的 '=' 填充
	n := len(im.Data) * 3 / 4
	if len(im.Data)%4 != 0 {
		n++
	}
	return n
}

// ImageType 按文件头魔数嗅探图片类型（png/jpeg/gif/webp/bmp），返回 image/* MIME；
// 不是常见图片格式返回空串。用于防止把 PDF 等误标成图片的附件送进多模态上下文
// （上游对内容和 media_type 不符的图片直接报 400）。
func ImageType(data []byte) string {
	switch {
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 4 && string(data[:4]) == "GIF8":
		return "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	case len(data) >= 2 && string(data[:2]) == "BM":
		return "image/bmp"
	}
	return ""
}

// IsSVG 粗略判断字节内容是否为 SVG（纯文本 XML，无魔数，按首次出现 <svg 识别）。
func IsSVG(data []byte) bool {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	return bytes.Contains(bytes.ToLower(head), []byte("<svg"))
}

// ValidImageB64 判断 base64 图片数据开头是否为常见图片（或 SVG）。
// 只解码前 32 字节左右，成本常数，供发送前对历史持久化的图片做消毒：
// 历史上曾有 PDF 冒充 image/png 落进会话文件，之后每轮请求都把上游打成 400。
func ValidImageB64(data string) bool {
	if data == "" {
		return false
	}
	// 32 字节需 ~44 个 base64 字符；不足时取全长。截断会剩下非 4 倍数残字节，
	// 用 RawStdEncoding 解前缀即可，省去补齐与去 padding 的边界处理。
	n := 44
	if len(data) < n {
		n = len(data)
	}
	head, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(data[:n], "="))
	if err != nil {
		return false
	}
	return ImageType(head) != "" || IsSVG(head)
}

// Message 是多模态消息：文本、图片、工具调用/结果的组合。
type Message struct {
	Role       Role        `json:"role"`
	Text       string      `json:"text,omitempty"`
	Images     []Image     `json:"images,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
	// ElapsedMs 是这一轮（最终回答）的工作耗时（毫秒），仅 assistant 最终答复上有值；
	// 持久化后重启会话仍能在回复上显示「已工作」，不为 0 时省略。
	ElapsedMs int64 `json:"elapsed_ms,omitempty"`
	// ReplyModel / OutputTokens 记录最终答复由哪个模型产出、输出多少 token，
	// 与 ElapsedMs 一起供回复末行显示「模型名 · Token速度 · 耗时」；仅最终答复有值。
	ReplyModel   string `json:"reply_model,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
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
	EventText      StreamEventType = "text"      // 文本增量
	EventReasoning StreamEventType = "reasoning" // 思考/推理内容增量（不进历史，仅供 UI 展示）
	EventToolCall  StreamEventType = "tool_call" // 工具调用开始（含完整参数）
	EventToolArgs  StreamEventType = "tool_args" // 工具参数增量（可选）
	EventDone      StreamEventType = "done"
	EventError     StreamEventType = "error"
	EventUsage     StreamEventType = "usage" // 令牌用量
	EventRetry     StreamEventType = "retry" // 上游瞬时故障，provider 正在自动重试（仅 UI 展示）
)

// Usage 记录令牌统计。
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// RetryInfo 描述一次上游重试的进度（仅 UI 展示用，不进历史）。
type RetryInfo struct {
	Attempt int // 当前第几次重试（从 1 起）
	Max     int // 最多重试次数
}

// StreamEvent 是流式回调事件。
type StreamEvent struct {
	Type  StreamEventType
	Text  string
	Tool  *ToolCall
	Usage *Usage
	Err   error
	Retry *RetryInfo
}

// ChatRequest 是 Chat 的入参。
type ChatRequest struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
	// Reasoning 思考开关：on | off；空 = 不控制，跟随服务端默认。
	Reasoning string
	// ReasoningEffort 推理强度：low | medium | high；空 = 不发送（模型不支持时留空）。
	ReasoningEffort string
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
	// ListModels 向供应商 API 实时拉取模型清单（GET /models）。
	// 不支持或失败时返回 error，调用方应回退到本地配置列表。
	ListModels(ctx context.Context) ([]string, error)
	// Chat 流式调用。onEvent 收到增量；返回最终结构化结果。
	Chat(ctx context.Context, req ChatRequest, onEvent func(StreamEvent) error) (*ChatResponse, error)
}

// fetchJSON 发 GET 请求并把响应解析到 out（out 为 nil 时只检查状态码）。
func fetchJSON(ctx context.Context, c *http.Client, url string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("请求 %s 失败: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
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
