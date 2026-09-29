package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"omni/internal/llm"
)

// Env 描述工具执行环境。
type Env struct {
	Cwd       string
	Unsafe    bool
	MaxOutput int
	Timeout   time.Duration
}

// EnvFromConfig 便捷构造。
// func EnvFromConfig ...

// Tool 是单个工具的定义与实现。
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	ReadOnly    bool
	Execute     func(ctx context.Context, args map[string]any, env *Env) (string, error)
}

// Registry 持有全部工具。
type Registry struct {
	env   *Env
	tools map[string]*Tool
}

// NewRegistry 注册全部内置工具。
func NewRegistry(env *Env) *Registry {
	r := &Registry{
		env:   env,
		tools: map[string]*Tool{},
	}
	if env == nil {
		r.env = &Env{Cwd: ".", MaxOutput: 4000, Timeout: 10 * time.Second}
	}
	if r.env.Cwd == "" {
		cwd, _ := os.Getwd()
		r.env.Cwd = cwd
	}
	if r.env.MaxOutput <= 0 {
		r.env.MaxOutput = 4000
	}
	if r.env.Timeout <= 0 {
		r.env.Timeout = 10 * time.Second
	}
	for _, t := range builtinTools() {
		r.tools[t.Name] = t
	}
	return r
}

// Env 返回工具环境。
func (r *Registry) Env() *Env { return r.env }

// Names 列出全部工具名。
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}

// Lookup 查找工具。
func (r *Registry) Lookup(name string) (*Tool, error) {
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("未知工具: %s", name)
	}
	return t, nil
}

// IsReadOnly 返回工具是否只读。
func (r *Registry) IsReadOnly(name string) bool {
	t, ok := r.tools[name]
	if !ok {
		return false
	}
	return t.ReadOnly
}

// Execute 带安全检查执行一次工具调用。
// 返回 (输出, 是否为工具本身失败的标志，直接由 Execute 返回 error 表示失败)
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any, forceReadOnly bool) (string, error) {
	t, err := r.Lookup(name)
	if err != nil {
		return "", err
	}
	if forceReadOnly && !t.ReadOnly {
		return "", fmt.Errorf("工具 %s 在 plan（只读）模式下不可用", name)
	}
	out, err := t.Execute(ctx, args, r.env)
	if err != nil {
		return "", err
	}
	return out, nil
}

// Definitions 返回全部工具定义（llm.ToolDef）。
func (r *Registry) Definitions() []llm.ToolDef {
	defs := make([]llm.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, llm.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}
	return defs
}

// ReadOnlyDefinitions 返回只读工具定义（plan 模式用）。
func (r *Registry) ReadOnlyDefinitions() []llm.ToolDef {
	var defs []llm.ToolDef
	for _, t := range r.tools {
		if t.ReadOnly {
			defs = append(defs, llm.ToolDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			})
		}
	}
	return defs
}

// resolvePath 将相对/绝对路径解析为 cwd 内的绝对路径；unsafe 时允许越界。
func resolvePath(cwd, p string, unsafe bool) (string, error) {
	if p == "" {
		return "", fmt.Errorf("路径为空")
	}
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			if p == "~" {
				p = home
			} else {
				p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
			}
		}
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, p)
	}
	abs = filepath.Clean(abs)
	if !unsafe {
		base := filepath.Clean(cwd)
		rel, err := filepath.Rel(base, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("路径 %s 在安全工作目录之外，拒绝访问（使用 --unsafe 可放开）", p)
		}
	}
	return abs, nil
}
