package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultConfigDir      = ".config/omni"
	defaultMaxToolOutput  = 4000
	defaultBashTimeoutSec = 10
)

// ProviderConfig 描述单个 LLM 提供方。
type ProviderConfig struct {
	Type    string `json:"type"` // openai | anthropic | gemini
	Model   string `json:"model"`
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	EnvKey  string `json:"env_key,omitempty"` // 从该环境变量读取 key，优先级高于 api_key
	Default bool   `json:"default,omitempty"`
}

// Config 是 omni 的顶层配置。
type Config struct {
	Provider          string           `json:"provider"`
	Model             string           `json:"model,omitempty"`
	Providers         []ProviderConfig `json:"providers"`
	MaxToolOutput     int              `json:"max_tool_output"`
	BashTimeoutSec    int              `json:"bash_timeout_seconds"`
	Cwd               string           `json:"cwd,omitempty"` // 工作目录；为空则取启动时目录
	SessionDir        string           `json:"session_dir,omitempty"`
	SystemPromptExtra string           `json:"system_prompt_extra,omitempty"`
}

// DefaultPath 返回默认配置文件路径。
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, defaultConfigDir, "config.json")
}

func DefaultSessionDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".omni-sessions"
	}
	return filepath.Join(home, ".local", "share", "omni", "sessions")
}

// Default 返回带默认值的配置。
func Default() *Config {
	return &Config{
		Provider:          "",
		MaxToolOutput:     defaultMaxToolOutput,
		BashTimeoutSec:    defaultBashTimeoutSec,
		SessionDir:        DefaultSessionDir(),
		SystemPromptExtra: "",
	}
}

// Load 从路径加载配置；path 为空则尝试默认路径。文件不存在时返回默认配置且不报错。
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		path = DefaultPath()
	}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("读取配置 %s 失败: %w", path, err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置 %s 失败: %w", path, err)
	}
	if cfg.MaxToolOutput <= 0 {
		cfg.MaxToolOutput = defaultMaxToolOutput
	}
	if cfg.BashTimeoutSec <= 0 {
		cfg.BashTimeoutSec = defaultBashTimeoutSec
	}
	if cfg.SessionDir == "" {
		cfg.SessionDir = DefaultSessionDir()
	}
	// 优先的默认 provider
	if cfg.Provider == "" {
		for _, p := range cfg.Providers {
			if p.Default {
				cfg.Provider = p.Model
				break
			}
		}
	}
	return cfg, nil
}

// Save 将配置写为 JSON。
func (c *Config) Save(path string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ResolveAPIKey 解析 API Key：优先 passthrough 参数，其次 EnvKey env，其次 APIKey 字段。
func (p ProviderConfig) ResolveAPIKey() string {
	if p.EnvKey != "" {
		if v := os.Getenv(p.EnvKey); v != "" {
			return v
		}
	}
	return p.APIKey
}

// DetectFromEnv 在无配置时从环境变量探测可用的 provider 类型。
// 返回 (type, model, baseURL, ok)。
func DetectFromEnv() (string, string, string, bool) {
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		return "openai", os.Getenv("OPENAI_MODEL"), "", true
	}
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		return "anthropic", os.Getenv("ANTHROPIC_MODEL"), "", true
	}
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		base := strings.TrimSpace(os.Getenv("GEMINI_BASE_URL"))
		return "gemini", os.Getenv("GEMINI_MODEL"), base, true
	}
	if v := os.Getenv("OLLAMA_BASE_URL"); v != "" {
		return "ollama", os.Getenv("OLLAMA_MODEL"), v, true
	}
	return "", "", "", false
}

// ExampleConfig 生成示例配置内容。
func ExampleConfig() string {
	return `{
  "provider": "gpt-4o",
  "model": "gpt-4o",
  "providers": [
    { "type": "openai",   "model": "gpt-4o",        "env_key": "OPENAI_API_KEY",   "default": true },
    { "type": "ollama",   "model": "qwen2.5-coder", "base_url": "http://localhost:11434/v1" },
    { "type": "anthropic","model": "claude-sonnet-4-20250514", "env_key": "ANTHROPIC_API_KEY" },
    { "type": "gemini",   "model": "gemini-2.5-pro", "env_key": "GEMINI_API_KEY" }
  ],
  "max_tool_output": 4000,
  "bash_timeout_seconds": 10
}
`
}
