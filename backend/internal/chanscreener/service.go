package chanscreener

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// defaultTimeout 覆盖批量选股的完整预算：拉数（并发）+ 逐只缠论计算。
	// 200 只上限实测计算侧只需秒级，主要耗时在上游K线接口，留足余量。
	defaultTimeout = 150 * time.Second
	// analyzeTimeout 是单只分析的预算。
	analyzeTimeout = 60 * time.Second
	// defaultLimit 是单只默认拉取的K线数量。
	defaultLimit = 500
	// maxLimit 是允许请求的最大K线数量。
	maxLimit = 1500
	// maxScreenSymbols 是单次选股的股票数上限。脚本侧同样限制，双保险。
	maxScreenSymbols = 200
	// cacheTTL 是分析与选股结果的缓存时长。
	cacheTTL = 90 * time.Second
	// maxCacheEntries 限制缓存条目数。
	maxCacheEntries = 32
	// stderrTailSize 是失败时回传的 Python 错误输出上限。
	stderrTailSize = 8 << 10
)

// Config 是服务构造参数。
type Config struct {
	// PythonPath 是运行脚本的解释器；为空时自动探测。
	PythonPath string
	// ScriptPath 是 chanpy_service.py 的绝对路径；为空时自动探测。
	ScriptPath string
	// WorkDir 是脚本工作目录，默认取脚本所在目录。
	WorkDir string
	// BackendURL 是脚本回调后端拉K线的地址。
	BackendURL string
	// Token 是回调后端接口时使用的访问令牌。
	Token string
	// Timeout 覆盖默认超时，仅测试或特殊部署使用。
	Timeout time.Duration
	// DisableCache 用于测试时绕过缓存。
	DisableCache bool
}

// Service 封装对 chan.py 服务脚本的调用。
type Service struct {
	pythonPath string
	scriptPath string
	workDir    string
	backendURL string
	token      string
	timeout    time.Duration

	cacheMu  sync.Mutex
	cache    map[string]cacheEntry
	cacheOff bool

	// runMu 串行化子进程调用：chan.py 是 CPU 密集计算，并发只会互相抢占。
	runMu sync.Mutex
}

type cacheEntry struct {
	payload   any
	expiresAt time.Time
}

// AnalyzeRequest 描述一次单股分析。
type AnalyzeRequest struct {
	Symbol string
	Period string
	Limit  int
	// Conf 覆盖 CChanConfig；nil 时用脚本默认参数。
	Conf map[string]any
	// Names 提供代码到名称的映射，脚本会回填到结果里。
	Names  map[string]string
	Autype string
}

// ScreenRequest 描述一次批量选股。
type ScreenRequest struct {
	Symbols []string
	// Names 提供代码到名称的映射，用于结果展示。
	Names  map[string]string
	Period string
	Limit  int
	// Filters 是选股条件，键与脚本约定一致：
	// side(buy/sell/any)、bs_types([]string)、zs_state、bi_direction、
	// bsp_recent_bars、min_score、require_sure。
	Filters map[string]any
	Conf    map[string]any
	// Workers 是脚本并发拉数的线程数，默认 8。
	Workers int
	Autype  string
}

// NewService 构造服务。解释器或脚本缺失时服务仍可构造，Available() 返回 false。
func NewService(cfg Config) *Service {
	pythonPath := strings.TrimSpace(cfg.PythonPath)
	if pythonPath == "" {
		pythonPath = discoverPython()
	}
	scriptPath := strings.TrimSpace(cfg.ScriptPath)
	if scriptPath == "" {
		scriptPath = discoverScript()
	}
	workDir := strings.TrimSpace(cfg.WorkDir)
	if workDir == "" && scriptPath != "" {
		workDir = filepath.Dir(scriptPath)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Service{
		pythonPath: pythonPath,
		scriptPath: scriptPath,
		workDir:    workDir,
		backendURL: strings.TrimRight(strings.TrimSpace(cfg.BackendURL), "/"),
		token:      strings.TrimSpace(cfg.Token),
		timeout:    timeout,
		cache:      map[string]cacheEntry{},
		cacheOff:   cfg.DisableCache,
	}
}

// SetBackendURL 设置脚本取数时访问的后端地址，启动时注入。
func (s *Service) SetBackendURL(url string) {
	if s == nil {
		return
	}
	s.backendURL = strings.TrimRight(strings.TrimSpace(url), "/")
}

// SetToken 设置回调后端接口时使用的访问令牌。
func (s *Service) SetToken(token string) {
	if s == nil {
		return
	}
	s.token = strings.TrimSpace(token)
}

// Available 报告解释器与脚本是否就位。
func (s *Service) Available() bool {
	return s != nil && s.UnavailableReason() == ""
}

// UnavailableReason 返回不可用原因，供接口回传明确提示。
func (s *Service) UnavailableReason() string {
	if s == nil {
		return "缠论选股服务未初始化"
	}
	if s.pythonPath == "" || !fileExists(s.pythonPath) {
		return "未找到 chan.py 缠论引擎的 Python 解释器（需 Python ≥3.11）；" +
			"可设置环境变量 A_STOCK_CHANPY_PYTHON 指向解释器，或创建 " +
			defaultEnvRoot() + " 虚拟环境"
	}
	if s.scriptPath == "" || !fileExists(s.scriptPath) {
		return "未找到 chan.py 服务脚本 chanpy_service.py；" +
			"请放置到 " + filepath.Join(defaultEnvRoot(), "chanpy-service") +
			"，或设置环境变量 A_STOCK_CHANPY_SCRIPT 指向脚本路径"
	}
	return ""
}

// defaultEnvRoot 返回提示信息用的约定环境根目录（与 czsc-service 同级布局）。
func defaultEnvRoot() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		kDrive := filepath.Join("K:"+string(filepath.Separator), "easy-stock-env")
		if info, statErr := os.Stat(kDrive); statErr == nil && info.IsDir() {
			return kDrive
		}
		return filepath.Join(home, "easy-stock-env")
	}
	return filepath.Join("K:"+string(filepath.Separator), "easy-stock-env")
}

// ScriptPath 返回脚本路径，便于健康检查。
func (s *Service) ScriptPath() string {
	if s == nil {
		return ""
	}
	return s.scriptPath
}

// PythonPath 返回解释器路径，便于健康检查。
func (s *Service) PythonPath() string {
	if s == nil {
		return ""
	}
	return s.pythonPath
}

// Analyze 执行单股分析。
func (s *Service) Analyze(ctx context.Context, req AnalyzeRequest) (AnalyzeResult, error) {
	if s == nil {
		return AnalyzeResult{}, errors.New("缠论选股服务未初始化")
	}
	if reason := s.UnavailableReason(); reason != "" {
		return AnalyzeResult{}, errors.New(reason)
	}
	symbol := strings.TrimSpace(req.Symbol)
	if symbol == "" {
		return AnalyzeResult{}, errors.New("缺少股票代码")
	}
	req.Symbol = symbol
	req.Period = normalizePeriod(req.Period)
	req.Limit = clampLimit(req.Limit)

	cacheKey := ""
	if !s.cacheOff {
		cacheKey = "analyze|" + analyzeCacheKey(req)
		if cached, ok := s.lookup(cacheKey); ok {
			if result, ok := cached.(AnalyzeResult); ok {
				return result, nil
			}
		}
	}

	var result AnalyzeResult
	err := s.run(ctx, "analyze", req.Period, req.Limit, map[string]string{
		"--symbol": req.Symbol,
	}, req, &result)
	if err != nil {
		return AnalyzeResult{}, err
	}
	if cacheKey != "" {
		s.store(cacheKey, result)
	}
	return result, nil
}

// Screen 执行批量选股。
func (s *Service) Screen(ctx context.Context, req ScreenRequest) (ScreenResult, error) {
	if s == nil {
		return ScreenResult{}, errors.New("缠论选股服务未初始化")
	}
	if reason := s.UnavailableReason(); reason != "" {
		return ScreenResult{}, errors.New(reason)
	}
	if len(req.Symbols) == 0 {
		return ScreenResult{}, errors.New("股票池为空")
	}
	if len(req.Symbols) > maxScreenSymbols {
		return ScreenResult{}, fmt.Errorf("单次最多扫描 %d 只", maxScreenSymbols)
	}
	req.Period = normalizePeriod(req.Period)
	req.Limit = clampLimit(req.Limit)

	cacheKey := ""
	if !s.cacheOff {
		cacheKey = "screen|" + screenCacheKey(req)
		if cached, ok := s.lookup(cacheKey); ok {
			if result, ok := cached.(ScreenResult); ok {
				return result, nil
			}
		}
	}

	var result ScreenResult
	err := s.run(ctx, "screen", req.Period, req.Limit, map[string]string{
		"--symbols": strings.Join(req.Symbols, ","),
	}, req, &result)
	if err != nil {
		return ScreenResult{}, err
	}
	if cacheKey != "" {
		s.store(cacheKey, result)
	}
	return result, nil
}

// run 组装参数、执行脚本并解码 JSON 到 out。
func (s *Service) run(ctx context.Context, mode, period string, limit int,
	fixedArgs map[string]string, req any, out any) error {
	args := []string{mode, "--period", period, "--limit", fmt.Sprint(limit)}
	for name, value := range fixedArgs {
		args = append(args, name, value)
	}
	if s.backendURL != "" {
		args = append(args, "--backend", s.backendURL)
	}
	if s.token != "" {
		args = append(args, "--token", s.token)
	}
	if autype := requestAutype(req); autype != "" && autype != "qfq" {
		args = append(args, "--autype", autype)
	}
	if conf := requestConf(req); len(conf) > 0 {
		encoded, err := json.Marshal(conf)
		if err != nil {
			return fmt.Errorf("编码 chan.py 配置失败: %w", err)
		}
		args = append(args, "--conf", string(encoded))
	}
	if filters := requestFilters(req); len(filters) > 0 {
		encoded, err := json.Marshal(filters)
		if err != nil {
			return fmt.Errorf("编码选股条件失败: %w", err)
		}
		args = append(args, "--filters", string(encoded))
	}
	if names := requestNames(req); len(names) > 0 {
		encoded, err := json.Marshal(names)
		if err != nil {
			return fmt.Errorf("编码股票名称映射失败: %w", err)
		}
		args = append(args, "--names", string(encoded))
	}
	if workers := requestWorkers(req); workers > 0 {
		args = append(args, "--workers", fmt.Sprint(workers))
	}

	timeout := s.timeout
	if mode == "analyze" && timeout > analyzeTimeout {
		timeout = analyzeTimeout
	}

	output, err := s.exec(ctx, args, timeout)
	if err != nil {
		return err
	}
	return decodePayload(output, out)
}

// decodePayload 解码脚本 JSON 输出。脚本失败时输出 ok=false 的信封。
func decodePayload(output []byte, out any) error {
	var envelope struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return fmt.Errorf("解析 chan.py 结果失败: %w", err)
	}
	if envelope.OK == nil {
		return errors.New("chan.py 返回的结果缺少 ok 字段")
	}
	if !*envelope.OK {
		return errors.New(firstNonEmpty(envelope.Error, "chan.py 计算失败"))
	}
	if err := json.Unmarshal(output, out); err != nil {
		return fmt.Errorf("解析 chan.py 结果失败: %w", err)
	}
	return nil
}

// exec 是子进程调用唯一入口，负责超时、环境与错误整形。
func (s *Service) exec(ctx context.Context, args []string, timeout time.Duration) ([]byte, error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, s.pythonPath, append([]string{s.scriptPath}, args...)...)
	if s.workDir != "" {
		cmd.Dir = s.workDir
	}
	cmd.Env = processEnvironment()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("chan.py 计算超时（%s），请缩小股票池或K线数量", timeout)
	}
	if parentErr := ctx.Err(); parentErr != nil {
		return nil, parentErr
	}

	output := bytes.TrimSpace(stdout.Bytes())
	// 脚本约定：无论成败都输出 JSON；失败信封交给上层转成业务错误。
	if len(output) > 0 && json.Valid(output) {
		return output, nil
	}
	if runErr != nil {
		if diagnostic := strings.TrimSpace(stderr.String()); diagnostic != "" {
			return nil, fmt.Errorf("chan.py 进程失败: %s", tail(diagnostic, stderrTailSize))
		}
		return nil, fmt.Errorf("chan.py 进程失败: %w", runErr)
	}
	if len(output) == 0 {
		return nil, errors.New("chan.py 未返回任何结果")
	}
	return nil, fmt.Errorf("chan.py 返回了非 JSON 结果: %s", tail(string(output), stderrTailSize))
}

// processEnvironment 继承父进程环境并强制 UTF-8 输出。
func processEnvironment() []string {
	values := append([]string(nil), os.Environ()...)
	values = setEnv(values, "PYTHONUNBUFFERED", "1")
	values = setEnv(values, "PYTHONIOENCODING", "utf-8")
	values = setEnv(values, "NO_COLOR", "1")
	return values
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

func (s *Service) lookup(key string) (any, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(s.cache, key)
		return nil, false
	}
	return entry.payload, true
}

func (s *Service) store(key string, payload any) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if len(s.cache) >= maxCacheEntries {
		now := time.Now()
		for existing, entry := range s.cache {
			if now.After(entry.expiresAt) {
				delete(s.cache, existing)
			}
		}
		for existing := range s.cache {
			if len(s.cache) < maxCacheEntries {
				break
			}
			delete(s.cache, existing)
		}
	}
	s.cache[key] = cacheEntry{payload: payload, expiresAt: time.Now().Add(cacheTTL)}
}

func normalizePeriod(period string) string {
	cleaned := strings.ToLower(strings.TrimSpace(period))
	if cleaned == "" {
		return "day"
	}
	switch cleaned {
	case "daily", "d":
		return "day"
	case "weekly", "w":
		return "week"
	case "monthly":
		return "month"
	}
	return cleaned
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

// requestAutype / requestConf / requestFilters / requestNames / requestWorkers
// 从两种请求类型里取可选字段，避免 run 的参数列表爆炸。
func requestAutype(req any) string {
	switch value := req.(type) {
	case AnalyzeRequest:
		return value.Autype
	case ScreenRequest:
		return value.Autype
	}
	return ""
}

func requestConf(req any) map[string]any {
	switch value := req.(type) {
	case AnalyzeRequest:
		return value.Conf
	case ScreenRequest:
		return value.Conf
	}
	return nil
}

func requestFilters(req any) map[string]any {
	switch value := req.(type) {
	case AnalyzeRequest:
		return nil
	case ScreenRequest:
		return value.Filters
	}
	return nil
}

func requestNames(req any) map[string]string {
	switch value := req.(type) {
	case AnalyzeRequest:
		return value.Names
	case ScreenRequest:
		return value.Names
	}
	return nil
}

func requestWorkers(req any) int {
	if value, ok := req.(ScreenRequest); ok {
		return value.Workers
	}
	return 0
}

func analyzeCacheKey(req AnalyzeRequest) string {
	conf, _ := json.Marshal(req.Conf)
	names, _ := json.Marshal(req.Names)
	return strings.Join([]string{req.Symbol, req.Period, fmt.Sprint(req.Limit), string(conf), req.Autype, string(names)}, "|")
}

func screenCacheKey(req ScreenRequest) string {
	conf, _ := json.Marshal(req.Conf)
	filters, _ := json.Marshal(req.Filters)
	names, _ := json.Marshal(req.Names)
	return strings.Join([]string{
		strings.Join(req.Symbols, ","), req.Period, fmt.Sprint(req.Limit),
		string(filters), string(conf), req.Autype, fmt.Sprint(req.Workers), string(names),
	}, "|")
}

// discoverPython 查找能运行 chan.py 的解释器：环境变量优先，其次
// a-stock-data 虚拟环境（chan.py 无第三方依赖，复用该 venv 只是图方便），
// 最后是系统 python3。
func discoverPython() string {
	candidates := []string{}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CHANPY_PYTHON")); configured != "" {
		candidates = append(candidates, configured)
	}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_PYTHON")); configured != "" {
		// 与 czsc 引擎共用解释器是常见部署形态，顺带探测。
		candidates = append(candidates, configured)
	}
	roots := envRoots()
	roots = append(roots, serviceRoots()...)
	for _, root := range roots {
		for _, relative := range venvRelativePython() {
			candidates = append(candidates, filepath.Join(root, relative))
		}
	}
	for _, candidate := range []string{"python3", "python"} {
		if path, err := exec.LookPath(candidate); err == nil {
			candidates = append(candidates, path)
		}
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return filepath.Clean(candidate)
		}
	}
	return ""
}

// discoverScript 查找 chanpy_service.py。
func discoverScript() string {
	candidates := []string{}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CHANPY_SCRIPT")); configured != "" {
		candidates = append(candidates, configured)
	}
	for _, root := range serviceRoots() {
		candidates = append(candidates, filepath.Join(root, "chanpy_service.py"))
	}
	if executable, err := os.Executable(); err == nil {
		base := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(base, "chanpy-service", "chanpy_service.py"),
			filepath.Join(base, "..", "chanpy-service", "chanpy_service.py"),
			filepath.Join(base, "..", "..", "chanpy-service", "chanpy_service.py"),
		)
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return filepath.Clean(candidate)
		}
	}
	return ""
}

// envRoots 返回 easy-stock-env 目录候选（与 chananalysis 的约定一致）。
func envRoots() []string {
	roots := []string{}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_ENV_ROOT")); configured != "" {
		roots = append(roots, configured)
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots,
			filepath.Join(cwd, "easy-stock-env"),
			filepath.Join(cwd, "..", "easy-stock-env"),
			filepath.Join(cwd, "..", "..", "easy-stock-env"),
			filepath.Join(cwd, "..", "..", "..", "easy-stock-env"),
		)
	}
	if executable, err := os.Executable(); err == nil {
		base := filepath.Dir(executable)
		roots = append(roots,
			filepath.Join(base, "easy-stock-env"),
			filepath.Join(base, "..", "easy-stock-env"),
			filepath.Join(base, "..", "..", "easy-stock-env"),
		)
	}
	roots = append(roots, filepath.Join("K:"+string(filepath.Separator), "easy-stock-env"))
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, "easy-stock-env"))
	}
	return roots
}

// serviceRoots 返回 chanpy-service 目录候选。
func serviceRoots() []string {
	roots := []string{}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_ROOT")); configured != "" {
		roots = append(roots, filepath.Join(filepath.Dir(configured), "chanpy-service"))
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots,
			filepath.Join(cwd, "chanpy-service"),
			filepath.Join(cwd, "..", "chanpy-service"),
			// 仓库自带的 vendored 副本：克隆即用，无需部署到 easy-stock-env。
			filepath.Join(cwd, "integrations", "chanpy-service"),
			filepath.Join(cwd, "..", "integrations", "chanpy-service"),
			filepath.Join(cwd, "..", "easy-stock-env", "chanpy-service"),
			filepath.Join(cwd, "..", "..", "easy-stock-env", "chanpy-service"),
		)
	}
	for _, envRoot := range envRoots() {
		roots = append(roots, filepath.Join(envRoot, "chanpy-service"))
	}
	return roots
}

func venvRelativePython() []string {
	if runtime.GOOS == "windows" {
		return []string{
			filepath.Join("a-stock-data-venv", "Scripts", "python.exe"),
			filepath.Join("Scripts", "python.exe"),
		}
	}
	return []string{
		filepath.Join("a-stock-data-venv", "bin", "python"),
		filepath.Join("bin", "python"),
	}
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func tail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
