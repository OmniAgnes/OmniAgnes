package webui

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
		Model           string   `json:"model"`
		Type            string   `json:"type"`
		Provider        string   `json:"provider"`
		Default         bool     `json:"default"`
		ContextWindow   int      `json:"context_window,omitempty"`
		MaxOutputTokens int      `json:"max_output_tokens,omitempty"`
		InputTypes      []string `json:"input_types,omitempty"`
		Reasoning       string   `json:"reasoning,omitempty"`
	}
	out := make([]modelInfo, 0)
	for i := range s.cfg.Providers {
		p := &s.cfg.Providers[i]
		for _, m := range p.Models {
			opt := p.OptionsFor(m)
			out = append(out, modelInfo{
				Model: m, Type: p.Type, Provider: p.Name,
				Default:         p.Default && m == p.Model,
				ContextWindow:   opt.ContextWindow,
				MaxOutputTokens: opt.MaxOutputTokens,
				InputTypes:      opt.InputTypes,
				Reasoning:       opt.Reasoning,
			})
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
	// 按"最后对话时间"倒序：项目取其中任务的最新 UpdatedAt，
	// 没有任务的项目回落到自身创建时间——新添加的文件夹因此排在最前。
	tasks, _ := s.store.ListTasks()
	lastActive := map[string]time.Time{}
	for _, t := range tasks {
		if t.ProjectID == "" {
			continue
		}
		if ts, ok := lastActive[t.ProjectID]; !ok || t.UpdatedAt.After(ts) {
			lastActive[t.ProjectID] = t.UpdatedAt
		}
	}
	sort.Slice(projects, func(i, j int) bool {
		ti, ok := lastActive[projects[i].ID]
		if !ok {
			ti = projects[i].CreatedAt
		}
		tj, ok := lastActive[projects[j].ID]
		if !ok {
			tj = projects[j].CreatedAt
		}
		return ti.After(tj)
	})
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

// ---------- 附件 ----------

// sanitizeFileName 只保留文件名本体，替换路径分隔符与 Windows 非法字符。
func sanitizeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || strings.ContainsRune(`:*?"<>|`, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		out = "file"
	}
	return out
}

// handleUpload 保存随消息发送的附件（图片/PDF/Office 文档/音视频等）：
// 落盘到 dataDir/attachments/<task_id>/ 下，返回绝对路径。模型看到的是
// 消息里拼进的这些路径，文件内容靠它自己的工具/MCP 读取处理。
func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	if _, err := hex.DecodeString(taskID); err != nil || len(taskID) != 12 {
		writeErr(w, "非法任务 ID", http.StatusBadRequest)
		return
	}
	// 单次上传总量的硬上限（音视频可能较大）
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, "附件过大或格式不正确（总大小上限 200MB）", http.StatusBadRequest)
		return
	}
	fhs := r.MultipartForm.File["files"]
	if len(fhs) == 0 {
		writeErr(w, "没有收到文件", http.StatusBadRequest)
		return
	}
	dir := filepath.Join(config.DefaultDataDir(), "attachments", taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, "创建附件目录失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	type uploaded struct {
		Path string `json:"path"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	files := make([]uploaded, 0, len(fhs))
	for _, fh := range fhs {
		safe := sanitizeFileName(fh.Filename)
		dst := filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), safe))
		src, err := fh.Open()
		if err != nil {
			writeErr(w, "读取文件失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		out, err := os.Create(dst)
		if err != nil {
			_ = src.Close()
			writeErr(w, "保存文件失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		_, cpErr := io.Copy(out, src)
		_ = src.Close()
		_ = out.Close()
		if cpErr != nil {
			_ = os.Remove(dst)
			writeErr(w, "保存文件失败: "+cpErr.Error(), http.StatusInternalServerError)
			return
		}
		files = append(files, uploaded{Path: dst, Name: safe, Size: fh.Size})
	}
	writeJSON(w, map[string]any{"files": files})
}

// ---------- 聊 ----------

// handleChat 以 SSE 流式返回一轮问答，并把消息持久化到任务。
func (s *server) handleChat(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		TaskID      string   `json:"task_id"`
		Text        string   `json:"text"`
		Mode        string   `json:"mode"`
		Model       string   `json:"model"`
		Attachments []string `json:"attachments"`
	}](r)
	if err != nil {
		writeErr(w, `请求体需为 {"task_id": "...", "text": "..."}`, http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" && len(req.Attachments) == 0 {
		writeErr(w, `请求体需为 {"task_id": "...", "text": "..."}`, http.StatusBadRequest)
		return
	}
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
	// 标题来源：优先正文；只发附件时用首个附件的文件名，避免生成「未命名任务」
	titleSeed := text
	if titleSeed == "" && len(req.Attachments) > 0 {
		titleSeed = filepath.Base(strings.ReplaceAll(req.Attachments[0], "\\", "/"))
	}
	if t.Title == "" || t.Title == "未命名任务" {
		t.Title = store.TitleFromText(titleSeed)
		// 标题立即落盘：整轮可能跑几分钟，不让其他实例（TUI /tasks、另一网页版）一直看到「未命名任务」
		_ = s.store.SaveTask(t)
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
	// ask 工具：向用户提问并阻塞等待回答。
	a.SetAsk(func(callID, question string, options []string) string {
		return s.waitQuestion(r.Context(), send, callID, question, options)
	})
	defer a.SetAsk(nil)

	// 附件：把已上传文件的路径拼进消息（在标题生成之后），模型通过自身工具/MCP
	// 读取这些文件。历史里保留同一段文本，回看时仍能对上是哪几个文件。
	if text == "" {
		text = "请查看并处理这些附件。"
	}
	if len(req.Attachments) > 0 {
		var b strings.Builder
		b.WriteString(text)
		b.WriteString("\n\n[附件]")
		for i, p := range req.Attachments {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			fmt.Fprintf(&b, "\n%d. %s", i+1, p)
		}
		text = b.String()
	}

	// 不用 Run（失败即回滚）：webui 保留失败轮次已产生的内容（至少保留提问），便于回溯
	rollback := a.Prepare(text)
	_ = rollback
	errSent := false // Execute 内部对失败轮已 emit EventError（带 round）；外层不得重复发送
	var lastCk time.Time // 进行中任务的检查点节流：至少间隔 3s 落盘一次
	runErr := a.Execute(r.Context(), func(ev agent.Event) {
		switch ev.Type {
		case agent.EventText:
			send(map[string]any{"type": "text", "text": ev.Text, "round": ev.Round})
		case agent.EventReasoning:
			send(map[string]any{"type": "thinking", "text": ev.Text, "round": ev.Round})
		case agent.EventToolStart:
			send(map[string]any{
				"type": "tool_start", "round": ev.Round,
				"id": ev.Tool.ID, "name": ev.Tool.Name, "args": llm.MarshalToolArgs(ev.Tool.Args),
			})
			// write 会覆盖旧文件：此刻（执行前）抓旧内容快照发给前端渲染差异。
			// 不进模型上下文，纯 UI 数据；超大约定阈值跳过。
			if ev.Tool.Name == "write" && ev.Tool.Args != nil {
				// plan 模式下审批通过才会走到这里执行；build 模式直接执行。
				// 读取失败（新建/越界）时不发送，前端维持"仅内容"视图。
				if old, ok := readBeforeWrite(s.cwdFor(t), ev.Tool); ok {
					send(map[string]any{"type": "write_base", "round": ev.Round, "id": ev.Tool.ID, "old": old})
				}
			}
		case agent.EventToolOutput:
			send(map[string]any{
				"type": "tool_output", "round": ev.Round, "id": ev.Tool.ID,
				"name": ev.Tool.Name, "output": ev.Output, "is_error": ev.IsError,
			})
		case agent.EventToolEnd:
			send(map[string]any{
				"type": "tool_end", "round": ev.Round, "id": ev.Tool.ID,
				"name": ev.Tool.Name, "is_error": ev.IsError,
			})
			// 检查点落盘：任务运行中，其他实例（TUI /tasks、另一网页版）也能看到消息进度
			if time.Since(lastCk) >= 3*time.Second {
				lastCk = time.Now()
				t.Messages = a.History()
				_ = s.store.SaveTask(t)
			}
		case agent.EventApproval:
			send(map[string]any{"type": "approval_pending", "round": ev.Round, "id": ev.Tool.ID})
		case agent.EventQuestion:
			send(map[string]any{"type": "question_pending", "round": ev.Round, "id": ev.Tool.ID})
		case agent.EventError:
			errSent = true
			send(map[string]any{"type": "error", "error": ev.Err.Error(), "round": ev.Round})
		case agent.EventDone:
			// 每次提问的累计 token 用量落盘（usage.jsonl），供设置页统计
			if ev.Usage != nil && (ev.Usage.InputTokens > 0 || ev.Usage.OutputTokens > 0) {
				recordUsage(config.DefaultDataDir(), ev.Model, ev.Usage)
			}
			send(map[string]any{"type": "done", "model": ev.Model, "round": ev.Round})
		}
	})
	if runErr != nil && !errSent {
		// 兜底：Execute 未经事件流报错（如 ctx 取消）时才补发，避免同一错误显示两遍
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
	// 兼容 @组/名 嵌套布局：先按名定位技能实际所在目录
	dir, err = skills.Resolve(dir, name)
	if err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
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
	// 兼容 @组/名 嵌套布局：先按名定位技能实际所在目录
	if dir, err = skills.Resolve(dir, name); err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
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
	// 兼容 @组/名 嵌套布局：先按名定位技能实际所在目录
	if dir, err = skills.Resolve(dir, name); err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
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

// ---------- 目录直达（技能 / MCP 粘贴导入） ----------

// openInFileManager 在系统文件管理器中打开目录或定位文件。
func openInFileManager(path string, selectFile bool) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		if selectFile {
			cmd = exec.Command("explorer", "/select,"+path)
		} else {
			cmd = exec.Command("explorer", path)
		}
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// ensureDir 确保目录存在（不存在则创建），返回绝对路径。
func ensureDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return abs, nil
}

// handleSkillDirs 返回全局/项目技能目录，并确保目录已创建。
// 前端拿到路径后提供「打开目录」按钮，用户直接把 SKILL.md 粘进去即可。
func (s *server) handleSkillDirs(w http.ResponseWriter, r *http.Request) {
	projectDir := r.URL.Query().Get("project_dir")
	if projectDir == "" {
		projectDir = s.opts.StartCwd
	}
	out := map[string]any{}
	for _, source := range []string{"global", "project"} {
		dir, _, err := s.skillDir(r, source)
		if err != nil {
			out[source] = map[string]any{"error": err.Error()}
			continue
		}
		abs, err := ensureDir(dir)
		if err != nil {
			out[source] = map[string]any{"error": err.Error(), "path": dir}
			continue
		}
		out[source] = map[string]any{"path": abs}
	}
	// MCP 配置文件所在目录
	mcpPath := s.opts.ConfigPath
	if mcpPath == "" {
		mcpPath = filepath.Join(config.DefaultDataDir(), "config.json")
	}
	dir, err := ensureDir(filepath.Dir(mcpPath))
	if err != nil {
		out["mcp"] = map[string]any{"error": err.Error(), "path": mcpPath}
	} else {
		out["mcp"] = map[string]any{"path": mcpPath, "dir": dir}
	}
	writeJSON(w, map[string]any{"dirs": out})
}

// handleSkillOpen 在文件管理器中打开技能目录或定位某个 SKILL.md。
func (s *server) handleSkillOpen(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		Source string `json:"source"`
		Name   string `json:"name"`
	}](r)
	if err != nil {
		writeErr(w, `请求体需为 {"source":"global|project", "name":"技能名"}`, http.StatusBadRequest)
		return
	}
	dir, _, err := s.skillDir(r, req.Source)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	abs, err := ensureDir(dir)
	if err != nil {
		writeErr(w, "打开目录失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	target := abs
	if n := strings.TrimSpace(req.Name); n != "" {
		// 兼容 @组/名 嵌套布局：定位技能实际所在目录，直接打开该技能文件夹
		resolved, rerr := skills.Resolve(abs, n)
		if rerr != nil {
			writeErr(w, rerr.Error(), http.StatusNotFound)
			return
		}
		target = filepath.Join(resolved, n)
	}
	if err := openInFileManager(target, false); err != nil {
		writeErr(w, "打开目录失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "path": target})
}

// handleMCPOpen 在文件管理器中打开 MCP 配置文件。
func (s *server) handleMCPOpen(w http.ResponseWriter, r *http.Request) {
	path := s.opts.ConfigPath
	if path == "" {
		path = filepath.Join(config.DefaultDataDir(), "config.json")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(abs); err != nil {
		// 文件还不存在：打开所在目录
		if err := openInFileManager(filepath.Dir(abs), false); err != nil {
			writeErr(w, "打开目录失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "path": abs, "exists": false})
		return
	}
	if err := openInFileManager(abs, true); err != nil {
		writeErr(w, "打开文件失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "path": abs, "exists": true})
}

// ---------- 技能导入（粘贴 SKILL.md 全文） ----------

// handleSkillImport 从粘贴的 Markdown 文本导入技能。
// 自动解析 frontmatter 里的 name/description；缺 name 时用 name 参数或从标题推断。
func (s *server) handleSkillImport(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		Name        string `json:"name"`
		Source      string `json:"source"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}](r)
	if err != nil {
		writeErr(w, `请求体需为 {"content":"SKILL.md 全文", ...}`, http.StatusBadRequest)
		return
	}
	content := strings.ReplaceAll(req.Content, "\r\n", "\n")
	if strings.TrimSpace(content) == "" {
		writeErr(w, "内容为空", http.StatusBadRequest)
		return
	}
	dir, _, err := s.skillDir(r, req.Source)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	abs, err := ensureDir(dir)
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name := strings.TrimSpace(req.Name)
	desc := strings.TrimSpace(req.Description)
	// 文本里已带 frontmatter 就原样落盘（保真用户粘贴的内容）
	if skills.HasFrontmatter(content) {
		n, d, err := skills.Import(abs, content)
		if err != nil {
			writeErr(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "name": n, "description": d, "path": filepath.Join(abs, n, "SKILL.md")})
		return
	}
	// 纯正文：补上 frontmatter 后写入
	if name == "" {
		name = skills.GuessName(content)
	}
	if name == "" {
		writeErr(w, "无法确定技能名，请手动填写", http.StatusBadRequest)
		return
	}
	if desc == "" {
		desc = skills.GuessDescription(content)
	}
	sk, err := skills.Create(abs, name, desc, content)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "skill": sk})
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
	// 最大输出 token 取该模型的有效配置（模型级优先，回落供应商级），缺省 8192
	maxTokens := 8192
	if p := s.cfg.ProviderForModel(modelName); p != nil {
		if n := p.OptionsFor(modelName).MaxOutputTokens; n > 0 {
			maxTokens = n
		}
	}
	reg := s.newRegistry(cwd)
	a := agent.New(prov, reg, agent.Options{
		Model:             modelName,
		Mode:              mode,
		Cwd:               cwd,
		MaxIterations:     500,
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

// readBeforeWrite 在 write 工具执行前读取目标文件的旧内容（供前端渲染差异）。
// 新建文件、越界路径或超过 256KB 的大文件返回 false（跳过差异展示）。
func readBeforeWrite(cwd string, tc *llm.ToolCall) (string, bool) {
	p, _ := tc.Args["path"].(string)
	if p == "" {
		return "", false
	}
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				p = home
			} else {
				p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
			}
		}
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, p)
	}
	abs = filepath.Clean(abs)
	// 与工作目录安全边界保持一致：越界不读
	base := filepath.Clean(cwd)
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() || st.Size() > 256*1024 {
		return "", false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", false
	}
	return string(data), true
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

// ---------- 向用户提问（ask 工具） ----------

// waitQuestion 通过 SSE 推送一条提问，阻塞等待前端答复。
// 10 分钟未答复返回空串（agent 侧转为"用户未回答"提示继续）。
func (s *server) waitQuestion(ctx context.Context, send func(map[string]any), callID, question string, options []string) string {
	req := &questionReq{
		id:       fmt.Sprintf("q-%d", s.approvalSeq.Add(1)),
		question: question,
		options:  options,
		ch:       make(chan string, 1),
	}
	s.mu.Lock()
	if s.questions == nil {
		s.questions = map[string]*questionReq{}
	}
	s.questions[req.id] = req
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.questions, req.id)
		s.mu.Unlock()
	}()

	payload := map[string]any{
		"type":     "question",
		"id":       req.id,
		"call_id":  callID,
		"question": question,
	}
	if len(options) > 0 {
		payload["options"] = options
	}
	send(payload)

	select {
	case ans := <-req.ch:
		send(map[string]any{"type": "question_answered", "id": req.id, "call_id": callID, "answer": ans})
		return ans
	case <-ctx.Done():
		return ""
	case <-time.After(10 * time.Minute):
		return ""
	}
}

// handleQuestionAnswer 接收用户对某次提问的回答。
func (s *server) handleQuestionAnswer(w http.ResponseWriter, r *http.Request) {
	req, err := decode[struct {
		Answer string `json:"answer"`
	}](r)
	if err != nil {
		writeErr(w, `请求体需为 {"answer": "..."}`, http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	qr, ok := s.questions[id]
	s.mu.Unlock()
	if !ok {
		writeErr(w, "提问已失效", http.StatusNotFound)
		return
	}
	select {
	case qr.ch <- req.Answer:
	default:
	}
	writeJSON(w, map[string]any{"ok": true})
}
