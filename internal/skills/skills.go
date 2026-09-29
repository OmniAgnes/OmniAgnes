// Package skills 加载与管理技能（SKILL.md）。
// 技能是带 frontmatter（name/description）的 Markdown 指令文件：
// 全局技能在 ~/.config/omni/skills/<name>/SKILL.md，项目技能在 <项目>/.omni/skills/<name>/SKILL.md，
// 项目级覆盖同名全局技能。
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill 是一个技能定义。
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`   // SKILL.md 绝对路径
	Source      string `json:"source"` // global | project
}

// Store 按目录加载技能。
type Store struct {
	GlobalDir string
}

// For 返回某项目目录下的可用技能（全局 + 项目级，项目级覆盖同名）。
func (st *Store) For(projectDir string) []*Skill {
	byName := map[string]*Skill{}
	for _, dir := range []struct {
		dir    string
		source string
	}{
		{st.GlobalDir, "global"},
		{filepath.Join(projectDir, ".omni", "skills"), "project"},
	} {
		found, _ := LoadDir(dir.dir, dir.source)
		for _, s := range found {
			byName[s.Name] = s
		}
	}
	out := make([]*Skill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Lookup 在项目目录下按名查找技能。
func (st *Store) Lookup(projectDir, name string) (*Skill, bool) {
	name = strings.TrimPrefix(name, "/")
	for _, s := range st.For(projectDir) {
		if s.Name == name {
			return s, true
		}
	}
	return nil, false
}

// Body 读取技能正文（SKILL.md 去掉 frontmatter 后的内容）。
func (s *Skill) Body() string {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return ""
	}
	_, body := splitFrontmatter(string(data))
	return strings.TrimSpace(body)
}

// LoadDir 扫描一个技能目录：每个直接子目录里的 SKILL.md 视为一个技能。
// frontmatter 缺 name 或 description 的技能跳过（返回 warning，不中断）。
func LoadDir(dir, source string) ([]*Skill, []string) {
	var out []*Skill
	var warnings []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fm, _ := splitFrontmatter(string(data))
		name := strings.TrimSpace(fm["name"])
		desc := strings.TrimSpace(fm["description"])
		if name == "" || desc == "" {
			warnings = append(warnings, fmt.Sprintf("技能 %s 缺少 name 或 description，已跳过", path))
			continue
		}
		out = append(out, &Skill{Name: name, Description: desc, Path: path, Source: source})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, warnings
}

// splitFrontmatter 解析文件开头的 `---` 包裹的 key: value 区块。
// 支持 `|` / `>` 折叠块。返回 (frontmatter, 正文)。
func splitFrontmatter(text string) (map[string]string, string) {
	fm := map[string]string{}
	norm := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(norm, "---\n") {
		return fm, norm
	}
	rest := norm[4:]
	var block, body string
	if i := strings.Index(rest, "\n---\n"); i >= 0 {
		block, body = rest[:i], rest[i+5:]
	} else if strings.HasSuffix(rest, "\n---") {
		block, body = strings.TrimSuffix(rest, "\n---"), ""
	} else {
		return fm, norm
	}
	var lastKey string
	for _, line := range strings.Split(block, "\n") {
		switch {
		case strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t"):
			if lastKey != "" {
				fm[lastKey] += " " + strings.TrimSpace(line)
			}
		default:
			key, val, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			val = strings.TrimSpace(val)
			if val == "|" || val == ">-" || val == ">" {
				lastKey = key
				fm[key] = ""
				continue
			}
			lastKey = key
			fm[key] = strings.Trim(val, `"'`)
		}
	}
	for k := range fm {
		fm[k] = strings.TrimSpace(fm[k])
	}
	return fm, body
}

// Summary 生成给系统提示词看的技能清单。
func Summary(skills []*Skill) string {
	var b strings.Builder
	b.WriteString("用户定义了以下技能。当任务匹配某技能的描述时，调用 skill 工具（参数 name）读取其完整指令并遵循执行；用户也可能直接用 /技能名 触发。\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
	}
	return b.String()
}
