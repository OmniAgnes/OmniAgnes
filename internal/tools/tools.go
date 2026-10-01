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
	// ExecuteMedia 可选：产出多媒体的工具（截图/读图/生图/生视频/语音合成）用它
	// 把图片、视频、音频一并交给 UI 渲染。设置后 Registry.ExecuteWithMedia 优先走这里。
	// 其中图片类附件还会作为多模态输入送进模型（见 agent）。
	ExecuteMedia func(ctx context.Context, args map[string]any, env *Env) (string, []llm.Attachment, error)
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

// Register 动态注册一个工具（MCP、skill 等扩展用），同名覆盖。
func (r *Registry) Register(t *Tool) {
	if t == nil || t.Name == "" {
		return
	}
	r.tools[t.Name] = t
}

// UnregisterByPrefix 删除所有名字以 prefix 开头的工具（刷新 MCP 工具列表用）。
func (r *Registry) UnregisterByPrefix(prefix string) {
	for name := range r.tools {
		if strings.HasPrefix(name, prefix) {
			delete(r.tools, name)
		}
	}
}

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
	out, _, err := r.ExecuteWithMedia(ctx, name, args, forceReadOnly)
	return out, err
}

// ExecuteWithMedia 执行工具并一并返回多媒体附件（截图/读图/生图类工具用）。
// 大多数工具不产多媒体，此时 media 为 nil，行为与 Execute 完全一致。
func (r *Registry) ExecuteWithMedia(ctx context.Context, name string, args map[string]any, forceReadOnly bool) (string, []llm.Attachment, error) {
	t, err := r.Lookup(name)
	if err != nil {
		return "", nil, err
	}
	if forceReadOnly && !t.ReadOnly {
		return "", nil, fmt.Errorf("工具 %s 在 plan（只读）模式下不可用", name)
	}
	// 产多媒体的工具走可选通道
	if t.ExecuteMedia != nil {
		return t.ExecuteMedia(ctx, args, r.env)
	}
	out, err := t.Execute(ctx, args, r.env)
	if err != nil {
		return "", nil, err
	}
	return out, nil, nil
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

// windowsReservedNames 是 Windows 的保留设备名：这些名字无法作为普通文件/目录名使用，
// 写入时会静默落到设备上（历史上 `> nul` 就会凭空造出一个名为 nul 的空文件）。
// 匹配时忽略大小写与扩展名（nul / NUL / nul.txt 都算）。
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// checkReservedName 拒绝 Windows 保留设备名路径。返回可读的错误说明。
func checkReservedName(abs string) error {
	base := filepath.Base(abs)
	if dot := strings.IndexByte(base, '.'); dot > 0 {
		base = base[:dot] // 去掉扩展名：nul.txt同样非法
	}
	if windowsReservedNames[strings.ToLower(base)] {
		return fmt.Errorf("路径名 %q 是 Windows 保留设备名，不能作为文件使用；丢弃命令输出请用 /dev/null，临时产物请写到 .omni/log/", filepath.Base(abs))
	}
	return nil
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
	if err := checkReservedName(abs); err != nil {
		return "", err
	}
	if !unsafe {
		base := filepath.Clean(cwd)
		rel, err := filepath.Rel(base, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("路径 %s 在安全工作目录之外，拒绝访问（使用 --unsafe 可放开）", p)
		}
	}
	return abs, nil
}
