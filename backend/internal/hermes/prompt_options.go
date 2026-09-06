package hermes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultSandboxCodeTimeout = 60
	defaultSandboxToolCalls   = 50
)

var defaultSandboxToolsets = []string{"code_execution", "web"}

var allowedSandboxToolsets = map[string]bool{
	"code_execution": true,
	"web":            true,
}

// PromptOptions controls an isolated, unattended Hermes task. Auto approval
// is intentionally coupled to Sandbox so callers cannot bypass consent while
// the agent still has access to the normal application workspace.
type PromptOptions struct {
	Sandbox          bool
	AutoApprove      bool
	Toolsets         []string
	BrowserStatePath string
}

type OptionsPrompter interface {
	PromptWithOptions(ctx context.Context, prompt string, options PromptOptions) (PromptResult, error)
}

// PromptUsingOptions preserves compatibility with lightweight test and older
// prompters while allowing Runtime to enforce the sandbox for unattended jobs.
func PromptUsingOptions(ctx context.Context, prompter Prompter, prompt string, options PromptOptions) (PromptResult, error) {
	if options.AutoApprove && !options.Sandbox {
		return PromptResult{}, errors.New("Hermes 自动授权只能在隔离沙箱中启用")
	}
	if enhanced, ok := prompter.(OptionsPrompter); ok {
		return enhanced.PromptWithOptions(ctx, prompt, options)
	}
	return prompter.Prompt(ctx, prompt)
}

// PromptFullyAuthorized is for unattended product workflows (reviews,
// analysis jobs and probes), never for the interactive AI chat. It runs in an
// isolated workspace and automatically approves the limited toolsets allowed
// by that workspace.
func PromptFullyAuthorized(ctx context.Context, prompter Prompter, prompt string) (PromptResult, error) {
	return PromptUsingOptions(ctx, prompter, prompt, PromptOptions{Sandbox: true, AutoApprove: true})
}

// PromptFullyAuthorizedWithBrowserState is the unattended equivalent for a
// workflow that explicitly selected a browser login state.
func PromptFullyAuthorizedWithBrowserState(ctx context.Context, prompter BrowserStatePrompter, prompt, statePath string) (PromptResult, error) {
	if enhanced, ok := prompter.(interface {
		PromptWithOptionsAndBrowserState(context.Context, string, string, PromptOptions) (PromptResult, error)
	}); ok {
		return enhanced.PromptWithOptionsAndBrowserState(ctx, prompt, statePath, PromptOptions{Sandbox: true, AutoApprove: true, Toolsets: []string{"web"}, BrowserStatePath: statePath})
	}
	return prompter.PromptWithBrowserState(ctx, prompt, statePath)
}

type promptProcessOptions struct {
	workDir string
	env     map[string]string
	unset   []string
}

type promptSandbox struct {
	root    string
	process promptProcessOptions
}

func (s *promptSandbox) close() {
	if s == nil || s.root == "" {
		return
	}
	_ = os.RemoveAll(s.root)
}

func (r *Runtime) preparePromptSandbox(options PromptOptions) (*promptSandbox, error) {
	root, err := os.MkdirTemp("", "easy-stock-hermes-sandbox-")
	if err != nil {
		return nil, fmt.Errorf("创建 Hermes 临时沙箱: %w", err)
	}
	sandbox := &promptSandbox{root: root}
	fail := func(err error) (*promptSandbox, error) {
		sandbox.close()
		return nil, err
	}

	home := filepath.Join(root, "home")
	tempDir := filepath.Join(root, "tmp")
	workDir := filepath.Join(root, "work")
	siteDir := filepath.Join(root, "python")
	for _, path := range []string{home, tempDir, workDir, siteDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fail(fmt.Errorf("初始化 Hermes 临时沙箱: %w", err))
		}
	}

	baseConfig, err := r.readConfigMap()
	if err != nil {
		return fail(err)
	}
	sandboxConfig := minimalSandboxConfig(baseConfig, workDir)
	configData, err := yaml.Marshal(sandboxConfig)
	if err != nil {
		return fail(fmt.Errorf("编码 Hermes 沙箱配置: %w", err))
	}
	configPath := filepath.Join(home, "config.yaml")
	if err := writeSecureFile(configPath, configData); err != nil {
		return fail(fmt.Errorf("写入 Hermes 沙箱配置: %w", err))
	}
	if err := writeSecureFile(filepath.Join(siteDir, "sitecustomize.py"), []byte(sandboxSiteCustomize)); err != nil {
		return fail(fmt.Errorf("写入 Hermes Python 沙箱: %w", err))
	}

	toolsets, err := cleanPromptToolsets(options.Toolsets)
	if err != nil {
		return fail(err)
	}
	if len(toolsets) == 0 {
		toolsets = append([]string(nil), defaultSandboxToolsets...)
	}
	sandbox.process = promptProcessOptions{
		workDir: workDir,
		env: map[string]string{
			"HOME":                   home,
			"HERMES_HOME":            home,
			"TMPDIR":                 tempDir,
			"TEMP":                   tempDir,
			"TMP":                    tempDir,
			"HERMES_CONFIG":          configPath,
			staleTimeoutEnvName:      strconv.Itoa(r.responseTimeoutSeconds()),
			"HERMES_TUI_TOOLSETS":    strings.Join(toolsets, ","),
			"HERMES_IGNORE_RULES":    "1",
			"HERMES_TUI_CHECKPOINTS": "0",
			"TERMINAL_CWD":           workDir,
			"PYTHONPATH":             siteDir,
		},
		unset: []string{
			"AGENT_BROWSER_PROFILE",
			"AGENT_BROWSER_STATE",
			"HERMES_ENV",
			"HERMES_PROFILE",
			"HERMES_YOLO_MODE",
		},
	}
	if options.BrowserStatePath != "" {
		sandbox.process.env["AGENT_BROWSER_STATE"] = options.BrowserStatePath
		sandbox.process.unset = append(sandbox.process.unset, "AGENT_BROWSER_PROFILE")
		// The selected storage state is an explicit input to this unattended
		// workflow and is intentionally kept available inside the sandbox.
		for i, key := range sandbox.process.unset {
			if key == "AGENT_BROWSER_STATE" {
				sandbox.process.unset = append(sandbox.process.unset[:i], sandbox.process.unset[i+1:]...)
				break
			}
		}
	}
	return sandbox, nil
}

func minimalSandboxConfig(base map[string]any, workDir string) map[string]any {
	config := map[string]any{}
	baseModel, _ := stringMap(base["model"])
	model := copyMapKeys(baseModel, "default", "provider", "base_url", "api_mode")
	if len(model) > 0 {
		config["model"] = model
	}
	providerName := strings.TrimSpace(stringValue(model["provider"]))
	baseProviders, _ := stringMap(base["providers"])
	if providerName != "" {
		if baseProvider, ok := stringMap(baseProviders[providerName]); ok {
			provider := copyMapKeys(baseProvider,
				"name", "api", "key_env", "default_model", "transport", "stale_timeout_seconds",
			)
			if len(provider) > 0 {
				config["providers"] = map[string]any{providerName: provider}
			}
		}
	}
	if serviceTier, ok := base["service_tier"].(string); ok && strings.TrimSpace(serviceTier) != "" {
		config["service_tier"] = serviceTier
	}
	baseAgent, _ := stringMap(base["agent"])
	agent := copyMapKeys(baseAgent, "system_prompt", "reasoning_effort")
	config["agent"] = agent
	config["curator"] = map[string]any{"enabled": false}
	config["memory"] = map[string]any{"memory_enabled": false, "user_profile_enabled": false, "nudge_interval": 0}
	config["security"] = map[string]any{"allow_lazy_installs": false}
	config["approvals"] = map[string]any{"mode": "manual"}
	config["terminal"] = map[string]any{"cwd": workDir, "env_passthrough": []string{}}
	config["code_execution"] = map[string]any{
		"mode":           "strict",
		"timeout":        defaultSandboxCodeTimeout,
		"max_tool_calls": defaultSandboxToolCalls,
	}
	return config
}

func copyMapKeys(source map[string]any, keys ...string) map[string]any {
	result := map[string]any{}
	for _, key := range keys {
		if value, ok := source[key]; ok {
			result[key] = value
		}
	}
	return result
}

func cleanPromptToolsets(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		if !allowedSandboxToolsets[value] {
			return nil, fmt.Errorf("Hermes 沙箱不允许工具集 %q", value)
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

// The hook activates only in execute_code children, identified by the private
// RPC endpoint Hermes injects after spawning the child. The gateway itself
// keeps normal network access so it can reach the configured model provider.
const sandboxSiteCustomize = `import os
import sys

if os.environ.get("HERMES_RPC_SOCKET"):
    def _real(value):
        try:
            return os.path.realpath(os.fspath(value))
        except (TypeError, ValueError):
            return ""

    def _roots(values):
        result = []
        for value in values:
            path = _real(value)
            if path and path not in result:
                result.append(path)
        return tuple(result)

    _script_dir = os.path.dirname(_real(sys.argv[0]))
    _read_roots = _roots([
        os.environ.get("HOME", ""),
        os.environ.get("TMPDIR", ""),
        _script_dir,
        sys.prefix,
        sys.base_prefix,
        *[entry for entry in sys.path if entry],
    ])
    _write_roots = _roots([
        os.environ.get("HOME", ""),
        os.environ.get("TMPDIR", ""),
        _script_dir,
    ])
    _devices = {"/dev/null", "/dev/urandom", "NUL"}
    _rpc = os.environ.get("HERMES_RPC_SOCKET", "")

    def _inside(path, roots):
        path = _real(path)
        if not path:
            return False
        if path in _devices:
            return True
        for root in roots:
            try:
                if os.path.commonpath((path, root)) == root:
                    return True
            except (ValueError, OSError):
                pass
        return False

    def _deny(message):
        raise PermissionError("easy-stock review sandbox: " + message)

    def _open_is_write(args):
        mode = args[1] if len(args) > 1 else "r"
        flags = args[2] if len(args) > 2 else 0
        if isinstance(mode, str) and any(mark in mode for mark in "wax+"):
            return True
        if isinstance(flags, int):
            write_flags = os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_TRUNC | os.O_APPEND
            return bool(flags & write_flags)
        return False

    def _audit(event, args):
        if event == "open" and args and not isinstance(args[0], int):
            roots = _write_roots if _open_is_write(args) else _read_roots
            if not _inside(args[0], roots):
                _deny("file access outside the temporary workspace")
        elif event in {"os.listdir", "os.scandir"} and args:
            if not _inside(args[0], _read_roots):
                _deny("directory access outside the temporary workspace")
        elif event in {
            "os.remove", "os.rmdir", "os.mkdir", "os.chmod", "os.chown",
            "os.truncate", "os.utime", "os.chdir"
        } and args:
            if not _inside(args[0], _write_roots):
                _deny("filesystem mutation outside the temporary workspace")
        elif event in {"os.rename", "os.replace"} and len(args) >= 2:
            if not _inside(args[0], _write_roots) or not _inside(args[1], _write_roots):
                _deny("filesystem mutation outside the temporary workspace")
        elif event == "os.link" and len(args) >= 2:
            if not _inside(args[0], _read_roots) or not _inside(args[1], _write_roots):
                _deny("filesystem mutation outside the temporary workspace")
        elif event == "os.symlink" and len(args) >= 2:
            if not _inside(args[1], _write_roots):
                _deny("filesystem mutation outside the temporary workspace")
        elif event.startswith("subprocess.") or event in {
            "os.system", "os.posix_spawn", "os.spawn", "pty.spawn"
        }:
            _deny("subprocess execution is disabled")
        elif event in {"ctypes.dlopen", "ctypes.dlsym", "ctypes.call_function"}:
            _deny("native code loading is disabled")
        elif event == "socket.connect" and len(args) >= 2:
            address = args[1]
            allowed = address == _rpc
            if isinstance(address, tuple) and _rpc.startswith("tcp://"):
                host_port = _rpc[6:].rsplit(":", 1)
                if len(host_port) == 2:
                    allowed = str(address[0]) in {"127.0.0.1", "::1"} and str(address[1]) == host_port[1]
            if not allowed:
                _deny("direct network access is disabled; use approved Hermes web tools")
        elif event in {
            "socket.bind", "socket.getaddrinfo", "socket.gethostbyname",
            "socket.gethostbyaddr", "socket.getservbyname"
        }:
            _deny("direct network access is disabled; use approved Hermes web tools")

    sys.addaudithook(_audit)

    try:
        import resource
        resource.setrlimit(resource.RLIMIT_CPU, (30, 30))
        resource.setrlimit(resource.RLIMIT_FSIZE, (10 * 1024 * 1024, 10 * 1024 * 1024))
        resource.setrlimit(resource.RLIMIT_NOFILE, (64, 64))
    except (ImportError, OSError, ValueError):
        pass
`
