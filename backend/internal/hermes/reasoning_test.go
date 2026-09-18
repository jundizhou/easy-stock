package hermes

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"easy-stock/backend/internal/appsettings"
)

func optionValues(c ReasoningCapability) []string {
	values := []string{}
	for _, o := range c.Options {
		values = append(values, o.Value)
	}
	return values
}
func TestOfficialReasoningCapabilities(t *testing.T) {
	for _, tc := range []struct {
		model, url, mode string
		want             []string
	}{
		{"glm-5.3-flash", "https://open.bigmodel.cn/api/paas/v4", "chat_completions", []string{"low", "high", "max"}},
		{"GLM-5.3-FLASH", "https://open.bigmodel.cn/api/coding/paas/v4", "chat_completions", []string{"low", "high", "max"}},
		{"glm-5.2", "https://open.bigmodel.cn/api/paas/v4", "chat_completions", []string{"none", "high", "max"}},
		{"glm-4.7", "https://open.bigmodel.cn/api/paas/v4", "chat_completions", []string{"none", "enabled"}},
		{"glm-5.3-flash", "https://proxy.example/v1", "chat_completions", []string{"default"}},
		{"glm-5.3-flash", "https://open.bigmodel.cn/api/paas/v4", "anthropic_messages", []string{"default"}},
		{"glm-6", "https://open.bigmodel.cn/api/paas/v4", "chat_completions", []string{"default"}},
		{"gpt-5", "https://api.openai.com/v1", "codex_responses", []string{"minimal", "low", "medium", "high"}},
		{"gpt-5.1", "https://api.openai.com/v1", "chat_completions", []string{"none", "low", "medium", "high"}},
		{"gpt-5.5", "https://api.openai.com/v1", "codex_responses", []string{"none", "low", "medium", "high", "xhigh"}},
		{"gpt-5.5-pro", "https://api.openai.com/v1", "codex_responses", []string{"default"}},
		{"gpt-4o", "https://api.openai.com/v1", "chat_completions", []string{"default"}},
	} {
		t.Run(tc.model+tc.url+tc.mode, func(t *testing.T) {
			got := OfficialReasoningCapability(appsettings.LLM{Model: tc.model, BaseURL: tc.url, APIMode: tc.mode})
			if !reflect.DeepEqual(optionValues(got), tc.want) {
				t.Fatalf("got %v want %v", optionValues(got), tc.want)
			}
		})
	}
}

func TestReasoningMigrationValidationAndModelSwitch(t *testing.T) {
	r := NewRuntime(Config{Home: t.TempDir()})
	cfg := appsettings.LLM{Provider: "zhipu", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.3-flash", APIMode: "chat_completions"}
	// An old seven-level selection must not survive as an invalid wire value.
	if err := os.WriteFile(filepath.Join(r.home, "config.yaml"), []byte("agent:\n  reasoning_effort: xhigh\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.SyncLLM(cfg, nil); err != nil {
		t.Fatal(err)
	}
	settings, err := r.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ReasoningEffort != "max" {
		t.Fatalf("migration: %+v", settings)
	}
	for _, v := range []string{"none", "minimal", "medium", "xhigh"} {
		bad := settings
		bad.ReasoningEffort = v
		if err := r.SyncAgentSettings(bad); err == nil {
			t.Fatalf("accepted unsupported %s", v)
		}
	}
	settings.ReasoningEffort = "low"
	if err := r.SyncAgentSettings(settings); err != nil {
		t.Fatal(err)
	}
	saved, _ := r.readConfigMap()
	policy, _ := stringMap(saved["easy_stock_reasoning"])
	capability, ok := policy["capability"].(map[string]any)
	if !ok || capability["source"] != "https://docs.bigmodel.cn/cn/guide/capabilities/thinking" {
		t.Fatalf("missing native provider policy: %v", policy)
	}
	agent, _ := stringMap(saved["agent"])
	if agent["reasoning_effort"] != "low" {
		t.Fatalf("effort not passed to Hermes: %v", agent)
	}
	cfg.Model = "unknown-model"
	if err := r.SyncLLM(cfg, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.SyncAgentSettings(settings); !errors.Is(err, ErrReasoningContextChanged) {
		t.Fatalf("stale update: %v", err)
	}
	current, _ := r.AgentSettings()
	if current.ReasoningEffort != "default" {
		t.Fatalf("unknown: %+v", current)
	}
	saved, _ = r.readConfigMap()
	policy, _ = stringMap(saved["easy_stock_reasoning"])
	if _, ok := policy["body"]; ok {
		t.Fatal("SDK request override policy survived")
	}
	agent, _ = stringMap(saved["agent"])
	if agent["reasoning_effort"] != "" {
		t.Fatalf("unknown retained effort: %v", agent)
	}
}

const anthropicCapabilityFixture = `{"effort":{"supported":true,"low":{"supported":true},"medium":{"supported":true},"high":{"supported":true},"max":{"supported":false}},"thinking":{"supported":true,"types":{"adaptive":{"supported":true}}}}`

func TestDiscoveredCapabilitiesPersistAndStayRouteScoped(t *testing.T) {
	c, ok := DiscoveredReasoningCapability("anthropic_messages", json.RawMessage(anthropicCapabilityFixture))
	if !ok || !reflect.DeepEqual(optionValues(c), []string{"low", "medium", "high"}) {
		t.Fatalf("bad discovery: %+v", c)
	}
	if _, ok := DiscoveredReasoningCapability("chat_completions", json.RawMessage(anthropicCapabilityFixture)); ok {
		t.Fatal("applied Anthropic schema to Chat Completions")
	}
	for _, raw := range []string{`{}`, `{"reasoning":true}`, `{"effort":{"supported":true}}`, `{"effort":{"supported":false}}`} {
		if _, ok := DiscoveredReasoningCapability("anthropic_messages", json.RawMessage(raw)); ok {
			t.Fatalf("invented effort levels from %s", raw)
		}
	}
	home := t.TempDir()
	r := NewRuntime(Config{Home: home})
	cfg := appsettings.LLM{Provider: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-from-api", APIMode: "anthropic_messages"}
	if err := r.SyncLLM(cfg, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.SyncModelCapabilities(cfg.BaseURL, cfg.APIMode, map[string]ReasoningCapability{cfg.Model: c}); err != nil {
		t.Fatal(err)
	}
	restarted := NewRuntime(Config{Home: home})
	s, err := restarted.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Reasoning.Source != "model_api" || s.ReasoningEffort != "high" {
		t.Fatalf("cache not restored: %+v", s)
	}
	cfg.BaseURL = "https://other.example"
	if got := r.reasoningCapability(cfg); got.Source != "unknown" {
		t.Fatal("metadata leaked to another endpoint")
	}
	if err := r.SyncModelCapabilities("https://api.anthropic.com", cfg.APIMode, map[string]ReasoningCapability{}); err != nil {
		t.Fatal(err)
	}
	s, _ = r.AgentSettings()
	if s.Reasoning.Source != "unknown" {
		t.Fatal("removed metadata was retained")
	}
}

// Opt in with the bundled Python to verify the Go -> Hermes catalog boundary.
func TestBundledReasoningBridge(t *testing.T) {
	python := os.Getenv("HERMES_TEST_PYTHON")
	if python == "" {
		t.Skip("set HERMES_TEST_PYTHON to the bundled runtime Python")
	}
	r := NewRuntime(Config{Home: t.TempDir(), PythonPath: python})
	caps, err := r.ResolveModelCapabilities("https://api.deepseek.com/v1", "chat_completions", map[string]json.RawMessage{"deepseek-chat": nil})
	if err != nil {
		t.Fatal(err)
	}
	c := caps["deepseek-chat"]
	if c.Source != "hermes" || c.Profile != "deepseek" || !c.Allows("max") || c.Allows("xhigh") {
		t.Fatalf("native bridge: %+v", c)
	}
	caps, err = r.ResolveModelCapabilities("https://open.bigmodel.cn/api/paas/v4", "chat_completions", map[string]json.RawMessage{"glm-5.3-flash": nil})
	if err != nil {
		t.Fatal(err)
	}
	if c = caps["glm-5.3-flash"]; c.Profile != "zai" || !reflect.DeepEqual(optionValues(c), []string{"low", "high", "max"}) {
		t.Fatalf("documented supplement: %+v", c)
	}
}

func TestResponsesDisableSurvivesIsolatedConfig(t *testing.T) {
	r := NewRuntime(Config{Home: t.TempDir()})
	cfg := appsettings.LLM{Provider: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-5.5", APIMode: "codex_responses"}
	if err := r.SyncLLM(cfg, nil); err != nil {
		t.Fatal(err)
	}
	settings, err := r.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.ReasoningEffort = "none"
	if err := r.SyncAgentSettings(settings); err != nil {
		t.Fatal(err)
	}
	config, err := r.readConfigMap()
	if err != nil {
		t.Fatal(err)
	}
	isolated := minimalSandboxConfig(config, t.TempDir())
	providers, _ := stringMap(isolated["providers"])
	provider, _ := stringMap(providers[providerSlug])
	extra, _ := stringMap(provider["extra_body"])
	reasoning, _ := stringMap(extra["reasoning"])
	if reasoning["effort"] != "none" {
		t.Fatalf("isolated disable lost: %v", provider)
	}
	settings.ReasoningEffort = "high"
	if err := r.SyncAgentSettings(settings); err != nil {
		t.Fatal(err)
	}
	config, _ = r.readConfigMap()
	providers, _ = stringMap(config["providers"])
	provider, _ = stringMap(providers[providerSlug])
	if _, exists := provider["extra_body"]; exists {
		t.Fatalf("old disable retained: %v", provider)
	}
}
