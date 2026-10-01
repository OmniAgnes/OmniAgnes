// agnes MCP 服务器：Agnes AI 生图 / 生视频插件。
//
// 工具：
//   - generate_image：文生图 / 图生图（agnes-image-2.5-flash / agnes-image-2.1-flash）
//   - generate_video：文生视频（agnes-video-2.5 / agnes-video-2.5-flash），异步任务轮询
//
// 认证：环境变量 AGNES_API_KEY（Bearer Token）。
// 可选环境变量 AGNES_BASE_URL，默认 https://api.agnes-ai.cn。
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultBaseURL = "https://api.agnes-ai.cn"
	pollInterval   = 3 * time.Second
	videoTimeout   = 10 * time.Minute // 视频生成上限
	refImageLimit  = 10 << 20         // 图生图参考图上限
)

// 可选模型列表，第一个为默认。
var (
	imageModels = []string{"agnes-image-2.5-flash", "agnes-image-2.1-flash"}
	videoModels = []string{"agnes-video-2.5-flash", "agnes-video-2.5"}
)

type generateImageArgs struct {
	Prompt   string `json:"prompt" jsonschema:"画面描述（生图指令）"`
	Model    string `json:"model,omitempty" jsonschema:"模型：agnes-image-2.5-flash（默认）/ agnes-image-2.1-flash"`
	Size     string `json:"size,omitempty" jsonschema:"尺寸档位：1K/2K/3K/4K，默认 2K"`
	Ratio    string `json:"ratio,omitempty" jsonschema:"宽高比：1:1/3:4/4:3/16:9/9:16/2:3/3:2/21:9，默认 1:1"`
	RefImage string `json:"ref_image,omitempty" jsonschema:"图生图参考图：本地文件路径或图片 URL（可省略）"`
}

type generateVideoArgs struct {
	Prompt      string `json:"prompt" jsonschema:"视频内容描述；reference 模式可用 <Picture N>/<Audio N> 指代素材"`
	Model       string `json:"model,omitempty" jsonschema:"模型：agnes-video-2.5-flash（默认）/ agnes-video-2.5"`
	Seconds     string `json:"seconds,omitempty" jsonschema:"时长秒数（字符串）：4~12，默认 5"`
	AspectRatio string `json:"aspect_ratio,omitempty" jsonschema:"宽高比：21:9/16:9（默认）/4:3/1:1/3:4/9:16"`
	Mode        string `json:"mode,omitempty" jsonschema:"text（默认，纯文生视频）/ keyframe（首尾帧驱动）/ reference（参考图或音频生成）"`
	FirstFrame  string `json:"first_frame,omitempty" jsonschema:"keyframe 模式：首帧图片的公网 URL（与 last_frame 至少一个）"`
	LastFrame   string `json:"last_frame,omitempty" jsonschema:"keyframe 模式：尾帧图片的公网 URL（与 first_frame 至少一个）"`
	Images      []string `json:"images,omitempty" jsonschema:"reference 模式：参考图片公网 URL 列表（最多 5 张）"`
	Audios      []string `json:"audios,omitempty" jsonschema:"reference 模式：参考音频公网 URL 列表（最多 3 段）"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "omni-agnes", Version: "0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "generate_image",
		Description: "用 Agnes AI 生成图片（文生图 / 图生图）。返回生成的图片（可供视觉模型直接分析）。",
		Annotations: &mcp.ToolAnnotations{Title: "生成图片"},
	}, generateImage)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "generate_video",
		Description: "用 Agnes AI 生成视频（文生视频）。提交任务后轮询直到完成，返回视频 URL。",
		Annotations: &mcp.ToolAnnotations{Title: "生成视频"},
	}, generateVideo)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "omni-agnes:", err)
		os.Exit(1)
	}
}

func pickModel(requested, envKey string, models []string) string {
	// 插件设置里可通过环境变量固定默认模型（如 AGNES_IMAGE_MODEL=agnes-image-2.1-flash）
	if env := strings.TrimSpace(os.Getenv(envKey)); env != "" {
		for _, m := range models {
			if m == env {
				return m
			}
		}
	}
	if requested != "" {
		for _, m := range models {
			if m == requested {
				return m
			}
		}
	}
	return models[0]
}

func apiKey() (string, error) {
	key := strings.TrimSpace(os.Getenv("AGNES_API_KEY"))
	if key == "" {
		return "", errors.New("未配置 AGNES_API_KEY 环境变量（在 插件 设置里为 agnes 服务器加 env）")
	}
	return key, nil
}

// api 调一次 Agnes 的 JSON 接口，data 非 nil 时响应体解析进 data。
func api(ctx context.Context, method, path string, body, data any) error {
	key, err := apiKey()
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(defaultBaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// 生图/生视频耗时较长，必须给足超时
	client := &http.Client{Timeout: 6 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Agnes 接口错误（HTTP %d）: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if data != nil {
		return json.Unmarshal(raw, data)
	}
	return nil
}

// ---------- 生图 ----------

type imageResponse struct {
	Data []struct {
		URL     string `json:"url"`
		B64JSON string `json:"b64_json"`
	} `json:"data"`
}

func generateImage(ctx context.Context, req *mcp.CallToolRequest, args generateImageArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Prompt) == "" {
		return nil, nil, fmt.Errorf("缺少 prompt 参数")
	}
	model := pickModel(args.Model, "AGNES_IMAGE_MODEL", imageModels)
	size, ratio := orDefault(args.Size, "2K"), orDefault(args.Ratio, "1:1")

	body := map[string]any{
		"model":  model,
		"prompt": args.Prompt,
		"size":   size,
		"ratio":  ratio,
	}
	// 图生图：参考图转 data URI 塞进 extra_body.image
	if ref := strings.TrimSpace(args.RefImage); ref != "" {
		uri, err := imageDataURI(ref)
		if err != nil {
			return nil, nil, err
		}
		body["extra_body"] = map[string]any{"image": []string{uri}, "response_format": "b64_json"}
	}

	var res imageResponse
	if err := api(ctx, http.MethodPost, "/v1/images/generations", body, &res); err != nil {
		return nil, nil, fmt.Errorf("generate_image: %w", err)
	}
	if len(res.Data) == 0 {
		return nil, nil, fmt.Errorf("generate_image: 接口未返回图片")
	}

	var content []mcp.Content
	if b64 := res.Data[0].B64JSON; b64 != "" {
		content = append(content,
			&mcp.ImageContent{Data: mustDecodeB64(b64), MIMEType: "image/png"},
			&mcp.TextContent{Text: fmt.Sprintf("已用 %s 生成图片（%s，%s）。", model, size, ratio)},
		)
	} else if url := res.Data[0].URL; url != "" {
		// 尽量下载转内联图片，让模型也能直接看图；失败退回资源链接（前端可预览）
		if data, mime, err := download(url); err == nil {
			content = append(content, &mcp.ImageContent{Data: data, MIMEType: mime})
		} else {
			content = append(content, &mcp.ResourceLink{Name: "生成的图片", URI: url, MIMEType: "image/png"})
		}
		content = append(content, &mcp.TextContent{Text: fmt.Sprintf("已用 %s 生成图片：%s", model, url)})
	}
	if content == nil {
		return nil, nil, fmt.Errorf("generate_image: 响应里既无 b64_json 也无 url")
	}
	return &mcp.CallToolResult{Content: content}, nil, nil
}

// imageDataURI 把参考图转成 data URI（本地路径读文件；URL 下载）。
func imageDataURI(ref string) (string, error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		data, mime, err := download(ref)
		if err != nil {
			return "", fmt.Errorf("参考图下载失败: %w", err)
		}
		return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data)), nil
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		return "", fmt.Errorf("参考图读取失败: %w", err)
	}
	if len(data) > refImageLimit {
		return "", fmt.Errorf("参考图过大（%.1f MB，上限 10 MB）", float64(len(data))/(1<<20))
	}
	mime := "image/png"
	switch {
	case strings.HasSuffix(strings.ToLower(ref), ".jpg"), strings.HasSuffix(strings.ToLower(ref), ".jpeg"):
		mime = "image/jpeg"
	case strings.HasSuffix(strings.ToLower(ref), ".webp"):
		mime = "image/webp"
	}
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data)), nil
}

// ---------- 生视频 ----------

type videoCreateResponse struct {
	ID string `json:"video_id"`
}

type videoStatusResponse struct {
	Status   string `json:"status"` // completed / failed / processing 等
	Progress int    `json:"progress"`
	URL      string `json:"url"`
	Error    string `json:"error,omitempty"`
}

func generateVideo(ctx context.Context, req *mcp.CallToolRequest, args generateVideoArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Prompt) == "" {
		return nil, nil, fmt.Errorf("缺少 prompt 参数")
	}
	model := pickModel(args.Model, "AGNES_VIDEO_MODEL", videoModels)
	seconds := orDefault(args.Seconds, "5")
	if n, err := strconv.Atoi(seconds); err != nil || n < 4 || n > 12 {
		return nil, nil, fmt.Errorf("seconds 需为 4~12 的数字字符串，收到 %q", seconds)
	}
	ratio := orDefault(args.AspectRatio, "16:9")
	mode := orDefault(args.Mode, "text")

	body := map[string]any{
		"model":        model,
		"prompt":       args.Prompt,
		"seconds":      seconds,
		"mode":         mode,
		"size":         "720P", // flash 模型仅支持 720P
		"aspect_ratio": ratio,
	}
	// 模式专用参数：URL 直接透传；本地文件转 data URI 直传内容
	// （文档只承诺公网 URL，但网关普遍兼容 data URI；服务端若拒绝，错误原样返回给模型）
	switch mode {
	case "keyframe":
		first, last := strings.TrimSpace(args.FirstFrame), strings.TrimSpace(args.LastFrame)
		if first == "" && last == "" {
			return nil, nil, fmt.Errorf("keyframe 模式需要 first_frame / last_frame 至少一个（图片 URL 或本地路径）")
		}
		for name, u := range map[string]string{"first_frame": first, "last_frame": last} {
			if u == "" {
				continue
			}
			ref, err := mediaRef(u)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", name, err)
			}
			body[name] = ref
		}
	case "reference":
		images, err := filterMedia(args.Images, "参考图", 5)
		if err != nil {
			return nil, nil, err
		}
		audios, err := filterMedia(args.Audios, "参考音频", 3)
		if err != nil {
			return nil, nil, err
		}
		if len(images) == 0 && len(audios) == 0 {
			return nil, nil, fmt.Errorf("reference 模式需要 images（≤5）或 audios（≤3）至少一项")
		}
		if len(images) > 0 {
			body["images"] = images
		}
		if len(audios) > 0 {
			body["audios"] = audios
		}
	default:
		mode = "text"
	}

	create := &videoCreateResponse{}
	if err := api(ctx, http.MethodPost, "/v1/videos", body, create); err != nil {
		return nil, nil, fmt.Errorf("generate_video: %w", err)
	}
	if create.ID == "" {
		return nil, nil, fmt.Errorf("generate_video: 响应缺少 video_id")
	}

	// 轮询任务状态（文档建议 1~2s 一次，这里 3s 保守一些）
	deadline := time.Now().Add(videoTimeout)
	for {
		st := &videoStatusResponse{}
		if err := api(ctx, http.MethodGet, "/agnesapi?video_id="+create.ID+"&model_name="+model, nil, st); err != nil {
			return nil, nil, fmt.Errorf("generate_video: 查询任务失败: %w", err)
		}
		switch st.Status {
		case "completed":
			if st.URL == "" {
				return nil, nil, fmt.Errorf("generate_video: 任务完成但没有视频 URL")
			}
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.ResourceLink{Name: "生成的视频", URI: st.URL, MIMEType: "video/mp4"},
				&mcp.TextContent{Text: fmt.Sprintf("已用 %s 生成 %s 秒视频（%s）：%s", model, seconds, ratio, st.URL)},
			}}, nil, nil
		case "failed":
			return nil, nil, fmt.Errorf("generate_video: 任务失败 %s", st.Error)
		}
		if time.Now().After(deadline) {
			return nil, nil, fmt.Errorf("generate_video: 等待超时（%s），任务 %s 可能仍在进行", videoTimeout, create.ID)
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// ---------- 小工具 ----------

func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// mediaRef 把素材引用转成 API 需要的形式：公网 URL 透传；本地文件读出转 data URI 直传内容。
func mediaRef(u string) (string, error) {
	if isHTTPURL(u) {
		return u, nil
	}
	if _, err := os.Stat(u); err != nil {
		return "", fmt.Errorf("素材不是可访问的 URL，也不是存在的本地文件: %s", u)
	}
	return imageDataURI(u)
}

// filterMedia 批量处理素材引用（URL 或本地路径），并校验数量上限。
func filterMedia(list []string, label string, limit int) ([]string, error) {
	var out []string
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		ref, err := mediaRef(s)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		out = append(out, ref)
	}
	if len(out) > limit {
		return nil, fmt.Errorf("%s最多 %d 项，收到 %d 项", label, limit, len(out))
	}
	return out, nil
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return strings.TrimSpace(s)
}

func download(url string) (data []byte, mime string, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, "", err
	}
	mime = resp.Header.Get("Content-Type")
	if mime == "" || mime == "application/octet-stream" {
		mime = "image/png"
	}
	return data, mime, nil
}

func mustDecodeB64(b64 string) []byte {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return []byte(b64) // 个别网关可能直接回原始字节
	}
	return b
}
