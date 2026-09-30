// Package skills 加载与管理技能（SKILL.md）。
// 技能是带 frontmatter（name/description）的 Markdown 指令文件：
// 全局技能在 ~/.config/omni/skills/<name>/SKILL.md，项目技能在 <项目>/.omni/skills/<name>/SKILL.md，
// 项目级覆盖同名全局技能。
package skills

import (
	"fmt"
	"io/fs"
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

// LoadDir 扫描一个技能目录：<dir>/<名>/SKILL.md 视为一个技能；
// 兼容 skillhub 的分组布局 <dir>/<组>/<名>/SKILL.md（如 @org/name）——
// 子目录下没有 SKILL.md 时，再往下扫一层。
// frontmatter 缺 name 或 description 的技能跳过（返回 warning，不中断）。
func LoadDir(dir, source string) ([]*Skill, []string) {
	var out []*Skill
	var warnings []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	add := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		fm, _ := splitFrontmatter(string(data))
		name := strings.TrimSpace(fm["name"])
		desc := strings.TrimSpace(fm["description"])
		if name == "" || desc == "" {
			warnings = append(warnings, fmt.Sprintf("技能 %s 缺少 name 或 description，已跳过", path))
			return
		}
		out = append(out, &Skill{Name: name, Description: desc, Path: path, Source: source})
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(sub, "SKILL.md")); err == nil {
			add(filepath.Join(sub, "SKILL.md"))
			continue
		}
		// 第二层：skillhub 的 @组/技能名 布局
		subs, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, se := range subs {
			if !se.IsDir() {
				continue
			}
			add(filepath.Join(sub, se.Name(), "SKILL.md"))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, warnings
}

// Parse 解析单个 SKILL.md 文件（要求 name/description 齐全）。
func Parse(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fm, _ := splitFrontmatter(string(data))
	name := strings.TrimSpace(fm["name"])
	desc := strings.TrimSpace(fm["description"])
	if name == "" || desc == "" {
		return nil, fmt.Errorf("SKILL.md 缺少 name 或 description: %s", path)
	}
	return &Skill{Name: name, Description: desc, Path: path, Source: "global"}, nil
}

// Resolve 在技能目录里按技能名定位其实际所在目录（返回「包含 <名>/SKILL.md」的目录）。
// 兼容 @组/名 嵌套布局：商店装的技能不在根目录一层，平铺拼接会找不到。
func Resolve(dir, name string) (string, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	found, _ := LoadDir(dir, "")
	for _, sk := range found {
		if sk.Name == name {
			return filepath.Dir(filepath.Dir(sk.Path)), nil
		}
	}
	return "", fmt.Errorf("技能不存在: %s", name)
}

// SeedBuiltIn 把内置技能（go:embed 打进二进制的 builtinFS）补齐到 dir（全局技能目录）。
// 幂等：已存在且内容一致的条目跳过，缺失或变化才写入；不删不覆盖用户改动。
// embed 缺失（未生成）时静默跳过，不阻断启动。
func SeedBuiltIn(dir string) error {
	if dir == "" {
		return nil
	}
	sub, err := fs.Sub(builtinFS, "embedded")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	roots, err := fs.ReadDir(sub, ".")
	if err != nil {
		return err
	}
	seeded := 0
	for _, e := range roots {
		if !e.IsDir() {
			continue
		}
		root := e.Name() // 技能根目录：<名> 或 @组
		// 该技能根目录里已有 SKILL.md（任一层级）就不动，避免覆盖用户改过的内容
		if dirHasSkillMD(filepath.Join(dir, root)) {
			continue
		}
		seeded++
		if err := walkFS(sub, root, func(rel string, info fs.DirEntry) error {
			target := filepath.Join(dir, root, filepath.FromSlash(rel))
			if info.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			data, err := fs.ReadFile(sub, filepath.ToSlash(root+"/"+rel))
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.WriteFile(target, data, 0o644)
		}); err != nil {
			return err
		}
	}
	if seeded > 0 {
		fmt.Fprintf(os.Stderr, "omni: 已补齐 %d 个内置技能到 %s\n", seeded, dir)
	}
	return nil
}

// dirHasSkillMD 检查 dir 下（第一、二层）是否已有 SKILL.md。
func dirHasSkillMD(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil {
		return true
	}
	sub, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range sub {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
				return true
			}
		}
	}
	return false
}

// walkFS 遍历 fs 子树 dir/... 下的每个条目（相对路径），回调报错则中断。
func walkFS(root fs.FS, dir string, fn func(rel string, info fs.DirEntry) error) error {
	return fs.WalkDir(root, dir, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, dir+"/")
		return fn(rel, info)
	})
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

// HasFrontmatter 判断文本是否已带 `---` frontmatter 头。
func HasFrontmatter(text string) bool {
	norm := strings.ReplaceAll(text, "\r\n", "\n")
	return strings.HasPrefix(norm, "---\n")
}

// Import 把一段完整的 SKILL.md 文本写入 <dir>/<name>/SKILL.md。
// 文本自带 frontmatter 时原样落盘（保真），返回解析出的 name/description；
// 同名已存在则覆盖（用户明确是「粘贴导入」，覆盖比报错更符合预期）。
func Import(dir, content string) (string, string, error) {
	fm, body := splitFrontmatter(content)
	name := strings.TrimSpace(fm["name"])
	if name == "" {
		name = GuessName(body)
	}
	if name == "" {
		return "", "", fmt.Errorf("无法从内容中解析技能名，请在导入时手动填写名称")
	}
	if err := validName(name); err != nil {
		return "", "", err
	}
	desc := strings.TrimSpace(fm["description"])
	if desc == "" {
		desc = GuessDescription(body)
	}
	if desc == "" {
		desc = name
	}
	// 补齐缺失字段，保证落盘后是合法技能
	normalized := render(name, desc, body)
	path := filepath.Join(dir, name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(path, []byte(normalized), 0o644); err != nil {
		return "", "", err
	}
	return name, desc, nil
}

// GuessName 从正文猜测技能名：优先一级标题，否则取首个非空行的前若干字符。
func GuessName(body string) string {
	norm := strings.ReplaceAll(body, "\r\n", "\n")
	for _, line := range strings.Split(norm, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "#") {
			t = strings.TrimSpace(strings.TrimLeft(t, "#"))
			if t = sanitizeName(t); t != "" {
				return t
			}
			continue
		}
		// 正文第一行不是标题就不猜了，避免生成奇怪的名字
		return ""
	}
	return ""
}

// GuessDescription 取正文首个非标题、非空行作为描述。
func GuessDescription(body string) string {
	_, b := splitFrontmatter(body)
	for _, line := range strings.Split(strings.ReplaceAll(b, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "---") {
			continue
		}
		if len([]rune(t)) > 120 {
			t = string([]rune(t)[:120]) + "…"
		}
		return t
	}
	return ""
}

// sanitizeName 把任意标题转成合法技能名：小写、非字母数字换成连字符、去首尾连字符。
func sanitizeName(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			// 中日韩等非 ASCII 字符无法作为目录名，退化为分隔符
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = strings.Trim(out[:64], "-")
	}
	return out
}
