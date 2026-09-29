package webui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omni/internal/agent"
	"omni/internal/config"
	"omni/internal/llm"
	"omni/internal/store"
	"omni/internal/tools"
)

type fakeProvider struct{}

func (fakeProvider) Name() string          { return "fake" }
func (fakeProvider) Models() []string      { return []string{"fake-model"} }
func (fakeProvider) String() string        { return "fake" }

func (fakeProvider) Chat(ctx context.Context, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (*llm.ChatResponse, error) {
	_ = onEvent(llm.StreamEvent{Type: llm.EventText, Text: "你好"})
	return &llm.ChatResponse{Content: "你好"}, nil
}

func newTestServer(t *testing.T, withProvider bool) *server {
	t.Helper()
	dir := t.TempDir()
	s := &server{
		opts: Options{
			ConfigPath: filepath.Join(dir, "config.json"),
			StartCwd:   dir,
			Version:    "test",
		},
		cfg:    config.Default(),
		store:  store.New(dir),
		agents: map[string]*agent.Agent{},
	}
	if withProvider {
		s.provider = fakeProvider{}
	}
	return s
}

func do(t *testing.T, s *server, method, path, body string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestWebUIEndToEnd(t *testing.T) {
	s := newTestServer(t, true)

	// 页面（dist 占位内容）
	code, body := do(t, s, "GET", "/", "")
	if code != 200 || !strings.Contains(body, "omni") {
		t.Fatalf("页面异常: %d %s", code, body)
	}

	// 状态
	code, body = do(t, s, "GET", "/api/state", "")
	if code != 200 || !strings.Contains(body, `"configured":true`) || !strings.Contains(body, "fake-model") {
		t.Fatalf("state 异常: %d %s", code, body)
	}

	// 建任务
	code, body = do(t, s, "POST", "/api/tasks", `{}`)
	if code != 200 {
		t.Fatalf("建任务失败: %d %s", code, body)
	}
	var created struct {
		Task *store.Task `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.Task == nil {
		t.Fatalf("解析任务失败: %s", body)
	}
	taskID := created.Task.ID

	// 聊天 SSE
	code, body = do(t, s, "POST", "/api/chat", `{"task_id":"`+taskID+`","text":"hi"}`)
	if code != 200 {
		t.Fatalf("chat 失败: %d %s", code, body)
	}
	if !strings.Contains(body, `"text":"你好"`) || !strings.Contains(body, `"type":"done"`) {
		t.Fatalf("SSE 异常: %s", body)
	}

	// 消息已持久化、标题已生成
	code, body = do(t, s, "GET", "/api/tasks/"+taskID, "")
	if code != 200 || !strings.Contains(body, "你好") || !strings.Contains(body, `"title":"hi"`) {
		t.Fatalf("任务持久化异常: %d %s", code, body)
	}

	// 任务列表
	code, body = do(t, s, "GET", "/api/tasks", "")
	if code != 200 || !strings.Contains(body, taskID) {
		t.Fatalf("任务列表异常: %d %s", code, body)
	}

	// 设置：切换主题并写回配置文件
	code, body = do(t, s, "POST", "/api/settings", `{"theme":"light"}`)
	if code != 200 || !strings.Contains(body, `"theme":"light"`) {
		t.Fatalf("设置异常: %d %s", code, body)
	}
	if _, err := os.Stat(s.opts.ConfigPath); err != nil {
		t.Fatal("设置应写回配置文件")
	}
}

func TestWebUIUnconfigured(t *testing.T) {
	s := newTestServer(t, false)
	code, _ := do(t, s, "POST", "/api/tasks", `{}`)
	if code != 200 {
		t.Fatalf("建任务不应依赖 provider: %d", code)
	}
	code, body := do(t, s, "POST", "/api/chat", `{"task_id":"nonexist","text":"hi"}`)
	if code != http.StatusNotFound {
		t.Fatalf("不存在的任务应 404: %d %s", code, body)
	}
}

func TestProjectsAndBrowse(t *testing.T) {
	s := newTestServer(t, false)
	dir := t.TempDir()

	// 添加项目
	code, body := do(t, s, "POST", "/api/projects", `{"path":"`+strings.ReplaceAll(dir, `\`, `\\`)+`"}`)
	if code != 200 || !strings.Contains(body, `"name"`) {
		t.Fatalf("添加项目失败: %d %s", code, body)
	}
	var added struct {
		Project *struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	_ = json.Unmarshal([]byte(body), &added)
	if added.Project == nil || added.Project.ID == "" {
		t.Fatalf("项目响应异常: %s", body)
	}

	// 不存在的目录应拒绝
	code, _ = do(t, s, "POST", "/api/projects", `{"path":"Z:/definitely/not/exist"}`)
	if code == 200 {
		t.Fatal("不存在的目录应被拒绝")
	}

	// 目录浏览
	code, body = do(t, s, "GET", "/api/browse?path="+strings.ReplaceAll(dir, `\`, `/`), "")
	if code != 200 || !strings.Contains(body, "entries") {
		t.Fatalf("浏览失败: %d %s", code, body)
	}

	// 建项目任务 → 删除项目 → 任务归属清空
	code, body = do(t, s, "POST", "/api/tasks", `{"project_id":"`+added.Project.ID+`"}`)
	var created struct {
		Task *store.Task `json:"task"`
	}
	_ = json.Unmarshal([]byte(body), &created)
	if code != 200 || created.Task == nil {
		t.Fatalf("建任务失败: %d %s", code, body)
	}
	code, _ = do(t, s, "DELETE", "/api/projects/"+added.Project.ID, "")
	if code != 200 {
		t.Fatalf("删除项目失败: %d", code)
	}
	got, err := s.store.GetTask(created.Task.ID)
	if err != nil || got.ProjectID != "" {
		t.Fatalf("删除项目后任务归属应为空: %v %+v", err, got)
	}
}

func TestAgentForReusesAgent(t *testing.T) {
	s := newTestServer(t, true)
	t1, _ := s.store.CreateTask("", "fake-model", "build", "x")
	a1, err := s.agentFor(t1)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := s.agentFor(t1)
	if err != nil {
		t.Fatal(err)
	}
	if a1 != a2 {
		t.Fatal("同一任务应复用同一 Agent")
	}
	_ = tools.NewRegistry // 保持 tools 引用
}
