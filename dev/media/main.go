// media MCP 服务器：音视频信息 / 格式转换 / 抽音轨 / 截图，基于 ffmpeg 包装。
//
// 为什么不纯 Go 实现：音视频编解码（H.264/H.265/AAC/MP3/VP9…）在 Go 标准库里
// 完全没有，纯 Go 生态也没有可与 ffmpeg 匹敌的实现。业界通行做法就是调用 ffmpeg，
// 本插件因此是**纯 Go 的 ffmpeg 前端**：无 cgo、无库依赖，只要求运行环境有 ffmpeg。
//
// ffmpeg / ffprobe 解析顺序（先命中先用）：
//  1. 环境变量 OMNI_FFMPEG / OMNI_FFPROBE（可指到任意路径）
//  2. 本插件 exe 同目录（含 bin/ffmpeg/ 子目录，用户也可手动放）
//  3. 数据目录 bin（~/.local/share/omni/bin/ffmpeg.exe —— media_setup 按需装到这里）
//  4. 系统 PATH
//
// ⚠️ 安装包**不再内置** ffmpeg：静态构建是安装包里最大的一块（双 exe 约 180MB，
//    NSIS 压不动），绝大多数用户根本用不到。改为由 AI 在对话中按需调用 media_setup
//    下载安装（见 setup.go），装一次永久可用，安装包升级不覆盖。
//
// 工具：
//   - media_setup：按需下载安装 ffmpeg/ffprobe（其他 media 工具报缺环境时先调它）
//   - media_info：读取音视频信息（时长/分辨率/码率/编码/音视频轨）
//   - media_convert：通用格式转换（容器/编码/质量/分辨率/裁剪时间；可仅重封装）
//   - media_extract_audio：只抽音轨（mp3/m4a/wav/flac/ogg）
//   - media_thumbnail：截取指定时间点的一帧作为封面图（jpg/png）
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const convertTimeout = 30 * time.Minute // 单个转码任务上限（长视频留足时间）

type infoArgs struct {
	Path string `json:"path" jsonschema:"音视频文件路径"`
}

type convertArgs struct {
	Path       string `json:"path" jsonschema:"源文件路径"`
	Out        string `json:"out" jsonschema:"输出文件路径，格式由后缀决定（如 .mp4/.mkv/.mp3/.m4a/.wav/.flac/.webm/.gif）"`
	AudioOnly  bool   `json:"audio_only,omitempty" jsonschema:"是否丢弃视频只输出音频"`
	CopyStream bool   `json:"copy_stream,omitempty" jsonschema:"是否直接流复制不重编码（最快且无损；仅当目标容器兼容源编码时可用）"`
	VideoCodec string `json:"video_codec,omitempty" jsonschema:"视频编码器，如 libx264 / libx265 / h264_nvenc；默认由 ffmpeg 按容器推断"`
	AudioCodec string `json:"audio_codec,omitempty" jsonschema:"音频编码器，如 aac / libmp3lame / libopus；默认由 ffmpeg 按容器推断"`
	CRF        int    `json:"crf,omitempty" jsonschema:"视频质量 0-51（越小越清晰体积越大），默认 23，仅重编码时有效"` 
	Scale      string `json:"scale,omitempty" jsonschema:"缩放，如 1280:-2 表示宽 1280 高按比例；-2 会在需要时对齐偶数"`
	AudioRate  string `json:"audio_rate,omitempty" jsonschema:"音频码率，如 128k / 192k"`
	Start      string `json:"start,omitempty" jsonschema:"起始时间，如 00:00:10 或 10（秒），可选"`
	Duration   string `json:"duration,omitempty" jsonschema:"截取时长，如 00:00:30 或 30（秒），可选"`
}

type extractAudioArgs struct {
	Path  string `json:"path" jsonschema:"源音视频文件路径"`
	Out   string `json:"out,omitempty" jsonschema:"输出音频路径；省略则按源文件名生成 .mp3 到用户「下载」目录"`
	Rate  string `json:"rate,omitempty" jsonschema:"音频码率，如 192k，默认 192k"`
	Codec string `json:"codec,omitempty" jsonschema:"音频编码，默认按输出后缀自动（mp3→libmp3lame，m4a→aac，wav→pcm_s16le，flac→flac，ogg→libvorbis）"`
}

type thumbArgs struct {
	Path  string  `json:"path" jsonschema:"源视频文件路径"`
	Out   string  `json:"out,omitempty" jsonschema:"输出图片路径；省略则生成 <原名>-thumb.jpg 到用户「下载」目录"`
	Time  string  `json:"time,omitempty" jsonschema:"截取时间点，如 00:00:05 或 5（秒），默认 1 秒"`
	Width int     `json:"width,omitempty" jsonschema:"输出宽度像素，默认保持原宽；高度按比例"`
	Scale float64 `json:"scale,omitempty" jsonschema:"缩放比例（与 width 二选一），如 0.5"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "omni-media", Version: "0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "media_setup",
		Annotations: &mcp.ToolAnnotations{Title: "安装音视频处理环境"},
		Description: "准备音视频处理环境：先探测本机是否已有 ffmpeg/ffprobe（系统 PATH 里 brew/winget/scoop/apt 装的会被直接复用），只下载缺失的部分（约 20-40MB，装一次永久可用）。其他 media 工具若报「未找到 ffmpeg」，先调用本工具再重试；报告可用 ffmpeg 版本。",
	}, mediaSetup)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "media_info",
		Annotations: &mcp.ToolAnnotations{Title: "音视频信息", ReadOnlyHint: true},
		Description: "读取音视频文件的时长、分辨率、码率、编码格式与音视频轨信息（基于 ffprobe）。",
	}, mediaInfo)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "media_convert",
		Annotations: &mcp.ToolAnnotations{Title: "音视频格式转换"},
		Description: "音视频格式转换：容器与编码按输出后缀自动推断，可指定编码器/质量/分辨率/截取时间段，也可 audio_only 只取音频或 copy_stream 仅重封装（快且无损）。",
	}, mediaConvert)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "media_extract_audio",
		Annotations: &mcp.ToolAnnotations{Title: "提取音轨"},
		Description: "从视频中提取音频轨，输出 mp3/m4a/wav/flac/ogg（编码按后缀自动选择）。",
	}, mediaExtractAudio)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "media_thumbnail",
		Annotations: &mcp.ToolAnnotations{Title: "视频截图"},
		Description: "截取视频指定时间点的一帧保存为 jpg/png，用作封面或预览图。",
	}, mediaThumbnail)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "omni-media:", err)
		os.Exit(1)
	}
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// ---------- 输出目录 ----------

// outputDir 返回生成文件的默认输出目录：优先 OMNI_OUTPUT_DIR（内核注入，
// 通常就是用户的「下载」目录），否则用户主目录下的 Downloads。
func outputDir() string {
	if d := strings.TrimSpace(os.Getenv("OMNI_OUTPUT_DIR")); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Downloads")
	}
	return "Downloads"
}

// deriveOut 由源文件派生输出文件名，落在输出目录（默认用户的「下载」目录）
// 而非源文件所在目录：suffix 就是要换成的后缀（如 ".mp3"、"-thumb.jpg"）。
func deriveOut(src, suffix string) string {
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	return filepath.Join(outputDir(), base+suffix)
}

// ---------- ffmpeg 定位 ----------

func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// locateTool 按 4 级顺序找 ffmpeg / ffprobe，并报告**命中来源**。
// 来源要报出来给 AI 与用户看：media_setup 靠它判断「用户机器上已经有了」，
// 从而跳过下载（用户 brew/winget/scoop 装过 ffmpeg 很常见，没理由再下 80MB）。
func locateTool(name string) (path string, source string, err error) {
	// 1. 环境变量
	envKey := "OMNI_" + strings.ToUpper(name)
	if p := strings.TrimSpace(os.Getenv(envKey)); p != "" {
		if fileExists(p) {
			return p, "环境变量 " + envKey, nil
		}
		return "", "", fmt.Errorf("环境变量 %s 指向的 %s 不存在", envKey, p)
	}
	// 2. 插件 exe 同目录：也支持用户手动把 ffmpeg 丢在插件同目录或其 ffmpeg/ 子目录
	//    （安装包已不再内置，这里是留给手动放置与旧版本残留的位置）。
	if self, err := os.Executable(); err == nil {
		dir := filepath.Dir(self)
		for _, p := range []string{
			filepath.Join(dir, "ffmpeg", exeName(name)),
			filepath.Join(dir, exeName(name)),
		} {
			if fileExists(p) {
				return p, "插件同目录 " + p, nil
			}
		}
	}
	// 3. 数据目录：media_setup 按需装到这里（~/.local/share/omni/bin）
	if home, err := os.UserHomeDir(); err == nil {
		for _, p := range []string{
			filepath.Join(home, ".local", "share", "omni", "bin", exeName(name)),
			filepath.Join(home, ".local", "share", "omni", "ffmpeg", exeName(name)),
		} {
			if fileExists(p) {
				return p, "omni 数据目录 " + p, nil
			}
		}
	}
	// 4. PATH：用户自己用系统包管理器装的（brew / winget / scoop / apt）
	if p, err := exec.LookPath(name); err == nil {
		return p, "系统 PATH " + p, nil
	}
	// 找不到时的提示是**给 AI 看的**：安装包不再内置 ffmpeg，AI 读到这段就该
	// 先调 media_setup 装好再重试本工具，而不是把一堆手工步骤甩给用户。
	return "", "", fmt.Errorf(
		"未找到 %s。安装包不再内置 ffmpeg，请先调用 media_setup 工具自动下载安装（约 20-40MB，装一次永久可用；"+
			"若用户已自行装过 ffmpeg，media_setup 会自动识别并跳过下载），装好后重试本工具。\n"+
			"若用户明确不想自动下载，可选这些手工办法：\n"+
			"  1) 系统包管理器（Windows: winget install Gyan.FFmpeg / scoop install ffmpeg；macOS: brew install ffmpeg；Linux: apt install ffmpeg）\n"+
			"  2) 把 %s（及 ffprobe）放到插件同目录的 ffmpeg/ 子目录：%s\n"+
			"  3) 用环境变量 %s 指定完整路径",
		name, name, filepath.Join(selfDir(), "ffmpeg"), "OMNI_"+strings.ToUpper(name))
}

// findTool 是 locateTool 的薄封装，只要路径的既有调用点用它。
func findTool(name string) (string, error) {
	p, _, err := locateTool(name)
	return p, err
}

func selfDir() string {
	if self, err := os.Executable(); err == nil {
		return filepath.Dir(self)
	}
	return "（插件所在目录）"
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// runFF 执行 ffmpeg 类命令，返回 stdout 与合并后的错误输出尾部。
func runFF(ctx context.Context, bin string, timeout time.Duration, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, args...)
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if cctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("执行超时（%s），已终止", timeout)
	}
	if err != nil {
		tail := tailLines(errBuf.String(), 12)
		return out.String(), fmt.Errorf("%s 执行失败：%v\n%s", filepath.Base(bin), err, tail)
	}
	return out.String(), nil
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ---------- media_info ----------

// ffprobeJSON 对应 ffprobe -print_format json 的关键字段。
type ffprobeJSON struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
		BitRate    string `json:"bit_rate"`
		Tags       struct {
			Title string `json:"title"`
		} `json:"tags"`
	} `json:"format"`
	Streams []struct {
		Index     int    `json:"index"`
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		RFrame    string `json:"r_frame_rate"`
		Channels  int    `json:"channels"`
		SampleRt  string `json:"sample_rate"`
		BitRate   string `json:"bit_rate"`
		Duration  string `json:"duration"`
	} `json:"streams"`
}

func mediaInfo(ctx context.Context, _ *mcp.CallToolRequest, args infoArgs) (*mcp.CallToolResult, any, error) {
	p := strings.TrimSpace(args.Path)
	if p == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	if !fileExists(p) {
		return nil, nil, fmt.Errorf("文件不存在：%s", p)
	}
	probe, err := findTool("ffprobe")
	if err != nil {
		return nil, nil, err
	}
	out, err := runFF(ctx, probe, 60*time.Second,
		"-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", p)
	if err != nil {
		return nil, nil, err
	}
	var info ffprobeJSON
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return nil, nil, fmt.Errorf("解析 ffprobe 输出失败：%w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "文件：%s\n", p)
	if sz, err := os.Stat(p); err == nil {
		fmt.Fprintf(&b, "体积：%s\n", humanSize(sz.Size()))
	}
	fmt.Fprintf(&b, "容器：%s\n", orDash(info.Format.FormatName))
	fmt.Fprintf(&b, "时长：%s\n", formatDuration(info.Format.Duration))
	if br := atoiSafe(info.Format.BitRate); br > 0 {
		fmt.Fprintf(&b, "总码率：%.2f Mbps\n", float64(br)/1e6)
	}
	if info.Format.Tags.Title != "" {
		fmt.Fprintf(&b, "标题：%s\n", info.Format.Tags.Title)
	}
	for _, s := range info.Streams {
		switch s.CodecType {
		case "video":
			fmt.Fprintf(&b, "视频轨 #%d：%s %dx%d", s.Index, s.CodecName, s.Width, s.Height)
			if s.RFrame != "" && s.RFrame != "0/0" {
				fmt.Fprintf(&b, " %s fps", fpsText(s.RFrame))
			}
			if br := atoiSafe(s.BitRate); br > 0 {
				fmt.Fprintf(&b, " %.2f Mbps", float64(br)/1e6)
			}
			b.WriteString("\n")
		case "audio":
			fmt.Fprintf(&b, "音频轨 #%d：%s %d 声道 %s Hz", s.Index, s.CodecName, s.Channels, orDash(s.SampleRt))
			if br := atoiSafe(s.BitRate); br > 0 {
				fmt.Fprintf(&b, " %d kbps", br/1000)
			}
			b.WriteString("\n")
		default:
			fmt.Fprintf(&b, "%s轨 #%d：%s\n", s.CodecType, s.Index, s.CodecName)
		}
	}
	return textResult(b.String()), nil, nil
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// fpsText 把 "30000/1001" 这类分数转成 "29.97"。
func fpsText(s string) string {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return s
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return s
	}
	v := num / den
	if v == float64(int(v)) {
		return strconv.Itoa(int(v))
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func formatDuration(sec string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(sec), 64)
	if err != nil || f <= 0 {
		return orDash(sec)
	}
	total := int(f + 0.5)
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d（%d 秒）", h, m, s, total)
	}
	return fmt.Sprintf("%02d:%02d（%d 秒）", m, s, total)
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// ---------- media_convert ----------

func mediaConvert(ctx context.Context, _ *mcp.CallToolRequest, args convertArgs) (*mcp.CallToolResult, any, error) {
	p := strings.TrimSpace(args.Path)
	out := strings.TrimSpace(args.Out)
	if p == "" || out == "" {
		return nil, nil, fmt.Errorf("path 与 out 都不能为空")
	}
	if !fileExists(p) {
		return nil, nil, fmt.Errorf("源文件不存在：%s", p)
	}
	if dir := filepath.Dir(out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
	}
	ff, err := findTool("ffmpeg")
	if err != nil {
		return nil, nil, err
	}

	var a []string
	a = append(a, "-hide_banner", "-nostdin", "-y")
	if args.Start != "" {
		a = append(a, "-ss", args.Start)
	}
	a = append(a, "-i", p)
	if args.Duration != "" {
		a = append(a, "-t", args.Duration)
	}

	switch {
	case args.CopyStream:
		a = append(a, "-c", "copy")
		if args.AudioOnly {
			a = append(a, "-vn")
		}
	default:
		if args.AudioOnly {
			a = append(a, "-vn")
		} else {
			if args.VideoCodec != "" {
				a = append(a, "-c:v", args.VideoCodec)
			}
			if args.Scale != "" {
				a = append(a, "-vf", "scale="+args.Scale)
			}
			// 未显式指定编码器时，只有给了 crf 才动编码参数
			if args.CRF > 0 && args.VideoCodec == "" {
				a = append(a, "-c:v", "libx264")
			}
			if args.CRF > 0 {
				if crf := clamp(args.CRF, 0, 51); crf > 0 {
					a = append(a, "-crf", strconv.Itoa(crf))
				}
			}
		}
		if args.AudioCodec != "" {
			a = append(a, "-c:a", args.AudioCodec)
		}
		if args.AudioRate != "" {
			a = append(a, "-b:a", args.AudioRate)
		}
	}
	a = append(a, out)

	if _, err := runFF(ctx, ff, convertTimeout, a...); err != nil {
		return nil, nil, err
	}
	return textResult(convertReport(p, out, "已转换", args)), nil, nil
}

func convertReport(src, out, verb string, args convertArgs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s：%s → %s\n", verb, src, out)
	if args.AudioOnly {
		b.WriteString("模式：仅音频（丢弃视频轨）\n")
	}
	if args.CopyStream {
		b.WriteString("模式：流复制（不重编码，快且无损）\n")
	}
	if args.VideoCodec != "" {
		fmt.Fprintf(&b, "视频编码：%s\n", args.VideoCodec)
	}
	if args.CRF > 0 {
		fmt.Fprintf(&b, "质量 CRF：%d\n", args.CRF)
	}
	if args.Scale != "" {
		fmt.Fprintf(&b, "分辨率：%s\n", args.Scale)
	}
	if args.AudioCodec != "" || args.AudioRate != "" {
		fmt.Fprintf(&b, "音频：%s %s\n", orDash(args.AudioCodec), orDash(args.AudioRate))
	}
	if args.Start != "" || args.Duration != "" {
		fmt.Fprintf(&b, "截取：起点 %s 时长 %s\n", orDash(args.Start), orDash(args.Duration))
	}
	appendSize(&b, out)
	return strings.TrimRight(b.String(), "\n")
}

func appendSize(b *strings.Builder, out string) {
	if fi, err := os.Stat(out); err == nil {
		fmt.Fprintf(b, "输出体积：%s\n", humanSize(fi.Size()))
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---------- media_extract_audio ----------

func mediaExtractAudio(ctx context.Context, _ *mcp.CallToolRequest, args extractAudioArgs) (*mcp.CallToolResult, any, error) {
	p := strings.TrimSpace(args.Path)
	if p == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	if !fileExists(p) {
		return nil, nil, fmt.Errorf("文件不存在：%s", p)
	}
	// out 可选：省略时按原名落到输出目录（默认用户的「下载」目录）
	out := strings.TrimSpace(args.Out)
	if out == "" {
		out = deriveOut(p, ".mp3")
	}
	ext := strings.ToLower(filepath.Ext(out))
	codec := strings.TrimSpace(args.Codec)
	if codec == "" {
		switch ext {
		case ".mp3":
			codec = "libmp3lame"
		case ".m4a", ".aac":
			codec = "aac"
		case ".wav":
			codec = "pcm_s16le"
		case ".flac":
			codec = "flac"
		case ".ogg", ".oga":
			codec = "libvorbis"
		default:
			return nil, nil, fmt.Errorf("不支持的音频输出后缀 %s（支持 .mp3/.m4a/.aac/.wav/.flac/.ogg）", ext)
		}
	}
	ff, err := findTool("ffmpeg")
	if err != nil {
		return nil, nil, err
	}
	if dir := filepath.Dir(out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
	}
	rate := args.Rate
	if rate == "" {
		rate = "192k"
	}
	if _, err := runFF(ctx, ff, convertTimeout,
		"-hide_banner", "-nostdin", "-y", "-i", p, "-vn", "-c:a", codec, "-b:a", rate, out); err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已提取音轨：%s → %s\n编码：%s（%s）\n", p, out, codec, rate)
	appendSize(&b, out)
	return textResult(strings.TrimRight(b.String(), "\n")), nil, nil
}

// ---------- media_thumbnail ----------

func mediaThumbnail(ctx context.Context, _ *mcp.CallToolRequest, args thumbArgs) (*mcp.CallToolResult, any, error) {
	p := strings.TrimSpace(args.Path)
	if p == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	if !fileExists(p) {
		return nil, nil, fmt.Errorf("文件不存在：%s", p)
	}
	// out 可选：省略时按原名落到输出目录（默认用户的「下载」目录）
	out := strings.TrimSpace(args.Out)
	if out == "" {
		out = deriveOut(p, "-thumb.jpg")
	}
	switch strings.ToLower(filepath.Ext(out)) {
	case ".jpg", ".jpeg", ".png":
	default:
		return nil, nil, fmt.Errorf("截图输出只支持 .jpg/.jpeg/.png")
	}
	at := strings.TrimSpace(args.Time)
	if at == "" {
		at = "1"
	}
	filter := ""
	switch {
	case args.Scale > 0:
		filter = fmt.Sprintf("scale=iw*%g:-2", args.Scale)
	case args.Width > 0:
		filter = fmt.Sprintf("scale=%d:-2", args.Width)
	}

	ff, err := findTool("ffmpeg")
	if err != nil {
		return nil, nil, err
	}
	if dir := filepath.Dir(out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
	}
	a := []string{"-hide_banner", "-nostdin", "-y", "-ss", at, "-i", p, "-frames:v", "1"}
	if filter != "" {
		a = append(a, "-vf", filter)
	}
	a = append(a, out)
	if _, err := runFF(ctx, ff, 5*time.Minute, a...); err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已截图：%s（第 %s 秒）→ %s\n", p, at, out)
	appendSize(&b, out)
	return textResult(strings.TrimRight(b.String(), "\n")), nil, nil
}
