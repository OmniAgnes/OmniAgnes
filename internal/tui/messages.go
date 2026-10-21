package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"

	"omni/internal/agent"
)

const (
	roleUser      = "user"
	roleAssistant = "assistant"
	roleSystem    = "system"
	roleStats     = "stats" // 本轮回复完成统计行（模型/tok/s/耗时）
)

// toolView 是单次工具调用的渲染状态。
type toolView struct {
	id        int
	name      string
	args      string
	running   bool
	done      bool
	isError   bool
	collapsed bool
	output    string
}

// viewMsg 是聊天流中一条可渲染消息。
type viewMsg struct {
	role      string
	text      string
	reasoning string // 思考/推理流（不进历史，仅供展示）
	isError   bool
	done      bool // assistant 消息整轮结束后置 true，届时用 markdown 渲染
	tools     []*toolView
}

// mdRenderer 为包级 markdown 渲染器：assistant 消息整轮结束后渲染条目样式。
// Update 单线程串行调用，无并发问题；宽度变化时重建。
var (
	mdRenderer *glamour.TermRenderer
	mdWidth    int
)

// renderMarkdown 将 markdown 文本渲染为带样式的终端文本；
// 渲染失败时退回原文。
func renderMarkdown(text string, width int) string {
	w := width - 6
	if w < 24 {
		w = 24
	}
	if mdRenderer == nil || mdWidth != w {
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(styles.DarkStyleConfig),
			glamour.WithWordWrap(w),
		)
		if err != nil {
			return text
		}
		mdRenderer = r
		mdWidth = w
	}
	out, err := mdRenderer.Render(text)
	if err != nil {
		return text
	}
	return strings.Trim(out, "\n")
}

func (m *Model) addUserMsg(text string) {
	m.msgs = append(m.msgs, &viewMsg{role: roleUser, text: text})
	m.setViewportContent()
}

func (m *Model) addSystemMsg(text string) {
	m.msgs = append(m.msgs, &viewMsg{role: roleSystem, text: text})
	m.setViewportContent()
}

// addStats 追加本轮回复的完成统计行：模型名 · tok/s · 耗时，
// token 用量齐全时一并附上。数据来自 EventDone 的 Model 与累计 Usage。
func (m *Model) addStats(ev agent.Event) {
	model := ev.Model
	if model == "" {
		model = m.a.Model()
	}
	elapsed := time.Since(m.turnStart)
	var b strings.Builder
	fmt.Fprintf(&b, "✔ %s", model)
	if u := ev.Usage; u != nil && u.OutputTokens > 0 && elapsed.Seconds() > 0.05 {
		fmt.Fprintf(&b, " · %.1f tok/s", float64(u.OutputTokens)/elapsed.Seconds())
	}
	if elapsed > 0 {
		fmt.Fprintf(&b, " · %s", fmtDur(elapsed))
	}
	if u := ev.Usage; u != nil && (u.InputTokens > 0 || u.OutputTokens > 0) {
		fmt.Fprintf(&b, " · 输出 %s / 输入 %s tokens", fmtTokens(u.OutputTokens), fmtTokens(u.InputTokens))
	}
	m.msgs = append(m.msgs, &viewMsg{role: roleStats, text: b.String()})
}

// fmtDur 耗时显示：一分钟内一位小数秒，跨分钟合并成分秒。
func fmtDur(d time.Duration) string {
	sec := d.Seconds()
	if sec < 60 {
		return fmt.Sprintf("%.1f 秒", sec)
	}
	return fmt.Sprintf("%d 分 %d 秒", int(sec)/60, int(sec)%60)
}

// fmtTokens 令牌数缩写：千以下原文，千到十万一位小数 k。
func fmtTokens(n int) string {
	if n < 0 {
		return "?"
	}
	if n < 1000 {
		return strconv.Itoa(n)
	}
	if n < 100000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return strconv.Itoa(n/1000) + "k"
}

// renderWelcome 空会话的欢迎屏：版本、一句话简介、模型与目录。
func (m *Model) renderWelcome() string {
	modelName := m.a.Model()
	if modelName == "" {
		modelName = "未配置（用 /config 添加）"
	}
	var modeDesc string
	switch m.a.Mode() {
	case agent.ModeBuild:
		modeDesc = "build（可写文件、执行命令）"
	case agent.ModePlan:
		modeDesc = "plan（只读）"
	default:
		modeDesc = string(m.a.Mode())
	}
	title := "✻ 欢迎使用 omni"
	if m.version != "" {
		title += " v" + m.version
	}
	lines := []string{
		styleAssistant.Bold(true).Render(title),
		"",
		styleSysMsg.Render(wrapText("  模型  "+modelName+" · "+modeDesc, m.viewport.Width)),
		styleSysMsg.Render(wrapText("  cwd   "+m.cwd, m.viewport.Width)),
	}
	return strings.Join(lines, "\n")
}

// currentAssistant 返回当前正在累积文本的 assistant 消息；无则新建。
func (m *Model) currentAssistant() *viewMsg {
	if n := len(m.msgs); n > 0 {
		last := m.msgs[n-1]
		if last.role == roleAssistant {
			return last
		}
	}
	msg := &viewMsg{role: roleAssistant}
	m.msgs = append(m.msgs, msg)
	return msg
}

// renderMessages 将消息渲染为 viewport 文本。
func (m *Model) renderMessages(width int) string {
	if len(m.msgs) == 0 {
		return m.renderWelcome()
	}
	var sb strings.Builder
	for i, msg := range m.msgs {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(m.renderMsg(msg, width))
	}
	return sb.String()
}

func (m *Model) renderMsg(msg *viewMsg, width int) string {
	switch msg.role {
	case roleUser:
		content := styleUserMsg.Render("✦ You") + "\n"
		content += styleBase.Render(wrapText(msg.text, width))
		return content
	case roleAssistant:
		var sb strings.Builder
		// 思考流：灰色展示在正文上方；正文未出时显示「思考中…」
		if msg.reasoning != "" {
			head := "✻ 思考中…"
			if msg.text != "" {
				head = "✻ 思考"
			}
			sb.WriteString(styleSysMsg.Render(wrapText(head+"\n"+msg.reasoning, width)))
		}
		if msg.text != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			// 整轮结束后 markdown 渲染（标题/列表/代码块），流式期间保持纯文本（逐 token 重渲会卡）
			if msg.done {
				sb.WriteString(renderMarkdown(msg.text, width))
			} else {
				sb.WriteString(styleAssistant.Render(wrapText(msg.text, width)))
			}
		}
		for _, t := range msg.tools {
			sb.WriteString("\n")
			sb.WriteString(m.renderTool(t, width))
		}
		return sb.String()
	case roleSystem:
		return styleSysMsg.Render(wrapText(msg.text, width))
	case roleStats:
		return styleStats.Render(" " + msg.text)
	}
	return ""
}

func (m *Model) renderTool(t *toolView, width int) string {
	var sb strings.Builder
	name := t.name
	if name == "" {
		name = "tool"
	}
	if t.running {
		sb.WriteString(styleToolLine.Render("▸ " + name + " " + spinnerChar + " 运行中…"))
	} else if t.done && t.isError {
		sb.WriteString(styleError.Render("✘ " + name + " " + shortArgs(t.args)))
	} else if t.done {
		sb.WriteString(styleToolLine.Render("✔ " + name + " " + shortArgs(t.args)))
	} else {
		sb.WriteString(styleToolLine.Render("▸ " + name + " " + shortArgs(t.args)))
	}
	if t.done && !t.collapsed && t.output != "" {
		sb.WriteString("\n")
		body := t.output
		if len(body) > 1200 {
			body = body[:1200] + "\n...（长输出折叠，按 Ctrl+o 展开/收起）"
		}
		// 必须按终端宽度折行：长行会被终端硬折行撑破 frame，
		// alt-screen 下 spinner 高频重绘会把状态行顶成刷屏
		sb.WriteString(styleToolOutput.Render(prefixLines(wrapText(body, width), "│ ")))
	}
	return sb.String()
}

const spinnerChar = "⏳"

func shortArgs(args string) string {
	if len(args) > 80 {
		return args[:80] + "…"
	}
	return args
}

func prefixLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func wrapText(s string, width int) string {
	if width <= 0 || len(s) == 0 {
		return s
	}
	return lipgloss.NewStyle().Width(width - 4).Render(s)
}

// toggleAllTools 展开/折叠所有已完成的工具。
func (m *Model) toggleAllTools() {
	var done []*toolView
	for _, msg := range m.msgs {
		for _, t := range msg.tools {
			if t.done {
				done = append(done, t)
			}
		}
	}
	if len(done) == 0 {
		return
	}
	allCollapsed := true
	for _, t := range done {
		if !t.collapsed {
			allCollapsed = false
			break
		}
	}
	if allCollapsed {
		for _, t := range done {
			t.collapsed = false
		}
	} else {
		for _, t := range done {
			t.collapsed = true
		}
	}
}
