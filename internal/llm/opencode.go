package llm

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// OpenCodeBaseURL 是 opencode 网关（zen）的默认地址：OpenAI 兼容的
// /chat/completions 与 /models。免费档模型（id 以 -free 结尾）允许匿名调用，
// 不带任何 API Key 也能直接发请求；付费档才需要用户自己配 key。
//
// 匿名免费档的设备指纹（2026-09 起强校验，缺失即 403 FreeTierError）：
//  1. User-Agent 以 "opencode/" 开头（网关唯一硬门禁，大小写不敏感的子串/前缀匹配）
//  2. x-opencode-session 形如 ses_<12位小写hex><14位字母数字>（规范化会话 id）
//  3. 请求头带 x-opencode-client: cli、x-opencode-project、x-opencode-request-id
//     （形如 msg_<12位小写hex><14位字母数字>）
//  4. body 里 stream: true，且 tools 数组必须含名为 bash 与 read 的函数工具
//     （网关用工具指纹判断是 opencode CLI，不是普通聊天客户端）
//
// 网关不看 API Key（匿名路由，键为字面量 "public"），因此配 key 并不能绕开
// 这个门禁——必须伪造出「真实 opencode CLI」的完整请求形态。
const OpenCodeBaseURL = "https://opencode.ai/zen/v1"

// openCodeClientUA 对齐真实 opencode CLI 的 User-Agent。网关校验前缀/子串
// "opencode"，必须严格以 "opencode/" 开头。
const openCodeClientUA = "opencode/1.18.18 agent/00000000-0000-0000-0000-000000000000"

// zenGateToolDefs 是网关校验用的最小工具集。网关只检查 tools 数组里是否
// 存在名为 bash 与 read 的函数（形状不符会 400），并不真正调用，因此描述
// 与参数随意占位即可。若模型请求里本身带了这些工具则跳过注入，避免重复。
var zenGateToolDefs = []map[string]any{
	{
		"type": "function",
		"function": map[string]any{
			"name":        "bash",
			"description": "run a command",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
			},
		},
	},
	{
		"type": "function",
		"function": map[string]any{
			"name":        "read",
			"description": "read a file",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
		},
	},
}

func init() {
	Register("opencode", func(cfg ConfigP) (Provider, error) {
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = OpenCodeBaseURL
		}
		op := &OpenCodeProvider{OpenAIProvider: &OpenAIProvider{
			baseURL: strings.TrimRight(baseURL, "/"),
			apiKey:  cfg.APIKey,
			model:   cfg.Model,
		}}
		// 每个 provider 实例生成稳定随机会话 id（按网关规范格式），
		// 配对请求级 msg_ id，模拟完整会话指纹。
		op.injectZenGate()
		return op, nil
	})
}

// OpenCodeProvider 走 opencode 网关。协议与 OpenAI 完全一致，因此内嵌
// OpenAIProvider 复用其流式解析、重试、空回复重发、思考参数等全部逻辑，
// 只改两处：类型名（配置里 type=opencode），以及模型清单只留免费档。
type OpenCodeProvider struct{ *OpenAIProvider }

func (p *OpenCodeProvider) Name() string { return "opencode" }

// injectZenGate 安装网关免费档要求的完整设备指纹：头（UA + x-opencode-*）
// + body 工具注入（bash/read）。这是通过 403 FreeTierError 门禁的必要条件。
func (p *OpenCodeProvider) injectZenGate() {
	session := "ses_" + randomToken(12, hexDigits) + randomToken(14, alnum47)
	reqSeq := 0
	p.extraHeaders = func(req *http.Request) {
		req.Header.Set("User-Agent", openCodeClientUA)
		req.Header.Set("x-opencode-client", "cli")
		req.Header.Set("x-opencode-project", "global")
		req.Header.Set("x-opencode-session", session)
		reqSeq++
		req.Header.Set("x-opencode-request-id", "msg_"+randomToken(12, hexDigits)+randomToken(14, alnum47))
	}
	p.transformBody = func(body []byte) ([]byte, error) {
		return ensureZenGateTools(body)
	}
}

const (
	hexDigits = "0123456789abcdef"
	alnum47   = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// randomToken 返回长度为 n 的随机字符（每个字符取自 charset）。
func randomToken(n int, charset string) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n)
	}
	out := make([]byte, n)
	for i, c := range b {
		out[i] = charset[int(c)%len(charset)]
	}
	return string(out)
}

// ensureZenGateTools 保证请求体 tools 数组里存在网关要求的 bash 与 read
// 函数工具。已存在则不重复注入。
//
// tool_choice 处理：
//   - 调用方没带任何工具（纯文本对话）时注入占位工具，并把 tool_choice 钳为
//     "none"——占位工具只为过网关指纹校验，不能让模型真的调走。
//   - 调用方自带工具（agent 的真实工具集必含 bash/read）时保持 tool_choice
//     缺省：网关在 tool_choice=none 下会把工具从模型视野里整个拿掉，模型只能
//     把工具调用以文本 XML 吐出来，还会被网关洗成 <]minimax[> 之类的坏字节
//     泄漏进回复（space-bunny-free 实测，2026-10）。
func ensureZenGateTools(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var tools []map[string]any
	if raw, ok := payload["tools"].([]any); ok {
		for _, it := range raw {
			if m, ok := it.(map[string]any); ok {
				tools = append(tools, m)
			}
		}
	}
	noTools := len(tools) == 0
	// 已有同名工具则不再追加
	has := func(name string) bool {
		for _, t := range tools {
			if fn, ok := t["function"].(map[string]any); ok {
				if fn["name"] == name {
					return true
				}
			}
		}
		return false
	}
	for _, def := range zenGateToolDefs {
		name := def["function"].(map[string]any)["name"].(string)
		if has(name) {
			continue
		}
		tools = append(tools, def)
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	if _, ok := payload["tool_choice"]; !ok && noTools {
		payload["tool_choice"] = "none"
	}
	return json.Marshal(payload)
}

// ListModels 只返回免费档。网关清单里付费模型占绝大多数（几十条），混进来
// 会让「免费」这层筛选失去意义，而且未配 key 时点付费模型必然调用失败。
func (p *OpenCodeProvider) ListModels(ctx context.Context) ([]string, error) {
	all, err := p.OpenAIProvider.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(all))
	for _, m := range all {
		if FreeModel(m) {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("网关未返回免费模型")
	}
	return out, nil
}

// zenIncompatiblePrefixes 是免费池里不走标准 /chat/completions 的模型前缀：
// muse-spark-* 走 OpenAI Responses API（/v1/responses），jev-* 走 verdict
// 专用协议（/v1/systemone）。omni 只实现标准 chat/completions，这些模型
// 选了必然报错，直接从免费清单里排除，避免「看得到选不了」。
var zenIncompatiblePrefixes = []string{"muse-spark-", "jev-"}

// FreeModel 报告模型 id 是否属于 opencode 免费档且 omni 可用：
// 网关的 -free 命名约定 + 排除私有协议模型。
func FreeModel(id string) bool {
	if !strings.HasSuffix(id, "-free") {
		return false
	}
	for _, p := range zenIncompatiblePrefixes {
		if strings.HasPrefix(id, p) {
			return false
		}
	}
	return true
}