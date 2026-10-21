package tui

// 布局自检：headless 渲染 View()，按行号打印，检查分隔线与输入行的间距。
// 临时诊断用，跑完可留作回归。

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omni/internal/agent"
)

func TestRenderLayoutInspect(t *testing.T) {
	m := New(agent.New(nil, nil, agent.Options{}), "sess", "cfg.json", nil, `C:\demo`)
	m.configOpen = false
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/h")})
	for i, line := range strings.Split(m.View(), "\n") {
		t.Logf("%2d|%s", i+1, line)
	}
}
