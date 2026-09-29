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

// ---------- 技能增删改（供 webui 设置界面使用） ----------

// DirFor 返回技能所在目录：source=global 用全局目录；source=project 用 <projectDir>/.omni/skills。
func (st *Store) DirFor(source, projectDir string) (string, error) {
	switch source {
	case "", "global":
		if st.GlobalDir == "" {
			return "", fmt.Errorf("全局技能目录未配置")
		}
		return st.GlobalDir, nil
	case "project":
		if projectDir == "" {
			return "", fmt.Errorf("未指定项目目录")
		}
		return filepath.Join(projectDir, ".omni", "skills"), nil
	default:
		return "", fmt.Errorf("未知技能来源: %s", source)
	}
}

// validName 校验技能名（同时用作目录名，禁止路径穿越与非法字符）。
func validName(name string) error {
	name = strings.TrimSpace(strings.TrimPrefix(name, "/"))
	if name == "" {
		return fmt.Errorf("技能名不能为空")
	}
	if name == "." || name == ".." || len(name) > 64 {
		return fmt.Errorf("非法技能名: %q", name)
	}
	if strings.ContainsAny(name, `/\:*?"<>|`) {
		return fmt.Errorf("技能名不能包含路径或非法字符: %q", name)
	}
	return nil
}

// Read 读取技能原始 SKILL.md 文本。
func Read(dir, name string) (string, error) {
	if err := validName(name); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, strings.TrimPrefix(name, "/"), "SKILL.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("技能不存在: %s", name)
		}
		return "", err
	}
	return string(data), nil
}

// render 组装带 frontmatter 的 SKILL.md 内容。
func render(name, description, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", name)
	fmt.Fprintf(&b, "description: %s\n", strings.ReplaceAll(strings.TrimSpace(description), "\n", " "))
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}

// Create 在 dir 下新建技能（<dir>/<name>/SKILL.md），同名已存在时报错。
func Create(dir, name, description, body string) (*Skill, error) {
	name = strings.TrimSpace(strings.TrimPrefix(name, "/"))
	if err := validName(name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(description) == "" {
		return nil, fmt.Errorf("技能描述不能为空")
	}
	path := filepath.Join(dir, name, "SKILL.md")
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("技能已存在: %s", name)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(render(name, description, body)), 0o644); err != nil {
		return nil, err
	}
	return &Skill{Name: name, Description: strings.TrimSpace(description), Path: path, Source: "custom"}, nil
}

// Update 覆盖技能内容（技能名与目录保持不变）。
func Update(dir, name, description, body string) error {
	name = strings.TrimSpace(strings.TrimPrefix(name, "/"))
	if err := validName(name); err != nil {
		return err
	}
	if strings.TrimSpace(description) == "" {
		return fmt.Errorf("技能描述不能为空")
	}
	path := filepath.Join(dir, name, "SKILL.md")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("技能不存在: %s", name)
		}
		return err
	}
	return os.WriteFile(path, []byte(render(name, description, body)), 0o644)
}

// Delete 删除技能目录（含其下所有文件）。
func Delete(dir, name string) error {
	name = strings.TrimSpace(strings.TrimPrefix(name, "/"))
	if err := validName(name); err != nil {
		return err
	}
	target := filepath.Join(dir, name)
	if st, err := os.Stat(target); err != nil || !st.IsDir() {
		return fmt.Errorf("技能不存在: %s", name)
	}
	return os.RemoveAll(target)
}
