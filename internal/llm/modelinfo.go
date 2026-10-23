package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"omni/internal/config"
)

// models.dev 是开放的模型元数据库（上下文窗口、最大输出、输入模态等），
// 也是各种 AI 客户端自动识别模型参数的常用来源。供应商自己的 /models
// 通常只返回 id（OpenCode 网关就是如此），所以上下文长度只能靠它补齐。
const (
	modelsDevURL  = "https://models.dev/api.json"
	modelsDevTTL  = 24 * time.Hour
	modelsDevFile = "models.dev.json"
)

// ModelMeta 是从 models.dev 查到的模型能力参数。
type ModelMeta struct {
	ContextWindow   int      `json:"context_window,omitempty"`
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"`
	InputTypes      []string `json:"input_types,omitempty"`
	ToolCall        bool     `json:"tool_call,omitempty"`
	Reasoning       bool     `json:"reasoning,omitempty"`
	// Provider 是命中的 models.dev 供应商 id；Source 标明数据来源。
	Provider string `json:"provider,omitempty"`
	Source   string `json:"source,omitempty"`
}

// mdProvider 只保留查询需要的字段，其余（价格、发布时间等）在解析时忽略，
// 缓存到磁盘的也是裁剪后的结构，避免几 MB 的完整 registry 反复读写。
type mdProvider struct {
	API    string             `json:"api,omitempty"`
	Models map[string]mdModel `json:"models"`
}

type mdModel struct {
	Limit            *mdLimit      `json:"limit,omitempty"`
	Modalities       *mdModalities `json:"modalities,omitempty"`
	ToolCall         bool          `json:"tool_call,omitempty"`
	Reasoning        bool          `json:"reasoning,omitempty"`
	CanonicalModelID string        `json:"canonical_model_id,omitempty"`
}

type mdLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type mdModalities struct {
	Input []string `json:"input"`
}

// mdRegistry 的 JSON 顶层就是「供应商 id -> 供应商」，没有 providers 包一层。
type mdRegistry map[string]mdProvider

type mdCacheFile struct {
	FetchedAt time.Time             `json:"fetched_at"`
	Providers map[string]mdProvider `json:"providers"`
}

var (
	mdMu       sync.Mutex
	mdCache    mdRegistry
	mdLoadedAt time.Time
)

// LookupModelMeta 按供应商类型 + base URL + 模型名到 models.dev 查询参数。
// 匹配顺序：base URL 命中供应商 → 全库模型名唯一命中；都不中则返回错误。
func LookupModelMeta(ctx context.Context, providerType, baseURL, model string) (*ModelMeta, error) {
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("模型名为空")
	}
	reg, err := loadModelsDev(ctx)
	if err != nil {
		return nil, err
	}
	meta, ok := lookupModelMeta(reg, providerType, baseURL, model)
	if !ok {
		return nil, fmt.Errorf("models.dev 未收录模型 %s（可手动填写上下文/输出参数）", model)
	}
	meta.Source = "models.dev"
	return meta, nil
}

// loadModelsDev 优先用内存缓存，其次 24h 内的磁盘缓存，最后联网拉取。
// 联网失败时退回任意旧缓存（模型参数变化很慢，旧数据仍可用）。
func loadModelsDev(ctx context.Context) (mdRegistry, error) {
	mdMu.Lock()
	defer mdMu.Unlock()
	if mdCache != nil && time.Since(mdLoadedAt) < modelsDevTTL {
		return mdCache, nil
	}
	path := modelsDevCachePath()
	if reg, ok := readModelsDevCache(path, modelsDevTTL); ok {
		mdCache, mdLoadedAt = reg, time.Now()
		return reg, nil
	}
	reg, err := fetchModelsDev(ctx)
	if err != nil {
		if reg, ok := readModelsDevCache(path, 0); ok {
			mdCache, mdLoadedAt = reg, time.Now()
			return reg, nil
		}
		return nil, fmt.Errorf("获取 models.dev 模型元数据失败: %w", err)
	}
	_ = writeModelsDevCache(path, reg)
	mdCache, mdLoadedAt = reg, time.Now()
	return reg, nil
}

func modelsDevCachePath() string {
	return filepath.Join(config.DefaultDataDir(), modelsDevFile)
}

func fetchModelsDev(ctx context.Context) (mdRegistry, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevURL, nil)
	if err != nil {
		return nil, err
	}
	// 不设置 User-Agent 会被部分 CDN 当成脚本流量；这里表明身份即可。
	req.Header.Set("User-Agent", "omni-agent/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	var reg mdRegistry
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return nil, err
	}
	if len(reg) == 0 {
		return nil, fmt.Errorf("models.dev 返回空数据")
	}
	return reg, nil
}

func readModelsDevCache(path string, maxAge time.Duration) (mdRegistry, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return nil, false
	}
	if maxAge > 0 && time.Since(st.ModTime()) > maxAge {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cache mdCacheFile
	if err := json.Unmarshal(data, &cache); err != nil || len(cache.Providers) == 0 {
		return nil, false
	}
	return mdRegistry(cache.Providers), true
}

func writeModelsDevCache(path string, reg mdRegistry) error {
	if len(reg) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(mdCacheFile{FetchedAt: time.Now(), Providers: map[string]mdProvider(reg)})
	if err != nil {
		return err
	}
	// 先写临时文件再改名，避免进程中途退出留下半截 JSON 让下次读缓存失败。
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// lookupModelMeta 在给定 registry 上做匹配（拆出来便于单测，不联网）。
func lookupModelMeta(reg mdRegistry, providerType, baseURL, model string) (*ModelMeta, bool) {
	if len(reg) == 0 || strings.TrimSpace(model) == "" {
		return nil, false
	}
	base := normalizeAPIURL(baseURL)
	if base == "" {
		base = normalizeAPIURL(defaultAPIForType(providerType))
	}

	ids := make([]string, 0, len(reg))
	for id := range reg {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// 1) base URL 命中供应商：只看这些供应商，避免同名模型跨供应商误配。
	var candidates []string
	if base != "" {
		for _, id := range ids {
			api := normalizeAPIURL(reg[id].API)
			if api == "" {
				continue
			}
			if api == base || strings.HasPrefix(api, base+"/") || strings.HasPrefix(base, api+"/") {
				candidates = append(candidates, id)
			}
		}
	}
	if len(candidates) > 0 {
		for _, id := range candidates {
			if m, ok := findMDModel(reg[id], model); ok {
				return metaFromMD(id, m), true
			}
		}
	}

	// 2) 没有 base URL 或没命中：全库按模型名找。多个供应商都有同名模型时，
	// 只有参数完全一致才采用——否则宁可报找不到，也不把参数配错。
	var found *ModelMeta
	for _, id := range ids {
		m, ok := findMDModel(reg[id], model)
		if !ok {
			continue
		}
		meta := metaFromMD(id, m)
		if found == nil {
			found = meta
			continue
		}
		if !sameMeta(found, meta) {
			return nil, false
		}
	}
	if found != nil {
		return found, true
	}
	return nil, false
}

// findMDModel 按模型 id 查找，兼容 key 带供应商前缀（provider/model）与 canonical_model_id。
func findMDModel(p mdProvider, model string) (mdModel, bool) {
	if m, ok := p.Models[model]; ok {
		return m, true
	}
	for id, m := range p.Models {
		if strings.EqualFold(id, model) {
			return m, true
		}
		if m.CanonicalModelID != "" && strings.EqualFold(m.CanonicalModelID, model) {
			return m, true
		}
		if i := strings.LastIndex(id, "/"); i >= 0 && strings.EqualFold(id[i+1:], model) {
			return m, true
		}
	}
	return mdModel{}, false
}

func metaFromMD(id string, m mdModel) *ModelMeta {
	meta := &ModelMeta{Provider: id, ToolCall: m.ToolCall, Reasoning: m.Reasoning}
	if m.Limit != nil {
		meta.ContextWindow = m.Limit.Context
		meta.MaxOutputTokens = m.Limit.Output
	}
	if m.Modalities != nil {
		meta.InputTypes = normalizeInputTypes(m.Modalities.Input)
	}
	return meta
}

func normalizeInputTypes(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		switch v {
		case "text", "image", "video", "pdf", "audio":
		default:
			continue
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func sameMeta(a, b *ModelMeta) bool {
	if a.ContextWindow != b.ContextWindow || a.MaxOutputTokens != b.MaxOutputTokens {
		return false
	}
	if len(a.InputTypes) != len(b.InputTypes) {
		return false
	}
	for i := range a.InputTypes {
		if a.InputTypes[i] != b.InputTypes[i] {
			return false
		}
	}
	return true
}

// normalizeAPIURL 统一 base URL：去掉首尾空白/尾斜杠与常见端点后缀，
// scheme/host 小写，便于和 models.dev 的 api 字段比对。
func normalizeAPIURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(strings.ToLower(raw), "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	lower := strings.ToLower(u.Path)
	for _, suffix := range []string{"/chat/completions", "/completions", "/messages", "/responses", "/models"} {
		if strings.HasSuffix(lower, suffix) {
			u.Path = u.Path[:len(u.Path)-len(suffix)]
			break
		}
	}
	return u.Scheme + "://" + u.Host + u.Path
}

// defaultAPIForType 在供应商没填 base_url 时给出各家默认地址。
func defaultAPIForType(typ string) string {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "openai":
		return "https://api.openai.com/v1"
	case "anthropic":
		return "https://api.anthropic.com/v1"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta"
	case "ollama":
		return "http://localhost:11434/v1"
	}
	return ""
}
