// Package extras 把 MCP、技能与全局记忆三类扩展聚合起来，
// 供 TUI 与 webui 以一致方式注册工具和构建系统提示词。
package extras

import (
	"context"
	"strings"

	"omni/internal/mcp"
	"omni/internal/memory"
	"omni/internal/skills"
	"omni/internal/tools"
)

// Extras 聚合运行时扩展。MCP 管理器全局共享；技能与记忆按目录/文件加载。
type Extras struct {
	MCP        *mcp.Manager
	Skills     *skills.Store
	MemoryPath string
}

// RegisterTools 向注册表注册扩展工具：skill（只读）、memory、以及 MCP 全部工具。
// projectDir 决定项目级技能目录（<projectDir>/.omni/skills）。
func (e *Extras) RegisterTools(reg *tools.Registry, projectDir string) {
	if e == nil || reg == nil {
		return
	}
	if e.Skills != nil {
		reg.Register(e.skillTool(projectDir))
	}
	if e.MemoryPath != "" {
		reg.Register(memory.Tool(e.MemoryPath))
	}
	if e.MCP != nil {
		e.MCP.RegisterInto(reg)
	}
}

// RefreshMCPTools 在 MCP 工具列表变化时重新注册 MCP 工具（不影响其他工具）。
func (e *Extras) RefreshMCPTools(reg *tools.Registry) {
	if e == nil || e.MCP == nil || reg == nil {
		return
	}
	e.MCP.RegisterInto(reg)
}

// ExtraPrompt 构建系统提示词的动态附加段（技能清单 + 全局记忆）。
// 返回空串表示没有附加内容。
func (e *Extras) ExtraPrompt(projectDir string) string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	if e.MemoryPath != "" {
		if mem := memory.Load(e.MemoryPath); mem != "" {
			b.WriteString("\n\n## 全局记忆\n以下是跨会话保存的长期记忆，回答时参考。可用 memory 工具更新（用户偏好、项目背景、重要约定等）：\n\n")
			b.WriteString(mem)
		}
	}
	if e.Skills != nil {
		if sks := e.Skills.For(projectDir); len(sks) > 0 {
			b.WriteString("\n\n## 可用技能\n")
			b.WriteString(skills.Summary(sks))
		}
	}
	return b.String()
}

// ExpandSlash 把 "/技能名" 展开为技能正文组成的用户消息；不匹配时原样返回。
func (e *Extras) ExpandSlash(projectDir, text string) (string, bool) {
	if e == nil || e.Skills == nil || !strings.HasPrefix(text, "/") {
		return text, false
	}
	name := strings.Fields(text)[0]
	s, ok := e.Skills.Lookup(projectDir, name)
	if !ok {
		return text, false
	}
	body := s.Body()
	if body == "" {
		return text, false
	}
	return "请使用技能 " + s.Name + " 执行以下指令：\n\n" + body, true
}

func (e *Extras) skillTool(projectDir string) *tools.Tool {
	return &tools.Tool{
		Name:        "skill",
		Description: "读取一个用户技能的完整指令。当系统提示词的「可用技能」清单与当前任务匹配时调用；参数 name 为技能名。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "技能名（见可用技能清单）",
				},
			},
			"required": []string{"name"},
		},
		ReadOnly: true,
		Execute: func(ctx context.Context, args map[string]any, env *tools.Env) (string, error) {
			name, _ := args["name"].(string)
			if strings.TrimSpace(name) == "" {
				return "", errSkill("缺少 name 参数")
			}
			s, ok := e.Skills.Lookup(projectDir, name)
			if !ok {
				return "", errSkill("未知技能: " + name)
			}
			body := s.Body()
			if body == "" {
				return "", errSkill("技能 " + name + " 内容为空")
			}
			return body, nil
		},
	}
}

type skillErr string

func (e skillErr) Error() string { return string(e) }

func errSkill(msg string) error { return skillErr(msg) }
