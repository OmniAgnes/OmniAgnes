package llm

import (
	"context"
	"fmt"
	"strings"
)

// OpenCodeBaseURL 是 opencode 网关（zen）的默认地址：OpenAI 兼容的
// /chat/completions 与 /models。免费档模型（id 以 -free 结尾）允许匿名调用，
// 不带任何 API Key 也能直接发请求；付费档才需要用户自己配 key。
const OpenCodeBaseURL = "https://opencode.ai/zen/v1"

func init() {
	Register("opencode", func(cfg ConfigP) (Provider, error) {
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = OpenCodeBaseURL
		}
		return &OpenCodeProvider{OpenAIProvider: &OpenAIProvider{
			baseURL: strings.TrimRight(baseURL, "/"),
			apiKey:  cfg.APIKey,
			model:   cfg.Model,
		}}, nil
	})
}

// OpenCodeProvider 走 opencode 网关。协议与 OpenAI 完全一致，因此内嵌
// OpenAIProvider 复用其流式解析、重试、空回复重发、思考参数等全部逻辑，
// 只改两处：类型名（配置里 type=opencode），以及模型清单只留免费档。
type OpenCodeProvider struct{ *OpenAIProvider }

func (p *OpenCodeProvider) Name() string { return "opencode" }

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

// FreeModel 报告模型 id 是否属于 opencode 免费档（网关的命名约定）。
func FreeModel(id string) bool {
	return strings.HasSuffix(id, "-free")
}