package tui

import (
	"fmt"
	"strconv"
	"strings"

	"omni/internal/agent"
	"omni/internal/llm"
)

// 任务同步：TUI 会话与网页版/桌面版共用 ~/.local/share/omni 下的任务存储，
// 任何一端创建的任务都能在另一端的列表里看到。

const taskListLimit = 15

// saveTask 把当前会话的完整历史落盘；没有任务（尚未对话或 /new 后）则跳过。
func (m *Model) saveTask() {
	if m.task == nil {
		return
	}
	m.task.Messages = m.a.History()
	m.task.Model = m.a.Model()
	m.task.Mode = string(m.a.Mode())
	_ = m.st.SaveTask(m.task)
}

// listTasks 列出存储中的任务（含其他端创建/正在处理的），/open 用同序编号。
func (m *Model) listTasks() {
	tasks, err := m.st.ListTasks()
	if err != nil {
		m.addSystemMsg("读取任务列表失败: " + err.Error())
		return
	}
	if len(tasks) == 0 {
		m.addSystemMsg("还没有任务。发起对话后会自动保存；桌面版/网页版创建的任务也会出现在这里。")
		return
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("任务 %d 个（/open <编号> 打开，* 为当前会话）:\n", len(tasks)))
	shown := len(tasks)
	if shown > taskListLimit {
		shown = taskListLimit
	}
	for i, t := range tasks[:shown] {
		mark := " "
		if m.task != nil && t.ID == m.task.ID {
			mark = "*"
		}
		fmt.Fprintf(&b, " %s%d. %s（%s）\n", mark, i+1, t.Title, t.SortKey().Format("01-02 15:04"))
	}
	if len(tasks) > shown {
		fmt.Fprintf(&b, " …另有 %d 个更早的任务\n", len(tasks)-shown)
	}
	m.addSystemMsg(strings.TrimRight(b.String(), "\n"))
}

// openTask 按 /tasks 列表序号加载任务：恢复历史、模型与模式，并设为当前任务。
func (m *Model) openTask(arg string) {
	if m.running {
		m.addSystemMsg("请先取消当前任务（Esc）")
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || n < 1 {
		m.addSystemMsg("用法: /open <编号>（编号见 /tasks）")
		return
	}
	tasks, err := m.st.ListTasks()
	if err != nil {
		m.addSystemMsg("读取任务列表失败: " + err.Error())
		return
	}
	if n > len(tasks) {
		m.addSystemMsg(fmt.Sprintf("编号超出范围: %d（共 %d 个任务，/tasks 查看）", n, len(tasks)))
		return
	}
	full, err := m.st.GetTask(tasks[n-1].ID)
	if err != nil {
		m.addSystemMsg("读取任务失败: " + err.Error())
		return
	}

	m.a.SetHistory(full.Messages)
	if full.Model != "" {
		m.a.SetModel(full.Model)
	}
	switch full.Mode {
	case string(agent.ModeBuild):
		m.a.SetMode(agent.ModeBuild)
	case string(agent.ModePlan):
		m.a.SetMode(agent.ModePlan)
	}

	// 从持久化消息重建聊天流：用户/助手文本照常渲染；
	// 工具调用过程只有请求没有输出，就不逐条还原了
	m.msgs = nil
	m.toolID = 0
	for _, msg := range full.Messages {
		switch msg.Role {
		case llm.RoleUser:
			m.msgs = append(m.msgs, &viewMsg{role: roleUser, text: msg.Text})
		case llm.RoleAssistant:
			if msg.Text != "" {
				m.msgs = append(m.msgs, &viewMsg{role: roleAssistant, text: msg.Text, done: true})
			}
		}
	}
	m.task = full
	m.setViewportContent()
	m.addSystemMsg("已打开任务: " + full.Title + "（" + full.ID + "）。继续输入可接着对话。")
}
