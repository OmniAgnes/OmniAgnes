// Package memory 管理记忆文件（跨会话长期记忆）：
// 全局记忆单文件；项目记忆在 <项目>/.omni/memory/ 下，分每日（YYYY-MM-DD.md）与总记忆（MEMORY.md）。
// 并提供让 agent 自主读写记忆的内置工具。
package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"omni/internal/tools"
)

// ProjectTotalFile 是项目总记忆的文件名；每日记忆按日期命名（如 2026-10-01.md）。
const ProjectTotalFile = "MEMORY.md"

// DailyPath 返回项目今日记忆文件路径（<project>/.omni/memory/YYYY-MM-DD.md）。
func DailyPath(projectDir string) string {
	return filepath.Join(projectDir, ".omni", "memory", time.Now().Format("2006-01-02")+".md")
}

// TotalPath 返回项目总记忆文件路径（<project>/.omni/memory/MEMORY.md）。
func TotalPath(projectDir string) string {
	return filepath.Join(projectDir, ".omni", "memory", ProjectTotalFile)
}

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

// appendDated 追加记忆并带上时间前缀（每日记忆用，便于按时间回溯）。
func appendDated(path, content string) error {
	return Append(path, fmt.Sprintf("- [%s] %s", time.Now().Format("15:04"), strings.TrimSpace(content)))
}

// Tool 构造 memory 内置工具（非只读，plan 模式不可用）。
// globalPath 为全局记忆文件；projectDir 非空时启用项目记忆（每日 + 总记忆）。
func Tool(globalPath, projectDir string) *tools.Tool {
	hasGlobal := globalPath != ""
	hasProject := strings.TrimSpace(projectDir) != ""
	scopes := []string{}
	if hasProject {
		scopes = append(scopes, "day", "total")
	}
	if hasGlobal {
		scopes = append(scopes, "global")
	}
	return &tools.Tool{
		Name: "memory",
		Description: "读写长期记忆（跨会话保留）。scope：day=项目今日记忆（默认，带时间追加）；" +
			"total=项目总记忆（精炼的长期约定/背景）；global=全局记忆（跨项目的用户偏好）。" +
			"mode 为 append（追加，默认）或 replace（整体替换，replace 前应先确认现有内容）。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scope": map[string]any{
					"type":        "string",
					"enum":        scopes,
					"description": "记忆范围：day=项目今日 / total=项目总记忆 / global=全局",
				},
				"mode": map[string]any{
					"type":        "string",
					"enum":        []string{"append", "replace"},
					"description": "append=追加一段记忆；replace=整体替换",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "记忆内容（Markdown）",
				},
			},
			"required": []string{"content"},
		},
		Execute: func(ctx context.Context, args map[string]any, env *tools.Env) (string, error) {
			scope := strings.ToLower(argString(args, "scope"))
			if scope == "" && hasProject {
				scope = "day"
			} else if scope == "" {
				scope = "global"
			}
			mode := strings.ToLower(argString(args, "mode"))
			if mode == "" {
				mode = "append"
			}
			content := argString(args, "content")

			var path string
			var dated bool
			switch scope {
			case "day":
				if !hasProject {
					return "", fmt.Errorf("当前无项目目录，day 记忆不可用")
				}
				path = filepath.Join(projectDir, ".omni", "memory", time.Now().Format("2006-01-02")+".md")
				dated = true
			case "total":
				if !hasProject {
					return "", fmt.Errorf("当前无项目目录，total 记忆不可用")
				}
				path = filepath.Join(projectDir, ".omni", "memory", ProjectTotalFile)
			case "global":
				if !hasGlobal {
					return "", fmt.Errorf("全局记忆未配置")
				}
				path = globalPath
			default:
				return "", fmt.Errorf("未知 scope: %s（可选 %s）", scope, strings.Join(scopes, " / "))
			}

			var err error
			switch mode {
			case "append":
				if dated {
					err = appendDated(path, content)
				} else {
					err = Append(path, content)
				}
			case "replace":
				err = Replace(path, content)
			default:
				return "", fmt.Errorf("未知 mode: %s（可选 append / replace）", mode)
			}
			if err != nil {
				return "", err
			}
			return "已写入记忆（" + scope + "）。", nil
		},
	}
}

func argString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}
