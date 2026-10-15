// media_setup：ffmpeg / ffprobe 的**按需下载安装**（不再随安装包内置）。
//
// 为什么改成按需：ffmpeg 静态构建是安装包里最大的一块——BtbN 的 win64 zip 解包后
// ffmpeg.exe + ffprobe.exe 各 90MB+，NSIS 再压缩几乎压不动（exe 本身已压缩），
// 让每个用户的安装包白白多背 180MB，哪怕他从不碰音视频。
// 改为由 AI 在对话中真正需要时才装：不装不下载，装了只下一次，装在用户数据目录
// （安装包升级不覆盖，用户装一次永久可用）。
//
// 来源矩阵（实测 2026-10-16）：
//   主源  npm 平台包 @ffmpeg-installer/* 与 @ffprobe-installer/*
//         —— 国内走 npmmirror（实测 0.34s 可达），tar.gz 约 20-40MB；
//            覆盖 win32-x64 / darwin-x64 / darwin-arm64 / linux-x64 / linux-arm64。
//   补源  eugeneware/ffmpeg-static b6.0 的 .gz 资源（单文件直链，18-30MB）
//         —— 用来补 win32-arm64 这个缺口（npm 两个包都没有 arm64 的 Windows），
//            同时作为主源的通用回退。
//
// 安装位置：~/.local/share/omni/bin/ —— 正是 findTool 的第 3 级查找路径。
// 先写 .part 临时文件再原子改名，避免下载中断留下半个可执行文件。
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const downloadTimeout = 20 * time.Minute // 单个工具的下载+安装上限

// npmRegistry 国内优先：npmmirror 实测可达且快，官方源作回退（顺序即优先级）。
var npmRegistries = []string{
	"https://registry.npmmirror.com",
	"https://registry.npmjs.org",
}

const staticBase = "https://github.com/eugeneware/ffmpeg-static/releases/download/b6.0"

type setupArgs struct {
	Force bool `json:"force,omitempty" jsonschema:"已安装时是否强制重新下载安装，默认 false（已装则直接返回，不重复下载）"`
}

// ---------- 来源矩阵 ----------

// npmPlatform 把当前平台映射成 npm 平台包后缀，如 win32-x64。
// 返回空串表示该平台在 npm 上没有对应包（目前只有 windows/arm64 缺）。
func npmPlatform() string {
	goos := runtime.GOOS
	switch goos {
	case "windows":
		goos = "win32"
	case "darwin", "linux":
		// npm 包名就是 darwin / linux，不用改
	default:
		return ""
	}
	switch runtime.GOARCH {
	case "amd64":
		return goos + "-x64"
	case "386":
		return goos + "-ia32"
	case "arm64":
		return goos + "-arm64"
	}
	return ""
}

// staticAsset 是 eugeneware/ffmpeg-static 的资源名后缀，如 win32-x64 / linux-arm64。
// 覆盖 6 个平台组合（含 win32-x64），但不覆盖 windows/arm64 —— 那正是需要主源的原因。
func staticAsset() string {
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "x64"
	case "386":
		arch = "ia32"
	case "arm64":
		arch = "arm64"
	default:
		return ""
	}
	switch runtime.GOOS {
	case "windows":
		return "win32-" + arch
	case "darwin", "linux":
		return runtime.GOOS + "-" + arch
	}
	return ""
}

// installDir 是按需安装的目标目录，与 findTool 的第 3 级查找路径保持一致。
func installDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "omni", "bin"), nil
}

// ---------- 安装单个工具 ----------

// setupTool 下载并安装 ffmpeg 或 ffprobe 到 destPath。
// 已存在且 force=false 时直接返回，不重复下载。
func setupTool(ctx context.Context, tool, destPath string, force bool) (string, error) {
	if !force {
		if fi, err := os.Stat(destPath); err == nil && fi.Size() > 1<<20 {
			return "已存在（未重复下载）", nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return "", err
	}
	// 临时文件与目标同目录 → 最后的 rename 是同分区原子操作，不会被并发/中断留下半个文件
	tmp := destPath + ".part"
	defer os.Remove(tmp)

	type attempt struct{ label string; run func(dest string) error }
	var attempts []attempt
	if npmPlatform() != "" {
		attempts = append(attempts, attempt{
			label: "npm 平台包 " + npmPkg(tool),
			run:   func(dest string) error { return fromNpm(ctx, tool, dest) },
		})
	}
	if staticAsset() != "" {
		attempts = append(attempts, attempt{
			label: "GitHub 静态构建 " + staticAsset(),
			run:   func(dest string) error { return fromStatic(ctx, tool, dest) },
		})
	}
	if len(attempts) == 0 {
		return "", fmt.Errorf("当前平台 %s/%s 没有可用的预编译来源，请改用系统包管理器安装", runtime.GOOS, runtime.GOARCH)
	}

	// 逐个来源尝试，**记录每个的失败原因**：曾经主源 404 却被回退源成功掩盖，
	// 导致国内用户全部默默走慢速 GitHub，日志里一个字都看不到。
	var fails []string
	for _, a := range attempts {
		if err := a.run(tmp); err != nil {
			fails = append(fails, fmt.Sprintf("%s → %v", a.label, err))
			continue
		}
		if err := os.Rename(tmp, destPath); err != nil {
			return "", err
		}
		// mac/Linux 缺 x 位会被 spawn 拒绝；Windows 不看权限位
		if runtime.GOOS != "windows" {
			_ = os.Chmod(destPath, 0o755)
		}
		if len(fails) > 0 {
			return a.label + "（已回退：" + strings.Join(fails, "；") + "）", nil
		}
		return a.label, nil
	}
	return "", fmt.Errorf("全部来源均失败 —— %s", strings.Join(fails, "；"))
}

// npmPkg 返回 npm 平台包名，形如 @ffmpeg-installer/win32-x64。
//
// ⚠️ scope 里**不能再带工具名**：npm 的平台包把工具名编码在包名里
//    （@ffmpeg-installer/win32-x64 而非 @ffmpeg-installer/ffmpeg/win32-x64），
//    多拼一层会得到 404 NOT_FOUND——这个 bug 曾让主源静默失效、全部请求
//    悄悄落到回退源上（GitHub 在国内慢得多）。改这里前先看 TestNpmPkgName。
func npmPkg(tool string) string {
	if tool == "ffprobe" {
		return "@ffprobe-installer/" + npmPlatform()
	}
	return "@ffmpeg-installer/" + npmPlatform()
}

// ---------- 来源 1：npm 平台包 ----------

// fromNpm 从 npm 平台包（.tgz）里取出可执行文件写到 dest。
// 流程：查 registry 拿 dist-tags.latest → 下 tar.gz → 解包挑出可执行文件。
func fromNpm(ctx context.Context, tool, dest string) error {
	pkg := npmPkg(tool)
	tarball, ver, err := resolveTarball(ctx, pkg)
	if err != nil {
		return err
	}
	data, err := httpGet(ctx, tarball, 0)
	if err != nil {
		return fmt.Errorf("下载 %s@%s 失败：%w", pkg, ver, err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractFromTar(gz, tool, dest)
}

// resolveTarball 查 registry 拿最新版 tarball 地址（不硬编码版本号，随上游升级）。
// 官方源响应里的 tarball 域名会替换成当前 registry，从而走国内镜像的 CDN。
func resolveTarball(ctx context.Context, pkg string) (url, ver string, err error) {
	var lastErr error
	for _, reg := range npmRegistries {
		body, e := httpGet(ctx, reg+"/"+pkg, 16<<20)
		if e != nil {
			lastErr = e
			continue
		}
		var doc struct {
			DistTags struct {
				Latest string `json:"latest"`
			} `json:"dist-tags"`
			Versions map[string]struct {
				Dist struct {
					Tarball string `json:"tarball"`
				} `json:"dist"`
			} `json:"versions"`
		}
		if e := json.Unmarshal(body, &doc); e != nil {
			lastErr = fmt.Errorf("解析 registry 响应失败：%w", e)
			continue
		}
		ver = doc.DistTags.Latest
		if v, ok := doc.Versions[ver]; ok && v.Dist.Tarball != "" {
			return strings.Replace(v.Dist.Tarball, "https://registry.npmjs.org", reg, 1), ver, nil
		}
		lastErr = fmt.Errorf("registry 响应里没有 %s 的 tarball 地址", pkg)
	}
	return "", "", lastErr
}

// extractFromTar 从 tgz 流里挑出目标工具的可执行文件写到 dest。
// npm 包内路径形如 package/ffmpeg；只认 package/ 前缀，避免误取同名文档。
func extractFromTar(r io.Reader, tool, dest string) error {
	tr := tar.NewReader(r)
	want := exeName(tool)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || !strings.HasPrefix(h.Name, "package/") {
			continue
		}
		base := filepath.Base(h.Name)
		if base != want && base != tool {
			continue
		}
		if err := writeStream(dest, tr); err != nil {
			return err
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("压缩包内未找到 %s", want)
	}
	return nil
}

// ---------- 来源 2：GitHub 静态构建（.gz 直链） ----------

func fromStatic(ctx context.Context, tool, dest string) error {
	url := fmt.Sprintf("%s/%s-%s.gz", staticBase, tool, staticAsset())
	data, err := httpGet(ctx, url, 0)
	if err != nil {
		return err
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	return writeStream(dest, gz)
}

func writeStream(dest string, r io.Reader) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ---------- HTTP ----------

// httpGet 下载到内存。maxBytes>0 时限流（查 registry 的 JSON 用它兜底）。
func httpGet(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "omni-media/1.0")
	// GitHub 资源会 302 到对象存储，http.Client 默认会跟随
	resp, err := (&http.Client{Timeout: downloadTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var r io.Reader = resp.Body
	if maxBytes > 0 {
		r = io.LimitReader(resp.Body, maxBytes)
	}
	return io.ReadAll(r)
}

// ---------- 工具入口 ----------

// toolProbe 描述某个工具当前在本机的可用情况。
type toolProbe struct {
	path   string // 空 = 不可用
	source string // 命中来源（环境变量 / 数据目录 / 系统 PATH…）
	err    error  // locateTool 的错误（不可用时非空）
}

// probeTools 探测 ffmpeg 与 ffprobe 的当前状态。
func probeTools() map[string]toolProbe {
	out := map[string]toolProbe{}
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		p, src, err := locateTool(tool)
		out[tool] = toolProbe{path: p, source: src, err: err}
	}
	return out
}

// probeVersion 读一次工具版本号（首行含 "ffmpeg version 6.1.1 ..."）。
// 拿到版本是为了让 AI/用户判断「系统那份是不是太老、缺编码器」——
// 有些系统包给的是裁剪版，功能不全但 ffmpeg -version 看着正常。
func probeVersion(bin string) string {
	cmd := exec.Command(bin, "-version")
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if len(line) > 110 {
		line = line[:110] + "…"
	}
	return line
}

// mediaSetup 是 media_setup 工具的实现。
//
// 关键行为：**先探测再决定**。用户机器上很可能已经有 ffmpeg（brew / winget /
// scoop / apt 装的），这时直接用系统那份、报告清楚版本，不重复下载 80MB。
// 只有确实缺哪个才补哪个——比如系统有 ffmpeg 但没 ffprobe，就只装 ffprobe。
// force=true 时无视现有环境，全部装到 omni 数据目录（用于「系统那份太老/是裁剪版」）。
func mediaSetup(ctx context.Context, _ *mcp.CallToolRequest, args setupArgs) (*mcp.CallToolResult, any, error) {
	dir, err := installDir()
	if err != nil {
		return nil, nil, err
	}
	tools := []string{"ffmpeg", "ffprobe"}
	before := probeTools()

	var b strings.Builder
	fmt.Fprintf(&b, "平台：%s/%s\n安装目录：%s\n", runtime.GOOS, runtime.GOARCH, dir)

	// 已有环境先报一遍，AI 与用户都能看到「用的是哪一份」
	b.WriteString("\n探测现有环境：\n")
	for _, tool := range tools {
		if pr := before[tool]; pr.err == nil {
			fmt.Fprintf(&b, "  ✓ %s 已存在（%s）\n", tool, pr.source)
		} else {
			fmt.Fprintf(&b, "  ✗ %s 未找到\n", tool)
		}
	}

	// 决定要装哪些：非 force 时只补缺失的
	var need []string
	for _, tool := range tools {
		if args.Force || before[tool].err != nil {
			need = append(need, tool)
		}
	}
	if len(need) == 0 {
		b.WriteString("\n无需下载：本机已具备可用的 ffmpeg/ffprobe，直接可用。\n")
		if v := versionOf(before, "ffmpeg"); v != "" {
			fmt.Fprintf(&b, "ffmpeg 版本：%s\n", v)
		}
		return textResult(strings.TrimRight(b.String(), "\n")), nil, nil
	}
	if !args.Force {
		b.WriteString("\n以下工具需要下载安装：" + strings.Join(need, "、") + "\n")
	}

	// 逐个安装、逐个报错：一个失败不阻断另一个
	failed := 0
	for _, tool := range need {
		dest := filepath.Join(dir, exeName(tool))
		from, err := setupTool(ctx, tool, dest, args.Force)
		if err != nil {
			failed++
			fmt.Fprintf(&b, "\n%s 安装失败 —— %v\n", tool, err)
			continue
		}
		size := "?"
		if fi, e := os.Stat(dest); e == nil {
			size = humanSize(fi.Size())
		}
		fmt.Fprintf(&b, "\n%s：%s → %s（%s）\n", tool, from, dest, size)
	}

	// 装完复查：确认 findTool 真能解析到，而不是只信安装过程没报错
	b.WriteString("\n复查：\n")
	after := probeTools()
	for _, tool := range tools {
		pr := after[tool]
		if pr.err != nil {
			fmt.Fprintf(&b, "  ✗ %s 仍不可用 —— %v\n", tool, pr.err)
			continue
		}
		fmt.Fprintf(&b, "  ✓ %s → %s（%s）\n", tool, pr.path, pr.source)
		if v := probeVersion(pr.path); v != "" {
			fmt.Fprintf(&b, "      %s\n", v)
		}
	}
	if failed > 0 {
		return textResult(strings.TrimRight(b.String(), "\n")), nil,
			fmt.Errorf("有 %d 个工具安装失败，详见上方输出", failed)
	}
	return textResult(strings.TrimRight(b.String(), "\n")), nil, nil
}

func versionOf(probes map[string]toolProbe, tool string) string {
	if pr, ok := probes[tool]; ok && pr.err == nil {
		return probeVersion(pr.path)
	}
	return ""
}