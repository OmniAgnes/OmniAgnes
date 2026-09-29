package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omni/internal/agent"
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

Slash 命令:
  /help          显示本帮助
  /new           开启新会话（清空历史）
  /config        打开模型配置表单（类型/模型/Base URL/API Key）
  /model <名称>   切换模型（如 /model gpt-4o）
  /mode <模式>    build=可修改文件/执行命令；plan=只读
  /quit          退出

工具: read · write · edit · bash · grep · glob
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
			m.addSystemMsg("已开启新会话。")
		} else {
			m.addSystemMsg("请先取消当前任务（Esc）")
		}
	case "/config":
		if m.running {
			m.addSystemMsg("请先取消当前任务（Esc）")
			return nil
		}
		m.openConfig()
	case "/model", "/m":
		if len(args) < 2 {
			m.addSystemMsg("用法: /model <模型名>")
			return nil
		}
		model := args[1]
		m.a.SetModel(model)
		m.addSystemMsg(fmt.Sprintf("模型已切换为 %s", model))
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
	case "/quit", "/exit", "/q":
		return tea.Quit
	default:
		m.addSystemMsg("未知命令: " + args[0] + "（输入 /help 查看帮助）")
	}
	return nil
}
