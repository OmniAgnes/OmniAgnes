package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ScheduledTask 是计划任务：到点（每 N 天的 HH:MM）自动以指定模型把 Prompt
// 作为一条用户消息执行一次；错过时点（软件未运行）后会在启动/轮询时补执行一次。
type ScheduledTask struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Prompt       string    `json:"prompt"`
	Model        string    `json:"model,omitempty"`
	IntervalDays int       `json:"interval_days"` // 每隔几天执行；1 = 每天
	At           string    `json:"at"`            // 执行时间点 HH:MM
	ProjectID    string    `json:"project_id,omitempty"`
	Enabled      bool      `json:"enabled"`
	LastRun      time.Time `json:"last_run,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Store) schedulesFile() string { return filepath.Join(s.dataDir, "schedules.json") }

func (s *Store) loadSchedules() ([]*ScheduledTask, error) {
	data, err := os.ReadFile(s.schedulesFile())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var sts []*ScheduledTask
	if err := json.Unmarshal(data, &sts); err != nil {
		return nil, fmt.Errorf("解析 schedules.json 失败: %w", err)
	}
	return sts, nil
}

func (s *Store) saveSchedules(sts []*ScheduledTask) error {
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.schedulesFile(), data, 0o644)
}

// ListSchedules 返回全部计划任务（按创建顺序）。
func (s *Store) ListSchedules() ([]*ScheduledTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadSchedules()
}

// CreateSchedule 新建计划任务并落盘（补齐 ID/名称/创建时间）。
func (s *Store) CreateSchedule(st *ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(st.Prompt) == "" {
		return fmt.Errorf("执行内容不能为空")
	}
	st.ID = newID()
	if st.IntervalDays < 1 {
		st.IntervalDays = 1
	}
	if strings.TrimSpace(st.Name) == "" {
		st.Name = TitleFromText(st.Prompt)
	}
	st.CreatedAt = time.Now()
	sts, err := s.loadSchedules()
	if err != nil {
		return err
	}
	return s.saveSchedules(append(sts, st))
}

// UpdateSchedule 保存对计划任务的修改（启停/编辑/记录执行时间）。
func (s *Store) UpdateSchedule(st *ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sts, err := s.loadSchedules()
	if err != nil {
		return err
	}
	for i, e := range sts {
		if e.ID == st.ID {
			sts[i] = st
			return s.saveSchedules(sts)
		}
	}
	return fmt.Errorf("计划任务不存在: %s", st.ID)
}

// DeleteSchedule 删除计划任务。
func (s *Store) DeleteSchedule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sts, err := s.loadSchedules()
	if err != nil {
		return err
	}
	var kept []*ScheduledTask
	found := false
	for _, e := range sts {
		if e.ID == id {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if !found {
		return fmt.Errorf("计划任务不存在: %s", id)
	}
	return s.saveSchedules(kept)
}

// ScheduleStatus 计划任务的到点状态：due = 现在应执行，next = 下一次执行时间。
// 规则：从锚点日（上次执行日；从未执行则创建日）起每 interval 天的 At 时点为一个执行位；
// 严格晚于基准（上次执行时间/创建时间）的第一个执行位已过期即为错过——
// 错过在 24 小时内则补执行一次，超过 24 小时的不再补，直接跳到下一个未来执行位。
func ScheduleStatus(st *ScheduledTask, now time.Time) (due bool, next time.Time) {
	h, m := parseAt(st.At)
	interval := st.IntervalDays
	if interval < 1 {
		interval = 1
	}
	anchor, floor := st.LastRun, st.LastRun
	if anchor.IsZero() {
		anchor, floor = st.CreatedAt, st.CreatedAt
	}
	cand := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), h, m, 0, 0, anchor.Location())
	// 锚点日及其前的执行位已被消耗（= LastRun 本身或创建前不存在），跳过
	for !cand.After(floor) {
		cand = cand.AddDate(0, 0, interval)
	}
	if cand.After(now) {
		return false, cand
	}
	// 第一个待执行位已过期：24 小时内补执行；更早的错过不补
	if now.Sub(cand) <= 24*time.Hour {
		next = cand
		for !next.After(now) {
			next = next.AddDate(0, 0, interval)
		}
		return true, next
	}
	for !cand.After(now) {
		cand = cand.AddDate(0, 0, interval)
	}
	return false, cand
}

func parseAt(s string) (int, int) {
	h, m := 9, 0
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) == 2 {
		if v, err := strconv.Atoi(parts[0]); err == nil && v >= 0 && v <= 23 {
			h = v
		}
		if v, err := strconv.Atoi(parts[1]); err == nil && v >= 0 && v <= 59 {
			m = v
		}
	}
	return h, m
}
