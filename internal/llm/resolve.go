package llm

import (
	"fmt"
	"os"
	"strings"

	"omni/internal/config"
)

// ResolveProvider 从配置（providers 列表）或环境变量解析可用的 Provider。
// 返回 (provider, 模型名, error)；规则与 main 的历史实现一致：
// 优先匹配 cfg.Provider/model 指向的模型名，其次 Default 标记，最后第一个。
func ResolveProvider(cfg *config.Config) (Provider, string, error) {
	if len(cfg.Providers) > 0 {
		target := cfg.Provider
		if target == "" {
			target = cfg.Model
		}
		// 内置模型（server: 前缀）不属于任何本地供应商：跳过按名反查，回落默认/首个
		// provider，模型名取去掉前缀后的真名——否则会被用户自配的同名模型劫持
		builtinName := ""
		if strings.HasPrefix(target, config.BuiltinModelPrefix) {
			builtinName = strings.TrimPrefix(target, config.BuiltinModelPrefix)
			target = ""
		}
		var pick *config.ProviderConfig
		if target != "" {
			for i := range cfg.Providers {
				if cfg.Providers[i].Model == target || cfg.Providers[i].HasModel(target) {
					pick = &cfg.Providers[i]
					break
				}
			}
		}
	// 目标指向的供应商若无可用 key（远程 API），跳过——避免 resolve 落到失效条目上
	if pick != nil && pick.ResolveAPIKey() == "" && !pick.NoAuth() {
		pick = nil
	}
	if pick == nil {
		for i := range cfg.Providers {
			if cfg.Providers[i].Default {
				pick = &cfg.Providers[i]
				break
			}
		}
	}
	if pick == nil {
		pick = &cfg.Providers[0]
	}
		p := *pick
		apiKey := p.ResolveAPIKey()
		// ollama/本地端点无需 key
		needKey := apiKey == "" && !p.NoAuth()
		if needKey {
			return nil, "", fmt.Errorf("provider %s 缺少 API Key：请配置 providers[].api_key 或 env_key，或设置对应环境变量", p.Type)
		}
		prov, err := New(ConfigP{Type: p.Type, Model: p.Model, BaseURL: p.BaseURL, APIKey: apiKey})
		if err != nil {
			return nil, "", err
		}
		model := p.Model
		if builtinName != "" {
			model = builtinName
		}
		return prov, model, nil
	}

	typ, model, base, ok := config.DetectFromEnv()
	if !ok {
		return nil, "", fmt.Errorf(
			"未找到可用的 LLM Provider。请在 %s 配置 providers，或设置 OPENAI_API_KEY / ANTHROPIC_API_KEY / GEMINI_API_KEY / OLLAMA_BASE_URL 环境变量",
			config.DefaultPath(),
		)
	}
	apiKey := ""
	switch typ {
	case "openai":
		apiKey = os.Getenv("OPENAI_API_KEY")
	case "anthropic":
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	case "gemini":
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	prov, err := New(ConfigP{Type: typ, Model: model, BaseURL: base, APIKey: apiKey})
	if err != nil {
		return nil, "", err
	}
	return prov, model, nil
}
