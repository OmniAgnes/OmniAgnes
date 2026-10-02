package api

import (
	"net/http"

	"omni-server/internal/store"
)

// 会员列表
func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// 积分赠送/回退
func (s *Server) handleAdminCredit(w http.ResponseWriter, r *http.Request) {
	userID := idOf(r)
	var req struct {
		Amount int64  `json:"amount"`
		Reason string `json:"reason"`
	}
	if err := decode(r, &req); err != nil || req.Amount == 0 {
		writeErr(w, "amount 必须非 0", http.StatusBadRequest)
		return
	}
	if req.Reason == "" {
		req.Reason = "管理调整"
	}
	bal, err := s.store.AddCredit(userID, req.Amount, req.Reason, "", "")
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"balance": bal})
}

// ---------- 供应商 ----------

func (s *Server) handleProvidersList(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.ListProviders()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": ps})
}

func (s *Server) handleProvidersSave(w http.ResponseWriter, r *http.Request) {
	var p store.Provider
	if err := decode(r, &p); err != nil {
		writeErr(w, "请求体解析失败", http.StatusBadRequest)
		return
	}
	if p.Name == "" || p.Type == "" {
		writeErr(w, "name/type 必填", http.StatusBadRequest)
		return
	}
	if r.PathValue("id") != "" {
		p.ID = idOf(r)
	}
	if err := s.store.SaveProvider(&p); err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 供应商里配置的模型自动进 models 表并绑定，用户在网关侧直接可用
	if err := s.store.SyncProviderModels(p.ID, p.Models); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": p})
}

func (s *Server) handleProvidersDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteProvider(idOf(r)); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 模型 ----------

func (s *Server) handleModelsList(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.ListModels()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mps, err := s.store.AllModelProviders()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	providers, err := s.store.ListProviders()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"models":          models,
		"model_providers": mps,
		"providers":       providers,
	})
}

func (s *Server) handleModelsSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         int64   `json:"id"`
		Key        string  `json:"key"`
		Builtin    bool    `json:"builtin"`
		Enabled    bool    `json:"enabled"`
		Multiplier float64 `json:"multiplier"`
		Free       bool    `json:"free"`
		ProviderID int64   `json:"provider_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, "请求体解析失败", http.StatusBadRequest)
		return
	}
	if req.Key == "" {
		writeErr(w, "模型名(key)必填", http.StatusBadRequest)
		return
	}
	if req.Multiplier <= 0 {
		req.Multiplier = 1
	}
	if r.PathValue("id") != "" {
		req.ID = idOf(r)
	}
	m := &store.Model{
		ID:         req.ID,
		Key:        req.Key,
		Builtin:    req.Builtin,
		Enabled:    req.Enabled,
		Multiplier: req.Multiplier,
		Free:       req.Free,
	}
	if err := s.store.SaveModel(m, req.ProviderID); err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model": m})
}

func (s *Server) handleModelsDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteModel(idOf(r)); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 模型×供应商 ----------

func (s *Server) handleModelProvidersList(w http.ResponseWriter, r *http.Request) {
	all, err := s.store.AllModelProviders()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model_providers": all})
}

func (s *Server) handleModelProviderToggle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModelID    int64 `json:"model_id"`
		ProviderID int64 `json:"provider_id"`
		Enabled    bool  `json:"enabled"`
	}
	if err := decode(r, &req); err != nil || req.ModelID == 0 || req.ProviderID == 0 {
		writeErr(w, "model_id/provider_id 必填", http.StatusBadRequest)
		return
	}
	if err := s.store.ToggleModelProviderEnabled(req.ModelID, req.ProviderID, req.Enabled); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
