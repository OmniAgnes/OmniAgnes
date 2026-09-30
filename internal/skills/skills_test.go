package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDirAndOverride(t *testing.T) {
	global := t.TempDir()
	project := t.TempDir()

	write(t, filepath.Join(global, "review", "SKILL.md"), `---
name: review
description: 审查代码质量并给出建议
---

请审查当前改动。`)
	write(t, filepath.Join(global, "broken", "SKILL.md"), "没有 frontmatter，应被跳过")
	write(t, filepath.Join(project, ".omni", "skills", "review", "SKILL.md"), `---
name: review
description: 项目级审查规则
---
项目规则优先。`)

	st := &Store{GlobalDir: global}
	got := st.For(project)
	if len(got) != 1 {
		t.Fatalf("同名技能应被项目级覆盖，实得 %d: %+v", len(got), got)
	}
	if got[0].Source != "project" || got[0].Description != "项目级审查规则" {
		t.Fatalf("覆盖结果不符: %+v", got[0])
	}

	s, ok := st.Lookup(project, "/review")
	if !ok || s.Name != "review" {
		t.Fatalf("Lookup 失败: %+v %v", s, ok)
	}
	if body := s.Body(); body != "项目规则优先。" {
		t.Fatalf("正文不符: %q", body)
	}
}

func TestSplitFrontmatter(t *testing.T) {
	fm, body := splitFrontmatter("---\r\nname: demo\r\ndescription: |\r\n  第一行\r\n  第二行\r\n---\r\n\r\n正文内容\r\n")
	if fm["name"] != "demo" {
		t.Fatalf("name 解析不符: %+v", fm)
	}
	if fm["description"] != "第一行 第二行" {
		t.Fatalf("折叠块解析不符: %q", fm["description"])
	}
	if body != "\n正文内容\n" && body != "正文内容\n" {
		t.Fatalf("正文不符: %q", body)
	}

	// 无 frontmatter 时原样返回
	fm2, body2 := splitFrontmatter("纯正文")
	if len(fm2) != 0 || body2 != "纯正文" {
		t.Fatalf("无 frontmatter 解析不符: %+v %q", fm2, body2)
	}
}

func TestSummary(t *testing.T) {
	out := Summary([]*Skill{{Name: "a", Description: "描述a"}, {Name: "b", Description: "描述b"}})
	if !contains(out, "- a: 描述a") || !contains(out, "- b: 描述b") {
		t.Fatalf("清单格式不符: %q", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// skillhub 等商店的分组布局（<dir>/<组>/<名>/SKILL.md，如 @org/name）也要能被发现。
func TestLoadDirNestedScopeLayout(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "@indiv-ebandao", "dev-expert", "SKILL.md"),
		"---\nname: dev-expert\ndescription: 编程专家\n---\n\n正文")
	write(t, filepath.Join(dir, "plain", "SKILL.md"),
		"---\nname: plain\ndescription: 普通技能\n---\n\n正文")

	found, warnings := LoadDir(dir, "global")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(found) != 2 {
		t.Fatalf("found = %d, want 2", len(found))
	}
	byName := map[string]*Skill{}
	for _, s := range found {
		byName[s.Name] = s
	}
	if byName["dev-expert"] == nil || byName["plain"] == nil {
		t.Fatalf("missing skills: %+v", found)
	}
	if !strings.Contains(filepath.ToSlash(byName["dev-expert"].Path), "@indiv-ebandao/dev-expert/SKILL.md") {
		t.Fatalf("nested path = %s", byName["dev-expert"].Path)
	}
}
