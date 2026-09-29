package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	roleUser      = "user"
	roleAssistant = "assistant"
	roleSystem    = "system"
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
	role    string
	text    string
	isError bool
	tools   []*toolView
}

func (m *Model) addUserMsg(text string) {
	m.msgs = append(m.msgs, &viewMsg{role: roleUser, text: text})
	m.setViewportContent()
}

func (m *Model) addSystemMsg(text string) {
	m.msgs = append(m.msgs, &viewMsg{role: roleSystem, text: text})
	m.setViewportContent()
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
		return styleSysMsg.Render(" 输入消息开始对话。输入 /help 查看帮助。")
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
		if msg.text != "" {
			sb.WriteString(styleAssistant.Render(wrapText(msg.text, width)))
		}
		for _, t := range msg.tools {
			sb.WriteString("\n")
			sb.WriteString(m.renderTool(t, width))
		}
		return sb.String()
	case roleSystem:
		return styleSysMsg.Render(msg.text)
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
		sb.WriteString(styleToolOutput.Render(prefixLines(body, "│ ")))
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
