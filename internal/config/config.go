package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	defaultConfigDir      = ".config/omni"
	defaultMaxToolOutput  = 4000
	defaultBashTimeoutSec = 10
)

// ModelOptions 描述单个模型的个性化参数。同一供应商下不同模型能力差异很大
// （上下文窗口、最大输出、支持的输入模态、是否开推理），因此这些参数按模型
// 粒度配置；未设置的字段回落到供应商级同名字段。
type ModelOptions struct {
	ContextWindow   int      `json:"context_window,omitempty"`    // 上下文窗口（token）
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"` // 最大输出 token
	InputTypes      []string `json:"input_types,omitempty"`       // 支持的输入类型：text/image/video/pdf/audio
	Reasoning       string   `json:"reasoning,omitempty"`         // 推理等级：default | off | on
}

// Empty 报告该模型是否未配置任何个性化参数。
func (o ModelOptions) Empty() bool {
	return o.ContextWindow == 0 && o.MaxOutputTokens == 0 && len(o.InputTypes) == 0 && o.Reasoning == ""
}

// ProviderConfig 描述单个 LLM 提供方。
type ProviderConfig struct {
	Name    string   `json:"name,omitempty"`   // 显示名称
	Type    string   `json:"type"`             // openai | anthropic | gemini | ollama
	Model   string   `json:"model,omitempty"`  // 主模型（与 models[0] 同步）
	Models  []string `json:"models,omitempty"` // 该供应商可用模型（首个为主模型）
	BaseURL string   `json:"base_url,omitempty"`
	APIKey  string   `json:"api_key,omitempty"`
	EnvKey  string   `json:"env_key,omitempty"` // 从该环境变量读取 key，优先级高于 api_key
	Default bool     `json:"default,omitempty"`

	// 以下为供应商级默认值，模型未单独配置时生效（兼容旧配置文件）
	ContextWindow   int      `json:"context_window,omitempty"`
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"`
	InputTypes      []string `json:"input_types,omitempty"`
	Reasoning       string   `json:"reasoning,omitempty"`

	// ModelOptions 按模型名覆盖上述默认值
	ModelOptions map[string]ModelOptions `json:"model_options,omitempty"`
}

// OptionsFor 返回指定模型的有效参数：模型级配置优先，缺项回落到供应商级默认值。
func (p *ProviderConfig) OptionsFor(model string) ModelOptions {
	o := ModelOptions{
		ContextWindow:   p.ContextWindow,
		MaxOutputTokens: p.MaxOutputTokens,
		InputTypes:      p.InputTypes,
		Reasoning:       p.Reasoning,
	}
	m, ok := p.ModelOptions[model]
	if !ok {
		return o
	}
	if m.ContextWindow > 0 {
		o.ContextWindow = m.ContextWindow
	}
	if m.MaxOutputTokens > 0 {
		o.MaxOutputTokens = m.MaxOutputTokens
	}
	if len(m.InputTypes) > 0 {
		o.InputTypes = m.InputTypes
	}
	if m.Reasoning != "" {
		o.Reasoning = m.Reasoning
	}
	return o
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

// Normalize 同步 Model 与 Models 字段，并剔除已删除模型残留的个性化配置。
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
	// 清理：模型已删或配置为空的一并移除，避免配置文件里堆积死条目
	if len(p.ModelOptions) > 0 {
		for name, opt := range p.ModelOptions {
			if !seen[name] || opt.Empty() {
				delete(p.ModelOptions, name)
			}
		}
		if len(p.ModelOptions) == 0 {
			p.ModelOptions = nil
		}
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
	MaxHistoryTurns   int                        `json:"max_history_turns"`     // 上下文保留的最近用户提问轮数；0 用默认
	Theme             string                     `json:"theme,omitempty"`       // dark | light | system
	ThemeColor        string                     `json:"theme_color,omitempty"` // 主题配色：zinc/violet/blue/emerald/amber/rose
	MCPServers        map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
	DisabledSkills    []string                   `json:"disabled_skills,omitempty"` // 禁用的技能名，默认全部启用
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
		Theme:             "light",
	}
}

// Load 从路径加载配置；path 为空则尝试默认路径。文件不存在时返回默认配置且不报错。
// builtinMCPServers 扫描内置插件目录，把其中的可执行文件自动注册为
// MCP 服务器（名为文件名去扩展名）。扫描范围：
//   - 工作目录 .omni/mcp/（本地开发构建产物，见 dev/README.md）
//   - 数据目录 mcp/（~/.local/share/omni/mcp）
//   - 主程序可执行文件同目录（桌面版随包内置）
//
// 配置里已有同名条目（包括已停用的）默认不覆盖，保留用户配置；
// 但条目的 command 文件已不存在时视为失效路径，改用内置插件路径（env 等字段保留）。
func (c *Config) builtinMCPServers() {
	dirs := []string{
		filepath.Join(".omni", "mcp"),
		filepath.Join(DefaultDataDir(), "mcp"),
	}
	if c.Cwd != "" { // 配置里指定了项目目录时，也扫它的 .omni/mcp
		dirs = append(dirs, filepath.Join(c.Cwd, ".omni", "mcp"))
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if runtime.GOOS == "windows" && filepath.Ext(name) != ".exe" {
				continue
			}
			if name == "omni"+filepath.Ext(name) { // 跳过主程序本体
				continue
			}
			server := strings.TrimSuffix(name, filepath.Ext(name))
			if c.MCPServers == nil {
				c.MCPServers = map[string]MCPServerConfig{}
			}
			// 同名条目保留用户配置；但 command 指向的文件已不存在（如卸载后残留的
			// 旧开发路径）视为失效，用内置路径覆盖——env 等其他字段仍保留。
			if cur, ok := c.MCPServers[server]; ok {
				if _, err := os.Stat(cur.Command); err == nil {
					continue
				}
				if cur.Command == "" || cur.URL != "" { // HTTP 或未填 command 的不算失效
					continue
				}
				cur.Command = filepath.Join(dir, name)
				c.MCPServers[server] = cur
				continue
			}
			c.MCPServers[server] = MCPServerConfig{Command: filepath.Join(dir, name)}
		}
	}
}

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
	if cfg.Theme != "dark" && cfg.Theme != "system" {
		cfg.Theme = "light"
	}
	switch cfg.ThemeColor {
	case "violet", "blue", "sky", "emerald", "amber", "rose":
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
	cfg.builtinMCPServers()
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
