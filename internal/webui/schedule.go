package webui

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"omni/internal/agent"
	"omni/internal/config"
	"omni/internal/llm"
	"omni/internal/store"
)

// schedRunning 防止同一计划任务并发重复执行（执行可能远长于轮询间隔）。
var schedRunning sync.Map

// startScheduler 启动计划任务轮询：启动即检查一次（补执行软件未运行期间错过的），
// 之后每 30 秒检查一次到点任务。
func (s *server) startScheduler() {
	go func() {
		s.runDueSchedules()
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			s.runDueSchedules()
		}
	}()
}

func (s *server) runDueSchedules() {
	sts, err := s.store.ListSchedules()
	if err != nil {
		return
	}
	now := time.Now()
	for _, st := range sts {
		if !st.Enabled {
			continue
		}
		due, _ := store.ScheduleStatus(st, now)
		if !due {
			continue
		}
		if _, loaded := schedRunning.LoadOrStore(st.ID, struct{}{}); loaded {
			continue
		}
		go func(st *store.ScheduledTask) {
			defer schedRunning.Delete(st.ID)
			s.runScheduledTask(st)
		}(st)
	}
}

// runScheduledTask 执行一次计划任务：新建任务 → 以指定模型跑一轮 → 持久化并记录执行时间。
// 与交互对话的差异：无 SSE 输出、完全访问模式、无审批/提问回调（ask 按合理假设继续）。
func (s *server) runScheduledTask(st *store.ScheduledTask) {
	t, err := s.store.CreateTask(st.ProjectID, st.Model, string(agent.ModeBuild), st.Prompt)
	if err != nil {
		log.Printf("[schedule] 创建任务失败: %v", err)
		return
	}
	// 标记运行中：任务列表据此显示「处理中」（前端实时状态追不到后台执行）
	s.runningTasks.Store(t.ID, struct{}{})
	defer s.runningTasks.Delete(t.ID)
	// 新任务已建好（可能还在跑）：通知前端刷新列表，侧栏立即能看到
	s.hub.broadcast()
	a, err := s.agentForOpts(t, false)
	if err != nil {
		log.Printf("[schedule] 创建 Agent 失败: %v", err)
		return
	}
	// 指定模型：模型可能属于另一个 provider，须连 provider 一起切
	if st.Model != "" {
		if p := s.cfg.ProviderForModel(st.Model); p != nil {
			if prov, err := llm.New(llm.ConfigP{Type: p.Type, Model: p.Model, BaseURL: p.BaseURL, APIKey: p.ResolveAPIKey()}); err == nil {
				a.SetProvider(prov, st.Model)
				t.Model = st.Model
			}
		}
	}
	rollback := a.Prepare(st.Prompt)
	_ = rollback
	// 立即落盘用户 prompt：后台执行的首个检查点要等第一轮工具结束，
	// 不先写的话执行期间点开任务会是空视图（显示成「新建任务」占位）
	t.Messages = a.History()
	_ = s.store.SaveTask(t)
	var lastCk time.Time
	_ = a.Execute(context.Background(), func(ev agent.Event) {
		switch ev.Type {
		case agent.EventToolEnd:
			// 检查点落盘：任务运行中，其他实例（TUI /tasks、另一网页版）也能看到进度
			if time.Since(lastCk) >= 3*time.Second {
				lastCk = time.Now()
				t.Messages = a.History()
				_ = s.store.SaveTask(t)
			}
		case agent.EventDone:
			if ev.Usage != nil && (ev.Usage.InputTokens > 0 || ev.Usage.OutputTokens > 0) {
				recordUsage(config.DefaultDataDir(), ev.Model, ev.Usage)
			}
		}
	})
	t.Messages = a.History()
	if len(t.Messages) == 0 {
		// 一条消息都没产生（首轮即失败）：删掉空任务；不记 LastRun，下个轮询再试
		_ = s.store.DeleteTask(t.ID)
		s.mu.Lock()
		delete(s.agents, t.ID)
		s.mu.Unlock()
		return
	}
	_ = s.store.SaveTask(t)
	st.LastRun = time.Now()
	_ = s.store.UpdateSchedule(st)
	// 本轮落盘完成：再广播一次，刷新列表里的更新时间排序
	s.hub.broadcast()
	log.Printf("[schedule] 计划任务 %q 已执行", st.Name)
}

// ---------- 计划任务 API ----------

type schedulePayload struct {
	Name         string `json:"name"`
	Prompt       string `json:"prompt"`
	Model        string `json:"model"`
	IntervalDays int    `json:"interval_days"`
	At           string `json:"at"`
	ProjectID    string `json:"project_id"`
	Enabled      *bool  `json:"enabled"`
}

func validAt(s string) bool {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	return err1 == nil && err2 == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59
}

func (s *server) handleSchedulesList(w http.ResponseWriter, r *http.Request) {
	sts, err := s.store.ListSchedules()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := time.Now()
	type info struct {
		*store.ScheduledTask
		NextRun time.Time `json:"next_run"`
	}
	out := make([]info, 0, len(sts))
	for _, st := range sts {
		_, next := store.ScheduleStatus(st, now)
		out = append(out, info{ScheduledTask: st, NextRun: next})
	}
	writeJSON(w, map[string]any{"schedules": out})
}

func (s *server) handleScheduleCreate(w http.ResponseWriter, r *http.Request) {
	req, err := decode[schedulePayload](r)
	if err != nil || strings.TrimSpace(req.Prompt) == "" {
		writeErr(w, `请求体需为 {"prompt": "...", "interval_days": 1, "at": "09:00"}`, http.StatusBadRequest)
		return
	}
	if !validAt(req.At) {
		writeErr(w, "时间格式需为 HH:MM", http.StatusBadRequest)
		return
	}
	iv := req.IntervalDays
	if iv < 1 {
		iv = 1
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	st := &store.ScheduledTask{
		Name:         req.Name,
		Prompt:       req.Prompt,
		Model:        req.Model,
		IntervalDays: iv,
		At:           req.At,
		ProjectID:    req.ProjectID,
		Enabled:      enabled,
	}
	if err := s.store.CreateSchedule(st); err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"schedule": st})
}

func (s *server) handleScheduleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sts, err := s.store.ListSchedules()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var st *store.ScheduledTask
	for _, e := range sts {
		if e.ID == id {
			st = e
			break
		}
	}
	if st == nil {
		writeErr(w, "计划任务不存在", http.StatusNotFound)
		return
	}
	req, err := decode[schedulePayload](r)
	if err != nil {
		writeErr(w, "请求体解析失败", http.StatusBadRequest)
		return
	}
	if req.Enabled != nil {
		st.Enabled = *req.Enabled
	}
	if strings.TrimSpace(req.Prompt) != "" {
		st.Prompt = req.Prompt
	}
	if req.Name != "" {
		st.Name = req.Name
	}
	st.Model = req.Model
	st.ProjectID = req.ProjectID
	if req.IntervalDays >= 1 {
		st.IntervalDays = req.IntervalDays
	}
	if validAt(req.At) {
		st.At = req.At
	}
	if err := s.store.UpdateSchedule(st); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"schedule": st})
}

func (s *server) handleScheduleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSchedule(r.PathValue("id")); err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleScheduleRun 手动触发一次计划任务（立即执行，不影响原有周期）。
func (s *server) handleScheduleRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sts, err := s.store.ListSchedules()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var st *store.ScheduledTask
	for _, e := range sts {
		if e.ID == id {
			st = e
			break
		}
	}
	if st == nil {
		writeErr(w, "计划任务不存在", http.StatusNotFound)
		return
	}
	if _, loaded := schedRunning.LoadOrStore(id, struct{}{}); loaded {
		writeErr(w, "该计划正在执行中，请稍后再试", http.StatusConflict)
		return
	}
	go func() {
		defer schedRunning.Delete(id)
		s.runScheduledTask(st)
	}()
	writeJSON(w, map[string]any{"ok": true})
}
