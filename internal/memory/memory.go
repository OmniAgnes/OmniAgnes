// Package memory 管理全局记忆文件（跨会话长期记忆），
// 并提供让 agent 自主读写记忆的内置工具。
package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"omni/internal/tools"
)

// Load 读取记忆内容；文件不存在或读失败返回空串。
func Load(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Append 追加一段记忆。
func Append(path, content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("记忆内容为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	existing := Load(path)
	if existing != "" {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(strings.TrimSpace(content) + "\n")
	return err
}

// Replace 整体替换记忆内容。
func Replace(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644)
}

// Tool 构造 memory 内置工具（非只读，plan 模式不可用）。
func Tool(path string) *tools.Tool {
	return &tools.Tool{
		Name: "memory",
		Description: "读写你的长期记忆（跨会话保留）。适合记录用户偏好、项目背景、常见约定等。" +
			"mode 为 append（追加，默认）或 replace（整体替换）。使用 replace 前应先用当前记忆内容确认，避免误删。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mode": map[string]any{
					"type":        "string",
					"enum":        []string{"append", "replace"},
					"description": "append=追加一段记忆；replace=整体替换全部记忆",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "记忆内容（Markdown）",
				},
			},
			"required": []string{"content"},
		},
		Execute: func(ctx context.Context, args map[string]any, env *tools.Env) (string, error) {
			mode := strings.ToLower(argString(args, "mode"))
			if mode == "" {
				mode = "append"
			}
			content := argString(args, "content")
			switch mode {
			case "append":
				if err := Append(path, content); err != nil {
					return "", err
				}
				return "已追加记忆。", nil
			case "replace":
				if err := Replace(path, content); err != nil {
					return "", err
				}
				return "已替换全部记忆。", nil
			default:
				return "", fmt.Errorf("未知 mode: %s（可选 append / replace）", mode)
			}
		},
	}
}

func argString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}
