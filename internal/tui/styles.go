package tui

import "github.com/charmbracelet/lipgloss"

var (
	// 主题色
	colorAccent  = lipgloss.Color("#7C3AED")
	colorAccent2 = lipgloss.Color("#A78BFA")
	colorBg      = lipgloss.Color("#1A1525")
	colorPanel   = lipgloss.Color("#241E33")
	colorBorder  = lipgloss.Color("#3B3350")
	colorMuted   = lipgloss.Color("#8B8499")
	colorText    = lipgloss.Color("#EAE6F2")
	colorUser    = lipgloss.Color("#F4D03F")
	colorOk      = lipgloss.Color("#2ECC71")
	colorErr     = lipgloss.Color("#FF6B6B")
	colorTool    = lipgloss.Color("#5EEAD4")
)

var (
	styleBase = lipgloss.NewStyle().Foreground(colorText)

	styleHeader = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorAccent).
			Padding(0, 1)

	styleModelBadge = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorBg).
			Padding(0, 1)

	styleModeBadge = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorOk).
			Padding(0, 1)

	styleStatusBar = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	styleInputBox = lipgloss.NewStyle().
			Padding(0, 1)

	// styleChatInput 聊天输入框：圆角边框替代上下 "----" 分隔线
	styleChatInput = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	styleUserMsg = lipgloss.NewStyle().
			Foreground(colorUser).
			Bold(true)

	styleAssistant = lipgloss.NewStyle().Foreground(colorText)

	styleToolLine = lipgloss.NewStyle().
			Foreground(colorTool).
			Margin(0, 0, 0, 1)

	styleToolOutput = lipgloss.NewStyle().
			Foreground(colorMuted).
			Margin(0, 0, 0, 3)

	styleError = lipgloss.NewStyle().
			Foreground(colorErr)

	styleSysMsg = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	// 完成统计行：绿色对勾 + 灰调数值
	styleStats = lipgloss.NewStyle().
			Foreground(colorOk)

	styleHintSel = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorAccent2)

	styleTimestamp = lipgloss.NewStyle().Foreground(colorMuted).Faint(true)
)

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
