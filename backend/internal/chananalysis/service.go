package chananalysis

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
	// defaultTimeout 覆盖取数 + 缠论计算 + 回测的完整预算。回测需逐根重算
	// CZSC，800 根日线实测约 5s；取数受上游行情接口波动影响，留足余量。
	defaultTimeout = 60 * time.Second
	// catalogTimeout 用于信号目录查询。24 万字符量级的 JSON 序列化很快，但
	// 冷启动导入 czsc 需要几秒。
	catalogTimeout = 60 * time.Second
	// defaultLimit 是默认拉取的 K 线数量，约三年日线。
	defaultLimit = 800
	// maxLimit 是允许请求的最大 K 线数量，避免单次请求拖垮服务。
	maxLimit = 2000
	// cacheTTL 是同一标的同一参数的结果缓存时长。缠论结构随最新价格变化，
	// 因此缓存窗口取分钟级，兼顾连续刷新与上游压力。
	cacheTTL = 90 * time.Second
	// catalogCacheTTL 是信号目录的缓存时长。目录来自本地 czsc 安装，几乎不变。
	catalogCacheTTL = 30 * time.Minute
	// maxCacheEntries 限制缓存条目数，避免长时间运行后内存无界增长。
	maxCacheEntries = 64
	// stderrTailSize 是失败时回传给上层的 Python 错误输出上限。
	stderrTailSize = 8 << 10
)

// Config 是分析服务的构造参数。
type Config struct {
	// PythonPath 是运行 analyze.py 的解释器；为空时按候选路径自动探测。
	PythonPath string
	// ScriptPath 是 analyze.py 的绝对路径；为空时按候选路径自动探测。
	ScriptPath string
	// WorkDir 是脚本的工作目录，默认取脚本所在目录。
	WorkDir string
	// BackendURL 是脚本取 K 线时访问的后端地址。脚本默认指向 20081，但服务
	// 可能监听在其他端口（测试、多实例），因此必须显式传入自身地址。
	BackendURL string
	// Token 是脚本回调后端接口时使用的访问令牌。后端启用鉴权时必填，否则
	// 脚本取数会被 401 拒绝。
	Token string
	// Timeout 覆盖默认超时，仅用于测试或特殊部署。
	Timeout time.Duration
	// DisableCache 用于测试时绕过缓存。
	DisableCache bool
}

// Service 封装对本地 czsc 分析脚本的调用。
type Service struct {
	pythonPath string
	scriptPath string
	workDir    string
	backendURL string
	// token 是回调后端接口时使用的访问令牌。后端启用鉴权时，脚本的取数请求
	// 必须携带同一个令牌，否则会被 401 拒绝。
	token   string
	timeout time.Duration

	cacheMu          sync.Mutex
	cache            map[string]cacheEntry
	catalog          []SignalMeta
	catalogExpiresAt time.Time
	cacheOff         bool

	// runMu 串行化子进程调用。czsc 计算是 CPU 密集的，并发调用只会互相抢占
	// CPU 并放大内存峰值，因此显式排队。
	runMu sync.Mutex
}

type cacheEntry struct {
	result    Result
	expiresAt time.Time
}

// Request 描述一次分析请求。
type Request struct {
	Symbol string
	// Period 是后端 K 线周期（day/week/month/60min/...），默认 day。
	Period string
	// Frequency 是 czsc 频率名；为空时脚本按 Period 推导。
	Frequency string
	// Limit 是拉取的 K 线数量，默认 800。
	Limit int
	// Signals 覆盖默认信号列表；为 nil 时使用脚本内置默认信号。
	Signals []SignalConfig
	// BacktestSignal 指定要回测的信号名；为空表示不回测。
	BacktestSignal string
	// BacktestParams 是回测信号的参数。
	BacktestParams map[string]any
	// WithChart 为 true 时生成离线缠论图。
	WithChart bool
	// ChartOutDir 是缠论图的输出目录；为空时 HTML 内联进 JSON。
	ChartOutDir string
	// InlineChart 为 true 时即使指定了 ChartOutDir 也把 HTML 一并返回。
	InlineChart bool
}

// SignalConfig 描述一个待计算的自定义信号。
type SignalConfig struct {
	Name   string         `json:"name"`
	Label  string         `json:"label,omitempty"`
	Params map[string]any `json:"params,omitempty"`
}

// NewService 构造分析服务。当 Python 解释器或脚本缺失时，服务仍可构造，
// 但 Available() 返回 false，调用 Query 会得到可读的错误。
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

// SetBackendURL 设置脚本取数时访问的后端地址。后端监听地址在启动时才能确定，
// 因此允许在构造后注入。为空时脚本回退到自身的默认地址。
func (s *Service) SetBackendURL(url string) {
	if s == nil {
		return
	}
	s.backendURL = strings.TrimRight(strings.TrimSpace(url), "/")
}

// SetToken 设置回调后端接口时使用的访问令牌。后端启用鉴权时必须在构造后注入，
// 否则脚本取数会被 401 拒绝。
func (s *Service) SetToken(token string) {
	if s == nil {
		return
	}
	s.token = strings.TrimSpace(token)
}

// Available 报告 Python 解释器与脚本是否都已就位。
func (s *Service) Available() bool {
	return s != nil && s.UnavailableReason() == ""
}

// UnavailableReason 返回不可用的具体原因，供接口回传明确提示。
func (s *Service) UnavailableReason() string {
	if s == nil {
		return "缠论分析服务未初始化"
	}
	if s.pythonPath == "" || !fileExists(s.pythonPath) {
		// 解释器缺失优先报告：这是最常见的部署问题（venv 必须与 czsc-service 同级）。
		return "未找到缠论分析所需的 Python 解释器；" +
			"请在 " + defaultEnvRoot() + " 下创建虚拟环境 a-stock-data-venv 并安装 czsc" +
			"（解释器应为 " + filepath.Join(defaultEnvRoot(), "a-stock-data-venv", venvRelativePython()[0]) + "），" +
			"或设置环境变量 A_STOCK_CZSC_PYTHON 指向解释器"
	}
	if s.scriptPath == "" || !fileExists(s.scriptPath) {
		return "未找到缠论分析脚本 analyze.py；" +
			"请把脚本放到 " + filepath.Join(defaultEnvRoot(), "czsc-service") + "，" +
			"或设置环境变量 A_STOCK_CZSC_SCRIPT 指向脚本路径"
	}
	return ""
}

// defaultEnvRoot 返回用于提示信息的约定环境根目录。
//
// 只给用户展示 K:\easy-stock-env 这一类**稳定的约定路径**，不展示 cwd/exe 相对探测出来的
// 中间候选——那些路径对用户没有意义，还会因为编译机与运行机不同而误导。
func defaultEnvRoot() string {
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_ENV_ROOT")); configured != "" {
		return filepath.Clean(configured)
	}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_ROOT")); configured != "" {
		cleaned := filepath.Clean(configured)
		if strings.EqualFold(filepath.Base(cleaned), "czsc-service") {
			return filepath.Dir(cleaned)
		}
		return cleaned
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		// 优先本机约定的 K 盘；K 盘不可用时退回用户主目录下的同名目录。
		kDrive := filepath.Join("K:"+string(filepath.Separator), "easy-stock-env")
		if info, statErr := os.Stat(kDrive); statErr == nil && info.IsDir() {
			return kDrive
		}
		return filepath.Join(home, "easy-stock-env")
	}
	return filepath.Join("K:"+string(filepath.Separator), "easy-stock-env")
}

// ScriptPath 返回脚本路径，便于健康检查与日志。
func (s *Service) ScriptPath() string {
	if s == nil {
		return ""
	}
	return s.scriptPath
}

// PythonPath 返回解释器路径，便于健康检查与日志。
func (s *Service) PythonPath() string {
	if s == nil {
		return ""
	}
	return s.pythonPath
}

// Query 执行一次缠论分析。ctx 取消会终止子进程。
func (s *Service) Query(ctx context.Context, req Request) (Result, error) {
	if s == nil {
		return Result{}, errors.New("缠论分析服务未初始化")
	}
	if reason := s.UnavailableReason(); reason != "" {
		return Result{}, errors.New(reason)
	}
	symbol := strings.TrimSpace(req.Symbol)
	if symbol == "" {
		return Result{}, errors.New("缺少股票代码")
	}
	req.Symbol = symbol
	if strings.TrimSpace(req.Period) == "" {
		req.Period = "day"
	}
	if req.Limit <= 0 {
		req.Limit = defaultLimit
	}
	if req.Limit > maxLimit {
		req.Limit = maxLimit
	}

	cacheKey := ""
	if !s.cacheOff {
		cacheKey = cacheKeyFor(req)
		if cached, ok := s.lookup(cacheKey); ok {
			return cached, nil
		}
	}

	result, err := s.run(ctx, req)
	if err != nil {
		return Result{}, err
	}
	if cacheKey != "" {
		s.store(cacheKey, result)
	}
	return result, nil
}

// Catalog 返回 czsc 全量信号目录（246 条元数据），供前端配置界面使用。
// 目录来自本地安装，几乎不变，因此缓存时间远长于分析结果。
func (s *Service) Catalog(ctx context.Context) ([]SignalMeta, error) {
	if s == nil {
		return nil, errors.New("缠论分析服务未初始化")
	}
	if reason := s.UnavailableReason(); reason != "" {
		return nil, errors.New(reason)
	}
	if !s.cacheOff {
		s.cacheMu.Lock()
		if len(s.catalog) > 0 && time.Now().Before(s.catalogExpiresAt) {
			cached := s.catalog
			s.cacheMu.Unlock()
			return cached, nil
		}
		s.cacheMu.Unlock()
	}

	catalogCtx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	// argparse 把 --symbol 声明为必填，因此列目录时也要给一个占位值；脚本在
	// --list-signals 分支会立即返回，不会真正取数。
	output, err := s.exec(catalogCtx, []string{"--symbol", "000001", "--list-signals"})
	if err != nil {
		return nil, err
	}
	metas, err := parseSignalCatalog(output)
	if err != nil {
		return nil, err
	}
	if !s.cacheOff {
		s.cacheMu.Lock()
		s.catalog = metas
		s.catalogExpiresAt = time.Now().Add(catalogCacheTTL)
		s.cacheMu.Unlock()
	}
	return metas, nil
}

// parseSignalCatalog 解析 list_all_signals() 的返回。实测 czsc 直接返回列表，
// 元素字段固定为 name / param_template / category / namespace。
func parseSignalCatalog(output []byte) ([]SignalMeta, error) {
	var items []map[string]any
	if err := json.Unmarshal(output, &items); err == nil {
		if metas := convertCatalog(items); len(metas) > 0 {
			return metas, nil
		}
	}
	// 兼容被包进 {"ok": true, "signals": [...]} 的形态，以及失败信封。
	var envelope struct {
		OK      *bool            `json:"ok"`
		Error   string           `json:"error"`
		Signals []map[string]any `json:"signals"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, fmt.Errorf("解析缠论信号目录失败: %w", err)
	}
	if envelope.OK != nil && !*envelope.OK {
		return nil, errors.New(firstNonEmpty(envelope.Error, "缠论信号目录读取失败"))
	}
	if metas := convertCatalog(envelope.Signals); len(metas) > 0 {
		return metas, nil
	}
	return nil, errors.New("缠论信号目录为空")
}

func convertCatalog(items []map[string]any) []SignalMeta {
	metas := make([]SignalMeta, 0, len(items))
	for _, item := range items {
		name := firstNonEmpty(stringField(item, "name"), stringField(item, "signal_name"), stringField(item, "key"))
		if name == "" {
			continue
		}
		template := firstNonEmpty(stringField(item, "param_template"), stringField(item, "params"), stringField(item, "param"))
		meta := SignalMeta{
			Name:          name,
			Namespace:     firstNonEmpty(stringField(item, "namespace"), namespaceOf(name)),
			Category:      firstNonEmpty(stringField(item, "category"), stringField(item, "class")),
			ParamTemplate: template,
			ParamKeys:     parseParamKeys(template),
		}
		metas = append(metas, meta)
	}
	return metas
}

// parseParamKeys 从 `{freq}_D{di}N{n}M{m}TH{th}_ADTMV230603` 这类模板中提取
// 参数占位符。freq 由调用方统一处理，因此单独剔除。
func parseParamKeys(template string) []string {
	if strings.TrimSpace(template) == "" {
		return nil
	}
	keys := make([]string, 0, 4)
	seen := map[string]bool{}
	for {
		start := strings.Index(template, "{")
		if start < 0 {
			break
		}
		end := strings.Index(template[start:], "}")
		if end < 0 {
			break
		}
		key := strings.TrimSpace(template[start+1 : start+end])
		template = template[start+end+1:]
		if key == "" || key == "freq" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

// run 调用脚本并解析 JSON 输出。
func (s *Service) run(ctx context.Context, req Request) (Result, error) {
	args := []string{
		"--symbol", req.Symbol,
		"--period", req.Period,
		"--limit", fmt.Sprint(req.Limit),
	}
	// 脚本默认指向 127.0.0.1:20081，多实例或测试端口下必须显式覆盖。
	if s.backendURL != "" {
		args = append(args, "--backend", s.backendURL)
	}
	// 后端启用鉴权时，脚本取数同样需要令牌，否则会被 401 拒绝。
	if s.token != "" {
		args = append(args, "--token", s.token)
	}
	if freq := strings.TrimSpace(req.Frequency); freq != "" {
		args = append(args, "--freq", freq)
	}
	if len(req.Signals) > 0 {
		encoded, err := json.Marshal(req.Signals)
		if err != nil {
			return Result{}, fmt.Errorf("编码缠论信号参数失败: %w", err)
		}
		args = append(args, "--signals", string(encoded))
	}
	if strings.TrimSpace(req.BacktestSignal) != "" {
		args = append(args, "--backtest", req.BacktestSignal)
		if len(req.BacktestParams) > 0 {
			encoded, err := json.Marshal(req.BacktestParams)
			if err != nil {
				return Result{}, fmt.Errorf("编码回测参数失败: %w", err)
			}
			args = append(args, "--backtest-params", string(encoded))
		}
	}
	// 未指定输出目录时，HTML 会内联进 JSON。
	if req.WithChart {
		args = append(args, "--chart")
		if dir := strings.TrimSpace(req.ChartOutDir); dir != "" {
			if err := validateChartOutDir(dir); err != nil {
				return Result{}, err
			}
			args = append(args, "--chart-out-dir", dir)
		}
	}

	output, err := s.exec(ctx, args)
	if err != nil {
		return Result{}, err
	}
	return decodeResult(output, req.Symbol)
}

// decodeResult 把脚本输出解码为 Result。分离出来便于单元测试。
func decodeResult(output []byte, requestedSymbol string) (Result, error) {
	var envelope struct {
		OK          *bool      `json:"ok"`
		Error       string     `json:"error"`
		Symbol      string     `json:"symbol"`
		Freq        string     `json:"freq"`
		Source      string     `json:"source"`
		GeneratedAt string     `json:"generated_at"`
		ElapsedMS   int        `json:"elapsed_ms"`
		Range       BarRange   `json:"range"`
		Structure   *Structure `json:"structure"`
		Summary     *Summary   `json:"summary"`
		Signals     []Signal   `json:"signals"`
		Backtest    *Backtest  `json:"backtest"`
		Chart       *Chart     `json:"chart"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return Result{}, fmt.Errorf("解析缠论分析结果失败: %w", err)
	}
	if envelope.OK == nil {
		return Result{}, errors.New("缠论分析返回的结果缺少 ok 字段")
	}
	if !*envelope.OK {
		return Result{}, errors.New(firstNonEmpty(envelope.Error, "缠论分析失败"))
	}

	result := Result{
		Symbol:    firstNonEmpty(envelope.Symbol, requestedSymbol),
		Freq:      envelope.Freq,
		Source:    envelope.Source,
		ElapsedMS: envelope.ElapsedMS,
		Range:     envelope.Range,
		Signals:   envelope.Signals,
		Backtest:  envelope.Backtest,
		Chart:     envelope.Chart,
	}
	if envelope.Structure != nil {
		result.Structure = *envelope.Structure
	}
	if envelope.Summary != nil {
		result.Summary = *envelope.Summary
	}
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(envelope.GeneratedAt)); err == nil {
		result.GeneratedAt = parsed
	}
	if result.GeneratedAt.IsZero() {
		result.GeneratedAt = time.Now()
	}
	return result, nil
}

// exec 是子进程调用的唯一入口，负责超时、环境注入与错误整形。
func (s *Service) exec(ctx context.Context, args []string) ([]byte, error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	fullArgs := append([]string{s.scriptPath}, args...)
	cmd := exec.CommandContext(runCtx, s.pythonPath, fullArgs...)
	if s.workDir != "" {
		cmd.Dir = s.workDir
	}
	cmd.Env = processEnvironment()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("缠论分析超时（%s），请缩小K线数量或稍后重试", s.timeout)
	}
	if parentErr := ctx.Err(); parentErr != nil {
		return nil, parentErr
	}

	output := bytes.TrimSpace(stdout.Bytes())
	// 脚本约定：无论成功失败都输出 JSON，失败时 ok=false 且退出码非 0。因此只要
	// 还有可解析的 JSON，就交给上层按业务错误处理，而不是抛出裸的退出码。
	if len(output) > 0 && json.Valid(output) {
		return output, nil
	}
	if runErr != nil {
		if diagnostic := strings.TrimSpace(stderr.String()); diagnostic != "" {
			return nil, fmt.Errorf("缠论分析进程失败: %s", tail(diagnostic, stderrTailSize))
		}
		return nil, fmt.Errorf("缠论分析进程失败: %w", runErr)
	}
	if len(output) == 0 {
		return nil, errors.New("缠论分析未返回任何结果")
	}
	return nil, fmt.Errorf("缠论分析返回了非 JSON 结果: %s", tail(string(output), stderrTailSize))
}

// processEnvironment 在继承父进程环境的基础上注入 czsc 运行所需的变量。
func processEnvironment() []string {
	values := append([]string(nil), os.Environ()...)
	// polars 在部分虚拟化主机上会报告构建特性表里不存在的 CPU 标记（如 sse3），
	// 触发 "unknown feature flag" 致命错误。该检查对纯数值计算无实际意义。
	values = setEnv(values, "POLARS_SKIP_CPU_CHECK", "1")
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

func (s *Service) lookup(key string) (Result, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache[key]
	if !ok {
		return Result{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(s.cache, key)
		return Result{}, false
	}
	return entry.result, true
}

func (s *Service) store(key string, result Result) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if len(s.cache) >= maxCacheEntries {
		now := time.Now()
		for existing, entry := range s.cache {
			if now.After(entry.expiresAt) {
				delete(s.cache, existing)
			}
		}
		// 仍然超限时按最早过期顺序清理，保证缓存有界。
		for len(s.cache) >= maxCacheEntries {
			oldestKey := ""
			var oldest time.Time
			for existing, entry := range s.cache {
				if oldestKey == "" || entry.expiresAt.Before(oldest) {
					oldestKey, oldest = existing, entry.expiresAt
				}
			}
			if oldestKey == "" {
				break
			}
			delete(s.cache, oldestKey)
		}
	}
	s.cache[key] = cacheEntry{result: result, expiresAt: time.Now().Add(cacheTTL)}
}

func cacheKeyFor(req Request) string {
	signals, _ := json.Marshal(req.Signals)
	backtestParams, _ := json.Marshal(req.BacktestParams)
	return strings.Join([]string{
		req.Symbol,
		req.Period,
		req.Frequency,
		fmt.Sprint(req.Limit),
		string(signals),
		req.BacktestSignal,
		string(backtestParams),
		fmt.Sprint(req.WithChart),
		req.ChartOutDir,
	}, "|")
}

// discoverPython 在常见位置查找 a-stock-data 虚拟环境的解释器。
// discoverPython 查找运行 analyze.py 的解释器。
//
// ⚠️ venv 与 czsc-service 是**同级目录**（都在 easy-stock-env 下），不是父子关系：
//
//	K:\easy-stock-env\a-stock-data-venv\Scripts\python.exe
//	K:\easy-stock-env\czsc-service\analyze.py
//
// 因此这里同时尝试 service 目录内部（自包含部署）与其上一级（本机同级布局）。
func discoverPython() string {
	candidates := []string{}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_PYTHON")); configured != "" {
		candidates = append(candidates, configured)
	}
	roots := envRoots()
	roots = append(roots, serviceRoots()...)
	for _, root := range roots {
		for _, relative := range venvRelativePython() {
			candidates = append(candidates, filepath.Join(root, relative))
		}
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return filepath.Clean(candidate)
		}
	}
	return ""
}

// discoverScript 查找 analyze.py。
func discoverScript() string {
	candidates := []string{}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_SCRIPT")); configured != "" {
		candidates = append(candidates, configured)
	}
	for _, root := range serviceRoots() {
		candidates = append(candidates, filepath.Join(root, "analyze.py"))
	}
	if executable, err := os.Executable(); err == nil {
		base := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(base, "czsc-service", "analyze.py"),
			filepath.Join(base, "..", "czsc-service", "analyze.py"),
			filepath.Join(base, "..", "..", "czsc-service", "analyze.py"),
		)
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return filepath.Clean(candidate)
		}
	}
	return ""
}

// envRoots 返回 easy-stock-env 目录的候选路径。
//
// 本机约定这两个目录是同级的：
//
//	K:\easy-stock-env\czsc-service\analyze.py
//	K:\easy-stock-env\a-stock-data-venv\Scripts\python.exe
//
// 因此脚本与解释器都应从 envRoots 出发定位，避免两处各写一套候选路径而漂移。
func envRoots() []string {
	roots := []string{}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_ENV_ROOT")); configured != "" {
		roots = append(roots, configured)
	}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_ROOT")); configured != "" {
		// 允许把 A_STOCK_CZSC_ROOT 指向 env 根或 service 目录，两种都兼容。
		roots = append(roots, configured, filepath.Dir(configured))
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
			filepath.Join(base, "..", "..", "..", "easy-stock-env"),
		)
	}
	// 开发与打包部署时服务脚本都放在 easy-stock-env 下，K 盘是约定盘符。
	roots = append(roots, filepath.Join("K:"+string(filepath.Separator), "easy-stock-env"))
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, "easy-stock-env"))
	}
	return roots
}

// serviceRoots 返回 czsc-service 目录的候选路径。
func serviceRoots() []string {
	roots := []string{}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_ROOT")); configured != "" {
		roots = append(roots, configured, filepath.Join(configured, "czsc-service"))
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots,
			filepath.Join(cwd, "czsc-service"),
			filepath.Join(cwd, "..", "czsc-service"),
			filepath.Join(cwd, "..", "easy-stock-env", "czsc-service"),
			filepath.Join(cwd, "..", "..", "easy-stock-env", "czsc-service"),
		)
	}
	for _, envRoot := range envRoots() {
		roots = append(roots, filepath.Join(envRoot, "czsc-service"))
	}
	return roots
}

// allowedChartOutRoots 返回图表 HTML 允许写入的根目录。
// chart 输出目录会被传给 Python 子进程写文件，若不限制路径，
// 攻击者可借未鉴权的本机 API 把文件写到任意位置（如启动目录）。
func allowedChartOutRoots() []string {
	roots := []string{}
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_CZSC_OUT_ROOTS")); configured != "" {
		for _, item := range filepath.SplitList(configured) {
			if item = strings.TrimSpace(item); item != "" {
				roots = append(roots, filepath.Clean(item))
			}
		}
	}
	for _, root := range serviceRoots() {
		roots = append(roots, filepath.Join(root, "out"))
	}
	roots = append(roots, os.TempDir())
	return roots
}

// validateChartOutDir 校验输出目录必须位于白名单根之下且不含路径穿越。
func validateChartOutDir(dir string) error {
	cleaned := filepath.Clean(strings.TrimSpace(dir))
	if !filepath.IsAbs(cleaned) {
		return errors.New("图表输出目录必须是绝对路径")
	}
	if strings.Contains(cleaned, "..") {
		return errors.New("图表输出目录不允许包含路径穿越")
	}
	for _, root := range allowedChartOutRoots() {
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(root, cleaned)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
			return nil
		}
	}
	return errors.New("图表输出目录不在允许范围内，请使用 czsc-service/out 目录或系统临时目录")
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

// stringField 从自由字典中取出字符串字段。
func stringField(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
}

// namespaceOf 从信号名推断命名空间。czsc 信号名形如 cxt_bi_status_V230101，
// 前缀即命名空间。
func namespaceOf(name string) string {
	if index := strings.Index(name, "_"); index > 0 {
		return name[:index]
	}
	return ""
}
