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
		{
			Name: "todo",
			Description: "维护一份多步骤任务清单，让用户实时了解你处理到哪一步。" +
				"接到较复杂任务时先建立清单；每开始或完成一步都再次调用它提交**整份**清单（全量覆盖，非增量）。" +
				"同一时刻最多一项 in_progress，其余 pending 或 completed。简单一步到位的任务无需使用。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"items"},
				"properties": map[string]any{
					"items": map[string]any{
						"type":        "array",
						"description": "任务项数组（整份清单，全量覆盖）",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"content", "status"},
							"properties": map[string]any{
								"content":     map[string]any{"type": "string", "description": "任务描述（祈使句，如“修复登录接口”）"},
								"active_form": map[string]any{"type": "string", "description": "进行中的描述（如“正在修复登录接口”）"},
								"status": map[string]any{
									"type":        "string",
									"enum":        []string{"pending", "in_progress", "completed"},
									"description": "pending=待办，in_progress=进行中，completed=已完成",
								},
							},
						},
					},
				},
			},
			ReadOnly: true,
			Execute:  execTodo,
		},
		{
			Name: "ask",
			Description: "向用户提一个澄清问题并等待其回答。仅在歧义会实质影响结果、无法合理假设时使用；" +
				"一次最多问一个问题，可提供 2-4 个候选项。拿到回答后再继续。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"question"},
				"properties": map[string]any{
					"question": map[string]any{"type": "string", "description": "要问用户的问题（简洁明了）"},
					"options": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "候选项（可选，2-4 个）；用户也可自由输入",
					},
				},
			},
			ReadOnly: true,
			// 由 Agent 在执行循环中拦截并向 UI 要答案；此兜底仅在界面不支持提问时返回提示。
			Execute: func(_ context.Context, _ map[string]any, _ *Env) (string, error) {
				return "（当前界面不支持向用户提问，请按最合理的假设继续，并在回复中说明所做的假设）", nil
			},
		},
	}
}

// execTodo 无副作用：仅确认清单已更新（真实渲染由前端从工具参数读取）。
func execTodo(_ context.Context, args map[string]any, _ *Env) (string, error) {
	raw, ok := args["items"]
	if !ok {
		return "", fmt.Errorf("todo: 缺少 items")
	}
	list, ok := raw.([]any)
	if !ok {
		return "", fmt.Errorf("todo: items 需为数组")
	}
	var done, doing int
	for _, it := range list {
		m, _ := it.(map[string]any)
		st, _ := m["status"].(string)
		switch st {
		case "completed":
			done++
		case "in_progress":
			doing++
		}
	}
	return fmt.Sprintf("任务清单已更新（共 %d 项，已完成 %d，进行中 %d）", len(list), done, doing), nil
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
	// 区分新建 / 覆盖（供摘要文案；差异渲染由 webui 在写入前抓取旧内容完成）
	_, statErr := os.Stat(abs)
	existed := statErr == nil
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	verb := "已新建"
	if existed {
		verb = "已覆盖写入"
	}
	return fmt.Sprintf("%s %s（%d 字节）", verb, abs, len(content)), nil
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
