package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// The host watchdog owns first-response, idle and total deadlines for bounded
// sandbox prompts. Hermes' Responses adapter also applies stale_timeout_seconds to
// elapsed wall time, even while reasoning/text is streaming. Keep that inner
// ceiling outside the host deadline, in a private per-call configuration.
func configurePromptTimeouts(ctx context.Context, options PromptOptions, process *promptProcessOptions) error {
	deadline, bounded := ctx.Deadline()
	if !options.Sandbox || !bounded || options.FirstResponseTimeout <= 0 || options.IdleTimeout <= 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	configPath := process.env["HERMES_CONFIG"]
	if configPath == "" || configPath != filepath.Join(process.env["HERMES_HOME"], "config.yaml") {
		return fmt.Errorf("模型等待配置必须位于本次沙箱目录")
	}
	var config map[string]any
	data, err := os.ReadFile(configPath)
	if err == nil {
		err = yaml.Unmarshal(data, &config)
	}
	if err != nil {
		return fmt.Errorf("准备模型等待配置: %w", err)
	}
	if config == nil {
		config = map[string]any{}
	}
	// Round up, with teardown margin, so the Go context always expires first.
	seconds := int((time.Until(deadline)+time.Second-1)/time.Second) + 10
	model, _ := stringMap(config["model"])
	providerID := firstNonEmpty(stringValue(model["provider"]), providerSlug)
	providers, ok := stringMap(config["providers"])
	if !ok {
		providers = map[string]any{}
	}
	provider, ok := stringMap(providers[providerID])
	if !ok {
		provider = map[string]any{}
	}
	provider["stale_timeout_seconds"] = seconds
	provider["request_timeout_seconds"] = seconds
	// A model-specific setting takes precedence over the provider-wide one.
	if models, ok := stringMap(provider["models"]); ok {
		if selected, ok := stringMap(models[stringValue(model["default"])]); ok {
			selected["stale_timeout_seconds"] = seconds
			selected["timeout_seconds"] = seconds
		}
	}
	providers[providerID] = provider
	config["providers"] = providers
	if options.MaxAttempts > 0 {
		agentConfig, ok := stringMap(config["agent"])
		if !ok {
			agentConfig = map[string]any{}
		}
		agentConfig["api_max_retries"] = options.MaxAttempts
		config["agent"] = agentConfig
	}
	data, err = yaml.Marshal(config)
	if err == nil {
		err = writeSecureFile(configPath, data)
	}
	if err != nil {
		return fmt.Errorf("写入模型等待配置: %w", err)
	}
	process.env[staleTimeoutEnvName] = strconv.Itoa(seconds)
	process.env["HERMES_API_TIMEOUT"] = strconv.Itoa(seconds)
	process.env["HERMES_STREAM_STALE_TIMEOUT"] = strconv.Itoa(seconds)
	process.env["HERMES_LOCAL_STREAM_STALE_TIMEOUT"] = strconv.Itoa(seconds)
	process.env["HERMES_CODEX_HARD_TIMEOUT_SECONDS"] = strconv.Itoa(seconds)
	// Go observes meaningful reasoning/text rather than transport heartbeats;
	// avoid a second watchdog reconnecting earlier than the configured wait.
	process.env["HERMES_CODEX_TTFB_TIMEOUT_SECONDS"] = "0"
	process.env["HERMES_CODEX_EVENT_STALE_TIMEOUT_SECONDS"] = "0"
	return nil
}
