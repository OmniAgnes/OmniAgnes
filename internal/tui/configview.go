package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"omni/internal/config"
	"omni/internal/llm"
)

var cfgLabels = []string{"类型", "模型", "Base URL", "API Key"}

// setupConfigInputs 构造配置表单的四个输入框。
func setupConfigInputs() []textinput.Model {
	inputs := make([]textinput.Model, len(cfgLabels))
	for i := range inputs {
		in := textinput.New()
		in.Prompt = "  "
		in.CharLimit = 512
		inputs[i] = in
	}
	inputs[0].Placeholder = "openai / anthropic / gemini / ollama"
	inputs[1].Placeholder = "如 gpt-4o / claude-sonnet-4-20250514 / qwen2.5-coder"
	inputs[2].Placeholder = "可留空；本地服务如 http://localhost:11434/v1"
	inputs[3].Placeholder = "云端服务必填；本地 Ollama 可留空"
	inputs[3].EchoMode = textinput.EchoPassword
	return inputs
}

// openConfig 打开模型配置表单。
func (m *Model) openConfig() {
	m.configOpen = true
	m.cfgErr = ""
	m.cfgFocus = 0
	for i := range m.cfgInputs {
		m.cfgInputs[i].Reset()
		m.cfgInputs[i].Blur()
	}
	m.cfgInputs[0].Focus()
}

func (m *Model) cycleConfigFocus(delta int) tea.Cmd {
	n := len(m.cfgInputs)
	m.cfgInputs[m.cfgFocus].Blur()
	m.cfgFocus = ((m.cfgFocus+delta)%n + n) % n
	return m.cfgInputs[m.cfgFocus].Focus()
}

// handleConfigKey 处理配置表单按键。
func (m *Model) handleConfigKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.configOpen = false
		m.cfgErr = ""
		return nil
	case "tab":
		return m.cycleConfigFocus(1)
	case "shift+tab":
		return m.cycleConfigFocus(-1)
	case "enter":
		if m.cfgFocus == len(m.cfgInputs)-1 {
			return m.submitConfig()
		}
		return m.cycleConfigFocus(1)
	default:
		var cmd tea.Cmd
		m.cfgInputs[m.cfgFocus], cmd = m.cfgInputs[m.cfgFocus].Update(msg)
		return cmd
	}
}

// submitConfig 校验并应用配置：连接 provider、写入配置文件。
func (m *Model) submitConfig() tea.Cmd {
	typ := strings.TrimSpace(m.cfgInputs[0].Value())
	model := strings.TrimSpace(m.cfgInputs[1].Value())
	baseURL := strings.TrimSpace(m.cfgInputs[2].Value())
	apiKey := m.cfgInputs[3].Value()
	if typ == "" || model == "" {
		m.cfgErr = "类型与模型为必填项"
		return nil
	}
	p, err := llm.New(llm.ConfigP{Type: typ, Model: model, BaseURL: baseURL, APIKey: apiKey})
	if err != nil {
		m.cfgErr = err.Error()
		return nil
	}
	m.a.SetProvider(p, model)
	if err := config.SaveProviderConfig(m.configPath, typ, model, baseURL, apiKey); err != nil {
		m.addSystemMsg("已连接，但写入配置文件失败: " + err.Error())
	} else {
		m.addSystemMsg(fmt.Sprintf("模型已配置: %s (%s)，配置已保存到 %s", model, typ, m.configPath))
	}
	m.configOpen = false
	m.cfgErr = ""
	return nil
}

// renderConfig 渲染全屏配置表单。
func (m *Model) renderConfig() string {
	title := styleHeader.Render(" 配置模型 ") + styleSysMsg.Render("  Esc 取消 · Tab 切换字段 · Enter 下一项/保存")
	rows := []string{title, ""}
	for i, in := range m.cfgInputs {
		label := cfgLabels[i] + ": "
		if i != m.cfgFocus {
			label = styleSysMsg.Render(label)
		} else {
			label = styleUserMsg.Render(label)
		}
		rows = append(rows, label+in.View())
	}
	rows = append(rows, "", styleSysMsg.Render("保存后立即生效，并写入配置文件供下次启动使用"))
	if m.cfgErr != "" {
		rows = append(rows, styleError.Render(m.cfgErr))
	}
	box := styleInputBox.Width(m.width - 6).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
