package webui

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"omni/internal/config"
	"omni/internal/llm"
)

// ---------- token 用量统计 ----------
//
// 每次问答完成（EventDone 带累计 usage）追加一行到 <dataDir>/usage.jsonl：
//   {"date":"2026-09-30","ts":...,"model":"gpt-4o","input":123,"output":45}
// GET /api/usage 读出并按 (日期, 模型) 聚合返回，时间区间/总量筛选都在前端做
//（文件行数 = 对话次数，个人使用规模下直接全量返回即可）。

type usageRecord struct {
	Date   string `json:"date"` // YYYY-MM-DD（本地时区）
	TS     int64  `json:"ts"`
	Model  string `json:"model"`
	Input  int    `json:"input"`
	Output int    `json:"output"`
}

// recordUsage 尽力落盘一条用量；失败静默（统计不能影响对话）。
func recordUsage(dataDir, model string, u *llm.Usage) {
	if u == nil || dataDir == "" {
		return
	}
	line, err := json.Marshal(usageRecord{
		Date:   time.Now().Format("2006-01-02"),
		TS:     time.Now().Unix(),
		Model:  model,
		Input:  u.InputTokens,
		Output: u.OutputTokens,
	})
	if err != nil {
		return
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "usage.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// handleUsage 返回按 (date, model) 聚合的用量序列，日期升序。
func (s *server) handleUsage(w http.ResponseWriter, r *http.Request) {
	type aggKey struct{ date, model string }
	sums := map[aggKey]*usageRecord{}

	path := filepath.Join(config.DefaultDataDir(), "usage.jsonl")
	if f, err := os.Open(path); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			t := strings.TrimSpace(sc.Text())
			if t == "" {
				continue
			}
			var rec usageRecord
			if json.Unmarshal([]byte(t), &rec) != nil || rec.Date == "" {
				continue
			}
			k := aggKey{rec.Date, rec.Model}
			cur, ok := sums[k]
			if !ok {
				cp := rec
				cp.TS = 0
				cur = &cp
				sums[k] = cur
			}
			cur.Input += rec.Input
			cur.Output += rec.Output
		}
	}

	out := make([]usageRecord, 0, len(sums))
	for _, v := range sums {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Model < out[j].Model
	})
	writeJSON(w, map[string]any{"records": out})
}
