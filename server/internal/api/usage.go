package api

import (
	"net/http"
	"strconv"

	"omni-server/internal/store"
)

// pageReq 从 query 解析分页参数：page 从 1 起，size 默认 20、上限 100。
func pageReq(r *http.Request) (page, size int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ = strconv.Atoi(r.URL.Query().Get("page_size"))
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return
}

// handleUsageGet 会员使用量页数据：请求汇总 / 积分累计获得与消耗 + 按 list 分页的记录。
// list=ai(默认) AI 请求记录 | spent 消耗记录 | got 获取记录。
func (s *Server) handleUsageGet(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	// 读路径先清扫过期积分，保证展示的余额已剔除失效部分
	_ = s.store.SweepExpired(u.ID)
	requests, in, out, credits, err := s.store.UsageSummary(u.ID)
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	got, spent, err := s.store.CreditSums(u.ID)
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	list := r.URL.Query().Get("list")
	if list == "" {
		list = "ai"
	}
	page, size := pageReq(r)
	resp := map[string]any{
		"summary": map[string]any{
			"requests":      requests,
			"input_tokens":  in,
			"output_tokens": out,
			"credits":       credits,
		},
		"credit":    map[string]any{"got": got, "spent": spent},
		"list":      list,
		"page":      page,
		"page_size": size,
	}
	switch list {
	case "spent", "got":
		rows, total, err := s.store.CreditRecordsPage(u.ID, list, page, size)
		if err != nil {
			writeErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp["items"], resp["total"] = rows, total
	default:
		rows, total, err := s.store.UsageLogsPage(u.ID, page, size)
		if err != nil {
			writeErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp["items"], resp["total"] = rows, total
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleUsagePost 桌面端上报一次 AI 请求：按模型倍率扣积分并留痕。
// 余额不足返回 402（记录仍以 insufficient 落库）。
func (s *Server) handleUsagePost(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	var req struct {
		Model        string `json:"model"`
		InputTokens  int64  `json:"input_tokens"`
		OutputTokens int64  `json:"output_tokens"`
		Status       string `json:"status"`
		Error        string `json:"error"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, "参数错误", http.StatusBadRequest)
		return
	}
	if req.Status == "" {
		req.Status = "ok"
	}
	credits, balance, err := s.store.ReportUsage(u.ID, req.Model, req.InputTokens, req.OutputTokens, req.Status, req.Error)
	if err == store.ErrInsufficient {
		writeErr(w, "积分不足", http.StatusPaymentRequired)
		return
	}
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credits": credits, "balance": balance})
}

// handleAdminUsage 管理端使用量：总览 + 按 list 分页的全体记录。
// list=ai(默认) 请求记录（phone 非空按手机号过滤） | credit 积分流水。
func (s *Server) handleAdminUsage(w http.ResponseWriter, r *http.Request) {
	overview, err := s.store.UsageOverview()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	page, size := pageReq(r)
	resp := map[string]any{
		"overview":  overview,
		"page":      page,
		"page_size": size,
	}
	if r.URL.Query().Get("list") == "credit" {
		rows, total, err := s.store.AllCreditRecordsPage(page, size)
		if err != nil {
			writeErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp["items"], resp["total"] = rows, total
	} else {
		rows, total, err := s.store.AllUsageLogsPage(r.URL.Query().Get("phone"), page, size)
		if err != nil {
			writeErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp["items"], resp["total"] = rows, total
	}
	writeJSON(w, http.StatusOK, resp)
}
