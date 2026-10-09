package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestStructuredEffortCapRespectsCapabilityAndUserChoice(t *testing.T) {
	c := reasoningCapability("test", "openai_responses", "", "max", "low", "high", "max")
	for _, tc := range []struct{ current, want string }{{"max", "high"}, {"high", "high"}, {"low", "low"}, {"", "high"}} {
		if got := cappedReasoningEffort(tc.current, "high", c); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	for _, c := range []ReasoningCapability{reasoningCapability("unknown", "", "", "default", "default"), reasoningCapability("toggle", "qwen_toggle", "", "enabled", "none", "enabled"), reasoningCapability("strict", "openai_responses", "", "max", "max")} {
		if got := cappedReasoningEffort(c.Default, "high", c); got != c.Default {
			t.Fatal("invented unsupported setting", got, c)
		}
	}
}

func TestStructuredEffortCapOnlyChangesSandboxProjection(t *testing.T) {
	home := t.TempDir()
	base := []byte("model:\n  default: test-model\n  base_url: https://example.test/v1\n  api_mode: responses\nagent:\n  reasoning_effort: max\n  easy_stock_reasoning_effort: max\n")
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), base, 0600); err != nil {
		t.Fatal(err)
	}
	r := NewHermesRuntime(HermesConfig{Home: home})
	cfg := reasoningLLM(map[string]any{"model": map[string]any{"default": "test-model", "base_url": "https://example.test/v1", "api_mode": "responses"}})
	cache, _ := json.Marshal(map[string]capabilityCacheEntry{capabilityKey(cfg.BaseURL, cfg.APIMode): {BridgeVersion: reasoningCapabilityVersion, Updated: time.Now(), Models: map[string]ReasoningCapability{cfg.Model: reasoningCapability("test", "openai_responses", "", "max", "low", "high", "max")}}})
	if err := os.WriteFile(filepath.Join(home, "model-capabilities.json"), cache, 0600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := r.preparePromptSandbox(PromptOptions{Sandbox: true, DisableTools: true, ReasoningEffortCap: "high"})
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.close()
	data, err := os.ReadFile(sandbox.process.env["HERMES_CONFIG"])
	if err != nil {
		t.Fatal(err)
	}
	var projection map[string]any
	if err := yaml.Unmarshal(data, &projection); err != nil {
		t.Fatal(err)
	}
	if storedReasoningEffort(projection) != "high" {
		t.Fatal("cap missing from effective sandbox", storedReasoningEffort(projection))
	}
	unchanged, _ := os.ReadFile(filepath.Join(home, "config.yaml"))
	if string(unchanged) != string(base) {
		t.Fatal("shared user configuration changed")
	}
}
