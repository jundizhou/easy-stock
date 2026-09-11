package hermes

import (
	"context"
	"fmt"
	"strings"
)

type TokenUsage struct {
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Model            string `json:"model,omitempty"`
}

type usageModuleContextKey struct{}

func WithUsageModule(ctx context.Context, module string) context.Context {
	return context.WithValue(ctx, usageModuleContextKey{}, strings.TrimSpace(module))
}

func UsageModule(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	module, _ := ctx.Value(usageModuleContextKey{}).(string)
	return strings.TrimSpace(module)
}

func usageFromFrame(frame rpcFrame) TokenUsage {
	if usage := usageFromValue(frame.Result); usage.TotalTokens > 0 {
		return usage
	}
	if usage := usageFromValue(frame.Params["usage"]); usage.TotalTokens > 0 {
		return usage
	}
	if usage := usageFromValue(frame.Params["token_usage"]); usage.TotalTokens > 0 {
		return usage
	}
	return usageFromValue(frame.Params)
}

func usageFromValue(value any) TokenUsage {
	object, ok := value.(map[string]any)
	if !ok {
		return TokenUsage{}
	}
	for _, key := range []string{"usage", "token_usage", "payload", "metadata", "stats", "response", "result", "data", "tokenUsage"} {
		if nested, exists := object[key]; exists {
			if usage := usageFromValue(nested); usage.TotalTokens > 0 {
				return usage
			}
		}
	}
	prompt := firstNumberValue(object, "prompt_tokens", "input_tokens", "input", "prompt")
	completion := firstNumberValue(object, "completion_tokens", "output_tokens", "output", "completion")
	total := firstNumberValue(object, "total_tokens", "total")
	if total == 0 {
		total = prompt + completion
	}
	if total <= 0 {
		return TokenUsage{}
	}
	model, _ := object["model"].(string)
	return TokenUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total, Model: strings.TrimSpace(model)}
}

func firstNumberValue(object map[string]any, keys ...string) int {
	for _, key := range keys {
		if value := numberValue(object[key]); value != 0 {
			return value
		}
	}
	return 0
}

func numberValue(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case float32:
		return int(number)
	case string:
		var parsed int
		_, _ = fmt.Sscanf(number, "%d", &parsed)
		return parsed
	default:
		return 0
	}
}
