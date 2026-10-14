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
	if len(c.Providers) != 1 || c.Providers[0].Type != "openai" {
		t.Fatalf("providers 解析失败: %+v", c.Providers)
	}
}

// 当前模型悬空（如旧配置选中已移除的限免模型）时，Load 回落到第一个可用模型
func TestLoadResetsDanglingCurrentModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := `{"provider":"xxx-free","providers":[{"type":"openai","model":"gpt-4o","models":["gpt-4o"]}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Provider != "gpt-4o" {
		t.Fatalf("当前模型应回落到 gpt-4o，实际 %q", c.Provider)
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
