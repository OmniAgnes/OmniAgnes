package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"

	"omni/internal/agent"
	"omni/internal/llm"
	"omni/internal/store"
)

func newViewport(w int) viewport.Model {
	return viewport.New(w, 10)
}

// TUI 恢复任务：/open 后 agent 历史、当前任务与聊天流都应就位。
func TestOpenTaskRestoresHistory(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)

	// 模拟桌面版/网页版创建并保存过的任务
	created, err := st.CreateTask("", "fake-model", "build", "修复登录页")
	if err != nil {
		t.Fatal(err)
	}
	created.Messages = []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "修复登录页"),
		llm.NewTextMessage(llm.RoleAssistant, "已修复，工具调用过程从略"),
	}
	if err := st.SaveTask(created); err != nil {
		t.Fatal(err)
	}

	m := &Model{st: st, cwd: dir, viewport: newViewport(80)}
	m.a = agent.New(nil, nil, agent.Options{})
	m.openTask("1")

	if m.task == nil || m.task.ID != created.ID {
		t.Fatalf("当前任务未切换: %+v", m.task)
	}
	hist := m.a.History()
	if len(hist) != 2 || hist[0].Text != "修复登录页" {
		t.Fatalf("agent 历史未恢复: %+v", hist)
	}
	if len(m.msgs) != 3 || m.msgs[0].role != roleUser || m.msgs[1].text != "已修复，工具调用过程从略" || m.msgs[2].role != roleSystem {
		t.Fatalf("聊天流重建异常: %+v", m.msgs)
	}

	// 恢复后继续对话，落盘的消息应写回任务
	m.a.Prepare("再检查一下")
	m.saveTask()
	saved, err := st.GetTask(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Messages) != 3 || saved.Messages[2].Text != "再检查一下" {
		t.Fatalf("落盘消息异常: %+v", saved.Messages)
	}
}

// /tasks 与 /open 共用同一排序（按更新时间倒序，编号 1 最新）：越界/非法编号不切换当前任务。
func TestTasksListAndOpenBounds(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if _, err := st.CreateTask("", "m", "build", "任务一"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // 保证更新时间有序
	if _, err := st.CreateTask("", "m", "build", "任务二"); err != nil {
		t.Fatal(err)
	}

	m := &Model{st: st, cwd: dir, viewport: newViewport(80)}
	m.a = agent.New(nil, nil, agent.Options{})

	m.listTasks() // 渲染不 panic 即可
	if m.task != nil {
		t.Fatal("列表操作不应切换当前任务")
	}

	m.openTask("1") // 编号 1 = 最新的任务二
	if m.task == nil || m.task.Title != "任务二" {
		t.Fatalf("应打开最新任务（任务二）: %+v", m.task)
	}

	m.openTask("3")
	m.openTask("abc")
	// 越界/非法编号应保持当前任务不变
	if m.task == nil || m.task.Title != "任务二" {
		t.Fatalf("非法编号不应切换任务: %+v", m.task)
	}
}
