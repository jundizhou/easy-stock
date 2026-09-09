package hermes

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type optionsRecordingPrompter struct {
	baseCalls    int
	optionCalls  int
	lastOptions  PromptOptions
	promptResult PromptResult
}

func (p *optionsRecordingPrompter) Prompt(context.Context, string) (PromptResult, error) {
	p.baseCalls++
	return p.promptResult, nil
}

func (p *optionsRecordingPrompter) PromptWithOptions(_ context.Context, _ string, options PromptOptions) (PromptResult, error) {
	p.optionCalls++
	p.lastOptions = options
	return p.promptResult, nil
}

func TestPromptUsingOptionsRequiresSandboxForAutoApproval(t *testing.T) {
	prompter := &optionsRecordingPrompter{}
	_, err := PromptUsingOptions(context.Background(), prompter, "test", PromptOptions{AutoApprove: true})
	if err == nil || !strings.Contains(err.Error(), "隔离沙箱") {
		t.Fatalf("PromptUsingOptions() error = %v, want sandbox requirement", err)
	}
	if prompter.baseCalls != 0 || prompter.optionCalls != 0 {
		t.Fatalf("unsafe prompt reached prompter: base=%d options=%d", prompter.baseCalls, prompter.optionCalls)
	}

	result, err := PromptUsingOptions(context.Background(), prompter, "test", PromptOptions{Sandbox: true, AutoApprove: true})
	if err != nil || result != prompter.promptResult {
		t.Fatalf("PromptUsingOptions() = %+v, %v", result, err)
	}
	if prompter.optionCalls != 1 || !prompter.lastOptions.Sandbox || !prompter.lastOptions.AutoApprove {
		t.Fatalf("options were not forwarded: %+v", prompter.lastOptions)
	}
}

func TestPreparePromptSandboxUsesMinimalConfigAndToolsets(t *testing.T) {
	home := t.TempDir()
	config := map[string]any{
		"model": map[string]any{"provider": "easy-stock", "default": "test-model"},
		"providers": map[string]any{"easy-stock": map[string]any{
			"api": "https://example.invalid/v1", "key_env": modelAPIKeyEnvName,
		}},
		"agent": map[string]any{"system_prompt": "test", "reasoning_effort": "medium"},
		"mcp_servers": map[string]any{"private": map[string]any{
			"enabled": true, "env": map[string]string{"TOKEN": "must-not-copy"},
		}},
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(Config{Home: home})
	sandbox, err := runtime.preparePromptSandbox(PromptOptions{
		Sandbox:  true,
		Toolsets: []string{"code_execution", "web", "web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	root := sandbox.root
	defer sandbox.close()

	if got := sandbox.process.env["HERMES_TUI_TOOLSETS"]; got != "code_execution,web" {
		t.Fatalf("toolsets = %q", got)
	}
	if got := sandbox.process.env["HERMES_HOME"]; got != filepath.Join(root, "home") {
		t.Fatalf("Hermes home = %q, want sandbox home", got)
	}
	if got := sandbox.process.env[staleTimeoutEnvName]; got != "300" {
		t.Fatalf("stale timeout = %q, want 300", got)
	}
	if sandbox.process.workDir == "" || !strings.HasPrefix(sandbox.process.workDir, root+string(os.PathSeparator)) {
		t.Fatalf("work dir is outside sandbox: %q", sandbox.process.workDir)
	}
	sandboxData, err := os.ReadFile(sandbox.process.env["HERMES_CONFIG"])
	if err != nil {
		t.Fatal(err)
	}
	var sandboxConfig map[string]any
	if err := yaml.Unmarshal(sandboxData, &sandboxConfig); err != nil {
		t.Fatal(err)
	}
	if _, ok := sandboxConfig["mcp_servers"]; ok || strings.Contains(string(sandboxData), "must-not-copy") {
		t.Fatalf("sandbox config copied MCP secrets:\n%s", sandboxData)
	}
	codeExecution, _ := stringMap(sandboxConfig["code_execution"])
	if stringValue(codeExecution["mode"]) != "strict" || intValue(codeExecution["timeout"]) != defaultSandboxCodeTimeout {
		t.Fatalf("unexpected code execution config: %+v", codeExecution)
	}
	if _, err := os.Stat(filepath.Join(sandbox.process.env["PYTHONPATH"], "sitecustomize.py")); err != nil {
		t.Fatalf("sitecustomize missing: %v", err)
	}

	sandbox.close()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("sandbox was not removed: %v", err)
	}
}

func TestPreparePromptSandboxUsesCurrentResponseTimeout(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(Config{Home: home})
	runtime.llm.ResponseTimeoutSeconds = 600
	t.Setenv(staleTimeoutEnvName, "90")

	sandbox, err := runtime.preparePromptSandbox(PromptOptions{Sandbox: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.close()

	if got := sandbox.process.env[staleTimeoutEnvName]; got != "600" {
		t.Fatalf("stale timeout = %q, want 600", got)
	}
	processEnv, err := runtime.processEnvironment("", sandbox.process)
	if err != nil {
		t.Fatal(err)
	}
	if got := environmentValue(processEnv, staleTimeoutEnvName); got != "600" {
		t.Fatalf("process stale timeout = %q, want 600", got)
	}
}

func TestPreparePromptSandboxRejectsToolsOutsideAllowlist(t *testing.T) {
	runtime := NewRuntime(Config{Home: t.TempDir()})
	_, err := runtime.preparePromptSandbox(PromptOptions{Sandbox: true, Toolsets: []string{"terminal"}})
	if err == nil || !strings.Contains(err.Error(), "不允许工具集") {
		t.Fatalf("preparePromptSandbox() error = %v, want rejected toolset", err)
	}
}

func TestPreparePromptSandboxCanDisableAllTools(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(Config{Home: home})
	sandbox, err := runtime.preparePromptSandbox(PromptOptions{Sandbox: true, DisableTools: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.close()

	if got := sandbox.process.env["HERMES_TUI_TOOLSETS"]; got != "context_engine" {
		t.Fatalf("toolsets = %q, want zero-tool context_engine pin", got)
	}
	if _, err := runtime.preparePromptSandbox(PromptOptions{Sandbox: true, DisableTools: true, Toolsets: []string{"web"}}); err == nil || !strings.Contains(err.Error(), "不能同时指定") {
		t.Fatalf("DisableTools with explicit toolsets error = %v", err)
	}
}

func TestSandboxSiteCustomizeBlocksHostFilesAndSubprocesses(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	hostDir := t.TempDir()
	hostFile := filepath.Join(hostDir, "outside.txt")
	if err := os.WriteFile(hostFile, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(Config{Home: t.TempDir()})
	sandbox, err := runtime.preparePromptSandbox(PromptOptions{Sandbox: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.close()

	script := `import subprocess
blocked = 0
try:
    open(` + strconvQuote(hostFile) + `, "r").read()
except PermissionError:
    blocked += 1
try:
    subprocess.run([sys.executable, "-c", "print('escaped')"], check=True)
except PermissionError:
    blocked += 1
print(blocked)
`
	command := exec.Command(python, "-c", "import sys\n"+script)
	command.Dir = sandbox.process.workDir
	values := append([]string(nil), os.Environ()...)
	for key, value := range sandbox.process.env {
		values = setEnv(values, key, value)
	}
	values = setEnv(values, "HERMES_RPC_SOCKET", filepath.Join(sandbox.root, "rpc.sock"))
	command.Env = values
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox probe failed: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "2" {
		t.Fatalf("sandbox blocked count = %q, want 2", output)
	}
}

func TestMinimalSandboxConfigOnlyCopiesSelectedProviderFields(t *testing.T) {
	config := minimalSandboxConfig(map[string]any{
		"model": map[string]any{
			"default": "test-model", "provider": "selected", "api_mode": "chat_completions", "secret": "model-secret",
		},
		"providers": map[string]any{
			"selected": map[string]any{
				"api": "https://example.invalid/v1", "key_env": modelAPIKeyEnvName, "default_model": "test-model", "api_key": "inline-secret", "headers": map[string]any{"Authorization": "secret"},
			},
			"unused": map[string]any{"api_key": "other-secret"},
		},
		"mcp_servers": map[string]any{"private": map[string]any{"token": "mcp-secret"}},
	}, "/tmp/sandbox-work")
	data, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, secret := range []string{"model-secret", "inline-secret", "other-secret", "mcp-secret", "Authorization"} {
		if strings.Contains(content, secret) {
			t.Fatalf("sandbox config leaked %q:\n%s", secret, content)
		}
	}
	providers, _ := stringMap(config["providers"])
	if len(providers) != 1 {
		t.Fatalf("sandbox providers = %+v, want selected provider only", providers)
	}
}

func TestRuntimePromptAutoApprovesGatewayRequestsInsideSandbox(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test gateway fixture uses a POSIX shell")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte(modelAPIKeyEnvName+"=test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	capturePath := filepath.Join(root, "approval.json")
	launcher := filepath.Join(root, "gateway-fixture.sh")
	fixture := `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
IFS= read -r create
printf '%s\n' '{"jsonrpc":"2.0","id":"1","result":{"session_id":"sandbox-session","stored_session_id":"stored-session"}}'
IFS= read -r submit
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"approval.request","session_id":"sandbox-session","payload":{"pattern_key":"execute_code"}}}'
IFS= read -r approval
printf '%s' "$approval" > "$APPROVAL_CAPTURE_PATH"
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","payload":{"content":"{\"ok\":true}","usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150}}}}'
`
	if err := os.WriteFile(launcher, []byte(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPROVAL_CAPTURE_PATH", capturePath)
	runtime := NewRuntime(Config{Home: home, WorkDir: root, PythonPath: launcher})
	runtime.configured = true
	runtime.hasAPIKey = true

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := runtime.PromptWithOptions(ctx, "test", PromptOptions{Sandbox: true, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != `{"ok":true}` || result.SessionID != "sandbox-session" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Usage.PromptTokens != 120 || result.Usage.CompletionTokens != 30 || result.Usage.TotalTokens != 150 {
		t.Fatalf("unexpected token usage: %+v", result.Usage)
	}
	approvalData, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	var approval map[string]any
	if err := json.Unmarshal(approvalData, &approval); err != nil {
		t.Fatalf("invalid approval RPC: %v\n%s", err, approvalData)
	}
	params, _ := stringMap(approval["params"])
	if stringValue(approval["method"]) != "approval.respond" || stringValue(params["session_id"]) != "sandbox-session" || stringValue(params["choice"]) != "session" {
		t.Fatalf("unexpected approval RPC: %s", approvalData)
	}
}

func strconvQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
