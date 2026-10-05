package tools

import (
	"bytes"
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
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
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

// hasEnvKey 判断环境变量是否已存在（跨平台：Windows 大小写不敏感）。
func hasEnvKey(env []string, key string) bool {
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

// toUTF8 把子进程输出转成合法 UTF-8。
//
// 已经是合法 UTF-8 就原样返回（绝大多数情况，Git Bash 的工具都输出 UTF-8）。
// 否则按 GB18030 解码：它是 GBK/GB2312 的超集，正好覆盖中文 Windows 上
// PowerShell、cmd 及大量 exe 按系统 ANSI 码页(936)吐出的字节。
// 两条都失败才退化成替换字符，此时至少不会再把半个汉字写坏。
func toUTF8(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	dec := simplifiedchinese.GBK.NewDecoder()
	if dec != nil {
		if s, err := dec.Bytes(b); err == nil && utf8.Valid(s) {
			return string(s)
		}
	}
	return strings.ToValidUTF8(string(b), "�")
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
	// 让子进程优先输出 UTF-8，能少走一次 GBK 转码（转码只是兜底，不是唯一保障）。
	// 不覆盖已有值：用户可能特意设了 locale。
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	if !hasEnvKey(cmd.Env, "LANG") {
		cmd.Env = append(cmd.Env, "LANG=C.UTF-8")
	}
	if !hasEnvKey(cmd.Env, "PYTHONIOENCODING") {
		cmd.Env = append(cmd.Env, "PYTHONIOENCODING=utf-8")
	}

	// 输出先按字节捕获，不要直接塞进 string：
	// Windows 上子进程（PowerShell、cmd、以及用户自己跑的 exe）常按系统 ANSI 码页
	// 输出 GBK 字节，若原样进 string 再被 json.Marshal，非法 UTF-8 字节会被永久替换成
	// U+FFFD 写进会话文件，模型和所有客户端（桌面 / 手机）之后读到的都是乱码。
	// 正确做法是捕获后统一转码，见 toUTF8。
	var stdout, stderr bytes.Buffer
	var mu sync.Mutex
	cap := env.MaxOutput*2 + 4096

	pipe := func(rc io.ReadCloser, dst *bytes.Buffer) {
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
	out := toUTF8(stdout.Bytes())
	errOut := toUTF8(stderr.Bytes())
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
