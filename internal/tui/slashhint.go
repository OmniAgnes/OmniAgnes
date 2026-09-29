package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type slashCmd struct {
	name string
	desc string
}

var slashCommands = []slashCmd{
	{"/help", "显示帮助"},
	{"/new", "开启新会话（清空历史）"},
	{"/config", "配置模型（类型/模型/API Key）"},
	{"/model", "切换模型：/model <名称>"},
	{"/mode", "切换模式：/mode build|plan"},
	{"/webui", "在浏览器打开网页版聊天"},
	{"/quit", "退出"},
}

func filterSlashCommands(prefix string) []slashCmd {
	var out []slashCmd
	for _, c := range slashCommands {
		if strings.HasPrefix(c.name, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// slashMatches 返回当前输入匹配的命令；不需要提示时返回 nil。
func (m *Model) slashMatches() []slashCmd {
	if m.running || m.hintDismissed || m.configOpen {
		return nil
	}
	v := strings.TrimSpace(m.textarea.Value())
	if !strings.HasPrefix(v, "/") || strings.Contains(v, " ") {
		return nil
	}
	matches := filterSlashCommands(v)
	if len(matches) == 0 {
		return nil
	}
	if m.hintIdx >= len(matches) {
		m.hintIdx = 0
	}
	return matches
}

// updateSlashHint 依据当前输入刷新提示框占位行数并重排布局。
func (m *Model) updateSlashHint() {
	h := 0
	if out := m.renderSlashHint(); out != "" {
		h = lipgloss.Height(out)
	}
	if h != m.hintLines {
		m.hintLines = h
		m.layoutViewport()
	}
}

// layoutViewport 按提示框高度计算消息区可用行数。
func (m *Model) layoutViewport() {
	h := m.height - 2 - inputHeight - m.hintLines
	if h < 3 {
		h = 3
	}
	m.viewport.Height = h
}

// renderSlashHint 渲染 slash 命令提示框；无匹配时返回空串。
func (m *Model) renderSlashHint() string {
	matches := m.slashMatches()
	if matches == nil {
		return ""
	}
	rows := []string{styleSysMsg.Render(" 命令 · ↑↓ 选择 · Tab/Enter 补全 · Esc 关闭")}
	for i, c := range matches {
		label := c.name
		if pad := 9 - len(c.name); pad > 0 {
			label += strings.Repeat(" ", pad)
		}
		label += c.desc
		if i == m.hintIdx {
			rows = append(rows, styleHintSel.Render(" "+label+" "))
		} else {
			rows = append(rows, styleBase.Render(" "+label+" "))
		}
	}
	return styleInputBox.Width(m.width - 6).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}
