// Package store 提供任务（会话）与项目的本地文件持久化。
// 任务保存为 <dataDir>/sessions/<id>.json，项目清单保存为 <dataDir>/projects.json。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"omni/internal/llm"
)

// Task 是一次会话任务（一问一答的完整消息流）。
type Task struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	ProjectID string        `json:"project_id,omitempty"`
	Model     string        `json:"model,omitempty"`
	Mode      string        `json:"mode,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Messages  []llm.Message `json:"messages,omitempty"`
}

// Meta 返回不含消息内容的摘要，供列表展示。
func (t *Task) Meta() *Task {
	c := *t
	c.Messages = nil
	return &c
}

// Project 是一个本地文件夹项目。
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	// Deleted 标记项目已被删除（软删除）：登记保留以便同路径重新添加时恢复，
	// 任务归属不动，因此其任务在恢复前处于隐藏状态。
	Deleted bool `json:"deleted,omitempty"`
}

// Store 是基于目录的存储。
type Store struct {
	dataDir string
	mu      sync.Mutex
}

// New 创建 Store；目录在首次写入时按需创建。
func New(dataDir string) *Store { return &Store{dataDir: dataDir} }

func (s *Store) sessionsDir() string  { return filepath.Join(s.dataDir, "sessions") }
func (s *Store) projectsFile() string { return filepath.Join(s.dataDir, "projects.json") }

func newID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "")
	}
	return hex.EncodeToString(b)
}

func validID(id string) bool {
	if len(id) != 12 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// TitleFromText 从首条用户消息生成任务标题。
func TitleFromText(text string) string {
	t := strings.Join(strings.Fields(text), " ")
	if t == "" {
		return "未命名任务"
	}
	if r := []rune(t); len(r) > 40 {
		t = string(r[:40]) + "…"
	}
	return t
}

// CreateTask 创建并落盘一个任务。firstText 用于生成标题。
func (s *Store) CreateTask(projectID, model, mode, firstText string) (*Task, error) {
	now := time.Now()
	t := &Task{
		ID:        newID(),
		Title:     TitleFromText(firstText),
		ProjectID: projectID,
		Model:     model,
		Mode:      mode,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.SaveTask(t); err != nil {
		return nil, err
	}
	return t, nil
}

// SaveTask 写入任务文件并刷新 UpdatedAt。
func (s *Store) SaveTask(t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.UpdatedAt = time.Now()
	if err := os.MkdirAll(s.sessionsDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if !validID(t.ID) {
		return fmt.Errorf("非法任务 ID: %q", t.ID)
	}
	return os.WriteFile(filepath.Join(s.sessionsDir(), t.ID+".json"), data, 0o644)
}

// GetTask 读取完整任务（含消息）。
func (s *Store) GetTask(id string) (*Task, error) {
	if !validID(id) {
		return nil, fmt.Errorf("非法任务 ID: %q", id)
	}
	data, err := os.ReadFile(filepath.Join(s.sessionsDir(), id+".json"))
	if err != nil {
		return nil, err
	}
	t := &Task{}
	if err := json.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("解析任务 %s 失败: %w", id, err)
	}
	return t, nil
}

// DeleteTask 删除任务文件；文件不存在视为成功。
func (s *Store) DeleteTask(id string) error {
	if !validID(id) {
		return fmt.Errorf("非法任务 ID: %q", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(filepath.Join(s.sessionsDir(), id+".json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ListTasks 返回全部任务的摘要，按 UpdatedAt 倒序。
func (s *Store) ListTasks() ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.sessionsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var tasks []*Task
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.sessionsDir(), e.Name()))
		if err != nil {
			continue
		}
		t := &Task{}
		if err := json.Unmarshal(data, t); err != nil {
			continue
		}
		tasks = append(tasks, t.Meta())
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt) })
	return tasks, nil
}

// ListTasksByProject 返回某项目的任务摘要，按 UpdatedAt 倒序。
func (s *Store) ListTasksByProject(projectID string) ([]*Task, error) {
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	var out []*Task
	for _, t := range all {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, nil
}

// loadProjects 读取项目清单（调用方需持锁）。
func (s *Store) loadProjects() ([]*Project, error) {
	data, err := os.ReadFile(s.projectsFile())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var projects []*Project
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, fmt.Errorf("解析 projects.json 失败: %w", err)
	}
	return projects, nil
}

// saveProjects 写入项目清单（调用方需持锁）。
func (s *Store) saveProjects(projects []*Project) error {
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.projectsFile(), data, 0o644)
}

// AddProject 登记一个本地文件夹（已存在同路径时直接返回）。
func (s *Store) AddProject(path string) (*Project, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("路径为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("目录不存在: %s", abs)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	projects, err := s.loadProjects()
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if strings.EqualFold(p.Path, abs) {
			// 同路径的已删除项目：复活原记录（ID 不变，原任务自动恢复归属）
			if p.Deleted {
				p.Deleted = false
				if err := s.saveProjects(projects); err != nil {
					return nil, err
				}
			}
			return p, nil
		}
	}
	p := &Project{ID: newID(), Name: filepath.Base(abs), Path: abs, CreatedAt: time.Now()}
	projects = append(projects, p)
	if err := s.saveProjects(projects); err != nil {
		return nil, err
	}
	return p, nil
}

// ListProjects 返回未删除的项目清单，按添加顺序。
func (s *Store) ListProjects() ([]*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.loadProjects()
	if err != nil {
		return nil, err
	}
	var out []*Project
	for _, p := range all {
		if !p.Deleted {
			out = append(out, p)
		}
	}
	return out, nil
}

// GetProject 按 ID 查找未删除的项目。
func (s *Store) GetProject(id string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projects, err := s.loadProjects()
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.ID == id && !p.Deleted {
			return p, nil
		}
	}
	return nil, fmt.Errorf("项目不存在: %s", id)
}

// DeleteProject 软删除项目登记：保留记录并标记 Deleted，任务归属不动——
// 其任务随之隐藏；同路径再次添加项目时复活原记录，任务自动恢复。
func (s *Store) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	projects, err := s.loadProjects()
	if err != nil {
		return err
	}
	found := false
	for _, p := range projects {
		if p.ID == id {
			found = true
			p.Deleted = true
			break
		}
	}
	if !found {
		return fmt.Errorf("项目不存在: %s", id)
	}
	return s.saveProjects(projects)
}
