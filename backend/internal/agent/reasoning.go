package agent

import (
	"bytes"
	"strconv"
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
	labels := map[string]string{"default": "模型默认", "none": "关闭思考", "enabled": "开启思考", "minimal": "极简", "low": "低", "medium": "中", "high": "高", "xhigh": "极高", "max": "最大"}
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

// [ES_OLLAMA_LOCAL_REASONING_PATCH] 判定是否为本机 / 私网推理端点。
// 仅覆盖 loopback、RFC1918 私网、IPv6 loopback 与云元数据之外的常见本地写法；
// 公网域名/公网 IP 不受影响，避免误关远程模型的思考。
func isLocalReasoningHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]" || host == "0.0.0.0" || host == "host.docker.internal" {
		return true
	}
	if strings.HasPrefix(host, "127.") {
		return true
	}
	if strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") {
		return true
	}
	if strings.HasPrefix(host, "172.") {
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			if second, err := strconv.Atoi(parts[1]); err == nil && second >= 16 && second <= 31 {
				return true
			}
		}
	}
	return false
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

// Official rules checked 2026-09-30. Exact model IDs and routes deliberately
// avoid guessing future model capabilities or a proxy's parameter semantics.
func OfficialReasoningCapability(cfg appsettings.LLM) ReasoningCapability {
	unknown := reasoningCapability("unknown", "", "尚未确认当前模型在此接口上的可调档位，使用运行时默认设置。", "default", "default")
	unknown.Options[0].Label = "能力未确认"
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return unknown
	}
	host := strings.ToLower(u.Hostname())
	// [ES_OLLAMA_LOCAL_REASONING_PATCH] 本机 / 私网 Ollama 等 OpenAI 兼容端点：
	// 官方域名白名单不覆盖，但它确实支持关闭思考（reasoning.enabled=false 或
	// reasoning_effort=none）。若不在此显式声明 none 能力，则：
	//   ① validateReasoning 拒绝 effort="none"；② Normalize 把 none 降级为 default；
	//   ③ applyReasoning 的 none 注入分支永远走不到 → thinking 吃光 max_tokens →
	//      AI 研究「核心判断」拿到空 content 而失败。
	var localMode = cfg.APIMode
	if localMode == "" {
		localMode = "chat_completions"
	}
	if localMode == "chat_completions" && isLocalReasoningHost(host) {
		c := reasoningCapability("local_ollama", "openai_chat",
			"本机 OpenAI 兼容端点（Ollama / LM Studio / vLLM 等）：默认模型自带思考，可在此显式关闭。",
			"none", "none", "default")
		c.EffectiveModel = cfg.Model
		return c
	}
	model := strings.ToLower(strings.TrimSpace(cfg.Model))
	mode := cfg.APIMode
	if mode == "" {
		mode = "chat_completions"
	}
	if (host == "dashscope.aliyuncs.com" || host == "dashscope-intl.aliyuncs.com") && SupportsResponses(cfg) {
		unknown.Note = "百炼原生 Responses 使用控制台提供的工作空间地址（*.maas.aliyuncs.com/compatible-mode/v1）；请核对当前地址与模型，或刷新模型目录确认档位。"
	}
	if c, ok := providerReasoningCapability(host, strings.TrimRight(u.Path, "/"), model, mode); ok {
		return c
	}
	// Verified against Z.ai's native Responses catalog and Codex setup guide.
	// This route accepts reasoning.effort directly; never use the Chat wire adapter.
	if host == "open.bigmodel.cn" && strings.TrimRight(u.Path, "/") == "/api/v1" && SupportsResponses(cfg) {
		switch model {
		case "glm-5.3", "glm-5.3-flash":
			return reasoningCapability("https://docs.bigmodel.cn/cn/coding-plan/tool/codex", "openai_responses", "Responses 接口支持低、高、最大三档，默认最大。", "max", "low", "high", "max")
		}
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
	// Chat Completions uses a thinking budget rather than reasoning_effort.
	if (host == "dashscope.aliyuncs.com" || host == "dashscope-intl.aliyuncs.com" || strings.HasSuffix(host, ".maas.aliyuncs.com")) && strings.TrimRight(u.Path, "/") == "/compatible-mode/v1" && mode == "chat_completions" {
		switch model {
		case "qwen-plus", "qwen-plus-latest", "qwen-flash", "qwen-turbo", "qwen3-max", "qwen3-max-preview", "qwen3-max-2026-01-23", "qwen3-235b-a22b", "qwen3-32b", "qwen3-30b-a3b", "qwen3-14b", "qwen3-8b", "qwen3.8-max", "qwen3.8-max-0902", "qwen3.8-flash", "qwen3.8-27b":
			return budgetReasoningCapability("https://help.aliyun.com/zh/model-studio/deep-thinking", "qwen_budget")
		}
	}
	if host == "api.openai.com" && strings.TrimRight(u.Path, "/") == "/v1" && (mode == "chat_completions" || SupportsResponses(cfg)) {
		wire := "openai_chat"
		if SupportsResponses(cfg) {
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

// Model catalogs carry Responses effort levels at the top level, whereas
// Anthropic exposes them in capabilities. Keep discovery scoped to the wire.
func DiscoveredModelReasoningCapability(mode string, raw json.RawMessage) (ReasoningCapability, bool) {
	var model struct {
		Capabilities  json.RawMessage `json:"capabilities"`
		Default       string          `json:"default_reasoning_level"`
		DefaultEffort string          `json:"default_reasoning_effort"`
		Efforts       *[]string       `json:"supported_reasoning_efforts"`
		Levels        *[]struct {
			Effort string `json:"effort"`
		} `json:"supported_reasoning_levels"`
	}
	if json.Unmarshal(raw, &model) != nil {
		return ReasoningCapability{}, false
	}
	if mode == "anthropic_messages" {
		return DiscoveredReasoningCapability(mode, model.Capabilities)
	}
	responses := SupportsResponses(appsettings.LLM{APIMode: mode})
	if !responses && mode != "chat_completions" {
		return ReasoningCapability{}, false
	}
	// Explicit effort catalogs work for arbitrary native OpenAI-compatible
	// endpoints. A reasoning=true flag alone never supplies a level list.
	if model.Efforts != nil {
		levels := []struct {
			Effort string `json:"effort"`
		}{}
		for _, effort := range *model.Efforts {
			levels = append(levels, struct {
				Effort string `json:"effort"`
			}{effort})
		}
		model.Levels = &levels
		if model.Default == "" {
			model.Default = model.DefaultEffort
		}
	} else if !responses {
		return ReasoningCapability{}, false
	}
	wire := "openai_chat"
	if responses {
		wire = "openai_responses"
	}
	if model.Levels == nil {
		return ReasoningCapability{}, false
	}
	if len(*model.Levels) == 0 {
		return reasoningCapability("model_api", wire, "模型接口未提供可调思考档位，使用默认设置。", "default", "default"), true
	}
	values := []string{}
	for _, value := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		for _, level := range *model.Levels {
			if level.Effort == value {
				values = append(values, value)
				break
			}
		}
	}
	if len(values) == 0 {
		return ReasoningCapability{}, false
	}
	c := reasoningCapability("model_api", wire, "思考档位来自当前接口的模型目录。", model.Default, values...)
	if !c.Allows(c.Default) {
		c.Default = values[0]
	}
	return c, true
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

const reasoningCapabilityVersion = 5

type CapabilityGateway interface {
	ResolveModelCapabilities(baseURL, apiMode string, models map[string]json.RawMessage) (map[string]ReasoningCapability, error)
	SyncModelCapabilities(baseURL, apiMode string, models map[string]ReasoningCapability) error
}

func capabilityKey(baseURL, mode string) string {
	if mode == "responses" {
		mode = "codex_responses"
	}
	return strings.TrimRight(baseURL, "/") + "|" + mode
}
func (r *HermesRuntime) readCapabilityCache() map[string]capabilityCacheEntry {
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
func (r *HermesRuntime) SyncModelCapabilities(baseURL, mode string, models map[string]ReasoningCapability) error {
	r.configMu.Lock()
	defer r.configMu.Unlock()
	cache := r.readCapabilityCache()
	cache[capabilityKey(baseURL, mode)] = capabilityCacheEntry{BridgeVersion: reasoningCapabilityVersion, Updated: time.Now(), Models: models}
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
func (r *HermesRuntime) reasoningCapability(cfg appsettings.LLM) ReasoningCapability {
	if entry, ok := r.readCapabilityCache()[capabilityKey(cfg.BaseURL, cfg.APIMode)]; ok {
		if c, ok := entry.Models[cfg.Model]; ok && entry.BridgeVersion == reasoningCapabilityVersion && time.Since(entry.Updated) < 30*24*time.Hour {
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
func (r *HermesRuntime) applyReasoning(config map[string]any, cfg appsettings.LLM, value string) {
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
		// Responses: OpenAI 规范；Chat Completions: Ollama / 兼容端点需要显式关闭 thinking，
		// 否则 thinking 会吃光 max_tokens 导致 content 为空（AI 研究「核心判断」失败）。
		// [ES_OLLAMA_LOCAL_REASONING_PATCH] openai_chat 覆盖本机 OpenAI 兼容端点；
		// 这类端点（Ollama 0.35+ 实测）认 reasoning.enabled=false 与 reasoning_effort=none。
		if effort == "none" && (c.Wire == "openai_responses" || c.Wire == "chat_completions" || c.Wire == "openai_chat") {
			if c.Wire == "openai_responses" {
				extra["reasoning"] = map[string]any{"effort": "none"}
			} else {
				extra["reasoning"] = map[string]any{"enabled": false, "effort": "none"}
			}
		}
		if len(extra) > 0 {
			provider["extra_body"] = extra
		} else {
			delete(provider, "extra_body")
		}
	}
}
func (r *HermesRuntime) validateReasoning(config map[string]any, effort string) error {
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
func (r *HermesRuntime) ResolveModelCapabilities(baseURL, mode string, models map[string]json.RawMessage) (map[string]ReasoningCapability, error) {
	type item struct {
		Model      string              `json:"model"`
		Metadata   json.RawMessage     `json:"metadata,omitempty"`
		Supplement ReasoningCapability `json:"supplement"`
	}
	request := struct {
		HermesConfig map[string]string `json:"config"`
		Models       []item            `json:"models"`
	}{HermesConfig: map[string]string{"base_url": baseURL, "api_mode": mode}}
	fallback := map[string]ReasoningCapability{}
	for model, metadata := range models {
		supplement := OfficialReasoningCapability(appsettings.LLM{Model: model, BaseURL: baseURL, APIMode: mode})
		if discovered, ok := DiscoveredModelReasoningCapability(mode, metadata); ok {
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
