// ES_OLLAMA_LOCAL_REASONING_PATCH 验证测试
// 目的：证明本机 Ollama 端点现在能选中 none，且 applyReasoning 会真正注入关闭思考参数。
package agent

import (
	"testing"

	"easy-stock/backend/internal/appsettings"
)

func TestLocalOllamaCapabilityAllowsNone(t *testing.T) {
	cases := []struct {
		baseURL string
		mode    string
		allow   bool
	}{
		{"http://127.0.0.1:11434/v1", "chat_completions", true},
		{"http://localhost:11434/v1", "chat_completions", true},
		{"http://192.168.1.50:11434/v1", "chat_completions", true},
		{"http://10.0.0.7:1234/v1", "chat_completions", true},
		{"http://172.20.3.9:8000/v1", "chat_completions", true},
		// 公网域名绝不能被误判
		{"https://api.openai.com/v1", "chat_completions", false},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "chat_completions", false},
		{"http://172.15.0.1:11434/v1", "chat_completions", false},
	}
	for _, tc := range cases {
		cfg := appsettings.LLM{Model: "qwen3.6:35b", BaseURL: tc.baseURL, APIMode: tc.mode}
		c := OfficialReasoningCapability(cfg)
		got := c.Allows("none")
		if got != tc.allow {
			t.Errorf("Allows(none) for %s mode=%s: got %v want %v (options=%v)",
				tc.baseURL, tc.mode, got, tc.allow, optionValues(c))
		}
		if got && c.Normalize("none") != "none" {
			t.Errorf("Normalize(none) for %s = %q, want none", tc.baseURL, c.Normalize("none"))
		}
	}
}

// 端到端：applyReasoning 在 effort=none 时必须写入 extra_body.reasoning
func TestApplyReasoningInjectsLocalOllamaNone(t *testing.T) {
	r := &HermesRuntime{}
	cfg := appsettings.LLM{Model: "qwen3.6:35b", BaseURL: "http://127.0.0.1:11434/v1", APIMode: "chat_completions"}
	c := OfficialReasoningCapability(cfg)

	config := map[string]any{
		"providers": map[string]any{
			providerSlug: map[string]any{
				"name":          "自定义模型",
				"transport":     "chat_completions",
				"default_model": "qwen3.6:35b",
			},
		},
	}
	// r.reasoningCapability 无 python 时回退 OfficialReasoningCapability
	r.applyReasoning(config, cfg, "none")

	agent, _ := stringMap(config["agent"])
	if got := stringValue(agent["easy_stock_reasoning_effort"]); got != "none" {
		t.Fatalf("easy_stock_reasoning_effort = %q, want none", got)
	}

	providers, _ := stringMap(config["providers"])
	provider, _ := stringMap(providers[providerSlug])
	extra, _ := stringMap(provider["extra_body"])
	if extra == nil {
		t.Fatal("extra_body 未写入")
	}
	reasoning, _ := stringMap(extra["reasoning"])
	if reasoning == nil {
		t.Fatalf("extra_body.reasoning 未注入, extra=%v capability wire=%s options=%v", extra, c.Wire, optionValues(c))
	}
	if enabled, ok := reasoning["enabled"].(bool); !ok || enabled {
		t.Fatalf("reasoning.enabled = %v, want false", reasoning["enabled"])
	}
	if effort := stringValue(reasoning["effort"]); effort != "none" {
		t.Fatalf("reasoning.effort = %q, want none", effort)
	}
	t.Logf("OK: extra_body.reasoning = %v", reasoning)
}
