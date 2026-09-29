package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"omni/internal/config"
)

func TestSaveProviderConfigAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	err := os.WriteFile(path, []byte(`{"providers":[{"type":"openai","model":"gpt-4o","env_key":"OPENAI_API_KEY"}]}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	err = saveProviderConfig(path, "ollama", "qwen2.5-coder", "http://localhost:11434/v1", "")
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("期望保留原 provider 并追加 1 个，实际 %d 个", len(cfg.Providers))
	}
	if cfg.Providers[0].Model != "gpt-4o" {
		t.Fatalf("原 provider 被破坏: %+v", cfg.Providers[0])
	}
	got := cfg.Providers[1]
	if got.Type != "ollama" || got.BaseURL != "http://localhost:11434/v1" || !got.Default {
		t.Fatalf("追加的 provider 不正确: %+v", got)
	}
	if cfg.Provider != "qwen2.5-coder" {
		t.Fatalf("默认 provider 应为 qwen2.5-coder，实际 %q", cfg.Provider)
	}
}

func TestSaveProviderConfigUpdatesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	err := saveProviderConfig(path, "openai", "gpt-4o", "https://api.example.com/v1", "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	err = saveProviderConfig(path, "openai", "gpt-4o", "https://api2.example.com/v1", "sk-new")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("同类型同模型应原地更新，实际 %d 个 provider", len(cfg.Providers))
	}
	if cfg.Providers[0].APIKey != "sk-new" || cfg.Providers[0].BaseURL != "https://api2.example.com/v1" {
		t.Fatalf("更新未生效: %+v", cfg.Providers[0])
	}
}
