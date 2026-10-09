package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/appsettings"
)

//go:embed mcp_launcher.py
var mcpLauncher string

type CodexConfig struct {
	RuntimeRoot string
	Executable  string
	Home        string
	WorkDir     string
}

type CodexRuntime struct {
	config CodexConfig
	shared *HermesRuntime
}

func NewCodexRuntime(cfg CodexConfig, shared *HermesRuntime) *CodexRuntime {
	if cfg.Executable == "" && cfg.RuntimeRoot != "" {
		name := "codex"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		cfg.Executable = filepath.Join(cfg.RuntimeRoot, "bin", name)
	}
	return &CodexRuntime{config: cfg, shared: shared}
}

func (r *CodexRuntime) Status() Status {
	r.shared.mu.RLock()
	cfg, configured, hasKey := r.shared.llm, r.shared.configured, r.shared.hasAPIKey
	r.shared.mu.RUnlock()
	status := Status{Runtime: Codex, APIKeyConfigured: hasKey}
	if data, err := os.ReadFile(filepath.Join(r.config.RuntimeRoot, "runtime-manifest.json")); err == nil {
		var manifest struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(data, &manifest) == nil {
			status.Version = manifest.Version
		}
	}
	if !filepath.IsAbs(r.config.Executable) {
		status.Message = "Codex 包内运行时路径未配置"
		return status
	}
	info, err := os.Stat(r.config.Executable)
	if err != nil || info.IsDir() {
		status.Message = "Codex 运行时不可用，请检查安装包"
		return status
	}
	status.Available = true
	if !configured {
		status.Message = "请先配置模型连接"
		return status
	}
	if !SupportsResponses(cfg) {
		status.Message = ErrModelProtocolUnsupported.Error()
		return status
	}
	status.Configured = true
	return status
}

// Config projections contain no provider key. Secrets are supplied only in the
// child environment, which shell tools are explicitly forbidden to inherit.
func (r *CodexRuntime) renderConfiguration(options PromptOptions) (string, map[string]string, error) {
	r.shared.mu.RLock()
	cfg := r.shared.llm
	r.shared.mu.RUnlock()
	settings, err := r.shared.AgentSettings()
	if err != nil {
		return "", nil, err
	}
	key, err := r.shared.ModelAPIKey()
	if err != nil {
		return "", nil, err
	}
	env := map[string]string{"EASY_STOCK_MODEL_API_KEY": key}
	var b strings.Builder
	fmt.Fprintf(&b, "model = %s\nmodel_provider = \"easy-stock\"\nweb_search = \"disabled\"\ncheck_for_update_on_startup = false\ncli_auth_credentials_store = \"ephemeral\"\n", strconv.Quote(cfg.Model))
	effort := settings.ReasoningEffort
	if options.Sandbox && options.ReasoningEffortCap != "" {
		effort = cappedReasoningEffort(effort, options.ReasoningEffortCap, settings.Reasoning)
	}
	// Native Responses toggles (e.g. MiniMax M3) use any non-none effort
	// to enable thinking. Keep the shared UI as a toggle, not fake levels.
	if effort == "enabled" && settings.Reasoning.Wire == "openai_responses" {
		effort = "medium"
	}
	if effort != "" && effort != "default" && effort != "enabled" {
		if !strings.Contains("|none|minimal|low|medium|high|xhigh|max|", "|"+effort+"|") {
			return "", nil, fmt.Errorf("Codex 不支持当前思考强度 %s，请调整共享模型设置", effort)
		}
		fmt.Fprintf(&b, "model_reasoning_effort = %s\n", strconv.Quote(effort))
	}
	fmt.Fprintf(&b, "[model_providers.easy-stock]\nname = \"easy-stock\"\nbase_url = %s\nwire_api = \"responses\"\nenv_key = \"EASY_STOCK_MODEL_API_KEY\"\nrequires_openai_auth = false\nsupports_websockets = false\nstream_idle_timeout_ms = %d\n", strconv.Quote(strings.TrimRight(cfg.BaseURL, "/")), appsettings.NormalizeLLMResponseTimeoutSeconds(cfg.ResponseTimeoutSeconds)*1000)
	b.WriteString("[skills]\nbundled = { enabled = false }\n[analytics]\nenabled = false\n[feedback]\nenabled = false\n[shell_environment_policy]\ninherit = \"core\"\nignore_default_excludes = false\nexclude = [\"*KEY*\", \"*TOKEN*\", \"*SECRET*\", \"*PASSWORD*\", \"*COOKIE*\"]\n[features]\nmulti_agent = false\nmemories = false\nshell_snapshot = false\ngoals = false\nsleep_tool = false\nhooks = false\nplugins = false\napps = false\nskill_mcp_dependency_install = false\n")
	if options.Sandbox || options.DisableTools {
		b.WriteString("shell_tool = false\nunified_exec = false\nview_image = false\n[tools]\nview_image = false\nexperimental_request_user_input = { enabled = false }\nupdate_plan = { enabled = false }\n")
	}
	if !options.Sandbox && !options.DisableTools {
		for i, server := range settings.MCPServers {
			if !server.Enabled {
				continue
			}
			fmt.Fprintf(&b, "[mcp_servers.%s]\nrequired = true\n", strconv.Quote(server.Name))
			if server.Transport == "stdio" || server.Transport == "sse" {
				variable := fmt.Sprintf("EASY_STOCK_MCP_%d", i)
				spec, _ := json.Marshal(server)
				env[variable] = string(spec)
				fmt.Fprintf(&b, "command = %s\nargs = %s\nenv_vars = %s\n", strconv.Quote(r.shared.pythonPath), tomlStrings([]string{"-c", mcpLauncher, variable}), tomlStrings([]string{variable}))
			} else {
				fmt.Fprintf(&b, "url = %s\n", strconv.Quote(server.URL))
				headers := map[string]string{}
				names := make([]string, 0, len(server.Headers))
				for name := range server.Headers {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					value := server.Headers[name]
					varName := fmt.Sprintf("EASY_STOCK_MCP_%d_%d", i, len(headers))
					headers[name], env[varName] = varName, value
				}
				if len(headers) > 0 {
					fmt.Fprintf(&b, "env_http_headers = %s\n", tomlMap(headers))
				}
			}
			fmt.Fprintf(&b, "supports_parallel_tool_calls = %t\n", server.SupportsParallelToolCall)
			if server.Timeout > 0 {
				fmt.Fprintf(&b, "tool_timeout_sec = %d\n", server.Timeout)
			}
			if server.ConnectTimeout > 0 {
				fmt.Fprintf(&b, "startup_timeout_sec = %d\n", server.ConnectTimeout)
			}
		}
	}
	return b.String(), env, nil
}

func tomlStrings(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = strconv.Quote(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
func tomlMap(items map[string]string) string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, strconv.Quote(key)+" = "+strconv.Quote(items[key]))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func (r *CodexRuntime) SyncConfiguration() error {
	if r.config.Home == "" {
		return nil
	}
	content, _, err := r.renderConfiguration(PromptOptions{})
	if err != nil {
		return err
	}
	if err = os.MkdirAll(r.config.Home, 0700); err != nil {
		return err
	}
	return writeSecureFile(filepath.Join(r.config.Home, "config.toml"), []byte(content))
}

func (r *CodexRuntime) Start(ctx context.Context) (Process, error) {
	return r.start(ctx, PromptOptions{})
}

func (r *CodexRuntime) start(ctx context.Context, options PromptOptions) (Process, error) {
	status := r.Status()
	if !status.Available || !status.Configured {
		return nil, errors.New(status.Message)
	}
	if options.AutoApprove && !options.Sandbox {
		return nil, errors.New("自动授权只能在隔离任务中启用")
	}
	if options.DisableTools && len(options.Toolsets) > 0 {
		return nil, errors.New("禁用工具时不能指定工具集")
	}
	if _, err := cleanPromptToolsets(options.Toolsets); err != nil {
		return nil, err
	}
	content, secrets, err := r.renderConfiguration(options)
	if err != nil {
		return nil, err
	}
	// A configuration generation has its own native history. Existing threads
	// never observe another generation's model/MCP/skill settings mid-turn.
	skillDigest, err := r.skillDigest()
	if err != nil {
		return nil, err
	}
	secretBytes, _ := json.Marshal(secrets)
	digest := sha256.Sum256(append([]byte(content+skillDigest), secretBytes...))
	home := filepath.Join(r.config.Home, "generations", hex.EncodeToString(digest[:12]))
	cleanup := func() {}
	if options.Sandbox {
		home, err = os.MkdirTemp("", "easy-stock-codex-task-")
		if err != nil {
			return nil, err
		}
		cleanup = func() { _ = os.RemoveAll(home) }
	}
	if err = os.MkdirAll(home, 0700); err != nil {
		cleanup()
		return nil, err
	}
	if options.Sandbox || options.DisableTools {
		r.shared.mu.RLock()
		model := r.shared.llm.Model
		r.shared.mu.RUnlock()
		catalog := map[string]any{"models": []any{map[string]any{
			"slug": model, "display_name": model, "base_instructions": systemPrompt, "supported_reasoning_levels": []any{},
			"shell_type": "disabled", "visibility": "none", "supported_in_api": true, "priority": 1,
			"support_verbosity": false, "apply_patch_tool_type": nil,
			"truncation_policy":            map[string]any{"mode": "bytes", "limit": 10000},
			"experimental_supported_tools": []string{}, "supports_reasoning_summary_parameter": false,
			"include_skills_usage_instructions": false, "include_apps_usage_instructions": false,
		}}}
		data, _ := json.Marshal(catalog)
		catalogPath := filepath.Join(home, "models.json")
		if err = writeSecureFile(catalogPath, data); err != nil {
			cleanup()
			return nil, err
		}
		content = "model_catalog_json = " + strconv.Quote(catalogPath) + "\n" + content
	}
	if err = writeSecureFile(filepath.Join(home, "config.toml"), []byte(content)); err != nil {
		cleanup()
		return nil, err
	}
	var sandbox *promptSandbox
	if options.Sandbox && !options.DisableTools {
		sandbox, err = r.shared.preparePromptSandbox(options)
		if err != nil {
			cleanup()
			return nil, err
		}
		previousCleanup := cleanup
		cleanup = func() { sandbox.close(); previousCleanup() }
		toolsets := options.Toolsets
		if len(toolsets) == 0 {
			toolsets = defaultSandboxToolsets
		}
		toolEnv := sandbox.process.env
		for _, key := range []string{"A_STOCK_AGENT_BROWSER_WRAPPER_DIR", "A_STOCK_AGENT_BROWSER_REAL"} {
			if value := os.Getenv(key); value != "" {
				toolEnv[key] = value
			}
		}
		toolEnv["PATH"] = filepath.Dir(r.shared.pythonPath) + string(os.PathListSeparator) + os.Getenv("PATH")
		if wrapper := os.Getenv("A_STOCK_AGENT_BROWSER_WRAPPER_DIR"); wrapper != "" {
			toolEnv["PATH"] = wrapper + string(os.PathListSeparator) + toolEnv["PATH"]
		}
		spec, _ := json.Marshal(map[string]any{"transport": "builtin", "env": toolEnv, "toolsets": toolsets, "browser_state": options.BrowserStatePath})
		secrets["EASY_STOCK_MCP_BUILTIN"] = string(spec)
		content += fmt.Sprintf("\n[mcp_servers.easy_stock_research]\nrequired = true\ncommand = %s\nargs = %s\nenv_vars = [\"EASY_STOCK_MCP_BUILTIN\"]\nstartup_timeout_sec = 30\ntool_timeout_sec = 90\ndefault_tools_approval_mode = \"auto\"\n", strconv.Quote(r.shared.pythonPath), tomlStrings([]string{"-c", mcpLauncher, "EASY_STOCK_MCP_BUILTIN"}))
		if err = writeSecureFile(filepath.Join(home, "config.toml"), []byte(content)); err != nil {
			cleanup()
			return nil, err
		}
	}
	work := r.config.WorkDir
	if work == "" || options.Sandbox {
		work = filepath.Join(home, "workspace")
	}
	if err = os.MkdirAll(work, 0700); err != nil {
		cleanup()
		return nil, err
	}
	if !options.Sandbox && !options.DisableTools {
		if err = r.projectSkills(home); err != nil {
			cleanup()
			return nil, err
		}
	}
	values := os.Environ()
	for _, entry := range values {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "CODEX_") || strings.HasPrefix(name, "OPENAI_") || strings.HasPrefix(name, "MODEL_API_KEY") || strings.HasPrefix(name, "EASY_STOCK_MCP_") {
			values = unsetEnv(values, name)
		}
	}
	values = setEnv(values, "PATH", filepath.Join(r.config.RuntimeRoot, "codex-path")+string(os.PathListSeparator)+os.Getenv("PATH"))
	values = setEnv(values, "CODEX_HOME", home)
	// HOME is local to this child; this prevents discovery of the user's global
	// .agents skills, auth and shell profile without modifying their installation.
	values = setEnv(values, "HOME", home)
	values = setEnv(values, "USERPROFILE", home)
	for name, value := range secrets {
		values = setEnv(values, name, value)
	}
	cmd := exec.CommandContext(ctx, r.config.Executable, "app-server", "--listen", "stdio://")
	cmd.Dir, cmd.Env = work, values
	isolateProcess(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		cleanup()
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, err
	}
	errorsPipe, err := cmd.StderrPipe()
	if err != nil {
		cleanup()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		cleanup()
		return nil, fmt.Errorf("启动 Codex: %w", err)
	}
	p := newCodexProcess(ctx, cmd, input, output, errorsPipe, work, options, cleanup, secrets["EASY_STOCK_MODEL_API_KEY"])
	return p, nil
}

func (r *CodexRuntime) skillDigest() (string, error) {
	settings, err := r.shared.AgentSettings()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, skill := range settings.Skills {
		if !skill.Enabled {
			continue
		}
		source := skill.directory
		err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("Skill 不能包含符号链接")
			}
			if entry.IsDir() {
				return nil
			}
			relative, _ := filepath.Rel(r.shared.home, path)
			hash.Write([]byte(relative + "\x00"))
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			hash.Write(data)
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (r *CodexRuntime) projectSkills(home string) error {
	settings, err := r.shared.AgentSettings()
	if err != nil {
		return err
	}
	root := filepath.Join(home, "skills")
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	enabled := map[string]bool{}
	for _, skill := range settings.Skills {
		if !skill.Enabled {
			continue
		}
		if filepath.Base(skill.Name) != skill.Name || skill.Name == "." || strings.ContainsAny(skill.Name, "/\\") {
			return fmt.Errorf("无效的 Skill 名称: %s", skill.Name)
		}
		enabled[skill.Name] = true
		source := skill.directory
		if err = copySkillDirectory(source, filepath.Join(root, skill.Name)); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !enabled[entry.Name()] {
			if err = os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func copySkillDirectory(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Skill 不能包含符号链接: %s", entry.Name())
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("Skill 包含非普通文件: %s", entry.Name())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := writeSecureFile(dest, data); err != nil {
			return err
		}
		if info, err := entry.Info(); err == nil && info.Mode()&0111 != 0 {
			return os.Chmod(dest, 0700)
		}
		return nil
	})
}

func (r *CodexRuntime) PromptWithOptions(ctx context.Context, prompt string, options PromptOptions) (PromptResult, error) {
	p, err := r.start(ctx, options)
	if err != nil {
		return PromptResult{}, err
	}
	defer func() { _ = p.Stop(); _ = p.Wait() }()
	go io.Copy(io.Discard, p.Errors())
	frames := make(chan rpcFrame, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(frames)
		scanner := bufio.NewScanner(p.Output())
		scanner.Buffer(make([]byte, 65536), 4<<20)
		for scanner.Scan() {
			var f rpcFrame
			if json.Unmarshal(scanner.Bytes(), &f) == nil {
				select {
				case frames <- f:
				case <-done:
					return
				}
			}
		}
	}()
	send := func(id, method string, params any) error {
		data, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
		_, e := p.Input().Write(append(data, '\n'))
		return e
	}
	result := PromptResult{Runtime: Codex}
	watchdog := newPromptWatchdog(options)
	defer watchdog.close()
	for {
		select {
		case <-ctx.Done():
			result.Progress = watchdog.snapshot()
			return result, ctx.Err()
		case <-watchdog.channel():
			result.Progress = watchdog.snapshot()
			return result, watchdog.timeout()
		case f, ok := <-frames:
			if !ok {
				return result, errors.New("Codex 会话意外结束")
			}
			if f.Error != nil {
				return result, errors.New(f.Error.Message)
			}
			watchdog.observe(f)
			result.Progress = watchdog.snapshot()
			if err := watchdog.retryLimitError(); err != nil {
				return result, err
			}
			switch eventType(f) {
			case "message.delta":
				result.Content += firstNonEmpty(eventText(f, "delta"), eventText(f, "text"))
			case "gateway.ready":
				err = send("create", "session.create", map[string]any{})
			case "message.complete":
				result.Content = eventText(f, "content")
				result.Usage = usageFromFrame(f)
				if result.Usage.Model == "" {
					r.shared.mu.RLock()
					result.Usage.Model = r.shared.llm.Model
					r.shared.mu.RUnlock()
				}
				if strings.TrimSpace(result.Content) == "" {
					return result, errors.New("Codex 未返回有效内容")
				}
				return result, nil
			case "gateway.error", "message.error":
				return result, errors.New(eventText(f, "message"))
			case "approval", "clarify":
				data, _ := json.Marshal(map[string]any{"id": f.ID, "result": map[string]any{"choice": "deny", "answer": ""}})
				_, err = p.Input().Write(append(data, '\n'))
			}
			if f.ID == "create" && f.Result != nil {
				result.SessionID = stringValue(f.Result["session_id"])
				result.StoredSessionID = result.SessionID
				err = send("submit", "prompt.submit", map[string]any{"text": prompt, "session_id": result.SessionID})
			}
			if err != nil {
				return result, err
			}
		}
	}
}

type codexFrame struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params map[string]any  `json:"params,omitempty"`
	Result map[string]any  `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type codexProcess struct {
	cmd      *exec.Cmd
	input    *io.PipeWriter
	output   *io.PipeReader
	stderr   io.ReadCloser
	cancel   context.CancelFunc
	done     chan struct{}
	waitErr  error
	stopOnce sync.Once
}

func (p *codexProcess) Input() io.WriteCloser { return p.input }
func (p *codexProcess) Output() io.ReadCloser { return p.output }
func (p *codexProcess) Errors() io.ReadCloser { return p.stderr }
func (p *codexProcess) Wait() error           { <-p.done; return p.waitErr }
func (p *codexProcess) Stop() error {
	p.stopOnce.Do(func() {
		p.cancel()
		_ = p.input.Close()
		_ = p.output.Close()
	})
	return nil
}

func newCodexProcess(parent context.Context, cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, stderr io.ReadCloser, work string, options PromptOptions, cleanup func(), secret string) *codexProcess {
	ctx, cancel := context.WithCancel(parent)
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	p := &codexProcess{cmd: cmd, input: inWriter, output: outReader, stderr: stderr, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		defer cleanup()
		defer cancel()
		defer inReader.Close()
		defer outWriter.Close()
		defer stdin.Close()
		err := runCodexProtocol(ctx, inReader, outWriter, stdin, stdout, work, options, secret)
		if err != nil && ctx.Err() == nil {
			data, _ := json.Marshal(map[string]any{"method": "gateway.error", "params": map[string]any{"message": sanitizeHermesDiagnostic(err.Error(), secret)}})
			_, _ = outWriter.Write(append(data, '\n'))
		}
		// EOF lets App Server release its own MCP process groups and native
		// sessions. Hard termination is a bounded fallback, never the normal exit.
		_ = stdin.Close()
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		select {
		case p.waitErr = <-exited:
		case <-time.After(3 * time.Second):
			_ = killProcessTree(cmd)
			p.waitErr = <-exited
		}
	}()
	return p
}

func scanCodexFrames(ctx context.Context, reader io.Reader) <-chan codexFrame {
	ch := make(chan codexFrame, 8)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 65536), 4<<20)
		for scanner.Scan() {
			var f codexFrame
			if json.Unmarshal(scanner.Bytes(), &f) != nil {
				continue
			}
			select {
			case ch <- f:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

func runCodexProtocol(ctx context.Context, clientIn io.Reader, clientOut io.Writer, serverIn io.Writer, serverOut io.Reader, work string, options PromptOptions, secret string) error {
	write := func(w io.Writer, value any) error {
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = w.Write(append(b, '\n'))
		return err
	}
	notify := func(method string, params any) error {
		return write(clientOut, map[string]any{"method": method, "params": params})
	}
	request := func(id json.RawMessage, method string, params any) error {
		return write(serverIn, map[string]any{"id": id, "method": method, "params": params})
	}
	if err := request(json.RawMessage(`"easy-stock-init"`), "initialize", map[string]any{"clientInfo": map[string]string{"name": "easy_stock", "title": "easy-stock", "version": "1.2.2"}, "capabilities": map[string]any{"experimentalApi": true}}); err != nil {
		return err
	}
	upstream, client := scanCodexFrames(ctx, serverOut), scanCodexFrames(ctx, clientIn)
	pending := map[string]string{}
	interactions := map[string]codexFrame{}
	threadID, turnID, history := "", "", ""
	agentMessages := map[string]string{}
	messageOrder := []string{}
	finalMessages := map[string]bool{}
	usage := TokenUsage{}
	usageBase := TokenUsage{}
	haveUsageBase := false
	model := ""
	sequence := 0
	ready := false
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("Codex 初始化超时")
		case f, ok := <-client:
			if !ok {
				return nil
			}
			id := string(f.ID)
			if f.Method == "" && len(f.ID) > 0 {
				var clientID string
				_ = json.Unmarshal(f.ID, &clientID)
				original, exists := interactions[clientID]
				if !exists {
					continue
				}
				delete(interactions, clientID)
				result := map[string]any{}
				switch original.Method {
				case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
					decision := "decline"
					switch stringValue(f.Result["choice"]) {
					case "once":
						decision = "accept"
					case "session":
						decision = "acceptForSession"
					}
					result["decision"] = decision
				case "item/tool/requestUserInput", "tool/requestUserInput":
					answers := map[string]any{}
					given, _ := stringMap(f.Result["answers"])
					for qid, value := range given {
						answers[qid] = map[string]any{"answers": []string{stringValue(value)}}
					}
					result["answers"] = answers
				case "item/permissions/requestApproval":
					result = map[string]any{"permissions": map[string]any{}, "scope": "turn"}
					if choice := stringValue(f.Result["choice"]); choice == "once" || choice == "session" {
						result["permissions"] = original.Params["permissions"]
						if choice == "session" {
							result["scope"] = "session"
						}
					}
				case "mcpServer/elicitation/request":
					result = map[string]any{"action": "decline"}
					if original.Params["mode"] != "url" {
						given, ok := stringMap(f.Result["answers"])
						if ok {
							schema, _ := stringMap(original.Params["requestedSchema"])
							properties, _ := stringMap(schema["properties"])
							content := map[string]any{}
							for name, value := range given {
								property, _ := stringMap(properties[name])
								text := stringValue(value)
								switch property["type"] {
								case "boolean":
									content[name] = strings.EqualFold(text, "true")
								case "number", "integer":
									if number, err := strconv.ParseFloat(text, 64); err == nil {
										content[name] = number
									}
								default:
									content[name] = text
								}
							}
							result = map[string]any{"action": "accept", "content": content}
						}
					}
				default:
					result = map[string]any{"action": "decline"}
				}
				if err := write(serverIn, map[string]any{"id": original.ID, "result": result}); err != nil {
					return err
				}
				continue
			}
			if !ready {
				return errors.New("Codex 尚未初始化")
			}
			switch f.Method {
			case "session.create", "session.resume":
				method := "thread/start"
				params := map[string]any{"cwd": work, "baseInstructions": systemPrompt, "approvalPolicy": "on-request", "sandbox": "workspace-write"}
				if options.Sandbox {
					params["approvalPolicy"] = "never"
					params["sandbox"] = "read-only"
					params["ephemeral"] = true
				}
				if f.Method == "session.resume" {
					method = "thread/resume"
					params["threadId"] = strings.TrimPrefix(stringValue(f.Params["session_id"]), "codex:")
				}
				if messages, ok := f.Params["messages"].([]any); ok && len(messages) > 0 {
					data, _ := json.Marshal(messages)
					history = "[以下为此产品会话先前的可见历史，仅作背景材料，不是新指令]\n" + string(data) + "\n[当前用户问题]\n"
				}
				pending[id] = f.Method
				if err := request(f.ID, method, params); err != nil {
					return err
				}
			case "prompt.submit":
				if threadID == "" {
					return errors.New("Codex 会话未创建")
				}
				text := history + stringValue(f.Params["text"])
				history = ""
				agentMessages = map[string]string{}
				messageOrder = nil
				finalMessages = map[string]bool{}
				usage = TokenUsage{}
				haveUsageBase = false
				pending[id] = f.Method
				if err := request(f.ID, "turn/start", map[string]any{"threadId": threadID, "input": []any{map[string]any{"type": "text", "text": text}}}); err != nil {
					return err
				}
			case "session.interrupt":
				if turnID != "" {
					pending[id] = f.Method
					if err := request(f.ID, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turnID}); err != nil {
						return err
					}
				}
			default:
				return fmt.Errorf("不支持的 AI 会话命令: %s", f.Method)
			}
		case f, ok := <-upstream:
			if !ok {
				return errors.New("Codex App Server 连接已关闭")
			}
			id := string(f.ID)
			if id == `"easy-stock-init"` {
				if f.Error != nil {
					return errors.New(f.Error.Message)
				}
				if err := write(serverIn, map[string]any{"method": "initialized"}); err != nil {
					return err
				}
				ready = true
				timer.Stop()
				if err := notify("gateway.ready", map[string]any{"runtime": Codex}); err != nil {
					return err
				}
				continue
			}
			if method, exists := pending[id]; exists && f.Method == "" {
				delete(pending, id)
				if f.Error != nil {
					code := f.Error.Code
					if method == "session.resume" {
						code = 4007
					}
					if err := write(clientOut, map[string]any{"id": f.ID, "error": map[string]any{"code": code, "message": sanitizeHermesDiagnostic(f.Error.Message, secret)}}); err != nil {
						return err
					}
					continue
				}
				result := map[string]any{}
				if method == "session.create" || method == "session.resume" {
					thread, _ := stringMap(f.Result["thread"])
					threadID = stringValue(thread["id"])
					model = firstNonEmpty(stringValue(f.Result["model"]), model)
					result = map[string]any{"session_id": "codex:" + threadID, "stored_session_id": "codex:" + threadID}
				}
				if method == "prompt.submit" {
					turn, _ := stringMap(f.Result["turn"])
					turnID = stringValue(turn["id"])
				}
				if err := write(clientOut, map[string]any{"id": f.ID, "result": result}); err != nil {
					return err
				}
				continue
			}
			if len(f.ID) > 0 && f.Method != "" {
				sequence++
				clientID := fmt.Sprintf("codex-request-%d", sequence)
				interactions[clientID] = f
				if f.Method == "item/tool/requestUserInput" || f.Method == "tool/requestUserInput" {
					questions := []any{}
					list, _ := f.Params["questions"].([]any)
					for _, q := range list {
						question, _ := stringMap(q)
						choices := []string{}
						opts, _ := question["options"].([]any)
						for _, opt := range opts {
							o, _ := stringMap(opt)
							choices = append(choices, stringValue(o["label"]))
						}
						questions = append(questions, map[string]any{"qid": question["id"], "question": question["question"], "choices": choices})
					}
					if err := write(clientOut, map[string]any{"id": clientID, "method": "clarify", "params": map[string]any{"questions": questions}}); err != nil {
						return err
					}
				} else if f.Method == "mcpServer/elicitation/request" && f.Params["mode"] != "url" {
					schema, _ := stringMap(f.Params["requestedSchema"])
					properties, _ := stringMap(schema["properties"])
					names := make([]string, 0, len(properties))
					for name := range properties {
						names = append(names, name)
					}
					sort.Strings(names)
					questions := []any{}
					for _, name := range names {
						prop, _ := stringMap(properties[name])
						choices, _ := prop["enum"].([]any)
						if prop["type"] == "boolean" {
							choices = []any{"true", "false"}
						}
						questions = append(questions, map[string]any{"qid": name, "question": firstNonEmpty(stringValue(prop["title"]), name) + " · " + stringValue(f.Params["message"]), "choices": choices})
					}
					if len(questions) == 0 {
						if err := write(serverIn, map[string]any{"id": f.ID, "result": map[string]any{"action": "decline"}}); err != nil {
							return err
						}
						delete(interactions, clientID)
						continue
					}
					if err := write(clientOut, map[string]any{"id": clientID, "method": "clarify", "params": map[string]any{"questions": questions}}); err != nil {
						return err
					}
				} else if f.Method == "item/commandExecution/requestApproval" || f.Method == "item/fileChange/requestApproval" || f.Method == "item/permissions/requestApproval" {
					if err := write(clientOut, map[string]any{"id": clientID, "method": "approval", "params": map[string]any{"description": approvalDescription(f), "command": f.Params["command"]}}); err != nil {
						return err
					}
				} else {
					delete(interactions, clientID)
					response := map[string]any{"id": f.ID, "error": map[string]any{"code": -32601, "message": "当前应用不支持此交互，请在 MCP 服务端完成认证后重试"}}
					if f.Method == "mcpServer/elicitation/request" {
						response = map[string]any{"id": f.ID, "result": map[string]any{"action": "decline"}}
					}
					if err := write(serverIn, response); err != nil {
						return err
					}
				}
				continue
			}
			switch f.Method {
			case "item/agentMessage/delta":
				itemID := stringValue(f.Params["itemId"])
				if _, exists := agentMessages[itemID]; !exists {
					messageOrder = append(messageOrder, itemID)
				}
				delta := stringValue(f.Params["delta"])
				agentMessages[itemID] += delta
				if err := notify("message.delta", map[string]any{"delta": delta}); err != nil {
					return err
				}
			case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
				if err := notify("reasoning.delta", map[string]any{"delta": f.Params["delta"]}); err != nil {
					return err
				}
			case "item/started", "item/completed":
				item, _ := stringMap(f.Params["item"])
				kind := stringValue(item["type"])
				itemID := stringValue(item["id"])
				if kind == "agentMessage" && f.Method == "item/completed" {
					if _, exists := agentMessages[itemID]; !exists {
						messageOrder = append(messageOrder, itemID)
					}
					agentMessages[itemID] = stringValue(item["text"])
					finalMessages[itemID] = stringValue(item["phase"]) == "final_answer"
				} else if kind != "agentMessage" {
					if err := notify("status.update", map[string]any{"kind": "tool", "text": firstNonEmpty(stringValue(item["tool"]), stringValue(item["command"]), kind)}); err != nil {
						return err
					}
				}
			case "thread/tokenUsage/updated":
				tokenUsage, _ := stringMap(f.Params["tokenUsage"])
				last, _ := stringMap(tokenUsage["last"])
				total, _ := stringMap(tokenUsage["total"])
				if !haveUsageBase {
					usageBase = TokenUsage{PromptTokens: numberValue(total["inputTokens"]) - numberValue(last["inputTokens"]), CompletionTokens: numberValue(total["outputTokens"]) - numberValue(last["outputTokens"]), TotalTokens: numberValue(total["totalTokens"]) - numberValue(last["totalTokens"])}
					haveUsageBase = true
				}
				usage = TokenUsage{Model: model, PromptTokens: numberValue(total["inputTokens"]) - usageBase.PromptTokens, CompletionTokens: numberValue(total["outputTokens"]) - usageBase.CompletionTokens, TotalTokens: numberValue(total["totalTokens"]) - usageBase.TotalTokens}
			case "turn/completed":
				turn, _ := stringMap(f.Params["turn"])
				status := stringValue(turn["status"])
				if status != "completed" {
					detail, _ := stringMap(turn["error"])
					return errors.New(firstNonEmpty(stringValue(detail["message"]), "Codex 任务"+status))
				}
				hasFinal := false
				for _, yes := range finalMessages {
					hasFinal = hasFinal || yes
				}
				parts := []string{}
				for _, itemID := range messageOrder {
					if !hasFinal || finalMessages[itemID] {
						parts = append(parts, agentMessages[itemID])
					}
				}
				if err := notify("message.complete", map[string]any{"content": strings.Join(parts, "\n\n"), "usage": usage, "runtime": Codex}); err != nil {
					return err
				}
			case "error":
				if retrying, ok := f.Params["willRetry"].(bool); !ok || !retrying {
					e, _ := stringMap(f.Params["error"])
					return errors.New(firstNonEmpty(stringValue(e["message"]), "Codex 执行失败"))
				}
			}
		}
	}
}

func approvalDescription(frame codexFrame) string {
	detail := firstNonEmpty(stringValue(frame.Params["reason"]), frame.Method)
	if permissions, ok := frame.Params["permissions"]; ok {
		data, _ := json.Marshal(permissions)
		detail += "\n请求权限：" + string(data)
	}
	return detail
}
