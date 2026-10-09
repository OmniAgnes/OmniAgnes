package webui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omni/internal/agent"
	"omni/internal/config"
	"omni/internal/store"
)

// stubOpenCodeFree 预置一份「新鲜的」网关注册表缓存，使 /api/models 不会去打真实网关。
func stubOpenCodeFree(t *testing.T, models ...string) {
	t.Helper()
	openCodeFreeMu.Lock()
	defer openCodeFreeMu.Unlock()
	openCodeFreeCache.models = models
	openCodeFreeCache.at = time.Now()
	t.Cleanup(func() {
		openCodeFreeMu.Lock()
		openCodeFreeCache.models = nil
		openCodeFreeCache.at = time.Time{}
		openCodeFreeMu.Unlock()
	})
}

func newFreeTestServer(t *testing.T) *server {
	t.Helper()
	stubOpenCodeFree(t, "aaa-free", "bbb-free")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SyncOpenCodeFree([]string{"aaa-free", "bbb-free"})
	cfg.Providers = append([]config.ProviderConfig{{
		Type: "openai", Model: "gpt-4o", Models: []string{"gpt-4o"},
	}}, cfg.Providers...)
	return &server{
		opts:   Options{ConfigPath: path, StartCwd: dir, Version: "test"},
		cfg:    cfg,
		store:  store.New(dir),
		agents: map[string]*agent.Agent{},
	}
}

// 免费模型组默认出现在 /api/models 里，并带 opencode_free 标记供前端换显示名
func TestModelsIncludeOpenCodeFreeGroup(t *testing.T) {
	s := newFreeTestServer(t)
	code, body := do(t, s, "GET", "/api/models", "")
	if code != 200 {
		t.Fatalf("获取模型列表失败: %d %s", code, body)
	}
	var got struct {
		Models []struct {
			Model        string `json:"model"`
			Provider     string `json:"provider"`
			Type         string `json:"type"`
			OpenCodeFree bool   `json:"opencode_free"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("解析失败: %s", body)
	}
	var free []string
	for _, m := range got.Models {
		if m.OpenCodeFree {
			free = append(free, m.Model)
		}
		if !m.OpenCodeFree && m.Type == config.OpenCodeProviderType {
			t.Fatalf("免费组外的模型不应带标记: %+v", m)
		}
	}
	if len(free) != 2 || free[0] != "aaa-free" || free[1] != "bbb-free" {
		t.Fatalf("免费模型清单异常: %v", free)
	}
	// 选中的免费模型能被反查到供应商（聊天/计划任务据此寻址）
	if p := s.cfg.ProviderForModel("bbb-free"); p == nil || p.Type != config.OpenCodeProviderType {
		t.Fatalf("免费模型应可反查到供应商: %+v", p)
	}
}

// 关掉开关：菜单里不再有该组，配置文件里开关已落盘且没有虚拟条目
func TestOpenCodeFreeToggleOff(t *testing.T) {
	s := newFreeTestServer(t)
	s.cfg.Provider = "bbb-free"

	code, body := do(t, s, "POST", "/api/settings", `{"opencode_free":false}`)
	if code != 200 || !strings.Contains(body, `"opencode_free":false`) {
		t.Fatalf("关闭免费组失败: %d %s", code, body)
	}
	// 当前模型原在免费组里，应回落到仍可用的 gpt-4o
	if !strings.Contains(body, `"model":"gpt-4o"`) {
		t.Fatalf("当前模型应回落到 gpt-4o: %s", body)
	}
	_, body = do(t, s, "GET", "/api/models", "")
	if strings.Contains(body, "-free") {
		t.Fatalf("关闭后不应再返回免费模型: %s", body)
	}
	// 落盘：开关为 false，且虚拟条目没写进配置
	saved, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.OpenCodeFreeEnabled() {
		t.Fatal("开关应已落盘为 false")
	}
	for _, p := range saved.Providers {
		if p.Builtin {
			t.Fatalf("虚拟条目不应落盘: %+v", p)
		}
	}
}

// 重新开启：清单用已缓存的网关注册表，不落盘虚拟条目
func TestOpenCodeFreeToggleOn(t *testing.T) {
	s := newFreeTestServer(t)
	s.cfg.DropOpenCodeFree()
	if code, body := do(t, s, "POST", "/api/settings", `{"opencode_free":true}`); code != 200 || !strings.Contains(body, `"opencode_free":true`) {
		t.Fatalf("开启免费组失败: %d %s", code, body)
	}
	_, body := do(t, s, "GET", "/api/models", "")
	if !strings.Contains(body, "aaa-free") {
		t.Fatalf("开启后应返回网关注册表清单: %s", body)
	}
	data, err := os.ReadFile(s.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "aaa-free") {
		t.Fatalf("虚拟条目的清单不应落盘: %s", data)
	}
}

// PUT /api/providers 整体替换时要把前端带回来的虚拟条目丢掉，否则免费组会被保存成僵尸配置
func TestProvidersPutKeepsFreeGroup(t *testing.T) {
	s := newFreeTestServer(t)
	code, body := do(t, s, "PUT", "/api/providers",
		`{"providers":[{"type":"openai","model":"gpt-4o"},{"type":"opencode","builtin":true,"model":"aaa-free","models":["aaa-free","zzz-free"]}]}`)
	if code != 200 {
		t.Fatalf("保存供应商失败: %d %s", code, body)
	}
	var got struct {
		Providers []config.ProviderConfig `json:"providers"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("解析失败: %s", body)
	}
	var n int
	for _, p := range got.Providers {
		if p.Type == config.OpenCodeProviderType {
			n++
			if p.HasModel("zzz-free") {
				t.Fatal("前端回传的免费组清单不应被采纳（应由网关注册表决定）")
			}
		}
	}
	if n != 1 {
		t.Fatalf("免费组应保留 1 个，实际 %d 个: %s", n, body)
	}
	data, err := os.ReadFile(s.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"type": "openai"`) || strings.Contains(string(data), "aaa-free") {
		t.Fatalf("落盘内容异常: %s", data)
	}
}