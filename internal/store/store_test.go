package store

import (
	"os"
	"path/filepath"
	"testing"

	"omni/internal/llm"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return New(t.TempDir())
}

func TestTaskCRUD(t *testing.T) {
	s := newTestStore(t)

	ta, err := s.CreateTask("", "gpt-4o", "build", "帮我修复登录 bug\n第二步")
	if err != nil {
		t.Fatal(err)
	}
	if ta.Title != "帮我修复登录 bug 第二步" {
		t.Fatalf("标题不符合预期: %q", ta.Title)
	}
	if len(ta.ID) != 12 {
		t.Fatalf("任务 ID 长度应为 12: %q", ta.ID)
	}

	ta.Messages = append(ta.Messages,
		llm.NewTextMessage(llm.RoleUser, "帮我修复登录 bug"),
		llm.NewTextMessage(llm.RoleAssistant, "好的，先看下代码。"),
	)
	if err := s.SaveTask(ta); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(ta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("消息数不符: %d", len(got.Messages))
	}
	if got.Messages[1].Text != "好的，先看下代码。" {
		t.Fatalf("消息内容不符: %q", got.Messages[1].Text)
	}

	if err := s.DeleteTask(ta.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTask(ta.ID); err == nil {
		t.Fatal("删除后读取应失败")
	}
}

func TestTaskListOrderAndProject(t *testing.T) {
	s := newTestStore(t)
	p, err := s.AddProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t1, _ := s.CreateTask("", "m1", "build", "任务一")
	t2, _ := s.CreateTask(p.ID, "m1", "build", "项目任务")
	_ = t1

	byProject, err := s.ListTasksByProject(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(byProject) != 1 || byProject[0].ID != t2.ID {
		t.Fatalf("项目任务过滤不符: %+v", byProject)
	}

	all, err := s.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("应有 2 个任务: %d", len(all))
	}
	if all[0].Messages != nil {
		t.Fatal("列表摘要不应含消息")
	}
}

func TestProjectAddDedupeAndDelete(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()

	p1, err := s.AddProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.AddProject(filepath.Join(dir, "sub", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if p1.ID != p2.ID {
		t.Fatal("同一路径应去重返回同一项目")
	}
	if p1.Name == "" || p1.Path != p2.Path {
		t.Fatalf("项目字段不符: %+v", p1)
	}

	// 删除项目为软删除：任务归属保留（任务隐藏），项目清单不再列出
	ta, _ := s.CreateTask(p1.ID, "m", "build", "x")
	if err := s.DeleteProject(p1.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(ta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != p1.ID {
		t.Fatalf("删除项目后任务归属应保留: %q", got.ProjectID)
	}
	projects, _ := s.ListProjects()
	if len(projects) != 0 {
		t.Fatalf("项目清单应不含已删除项目: %d", len(projects))
	}

	// 重新添加同路径项目：复活原记录（同 ID），任务归属自动恢复
	p3, err := s.AddProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p3.ID != p1.ID {
		t.Fatalf("复活的项目应保持同一 ID: %s != %s", p3.ID, p1.ID)
	}
	got, err = s.GetTask(ta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != p3.ID {
		t.Fatalf("重新添加后任务应恢复归属: %q", got.ProjectID)
	}
	projects, _ = s.ListProjects()
	if len(projects) != 1 {
		t.Fatalf("复活后项目清单应有 1 个: %d", len(projects))
	}
}

func TestInvalidIDRejected(t *testing.T) {
	s := newTestStore(t)
	for _, id := range []string{"../../etc", "short", "zzzzzzzzzzzz"} {
		if _, err := s.GetTask(id); err == nil {
			t.Fatalf("应拒绝非法 ID: %q", id)
		}
		if err := s.DeleteTask(id); err == nil {
			t.Fatalf("应拒绝非法 ID: %q", id)
		}
	}
	_ = os.MkdirAll(s.sessionsDir(), 0o755)
}
