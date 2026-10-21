package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omni/internal/agent"
	"omni/internal/memory"
	"omni/internal/skills"
	"omni/internal/webui"
)

const helpText = `omni 终端 AI 编码智能体

快捷键:
  Enter          发送消息
  Alt+Enter      换行
  Esc            取消当前生成
  Ctrl+C         退出（生成中再次按键强制退出）
  Ctrl+O         展开 / 折叠已完成的工具调用
  Ctrl+R         重新生成最后一条回复
  Ctrl+L         滚动到底部

Slash 命令（输入 / 出现提示，↑↓ 选择 · Tab 补全）:
  /help          显示本帮助
  /new           开启新会话（原会话保留在任务列表）
  /tasks         列出任务（含桌面版/网页版创建的，双向同步）
  /open <编号>    打开任务继续对话（编号见 /tasks）
  /config        打开模型配置表单（类型/模型/Base URL/API Key）
  /model         列出模型供选择（/model <名称> 直接切换）
  /mode <模式>    build=可修改文件/执行命令；plan=只读
  /skills        列出并重新加载技能
  /memory        查看全局记忆
  /mcp           查看 MCP 服务器与工具状态
  /webui         在浏览器打开网页版聊天
  /quit          退出

技能: ~/.config/omni/skills/<名称>/SKILL.md（全局）或 .omni/skills/<名称>/SKILL.md（项目），
      输入 /技能名 直接触发（不进 / 补全提示列表，/skills 查看已装技能）。
工具: read · write · edit · bash · grep · glob · skill · memory · mcp__*
配置: 默认读取 ~/.config/omni/config.json，支持 OPENAI_API_KEY 等环境变量；
      无可用配置时启动会自动打开 /config 表单。`

func (m *Model) handleCommand(cmd string) tea.Cmd {
	args := strings.Fields(cmd)
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "/help", "/?":
		m.addSystemMsg(helpText)
	case "/new":
		if !m.running {
			m.a.ClearHistory()
			m.msgs = nil
			m.toolID = 0
			m.rollback = nil
			m.task = nil // 旧任务已落盘，之后重新挂到 /tasks 列表
			m.addSystemMsg("已开启新会话。原会话仍在任务列表（/tasks 查看）。")
		} else {
			m.addSystemMsg("请先取消当前任务（Esc）")
		}
	case "/tasks":
		m.listTasks()
	case "/open", "/o":
		if len(args) < 2 {
			m.addSystemMsg("用法: /open <编号>（编号见 /tasks）")
			return nil
		}
		m.openTask(args[1])
	case "/config":
		if m.running {
			m.addSystemMsg("请先取消当前任务（Esc）")
			return nil
		}
		m.openConfig()
	case "/model", "/m":
		if len(args) < 2 {
			// 不带参数：列出配置中所有模型供选择
			return m.openModelPicker()
		}
		m.switchModel(args[1], -1)
	case "/mode":
		if len(args) < 2 {
			m.addSystemMsg("用法: /mode build|plan")
			return nil
		}
		switch args[1] {
		case "build":
			m.a.SetMode(agent.ModeBuild)
			m.addSystemMsg("模式切换为 build（可写文件、执行命令）")
		case "plan":
			m.a.SetMode(agent.ModePlan)
			m.addSystemMsg("模式切换为 plan（只读）")
		default:
			m.addSystemMsg("未知模式: " + args[1] + "（可选 build / plan）")
		}
	case "/skills":
		m.reloadSkills()
		if len(m.skillCmds) == 0 {
			m.addSystemMsg("未找到技能。将 SKILL.md 放到 ~/.config/omni/skills/<名称>/ 或 .omni/skills/<名称>/ 后重试。")
			return nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("已加载 %d 个技能:\n", len(m.skillCmds)))
		for _, s := range m.skillCmds {
			src := "全局"
			if s.Source == "project" {
				src = "项目"
			}
			fmt.Fprintf(&b, "  /%s  %s（%s）\n", s.Name, s.Description, src)
		}
		m.addSystemMsg(strings.TrimRight(b.String(), "\n"))
	case "/memory":
		if m.extras == nil || m.extras.MemoryPath == "" {
			m.addSystemMsg("全局记忆不可用。")
			return nil
		}
		if mem := memory.Load(m.extras.MemoryPath); mem != "" {
			m.addSystemMsg("全局记忆（" + m.extras.MemoryPath + "）:\n" + mem)
		} else {
			m.addSystemMsg("全局记忆为空。agent 可通过 memory 工具自动沉淀；也可直接编辑 " + m.extras.MemoryPath)
		}
	case "/mcp":
		if m.extras == nil || m.extras.MCP == nil {
			m.addSystemMsg("未启用 MCP。在配置文件中添加 mcp_servers 后启动。")
			return nil
		}
		statuses := m.extras.MCP.Statuses()
		if len(statuses) == 0 {
			m.addSystemMsg("未配置 MCP 服务器。在 ~/.config/omni/config.json 中添加 mcp_servers。")
			return nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("MCP 服务器 %d 个:\n", len(statuses)))
		for _, st := range statuses {
			state := "已连接"
			if !st.Connected {
				state = "失败"
			}
			fmt.Fprintf(&b, "  %s [%s] %s — %s，工具 %d 个", st.Name, st.Kind, st.Target, state, st.ToolCount)
			if st.Err != "" {
				b.WriteString("（" + st.Err + "）")
			}
			b.WriteString("\n")
		}
		m.addSystemMsg(strings.TrimRight(b.String(), "\n"))
	case "/webui":
		url, err := webui.Launch(webui.Options{
			ConfigPath:  m.configPath,
			Extras:      m.extras,
			StartCwd:    m.cwd,
		})
		if err != nil {
			m.addSystemMsg("启动网页版失败: " + err.Error())
			return nil
		}
		m.addSystemMsg("网页版已打开: " + url)
	case "/quit", "/exit", "/q":
		return tea.Quit
	default:
		// 技能命令：/技能名 → 以技能正文发起一轮对话
		if s, ok := m.extrasSkill(args[0]); ok {
			prompt := "请使用技能 " + s.Name + " 执行以下指令：\n\n" + s.Body()
			m.addUserMsg("/" + s.Name)
			return m.startAgent(prompt)
		}
		m.addSystemMsg("未知命令: " + args[0] + "（输入 /help 查看帮助）")
	}
	return nil
}

// extrasSkill 按命令名查找技能。
func (m *Model) extrasSkill(cmdName string) (*skills.Skill, bool) {
	if m.extras == nil || m.extras.Skills == nil {
		return nil, false
	}
	return m.extras.Skills.Lookup(m.cwd, cmdName)
}
