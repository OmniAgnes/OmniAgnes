package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type slashCmd struct {
	name string
	desc string
}

// builtinSlashCommands 是内置命令；技能命令（/技能名）不在提示列表里——
// 技能数量多时会淹没内置命令，且技能触发走 handleCommand 的兜底分支即可。
var builtinSlashCommands = []slashCmd{
	{"/help", "显示帮助"},
	{"/new", "开启新会话（原会话保留在任务列表）"},
	{"/tasks", "列出任务（与桌面版/网页版同步）"},
	{"/open", "打开任务继续对话：/open <编号>"},
	{"/config", "配置模型（类型/模型/API Key）"},
	{"/model", "列出并切换模型"},
	{"/mode", "切换模式：/mode build|plan"},
	{"/skills", "列出并重新加载技能"},
	{"/memory", "查看全局记忆"},
	{"/mcp", "查看 MCP 服务器状态"},
	{"/webui", "在浏览器打开网页版聊天"},
	{"/quit", "退出"},
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
	if m.running || m.hintDismissed || m.configOpen || m.modelPickerOpen {
		return nil
	}
	v := strings.TrimSpace(m.textarea.Value())
	if !strings.HasPrefix(v, "/") || strings.Contains(v, " ") {
		return nil
	}
	matches := filterSlashCommands(builtinSlashCommands, v)
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

// layoutViewport 按底部区块（生成中的状态行 / 提示面板 / 输入区）计算消息区可用行数。
func (m *Model) layoutViewport() {
	extra := m.hintLines
	if m.running {
		extra++ // 生成中的 spinner 状态行
	}
	h := m.height - 1 - inputLines - extra
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
	// 描述与命令至少隔 3 格：列宽取匹配项中最长命令名 + 间距
	col := 9
	for _, c := range matches {
		if w := lipgloss.Width(c.name) + 3; w > col {
			col = w
		}
	}
	for i, c := range matches {
		label := c.name
		if pad := col - lipgloss.Width(c.name); pad > 0 {
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
