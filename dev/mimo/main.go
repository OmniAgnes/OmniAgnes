// mimo MCP 服务器：小米 MiMo 语音合成（TTS）插件。
//
// 工具：
//   - tts：文本转语音（mimo-v2.5-tts，可选 voicedesign / voiceclone）
//
// 认证：环境变量 MIMO_API_KEY（Bearer Token）。
// 可选环境变量 MIMO_BASE_URL，默认 https://api.xiaomimimo.com。
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
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultBaseURL = "https://api.xiaomimimo.com"

var ttsModels = []string{"mimo-v2.5-tts", "mimo-v2.5-tts-voicedesign", "mimo-v2.5-tts-voiceclone"}

type ttsArgs struct {
	Text   string `json:"text" jsonschema:"要合成的文本（可内嵌 (唱歌)、(怅然) 等情感标签）"`
	Model  string `json:"model,omitempty" jsonschema:"模型：mimo-v2.5-tts（默认，预置音色，支持唱歌）/ mimo-v2.5-tts-voicedesign（按描述定制音色）/ mimo-v2.5-tts-voiceclone（复刻音色）"`
	Voice  string `json:"voice,omitempty" jsonschema:"预置音色：冰糖/茉莉/苏打/白桦（中文），Mia/Chloe/Milo/Dean（英文），默认 冰糖。仅 mimo-v2.5-tts 有效"`
	Style  string `json:"style,omitempty" jsonschema:"风格指令（可选），如「温柔地、语速稍慢」；voicedesign 模型必填（音色描述）"`
	Format string `json:"format,omitempty" jsonschema:"音频格式：wav（默认）/ pcm16"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "omni-mimo", Version: "0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tts",
		Description: "用 MiMo 语音合成把文本转成语音，返回可直接播放的音频。",
		Annotations: &mcp.ToolAnnotations{Title: "语音合成"},
	}, tts)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "omni-mimo:", err)
		os.Exit(1)
	}
}

type ttsResponse struct {
	Choices []struct {
		Message struct {
			Audio struct {
				Data string `json:"data"` // base64 音频
			} `json:"audio"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func tts(ctx context.Context, req *mcp.CallToolRequest, args ttsArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Text) == "" {
		return nil, nil, fmt.Errorf("缺少 text 参数")
	}
	key := strings.TrimSpace(os.Getenv("MIMO_API_KEY"))
	if key == "" {
		return nil, nil, errors.New("未配置 MIMO_API_KEY 环境变量（在 插件 设置里为 mimo 服务器加 env）")
	}
	// 插件设置里可通过 MIMO_TTS_MODEL 环境变量固定默认模型
	model := ttsModels[0]
	if env := strings.TrimSpace(os.Getenv("MIMO_TTS_MODEL")); env != "" {
		for _, m := range ttsModels {
			if m == env {
				model = m
			}
		}
	}
	for _, m := range ttsModels {
		if m == args.Model {
			model = m
		}
	}

	messages := []map[string]any{}
	if style := strings.TrimSpace(args.Style); style != "" {
		messages = append(messages, map[string]any{"role": "user", "content": style})
	}
	messages = append(messages, map[string]any{"role": "assistant", "content": args.Text})

	audio := map[string]any{"format": orDefault(args.Format, "wav")}
	if voice := strings.TrimSpace(args.Voice); voice != "" && model == "mimo-v2.5-tts" {
		audio["voice"] = voice
	}

	body, _ := json.Marshal(map[string]any{
		"model":    model,
		"messages": messages,
		"audio":    audio,
		"stream":   false,
	})

	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, defaultBaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req2.Header.Set("Authorization", "Bearer "+key)
	req2.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req2)
	if err != nil {
		return nil, nil, fmt.Errorf("tts: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("tts: MiMo 接口错误（HTTP %d）: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	out := &ttsResponse{}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, nil, fmt.Errorf("tts: 解析响应失败: %w", err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return nil, nil, fmt.Errorf("tts: %s", out.Error.Message)
	}
	b64 := ""
	if len(out.Choices) > 0 {
		b64 = out.Choices[0].Message.Audio.Data
	}
	if b64 == "" {
		return nil, nil, fmt.Errorf("tts: 响应里没有音频数据")
	}

	format := orDefault(args.Format, "wav")
	mime := "audio/wav"
	if format == "pcm16" {
		mime = "audio/pcm" // 裸 PCM 无封装，浏览器多半不能播；建议保持 wav
	}
	return &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.AudioContent{Data: mustDecodeB64(b64), MIMEType: mime},
		&mcp.TextContent{Text: fmt.Sprintf("已用 %s 合成语音（%d 字符）。", model, len([]rune(args.Text)))},
	}}, nil, nil
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return strings.TrimSpace(s)
}

func mustDecodeB64(b64 string) []byte {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return []byte(b64) // 个别网关可能直接回原始字节
	}
	return b
}
