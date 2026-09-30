package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omni/internal/config"
	"omni/internal/llm"
)

// modelItem 是 /model 选择器中的一行。
type modelItem struct {
	name     string
	provider string // openai / anthropic / gemini / ollama
	provIdx  int    // 在 cfg.Providers 中的下标；-1 表示来源未知
	current  bool
}

// modelsFetchedMsg 是异步拉取供应商模型清单的结果。
type modelsFetchedMsg struct {
	items []modelItem // 远端模型（尚未合并）
	err   string      // 拉取失败原因（空为成功）
}

// openModelPicker 无参 /model 时打开模型选择器：
// 先立即展示配置中收录的模型，同时异步拉取各供应商 /models 全量清单，
// 拉到后合并刷新（modelsFetchedMsg）。
func (m *Model) openModelPicker() tea.Cmd {
	items := m.listModels()
	if len(items) == 0 {
		m.addSystemMsg("没有可切换的模型。先用 /config 配置供应商，或在 " + m.configPath + " 的 providers 中添加模型。")
		return nil
	}
	m.setModelItems(items)
	m.modelLoading = true
	return func() tea.Msg { return fetchModelsCmd(m.configPath)() }
}

// setModelItems 写入列表并把光标定位到当前模型。
func (m *Model) setModelItems(items []modelItem) {
	m.modelItems = items
	m.modelPickerIdx = 0
	for i, it := range items {
		if it.current {
			m.modelPickerIdx = i
			break
		}
	}
}

// listModels 汇总配置中所有供应商的模型（本地配置，立即可用）。
func (m *Model) listModels() []modelItem {
	cur := m.a.Model()
	var out []modelItem
	seen := map[string]bool{}
	cfg, err := config.Load(m.configPath)
	if err == nil {
		for i, p := range cfg.Providers {
			names := p.Models
			if len(names) == 0 && p.Model != "" {
				names = []string{p.Model}
			}
			for _, name := range names {
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				out = append(out, modelItem{name: name, provider: p.Type, provIdx: i, current: name == cur})
			}
		}
	}
	// 配置为空但已有 provider（如环境变量探测）：至少列出当前模型
	if len(out) == 0 && cur != "" {
		out = append(out, modelItem{name: cur, provider: "当前", provIdx: -1, current: true})
	}
	return out
}

// fetchModelsCmd 遍历配置中的供应商，调 GET /models 拉取全量模型清单。
func fetchModelsCmd(configPath string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load(configPath)
		if err != nil {
			return modelsFetchedMsg{err: "读取配置失败: " + err.Error()}
		}
		var items []modelItem
		var errs []string
		for i, p := range cfg.Providers {
			prov, nerr := llm.New(llm.ConfigP{
				Type: p.Type, Model: p.Model, BaseURL: p.BaseURL, APIKey: p.ResolveAPIKey(),
			})
			if nerr != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", p.Type, nerr))
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			names, lerr := prov.ListModels(ctx)
			cancel()
			if lerr != nil {
				errs = append(errs, fmt.Sprintf("%s(%s): %v", p.Type, p.BaseURL, lerr))
				continue
			}
			for _, n := range names {
				items = append(items, modelItem{name: n, provider: p.Type, provIdx: i})
			}
		}
		return modelsFetchedMsg{items: items, err: strings.Join(errs, "；")}
	}
}

// mergeModelItems 把远端清单合并进本地列表（本地在前去重），并保留当前模型标记。
func mergeModelItems(local, remote []modelItem, cur string) []modelItem {
	seen := map[string]bool{}
	out := make([]modelItem, 0, len(local)+len(remote))
	for _, it := range local {
		if !seen[it.name] {
			seen[it.name] = true
			it.current = it.name == cur
			out = append(out, it)
		}
	}
	for _, it := range remote {
		if !seen[it.name] {
			seen[it.name] = true
			it.current = it.name == cur
			out = append(out, it)
		}
	}
	return out
}

// switchModel 切换模型。provIdx ≥ 0 时直接采用对应供应商配置（远端拉来的模型也能正确寻址）；
// 目标模型未在配置收录时补写进该供应商 models 并持久化，下次 /model 直接可选。
func (m *Model) switchModel(name string, provIdx int) {
	cfg, err := config.Load(m.configPath)
	var p *config.ProviderConfig
	if err == nil {
		if provIdx >= 0 && provIdx < len(cfg.Providers) {
			p = &cfg.Providers[provIdx]
		} else {
			p = cfg.ProviderForModel(name)
		}
	}
	if p != nil {
		apiKey := p.ResolveAPIKey()
		if apiKey == "" && p.Type != "ollama" && !strings.Contains(p.BaseURL, "localhost") {
			m.addSystemMsg(fmt.Sprintf("供应商 %s（%s）缺少 API Key，无法切换到 %s；请用 /config 补全", p.Type, p.Model, name))
			return
		}
		prov, nerr := llm.New(llm.ConfigP{Type: p.Type, Model: name, BaseURL: p.BaseURL, APIKey: apiKey})
		if nerr != nil {
			m.addSystemMsg(fmt.Sprintf("切换到 %s 失败: %v", name, nerr))
			return
		}
		m.a.SetProvider(prov, name)
		cfg.Provider = name
		// 远端新模型补录进供应商配置，避免下次又找不到
		if err == nil && !p.HasModel(name) {
			p.Models = append(p.Models, name)
			p.Model = name
		}
		if serr := cfg.Save(m.configPath); serr != nil {
			m.addSystemMsg("已切换，但写入配置失败: " + serr.Error())
			return
		}
		m.addSystemMsg(fmt.Sprintf("模型已切换为 %s（%s），已设为默认", name, p.Type))
		return
	}
	m.a.SetModel(name)
	m.addSystemMsg(fmt.Sprintf("模型已切换为 %s（配置中未收录该模型，沿用当前供应商）", name))
}

// handleModelPickerKey 处理模型选择器按键。
func (m *Model) handleModelPickerKey(msg tea.KeyMsg) tea.Cmd {
	n := len(m.modelItems)
	k := msg.String()
	switch {
	case k == "ctrl+c":
		m.modelPickerOpen = false
		m.modelLoading = false
		return tea.Quit
	case k == "esc":
		m.modelPickerOpen = false
		m.modelLoading = false
	case k == "up":
		m.modelPickerIdx = (m.modelPickerIdx - 1 + n) % n
	case k == "down":
		m.modelPickerIdx = (m.modelPickerIdx + 1) % n
	case k == "enter":
		m.modelPickerOpen = false
		m.modelLoading = false
		m.switchModel(m.modelItems[m.modelPickerIdx].name, m.modelItems[m.modelPickerIdx].provIdx)
	default:
		// 数字键快捷选择（列表不超过 9 项时启用）
		if n <= 9 && len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			if idx := int(k[0] - '1'); idx < n {
				m.modelPickerOpen = false
				m.modelLoading = false
				m.switchModel(m.modelItems[idx].name, m.modelItems[idx].provIdx)
			}
		}
	}
	return nil
}

// renderModelPicker 渲染全屏模型选择器。
func (m *Model) renderModelPicker() string {
	hint := "  ↑↓ 移动 · Enter 选择 · 数字快捷 · Esc 取消"
	if m.modelLoading {
		hint = "  正在拉取供应商模型列表…" + hint
	}
	title := styleHeader.Render(" 选择模型 ") + styleSysMsg.Render(hint)
	rows := []string{title, ""}

	// 列表超过可用高度时以选中项为窗口滚动
	// 列宽按最长模型名动态取值，供应商列至少隔 3 格
	nameCol := 0
	for i, it := range m.modelItems {
		if w := lipgloss.Width(fmt.Sprintf("%d. %s", i+1, it.name)) + 3; w > nameCol {
			nameCol = w
		}
	}
	maxRows := m.height - 12
	if maxRows < 4 {
		maxRows = 4
	}
	start := 0
	if len(m.modelItems) > maxRows {
		start = m.modelPickerIdx - maxRows/2
		if start < 0 {
			start = 0
		}
		if start+maxRows > len(m.modelItems) {
			start = len(m.modelItems) - maxRows
		}
	}
	for i := start; i < start+maxRows && i < len(m.modelItems); i++ {
		it := m.modelItems[i]
		label := fmt.Sprintf("%d. %s", i+1, it.name)
		if pad := nameCol - lipgloss.Width(label); pad > 0 {
			label += strings.Repeat(" ", pad)
		}
		label += "  " + it.provider
		if it.current {
			label += "  ← 当前"
		}
		if i == m.modelPickerIdx {
			rows = append(rows, styleHintSel.Render(" "+label+" "))
		} else {
			rows = append(rows, styleBase.Render(" "+label+" "))
		}
	}

	foot := "模型来自 " + m.configPath + " 的 providers 与供应商 API；用 /config 可添加新供应商"
	if m.modelFetchErr != "" {
		foot = "⚠ " + m.modelFetchErr
	}
	if start > 0 {
		foot = fmt.Sprintf("↑ 还有 %d 项  ", start) + foot
	}
	if end := len(m.modelItems) - (start + maxRows); end > 0 {
		foot = fmt.Sprintf("↓ 还有 %d 项  ", end) + foot
	}
	rows = append(rows, "", styleSysMsg.Render(foot))

	box := styleInputBox.Width(m.width - 6).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
