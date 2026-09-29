package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testEnv(t *testing.T) (*Env, string) {
	t.Helper()
	dir := t.TempDir()
	return &Env{Cwd: dir, MaxOutput: 4000, Timeout: 10 * time.Second}, dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadTool(t *testing.T) {
	env, dir := testEnv(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "line1\nline2\nline3\n")
	r := NewRegistry(env)
	out, err := r.Execute(context.Background(), "read", map[string]any{"path": "a.txt"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "line2") {
		t.Fatalf("output: %q", out)
	}
	// offset/limit
	out, err = r.Execute(context.Background(), "read", map[string]any{"path": "a.txt", "offset": 1, "limit": 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "line1") || !strings.Contains(out, "line2") {
		t.Fatalf("offset read: %q", out)
	}
}

func TestWriteAndEditTool(t *testing.T) {
	env, dir := testEnv(t)
	r := NewRegistry(env)
	if _, err := r.Execute(context.Background(), "write", map[string]any{"path": "sub/b.txt", "content": "hello world"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "b.txt")); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	// edit
	if _, err := r.Execute(context.Background(), "edit", map[string]any{
		"path": "sub/b.txt", "old_string": "hello", "new_string": "goodbye",
	}, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "sub", "b.txt"))
	if string(data) != "goodbye world" {
		t.Fatalf("edited content: %q", data)
	}
	// 未命中报错
	if _, err := r.Execute(context.Background(), "edit", map[string]any{
		"path": "sub/b.txt", "old_string": "zzz", "new_string": "x",
	}, false); err == nil {
		t.Fatal("expected error for missing old_string")
	}
	// 多匹配需 replace_all
	writeFile(t, filepath.Join(dir, "c.txt"), "aa\naa\n")
	if _, err := r.Execute(context.Background(), "edit", map[string]any{
		"path": "c.txt", "old_string": "aa", "new_string": "bb",
	}, false); err == nil {
		t.Fatal("expected error for ambiguous old_string")
	}
	if _, err := r.Execute(context.Background(), "edit", map[string]any{
		"path": "c.txt", "old_string": "aa", "new_string": "bb", "replace_all": true,
	}, false); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "c.txt"))
	if string(data) != "bb\nbb\n" {
		t.Fatalf("replace_all content: %q", data)
	}
}

func TestSafetyBoundary(t *testing.T) {
	env, dir := testEnv(t)
	r := NewRegistry(env)
	// 工作目录外路径被拒绝
	outside := filepath.Join(dir, "..", "evil.txt")
	if _, err := r.Execute(context.Background(), "write", map[string]any{"path": outside, "content": "x"}, false); err == nil {
		t.Fatal("expected safety rejection")
	}
	// --unsafe 放开
	env.Unsafe = true
	target := filepath.Join(dir, "..", "omni-evil.txt")
	if _, err := r.Execute(context.Background(), "write", map[string]any{"path": target, "content": "x"}, false); err != nil {
		t.Fatalf("unsafe write failed: %v", err)
	}
	_ = os.Remove(target)
}

// requireBash 在 Windows 且找不到 bash 时跳过（生产代码会回退 cmd，POSIX 断言不适用）。
func requireBash(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" && findBash() == "" {
		t.Skip("未找到 bash，跳过 bash 工具测试")
	}
}

func TestPlanModeBlocksWrite(t *testing.T) {
	env, dir := testEnv(t)
	r := NewRegistry(env)
	if _, err := r.Execute(context.Background(), "write", map[string]any{"path": filepath.Join(dir, "x.txt"), "content": "x"}, true); err == nil {
		t.Fatal("expected plan-mode rejection")
	}
	// 只读工具允许
	writeFile(t, filepath.Join(dir, "x.txt"), "content")
	if _, err := r.Execute(context.Background(), "read", map[string]any{"path": "x.txt"}, true); err != nil {
		t.Fatalf("read should work in plan: %v", err)
	}
}

func TestBashTool(t *testing.T) {
	requireBash(t)
	env, dir := testEnv(t)
	r := NewRegistry(env)
	out, err := r.Execute(context.Background(), "bash", map[string]any{"command": "echo hello"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("output: %q", out)
	}
	// 工作目录正确
	writeFile(t, filepath.Join(dir, "mark.txt"), "mark")
	out, err = r.Execute(context.Background(), "bash", map[string]any{"command": "cat mark.txt"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "mark") {
		t.Fatalf("cwd not applied: %q", out)
	}
	// 非零退出状态
	out, err = r.Execute(context.Background(), "bash", map[string]any{"command": "exit 3"}, false)
	if err != nil {
		t.Fatalf("exit error should be returned as output: %v", err)
	}
	if !strings.Contains(out, "exit status 3") {
		t.Fatalf("output: %q", out)
	}
}

func TestBashTimeout(t *testing.T) {
	requireBash(t)
	env, _ := testEnv(t)
	env.Timeout = 300 * time.Millisecond
	r := NewRegistry(env)
	_, err := r.Execute(context.Background(), "bash", map[string]any{"command": "sleep 5"}, false)
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestGrepTool(t *testing.T) {
	env, dir := testEnv(t)
	writeFile(t, filepath.Join(dir, "go.mod"), "module demo\n")
	writeFile(t, filepath.Join(dir, "a.go"), "package main\n// TODO fix\nfunc main() {}\n")
	r := NewRegistry(env)
	out, err := r.Execute(context.Background(), "grep", map[string]any{"pattern": "TODO"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.go:2") || !strings.Contains(out, "TODO") {
		t.Fatalf("grep output: %q", out)
	}
	// include 过滤
	out, err = r.Execute(context.Background(), "grep", map[string]any{"pattern": "module", "include": "*.mod"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "go.mod") {
		t.Fatalf("grep include output: %q", out)
	}
}

func TestGlobTool(t *testing.T) {
	env, dir := testEnv(t)
	writeFile(t, filepath.Join(dir, "a.go"), "")
	writeFile(t, filepath.Join(dir, "pkg", "b.go"), "")
	r := NewRegistry(env)
	out, err := r.Execute(context.Background(), "glob", map[string]any{"pattern": "**/*.go"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "pkg/b.go") {
		t.Fatalf("glob output: %q", out)
	}
	// 单层 * 不跨目录
	out, err = r.Execute(context.Background(), "glob", map[string]any{"pattern": "*.go"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "pkg/b.go") {
		t.Fatalf("plain glob should not cross dirs: %q", out)
	}
}
