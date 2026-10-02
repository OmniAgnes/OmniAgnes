// Package api 提供 server 的 HTTP 接口：鉴权、用户面、管理面。
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"omni-server/internal/auth"
	"omni-server/internal/config"
	"omni-server/internal/store"
)

type Server struct {
	store *store.Store
	otp   *auth.OTPStore
	cfg   *config.Config
	asset http.Handler // 前端静态资源（SPA）
}

func New(st *store.Store, cfg *config.Config, asset http.Handler) *Server {
	return &Server{store: st, otp: auth.NewOTPStore(5 * time.Minute), cfg: cfg, asset: asset}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/site", s.handleSite)
	mux.HandleFunc("POST /api/auth/send-code", s.handleSendCode)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("GET /api/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("POST /api/me/phone", s.requireAuth(s.handleMePhone))
	mux.HandleFunc("POST /api/claim", s.requireAuth(s.handleClaim))
	mux.HandleFunc("GET /api/models", s.requireAuth(s.handleModelList))
	mux.HandleFunc("GET /api/usage", s.requireAuth(s.handleUsageGet))
	mux.HandleFunc("POST /api/usage", s.requireAuth(s.handleUsagePost))

	// 管理面
	mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.handleAdminUsers))
	mux.HandleFunc("POST /api/admin/users/{id}/credit", s.requireAdmin(s.handleAdminCredit))
	mux.HandleFunc("GET /api/admin/usage", s.requireAdmin(s.handleAdminUsage))
	mux.HandleFunc("GET /api/admin/providers", s.requireAdmin(s.handleProvidersList))
	mux.HandleFunc("POST /api/admin/providers", s.requireAdmin(s.handleProvidersSave))
	mux.HandleFunc("PUT /api/admin/providers/{id}", s.requireAdmin(s.handleProvidersSave))
	mux.HandleFunc("DELETE /api/admin/providers/{id}", s.requireAdmin(s.handleProvidersDelete))
	mux.HandleFunc("GET /api/admin/models", s.requireAdmin(s.handleModelsList))
	mux.HandleFunc("POST /api/admin/models", s.requireAdmin(s.handleModelsSave))
	mux.HandleFunc("PUT /api/admin/models/{id}", s.requireAdmin(s.handleModelsSave))
	mux.HandleFunc("DELETE /api/admin/models/{id}", s.requireAdmin(s.handleModelsDelete))
	mux.HandleFunc("GET /api/admin/model-providers", s.requireAdmin(s.handleModelProvidersList))
	mux.HandleFunc("POST /api/admin/model-providers/toggle", s.requireAdmin(s.handleModelProviderToggle))
	mux.HandleFunc("GET /api/admin/settings", s.requireAdmin(s.handleAdminSettingsGet))
	mux.HandleFunc("POST /api/admin/settings", s.requireAdmin(s.handleAdminSettingsPost))

	return s.cors(s.assetRouter(mux))
}

// cors 放开本机任意端口跨源（桌面端在随机端口调用 9000）。
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// assetRouter 把前端静态资源挂在根路径；/api/* 之外的都交给它（SPA）。
func (s *Server) assetRouter(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
			mux.ServeHTTP(w, r)
			return
		}
		if s.asset != nil {
			s.asset.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"app": "omni-server", "base_url": s.cfg.BaseURL})
	})
}

type ctxKey int

const userKey ctxKey = 1

func userOf(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

// bearer 从 Authorization 取出 token。
func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	s := strings.TrimSpace(h)
	if len(s) > 7 && strings.EqualFold(s[:7], "Bearer ") {
		return strings.TrimSpace(s[7:])
	}
	return ""
}

// requireAuth 校验 token 并放入 context。
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		u, err := s.store.UserByToken(tok)
		if err != nil || u == nil {
			writeErr(w, "未登录或会话已过期", http.StatusUnauthorized)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	}
}

// requireAdmin 在 requireAuth 基础上要求管理员。
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if !userOf(r).IsAdmin {
			writeErr(w, "无权限", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// ---------- 基础工具 ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, msg string, code int) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func decode(r *http.Request, into any) error {
	return json.NewDecoder(r.Body).Decode(into)
}

func idOf(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// ---------- 状态与鉴权 ----------

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"base_url":  s.cfg.BaseURL,
		"test_mode": s.cfg.TestMode,
		"now":       time.Now().Format(time.RFC3339),
	})
}

// handleSite 站点公开信息（标题/页脚/用户协议）：登录页、文档标题与页脚用，管理端可在后台设置。
func (s *Server) handleSite(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"title":     s.store.GetSetting("site_title"),
		"footer":    s.store.GetSetting("site_footer"),
		"agreement": s.store.GetSetting("site_agreement"),
		"test_mode": s.cfg.TestMode,
	})
}

// handleMePhone 用户修改自己的手机号：新号需先发短信/测试验证码并校验，且不能被他人占用。
func (s *Server) handleMePhone(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	var req struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := decode(r, &req); err != nil || !validPhone(req.Phone) || req.Code == "" {
		writeErr(w, "请输入新手机号与验证码", http.StatusBadRequest)
		return
	}
	if !s.otp.Verify(req.Phone, req.Code) {
		writeErr(w, "验证码错误或已过期", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateUserPhone(u.ID, req.Phone); err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	n, err := s.store.UserByID(u.ID)
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": n})
}

func (s *Server) handleSendCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if err := decode(r, &req); err != nil || !validPhone(req.Phone) {
		writeErr(w, "请输入正确手机号", http.StatusBadRequest)
		return
	}
	code := s.otp.Send(req.Phone, s.cfg.TestMode)
	// 测试模式把验证码随响应返回给前端展示，便于体验
	resp := map[string]any{
		"ok":        true,
		"test_mode": s.cfg.TestMode,
	}
	if s.cfg.TestMode && code != "" {
		resp["code"] = code
		resp["hint"] = "测试模式：验证码 " + code + "（正式开通短信后不会回显）"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone     string `json:"phone"`
		Code      string `json:"code"`
		Agreement bool   `json:"agreement"`
	}
	if err := decode(r, &req); err != nil || !validPhone(req.Phone) || req.Code == "" {
		writeErr(w, "请输入手机号与验证码", http.StatusBadRequest)
		return
	}
	if !req.Agreement {
		writeErr(w, "请先勾选并同意用户协议", http.StatusBadRequest)
		return
	}
	if !s.otp.Verify(req.Phone, req.Code) {
		writeErr(w, "验证码错误或已过期", http.StatusBadRequest)
		return
	}

	u, err := s.store.UserByPhone(req.Phone)
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	isNew := false
	if u == nil {
		// 未注册：自动注册
		u, err = s.store.CreateUser(req.Phone)
		if err != nil {
			writeErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		isNew = true
	}
	if err := s.store.AcceptAgreement(u.ID); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	token, err := s.store.CreateToken(u.ID, 30*24*time.Hour)
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":  token,
		"user":   u,
		"is_new": isNew,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteToken(bearer(r)); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleMe 用户信息 + 积分流水分页。kind=gain 只要获取、spend 只要消耗，空=全部。
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	// 读路径先清扫过期积分，保证展示的余额已剔除失效部分
	_ = s.store.SweepExpired(u.ID)
	if fresh, err := s.store.UserByID(u.ID); err == nil {
		u = fresh
	}
	kind := r.URL.Query().Get("kind")
	switch kind {
	case "gain":
		kind = "got"
	case "spend":
		kind = "spent"
	}
	page, size := pageReq(r)
	records, total, err := s.store.CreditRecordsPage(u.ID, kind, page, size)
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":      u,
		"records":   records,
		"total":     total,
		"page":      page,
		"page_size": size,
	})
}

// handleClaim 每日积分领取（打开软件自动调用，幂等：当天已领不再发放）。
// 开关/数量/有效期走 settings 表：daily_credit_enabled="1" /
// daily_credit_amount=<int> / daily_credit_days=<int>（0=不限，仅每天一次）。
func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	enabled := s.store.GetSetting("daily_credit_enabled") == "1"
	amount, _ := strconv.ParseInt(s.store.GetSetting("daily_credit_amount"), 10, 64)
	days, _ := strconv.ParseInt(s.store.GetSetting("daily_credit_days"), 10, 64)
	if !enabled || amount <= 0 {
		writeJSON(w, http.StatusOK, map[string]any{"claimed": false, "amount": 0, "balance": u.Balance})
		return
	}
	claimed, bal, err := s.store.ClaimDaily(u.ID, amount, days)
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	got := int64(0)
	if claimed {
		got = amount
	}
	writeJSON(w, http.StatusOK, map[string]any{"claimed": claimed, "amount": got, "balance": bal})
}

// handleModelList 全部启用中的模型，供前端「内置模型」组展示（含倍率/限免标记）。
func (s *Server) handleModelList(w http.ResponseWriter, r *http.Request) {
	ms, err := s.store.EnabledModels()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type item struct {
		Key        string  `json:"key"`
		Builtin    bool    `json:"builtin"`
		Multiplier float64 `json:"multiplier"`
		Free       bool    `json:"free"`
	}
	out := make([]item, 0, len(ms))
	for _, m := range ms {
		out = append(out, item{Key: m.Key, Builtin: m.Builtin, Multiplier: m.Multiplier, Free: m.Free})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": out})
}

// handleAdminSettingsGet 管理端读系统设置：站点信息（标题/页脚/协议）+ 每日积分领取。
func (s *Server) handleAdminSettingsGet(w http.ResponseWriter, r *http.Request) {
	amount, _ := strconv.ParseInt(s.store.GetSetting("daily_credit_amount"), 10, 64)
	days, _ := strconv.ParseInt(s.store.GetSetting("daily_credit_days"), 10, 64)
	writeJSON(w, http.StatusOK, map[string]any{
		"site_title":          s.store.GetSetting("site_title"),
		"footer":              s.store.GetSetting("site_footer"),
		"agreement":           s.store.GetSetting("site_agreement"),
		"daily_credit_enabled": s.store.GetSetting("daily_credit_enabled") == "1",
		"daily_credit_amount":  amount,
		"daily_credit_days":    days,
	})
}

// handleAdminSettingsPost 管理端改系统设置（字段可缺省，缺省不动）。
func (s *Server) handleAdminSettingsPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SiteTitle          *string `json:"site_title"`
		Footer             *string `json:"footer"`
		Agreement          *string `json:"agreement"`
		DailyCreditEnabled *bool   `json:"daily_credit_enabled"`
		DailyCreditAmount  *int64  `json:"daily_credit_amount"`
		DailyCreditDays    *int64  `json:"daily_credit_days"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, "参数错误", http.StatusBadRequest)
		return
	}
	type kv struct {
		key, val string
	}
	jobs := []kv{}
	if req.SiteTitle != nil {
		jobs = append(jobs, kv{"site_title", *req.SiteTitle})
	}
	if req.Footer != nil {
		jobs = append(jobs, kv{"site_footer", *req.Footer})
	}
	if req.Agreement != nil {
		jobs = append(jobs, kv{"site_agreement", *req.Agreement})
	}
	if req.DailyCreditEnabled != nil {
		jobs = append(jobs, kv{"daily_credit_enabled", boolStr(*req.DailyCreditEnabled)})
	}
	if req.DailyCreditAmount != nil && *req.DailyCreditAmount >= 0 {
		jobs = append(jobs, kv{"daily_credit_amount", strconv.FormatInt(*req.DailyCreditAmount, 10)})
	}
	if req.DailyCreditDays != nil && *req.DailyCreditDays >= 0 {
		jobs = append(jobs, kv{"daily_credit_days", strconv.FormatInt(*req.DailyCreditDays, 10)})
	}
	for _, j := range jobs {
		if err := s.store.SetSetting(j.key, j.val); err != nil {
			writeErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.handleAdminSettingsGet(w, r)
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func validPhone(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || len(p) > 20 {
		return false
	}
	for _, c := range p {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}