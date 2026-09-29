package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type slashCmd struct {
	name string
	desc string
}

// builtinSlashCommands 是内置命令；技能命令在运行时追加。
var builtinSlashCommands = []slashCmd{
	{"/help", "显示帮助"},
	{"/new", "开启新会话（清空历史）"},
	{"/config", "配置模型（类型/模型/API Key）"},
	{"/model", "切换模型：/model <名称>"},
	{"/mode", "切换模式：/mode build|plan"},
	{"/skills", "列出并重新加载技能"},
	{"/memory", "查看全局记忆"},
	{"/mcp", "查看 MCP 服务器状态"},
	{"/webui", "在浏览器打开网页版聊天"},
	{"/quit", "退出"},
}

// slashCommands 返回内置命令 + 技能命令（/技能名）。
func (m *Model) slashCommands() []slashCmd {
	out := make([]slashCmd, 0, len(builtinSlashCommands)+len(m.skillCmds))
	out = append(out, builtinSlashCommands...)
	for _, s := range m.skillCmds {
		out = append(out, slashCmd{"/" + s.Name, s.Description})
	}
	return out
}

// reloadSkills 重新扫描技能目录，刷新 slash 提示。
func (m *Model) reloadSkills() {
	m.skillCmds = nil
	if m.extras == nil || m.extras.Skills == nil {
		return
	}
	m.skillCmds = m.extras.Skills.For(m.cwd)
}

func filterSlashCommands(all []slashCmd, prefix string) []slashCmd {
	var out []slashCmd
	for _, c := range all {
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
	matches := filterSlashCommands(m.slashCommands(), v)
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
