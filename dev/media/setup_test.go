package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestNpmPkgName 固化包名拼接规则。
//
// 曾经的 bug：包名拼成 @ffmpeg-installer/ffmpeg/win32-x64（多一层工具名），
// registry 返回 404，主源静默失效、所有请求悄悄落到慢速的 GitHub 回退源。
// 这里断言 scope 包名里工具名只出现一次，防止再拼错。
func TestNpmPkgName(t *testing.T) {
	plat := npmPlatform()
	if plat == "" {
		t.Skipf("平台 %s/%s 无 npm 平台包", runtime.GOOS, runtime.GOARCH)
	}
	cases := map[string]string{
		"ffmpeg":  "@ffmpeg-installer/" + plat,
		"ffprobe": "@ffprobe-installer/" + plat,
	}
	for tool, want := range cases {
		got := npmPkg(tool)
		if got != want {
			t.Errorf("npmPkg(%q) = %q, want %q", tool, got, want)
		}
		// scope 包名里工具名只能出现一次：@<scope>/<platform>，不能再挂一层工具名
		if strings.Count(got, tool) != 1 {
			t.Errorf("npmPkg(%q)=%q 里工具名出现 %d 次，应恰好 1 次", tool, got, strings.Count(got, tool))
		}
		if strings.Contains(got, "/"+tool+"/") {
			t.Errorf("npmPkg(%q)=%q 含多余的 /%s/ 路径段", tool, got, tool)
		}
	}
}

// TestPlatformSuffix 断言 GOOS/GOARCH → npm 平台后缀的映射。
func TestPlatformSuffix(t *testing.T) {
	want := map[string]string{
		"windows/amd64": "win32-x64",
		"windows/386":   "win32-ia32",
		"darwin/amd64":  "darwin-x64",
		"darwin/arm64":  "darwin-arm64",
		"linux/amd64":   "linux-x64",
		"linux/arm64":   "linux-arm64",
	}
	if got := npmPlatform(); got != want[runtime.GOOS+"/"+runtime.GOARCH] {
		t.Errorf("npmPlatform() = %q, want %q", got, want[runtime.GOOS+"/"+runtime.GOARCH])
	}
}

// TestResolveTarballReal 打真实网络，确认 registry 可达且 tarball 能解析出来。
// 这是主源可用性的回归测试——包名再拼错就会在这里 404 失败。
func TestResolveTarballReal(t *testing.T) {
	if testing.Short() {
		t.Skip("需要网络")
	}
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		pkg := npmPkg(tool)
		url, ver, err := resolveTarball(context.Background(), pkg)
		if err != nil {
			t.Errorf("resolveTarball(%s): %v", pkg, err)
			continue
		}
		if ver == "" || !strings.HasSuffix(url, ".tgz") {
			t.Errorf("resolveTarball(%s) 返回可疑地址：url=%q ver=%q", pkg, url, ver)
		}
	}
}

// TestFromNpmReal 真实下载并解包一个工具，验证端到端可用。
// 网络慢时可 -short 跳过；CI 与本地发版前建议跑一次。
func TestFromNpmReal(t *testing.T) {
	if testing.Short() {
		t.Skip("需要网络且会下载数十 MB")
	}
	dest := filepath.Join(t.TempDir(), exeName("ffmpeg"))
	if err := fromNpm(context.Background(), "ffmpeg", dest); err != nil {
		t.Fatalf("fromNpm 失败：%v", err)
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("产物不存在：%v", err)
	}
	// 静态构建的 ffmpeg 远大于 1MB；小于此说明解包出了错文件
	if fi.Size() < 1<<20 {
		t.Fatalf("产物仅 %d 字节，疑似解包错误", fi.Size())
	}
}

// TestSetupToolIdempotent 验证已安装时不重复下载（force=false 直接返回）。
func TestSetupToolIdempotent(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, exeName("ffprobe"))
	// 造一个够大的假文件，模拟"已安装"
	if err := os.WriteFile(dest, make([]byte, 2<<20), 0o755); err != nil {
		t.Fatal(err)
	}
	from, err := setupTool(context.Background(), "ffprobe", dest, false)
	if err != nil {
		t.Fatalf("已安装时不该报错：%v", err)
	}
	if !strings.Contains(from, "已存在") {
		t.Errorf("from = %q, 应含「已存在」", from)
	}
}

// TestMediaSetupReusesSystemFFmpeg 是本文件最要紧的一条：用户机器上很可能
// 已经装了 ffmpeg（brew / winget / scoop / apt），这时必须**复用系统那份、
// 一个字节都不下载**，而不是白下 80MB。
//
// 做法：造一个假 ffmpeg 塞进临时目录并加进 PATH，断言 media_setup 走完
// 「无需下载」分支；PATH 里的那份必须原封不动（没被下载覆盖）。
func TestMediaSetupReusesSystemFFmpeg(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows 上临时目录里的 exe 不能直接当 PATH 可执行文件跑起来，
		// 且 probeVersion 会去执行它；跳过以免假失败。行为由本机手工验证。
		t.Skip("Windows 下临时目录 exe 无法直接经 PATH 探测")
	}
	dir := t.TempDir()
	// 假 ffmpeg：一段 shell 脚本，-version 打印版本号，足以让探测通过
	script := "#!/bin/sh\necho 'ffmpeg version 6.1-test'\n"
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		p := filepath.Join(dir, tool)
		if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	res, _, err := mediaSetup(context.Background(), nil, setupArgs{})
	if err != nil {
		t.Fatalf("mediaSetup 报错：%v", err)
	}
	out := textOf(res)
	if !strings.Contains(out, "无需下载") {
		t.Errorf("系统已有 ffmpeg 时应走「无需下载」分支，实际输出：\n%s", out)
	}
	if !strings.Contains(out, "系统 PATH") {
		t.Errorf("应报告命中来源为系统 PATH，实际输出：\n%s", out)
	}
	// 版本号要被报出来（AI 据此判断系统那份是否可用）
	if !strings.Contains(out, "6.1-test") {
		t.Errorf("应报告 ffmpeg 版本，实际输出：\n%s", out)
	}
}

// textOf 从 CallToolResult 里取文本内容，简化测试断言。
func textOf(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// TestProbeToolsShape 保证 probeTools 对两个工具都给出结论，
// 且「有路径」与「无错误」始终一致（避免报告自相矛盾）。
func TestProbeToolsShape(t *testing.T) {
	probes := probeTools()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		pr, ok := probes[tool]
		if !ok {
			t.Fatalf("probeTools 缺少 %s", tool)
		}
		if pr.err == nil && pr.path == "" {
			t.Errorf("%s：无错误但路径为空，自相矛盾", tool)
		}
		if pr.err != nil && pr.path != "" {
			t.Errorf("%s：有错误却仍返回路径 %q，自相矛盾", tool, pr.path)
		}
		if pr.err == nil && pr.source == "" {
			t.Errorf("%s：可用但未记录来源，报告里说不清是哪一份", tool)
		}
	}
}