package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// gitBinOverride 供测试注入 git 可执行路径（Windows 上用假 git 脚本时）。
var gitBinOverride string

// gitBin 返回 git 可执行文件。非 Windows 直接用 git；Windows 从 PATH 找，
// 找不到时回落到 Git 安装目录。
func gitBin() string {
	if gitBinOverride != "" {
		return gitBinOverride
	}
	if runtime.GOOS != "windows" {
		return "git"
	}
	if p, err := exec.LookPath("git"); err == nil {
		return p
	}
	return "git"
}

// runGit 在 env.Cwd 里执行 git 命令，返回合并输出（stdout+stderr）。
// 做 UTF-8 兜底转码（复用 bash.go 的 toUTF8）；关闭凭据交互，避免无 stdin 时挂起。
func runGit(cmdArgs []string, env *Env) (string, error) {
	cmd := exec.Command(gitBin(), cmdArgs...)
	cmd.Dir = env.Cwd
	cmd.Env = append([]string{}, os.Environ()...)
	if !hasEnvKey(cmd.Env, "LANG") {
		cmd.Env = append(cmd.Env, "LANG=C.UTF-8")
	}
	if !hasEnvKey(cmd.Env, "GIT_TERMINAL_PROMPT") {
		// 关键：git 需要凭据交互时不要阻塞等待输入（工具没有 stdin，否则挂到超时）。
		// 已配置的 credential helper（wincred 等）不受影响。
		cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0")
	}
	outBytes, err := cmd.CombinedOutput()
	out := toUTF8(outBytes)
	if err != nil {
		return out, fmt.Errorf("git %s: %w", strings.Join(cmdArgs, " "), err)
	}
	return out, nil
}

// trimOut 去掉命令输出两端空白；空输出返回提示文案。
func trimOut(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return "（无输出）"
	}
	return out
}

// gitOp 描述一个 git 子命令：build 把 op + 参数拼成 git 参数数组（不含 git 本身）。
type gitOp struct {
	name  string
	desc  string
	build func(args map[string]any) ([]string, error)
}

// gitReadOnlyOps 只读、可在 plan 模式放开的操作。
var gitReadOnlyOps = []gitOp{
	{
		name: "status", desc: "简略状态（git status -sb）",
		build: func(map[string]any) ([]string, error) { return []string{"status", "-sb"}, nil },
	},
	{
		name: "log", desc: "最近提交日志，n 控制条数（默认 10）",
		build: func(a map[string]any) ([]string, error) {
			n := argInt(a, "n", 10)
			if n < 1 {
				n = 10
			}
			return []string{"log", "--oneline", "-n", fmt.Sprintf("%d", n)}, nil
		},
	},
	{
		name: "branch", desc: "本地分支列表",
		build: func(map[string]any) ([]string, error) { return []string{"branch", "--list"}, nil },
	},
	{
		name: "remote", desc: "远程列表（git remote -v）",
		build: func(map[string]any) ([]string, error) { return []string{"remote", "-v"}, nil },
	},
	{
		name: "diff", desc: "工作区相对 HEAD 的差异文本",
		build: func(map[string]any) ([]string, error) { return []string{"diff", "--no-ext-diff", "HEAD"}, nil },
	},
	{
		name: "diff_stat", desc: "差异统计（改动文件 + 增删行数）",
		build: func(map[string]any) ([]string, error) { return []string{"diff", "--stat", "HEAD"}, nil },
	},
	{
		name: "staged", desc: "已暂存更改（git diff --cached --stat）",
		build: func(map[string]any) ([]string, error) { return []string{"diff", "--cached", "--stat"}, nil },
	},
	{
		name: "show", desc: "查看某次提交，commit=分支名/哈希（默认 HEAD）",
		build: func(a map[string]any) ([]string, error) {
			c := ""
			if v, ok := a["commit"].(string); ok {
				c = strings.TrimSpace(v)
			}
			if c == "" {
				c = "HEAD"
			}
			return []string{"show", "--no-ext-diff", "--stat", c}, nil
		},
	},
}

// gitWriteOps 可写操作，plan 模式下需审批，build 模式直接执行。
var gitWriteOps = []gitOp{
	{
		name: "add", desc: "暂存文件：paths 目标文件/目录列表",
		build: func(a map[string]any) ([]string, error) {
			paths, err := argStringList(a, "paths")
			if err != nil {
				return nil, err
			}
			return append([]string{"add", "--"}, paths...), nil
		},
	},
	{
		name: "commit", desc: "提交暂存区（需 message），完成后可用 push 推送",
		build: func(a map[string]any) ([]string, error) {
			msg, ok := a["message"].(string)
			if !ok || strings.TrimSpace(msg) == "" {
				return nil, fmt.Errorf("commit: 需要 message 参数（非空）")
			}
			return []string{"commit", "-m", msg}, nil
		},
	},
	{
		name: "push", desc: "推送到远程当前分支的上游",
		build: func(map[string]any) ([]string, error) { return []string{"push"}, nil },
	},
	{
		name: "restore", desc: "放弃工作区改动：paths 目标文件列表",
		build: func(a map[string]any) ([]string, error) {
			paths, err := argStringList(a, "paths")
			if err != nil {
				return nil, err
			}
			return append([]string{"restore", "--"}, paths...), nil
		},
	},
}

func argStringList(args map[string]any, key string) ([]string, error) {
	v, ok := args[key]
	if !ok {
		return nil, fmt.Errorf("缺少参数 %s（至少一个字符串）", key)
	}
	// 兼容两种调用形态：真实 JSON 传的是 []any，测试/内部直接传 []string。
	var list []string
	switch raw := v.(type) {
	case []string:
		list = raw
	case []any:
		list = make([]string, 0, len(raw))
		for _, it := range raw {
			if s, ok := it.(string); ok {
				list = append(list, s)
			}
		}
	default:
		return nil, fmt.Errorf("参数 %s 需为字符串数组", key)
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("参数 %s 不能为空", key)
	}
	return out, nil
}

func opNames(ops []gitOp) []string {
	out := make([]string, 0, len(ops))
	for _, g := range ops {
		out = append(out, g.name)
	}
	return out
}

// makeGitTool 生成一个 git 工具。ops 决定 op 白名单；readOnly 决定 plan 模式是否放开。
func makeGitTool(name string, ops []gitOp, readOnly bool, intro string) *Tool {
	return &Tool{
		Name:     name,
		ReadOnly: readOnly,
		Description: "调用本机 git 操作仓库。" + intro,
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"op"},
			"properties": map[string]any{
				"op": map[string]any{
					"type":        "string",
					"enum":        opNames(ops),
					"description": "要执行的 git 子命令",
				},
				"paths":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "add/restore：目标文件/目录列表"},
				"message": map[string]any{"type": "string", "description": "commit：提交信息"},
				"commit":  map[string]any{"type": "string", "description": "show：分支名/哈希（默认 HEAD）"},
				"n":       map[string]any{"type": "integer", "description": "log：条数（默认 10）"},
			},
		},
		Execute: func(_ context.Context, args map[string]any, env *Env) (string, error) {
			op, err := argString(args, "op")
			if err != nil {
				return "", err
			}
			for _, g := range ops {
				if g.name == op {
					gitArgs, err := g.build(args)
					if err != nil {
						return "", err
					}
					out, err := runGit(gitArgs, env)
					if err != nil {
						return out, err
					}
					return trimOut(out), nil
				}
			}
			return "", fmt.Errorf("git: 未知 op %q", op)
		},
	}
}