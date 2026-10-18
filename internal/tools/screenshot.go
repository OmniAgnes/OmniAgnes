package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"omni/internal/llm"
)

// maxImageBytes 单张图片送入模型的上限（base64 前的原始字节）。
// 视觉模型对图片大小敏感，超过 ~4MB 往往被网关拒收。
const maxImageBytes = 4 << 20 // 4MB

// captureOpts 是抓屏的可选参数。
type captureOpts struct {
	// window 按标题关键词指定要截的窗口（Windows 生效，其他平台忽略）
	window string
}

// AttachmentKind 是工具产出的多媒体类型。
type AttachmentKind = llm.AttachmentKind

const (
	KindImage = llm.KindImage
	KindVideo = llm.KindVideo
	KindAudio = llm.KindAudio
)

// Attachment 是一条多媒体产出（图片/视频/音频）。与 llm.Attachment 同类型，
// 便于插件工具直接返回。
type Attachment = llm.Attachment

func screenshotTool() *Tool {
	return &Tool{
		Name: "screenshot",
		Description: "截取屏幕/窗口，或读取本地图片，并把它直接交给模型（供视觉模型分析界面、报错截图、图表）。" +
			"参数：path=读取本地图片文件（仅限 png/jpg/webp/gif/bmp/svg，PDF 等文档不是图片、会被拒绝）；" +
			"window=按标题关键词截取指定窗口（如 'Visual Studio Code'，不传则截最前面的窗口）；" +
			"region={x,y,width,height} 截取窗口内的局部区域（相对窗口左上角）。" +
			"截图会保存到工作目录 .omni/screenshots/ 并返回该路径。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "改为读取本地图片文件（与 window/region 二选一）",
				},
				"window": map[string]any{
					"type":        "string",
					"description": "按标题关键词截取指定窗口；省略则截最前面的窗口",
				},
				"region": map[string]any{
					"type":        "object",
					"description": "截取窗口内的局部区域（相对窗口左上角）",
					"properties": map[string]any{
						"x":      map[string]any{"type": "integer", "description": "左上角 x"},
						"y":      map[string]any{"type": "integer", "description": "左上角 y"},
						"width":  map[string]any{"type": "integer", "description": "宽度"},
						"height": map[string]any{"type": "integer", "description": "高度"},
					},
				},
			},
		},
		ReadOnly: true,
		Execute: func(ctx context.Context, args map[string]any, env *Env) (string, error) {
			out, _, err := execScreenshot(ctx, args, env)
			return out, err
		},
		ExecuteMedia: execScreenshot,
	}
}

// execScreenshot 抓屏或读图，返回文本摘要 + 多媒体附件（供 UI 展示与视觉模型消费）。
func execScreenshot(ctx context.Context, args map[string]any, env *Env) (string, []llm.Attachment, error) {
	// 分支1：读本地图片
	if p, _ := args["path"].(string); strings.TrimSpace(p) != "" {
		return readImageFile(p, env)
	}

	// 分支2：截屏 / 截窗口
	win, _ := args["window"].(string)
	img, err := captureScreen(args["region"], captureOpts{window: win})
	if err != nil {
		return "", nil, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", nil, fmt.Errorf("screenshot: 编码 PNG 失败: %w", err)
	}
	if buf.Len() > maxImageBytes {
		return "", nil, fmt.Errorf("screenshot: 截图过大（%.1f MB），请用 region 缩小范围",
			float64(buf.Len())/(1<<20))
	}

	// 存盘（便于用户查看 / 后续引用）
	saved := ""
	if env != nil && env.Cwd != "" {
		dir := filepath.Join(env.Cwd, ".omni", "screenshots")
		if err := os.MkdirAll(dir, 0o755); err == nil {
			fp := filepath.Join(dir, fmt.Sprintf("shot-%d.png", time.Now().Unix()))
			if err := os.WriteFile(fp, buf.Bytes(), 0o644); err == nil {
				saved = fp
			}
		}
	}

	b := img.Bounds()
	desc := "已截取屏幕"
	if strings.TrimSpace(win) != "" {
		desc = fmt.Sprintf("已截取窗口「%s」", win)
	}
	text := fmt.Sprintf("%s（%dx%d，%.1f KB）", desc, b.Dx(), b.Dy(), float64(buf.Len())/1024)
	if saved != "" {
		text += fmt.Sprintf("，已保存到 %s", saved)
	}
	// 落盘了就用 Path 引用（省去把 base64 塞进历史 JSON，agent 按需读文件送给模型）；
	// 没落盘才内联 data URL。截图的目的就是给模型看，标记 ForModel。
	att := llm.Attachment{Kind: llm.KindImage, MediaType: "image/png", Caption: text, ForModel: true}
	if saved != "" {
		att.Path = saved
	} else {
		att.Data = base64.StdEncoding.EncodeToString(buf.Bytes())
	}
	return text, []llm.Attachment{att}, nil
}

// readImageFile 读本地图片并转成模型可用的 base64。
func readImageFile(p string, env *Env) (string, []llm.Attachment, error) {
	abs, err := resolvePath(env.Cwd, p, env.Unsafe)
	if err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", nil, fmt.Errorf("screenshot: 读取失败: %w", err)
	}
	if len(data) > maxImageBytes {
		return "", nil, fmt.Errorf("screenshot: 图片过大（%.1f MB），上限 %d MB；请先裁剪或压缩",
			float64(len(data))/(1<<20), maxImageBytes>>20)
	}
	// 只接受真图片：按魔数校验，防止把 PDF 等非图片文件误标成 image/png
	// 送给模型（上游会对内容与 media_type 不符直接 400，历史踩坑见会话排查）。
	// PDF/Word 等文档请让用户直接打开，或用 write_pdf 等生成类工具产出（自动展示）。
	sniffed := llm.ImageType(data)
	if strings.ToLower(filepath.Ext(abs)) != ".svg" && sniffed == "" && !llm.IsSVG(data) {
		return "", nil, fmt.Errorf("screenshot: %s 不是图片文件（无 PNG/JPEG/WebP/GIF/BMP/SVG 内容）。" +
			"展示 PDF 等生成的文档请通过 write_pdf 生成（附件会自动展示给用户），或直接告诉用户文件路径",
			abs)
	}
	// 魔数识别出的真实类型比按扩展名猜的更可靠，优先采用
	mt := imageMime(abs)
	if sniffed != "" {
		mt = sniffed
	}
	w, h := imageDims(data)
	text := fmt.Sprintf("已读取图片 %s（%dx%d，%.1f KB）", abs, w, h, float64(len(data))/1024)
	// 用 Path 引用而非内联 base64：避免把几 MB 的图塞进历史 JSON，
	// agent 需要送模型时会按 Path 读文件。
	return text, []llm.Attachment{{
		Kind:      llm.KindImage,
		MediaType: mt,
		Path:      abs,
		Caption:   text,
		ForModel:  true, // 读图的目的就是给模型分析
	}}, nil
}

func imageMime(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/png"
	}
}

// imageDims 尽力解析图片尺寸，失败返回 0（不影响主流程）
func imageDims(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}
