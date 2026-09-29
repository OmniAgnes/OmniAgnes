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
	Name            string   `json:"name,omitempty"`              // 显示名称
	Type            string   `json:"type"`                        // openai | anthropic | gemini | ollama
	Model           string   `json:"model,omitempty"`             // 主模型（与 models[0] 同步）
	Models          []string `json:"models,omitempty"`            // 该供应商可用模型（首个为主模型）
	BaseURL         string   `json:"base_url,omitempty"`
	APIKey          string   `json:"api_key,omitempty"`
	EnvKey          string   `json:"env_key,omitempty"`           // 从该环境变量读取 key，优先级高于 api_key
	Default         bool     `json:"default,omitempty"`
	ContextWindow   int      `json:"context_window,omitempty"`    // 上下文窗口（token）
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"` // 最大输出 token
	InputTypes      []string `json:"input_types,omitempty"`       // 支持的输入类型：text/image/video/pdf
	Reasoning       string   `json:"reasoning,omitempty"`         // 推理等级：default | off | on
}

// ProviderForModel 返回提供指定模型的供应商配置；找不到返回 nil。
func (c *Config) ProviderForModel(model string) *ProviderConfig {
	if model == "" {
		return nil
	}
	for i := range c.Providers {
		if c.Providers[i].HasModel(model) {
			return &c.Providers[i]
		}
	}
	return nil
}

// Normalize 同步 Model 与 Models 字段。
func (p *ProviderConfig) Normalize() {
	if len(p.Models) == 0 {
		if p.Model != "" {
			p.Models = []string{p.Model}
		}
		return
	}
	// 过滤空项并去重
	var models []string
	seen := map[string]bool{}
	for _, m := range p.Models {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		models = append(models, m)
	}
	p.Models = models
	if len(p.Models) > 0 {
		p.Model = p.Models[0]
	}
}

// HasModel 报告该供应商是否提供指定模型。
func (p *ProviderConfig) HasModel(model string) bool {
	for _, m := range p.Models {
		if m == model {
			return true
		}
	}
	return false
}

// MCPServerConfig 描述一个 MCP 服务器：command（stdio）或 url（streamable HTTP）二选一。
type MCPServerConfig struct {
	Command  string            `json:"command,omitempty"`
	Args     []string          `json:"args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	URL      string            `json:"url,omitempty"`
	Disabled bool              `json:"disabled,omitempty"`
}

// Config 是 omni 的顶层配置。
type Config struct {
	Provider          string                     `json:"provider"`
	Model             string                     `json:"model,omitempty"`
	Providers         []ProviderConfig           `json:"providers"`
	MaxToolOutput     int                        `json:"max_tool_output"`
	BashTimeoutSec    int                        `json:"bash_timeout_seconds"`
	Cwd               string                     `json:"cwd,omitempty"` // 工作目录；为空则取启动时目录
	SessionDir        string                     `json:"session_dir,omitempty"`
	SystemPromptExtra string                     `json:"system_prompt_extra,omitempty"`
	Theme             string                     `json:"theme,omitempty"`           // dark | light | system
	ThemeColor        string                     `json:"theme_color,omitempty"`     // 主题配色：zinc/violet/blue/emerald/amber/rose
	MCPServers        map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
}

// DefaultPath 返回默认配置文件路径。
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, defaultConfigDir, "config.json")
}

func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".omni-data"
	}
	return filepath.Join(home, ".local", "share", "omni")
}

func DefaultSessionDir() string {
	return filepath.Join(DefaultDataDir(), "sessions")
}

// DefaultMemoryPath 返回全局记忆文件路径。
func DefaultMemoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "omni-memory.md"
	}
	return filepath.Join(home, defaultConfigDir, "memory.md")
}

// DefaultSkillsDir 返回全局技能目录。
func DefaultSkillsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".omni-skills"
	}
	return filepath.Join(home, defaultConfigDir, "skills")
}

// Default 返回带默认值的配置。
func Default() *Config {
	return &Config{
		Provider:          "",
		MaxToolOutput:     defaultMaxToolOutput,
		BashTimeoutSec:    defaultBashTimeoutSec,
		SessionDir:        DefaultSessionDir(),
		SystemPromptExtra: "",
		Theme:             "dark",
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
	if cfg.Theme != "light" && cfg.Theme != "system" {
		cfg.Theme = "dark"
	}
	switch cfg.ThemeColor {
	case "violet", "blue", "emerald", "amber", "rose":
	default:
		cfg.ThemeColor = "zinc"
	}
	for i := range cfg.Providers {
		cfg.Providers[i].Normalize()
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
