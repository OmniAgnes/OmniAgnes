package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func builtinTools() []*Tool {
	return []*Tool{
		{
			Name:        "read",
			Description: "读取文件内容。可指定 offset(起始行，0 基) 与 limit(读取行数)。路径限制在工作目录内。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"path"},
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "文件路径"},
					"offset": map[string]any{"type": "integer", "description": "起始行号（从 0 开始）"},
					"limit":  map[string]any{"type": "integer", "description": "读取的最大行数"},
				},
			},
			ReadOnly: true,
			Execute:  execRead,
		},
		{
			Name:        "write",
			Description: "将 content 完整写入 path（覆盖已有内容）。新建中间目录。仅供 build 模式。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"path", "content"},
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "目标文件路径"},
					"content": map[string]any{"type": "string", "description": "写入内容"},
				},
			},
			ReadOnly: false,
			Execute:  execWrite,
		},
		{
			Name:        "edit",
			Description: "在文件中用 new_string 精确替换 old_string。old_string 必须唯一匹配，否则报错。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"path", "old_string", "new_string"},
				"properties": map[string]any{
					"path":        map[string]any{"type": "string", "description": "文件路径"},
					"old_string":  map[string]any{"type": "string", "description": "需被替换的原始文本"},
					"new_string":  map[string]any{"type": "string", "description": "替换后的文本"},
					"replace_all": map[string]any{"type": "boolean", "description": "是否替换所有匹配"},
				},
			},
			ReadOnly: false,
			Execute:  execEdit,
		},
		{
			Name:        "bash",
			Description: "在工作目录执行 shell 命令，返回 stdout+stderr。默认超时 10 秒，输出会被截断。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"command"},
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "要执行的 shell 命令"},
					"timeout": map[string]any{"type": "integer", "description": "超时秒数（可覆盖默认）"},
				},
			},
			ReadOnly: false,
			Execute:  execBash,
		},
		{
			Name:        "grep",
			Description: "在目录中按正则表达式搜索文本，输出 path:line: content。可指定 include 模式过滤文件（如 *.go）。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"pattern"},
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "正则表达式"},
					"path":    map[string]any{"type": "string", "description": "搜索目录（默认工作目录）"},
					"include": map[string]any{"type": "string", "description": "文件通配模式，逗号分隔，如 *.go,*.md"},
				},
			},
			ReadOnly: true,
			Execute:  execGrep,
		},
		{
			Name:        "glob",
			Description: "根据通配模式列出匹配的文件路径，支持 ** 递归。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"pattern"},
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "通配模式，如 **/*.go"},
				},
			},
			ReadOnly: true,
			Execute:  execGlob,
		},
	}
}

func execRead(ctx context.Context, args map[string]any, env *Env) (string, error) {
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	abs, err := resolvePath(env.Cwd, path, env.Unsafe)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	offset := argInt(args, "offset", 0)
	limit := argInt(args, "limit", 0)
	if offset < 0 {
		offset = 0
	}
	if offset > len(lines) {
		offset = len(lines)
	}
	end := len(lines)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	sel := lines[offset:end]
	body := strings.Join(sel, "\n")
	if env.MaxOutput > 0 && len(body) > env.MaxOutput {
		body = body[:env.MaxOutput] + "\n...（输出截断）"
	}
	return body, nil
}

func execWrite(ctx context.Context, args map[string]any, env *Env) (string, error) {
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	content, _ := args["content"].(string)
	abs, err := resolvePath(env.Cwd, path, env.Unsafe)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	return fmt.Sprintf("已写入 %s（%d 字节）", abs, len(content)), nil
}

func execEdit(ctx context.Context, args map[string]any, env *Env) (string, error) {
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	oldStr, err := argString(args, "old_string")
	if err != nil {
		return "", err
	}
	newStr, _ := args["new_string"].(string)
	replaceAll := argBool(args, "replace_all", false)
	abs, err := resolvePath(env.Cwd, path, env.Unsafe)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	content := string(data)
	if oldStr == "" {
		return "", fmt.Errorf("edit: old_string 不能为空")
	}
	n := strings.Count(content, oldStr)
	if n == 0 {
		return "", fmt.Errorf("edit: 未找到 old_string。请确保与文件内容完全一致（含缩进/换行），可先用 read 查看")
	}
	if n > 1 && !replaceAll {
		return "", fmt.Errorf("edit: old_string 匹配了 %d 处，请给出更多上下文使匹配唯一，或设置 replace_all=true", n)
	}
	var out string
	if replaceAll {
		out = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		out = strings.Replace(content, oldStr, newStr, 1)
	}
	if err := os.WriteFile(abs, []byte(out), 0o644); err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	return fmt.Sprintf("edit: 已替换 %d 处", n), nil
}

func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("缺少参数 %s", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("参数 %s 必须为非空字符串", key)
	}
	return s, nil
}

func argInt(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	}
	return def
}

func argBool(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}
