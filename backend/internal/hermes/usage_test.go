package hermes

import "testing"

func TestUsageFromFrameReadsGatewayUsageSnapshot(t *testing.T) {
	frame := rpcFrame{
		Method: "event",
		Params: map[string]any{
			"type": "message.complete",
			"payload": map[string]any{
				"text": "done",
				"usage": map[string]any{
					"model":     "kimi-k3",
					"input":     11,
					"output":    7,
					"total":     18,
					"reasoning": 4,
					"calls":     1,
				},
			},
		},
	}

	usage := usageFromFrame(frame)
	if usage.PromptTokens != 11 || usage.CompletionTokens != 7 || usage.TotalTokens != 18 || usage.Model != "kimi-k3" {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestUsageFromFrameKeepsProviderTokenNames(t *testing.T) {
	frame := rpcFrame{
		Result: map[string]any{
			"usage": map[string]any{
				"input_tokens":  13,
				"output_tokens": 5,
				"model":         "claude-sonnet-4",
			},
		},
	}

	usage := usageFromFrame(frame)
	if usage.PromptTokens != 13 || usage.CompletionTokens != 5 || usage.TotalTokens != 18 || usage.Model != "claude-sonnet-4" {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}
