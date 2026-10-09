package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.MaxToolOutput != 4000 {
		t.Fatalf("MaxToolOutput = %d, want 4000", c.MaxToolOutput)
	}
	if c.BashTimeoutSec != 10 {
		t.Fatalf("BashTimeoutSec = %d, want 10", c.BashTimeoutSec)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.MaxToolOutput != 4000 {
		t.Fatalf("MaxToolOutput = %d", c.MaxToolOutput)
	}
}

func TestLoadAndNormalize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{
      "provider": "gpt-4o",
      "providers": [
        {"type":"openai","model":"gpt-4o","env_key":"TEST_KEY","default":true}
      ],
      "max_tool_output": 100,
      "bash_timeout_seconds": 3
    }`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Provider != "gpt-4o" {
		t.Fatalf("Provider = %q", c.Provider)
	}
	if c.MaxToolOutput != 100 {
		t.Fatalf("MaxToolOutput = %d", c.MaxToolOutput)
	}
	if c.BashTimeoutSec != 3 {
		t.Fatalf("BashTimeoutSec = %d", c.BashTimeoutSec)
	}
	// 用户自己配的供应商之外，Load 还会注入 opencode 免费模型组（虚拟条目）
	user := c.Providers[:0:0]
	for _, p := range c.Providers {
		if !p.Builtin {
			user = append(user, p)
		}
	}
	if len(user) != 1 || user[0].Type != "openai" {
		t.Fatalf("providers 解析失败: %+v", c.Providers)
	}
	free := c.ProviderForModel(seedOpenCodeFreeModels[0])
	if free == nil || free.Type != OpenCodeProviderType || !free.Builtin {
		t.Fatalf("opencode 免费模型组未注入: %+v", free)
	}
	if free.ResolveAPIKey() != "" || !free.NoAuth() {
		t.Fatalf("免费组应无需 API Key: %+v", free)
	}
}

// opencode 免费组默认开启；显式关掉后 Load 不再注入，且当前模型回落到可用项
func TestOpenCodeFreeToggle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := `{"providers":[{"type":"openai","model":"gpt-4o","default":true}],"opencode_free":false}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.OpenCodeFreeEnabled() {
		t.Fatal("opencode_free=false 应视为关闭")
	}
	for _, p := range c.Providers {
		if p.Type == OpenCodeProviderType {
			t.Fatalf("关闭后不应注入免费组: %+v", p)
		}
	}
	// 未配置开关 = 默认开启
	c2, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c2.OpenCodeFreeEnabled() {
		t.Fatal("缺省应开启 opencode 免费模型组")
	}
}

// 关闭免费组时，若当前模型正落在该组里要回落到仍可用的模型（否则运行时会找不到供应商）
func TestDropOpenCodeFreeResetsCurrentModel(t *testing.T) {
	c := Default()
	c.Providers = []ProviderConfig{{Type: "openai", Model: "gpt-4o", Models: []string{"gpt-4o"}}}
	c.SyncOpenCodeFree(nil)
	c.Provider = c.Providers[1].Model // 选中某个免费模型
	c.DropOpenCodeFree()
	if c.Provider != "gpt-4o" {
		t.Fatalf("当前模型应回落到 gpt-4o，实际 %q", c.Provider)
	}
	if c.OpenCodeFreeEnabled() {
		t.Fatal("关闭后开关应为 false")
	}
	for _, p := range c.Providers {
		if p.Type == OpenCodeProviderType {
			t.Fatalf("虚拟条目应被移除: %+v", p)
		}
	}
}

// Save 不落盘虚拟供应商（清单来自网关、会上下架），Reload 后按开关重新注入
func TestSaveSkipsBuiltinProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Providers = []ProviderConfig{{Type: "openai", Model: "gpt-4o", Models: []string{"gpt-4o"}}}
	c.SyncOpenCodeFree([]string{"xxx-free"})
	c.Provider = "xxx-free"
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Providers) != 2 {
		t.Fatalf("应为 1 个用户供应商 + 1 个注入的免费组，实际 %d 个", len(got.Providers))
	}
	// 重启后按开关重新注入（Load 不联网，用兜底种子）；旧的实时清单不落盘
	if got.ProviderForModel("xxx-free") != nil {
		t.Fatalf("实时清单不应落盘: %+v", got.Providers)
	}
	if got.ProviderForModel(seedOpenCodeFreeModels[0]) == nil {
		t.Fatalf("免费组未在重启后恢复: %+v", got.Providers)
	}
	if got.Provider != "xxx-free" {
		t.Fatalf("当前模型应保留，实际 %q", got.Provider)
	}
}

// SyncOpenCodeFree 幂等：反复注入不会堆叠出多个免费组
func TestSyncOpenCodeFreeIdempotent(t *testing.T) {
	c := Default()
	for i := 0; i < 3; i++ {
		c.SyncOpenCodeFree([]string{"xxx-free"})
	}
	n := 0
	for _, p := range c.Providers {
		if p.Type == OpenCodeProviderType {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("免费组应只有 1 个，实际 %d 个", n)
	}
}

func TestInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestResolveAPIKeyPrecedence(t *testing.T) {
	t.Setenv("TEST_OMNI_KEY", "from-env")
	p := ProviderConfig{APIKey: "literal", EnvKey: "TEST_OMNI_KEY"}
	if got := p.ResolveAPIKey(); got != "from-env" {
		t.Fatalf("got %q, want from env", got)
	}
	p = ProviderConfig{APIKey: "literal"}
	if got := p.ResolveAPIKey(); got != "literal" {
		t.Fatalf("got %q, want literal", got)
	}
}

func TestDetectFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "x")
	typ, model, base, ok := DetectFromEnv()
	if !ok || typ != "anthropic" {
		t.Fatalf("expected anthropic, got %q ok=%v", typ, ok)
	}
	_ = model
	_ = base
}

func TestSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")
	c := Default()
	c.Provider = "m"
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
