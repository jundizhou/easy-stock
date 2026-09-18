package hermes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"easy-stock/backend/internal/appsettings"
)

type ReasoningOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type ReasoningCapability struct {
	Options []ReasoningOption `json:"options"`
	Default string            `json:"default"`
	Source  string            `json:"source"`
	Note    string            `json:"note"`
	// Wire is internal routing metadata, never inferred from an arbitrary model name.
	Wire           string `json:"wire,omitempty"`
	Profile        string `json:"profile,omitempty"`
	EffectiveModel string `json:"effective_model,omitempty"`
}

func reasoningCapability(source, wire, note, fallback string, values ...string) ReasoningCapability {
	labels := map[string]string{"default": "暂不支持调节", "none": "关闭思考", "enabled": "开启思考", "minimal": "极简", "low": "低", "medium": "中", "high": "高", "xhigh": "极高", "max": "最大"}
	c := ReasoningCapability{Default: fallback, Source: source, Wire: wire, Note: note, Options: []ReasoningOption{}}
	for _, v := range values {
		c.Options = append(c.Options, ReasoningOption{Value: v, Label: labels[v]})
	}
	switch wire {
	case "glm":
		c.Profile = "zai"
	case "openai_chat", "openai_responses":
		c.Profile = "custom"
	case "qwen_toggle":
		c.Profile = "alibaba"
	}
	return c
}

func (c ReasoningCapability) Allows(value string) bool {
	for _, option := range c.Options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func (c ReasoningCapability) Normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if c.Allows(value) {
		return value
	}
	return c.Default
}

// Official rules checked 2026-09-18. Exact model IDs and routes deliberately
// avoid guessing future model capabilities or a proxy's parameter semantics.
func OfficialReasoningCapability(cfg appsettings.LLM) ReasoningCapability {
	unknown := reasoningCapability("unknown", "", "Hermes 尚未声明当前模型在此接口上的可调档位，使用运行时默认设置。", "default", "default")
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return unknown
	}
	host := strings.ToLower(u.Hostname())
	model := strings.ToLower(strings.TrimSpace(cfg.Model))
	mode := cfg.APIMode
	if mode == "" {
		mode = "chat_completions"
	}
	if host == "open.bigmodel.cn" && mode == "chat_completions" && (strings.TrimRight(u.Path, "/") == "/api/paas/v4" || strings.TrimRight(u.Path, "/") == "/api/coding/paas/v4") {
		source := "https://docs.bigmodel.cn/cn/guide/capabilities/thinking"
		switch model {
		case "glm-5.3", "glm-5.3-flash":
			return reasoningCapability(source, "glm", "官方支持低、高、最大三档；不提供关闭选项。", "max", "low", "high", "max")
		case "glm-5.3-flashx":
			return reasoningCapability(source, "glm", "官方提供思考开关及低、高、最大三档。", "max", "none", "low", "high", "max")
		case "glm-5.2":
			return reasoningCapability(source, "glm", "仅展示独立效果：关闭、高、最大；其他兼容值会映射到这些档位。", "max", "none", "high", "max")
		case "glm-5.1", "glm-5", "glm-5-turbo", "glm-5v-turbo", "glm-4.7", "glm-4.6", "glm-4.5":
			return reasoningCapability(source, "glm", "官方提供思考开关，不提供独立强度档位。", "enabled", "none", "enabled")
		}
	}
	// DashScope is not Hermes' qwen-oauth provider. Its documented hybrid
	// models use a boolean, not a fabricated effort ladder.
	if (host == "dashscope.aliyuncs.com" || host == "dashscope-intl.aliyuncs.com") && strings.TrimRight(u.Path, "/") == "/compatible-mode/v1" && mode == "chat_completions" {
		switch model {
		case "qwen-plus", "qwen-plus-latest", "qwen-flash", "qwen-turbo", "qwen3-max", "qwen3-max-preview", "qwen3-max-2026-01-23", "qwen3-235b-a22b", "qwen3-32b", "qwen3-30b-a3b", "qwen3-14b", "qwen3-8b", "qwen3.8-max", "qwen3.8-max-0902", "qwen3.8-flash", "qwen3.8-27b":
			return reasoningCapability("https://help.aliyun.com/zh/model-studio/deep-thinking", "qwen_toggle", "百炼混合思考模型使用开关，不提供独立的强度档位。", "enabled", "none", "enabled")
		}
	}
	if host == "api.openai.com" && strings.TrimRight(u.Path, "/") == "/v1" && (mode == "chat_completions" || mode == "codex_responses") {
		wire := "openai_chat"
		if mode == "codex_responses" {
			wire = "openai_responses"
		}
		source := "https://developers.openai.com/api/docs/models/"
		switch model {
		case "gpt-5", "gpt-5-2025-08-07":
			return reasoningCapability(source+"gpt-5", wire, "按官方模型文档提供档位。", "medium", "minimal", "low", "medium", "high")
		case "gpt-5.1", "gpt-5.1-2025-11-13":
			return reasoningCapability(source+"gpt-5.1", wire, "按官方模型文档提供档位。", "none", "none", "low", "medium", "high")
		case "gpt-5.2", "gpt-5.2-2025-12-11":
			return reasoningCapability(source+"gpt-5.2", wire, "按官方模型文档提供档位。", "none", "none", "low", "medium", "high", "xhigh")
		case "gpt-5.5", "gpt-5.5-2026-04-23":
			return reasoningCapability(source+"gpt-5.5", wire, "按官方模型文档提供档位。", "medium", "none", "low", "medium", "high", "xhigh")
		}
	}
	return unknown
}

// Anthropic /v1/models exposes actual supported effort levels. A mere boolean
// such as OpenRouter's supported_parameters:["reasoning"] is NOT a level list.
func DiscoveredReasoningCapability(mode string, raw json.RawMessage) (ReasoningCapability, bool) {
	var caps struct {
		Effort struct {
			Supported                     bool `json:"supported"`
			Low, Medium, High, Xhigh, Max struct {
				Supported bool `json:"supported"`
			}
		} `json:"effort"`
		Thinking struct {
			Supported bool `json:"supported"`
			Types     struct {
				Adaptive struct {
					Supported bool `json:"supported"`
				} `json:"adaptive"`
			} `json:"types"`
		} `json:"thinking"`
	}
	if mode != "anthropic_messages" || json.Unmarshal(raw, &caps) != nil || !caps.Effort.Supported || !caps.Thinking.Supported || !caps.Thinking.Types.Adaptive.Supported {
		return ReasoningCapability{}, false
	}
	values := []string{}
	for i, yes := range []bool{caps.Effort.Low.Supported, caps.Effort.Medium.Supported, caps.Effort.High.Supported, caps.Effort.Xhigh.Supported, caps.Effort.Max.Supported} {
		if yes {
			values = append(values, []string{"low", "medium", "high", "xhigh", "max"}[i])
		}
	}
	if len(values) == 0 {
		return ReasoningCapability{}, false
	}
	// Do not invent the provider's default: select a supported level explicitly.
	fallback := values[0]
	for _, v := range values {
		if v == "high" {
			fallback = v
		}
	}
	return reasoningCapability("model_api", "anthropic", "档位来自模型接口；思考采用接口声明支持的自适应模式。", fallback, values...), true
}

type capabilityCacheEntry struct {
	BridgeVersion int                            `json:"bridge_version"`
	Updated       time.Time                      `json:"updated"`
	Models        map[string]ReasoningCapability `json:"models"`
}
type CapabilityGateway interface {
	ResolveModelCapabilities(baseURL, apiMode string, models map[string]json.RawMessage) (map[string]ReasoningCapability, error)
	SyncModelCapabilities(baseURL, apiMode string, models map[string]ReasoningCapability) error
}

func capabilityKey(baseURL, mode string) string { return strings.TrimRight(baseURL, "/") + "|" + mode }
func (r *Runtime) readCapabilityCache() map[string]capabilityCacheEntry {
	cache := map[string]capabilityCacheEntry{}
	data, err := os.ReadFile(filepath.Join(r.home, "model-capabilities.json"))
	if err == nil {
		_ = json.Unmarshal(data, &cache)
	}
	if cache == nil {
		cache = map[string]capabilityCacheEntry{}
	}
	return cache
}
func (r *Runtime) SyncModelCapabilities(baseURL, mode string, models map[string]ReasoningCapability) error {
	r.configMu.Lock()
	defer r.configMu.Unlock()
	cache := r.readCapabilityCache()
	cache[capabilityKey(baseURL, mode)] = capabilityCacheEntry{BridgeVersion: 2, Updated: time.Now(), Models: models}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	if err = writeSecureFile(filepath.Join(r.home, "model-capabilities.json"), data); err != nil {
		return err
	}
	config, err := r.readConfigMap()
	if err != nil {
		return err
	}
	cfg := reasoningLLM(config)
	if capabilityKey(cfg.BaseURL, cfg.APIMode) == capabilityKey(baseURL, mode) {
		r.applyReasoning(config, cfg, storedReasoningEffort(config))
		return r.writeConfigMap(config)
	}
	return nil
}
func (r *Runtime) reasoningCapability(cfg appsettings.LLM) ReasoningCapability {
	if entry, ok := r.readCapabilityCache()[capabilityKey(cfg.BaseURL, cfg.APIMode)]; ok {
		if c, ok := entry.Models[cfg.Model]; ok && entry.BridgeVersion == 2 && time.Since(entry.Updated) < 30*24*time.Hour {
			return c
		}
	}
	key := capabilityKey(cfg.BaseURL, cfg.APIMode) + "|" + cfg.Model
	r.reasoningMu.Lock()
	defer r.reasoningMu.Unlock()
	if c, ok := r.reasoningResolved[key]; ok {
		return c
	}
	caps, err := r.ResolveModelCapabilities(cfg.BaseURL, cfg.APIMode, map[string]json.RawMessage{cfg.Model: nil})
	c := OfficialReasoningCapability(cfg)
	if err == nil {
		if resolved, ok := caps[cfg.Model]; ok {
			c = resolved
		}
	}
	if r.reasoningResolved == nil {
		r.reasoningResolved = map[string]ReasoningCapability{}
	}
	r.reasoningResolved[key] = c
	return c
}
func reasoningLLM(config map[string]any) appsettings.LLM {
	model, _ := stringMap(config["model"])
	return appsettings.LLM{Model: stringValue(model["default"]), BaseURL: stringValue(model["base_url"]), APIMode: stringValue(model["api_mode"])}
}
func storedReasoningEffort(config map[string]any) string {
	agent, _ := stringMap(config["agent"])
	if v, ok := agent["easy_stock_reasoning_effort"].(string); ok {
		return v
	}
	return stringValue(agent["reasoning_effort"])
}
func (r *Runtime) applyReasoning(config map[string]any, cfg appsettings.LLM, value string) {
	c := r.reasoningCapability(cfg)
	effort := c.Normalize(value)
	agent, _ := stringMap(config["agent"])
	if agent == nil {
		agent = map[string]any{}
	}
	agent["easy_stock_reasoning_effort"] = effort
	// Keep Hermes' reasoning-aware history handling enabled when appropriate.
	// The managed provider delegates parameter conversion to Hermes.
	engineEffort := effort
	if effort == "enabled" {
		engineEffort = "medium"
	}
	if effort == "default" {
		engineEffort = ""
	}
	agent["reasoning_effort"] = engineEffort
	config["agent"] = agent
	config["easy_stock_reasoning"] = map[string]any{"config": map[string]any{"model": cfg.Model, "base_url": cfg.BaseURL, "api_mode": cfg.APIMode}, "capability": c}
	// Hermes omits reasoning when disabled on Responses. Explicit OpenAI none
	// must be sent to override that model's reasoning-enabled server default.
	providers, _ := stringMap(config["providers"])
	provider, _ := stringMap(providers[providerSlug])
	if provider != nil {
		extra, _ := stringMap(provider["extra_body"])
		if extra == nil {
			extra = map[string]any{}
		}
		delete(extra, "reasoning")
		if c.Wire == "openai_responses" && effort == "none" {
			extra["reasoning"] = map[string]any{"effort": "none"}
		}
		if len(extra) > 0 {
			provider["extra_body"] = extra
		} else {
			delete(provider, "extra_body")
		}
	}
}
func (r *Runtime) validateReasoning(config map[string]any, effort string) error {
	if !r.reasoningCapability(reasoningLLM(config)).Allows(effort) {
		return fmt.Errorf("当前模型或接口不支持思考选项 %q，请刷新模型能力后重试", effort)
	}
	return nil
}

var ErrReasoningContextChanged = errors.New("模型配置已变更，请刷新思考选项后重试")

func reasoningContext(config map[string]any) string {
	cfg := reasoningLLM(config)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.BaseURL+"\n"+cfg.Model+"\n"+cfg.APIMode)))
}

// ResolveModelCapabilities asks the bundled runtime once for an entire model
// list. No provider keys or network access are needed: metadata is supplied by
// the existing authenticated model discovery call.
func (r *Runtime) ResolveModelCapabilities(baseURL, mode string, models map[string]json.RawMessage) (map[string]ReasoningCapability, error) {
	type item struct {
		Model      string              `json:"model"`
		Metadata   json.RawMessage     `json:"metadata,omitempty"`
		Supplement ReasoningCapability `json:"supplement"`
	}
	request := struct {
		Config map[string]string `json:"config"`
		Models []item            `json:"models"`
	}{Config: map[string]string{"base_url": baseURL, "api_mode": mode}}
	fallback := map[string]ReasoningCapability{}
	for model, metadata := range models {
		supplement := OfficialReasoningCapability(appsettings.LLM{Model: model, BaseURL: baseURL, APIMode: mode})
		var raw struct {
			Capabilities json.RawMessage `json:"capabilities"`
		}
		_ = json.Unmarshal(metadata, &raw)
		if discovered, ok := DiscoveredReasoningCapability(mode, raw.Capabilities); ok {
			supplement = discovered
		}
		fallback[model] = supplement
		request.Models = append(request.Models, item{Model: model, Metadata: metadata, Supplement: supplement})
	}
	if r.pythonPath == "" {
		return fallback, nil
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.pythonPath, "-c", reasoningLauncher, "describe")
	cmd.Stdin = bytes.NewReader(data)
	cmd.Env = hermesEnvironment(os.Environ(), r.home, r.workDir, filepath.Dir(r.pythonPath))
	output, err := cmd.Output()
	if err != nil {
		return fallback, nil
	}
	var result map[string]ReasoningCapability
	if json.Unmarshal(output, &result) != nil {
		return fallback, nil
	}
	for model, c := range result {
		if len(c.Options) > 0 && c.Allows(c.Default) {
			fallback[model] = c
		}
	}
	return fallback, nil
}
