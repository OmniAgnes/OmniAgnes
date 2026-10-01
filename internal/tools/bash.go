package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// resolveShell 返回执行命令所用的解释器。Windows 优先用 bash（Git Bash），无则回退 cmd。
// 第二个返回值 isBash 表示解释器是否为 POSIX bash——只有 bash 才需要做 nul → /dev/null 改写。
func resolveShell(command string) (string, []string, bool) {
	if runtime.GOOS != "windows" {
		return "/bin/bash", []string{"-c", command}, true
	}
	if bash := findBash(); bash != "" {
		return bash, []string{"-c", command}, true
	}
	return "cmd", []string{"/c", command}, false
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

// nullRedirectRe 匹配 Windows cmd 风格的空设备重定向：> nul、2>nul、1> nul、&>nul、>>nul。
// 前后必须有非单词边界，避免误伤 /dev/null、null、nullable 等正常词。
var nullRedirectRe = regexp.MustCompile(`(?i)(>>?|&>)\s*(?:\./)?nul(?:\s|$|&|\||;|"|')`)

// rewriteNullRedirect 把 cmd 风格的空设备重定向改写成 POSIX 的 /dev/null。
//
// 背景：模型在 Windows 上常沿用 cmd 习惯写 `> nul 2>&1`，但我们的 shell 是 bash——
// bash 不认识 nul这个设备，会在工作目录里真的创建一个名为 nul 的空文件。
// 这里做无条件改写（bash 语义下 > /dev/null 与 > nul 意图一致），返回改写后的命令与是否发生改写。
func rewriteNullRedirect(command string) (string, bool) {
	if !nullRedirectRe.MatchString(command) {
		return command, false
	}
	out := nullRedirectRe.ReplaceAllString(command, `${1} /dev/null `)
	// 收敛可能出现的重复空格（仅限重定向符与 /dev/null 之间那一处）。
	out = strings.ReplaceAll(out, ">  /dev/null", "> /dev/null")
	out = strings.ReplaceAll(out, "&>  /dev/null", "&> /dev/null")
	return strings.TrimRight(out, " \t"), true
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

	shell, shellArgs, isBash := resolveShell(command)
	// 用 bash 执行时把 cmd 风格的空设备重定向（> nul）改写成 /dev/null，
	// 否则 bash 会在工作目录创建出一个名为 nul 的垃圾文件。
	if isBash {
		if fixed, changed := rewriteNullRedirect(command); changed {
			shellArgs = []string{"-c", fixed}
		}
	}
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
