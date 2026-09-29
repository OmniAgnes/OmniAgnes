package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendReplaceAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "memory.md")

	if Load(path) != "" {
		t.Fatal("不存在的记忆应为空")
	}

	if err := Append(path, "用户偏好深色主题"); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, "项目使用 Go 1.27"); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if !strings.Contains(got, "深色主题") || !strings.Contains(got, "Go 1.27") {
		t.Fatalf("追加结果不符: %q", got)
	}

	if err := Replace(path, "整体替换后的记忆"); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got != "整体替换后的记忆" {
		t.Fatalf("替换结果不符: %q", got)
	}

	if err := Append(path, "  "); err == nil {
		t.Fatal("空内容应报错")
	}
}

func TestTool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.md")
	tl := Tool(path)
	ctx := context.Background()

	out, err := tl.Execute(ctx, map[string]any{"content": "第一条记忆"}, nil)
	if err != nil || !strings.Contains(out, "追加") {
		t.Fatalf("append 执行不符: %q %v", out, err)
	}
	if Load(path) != "第一条记忆" {
		t.Fatalf("写入不符: %q", Load(path))
	}

	out, err = tl.Execute(ctx, map[string]any{"mode": "replace", "content": "第二条"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if Load(path) != "第二条" {
		t.Fatalf("replace 不符: %q", Load(path))
	}

	if _, err := tl.Execute(ctx, map[string]any{"mode": "bad", "content": "x"}, nil); err == nil {
		t.Fatal("未知 mode 应报错")
	}
}
