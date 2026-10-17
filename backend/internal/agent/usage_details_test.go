package agent

import "testing"

func TestProviderUsageDetailsDoNotDoubleCount(t *testing.T) {
	u := usageFromValue(map[string]any{"usage": map[string]any{"input_tokens": 100, "output_tokens": 40, "input_tokens_details": map[string]any{"cached_tokens": 70}, "output_tokens_details": map[string]any{"reasoning_tokens": 30}}})
	if u.TotalTokens != 140 || u.CachedInputTokens == nil || *u.CachedInputTokens != 70 || u.ReasoningTokens == nil || *u.ReasoningTokens != 30 {
		t.Fatal(u)
	}
	u = usageFromValue(map[string]any{"prompt_tokens": 100, "completion_tokens": 40})
	if u.CachedInputTokens != nil || u.ReasoningTokens != nil {
		t.Fatal("unknown provider details became zero")
	}
}
