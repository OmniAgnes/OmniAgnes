package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omni/internal/agent"
	"omni/internal/llm"
)

const (
	inputHeight = 6
)

// eventMsg 将 agent 事件桥接为 tea 消息。
type eventMsg struct{ ev agent.Event }

// eventsClosed 表示 agent 事件通道已关闭。
type eventsClosed struct{}

// Model 是 TUI 根模型。
type Model struct {
	a *agent.Agent

	ready  bool
	width  int
	height int

	viewport viewport.Model
	textarea textarea.Model
	spinner  spinner.Model

	msgs []*viewMsg

	running  bool
	cancel   context.CancelFunc
	evtCh    chan agent.Event
	rollback func()

	statusText string
	toolID     int

	sessionID string
	startTime time.Time

	// 配置表单（/config 或未配置时自动打开）
	configPath string
	configOpen bool
	cfgInputs  []textinput.Model
	cfgFocus   int
	cfgErr     string

	// slash 命令提示
	hintIdx      int
	hintDismissed bool
	hintLines    int
}

// New 构造 TUI 模型。
func New(a *agent.Agent, sessionID, configPath string) *Model {
	ta := textarea.New()
	ta.Placeholder = " 输入消息，Enter 发送，Alt+Enter 换行；/help 查看帮助"
	// 仅首行显示 ❯，其余行（换行/自动折行）留空对齐
	ta.SetPromptFunc(2, func(line int) string {
		if line == 0 {
			return "❯ "
		}
		return ""
	})
	ta.ShowLineNumbers = false
	ta.CharLimit = 100000
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	ta.SetHeight(3)

	sp := spinner.New()
	sp.Style = lipgloss.NewStyle().Foreground(colorAccent)
	sp.Spinner = spinner.Dot

	vp := viewport.New(0, 0)

	m := &Model{
		a:          a,
		viewport:   vp,
		textarea:   ta,
		spinner:    sp,
		sessionID:  sessionID,
		startTime:  time.Now(),
		configPath: configPath,
		cfgInputs:  setupConfigInputs(),
	}
	if !a.HasProvider() {
		m.openConfig()
	}
	return m
}

// Init 初始化。
func (m *Model) Init() tea.Cmd {
	ta := m.textarea
	ta.Focus()
	m.textarea = ta
	return textarea.Blink
}

func (m *Model) View() string {
	if !m.ready {
		return "正在启动…"
	}
	if m.configOpen {
		return m.renderConfig()
	}
	parts := []string{m.renderHeader(), m.viewport.View(), m.renderStatus()}
	if hint := m.renderSlashHint(); hint != "" {
		parts = append(parts, hint)
	}
	parts = append(parts, m.renderInputBox())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) renderHeader() string {
	modelName := m.a.Model()
	if modelName == "" {
		modelName = "未配置"
	}
	modelBadge := styleModelBadge.Render(" " + modelName + " ")
	modeName := string(m.a.Mode())
	modeBadge := styleModeBadge.Render(" " + modeName + " ")
	left := styleHeader.Render(" omni ") + " " + modelBadge + " " + modeBadge
	right := styleTimestamp.Render(m.sessionID)
	fill := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if fill < 1 {
		fill = 1
	}
	return left + strings.Repeat(" ", fill) + right
}

func (m *Model) renderStatus() string {
	var text string
	if m.running {
		txt := m.statusText
		if txt == "" {
			txt = "思考中…"
		}
		text = m.spinner.View() + " " + txt
	} else {
		text = "Enter 发送 · Esc 取消 · Ctrl+o 收起工具 · Ctrl+r 重新生成 · /help"
	}
	return styleStatusBar.Render(text)
}

func (m *Model) renderInputBox() string {
	taStyle := styleInputBox.Width(m.width - 4)
	return taStyle.Render(m.textarea.View())
}

// Update 处理事件。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.textarea.SetWidth(m.width - 8)
		m.viewport.Width = m.width - 2
		for i := range m.cfgInputs {
			m.cfgInputs[i].Width = m.width - 20
		}
		m.layoutViewport()
		m.setViewportContent()
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case eventMsg:
		m.handleAgentEvent(msg.ev)
		cmds = append(cmds, m.readEvents(), m.spinnerTickIfRunning())
	case eventsClosed:
		m.running = false
		m.cancel = nil
		m.setViewportContent()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.running {
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) spinnerTickIfRunning() tea.Cmd {
	if m.running {
		return m.spinner.Tick
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	if m.configOpen {
		return m.handleConfigKey(msg)
	}
	// slash 提示菜单的导航键
	if matches := m.slashMatches(); matches != nil {
		switch msg.String() {
		case "up":
			m.hintIdx--
			if m.hintIdx < 0 {
				m.hintIdx = len(matches) - 1
			}
			return nil
		case "down":
			m.hintIdx = (m.hintIdx + 1) % len(matches)
			return nil
		case "tab":
			m.textarea.SetValue(matches[m.hintIdx].name + " ")
			m.updateSlashHint()
			return nil
		case "esc":
			m.hintDismissed = true
			m.updateSlashHint()
			return nil
		}
	}
	switch msg.String() {
	case "ctrl+c":
		return m.handleCtrlC()
	case "esc":
		return m.handleEscape()
	case "ctrl+r":
		return m.regenerate()
	case "ctrl+o":
		m.toggleAllTools()
		m.setViewportContent()
	case "ctrl+l":
		m.viewport.GotoBottom()
	case "enter":
		return m.handleEnter()
	default:
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.hintDismissed = false
		m.updateSlashHint()
		return cmd
	}
	return nil
}

func (m *Model) handleCtrlC() tea.Cmd {
	if m.running {
		if m.cancel != nil {
			m.cancel()
		}
		m.statusText = "正在取消…"
		return nil
	}
	return tea.Quit
}

func (m *Model) handleEscape() tea.Cmd {
	if m.running && m.cancel != nil {
		m.cancel()
		m.statusText = "已取消本轮生成"
		m.running = false
		return nil
	}
	return nil
}

func (m *Model) handleEnter() tea.Cmd {
	if m.running {
		m.addSystemMsg("请等待当前任务完成，或按 Esc 取消")
		return nil
	}
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return nil
	}
	// 命令未输入完整时，Enter 先补全
	if strings.HasPrefix(text, "/") {
		if matches := m.slashMatches(); matches != nil && text != matches[m.hintIdx].name {
			m.textarea.SetValue(matches[m.hintIdx].name + " ")
			m.updateSlashHint()
			return nil
		}
	}
	m.textarea.Reset()
	m.updateSlashHint()
	if strings.HasPrefix(text, "/") {
		return m.handleCommand(text)
	}
	m.addUserMsg(text)
	return m.startAgent(text)
}

// startAgent 启动一轮 agent 执行。
func (m *Model) startAgent(text string) tea.Cmd {
	if !m.a.HasProvider() {
		m.openConfig()
		m.addSystemMsg("尚未配置模型：请在表单中填写类型与模型，保存后即可对话")
		return nil
	}
	if m.rollback != nil {
		m.rollback()
	}
	m.rollback = m.a.Prepare(text)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.running = true
	m.statusText = ""
	m.evtCh = make(chan agent.Event, 256)

	go func() {
		defer close(m.evtCh)
		err := m.a.Execute(ctx, func(ev agent.Event) {
			select {
			case m.evtCh <- ev:
			case <-ctx.Done():
			}
		})
		if err != nil && ctx.Err() != context.Canceled {
			select {
			case m.evtCh <- agent.Event{Type: agent.EventError, Err: err}:
			case <-ctx.Done():
			}
		}
	}()

	return tea.Batch(m.readEvents(), m.spinner.Tick)
}

func (m *Model) readEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.evtCh
		if !ok {
			return eventsClosed{}
		}
		return eventMsg{ev: ev}
	}
}

func (m *Model) setViewportContent() {
	m.viewport.SetContent(m.renderMessages(m.viewport.Width))
	m.viewport.GotoBottom()
}

// handleAgentEvent 处理 agent 事件。
func (m *Model) handleAgentEvent(ev agent.Event) {
	switch ev.Type {
	case agent.EventText:
		cur := m.currentAssistant()
		cur.text += ev.Text
		m.setViewportContent()
	case agent.EventToolStart:
		cur := m.currentAssistant()
		tv := &toolView{
			id:      m.toolID,
			name:    ev.Tool.Name,
			args:    llm.MarshalToolArgs(ev.Tool.Args),
			running: true,
		}
		m.toolID++
		cur.tools = append(cur.tools, tv)
		m.statusText = "运行 " + ev.Tool.Name
		m.setViewportContent()
	case agent.EventToolOutput:
		if t := m.lastTool(); t != nil {
			t.output = ev.Output
			t.isError = ev.IsError
			t.done = true
			t.running = false
		}
		m.setViewportContent()
	case agent.EventToolEnd:
		if t := m.lastTool(); t != nil {
			t.running = false
			t.done = true
			t.isError = ev.IsError
		}
		if ev.IsError {
			m.statusText = ev.Tool.Name + " 执行失败"
		} else {
			m.statusText = ""
		}
		m.setViewportContent()
	case agent.EventError:
		m.addSystemMsg("error: " + ev.Err.Error())
		m.running = false
		m.cancel = nil
	case agent.EventDone:
		m.running = false
		m.cancel = nil
		m.rollback = nil
		m.statusText = ""
		m.setViewportContent()
	}
}

func (m *Model) lastTool() *toolView {
	for i := len(m.msgs) - 1; i >= 0; i-- {
		msg := m.msgs[i]
		if len(msg.tools) > 0 {
			return msg.tools[len(msg.tools)-1]
		}
	}
	return nil
}

// regenerate 重新生成最后一条回复。
func (m *Model) regenerate() tea.Cmd {
	if m.running {
		m.addSystemMsg("请先取消当前任务（Esc）再重新生成")
		return nil
	}
	text := m.lastUserText()
	if text == "" {
		return nil
	}
	// 回滚 agent 历史中的最后一轮
	m.a.RollbackLastTurn()
	if m.rollback != nil {
		m.rollback()
		m.rollback = nil
	}
	// 截掉 UI 中从最后一条 user 消息起的展示
	for i := len(m.msgs) - 1; i >= 0; i-- {
		if m.msgs[i].role == roleUser {
			m.msgs = m.msgs[:i]
			break
		}
	}
	m.setViewportContent()
	return m.startAgent(text)
}

func (m *Model) lastUserText() string {
	for i := len(m.msgs) - 1; i >= 0; i-- {
		if m.msgs[i].role == roleUser {
			return m.msgs[i].text
		}
	}
	return ""
}
