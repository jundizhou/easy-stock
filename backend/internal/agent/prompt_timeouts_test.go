package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestManagedPromptTimeoutsKeepHermesCeilingOutsideHostDeadline(t *testing.T) {
	home := t.TempDir()
	original := []byte(`model:
  provider: easy-stock
  default: test-model
  api_mode: codex_responses
providers:
  easy-stock:
    stale_timeout_seconds: 300
    models:
      test-model:
        stale_timeout_seconds: 90
        timeout_seconds: 90
      other-model:
        stale_timeout_seconds: 90
`)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), original, 0600); err != nil {
		t.Fatal(err)
	}
	r := NewHermesRuntime(HermesConfig{Home: home})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	for _, modelOverrides := range []bool{false, true} {
		t.Run(strconv.FormatBool(modelOverrides), func(t *testing.T) {
			options := PromptOptions{Sandbox: true, FirstResponseTimeout: 300 * time.Second, IdleTimeout: 300 * time.Second, MaxAttempts: 2}
			process := promptProcessOptions{}
			if !modelOverrides {
				sandbox, err := r.preparePromptSandbox(options)
				if err != nil {
					t.Fatal(err)
				}
				defer sandbox.close()
				process = sandbox.process
			} else {
				privateHome := t.TempDir()
				privateConfig := filepath.Join(privateHome, "config.yaml")
				if err := os.WriteFile(privateConfig, original, 0600); err != nil {
					t.Fatal(err)
				}
				process.env = map[string]string{"HERMES_CONFIG": privateConfig, "HERMES_HOME": privateHome}
			}
			if err := configurePromptTimeouts(ctx, options, &process); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(process.env["HERMES_CONFIG"])
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := yaml.Unmarshal(data, &config); err != nil {
				t.Fatal(err)
			}
			providers, _ := stringMap(config["providers"])
			provider, _ := stringMap(providers[providerSlug])
			ceiling := intValue(provider["stale_timeout_seconds"])
			if ceiling < 489 || ceiling > 490 || intValue(provider["request_timeout_seconds"]) != ceiling {
				t.Fatalf("healthy Responses generation would still be cut off: %+v", provider)
			}
			agentConfig, _ := stringMap(config["agent"])
			if intValue(agentConfig["api_max_retries"]) != 2 {
				t.Fatal("Hermes retained its independent retry budget")
			}
			if modelOverrides {
				models, _ := stringMap(provider["models"])
				selected, _ := stringMap(models["test-model"])
				other, _ := stringMap(models["other-model"])
				if intValue(selected["stale_timeout_seconds"]) != ceiling || intValue(selected["timeout_seconds"]) != ceiling || intValue(other["stale_timeout_seconds"]) != 90 {
					t.Fatalf("model-specific ceiling overrides host budget: %+v", models)
				}
			}
			for _, key := range []string{staleTimeoutEnvName, "HERMES_API_TIMEOUT", "HERMES_STREAM_STALE_TIMEOUT", "HERMES_LOCAL_STREAM_STALE_TIMEOUT", "HERMES_CODEX_HARD_TIMEOUT_SECONDS"} {
				if process.env[key] != strconv.Itoa(ceiling) {
					t.Fatalf("ambient timeout can override host budget: %s=%s", key, process.env[key])
				}
			}
			if process.env["HERMES_CODEX_TTFB_TIMEOUT_SECONDS"] != "0" || process.env["HERMES_CODEX_EVENT_STALE_TIMEOUT_SECONDS"] != "0" {
				t.Fatal("Hermes can reconnect before the host activity watchdog")
			}
			unchanged, err := os.ReadFile(filepath.Join(home, "config.yaml"))
			if err != nil || string(unchanged) != string(original) {
				t.Fatal("per-call timeouts changed persistent model settings")
			}
		})
	}
}

func TestPromptTimeoutOverridesRequireHostActivityWatchdogAndDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, tc := range []struct {
		ctx     context.Context
		options PromptOptions
	}{
		{context.Background(), PromptOptions{Sandbox: true, FirstResponseTimeout: time.Second, IdleTimeout: time.Second}},
		{ctx, PromptOptions{Sandbox: true}},
		{ctx, PromptOptions{Sandbox: true, FirstResponseTimeout: time.Second}},
		{ctx, PromptOptions{FirstResponseTimeout: time.Second, IdleTimeout: time.Second}},
	} {
		process := promptProcessOptions{}
		err := configurePromptTimeouts(tc.ctx, tc.options, &process)
		if err != nil || len(process.env) != 0 {
			t.Fatalf("overrode an unmanaged prompt: %+v, %v", process, err)
		}
	}
}

// Exercise the installed Hermes policy, including its provider-ID lookup and
// its separate wall-clock/TTFB/idle watchdogs. This is entirely offline.
func TestManagedPromptTimeoutsAgainstBundledHermesResponsesWatchdog(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	config := "model:\n  provider: easy-stock\n  default: test-model\n  base_url: https://example.invalid/v1\n  api_mode: codex_responses\nproviders:\n  easy-stock:\n    stale_timeout_seconds: 300\n"
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewHermesRuntime(HermesConfig{Home: home, RuntimeRoot: filepath.Join("..", "..", "..", "desktop", "resources", "hermes-runtime")})
	if r.pythonPath == "" {
		t.Skip("bundled Hermes runtime is not installed")
	}
	if _, err := os.Stat(r.pythonPath); err != nil {
		t.Skip("bundled Hermes runtime is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	options := PromptOptions{Sandbox: true, FirstResponseTimeout: 300 * time.Second, IdleTimeout: 300 * time.Second}
	sandbox, err := r.preparePromptSandbox(options)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.close()
	process := sandbox.process
	if err := configurePromptTimeouts(ctx, options, &process); err != nil {
		t.Fatal(err)
	}
	script := `import json
from run_agent import AIAgent
from agent.chat_completion_helpers import _resolve_nonstream_watchdogs
agent = AIAgent.__new__(AIAgent)
agent.provider = "easy-stock"
agent.model = "test-model"
agent.base_url = agent._base_url = "https://example.invalid/v1"
agent.api_mode = "codex_responses"
wd = _resolve_nonstream_watchdogs(agent, {"model": agent.model, "input": [{"role": "user", "content": "test"}]})
print(json.dumps({"stale_timeout": wd.stale_timeout, "ttfb_enabled": wd.ttfb_enabled, "idle_enabled": wd.idle_enabled}))
`
	probeCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	cmd := exec.CommandContext(probeCtx, r.pythonPath, "-c", script)
	cmd.Env = setEnv(os.Environ(), "HERMES_HOME", home)
	cmd.Env = setEnv(cmd.Env, "HOME", home)
	for key, value := range process.env {
		cmd.Env = setEnv(cmd.Env, key, value)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bundled Hermes watchdog probe: %v\n%s", err, output)
	}
	var policy struct {
		StaleTimeout float64 `json:"stale_timeout"`
		TTFBEnabled  bool    `json:"ttfb_enabled"`
		IdleEnabled  bool    `json:"idle_enabled"`
	}
	if err := json.Unmarshal(output, &policy); err != nil {
		t.Fatalf("Hermes policy output: %v\n%s", err, output)
	}
	if policy.StaleTimeout < 480 || policy.TTFBEnabled || policy.IdleEnabled {
		t.Fatalf("Hermes can still interrupt an active host-managed request: %+v", policy)
	}
}
