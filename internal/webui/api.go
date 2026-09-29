package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"omni/internal/agent"
	"omni/internal/config"
	"omni/internal/llm"
	"omni/internal/mcp"
	"omni/internal/memory"
	"omni/internal/skills"
	"omni/internal/store"
	"omni/internal/tools"
)

// ---------- 状态与模型 ----------

func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	model := ""
	configured := s.provider != nil
	if configured {
		if ms := s.provider.Models(); len(ms) > 0 {
			model = ms[0]
		}
	} else if _, m, err := llm.ResolveProvider(s.cfg); err == nil {
		configured = true
		model = m
	}
	writeJSON(w, map[string]any{
		"model":       model,
		"theme":       s.cfg.Theme,
		"theme_color": s.cfg.ThemeColor,
		"configured":  configured,
		"version":     s.opts.Version,
		"start_cwd":   s.opts.StartCwd,
	})
}

func (s *server) handleModels(w http.ResponseWriter, r *http.Request) {
	type modelInfo struct {
		Model     string `json:"model"`
		Type      string `json:"type"`
		Provider  string `json:"provider"`
		Default   bool   `json:"default"`
	}
	out := make([]modelInfo, 0)
	for _, p := range s.cfg.Providers {
		for _, m := range p.Models {
			out = append(out, modelInfo{Model: m, Type: p.Type, Provider: p.Name, Default: p.Default && m == p.Model})
		}
	}
	writeJSON(w, map[string]any{"models": out, "current": s.cfg.Provider})
}

// ---------- 供应商 ----------

func (s *server) handleProvidersGet(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Providers == nil {
		s.cfg.Providers = []config.ProviderConfig{}
	}
	writeJSON(w, map[string]any{"providers": s.cfg.Providers})
}

// handleProvidersPut 整体替换 providers 列表（前端负责增删改后提交）。
func (s *server) handleProvidersPut(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		Providers []config.ProviderConfig `json:"providers"`
	}](r)
	if err != nil {
		writeErr(w, "请求体需为 {\"providers\": [...]}", http.StatusBadRequest)
		return
	}
	providers := req.Providers
	if providers == nil {
		providers = []config.ProviderConfig{}
	}
	for i := range providers {
		p := &providers[i]
		p.Type = strings.TrimSpace(p.Type)
		if p.Type == "" {
			writeErr(w, fmt.Sprintf("第 %d 个供应商缺少类型", i+1), http.StatusBadRequest)
			return
		}
		p.Normalize()
		if len(p.Models) == 0 {
			writeErr(w, fmt.Sprintf("供应商 %s 至少需要一个模型", providerLabel(p, i)), http.StatusBadRequest)
			return
		}
	}
	s.cfg.Providers = providers
	// 当前模型失效时回落到第一个可用模型
	valid := false
	for _, p := range providers {
		if p.HasModel(s.cfg.Provider) {
			valid = true
			break
		}
	}
	if !valid {
		if len(providers) > 0 {
			s.cfg.Provider = providers[0].Model
		} else {
			s.cfg.Provider = ""
		}
	}
	if err := s.cfg.Save(s.opts.ConfigPath); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"providers": s.cfg.Providers, "current": s.cfg.Provider})
}

func providerLabel(p *config.ProviderConfig, i int) string {
	if p.Name != "" {
		return p.Name
	}
	return fmt.Sprintf("#%d", i+1)
}

// ---------- 项目 ----------

func (s *server) handleProjectsList(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.ListProjects()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"projects": projects})
}

func (s *server) handleProjectsAdd(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		Path string `json:"path"`
	}](r)
	if err != nil || strings.TrimSpace(req.Path) == "" {
		writeErr(w, "请求体需为 {\"path\": \"...\"}", http.StatusBadRequest)
		return
	}
	p, err := s.store.AddProject(req.Path)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"project": p})
}

func (s *server) handleProjectDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteProject(r.PathValue("id")); err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleProjectOpen 在系统文件管理器中打开项目文件夹。
func (s *server) handleProjectOpen(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetProject(r.PathValue("id"))
	if err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
	if st, err := os.Stat(p.Path); err != nil || !st.IsDir() {
		writeErr(w, "文件夹不存在: "+p.Path, http.StatusBadRequest)
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", p.Path) // explorer 成功也返回非 0，用 Start 不等待
	case "darwin":
		cmd = exec.Command("open", p.Path)
	default:
		cmd = exec.Command("xdg-open", p.Path)
	}
	if err := cmd.Start(); err != nil {
		writeErr(w, "打开文件夹失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// browseEntry 是目录浏览的一项。
type browseEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

func (s *server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = s.opts.StartCwd
	}
	if path == "" {
		home, _ := os.UserHomeDir()
		path = home
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		writeErr(w, "目录不存在: "+abs, http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := make([]browseEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, browseEntry{Name: name, Path: filepath.Join(abs, name), IsDir: true})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	parent := filepath.Dir(abs)
	if parent == abs {
		parent = ""
	}
	writeJSON(w, map[string]any{"path": abs, "parent": parent, "entries": out})
}

// ---------- 任务 ----------

func (s *server) handleTasksList(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	var (
		tasks []*store.Task
		err   error
	)
	switch project {
	case "", "all":
		tasks, err = s.store.ListTasks()
	default:
		tasks, err = s.store.ListTasksByProject(project)
	}
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"tasks": tasks})
}

func (s *server) handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		ProjectID string `json:"project_id"`
	}](r)
	if err != nil {
		req.ProjectID = ""
	}
	_, model, _ := llm.ResolveProvider(s.cfg)
	t, err := s.store.CreateTask(req.ProjectID, model, string(agent.ModeBuild), "")
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"task": t})
}

func (s *server) handleTaskGet(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTask(r.PathValue("id"))
	if err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"task": t})
}

func (s *server) handleTaskDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteTask(id); err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
	s.mu.Lock()
	delete(s.agents, id)
	delete(s.regs, id)
	s.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- 聊 ----------

// handleChat 以 SSE 流式返回一轮问答，并把消息持久化到任务。
func (s *server) handleChat(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		TaskID string `json:"task_id"`
		Text   string `json:"text"`
		Mode   string `json:"mode"`
		Model  string `json:"model"`
	}](r)
	if err != nil || strings.TrimSpace(req.Text) == "" {
		writeErr(w, `请求体需为 {"task_id": "...", "text": "..."}`, http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(req.Text)
	t, err := s.store.GetTask(req.TaskID)
	if err != nil {
		writeErr(w, "任务不存在", http.StatusNotFound)
		return
	}
	// /技能名 展开
	if expanded, ok := s.opts.Extras.ExpandSlash(s.cwdFor(t), text); ok {
		text = expanded
	}
	a, err := s.agentFor(t)
	if err != nil {
		writeErr(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if req.Mode == "plan" || req.Mode == "build" {
		a.SetMode(agent.Mode(req.Mode))
		t.Mode = req.Mode
	}
	// 每轮使用界面记住的模型（设置里已持久化）
	if req.Model != "" && s.cfg.ProviderForModel(req.Model) != nil {
		a.SetModel(req.Model)
		t.Model = req.Model
	}
	if t.Title == "" || t.Title == "未命名任务" {
		t.Title = store.TitleFromText(req.Text)
	}

	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, "当前连接不支持流式输出", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")

	send := func(payload map[string]any) {
		b, _ := json.Marshal(payload)
		_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
		fl.Flush()
	}
	send(map[string]any{"type": "start", "task_id": t.ID, "title": t.Title, "mode": string(a.Mode())})

	// 普通访问模式：写操作通过 SSE 推给前端确认，这里阻塞等待用户答复。
	a.SetApprove(func(callID, name string, args map[string]any) bool {
		return s.waitApproval(r.Context(), send, callID, name, args)
	})
	defer a.SetApprove(nil)

	// 不用 Run（失败即回滚）：webui 保留失败轮次已产生的内容（至少保留提问），便于回溯
	rollback := a.Prepare(text)
	_ = rollback
	runErr := a.Execute(r.Context(), func(ev agent.Event) {
		switch ev.Type {
		case agent.EventText:
			send(map[string]any{"type": "text", "text": ev.Text})
		case agent.EventToolStart:
			send(map[string]any{"type": "tool_start", "name": ev.Tool.Name, "args": llm.MarshalToolArgs(ev.Tool.Args)})
		case agent.EventToolOutput:
			send(map[string]any{"type": "tool_output", "name": ev.Tool.Name, "output": ev.Output, "is_error": ev.IsError})
		case agent.EventToolEnd:
			send(map[string]any{"type": "tool_end", "name": ev.Tool.Name, "is_error": ev.IsError})
		case agent.EventApproval:
			send(map[string]any{"type": "approval_pending", "name": ev.Tool.Name})
		case agent.EventError:
			send(map[string]any{"type": "error", "error": ev.Err.Error()})
		case agent.EventDone:
			send(map[string]any{"type": "done", "model": ev.Model})
		}
	})
	if runErr != nil {
		send(map[string]any{"type": "error", "error": runErr.Error()})
	}
	// 持久化消息（无论成功失败都保存已有内容）
	t.Messages = a.History()
	if len(t.Messages) == 0 && runErr != nil {
		// 首轮即失败且没有任何内容：移除这个空任务，避免列表里出现打不开的任务
		_ = s.store.DeleteTask(t.ID)
		s.mu.Lock()
		delete(s.agents, t.ID)
		s.mu.Unlock()
		send(map[string]any{"type": "task_removed"})
		return
	}
	_ = s.store.SaveTask(t)
}

// ---------- 设置 ----------

func (s *server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"model":       s.cfg.Provider,
		"theme":       s.cfg.Theme,
		"theme_color": s.cfg.ThemeColor,
	})
}

func (s *server) handleSettingsSet(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		Model      string `json:"model"`
		Theme      string `json:"theme"`
		ThemeColor string `json:"theme_color"`
	}](r)
	if err != nil {
		writeErr(w, `请求体需为 {"model":..., "theme":..., "theme_color":...}`, http.StatusBadRequest)
		return
	}
	changed := false
	if req.Model != "" {
		found := false
		for _, p := range s.cfg.Providers {
			if p.HasModel(req.Model) {
				found = true
				break
			}
		}
		if !found {
			writeErr(w, "未知模型: "+req.Model, http.StatusBadRequest)
			return
		}
		s.cfg.Provider = req.Model
		changed = true
	}
	if req.Theme != "" {
		if req.Theme != "dark" && req.Theme != "light" && req.Theme != "system" {
			writeErr(w, "主题需为 dark/light/system", http.StatusBadRequest)
			return
		}
		s.cfg.Theme = req.Theme
		changed = true
	}
	switch req.ThemeColor {
	case "":
	case "zinc", "violet", "blue", "emerald", "amber", "rose":
		s.cfg.ThemeColor = req.ThemeColor
		changed = true
	default:
		writeErr(w, "未知主题配色: "+req.ThemeColor, http.StatusBadRequest)
		return
	}
	if changed {
		if err := s.cfg.Save(s.opts.ConfigPath); err != nil {
			writeErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]any{"model": s.cfg.Provider, "theme": s.cfg.Theme, "theme_color": s.cfg.ThemeColor})
}

func (s *server) handleMemoryGet(w http.ResponseWriter, r *http.Request) {
	path := ""
	if s.opts.Extras != nil {
		path = s.opts.Extras.MemoryPath
	}
	writeJSON(w, map[string]any{"path": path, "content": memory.Load(path)})
}

func (s *server) handleMemorySet(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		Content string `json:"content"`
	}](r)
	if err != nil || s.opts.Extras == nil || s.opts.Extras.MemoryPath == "" {
		writeErr(w, "全局记忆不可用", http.StatusBadRequest)
		return
	}
	if err := memory.Replace(s.opts.Extras.MemoryPath, req.Content); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- MCP / Skills ----------

func (s *server) handleMCP(w http.ResponseWriter, r *http.Request) {
	var statuses []any
	if s.opts.Extras != nil && s.opts.Extras.MCP != nil {
		ss := s.opts.Extras.MCP.Statuses()
		for _, st := range ss {
			statuses = append(statuses, st)
		}
	}
	writeJSON(w, map[string]any{"servers": statuses})
}

// skillDir 按 source（global|project）与 project_dir 解析技能目录。
func (s *server) skillDir(r *http.Request, source string) (string, string, error) {
	if s.opts.Extras == nil || s.opts.Extras.Skills == nil {
		return "", "", fmt.Errorf("技能功能不可用")
	}
	projectDir := r.URL.Query().Get("project_dir")
	if projectDir == "" {
		projectDir = s.opts.StartCwd
	}
	dir, err := s.opts.Extras.Skills.DirFor(source, projectDir)
	if err != nil {
		return "", "", err
	}
	return dir, projectDir, nil
}

// handleSkillGet 返回技能原始 SKILL.md 内容，供编辑。
func (s *server) handleSkillGet(w http.ResponseWriter, r *http.Request) {
	dir, projectDir, err := s.skillDir(r, r.URL.Query().Get("source"))
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimPrefix(r.PathValue("name"), "/")
	content, err := skills.Read(dir, name)
	if err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
	desc := ""
	if sk, ok := s.opts.Extras.Skills.Lookup(projectDir, name); ok {
		desc = sk.Description
	}
	writeJSON(w, map[string]any{"skill": map[string]any{
		"name":        name,
		"description": desc,
		"content":     content,
		"path":        filepath.Join(dir, name, "SKILL.md"),
	}})
}

// handleSkillCreate 新建技能。
func (s *server) handleSkillCreate(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
		Source      string `json:"source"`
	}](r)
	if err != nil {
		writeErr(w, `请求体需为 {"name":..., "description":..., "content":...}`, http.StatusBadRequest)
		return
	}
	dir, _, err := s.skillDir(r, req.Source)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	sk, err := skills.Create(dir, req.Name, req.Description, req.Content)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"skill": sk})
}

// handleSkillUpdate 覆盖技能描述与正文（技能名与目录不变）。
func (s *server) handleSkillUpdate(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		Description string `json:"description"`
		Content     string `json:"content"`
	}](r)
	if err != nil {
		writeErr(w, `请求体需为 {"description":..., "content":...}`, http.StatusBadRequest)
		return
	}
	dir, _, err := s.skillDir(r, r.URL.Query().Get("source"))
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimPrefix(r.PathValue("name"), "/")
	if err := skills.Update(dir, name, req.Description, req.Content); err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleSkillDelete 删除技能目录及其文件。
func (s *server) handleSkillDelete(w http.ResponseWriter, r *http.Request) {
	dir, _, err := s.skillDir(r, r.URL.Query().Get("source"))
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimPrefix(r.PathValue("name"), "/")
	if err := skills.Delete(dir, name); err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleMCPConfigGet 返回 MCP 服务器的原始配置（供设置界面编辑）。
func (s *server) handleMCPConfigGet(w http.ResponseWriter, r *http.Request) {
	servers := s.cfg.MCPServers
	if servers == nil {
		servers = map[string]config.MCPServerConfig{}
	}
	writeJSON(w, map[string]any{"servers": servers})
}

// handleMCPConfigPut 整体替换 MCP 服务器配置并重连。
func (s *server) handleMCPConfigPut(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		Servers map[string]config.MCPServerConfig `json:"servers"`
	}](r)
	if err != nil {
		writeErr(w, `请求体需为 {"servers": {...}}`, http.StatusBadRequest)
		return
	}
	servers := map[string]config.MCPServerConfig{}
	for name, cfg := range req.Servers {
		name = strings.TrimSpace(name)
		if name == "" {
			writeErr(w, "MCP 服务器名称不能为空", http.StatusBadRequest)
			return
		}
		cfg.Command = strings.TrimSpace(cfg.Command)
		cfg.URL = strings.TrimSpace(cfg.URL)
		if cfg.Command == "" && cfg.URL == "" {
			writeErr(w, fmt.Sprintf("MCP 服务器 %s 需要填写启动命令或 URL", name), http.StatusBadRequest)
			return
		}
		if cfg.URL != "" {
			// HTTP 模式只用 url，忽略 command/args
			cfg.Command, cfg.Args = "", nil
		}
		servers[name] = cfg
	}
	s.cfg.MCPServers = servers
	if err := s.cfg.Save(s.opts.ConfigPath); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 重连耗时较长，放后台执行，随后把新工具刷进所有在线任务
	if mgr := s.mcpManager(); mgr != nil {
		go func() {
			mgr.Reset()
			mgr.Start(context.Background(), servers)
			s.refreshMCPTools()
		}()
	}
	writeJSON(w, map[string]any{"servers": servers})
}

func (s *server) mcpManager() *mcp.Manager {
	if s.opts.Extras == nil {
		return nil
	}
	return s.opts.Extras.MCP
}

// refreshMCPTools 把最新 MCP 工具重新注册进所有在线任务的注册表。
func (s *server) refreshMCPTools() {
	mgr := s.mcpManager()
	if mgr == nil {
		return
	}
	s.mu.Lock()
	regs := make([]*tools.Registry, 0, len(s.regs))
	for _, r := range s.regs {
		regs = append(regs, r)
	}
	s.mu.Unlock()
	for _, r := range regs {
		mgr.RegisterInto(r)
	}
}

func (s *server) handleSkills(w http.ResponseWriter, r *http.Request) {
	var skills []any
	if s.opts.Extras != nil && s.opts.Extras.Skills != nil {
		projectDir := r.URL.Query().Get("project_dir")
		if projectDir == "" {
			projectDir = s.opts.StartCwd
		}
		for _, sk := range s.opts.Extras.Skills.For(projectDir) {
			skills = append(skills, sk)
		}
	}
	writeJSON(w, map[string]any{"skills": skills})
}

// ---------- Agent 池 ----------

// cwdFor 返回任务的工作目录：有项目用项目路径，否则用启动目录。
func (s *server) cwdFor(t *store.Task) string {
	if t.ProjectID != "" {
		if p, err := s.store.GetProject(t.ProjectID); err == nil {
			return p.Path
		}
	}
	return s.opts.StartCwd
}

// agentFor 取任务对应的 Agent，没有则创建（恢复历史、注册扩展工具）。
func (s *server) agentFor(t *store.Task) (*agent.Agent, error) {
	s.mu.Lock()
	if a, ok := s.agents[t.ID]; ok {
		s.mu.Unlock()
		return a, nil
	}
	s.mu.Unlock()

	prov, modelName := s.provider, ""
	if prov != nil {
		if ms := prov.Models(); len(ms) > 0 {
			modelName = ms[0]
		}
	} else {
		var err error
		prov, modelName, err = llm.ResolveProvider(s.cfg)
		if err != nil {
			return nil, err
		}
	}
	cwd := s.cwdFor(t)
	mode := agent.ModeBuild
	if t.Mode == string(agent.ModePlan) {
		mode = agent.ModePlan
	}
	if modelName == "" {
		modelName = t.Model
	}
	// 最大输出 token 取供应商配置，缺省 8192
	maxTokens := 8192
	if p := s.cfg.ProviderForModel(modelName); p != nil && p.MaxOutputTokens > 0 {
		maxTokens = p.MaxOutputTokens
	}
	reg := s.newRegistry(cwd)
	a := agent.New(prov, reg, agent.Options{
		Model:             modelName,
		Mode:              mode,
		Cwd:               cwd,
		MaxIterations:     32,
		MaxTokens:         maxTokens,
		SystemPromptExtra: s.cfg.SystemPromptExtra,
		// 恢复持久化的历史消息：重启后继续对话时模型仍记得之前的内容
		History: t.Messages,
		ExtraPrompt: func() string {
			return s.opts.Extras.ExtraPrompt(cwd)
		},
	})
	s.mu.Lock()
	// 双检：并发创建时复用先到者
	if existing, ok := s.agents[t.ID]; ok {
		s.mu.Unlock()
		return existing, nil
	}
	s.agents[t.ID] = a
	if s.regs == nil {
		s.regs = map[string]*tools.Registry{}
	}
	s.regs[t.ID] = reg
	s.mu.Unlock()
	return a, nil
}

// newRegistry 为任务创建带扩展工具的工具注册表。
func (s *server) newRegistry(cwd string) *tools.Registry {
	env := &tools.Env{
		Cwd:       cwd,
		MaxOutput: s.cfg.MaxToolOutput,
		Timeout:   time.Duration(s.cfg.BashTimeoutSec) * time.Second,
	}
	reg := tools.NewRegistry(env)
	s.opts.Extras.RegisterTools(reg, cwd)
	return reg
}

// ---------- 写操作确认（普通访问模式） ----------

// waitApproval 通过 SSE 推送一条待确认事件，并阻塞等待前端答复。
// 3 分钟内未答复视为拒绝，避免请求永久挂起。
func (s *server) waitApproval(ctx context.Context, send func(map[string]any), callID, name string, args map[string]any) bool {
	req := &approvalReq{
		id:   fmt.Sprintf("ap-%d", s.approvalSeq.Add(1)),
		name: name,
		args: args,
		ch:   make(chan bool, 1),
	}
	s.mu.Lock()
	if s.approvals == nil {
		s.approvals = map[string]*approvalReq{}
	}
	s.approvals[req.id] = req
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.approvals, req.id)
		s.mu.Unlock()
	}()

	send(map[string]any{
		"type":    "approval",
		"id":      req.id,
		"call_id": callID,
		"name":    name,
		"args":    llm.MarshalToolArgs(args),
	})

	select {
	case ok := <-req.ch:
		send(map[string]any{"type": "approval_result", "id": req.id, "call_id": callID, "allowed": ok})
		return ok
	case <-ctx.Done():
		return false
	case <-time.After(3 * time.Minute):
		return false
	}
}

// handleApproval 接收前端对某次写操作的确认结果。
func (s *server) handleApproval(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		Allow bool `json:"allow"`
	}](r)
	if err != nil {
		writeErr(w, `请求体需为 {"allow": true|false}`, http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	ar, ok := s.approvals[id]
	s.mu.Unlock()
	if !ok {
		writeErr(w, "确认请求已失效", http.StatusNotFound)
		return
	}
	select {
	case ar.ch <- req.Allow:
	default:
	}
	writeJSON(w, map[string]any{"ok": true})
}
