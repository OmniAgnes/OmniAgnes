package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// resolveShell 返回执行命令所用的解释器。Windows 优先用 bash（Git Bash），无则回退 cmd。
func resolveShell(command string) (string, []string) {
	if runtime.GOOS != "windows" {
		return "/bin/bash", []string{"-c", command}
	}
	if bash := findBash(); bash != "" {
		return bash, []string{"-c", command}
	}
	return "cmd", []string{"/c", command}
}

// findBash 定位 bash.exe：先查 PATH，再找 Git 安装目录（git.exe 位于 <root>\cmd，bash 在 <root>\bin）。
func findBash() string {
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	if git, err := exec.LookPath("git"); err == nil {
		p := filepath.Join(filepath.Dir(filepath.Dir(git)), "bin", "bash.exe")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func execBash(ctx context.Context, args map[string]any, env *Env) (string, error) {
	command, err := argString(args, "command")
	if err != nil {
		return "", err
	}
	timeout := env.Timeout
	if s := argInt(args, "timeout", 0); s > 0 {
		timeout = time.Duration(s) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdCtx, stop := context.WithCancel(ctx)
	defer stop()

	shell, shellArgs := resolveShell(command)
	cmd := exec.CommandContext(cmdCtx, shell, shellArgs...)
	cmd.Dir = env.Cwd

	var stdout, stderr strings.Builder
	var mu sync.Mutex
	cap := env.MaxOutput*2 + 4096

	pipe := func(rc io.ReadCloser, dst *strings.Builder) {
		defer rc.Close()
		buf := make([]byte, 4096)
		for {
			n, err := rc.Read(buf)
			if n > 0 {
				mu.Lock()
				if dst.Len() < cap {
					dst.Write(buf[:n])
				}
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}

	outPipe, _ := cmd.StdoutPipe()
	errPipe, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("bash: 启动失败: %w", err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pipe(outPipe, &stdout) }()
	go func() { defer wg.Done(); pipe(errPipe, &stderr) }()

	done := make(chan error, 1)
	go func() {
		wg.Wait()
		done <- cmd.Wait()
	}()

	var runErr error
	select {
	case <-ctx.Done():
		stop()
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("bash: 命令超时（%s）", timeout)
		}
		return "", fmt.Errorf("bash: 已取消")
	case runErr = <-done:
	}

	mu.Lock()
	out := stdout.String()
	errOut := stderr.String()
	mu.Unlock()

	var sb strings.Builder
	if out != "" {
		sb.WriteString(out)
	}
	if errOut != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[stderr]\n")
		sb.WriteString(errOut)
	}
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			sb.WriteString(fmt.Sprintf("\n[exit status %d]", exitErr.ExitCode()))
		} else {
			sb.WriteString(fmt.Sprintf("\n[bash error: %v]", runErr))
		}
	}
	result := sb.String()
	if env.MaxOutput > 0 && len(result) > env.MaxOutput {
		result = result[:env.MaxOutput] + "\n...（输出截断）"
	}
	if strings.TrimSpace(result) == "" {
		result = "（命令无输出）"
	}
	return result, nil
}
