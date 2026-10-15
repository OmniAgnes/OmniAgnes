// image MCP 服务器：图片处理（压缩 / 缩放 / 格式转换 / 元信息）。
//
// 工具：
//   - image_info：读取图片的格式、尺寸、体积、色彩模型
//   - image_resize：缩放（等比装箱 / 指定宽高 / 按百分比），输出格式由 out 后缀决定
//   - image_convert：格式转换（png / jpg / gif），jpg 可指定质量
//   - image_compress：压缩（按质量重编 + 可选限制最大边长），主用于把大图变小
//
// 纯 Go 标准库（image/png、image/jpeg、image/gif），无 CGO、无外部依赖。
// 说明：标准库解码不会保留 EXIF，因此经本服务器重新编码输出的图片
// **EXIF/GPS 等元数据会被自动剥离**（这通常也是我们想要的）；
// 不支持 webp（标准库无编解码器），需要时请用 bash 调外部工具。
package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxEdge       = 20000    // 单边像素上限
	maxPixels     = 100 << 20 // 总像素上限（约 1 亿，防 OOM）
	defaultJPEGQ  = 85
)

type infoArgs struct {
	Path string `json:"path" jsonschema:"图片文件路径"`
}

type resizeArgs struct {
	Path   string  `json:"path" jsonschema:"源图片路径"`
	Out    string  `json:"out,omitempty" jsonschema:"输出路径；省略则自动加后缀（如 photo-800x600.jpg）"`
	Width  int     `json:"width,omitempty" jsonschema:"目标宽度（像素）"`
	Height int     `json:"height,omitempty" jsonschema:"目标高度（像素）"`
	Scale  float64 `json:"scale,omitempty" jsonschema:"缩放比例，如 0.5 = 缩小一半（与 width/height 二选一）"`
	Exact  bool    `json:"exact,omitempty" jsonschema:"是否严格拉伸到指定宽高（可能变形）；默认 false 表示等比缩放到不超过该宽高"`
}

type convertArgs struct {
	Path    string `json:"path" jsonschema:"源图片路径"`
	Out     string `json:"out" jsonschema:"输出路径，格式由后缀决定（.png/.jpg/.jpeg/.gif）"`
	Quality int    `json:"quality,omitempty" jsonschema:"jpg 质量 1-100，默认 85（png/gif 忽略）"`
}

type compressArgs struct {
	Path     string `json:"path" jsonschema:"源图片路径"`
	Out      string `json:"out,omitempty" jsonschema:"输出路径；省略则生成 <原名>-compressed.jpg"`
	Quality  int    `json:"quality,omitempty" jsonschema:"jpg 质量 1-100，默认 80（越低越小越糊）"`
	MaxWidth int    `json:"max_width,omitempty" jsonschema:"可选：限制最大宽度，超出才等比缩小"`
	MaxEdge  int    `json:"max_edge,omitempty" jsonschema:"可选：限制最大边长（宽高中较大者），超出才等比缩小"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "omni-image", Version: "0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "image_info",
		Annotations: &mcp.ToolAnnotations{Title: "图片信息", ReadOnlyHint: true},
		Description: "读取图片的格式、像素尺寸、文件体积与色彩模型。用于在处理前确认图片情况。",
	}, imageInfo)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "image_resize",
		Annotations: &mcp.ToolAnnotations{Title: "缩放图片"},
		Description: "缩放图片：等比缩放到指定宽高以内（默认）、严格拉伸（exact=true）或按比例（scale）。输出格式由 out 后缀决定，省略 out 自动生成文件名。",
	}, imageResize)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "image_convert",
		Annotations: &mcp.ToolAnnotations{Title: "图片格式转换"},
		Description: "图片格式转换：png / jpg / gif（按 out 后缀）。转 jpg 可指定质量。重新编码会剥离 EXIF/GPS 元数据。不支持 webp。",
	}, imageConvert)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "image_compress",
		Annotations: &mcp.ToolAnnotations{Title: "压缩图片"},
		Description: "压缩图片体积：按质量重编为 jpg，可选同时限制最大宽度/最大边长（超出才缩小）。适合把大图压小后再上传或插入文档。",
	}, imageCompress)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "omni-image:", err)
		os.Exit(1)
	}
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// ---------- image_info ----------

func imageInfo(_ context.Context, _ *mcp.CallToolRequest, args infoArgs) (*mcp.CallToolResult, any, error) {
	p := strings.TrimSpace(args.Path)
	if p == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	fi, err := os.Stat(p)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return nil, nil, fmt.Errorf("无法解码（支持 png/jpg/gif；webp 不支持）：%w", err)
	}
	b := img.Bounds()
	return textResult(fmt.Sprintf("文件：%s\n格式：%s\n尺寸：%d × %d 像素\n体积：%s\n色彩模型：%s",
		p, format, b.Dx(), b.Dy(), humanSize(fi.Size()), modelName(img))), nil, nil
}

// modelName 把色彩模型转成人类可读描述。
func modelName(img image.Image) string {
	if _, ok := img.ColorModel().(color.Palette); ok {
		return "调色板（Paletted）"
	}
	switch img.ColorModel() {
	case color.RGBAModel, color.RGBA64Model:
		return "RGBA（含透明通道）"
	case color.NRGBAModel, color.NRGBA64Model:
		return "NRGBA（含透明通道）"
	case color.GrayModel, color.Gray16Model:
		return "灰度"
	case color.YCbCrModel:
		return "YCbCr（JPEG 常用）"
	default:
		return fmt.Sprintf("%T", img.ColorModel())
	}
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

// ---------- image_resize ----------

func imageResize(_ context.Context, _ *mcp.CallToolRequest, args resizeArgs) (*mcp.CallToolResult, any, error) {
	p := strings.TrimSpace(args.Path)
	if p == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	src, _, err := decodeFile(p)
	if err != nil {
		return nil, nil, err
	}
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	var tw, th int
	switch {
	case args.Scale > 0:
		tw = int(math.Round(float64(sw) * args.Scale))
		th = int(math.Round(float64(sh) * args.Scale))
	case args.Width > 0 && args.Height > 0:
		if args.Exact {
			tw, th = args.Width, args.Height
		} else { // 等比装箱：完整放入 W×H 内
			r := math.Min(float64(args.Width)/float64(sw), float64(args.Height)/float64(sh))
			tw = int(math.Round(float64(sw) * r))
			th = int(math.Round(float64(sh) * r))
		}
	case args.Width > 0:
		tw = args.Width
		th = int(math.Round(float64(sh) * float64(args.Width) / float64(sw)))
	case args.Height > 0:
		th = args.Height
		tw = int(math.Round(float64(sw) * float64(args.Height) / float64(sh)))
	default:
		return nil, nil, fmt.Errorf("请给 width / height / scale 至少一个")
	}
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	if tw > maxEdge || th > maxEdge {
		return nil, nil, fmt.Errorf("目标尺寸 %d×%d 超过单边 %d 上限", tw, th, maxEdge)
	}

	out := strings.TrimSpace(args.Out)
	if out == "" {
		out = deriveName(p, fmt.Sprintf("-%dx%d", tw, th))
	}
	dst, err := resizeBilinear(src, tw, th)
	if err != nil {
		return nil, nil, err
	}
	if err := writeImage(out, dst, 0); err != nil {
		return nil, nil, err
	}
	outInfo, _ := os.Stat(out)
	size := int64(0)
	if outInfo != nil {
		size = outInfo.Size()
	}
	return textResult(fmt.Sprintf("已缩放：%s\n%d × %d → %d × %d\n输出：%s（%s）",
		p, sw, sh, tw, th, out, humanSize(size))), nil, nil
}

// ---------- image_convert ----------

func imageConvert(_ context.Context, _ *mcp.CallToolRequest, args convertArgs) (*mcp.CallToolResult, any, error) {
	p := strings.TrimSpace(args.Path)
	out := strings.TrimSpace(args.Out)
	if p == "" || out == "" {
		return nil, nil, fmt.Errorf("path 与 out 都不能为空")
	}
	format, err := formatFromExt(out)
	if err != nil {
		return nil, nil, err
	}
	src, srcFormat, err := decodeFile(p)
	if err != nil {
		return nil, nil, err
	}
	if err := writeImage(out, src, args.Quality); err != nil {
		return nil, nil, err
	}
	fi, _ := os.Stat(out)
	size := int64(0)
	if fi != nil {
		size = fi.Size()
	}
	b := src.Bounds()
	return textResult(fmt.Sprintf("已转换：%s（%s）→ %s（%s）\n尺寸：%d × %d\n输出体积：%s\n注：EXIF/GPS 元数据已剥离",
		p, srcFormat, out, format, b.Dx(), b.Dy(), humanSize(size))), nil, nil
}

// ---------- image_compress ----------

func imageCompress(_ context.Context, _ *mcp.CallToolRequest, args compressArgs) (*mcp.CallToolResult, any, error) {
	p := strings.TrimSpace(args.Path)
	if p == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	src, _, err := decodeFile(p)
	if err != nil {
		return nil, nil, err
	}
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	// 按需等比缩小
	tw, th := sw, sh
	if lim := args.MaxEdge; lim > 0 {
		if long := max(sw, sh); long > lim {
			r := float64(lim) / float64(long)
			tw, th = int(math.Round(float64(sw)*r)), int(math.Round(float64(sh)*r))
		}
	}
	if lim := args.MaxWidth; lim > 0 && tw > lim {
		r := float64(lim) / float64(tw)
		tw, th = lim, int(math.Round(float64(th)*r))
	}
	img := src
	if tw != sw || th != sh {
		scaled, err := resizeBilinear(src, tw, th)
		if err != nil {
			return nil, nil, err
		}
		img = scaled
	}

	out := strings.TrimSpace(args.Out)
	if out == "" {
		out = deriveName(p, "-compressed")
		if !strings.HasSuffix(strings.ToLower(out), ".jpg") && !strings.HasSuffix(strings.ToLower(out), ".jpeg") {
			out = strings.TrimSuffix(out, filepath.Ext(out)) + ".jpg"
		}
	}
	// 压缩目标固定 jpg（体积优势最大）
	if err := encodeJPEG(out, img, args.Quality); err != nil {
		return nil, nil, err
	}
	oldInfo, _ := os.Stat(p)
	newInfo, _ := os.Stat(out)
	var ratio string
	if oldInfo != nil && newInfo != nil && oldInfo.Size() > 0 {
		ratio = fmt.Sprintf("，体积 %s → %s（%.0f%%）", humanSize(oldInfo.Size()), humanSize(newInfo.Size()),
			float64(newInfo.Size())*100/float64(oldInfo.Size()))
	}
	q := args.Quality
	if q <= 0 || q > 100 {
		q = 80
	}
	return textResult(fmt.Sprintf("已压缩：%s\n尺寸：%d × %d → %d × %d\n质量：%d\n输出：%s%s",
		p, sw, sh, tw, th, q, out, ratio)), nil, nil
}

// ---------- 内部工具 ----------

func decodeFile(p string) (image.Image, string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return nil, "", fmt.Errorf("无法解码 %s（支持 png/jpg/gif；webp 不支持）：%w", p, err)
	}
	return img, format, nil
}

// writeImage 按输出后缀选择编码器写文件；quality<=0 时用默认值。
func writeImage(out string, img image.Image, quality int) error {
	format, err := formatFromExt(out)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	switch format {
	case "jpeg":
		return encodeJPEG(out, img, quality)
	case "png":
		return encodeFunc(out, func(w io.Writer) error { return png.Encode(w, img) })
	case "gif":
		return encodeFunc(out, func(w io.Writer) error {
			// NumColors=256 + FloydSteinberg；Quantizer 留空即用标准库默认 palette.Plan9
			return gif.Encode(w, img, &gif.Options{NumColors: 256, Drawer: draw.FloydSteinberg})
		})
	default:
		return fmt.Errorf("不支持的输出格式：%s", format)
	}
}

func encodeJPEG(out string, img image.Image, quality int) error {
	if quality <= 0 || quality > 100 {
		quality = defaultJPEGQ
	}
	// JPEG 无透明通道：有 alpha 时先合到白底，否则透明区域会变黑
	src := onWhite(img)
	if dir := filepath.Dir(out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := jpeg.Encode(f, src, &jpeg.Options{Quality: quality}); err != nil {
		return err
	}
	return f.Sync()
}

func encodeFunc(out string, fn func(io.Writer) error) error {
	if dir := filepath.Dir(out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := fn(f); err != nil {
		return err
	}
	return f.Sync()
}

// onWhite 把带透明通道的图合成到白底上；无 alpha 时原样返回。
func onWhite(img image.Image) image.Image {
	if _, ok := img.(*image.Gray); ok {
		return img
	}
	if op, ok := img.(interface{ Opaque() bool }); ok && op.Opaque() {
		return img
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
	return dst
}

// quantizerStep 已不再需要：gif.Options 留空 Quantizer 时标准库默认用 palette.Plan9。

func formatFromExt(p string) (string, error) {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png":
		return "png", nil
	case ".jpg", ".jpeg":
		return "jpeg", nil
	case ".gif":
		return "gif", nil
	case ".webp":
		return "", fmt.Errorf("webp 不支持（标准库无编解码器），请改用 png/jpg/gif")
	default:
		return "", fmt.Errorf("无法从后缀识别输出格式：%s（支持 .png/.jpg/.jpeg/.gif）", filepath.Ext(p))
	}
}

// deriveName 在源文件名后加后缀，扩展名保持不变。
func deriveName(src, suffix string) string {
	ext := filepath.Ext(src)
	return strings.TrimSuffix(src, ext) + suffix + ext
}

// resizeBilinear 双线性插值缩放。
func resizeBilinear(src image.Image, w, h int) (image.Image, error) {
	if w < 1 || h < 1 {
		return nil, fmt.Errorf("目标尺寸非法：%d×%d", w, h)
	}
	if int64(w)*int64(h) > maxPixels {
		return nil, fmt.Errorf("目标像素数 %d 超过上限 %d，请缩小目标尺寸", int64(w)*int64(h), int64(maxPixels))
	}
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if sw == 0 || sh == 0 {
		return dst, nil
	}
	scaleX := float64(sw) / float64(w)
	scaleY := float64(sh) / float64(h)
	for y := 0; y < h; y++ {
		fy := (float64(y)+0.5)*scaleY - 0.5
		y0 := int(math.Floor(fy))
		wy := fy - float64(y0)
		for x := 0; x < w; x++ {
			fx := (float64(x)+0.5)*scaleX - 0.5
			x0 := int(math.Floor(fx))
			wx := fx - float64(x0)
			// 四个邻点（越界钳制到边缘）
			r00, g00, b00, a00 := at(src, sb, x0, y0)
			r10, g10, b10, a10 := at(src, sb, x0+1, y0)
			r01, g01, b01, a01 := at(src, sb, x0, y0+1)
			r11, g11, b11, a11 := at(src, sb, x0+1, y0+1)
			r := lerp2(r00, r10, r01, r11, wx, wy)
			g := lerp2(g00, g10, g01, g11, wx, wy)
			b := lerp2(b00, b10, b01, b11, wx, wy)
			a := lerp2(a00, a10, a01, a11, wx, wy)
			dst.SetRGBA(x, y, color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)})
		}
	}
	return dst, nil
}

// at 取像素的 16 位非预乘分量（越界钳制）。
func at(src image.Image, sb image.Rectangle, x, y int) (uint32, uint32, uint32, uint32) {
	if x < sb.Min.X {
		x = sb.Min.X
	}
	if y < sb.Min.Y {
		y = sb.Min.Y
	}
	if x >= sb.Max.X {
		x = sb.Max.X - 1
	}
	if y >= sb.Max.Y {
		y = sb.Max.Y - 1
	}
	c := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
	return uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A)
}

// lerp2 对 4 个值做双线性插值；输入 0-255，内部放大 257 倍后由调用方 >>8 还原。
func lerp2(v00, v10, v01, v11 uint32, wx, wy float64) uint32 {
	top := float64(v00)*(1-wx) + float64(v10)*wx
	bot := float64(v01)*(1-wx) + float64(v11)*wx
	return uint32((top*(1-wy) + bot*wy) * 257)
}
