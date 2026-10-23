package llm

import (
	"context"
	"os"
	"testing"
)

func testMDModel(context, output int, inputs ...string) mdModel {
	m := mdModel{
		Limit:      &mdLimit{Context: context, Output: output},
		Modalities: &mdModalities{Input: inputs},
		ToolCall:   true,
		Reasoning:  true,
	}
	return m
}

func testRegistry() mdRegistry {
	return mdRegistry{
		"opencode": {
			API: "https://opencode.ai/inference/openai/v1",
			Models: map[string]mdModel{
				"deepseek-v4.1-flash": testMDModel(131072, 32768, "text"),
			},
		},
		"opencode-go": {
			API: "https://opencode.ai/inference/go/openai/v1",
			Models: map[string]mdModel{
				"deepseek-v4.1-flash": testMDModel(1000000, 384000, "text", "image"),
			},
		},
		"openai": {
			API: "https://api.openai.com/v1",
			Models: map[string]mdModel{
				"gpt-4o": testMDModel(128000, 16384, "text", "image"),
			},
		},
	}
}

// 同一模型名可能同时存在于多个供应商：必须按 base URL 选对那一条。
func TestLookupModelMetaByBaseURL(t *testing.T) {
	reg := testRegistry()
	meta, ok := lookupModelMeta(reg, "openai", "https://opencode.ai/inference/go/openai/v1", "deepseek-v4.1-flash")
	if !ok {
		t.Fatal("应命中 opencode-go")
	}
	if meta.Provider != "opencode-go" || meta.ContextWindow != 1000000 || meta.MaxOutputTokens != 384000 {
		t.Fatalf("命中错误: %+v", meta)
	}
	if len(meta.InputTypes) != 2 || meta.InputTypes[1] != "image" {
		t.Fatalf("input types = %v", meta.InputTypes)
	}

	// 同一网关的非 go 端点不能串到 go 的参数上
	meta, ok = lookupModelMeta(reg, "openai", "https://opencode.ai/inference/openai/v1", "deepseek-v4.1-flash")
	if !ok || meta.Provider != "opencode" || meta.ContextWindow != 131072 {
		t.Fatalf("命中了错误的供应商: %+v", meta)
	}
}

// base URL 的尾斜杠、端点后缀不影响匹配。
func TestNormalizeAPIURL(t *testing.T) {
	cases := map[string]string{
		"https://OpenCode.ai/inference/go/openai/v1/":                 "https://opencode.ai/inference/go/openai/v1",
		"https://opencode.ai/inference/go/openai/v1/chat/completions": "https://opencode.ai/inference/go/openai/v1",
		" https://api.openai.com/v1/ ":                                "https://api.openai.com/v1",
		"https://api.anthropic.com/v1/messages":                       "https://api.anthropic.com/v1",
		"":                                                            "",
	}
	for in, want := range cases {
		if got := normalizeAPIURL(in); got != want {
			t.Fatalf("normalizeAPIURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// 没有 base URL 且同名模型分属多个供应商时：参数一致才采用，否则不猜。
func TestLookupModelMetaAmbiguousName(t *testing.T) {
	// 参数一致：可以用
	same := mdRegistry{
		"a": {API: "https://a.example/v1", Models: map[string]mdModel{"m": testMDModel(1000, 100, "text")}},
		"b": {API: "https://b.example/v1", Models: map[string]mdModel{"m": testMDModel(1000, 100, "text")}},
	}
	if meta, ok := lookupModelMeta(same, "openai", "https://unknown.example/v1", "m"); !ok || meta.ContextWindow != 1000 {
		t.Fatalf("参数一致时应采用: %+v ok=%v", meta, ok)
	}
	// 参数不同：宁可不填也不配错
	diff := mdRegistry{
		"a": {API: "https://a.example/v1", Models: map[string]mdModel{"m": testMDModel(1000, 100, "text")}},
		"b": {API: "https://b.example/v1", Models: map[string]mdModel{"m": testMDModel(2000, 200, "text")}},
	}
	if meta, ok := lookupModelMeta(diff, "openai", "https://unknown.example/v1", "m"); ok {
		t.Fatalf("参数冲突时不应猜: %+v", meta)
	}
}

// 模型 key 带供应商前缀或只给了 canonical_model_id 时也要能命中。
func TestFindMDModelAliases(t *testing.T) {
	p := mdProvider{Models: map[string]mdModel{
		"deepseek/deepseek-chat": {CanonicalModelID: "deepseek-chat"},
	}}
	if _, ok := findMDModel(p, "deepseek-chat"); !ok {
		t.Fatal("应通过最后一段/canonical id 命中")
	}
	if got := normalizeInputTypes([]string{"TEXT", "image", " unknown ", "text"}); len(got) != 2 || got[0] != "text" || got[1] != "image" {
		t.Fatalf("normalizeInputTypes = %v", got)
	}
}

// 联网冒烟：验证真实 registry 里 OpenCode Go 的模型能按 base URL 命中。
// 默认跳过（CI 无网络/上游改数据都会让它不稳定），本地排查时设置 OMNI_LIVE_MODELSDEV=1。
func TestLookupModelMetaLiveOpencode(t *testing.T) {
	if os.Getenv("OMNI_LIVE_MODELSDEV") == "" {
		t.Skip("设置 OMNI_LIVE_MODELSDEV=1 以运行联网测试")
	}
	reg, err := fetchModelsDev(context.Background())
	if err != nil {
		t.Fatalf("拉取 models.dev 失败: %v", err)
	}
	meta, ok := lookupModelMeta(reg, "openai", "https://opencode.ai/inference/go/openai/v1", "deepseek-v4.1-flash")
	if !ok {
		t.Fatal("未命中 deepseek-v4.1-flash")
	}
	if meta.ContextWindow < 100000 || meta.MaxOutputTokens == 0 {
		t.Fatalf("上下文/输出参数异常: %+v", meta)
	}
	t.Logf("命中 %s: context=%d output=%d input=%v", meta.Provider, meta.ContextWindow, meta.MaxOutputTokens, meta.InputTypes)
}
