package hermes

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"easy-stock/backend/internal/appsettings"
)

const (
	providerSlug       = "easy-stock"
	modelAPIKeyEnvName = "MODEL_API_KEY"
)

const systemPrompt = `你是 easy-stock 的 AI 投研助手。easy-stock 是面向 A 股市场的 AI 原生行情分析与研究工作台。像 Codex 一样协作：先理解目标，再基于可追踪的数据和原文证据给出清晰、可执行、可验证的回答；主动区分事实、推断、市场预期与待验证条件，不编造实时数据，不承诺收益。涉及游资、心法、情绪周期、龙头战法、首板、打板、仓位或预期差时，优先使用本机的 a-stock-short-term-masters 技能核对原文，并说明这些内容属于历史经验与二次整理材料。默认使用中文，除非用户要求其他语言。`

type Status struct {
	Available        bool   `json:"available"`
	Configured       bool   `json:"configured"`
	APIKeyConfigured bool   `json:"api_key_configured"`
	Version          string `json:"version,omitempty"`
	Message          string `json:"message,omitempty"`
}

type PromptResult struct {
	Content         string
	SessionID       string
	StoredSessionID string
}

type Process interface {
	Input() io.WriteCloser
	Output() io.ReadCloser
	Errors() io.ReadCloser
	Wait() error
	Stop() error
}

type Prompter interface {
	Prompt(ctx context.Context, prompt string) (PromptResult, error)
}

type BrowserStatePrompter interface {
	PromptWithBrowserState(ctx context.Context, prompt, storageStatePath string) (PromptResult, error)
}

type Gateway interface {
	Prompter
	Status() Status
	ModelAPIKey() (string, error)
	SyncLLM(cfg appsettings.LLM, apiKeyUpdate *string) error
	Start(ctx context.Context) (Process, error)
}

type Config struct {
	RuntimeRoot string
	Home        string
	WorkDir     string
	PythonPath  string
}

type Runtime struct {
	runtimeRoot string
	home        string
	workDir     string
	pythonPath  string

	mu         sync.RWMutex
	llm        appsettings.LLM
	configured bool
	hasAPIKey  bool
}

func NewRuntime(cfg Config) *Runtime {
	runtimeRoot := strings.TrimSpace(cfg.RuntimeRoot)
	home := strings.TrimSpace(cfg.Home)
	workDir := strings.TrimSpace(cfg.WorkDir)
	pythonPath := strings.TrimSpace(cfg.PythonPath)
	if pythonPath == "" && runtimeRoot != "" {
		pythonPath = runtimePython(runtimeRoot)
	}
	return &Runtime{
		runtimeRoot: runtimeRoot,
		home:        home,
		workDir:     workDir,
		pythonPath:  pythonPath,
	}
}

func (r *Runtime) Status() Status {
	r.mu.RLock()
	configured := r.configured
	hasAPIKey := r.hasAPIKey
	r.mu.RUnlock()

	status := Status{Configured: configured, APIKeyConfigured: hasAPIKey, Version: r.runtimeVersion()}
	if r.pythonPath == "" {
		status.Message = "未配置 Hermes 运行时路径"
		return status
	}
	info, err := os.Stat(r.pythonPath)
	if err != nil || info.IsDir() {
		status.Message = "Hermes 运行时不可用"
		return status
	}
	status.Available = true
	if !configured {
		status.Message = "请先配置 Hermes 使用的模型"
	}
	return status
}

// ModelAPIKey returns the protected provider key for backend-only operations
// such as discovering models. Callers must never expose the returned value in
// responses or logs.
func (r *Runtime) ModelAPIKey() (string, error) {
	if strings.TrimSpace(r.home) == "" {
		return "", errors.New("Hermes 用户目录未配置")
	}
	return readEnvValue(filepath.Join(r.home, ".env"), modelAPIKeyEnvName)
}

// SyncLLM writes the app's model selection into Hermes' own config.yaml and
// writes secrets only into Hermes' .env. A nil apiKeyUpdate retains the
// existing MODEL_API_KEY; a pointer to an empty string clears it.
func (r *Runtime) SyncLLM(cfg appsettings.LLM, apiKeyUpdate *string) error {
	if strings.TrimSpace(r.home) == "" {
		return errors.New("Hermes 用户目录未配置")
	}
	if err := os.MkdirAll(r.home, 0o700); err != nil {
		return fmt.Errorf("创建 Hermes 用户目录: %w", err)
	}

	envPath := filepath.Join(r.home, ".env")
	existingKey, err := readEnvValue(envPath, modelAPIKeyEnvName)
	if err != nil {
		return err
	}
	effectiveKey := existingKey
	if apiKeyUpdate != nil {
		effectiveKey = strings.TrimSpace(*apiKeyUpdate)
		if strings.ContainsAny(effectiveKey, "\r\n") {
			return errors.New("模型 API Key 不能包含换行")
		}
		if err := writeEnvValue(envPath, modelAPIKeyEnvName, effectiveKey); err != nil {
			return err
		}
	} else if _, statErr := os.Stat(envPath); errors.Is(statErr, os.ErrNotExist) {
		if err := writeEnvValue(envPath, modelAPIKeyEnvName, ""); err != nil {
			return err
		}
	}

	normalized := normalizeLLM(cfg)
	configText := renderConfig(normalized, r.workDir)
	if err := writeSecureFile(filepath.Join(r.home, "config.yaml"), []byte(configText)); err != nil {
		return fmt.Errorf("写入 Hermes 模型配置: %w", err)
	}

	configured := normalized.Model != "" && normalized.BaseURL != "" && (normalized.Provider == "custom" || effectiveKey != "")
	r.mu.Lock()
	r.llm = normalized
	r.configured = configured
	r.hasAPIKey = effectiveKey != ""
	r.mu.Unlock()
	return nil
}

func (r *Runtime) Start(ctx context.Context) (Process, error) {
	return r.start(ctx, "")
}

func (r *Runtime) start(ctx context.Context, browserStatePath string) (Process, error) {
	status := r.Status()
	if !status.Available {
		return nil, errors.New(firstNonEmpty(status.Message, "Hermes 运行时不可用"))
	}
	if strings.TrimSpace(r.home) == "" {
		return nil, errors.New("Hermes 用户目录未配置")
	}
	processEnv, err := r.processEnvironment(browserStatePath)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, r.pythonPath, "-m", "tui_gateway.entry")
	cmd.Dir = firstExistingDirectory(r.workDir, r.home)
	cmd.Env = processEnv
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("启动 Hermes: %w", err)
	}
	return &commandProcess{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

func (r *Runtime) processEnvironment(browserStatePath string) ([]string, error) {
	modelAPIKey, err := readEnvValue(filepath.Join(r.home, ".env"), modelAPIKeyEnvName)
	if err != nil {
		return nil, err
	}
	values := hermesEnvironment(os.Environ(), r.home, r.workDir, filepath.Dir(r.pythonPath))
	// Hermes' TUI gateway resolves provider key_env values from the process
	// environment. Explicitly mirror the protected Hermes .env value and
	// override any ambient shell value so the application's setting is the
	// single source of truth, including after the key is cleared.
	values = setEnv(values, modelAPIKeyEnvName, modelAPIKey)
	if browserStatePath != "" {
		absolutePath, err := filepath.Abs(browserStatePath)
		if err != nil {
			return nil, fmt.Errorf("解析浏览器登录态路径: %w", err)
		}
		info, err := os.Stat(absolutePath)
		if err != nil || info.IsDir() {
			return nil, errors.New("雪球浏览器登录态不存在，请先在设置中登录")
		}
		values = setEnv(values, "AGENT_BROWSER_STATE", absolutePath)
		// agent-browser rejects storage_state and a persistent profile used at
		// the same time. The Hermes task session is already isolated, so this
		// browser run must use only the explicitly selected storage state.
		values = unsetEnv(values, "AGENT_BROWSER_PROFILE")
	}
	return values, nil
}

func (r *Runtime) Prompt(ctx context.Context, prompt string) (PromptResult, error) {
	return r.prompt(ctx, prompt, "")
}

func (r *Runtime) PromptWithBrowserState(ctx context.Context, prompt, storageStatePath string) (PromptResult, error) {
	if strings.TrimSpace(storageStatePath) == "" {
		return PromptResult{}, errors.New("浏览器登录态路径不能为空")
	}
	return r.prompt(ctx, prompt, strings.TrimSpace(storageStatePath))
}

func (r *Runtime) prompt(ctx context.Context, prompt, browserStatePath string) (PromptResult, error) {
	if strings.TrimSpace(prompt) == "" {
		return PromptResult{}, errors.New("Hermes 提示词不能为空")
	}
	status := r.Status()
	if !status.Available {
		return PromptResult{}, errors.New(firstNonEmpty(status.Message, "Hermes 运行时不可用"))
	}
	if !status.Configured {
		return PromptResult{}, errors.New(firstNonEmpty(status.Message, "请先配置 Hermes 模型"))
	}

	process, err := r.start(ctx, browserStatePath)
	if err != nil {
		return PromptResult{}, err
	}
	defer func() {
		_ = process.Stop()
		_ = process.Wait()
	}()
	go io.Copy(io.Discard, process.Errors())

	type lineResult struct {
		line []byte
		err  error
	}
	lines := make(chan lineResult, 1)
	go func() {
		scanner := bufio.NewScanner(process.Output())
		scanner.Buffer(make([]byte, 64<<10), 4<<20)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- lineResult{line: line}:
			case <-ctx.Done():
				return
			}
		}
		lines <- lineResult{err: scanner.Err()}
	}()

	writeRPC := func(id, method string, params map[string]any) error {
		data, marshalErr := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		if marshalErr != nil {
			return marshalErr
		}
		data = append(data, '\n')
		_, writeErr := process.Input().Write(data)
		return writeErr
	}

	created := false
	submitted := false
	result := PromptResult{}
	var streamed strings.Builder
	for {
		select {
		case <-ctx.Done():
			return PromptResult{}, ctx.Err()
		case item := <-lines:
			if item.err != nil {
				return PromptResult{}, fmt.Errorf("Hermes 会话结束: %w", item.err)
			}
			if len(item.line) == 0 {
				return PromptResult{}, errors.New("Hermes 会话意外结束")
			}
			var frame rpcFrame
			if err := json.Unmarshal(item.line, &frame); err != nil {
				continue
			}
			if frame.Error != nil {
				return PromptResult{}, fmt.Errorf("Hermes: %s", frame.Error.Message)
			}
			if eventType(frame) == "gateway.ready" && !created {
				created = true
				if err := writeRPC("1", "session.create", map[string]any{"client": "easy-stock", "cwd": r.workDir}); err != nil {
					return PromptResult{}, err
				}
				continue
			}
			if frame.ID == "1" && frame.Result != nil && !submitted {
				result.SessionID = stringValue(frame.Result["session_id"])
				result.StoredSessionID = firstNonEmpty(stringValue(frame.Result["stored_session_id"]), result.SessionID)
				if result.SessionID == "" {
					return PromptResult{}, errors.New("Hermes 未返回会话 ID")
				}
				submitted = true
				if err := writeRPC("2", "prompt.submit", map[string]any{"session_id": result.SessionID, "text": prompt}); err != nil {
					return PromptResult{}, err
				}
				continue
			}
			switch eventType(frame) {
			case "message.delta":
				streamed.WriteString(firstNonEmpty(eventText(frame, "delta"), eventText(frame, "text")))
			case "message.complete":
				result.Content = strings.TrimSpace(firstNonEmpty(eventText(frame, "content"), eventText(frame, "text"), streamed.String()))
				if result.Content == "" {
					return PromptResult{}, errors.New("Hermes 没有返回有效内容")
				}
				return result, nil
			case "message.error", "session.error", "run.error":
				return PromptResult{}, errors.New(firstNonEmpty(eventText(frame, "message"), "Hermes 执行失败"))
			}
		}
	}
}

type commandProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	once   sync.Once
}

func (p *commandProcess) Input() io.WriteCloser { return p.stdin }
func (p *commandProcess) Output() io.ReadCloser { return p.stdout }
func (p *commandProcess) Errors() io.ReadCloser { return p.stderr }
func (p *commandProcess) Wait() error           { return p.cmd.Wait() }
func (p *commandProcess) Stop() error {
	var result error
	p.once.Do(func() {
		_ = p.stdin.Close()
		if p.cmd.Process != nil {
			result = p.cmd.Process.Kill()
		}
	})
	return result
}

type rpcFrame struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
	Result map[string]any `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func eventType(frame rpcFrame) string {
	if frame.Method == "event" {
		return stringValue(frame.Params["type"])
	}
	return frame.Method
}

func eventText(frame rpcFrame, key string) string {
	if value := stringValue(frame.Params[key]); value != "" {
		return value
	}
	payload, _ := frame.Params["payload"].(map[string]any)
	return stringValue(payload[key])
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func normalizeLLM(cfg appsettings.LLM) appsettings.LLM {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "openai"
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL(provider)
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultModel(provider)
	}
	apiMode := strings.TrimSpace(cfg.APIMode)
	if apiMode == "" {
		if provider == "anthropic" {
			apiMode = "anthropic_messages"
		} else {
			apiMode = "chat_completions"
		}
	}
	if apiMode == "responses" {
		apiMode = "codex_responses"
	}
	return appsettings.LLM{Provider: provider, BaseURL: baseURL, Model: model, APIMode: apiMode}
}

func renderConfig(cfg appsettings.LLM, workDir string) string {
	providerName := cfg.Provider
	if providerName == "custom" {
		providerName = "自定义模型"
	}
	var text strings.Builder
	fmt.Fprintf(&text, "model:\n  default: %s\n  provider: %s\n  base_url: %s\n  api_mode: %s\n\n", yamlString(cfg.Model), providerSlug, yamlString(cfg.BaseURL), yamlString(cfg.APIMode))
	fmt.Fprintf(&text, "providers:\n  %s:\n    name: %s\n    api: %s\n    key_env: %s\n    default_model: %s\n    transport: %s\n\n", providerSlug, yamlString(providerName), yamlString(cfg.BaseURL), modelAPIKeyEnvName, yamlString(cfg.Model), yamlString(transportForAPIMode(cfg.APIMode)))
	text.WriteString("agent:\n  reasoning_effort: medium\n  system_prompt: |-\n")
	for _, line := range strings.Split(systemPrompt, "\n") {
		fmt.Fprintf(&text, "    %s\n", line)
	}
	if strings.TrimSpace(workDir) != "" {
		fmt.Fprintf(&text, "\nterminal:\n  cwd: %s\n", yamlString(workDir))
	}
	text.WriteString("\nmemory:\n  memory_enabled: true\n  user_profile_enabled: false\n  nudge_interval: 0\nskills:\n  creation_nudge_interval: 0\ncurator:\n  enabled: false\nsecurity:\n  allow_lazy_installs: false\n")
	return text.String()
}

func transportForAPIMode(apiMode string) string {
	switch apiMode {
	case "anthropic_messages":
		return "anthropic_messages"
	case "codex_responses", "responses":
		return "codex_responses"
	default:
		return "chat_completions"
	}
}

func defaultBaseURL(provider string) string {
	switch provider {
	case "deepseek":
		return "https://api.deepseek.com"
	case "qwen":
		return "https://dashscope.aliyuncs.com/compatible-mode/v1"
	case "moonshot":
		return "https://api.moonshot.cn/v1"
	case "anthropic":
		return "https://api.anthropic.com"
	default:
		return "https://api.openai.com/v1"
	}
}

func defaultModel(provider string) string {
	switch provider {
	case "deepseek":
		return "deepseek-chat"
	case "qwen":
		return "qwen-plus"
	case "moonshot":
		return "moonshot-v1-8k"
	case "anthropic":
		return "claude-3-5-haiku-latest"
	case "custom":
		return ""
	default:
		return "gpt-4o-mini"
	}
}

func runtimePython(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "venv", "Scripts", "python.exe")
	}
	return filepath.Join(root, "venv", "bin", "python")
}

func hermesEnvironment(base []string, home, workDir, binDir string) []string {
	values := append([]string(nil), base...)
	values = setEnv(values, "HERMES_HOME", home)
	values = setEnv(values, "HERMES_SESSION_SOURCE", "easy-stock")
	values = setEnv(values, "PYTHONNOUSERSITE", "1")
	values = setEnv(values, "PYTHONUNBUFFERED", "1")
	values = setEnv(values, "NO_COLOR", "1")
	if strings.TrimSpace(home) != "" {
		values = setEnv(values, "AGENT_BROWSER_PROFILE", filepath.Join(home, "browser-profile"))
	}
	if strings.TrimSpace(workDir) != "" {
		values = setEnv(values, "TERMINAL_CWD", workDir)
	}
	pathValue := os.Getenv("PATH")
	if pathValue == "" {
		pathValue = filepath.Dir(binDir)
	}
	pathEntries := []string{binDir}
	if wrapperDir := strings.TrimSpace(environmentValue(base, "A_STOCK_AGENT_BROWSER_WRAPPER_DIR")); wrapperDir != "" {
		if info, err := os.Stat(wrapperDir); err == nil && info.IsDir() {
			pathEntries = append(pathEntries, wrapperDir)
		}
	}
	if strings.TrimSpace(workDir) != "" {
		workspaceBin := filepath.Join(workDir, "node_modules", ".bin")
		if info, err := os.Stat(workspaceBin); err == nil && info.IsDir() {
			pathEntries = append(pathEntries, workspaceBin)
		}
	}
	pathEntries = append(pathEntries, pathValue)
	values = setEnv(values, "PATH", strings.Join(pathEntries, string(os.PathListSeparator)))
	return values
}

func environmentValue(values []string, key string) string {
	prefix := key + "="
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return ""
}

func setEnv(values []string, key, value string) []string {
	prefix := key + "="
	for index := range values {
		if strings.HasPrefix(values[index], prefix) {
			values[index] = prefix + value
			return values
		}
	}
	return append(values, prefix+value)
}

func unsetEnv(values []string, key string) []string {
	prefix := key + "="
	result := values[:0]
	for _, value := range values {
		if !strings.HasPrefix(value, prefix) {
			result = append(result, value)
		}
	}
	return result
}

func firstExistingDirectory(values ...string) string {
	for _, value := range values {
		if info, err := os.Stat(value); err == nil && info.IsDir() {
			return value
		}
	}
	return ""
}

func yamlString(value string) string { return strconv.Quote(value) }

func readEnvValue(path, key string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取 Hermes 环境配置: %w", err)
	}
	prefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), prefix))
		if unquoted, unquoteErr := strconv.Unquote(value); unquoteErr == nil {
			return unquoted, nil
		}
		return value, nil
	}
	return "", nil
}

func writeEnvValue(path, key, value string) error {
	lines := []string{}
	if data, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取 Hermes 环境配置: %w", err)
	}
	prefix := key + "="
	replacement := prefix + strconv.Quote(value)
	updated := false
	result := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			if !updated {
				result = append(result, replacement)
				updated = true
			}
			continue
		}
		if line != "" {
			result = append(result, line)
		}
	}
	if !updated {
		result = append(result, replacement)
	}
	if err := writeSecureFile(path, []byte(strings.Join(result, "\n")+"\n")); err != nil {
		return fmt.Errorf("写入 Hermes 环境配置: %w", err)
	}
	return nil
}

func writeSecureFile(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".hermes-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (r *Runtime) runtimeVersion() string {
	if strings.TrimSpace(r.runtimeRoot) == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(r.runtimeRoot, "runtime-manifest.json"))
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return ""
	}
	return strings.TrimSpace(manifest.Version)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
