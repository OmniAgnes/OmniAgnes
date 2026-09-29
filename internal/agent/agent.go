package agent

import (
	"context"
	"fmt"
	"sync"

	"omni/internal/llm"
	"omni/internal/tools"
)

// Mode 是 Agent 运行模式。
type Mode string

const (
	ModeBuild Mode = "build"
	ModePlan  Mode = "plan"
)

// EventType 是 Agent 向 UI 发出的事件类型。
type EventType string

const (
	EventText       EventType = "text"       // 助手文本增量
	EventToolStart  EventType = "tool_start" // 工具开始执行
	EventToolOutput EventType = "tool_output"
	EventToolEnd    EventType = "tool_end"
	EventError      EventType = "error"
	EventDone       EventType = "done" // 本轮问答结束
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
}

// Agent 持有一个 LLM provider、工具注册表与消息历史。
// TUI 与 webui 可能共用同一实例，历史相关操作以 mu 串行化。
type Agent struct {
	mu       sync.Mutex
	provider llm.Provider
	tools    *tools.Registry
	model    string
	mode     Mode
	cwd      string
	maxIter  int
	maxToken int
	extra    string
	extraPrompt func() string
	history  []llm.Message
	lastTurnStart int
}

// New 创建 Agent。
func New(provider llm.Provider, reg *tools.Registry, opts Options) *Agent {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 32
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 8192
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
		history:       opts.History,
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
}

// BuildSystemPrompt 组装系统提示词。
func (a *Agent) BuildSystemPrompt() string {
	modeDesc := "build：可以修改文件、执行命令并完成实际编码任务"
	if a.mode == ModePlan {
		modeDesc = "plan：只读模式，仅允许搜索和读取文件，禁止修改文件或执行非只读命令"
	}
	p := fmt.Sprintf(`你是 omni，一个运行在终端里的 AI 编码智能体，风格类似 opencode。
当前工作目录: %s
Agent 模式: %s

使用规则：
1. 优先用 grep/glob 快速定位，再用 read 读取相关文件，再决定怎么改。
2. 修改代码时使用 edit 做精确替换；大范围改写用 write。
3. 工具输出可能被截断；重要信息可分段读取。
4. 与用户对话默认使用简体中文；代码注释与标识符遵循项目惯例。
5. 每轮调用工具后，必须根据工具结果继续推进，直到任务完成或信息足够。
6. 不要编造文件内容；以工具返回的真实内容为准。`, a.cwd, modeDesc)
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
func (a *Agent) toolDefs() []llm.ToolDef {
	if a.mode == ModePlan {
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

// Execute 在已 Prepare 的上下文上运行迭代循环。
// 整轮执行期间持有 mu：TUI 与 webui 的并发请求将在此排队。
func (a *Agent) Execute(ctx context.Context, emit func(Event)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if emit == nil {
		emit = func(Event) {}
	}

	for i := 0; i < a.maxIter; i++ {
		req := llm.ChatRequest{
			Model:     a.model,
			System:    a.BuildSystemPrompt(),
			Messages:  a.history,
			Tools:     a.toolDefs(),
			MaxTokens: a.maxToken,
		}
		resp, err := a.provider.Chat(ctx, req, func(ev llm.StreamEvent) error {
			switch ev.Type {
			case llm.EventText:
				emit(Event{Type: EventText, Text: ev.Text})
			case llm.EventError:
				emit(Event{Type: EventError, Err: ev.Err})
			}
			return nil
		})
		if err != nil {
			emit(Event{Type: EventError, Err: err})
			return err
		}

		a.history = append(a.history, llm.Message{
			Role:      llm.RoleAssistant,
			Text:      resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		if len(resp.ToolCalls) == 0 {
			emit(Event{Type: EventDone, Model: a.model})
			return nil
		}

		for _, tc := range resp.ToolCalls {
			if err := ctx.Err(); err != nil {
				return err
			}
			emit(Event{Type: EventToolStart, Tool: &tc})
			out, terr := a.tools.Execute(ctx, tc.Name, tc.Args, a.mode == ModePlan)
			if terr != nil {
				emit(Event{Type: EventToolOutput, Tool: &tc, Output: "错误: " + terr.Error(), IsError: true})
				emit(Event{Type: EventToolEnd, Tool: &tc, IsError: true})
				a.history = append(a.history, llm.Message{
					Role:       llm.RoleTool,
					ToolResult: &llm.ToolResult{CallID: tc.ID, Content: "错误: " + terr.Error(), IsError: true},
				})
				continue
			}
			emit(Event{Type: EventToolOutput, Tool: &tc, Output: out})
			emit(Event{Type: EventToolEnd, Tool: &tc})
			a.history = append(a.history, llm.Message{
				Role:       llm.RoleTool,
				ToolResult: &llm.ToolResult{CallID: tc.ID, Content: out},
			})
		}
	}
	return fmt.Errorf("达到最大迭代次数 %d，任务可能未完成", a.maxIter)
}
