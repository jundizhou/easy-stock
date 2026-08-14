package hermes

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"easy-stock/backend/internal/appsettings"
	"gopkg.in/yaml.v3"
)

func TestRuntimeSyncLLMWritesHermesConfigAndKeepsSecretInEnv(t *testing.T) {
	root := t.TempDir()
	python := filepath.Join(root, "python")
	if err := os.WriteFile(python, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "hermes-home")
	runtime := NewRuntime(Config{RuntimeRoot: root, Home: home, WorkDir: root, PythonPath: python})
	key := "sk-hermes-private"
	if err := runtime.SyncLLM(appsettings.LLM{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", APIMode: "chat_completions"}, &key); err != nil {
		t.Fatal(err)
	}

	configData, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	configText := string(configData)
	var config map[string]any
	if err := yaml.Unmarshal(configData, &config); err != nil {
		t.Fatalf("invalid Hermes config: %v\n%s", err, configText)
	}
	model, _ := stringMap(config["model"])
	providers, _ := stringMap(config["providers"])
	provider, _ := stringMap(providers[providerSlug])
	if stringValue(model["provider"]) != providerSlug || stringValue(model["default"]) != "deepseek-chat" || stringValue(provider["transport"]) != "chat_completions" || intValue(provider["stale_timeout_seconds"]) != appsettings.DefaultLLMResponseTimeoutSeconds {
		t.Fatalf("unexpected Hermes config:\n%s", configText)
	}
	if timeout, err := readEnvValue(filepath.Join(home, ".env"), staleTimeoutEnvName); err != nil || timeout != strconv.Itoa(appsettings.DefaultLLMResponseTimeoutSeconds) {
		t.Fatalf("initial stale timeout env = %q, %v; want %d", timeout, err, appsettings.DefaultLLMResponseTimeoutSeconds)
	}
	if err := runtime.SyncLLM(appsettings.LLM{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", APIMode: "chat_completions", ResponseTimeoutSeconds: 600}, nil); err != nil {
		t.Fatal(err)
	}
	configData, err = os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	config = map[string]any{}
	if err := yaml.Unmarshal(configData, &config); err != nil {
		t.Fatalf("invalid Hermes config after timeout update: %v", err)
	}
	providers, _ = stringMap(config["providers"])
	provider, _ = stringMap(providers[providerSlug])
	if intValue(provider["stale_timeout_seconds"]) != 600 {
		t.Fatalf("updated stale timeout=%v, want 600", provider["stale_timeout_seconds"])
	}
	if strings.Contains(configText, key) {
		t.Fatal("Hermes config.yaml leaked model API key")
	}
	if storedKey, err := runtime.ModelAPIKey(); err != nil || storedKey != key {
		t.Fatalf("ModelAPIKey() = %q, %v; want saved key", storedKey, err)
	}
	if timeout, err := readEnvValue(filepath.Join(home, ".env"), staleTimeoutEnvName); err != nil || timeout != "600" {
		t.Fatalf("updated stale timeout env = %q, %v; want 600", timeout, err)
	}
	envData, err := os.ReadFile(filepath.Join(home, ".env"))
	if err != nil || !strings.Contains(string(envData), key) {
		t.Fatalf("Hermes .env missing key: err=%v data=%s", err, envData)
	}
	status := runtime.Status()
	if !status.Available || !status.Configured || !status.APIKeyConfigured {
		t.Fatalf("unexpected runtime status: %+v", status)
	}
}

func TestRuntimeAgentSettingsPreservesSecretsAndModelConfig(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	skillDir := filepath.Join(home, "skills", "trading", "test-skill")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: test description\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(Config{Home: home, WorkDir: root, PythonPath: filepath.Join(root, "python")})
	key := "model-secret"
	if err := runtime.SyncLLM(appsettings.LLM{Provider: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-test"}, &key); err != nil {
		t.Fatal(err)
	}
	config, err := runtime.readConfigMap()
	if err != nil {
		t.Fatal(err)
	}
	skills, _ := stringMap(config["skills"])
	skills["disabled"] = []string{"missing-local-skill"}
	config["skills"] = skills
	config["mcp_servers"] = map[string]any{"github": map[string]any{
		"enabled": true, "command": "npx", "args": []string{"-y", "server"},
		"env": map[string]string{"TOKEN": "mcp-secret"}, "keepalive_interval": 15,
	}}
	if err := runtime.writeConfigMap(config); err != nil {
		t.Fatal(err)
	}
	view, err := runtime.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	if view.ReasoningEffort != "medium" || len(view.Skills) != 1 || !view.Skills[0].Enabled || len(view.MCPServers) != 1 || view.MCPServers[0].Env["TOKEN"] != "mcp-secret" {
		t.Fatalf("unexpected agent settings: %+v", view)
	}
	view.ReasoningEffort = "xhigh"
	view.Skills[0].Enabled = false
	view.MCPServers[0].SupportsParallelToolCall = true
	if err := runtime.SyncAgentSettings(view); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "mcp_servers:") || !strings.Contains(text, "disabled:") || !strings.Contains(text, "gpt-test") {
		t.Fatalf("merged config missing managed sections:\n%s", text)
	}
	merged, _ := runtime.readConfigMap()
	mergedSkills, _ := stringMap(merged["skills"])
	servers, _ := stringMap(merged["mcp_servers"])
	github, _ := stringMap(servers["github"])
	if !slices.Contains(stringSlice(mergedSkills["disabled"]), "missing-local-skill") || intValue(github["keepalive_interval"]) != 15 {
		t.Fatalf("unmanaged Hermes settings were overwritten:\n%s", text)
	}
	if err := runtime.SyncLLM(appsettings.LLM{Provider: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-test-2"}, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := runtime.AgentSettings()
	if after.ReasoningEffort != "xhigh" || len(after.MCPServers) != 1 || after.MCPServers[0].Env["TOKEN"] != "mcp-secret" || after.Skills[0].Enabled {
		t.Fatalf("model save overwrote agent settings: %+v", after)
	}
}

func TestRuntimeSyncAgentSettingsRejectsInvalidReasoningEffort(t *testing.T) {
	runtime := NewRuntime(Config{Home: t.TempDir()})
	err := runtime.SyncAgentSettings(AgentSettings{ReasoningEffort: "turbo"})
	if err == nil || !strings.Contains(err.Error(), "思考等级") {
		t.Fatalf("SyncAgentSettings() error = %v, want invalid reasoning effort", err)
	}
}

func TestRuntimeSyncLLMRetainsAndClearsExistingKey(t *testing.T) {
	root := t.TempDir()
	python := filepath.Join(root, "python")
	_ = os.WriteFile(python, []byte("test"), 0o700)
	runtime := NewRuntime(Config{Home: filepath.Join(root, "home"), PythonPath: python})
	key := "first-key"
	cfg := appsettings.LLM{Provider: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-test"}
	if err := runtime.SyncLLM(cfg, &key); err != nil {
		t.Fatal(err)
	}
	cfg.Model = "gpt-test-2"
	if err := runtime.SyncLLM(cfg, nil); err != nil {
		t.Fatal(err)
	}
	if value, _ := readEnvValue(filepath.Join(root, "home", ".env"), modelAPIKeyEnvName); value != key {
		t.Fatalf("retained key = %q, want %q", value, key)
	}
	clear := ""
	if err := runtime.SyncLLM(cfg, &clear); err != nil {
		t.Fatal(err)
	}
	if runtime.Status().Configured || runtime.Status().APIKeyConfigured {
		t.Fatalf("runtime should be unconfigured after key clear: %+v", runtime.Status())
	}
}

func TestRuntimeProcessEnvironmentUsesHermesEnvInsteadOfAmbientKey(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeEnvValue(filepath.Join(home, ".env"), modelAPIKeyEnvName, "hermes-file-key"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(modelAPIKeyEnvName, "ambient-shell-key")
	runtime := NewRuntime(Config{Home: home, WorkDir: root, PythonPath: filepath.Join(root, "python")})

	values, err := runtime.processEnvironment("")
	if err != nil {
		t.Fatal(err)
	}
	if got := envValue(values, modelAPIKeyEnvName); got != "hermes-file-key" {
		t.Fatalf("process key = %q, want Hermes .env value", got)
	}

	if err := writeEnvValue(filepath.Join(home, ".env"), modelAPIKeyEnvName, ""); err != nil {
		t.Fatal(err)
	}
	values, err = runtime.processEnvironment("")
	if err != nil {
		t.Fatal(err)
	}
	if got := envValue(values, modelAPIKeyEnvName); got != "" {
		t.Fatalf("cleared process key = %q, want empty", got)
	}
}

func TestRuntimeProcessEnvironmentUsesBrowserStateWithoutConflictingProfile(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeEnvValue(filepath.Join(home, ".env"), modelAPIKeyEnvName, "test-key"); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, "browser-auth", "xueqiu", "profile.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(`{"cookies":[],"origins":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(Config{Home: home, WorkDir: root, PythonPath: filepath.Join(root, "python")})

	values, err := runtime.processEnvironment(statePath)
	if err != nil {
		t.Fatal(err)
	}
	absolutePath, _ := filepath.Abs(statePath)
	if got := envValue(values, "AGENT_BROWSER_STATE"); got != absolutePath {
		t.Fatalf("AGENT_BROWSER_STATE = %q, want %q", got, absolutePath)
	}
	for _, value := range values {
		if strings.HasPrefix(value, "AGENT_BROWSER_PROFILE=") {
			t.Fatalf("browser state environment must not include AGENT_BROWSER_PROFILE: %q", value)
		}
	}
}

func TestHermesEnvironmentIncludesWorkspaceNodeBin(t *testing.T) {
	workDir := t.TempDir()
	wrapperDir := filepath.Join(workDir, "browser-wrapper")
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	workspaceBin := filepath.Join(workDir, "node_modules", ".bin")
	if err := os.MkdirAll(workspaceBin, 0o755); err != nil {
		t.Fatal(err)
	}
	values := hermesEnvironment([]string{"PATH=/usr/bin", "A_STOCK_AGENT_BROWSER_WRAPPER_DIR=" + wrapperDir}, filepath.Join(workDir, "home"), workDir, filepath.Join(workDir, "runtime", "bin"))
	pathValue := envValue(values, "PATH")
	if !strings.Contains(pathValue, workspaceBin) {
		t.Fatalf("PATH = %q, want workspace node_modules/.bin", pathValue)
	}
	if !strings.Contains(pathValue, wrapperDir) {
		t.Fatalf("PATH = %q, want browser wrapper directory", pathValue)
	}
	if got := envValue(values, "AGENT_BROWSER_PROFILE"); got != filepath.Join(workDir, "home", "browser-profile") {
		t.Fatalf("AGENT_BROWSER_PROFILE = %q", got)
	}
}

func TestRuntimePythonUsesStandaloneWindowsInterpreter(t *testing.T) {
	root := filepath.Join("runtime", "hermes")
	if got := runtimePythonForOS(root, "windows"); got != filepath.Join(root, "python", "python.exe") {
		t.Fatalf("Windows runtime Python = %q", got)
	}
	if got := runtimePythonForOS(root, "darwin"); got != filepath.Join(root, "venv", "bin", "python") {
		t.Fatalf("macOS runtime Python = %q", got)
	}
}

func TestChineseLLMProviderDefaults(t *testing.T) {
	tests := []struct {
		provider string
		baseURL  string
		model    string
	}{
		{provider: "moonshot", baseURL: "https://api.moonshot.cn/v1", model: "moonshot-v1-8k"},
		{provider: "minimax", baseURL: "https://api.minimaxi.com/v1", model: "MiniMax-Text-01"},
		{provider: "zhipu", baseURL: "https://open.bigmodel.cn/api/paas/v4", model: "glm-4-plus"},
		{provider: "siliconflow", baseURL: "https://api.siliconflow.cn/v1", model: ""},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			if got := defaultBaseURL(tt.provider); got != tt.baseURL {
				t.Fatalf("base URL = %q, want %q", got, tt.baseURL)
			}
			if got := defaultModel(tt.provider); got != tt.model {
				t.Fatalf("model = %q, want %q", got, tt.model)
			}
		})
	}
}

func TestHermesFailureIncludesRuntimeDetailAndRedactsSecrets(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	const key = "test-model-key-value"
	if err := writeEnvValue(filepath.Join(home, ".env"), modelAPIKeyEnvName, key); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(Config{Home: home})
	err := runtime.hermesFailure("Hermes 会话意外结束", "Authorization: Bearer "+key+"\nMODEL_API_KEY="+key+"\npython runtime missing")
	message := err.Error()
	if strings.Contains(message, key) {
		t.Fatalf("Hermes error leaked API key: %s", message)
	}
	if !strings.Contains(message, "[REDACTED]") || !strings.Contains(message, "python runtime missing") {
		t.Fatalf("Hermes error omitted safe diagnostic detail: %s", message)
	}
}

func TestTailBufferKeepsOnlyTheLatestDiagnosticBytes(t *testing.T) {
	buffer := newTailBuffer(8)
	_, _ = buffer.Write([]byte("first-"))
	_, _ = buffer.Write([]byte("second"))
	if got := buffer.String(); got != "t-second" {
		t.Fatalf("tail buffer = %q, want %q", got, "t-second")
	}
}

func envValue(values []string, key string) string {
	prefix := key + "="
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return ""
}
