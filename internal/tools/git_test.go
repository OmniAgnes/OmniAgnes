package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// gitCLI 用系统 git 初始化一个临时裸仓库并返回其位置，供在真实 repo 上验证工具。
func gitCLI(t *testing.T, dir string, args ...string) string {
	t.Helper()
	args = append([]string{"-C", dir}, args...)
	cmd := exec.Command(gitBin(), args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGitReadOnlyTools(t *testing.T) {
	env, dir := testEnv(t)
	// 初始化一个 git 仓库，留一个已跟踪文件的改动。
	gitCLI(t, dir, "init", "-q", "-b", "main")
	gitCLI(t, dir, "config", "user.name", "tester")
	gitCLI(t, dir, "config", "user.email", "t@test.com")
	writeFile(t, dir+"/a.txt", "hello\n")
	gitCLI(t, dir, "add", "a.txt")
	gitCLI(t, dir, "commit", "-q", "-m", "init")
	// 改动但未提交
	writeFile(t, dir+"/a.txt", "hello\nworld\n")

	r := NewRegistry(env)

	cases := []struct {
		op   string
		want string
	}{
		{"status", "M"},
		{"log", "init"},
		{"branch", "main"},
		{"diff", "+world"},
		{"diff_stat", "a.txt"},
	}
	for _, c := range cases {
		out, err := r.Execute(context.Background(), "git", map[string]any{"op": c.op}, true)
		if err != nil {
			t.Fatalf("git %s: %v", c.op, err)
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("git %s 输出缺少 %q：\n%s", c.op, c.want, out)
		}
	}
}

func TestGitWriteTools(t *testing.T) {
	env, dir := testEnv(t)
	gitCLI(t, dir, "init", "-q", "-b", "main")
	gitCLI(t, dir, "config", "user.name", "tester")
	gitCLI(t, dir, "config", "user.email", "t@test.com")

	r := NewRegistry(env)

	// commit 前需要有暂存内容；先写文件再 add、commit、status 验证幂等往返
	writeFile(t, dir+"/f.txt", "x\n")
	if _, err := r.Execute(context.Background(), "git_write",
		map[string]any{"op": "add", "paths": []string{"f.txt"}}, false); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := r.Execute(context.Background(), "git_write",
		map[string]any{"op": "commit", "message": "add f"}, false); err != nil {
		t.Fatalf("commit: %v", err)
	}
	st, err := r.Execute(context.Background(), "git", map[string]any{"op": "status"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(st, "f.txt") {
		t.Errorf("commit 后仍显示 f.txt pending，status:\n%s", st)
	}
}

// 确保只读工具在 plan(forceReadOnly=true) 下可用，而可写工具被拒绝。
func TestGitPlanReadOnlyGuard(t *testing.T) {
	env, dir := testEnv(t)
	gitCLI(t, dir, "init", "-q", "-b", "main")
	r := NewRegistry(env)
	if _, err := r.Execute(context.Background(), "git", map[string]any{"op": "status"}, true); err != nil {
		t.Errorf("只读 git.status 在只读模式应可用：%v", err)
	}
	if _, err := r.Execute(context.Background(), "git_write", map[string]any{"op": "add", "paths": []string{"x"}}, true); err == nil {
		t.Error("git_write 在只读模式下应被拒绝")
	}
}