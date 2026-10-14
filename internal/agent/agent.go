package agent

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"omni/internal/llm"
	"omni/internal/tools"
)

// Mode 是 Agent 运行模式。
type Mode string

const (
	// ModeBuild 完全访问：直接执行写文件、跑命令等操作。
	ModeBuild Mode = "build"
	// ModePlan 普通访问：写文件、跑命令前需用户逐条确认。
	ModePlan Mode = "plan"
)

// EventType 是 Agent 向 UI 发出的事件类型。
type EventType string

const (
	EventText       EventType = "text"       // 助手文本增量
	EventReasoning  EventType = "reasoning"  // 思考/推理内容增量（不进历史）
	EventToolStart  EventType = "tool_start" // 工具开始执行
	EventToolOutput EventType = "tool_output"
	EventToolEnd    EventType = "tool_end"
	EventApproval   EventType = "approval" // 普通访问模式：等待用户确认写操作
	EventQuestion   EventType = "question" // ask 工具：等待用户回答澄清问题
	EventError      EventType = "error"
	EventMedia      EventType = "media" // 本轮产出的多媒体（图片/视频/音频），在回答结束时集中发出
	EventRetry      EventType = "retry" // 上游瞬时故障，provider 正在自动重试（供 UI 显示重试进度）
	EventDone       EventType = "done"  // 本轮问答结束
)

// Event 是发给 UI 的单一事件。
type Event struct {
	Type    EventType
	Text    string // 文本增量
	Tool    *llm.ToolCall
	Output  string
	IsError bool
	Err     error
	Model   string
	// Attachments 是工具产出的多媒体附件（图片/视频/音频），随 EventToolOutput 一起发出。
	Attachments []llm.Attachment
	// Usage 仅在 EventDone 上有值：本次提问所有 LLM 调用累计的令牌用量。
	Usage *llm.Usage
	// Retry 仅在 EventRetry 上有值：上游重试进度。
	Retry *llm.RetryInfo
	// Round 是 LLM 迭代轮次（从 0 开始）。
	// 一次提问会跑多轮：每轮 = 一段文本 + 若干工具调用。
	// UI 据此把每轮拆成独立消息，否则各轮文本与工具会全部堆在一起。
	Round int
}

// Options 构造 Agent 的参数。
type Options struct {
	Model         string
	Mode          Mode
	Cwd           string
	MaxIterations int
	MaxTokens     int
	// SystemPromptExtra 是静态附加段（来自配置）。
	SystemPromptExtra string
	// ExtraPrompt 每轮构建系统提示词时调用，返回动态附加段（技能清单、全局记忆等）；可为 nil。
	ExtraPrompt func() string
	// History 是初始消息历史（恢复持久化会话用）；为空则从空白开始。
	History []llm.Message
	// MaxHistoryTurns 发送给模型的上下文里保留的最近用户提问轮数；
	// 超出部分本轮不发给模型（完整历史仍持久化）。<=0 用默认值 defaultHistoryTurns。
	MaxHistoryTurns int
	// Approve 在普通访问（ModePlan）模式下于写操作前被调用，
	// 返回 true 允许执行，false 拒绝。nil 表示不询问（完全访问）。
	// callID 为本次工具调用 ID，便于 UI 把确认卡片与工具卡片对应起来。
	Approve func(callID, name string, args map[string]any) bool
	// Ask 在模型调用 ask 工具时被调用：向用户展示问题、阻塞等待回答，返回答案文本。
	// 为 nil 时 ask 走注册表兜底（提示按合理假设继续）。
	Ask func(callID, question string, options []string) string
	// Reasoning 按模型名解析思考参数：开关（on | off，空 = 跟随服务端默认）
	// 与推理强度（low | medium | high，空 = 不发送）。每轮请求前调用，
	// 模型中途切换时下一轮自动跟随；nil = 不控制。
	Reasoning func(model string) (onOff, effort string)
}

// Agent 持有一个 LLM provider、工具注册表与消息历史。
// TUI 与 webui 可能共用同一实例，历史相关操作以 mu 串行化。
type Agent struct {
	mu            sync.Mutex
	provider      llm.Provider
	tools         *tools.Registry
	model         string
	mode          Mode
	cwd           string
	maxIter       int
	maxToken      int
	extra         string
	extraPrompt   func() string
	approve       func(callID, name string, args map[string]any) bool
	question      func(callID, question string, options []string) string
	reasoning     func(model string) (onOff, effort string)
	history       []llm.Message
	lastTurnStart int
	maxKeepTurns  int
}

// New 创建 Agent。
func New(provider llm.Provider, reg *tools.Registry, opts Options) *Agent {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 500
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 8192
	}
	keep := opts.MaxHistoryTurns
	if keep <= 0 {
		keep = defaultHistoryTurns
	}
	return &Agent{
		provider:      provider,
		tools:         reg,
		model:         opts.Model,
		mode:          opts.Mode,
		cwd:           opts.Cwd,
		maxIter:       opts.MaxIterations,
		maxToken:      opts.MaxTokens,
		extra:         opts.SystemPromptExtra,
		extraPrompt:   opts.ExtraPrompt,
		approve:       opts.Approve,
		question:      opts.Ask,
		reasoning:     opts.Reasoning,
		history:       opts.History,
		maxKeepTurns:  keep,
		lastTurnStart: -1,
	}
}

// HasProvider 报告是否已配置可用的 LLM provider。
func (a *Agent) HasProvider() bool { return a.provider != nil }

// SetProvider 切换 provider 与模型（TUI 内配置后调用）。
func (a *Agent) SetProvider(p llm.Provider, model string) {
	a.provider = p
	a.model = model
}

// Mode 返回当前模式。
func (a *Agent) Mode() Mode { return a.mode }

// SetMode 切换模式。
func (a *Agent) SetMode(m Mode) { a.mode = m }

// SetApprove 设置普通访问模式下的写操作审批回调（webui 每轮聊天时注入）。
func (a *Agent) SetApprove(fn func(callID, name string, args map[string]any) bool) { a.approve = fn }

// SetAsk 设置 ask 工具的提问回调（webui 每轮聊天时注入）。
func (a *Agent) SetAsk(fn func(callID, question string, options []string) string) { a.question = fn }

// Model 返回当前模型名。
func (a *Agent) Model() string { return a.model }

// SetModel 切换模型。
func (a *Agent) SetModel(m string) { a.model = m }

// History 返回消息历史（若干轮次混在一起）。
func (a *Agent) History() []llm.Message { return a.history }

// ClearHistory 清除历史。
func (a *Agent) ClearHistory() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = nil
	a.lastTurnStart = -1
}

// SetHistory 替换消息历史（TUI 恢复持久化任务用）。
func (a *Agent) SetHistory(ms []llm.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = ms
	a.lastTurnStart = -1
}

// BuildSystemPrompt 组装系统提示词。
func (a *Agent) BuildSystemPrompt() string {
	modeDesc := "完全访问：可以直接修改文件、执行命令并完成实际编码任务"
	if a.mode == ModePlan {
		modeDesc = "普通访问：可以读取和分析文件；任何写文件、执行命令的操作前都会先征得用户确认，被拒绝时请改用其他方案"
	}
	p := fmt.Sprintf(`你是 omni，一个运行在终端里的 AI 编码智能体。
当前工作目录: %s
Agent 模式: %s
当前使用的模型: %s（用户问到「你是什么/用哪个模型」时，如实回答这条，不要隐瞒或说不清楚）

使用规则：
1. 优先用 grep/glob 快速定位，再用 read 读取相关文件，再决定怎么改。
2. 修改代码时使用 edit 做精确替换；大范围改写用 write。
3. 工具输出可能被截断；重要信息可分段读取。
4. 与用户对话默认使用简体中文；代码注释与标识符遵循项目惯例。
5. 每轮调用工具后，必须根据工具结果继续推进，直到任务完成或信息足够。
6. 不要编造文件内容；以工具返回的真实内容为准。
7. 任务包含 3 个以上步骤（调研+修改+验证等）时，先用 todo 工具建立任务清单，每完成一步都提交整份清单刷新进度，让用户实时看到处理到哪一步；一步都没有再开始下一步前，把上一步标 completed、当前步标 in_progress。
8. 当歧义会实质影响结果、无法合理假设时，用 ask 工具向用户提一个简短问题（可带候选项），拿到回答再继续；不要连环追问，一次只问一个。
9. bash工具的 shell 是 bash（Windows 上是 Git Bash），不是 cmd。丢弃输出必须写 '>/dev/null 2>&1'，绝对不要写 cmd 风格的 '> nul'——bash 会把它当成普通文件名，在工作目录里凭空造出一个名为 nul 的垃圾文件。同理不要创建名为 nul 的任何文件。
10. 调试日志、临时产物、脚本输出统一写到工作目录下的 .omni/log/（如 .omni/log/debug.log），不要散落在项目根目录或源码目录里；该目录已在 .gitignore 中。给用户的结论看工具返回即可，不要把完整日志贴进回复。`, a.cwd, modeDesc, a.model)
	if a.extra != "" {
		p += "\n\n额外要求（来自配置）:\n" + a.extra
	}
	if a.extraPrompt != nil {
		if dyn := a.extraPrompt(); dyn != "" {
			p += dyn
		}
	}
	return p
}

// toolDefs 按模式返回工具定义。
// 普通访问模式（ModePlan）下若配置了审批回调，仍下发全部工具，
// 由 Execute 在执行前逐条询问；否则退化为只读工具。
func (a *Agent) toolDefs() []llm.ToolDef {
	if a.mode == ModePlan && a.approve == nil {
		return a.tools.ReadOnlyDefinitions()
	}
	return a.tools.Definitions()
}

// Run 执行一轮问答（追加 user 消息后调用 Execute）。emit 回调生命周期内可被并发调用，UI 侧需同步处理。
func (a *Agent) Run(ctx context.Context, userInput string, emit func(Event)) error {
	rollback := a.Prepare(userInput)
	if err := a.Execute(ctx, emit); err != nil {
		rollback()
		return err
	}
	return nil
}

// Prepare 记录本轮起点并追加 user 消息，返回可回滚当前轮的函数。
func (a *Agent) Prepare(userInput string) func() {
	a.mu.Lock()
	defer a.mu.Unlock()
	start := len(a.history)
	a.lastTurnStart = start
	a.history = append(a.history, llm.NewTextMessage(llm.RoleUser, userInput))
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if len(a.history) >= start {
			a.history = a.history[:start]
		}
	}
}

// RollbackLastTurn 回滚到上一轮开始之前，用于"重新生成"。
func (a *Agent) RollbackLastTurn() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastTurnStart >= 0 {
		a.history = a.history[:a.lastTurnStart]
		a.lastTurnStart = -1
	}
}

// SetLastTurnStart 由 Run 在整轮提交后调用，以便后续 regen 可回滚完整一轮。
func (a *Agent) SetLastTurnStart() {
	a.lastTurnStart = len(a.history)
}

// defaultHistoryTurns 发送给模型的上下文里保留的最近用户提问轮数。
// 对话历史无限增长会让每次请求体积不断变大，超过模型上下文窗口后上游常返回空响应；
// 这里做滑动窗口：只把最近几轮发给模型，完整历史仍持久化（UI 展示与重启恢复不受影响）。
const defaultHistoryTurns = 20

// contextWindow 截取 history 中最近 keepTurns 个用户提问轮次。
// 从某个 user 消息处开始切——新一轮自含（先 user，再同轮的 assistant/tool），
// 切在这里不会截断跨轮的 tool 引用；用户轮不足时整段返回。
func contextWindow(history []llm.Message, keepTurns int) []llm.Message {
	if keepTurns <= 0 || len(history) <= 1 {
		return history
	}
	count := 0
	start := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleUser {
			count++
			if count == keepTurns {
				start = i
				break
			}
		}
	}
	return history[start:]
}

// elapsedMs 返回整轮工作耗时（毫秒）。仅最终答复（无工具调用）记录；
// 中间的工具轮返回 0，配合 json:"omitempty" 不落盘。
func elapsedMs(tcs []llm.ToolCall, start time.Time) int64 {
	if len(tcs) != 0 {
		return 0
	}
	return time.Since(start).Milliseconds()
}

// Execute 在已 Prepare 的上下文上运行迭代循环。
// 整轮执行期间持有 mu：TUI 与 webui 的并发请求将在此排队。
func (a *Agent) Execute(ctx context.Context, emit func(Event)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if emit == nil {
		emit = func(Event) {}
	}

	// 整轮累计令牌用量（多轮 = 用户一次提问触发的多次 LLM 调用）
	var totalUsage llm.Usage
	// 本次提问产出的全部多媒体：回答结束时统一落盘并集中发给 UI 展示
	var runMedia []llm.Attachment
	// 本提问开始处理的时刻：结束时写出最终回复的工作耗时（毫秒）供持久化展示
	turnStart := time.Now()

	for i := 0; i < a.maxIter; i++ {
		round := i // 本轮序号，供 UI 分组
		var reasoning, effort string
		if a.reasoning != nil {
			reasoning, effort = a.reasoning(a.model)
		}
		req := llm.ChatRequest{
			Model:           a.model,
			System:          a.BuildSystemPrompt(),
			Messages:        contextWindow(a.history, a.maxKeepTurns),
			Tools:           a.toolDefs(),
			MaxTokens:       a.maxToken,
			Reasoning:       reasoning,
			ReasoningEffort: effort,
		}
		resp, err := a.provider.Chat(ctx, req, func(ev llm.StreamEvent) error {
			switch ev.Type {
			case llm.EventText:
				emit(Event{Type: EventText, Text: ev.Text, Round: round})
			case llm.EventReasoning:
				// 思考/推理内容：只透传给 UI 展示，不进历史
				emit(Event{Type: EventReasoning, Text: ev.Text, Round: round})
			case llm.EventRetry:
				// 上游瞬时故障正在自动重试：透传重试进度，不进思考流
				emit(Event{Type: EventRetry, Retry: ev.Retry, Round: round})
			case llm.EventError:
				emit(Event{Type: EventError, Err: ev.Err, Round: round})
			}
			return nil
		})
		if err != nil {
			emit(Event{Type: EventError, Err: err, Round: round})
			return err
		}
		if resp.Usage != nil {
			totalUsage.InputTokens += resp.Usage.InputTokens
			totalUsage.OutputTokens += resp.Usage.OutputTokens
		}

		// 空最终回复（上游 200 却连文本/工具都没有，坏模型名/网关假完成，
		// openai 通道空响应重试耗尽后仍空）：按错误收场，报错带上模型名让用户
		// 知道该换哪个；且空 assistant 消息绝不进历史——否则它一直留在上下文里，
		// 后续每轮请求都带着这条空回复，换回正常模型也会跟着连续空响应，
		// 只有新建任务（干净历史）才能恢复。
		if len(resp.ToolCalls) == 0 && resp.Content == "" {
			err := fmt.Errorf("模型 %s 没有返回任何内容（上游持续空响应），请重试或换个模型", a.model)
			emit(Event{Type: EventError, Err: err, Round: round})
			return err
		}
		msg := llm.Message{
			Role:      llm.RoleAssistant,
			Text:      resp.Content,
			ToolCalls: resp.ToolCalls,
			// 最终答复（无工具调用）记录整轮工作耗时，持久化后重启仍可显示「已工作」
			ElapsedMs: elapsedMs(resp.ToolCalls, turnStart),
		}
		if len(resp.ToolCalls) == 0 {
			// 最终答复随耗时一起记录模型与输出 token：回复末行显示「模型名 · Token速度 · 耗时」
			msg.ReplyModel = a.model
			msg.OutputTokens = totalUsage.OutputTokens
		}
		a.history = append(a.history, msg)

		if len(resp.ToolCalls) == 0 {
			// 回答结束：把本次提问所有媒体附件统一落盘（内联的写到 .omni/media/），
			// 并作为一条 media 事件集中发给 UI——展示在最终回答里，而不是埋在工具卡片里。
			if len(runMedia) > 0 {
				finalizeMedia(a.cwd, runMedia)
				emit(Event{Type: EventMedia, Attachments: runMedia, Round: round})
			}
			u := totalUsage
			emit(Event{Type: EventDone, Model: a.model, Round: round, Usage: &u})
			return nil
		}

		for _, tc := range resp.ToolCalls {
			if err := ctx.Err(); err != nil {
				return err
			}
			// ask 工具：直接向用户提问并阻塞等回答（回调由 UI 注入）
			if tc.Name == "ask" && a.question != nil {
				q, _ := tc.Args["question"].(string)
				var opts []string
				if raw, ok := tc.Args["options"].([]any); ok {
					for _, v := range raw {
						if s, ok := v.(string); ok && s != "" {
							opts = append(opts, s)
						}
					}
				}
				emit(Event{Type: EventToolStart, Tool: &tc, Round: round})
				emit(Event{Type: EventQuestion, Tool: &tc, Round: round})
				ans := a.question(tc.ID, q, opts)
				if ans == "" {
					ans = "（用户未回答，跳过该问题）"
				}
				emit(Event{Type: EventToolOutput, Tool: &tc, Output: ans, Round: round})
				emit(Event{Type: EventToolEnd, Tool: &tc, Round: round})
				a.history = append(a.history, llm.Message{
					Role:       llm.RoleTool,
					ToolResult: &llm.ToolResult{CallID: tc.ID, Content: ans},
				})
				continue
			}
			// 普通访问模式：先推卡片再问用户，UI 可在原位显示「允许/拒绝」
			if a.mode == ModePlan && a.approve != nil && !a.tools.IsReadOnly(tc.Name) {
				emit(Event{Type: EventToolStart, Tool: &tc, Round: round})
				emit(Event{Type: EventApproval, Tool: &tc, Round: round})
				if !a.approve(tc.ID, tc.Name, tc.Args) {
					msg := "用户拒绝了该操作，未执行。请换一种方案或先向用户说明。"
					emit(Event{Type: EventToolOutput, Tool: &tc, Output: msg, IsError: true, Round: round})
					emit(Event{Type: EventToolEnd, Tool: &tc, IsError: true, Round: round})
					a.history = append(a.history, llm.Message{
						Role:       llm.RoleTool,
						ToolResult: &llm.ToolResult{CallID: tc.ID, Content: msg, IsError: true},
					})
					continue
				}
			} else {
				emit(Event{Type: EventToolStart, Tool: &tc, Round: round})
			}
			// 无审批回调的普通访问模式退化为强制只读
			forceRO := a.mode == ModePlan && a.approve == nil
			out, media, terr := a.tools.ExecuteWithMedia(ctx, tc.Name, tc.Args, forceRO)
			if terr != nil {
				emit(Event{Type: EventToolOutput, Tool: &tc, Output: "错误: " + terr.Error(), IsError: true, Round: round})
				emit(Event{Type: EventToolEnd, Tool: &tc, IsError: true, Round: round})
				a.history = append(a.history, llm.Message{
					Role:       llm.RoleTool,
					ToolResult: &llm.ToolResult{CallID: tc.ID, Content: "错误: " + terr.Error(), IsError: true},
				})
				continue
			}
			// 生成类工具产出了媒体（图/视频/音频）：提醒模型这些媒体会随最终回答
			// 集中展示给用户，正文不要再贴一遍 markdown/链接，避免重复显示。
			if len(media) > 0 {
				out += "\n\n[系统] 以上媒体已生成，将随你的最终回答集中展示给用户；回复正文里不要再插入这些图片/视频/音频的 markdown 或链接。"
			}
			emit(Event{Type: EventToolOutput, Tool: &tc, Output: out, Attachments: media, Round: round})
			emit(Event{Type: EventToolEnd, Tool: &tc, Round: round})
			runMedia = append(runMedia, media...)
			a.history = append(a.history, llm.Message{
				Role: llm.RoleTool,
				ToolResult: &llm.ToolResult{
					CallID: tc.ID, Content: out, Attachments: media,
				},
				// 只有标记了 ForModel 的图片（截图/读图，供视觉分析）才进模型上下文；
				// 生成类工具（生图/生视频/TTS）的产出只给 UI 展示，省 token。
				// provider 序列化时会把它们展开成紧随其后的 user 消息（tool 角色不能带图）。
				Images: imagesForModel(media),
			})
		}
	}
	return fmt.Errorf("达到最大迭代次数 %d，任务可能未完成", a.maxIter)
}

// finalizeMedia 把本次产出的媒体统一落到 <cwd>/.omni/download/：
//   - 内联 base64（Data）解码写文件；
//   - 远程 URL（如生视频返回的 mp4 地址）下载到本地；
//   - 已有本地 Path 的（截图）不动。
//
// 下载/写盘成功后都回填 Path，前端展示直接用本地文件。
// WAV 音频顺手修正头部长度字段——AI 生成的 WAV 头长度常为未知值，不改浏览器算不出时长。
func finalizeMedia(cwd string, atts []llm.Attachment) {
	if strings.TrimSpace(cwd) == "" {
		return
	}
	dir := filepath.Join(cwd, ".omni", "download")
	n := 0
	mkdir := func() bool {
		if n == 0 {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return false
			}
		}
		n++
		return true
	}
	for i := range atts {
		a := &atts[i]
		switch {
		case a.Path != "" || a.Data == "" && a.URL == "":
			// 已在本地，或既无数据也无地址
		case a.Data != "":
			if !mkdir() {
				return
			}
			ext := extForMedia(*a)
			fp := filepath.Join(dir, fmt.Sprintf("media-%d-%d%s", time.Now().Unix(), n, ext))
			if data, err := base64.StdEncoding.DecodeString(a.Data); err == nil {
				if strings.EqualFold(ext, ".wav") {
					patchWavHeader(data)
				}
				if err := os.WriteFile(fp, data, 0o644); err == nil {
					a.Path = fp
				}
			}
		case isHTTP(a.URL):
			if !mkdir() {
				return
			}
			ext := extForMedia(*a)
			fp := filepath.Join(dir, fmt.Sprintf("media-%d-%d%s", time.Now().Unix(), n, ext))
			if data, err := downloadFile(a.URL); err == nil {
				if strings.EqualFold(ext, ".wav") {
					patchWavHeader(data)
				}
				if err := os.WriteFile(fp, data, 0o644); err == nil {
					a.Path = fp
				}
			}
		}
	}
}

func isHTTP(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// downloadFile 下载远程媒体（上限 64MB）。
func downloadFile(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// patchWavHeader 原地修正 WAV 的 RIFF/data 块长度字段为真实大小。
// TTS 服务常按流式写 WAV（长度字段置 0 或 0xFFFFFFFF），浏览器因此读不出时长。
func patchWavHeader(data []byte) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return
	}
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	i := 12
	for i+8 <= len(data) {
		id := string(data[i : i+4])
		sz := binary.LittleEndian.Uint32(data[i+4 : i+8])
		if id == "data" {
			if int64(sz) != int64(len(data)-i-8) {
				binary.LittleEndian.PutUint32(data[i+4:i+8], uint32(len(data)-i-8))
			}
			return
		}
		i += 8 + int(sz)
		if sz%2 == 1 { // RIFF 块按 2 字节对齐
			i++
		}
	}
}

// extForMedia 按附件类型与 MIME 推断文件扩展名。
func extForMedia(a llm.Attachment) string {
	m := strings.ToLower(a.MediaType)
	switch {
	case strings.HasSuffix(m, "png"):
		return ".png"
	case strings.HasSuffix(m, "jpeg"), strings.HasSuffix(m, "jpg"):
		return ".jpg"
	case strings.HasSuffix(m, "webp"):
		return ".webp"
	case strings.HasSuffix(m, "gif"):
		return ".gif"
	case strings.HasSuffix(m, "wav"):
		return ".wav"
	case strings.HasSuffix(m, "mpeg"), strings.HasSuffix(m, "mp3"):
		return ".mp3"
	case strings.HasSuffix(m, "ogg"):
		return ".ogg"
	case strings.HasSuffix(m, "webm"):
		return ".webm"
	case a.Kind == llm.KindVideo:
		return ".mp4"
	case a.Kind == llm.KindAudio:
		return ".wav"
	default:
		return ".bin"
	}
}

// imagesForModel 从附件里挑出标记了 ForModel 的图片（截图/读图），转成模型可消费的
// llm.Image。生成类工具的产出（未标记）不进上下文，省 token。
// 落盘的图片在这里按需读文件；读失败就跳过（不影响工具本身已成功的事实）。
func imagesForModel(media []llm.Attachment) []llm.Image {
	var out []llm.Image
	for _, a := range media {
		if !a.IsImage() || !a.ForModel {
			continue
		}
		im := llm.Image{MediaType: a.MediaType, Data: a.Data}
		if im.Data == "" && a.Path != "" {
			b, err := os.ReadFile(a.Path)
			if err != nil {
				continue
			}
			im.Data = base64.StdEncoding.EncodeToString(b)
		}
		if im.Data == "" {
			continue
		}
		if im.MediaType == "" {
			im.MediaType = "image/png"
		}
		if im.ByteSize() > maxToolImageBytes {
			continue
		}
		out = append(out, im)
	}
	return out
}

// maxToolImageBytes 单张图片送进模型的上限（原始字节），webui 附件端点限流用。
const maxToolImageBytes = 4 << 20

// MaxToolImageBytes 暴露给其他包（webui 附件端点限流用）。
func MaxToolImageBytes() int { return maxToolImageBytes }
