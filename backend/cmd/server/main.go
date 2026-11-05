package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/httpapi"
	"easy-stock/backend/internal/methodology"
	"easy-stock/backend/internal/runtimelog"
)

func main() {
	addr := os.Getenv("A_STOCK_ADDR")
	if addr == "" {
		addr = "127.0.0.1:20081"
	}
	reviewDBPath := os.Getenv("A_STOCK_REVIEW_DB")
	portfolioDBPath := os.Getenv("A_STOCK_PORTFOLIO_DB")
	stockResearchDBPath := os.Getenv("A_STOCK_RESEARCH_DB")
	marketEmotionDBPath := os.Getenv("A_STOCK_MARKET_EMOTION_DB")
	themeRadarDBPath := os.Getenv("A_STOCK_THEME_RADAR_DB")
	themeLibraryDBPath := os.Getenv("A_STOCK_THEME_LIBRARY_DB")
	themeLibraryRefreshAt := strings.TrimSpace(os.Getenv("A_STOCK_THEME_LIBRARY_REFRESH_AT"))
	if themeLibraryRefreshAt == "" {
		themeLibraryRefreshAt = "20:00"
	}
	if strings.EqualFold(themeLibraryRefreshAt, "off") {
		themeLibraryRefreshAt = ""
	}
	sectorFlowSampleDBPath := os.Getenv("A_STOCK_SECTOR_FLOW_SAMPLE_DB")
	settingsPath := os.Getenv("A_STOCK_SETTINGS_PATH")
	masteryCacheDir := os.Getenv("A_STOCK_MASTERY_CACHE")
	dataDir := ""
	if configDir, err := os.UserConfigDir(); err == nil {
		dataDir = preferredDataDir(configDir)
	}
	if reviewDBPath == "" {
		reviewDBPath = dataPath(dataDir, "reviews.db")
	}
	if settingsPath == "" {
		settingsPath = dataPath(dataDir, "settings.json")
	}
	if portfolioDBPath == "" {
		portfolioDBPath = dataPath(dataDir, "portfolio-inspections.db")
	}
	if stockResearchDBPath == "" {
		stockResearchDBPath = dataPath(dataDir, "stock-research.db")
	}
	if marketEmotionDBPath == "" {
		marketEmotionDBPath = dataPath(dataDir, "market-emotion.db")
	}
	if themeRadarDBPath == "" {
		themeRadarDBPath = dataPath(dataDir, "theme-radar.db")
	}
	if themeLibraryDBPath == "" {
		themeLibraryDBPath = dataPath(dataDir, "theme-library.db")
	}
	if sectorFlowSampleDBPath == "" {
		sectorFlowSampleDBPath = dataPath(dataDir, "sector-flow-samples.db")
	}
	if masteryCacheDir == "" {
		masteryCacheDir = dataPath(dataDir, "trading-mastery")
	}
	logDirectory := os.Getenv("A_STOCK_LOG_DIR")
	if logDirectory == "" {
		logDirectory = dataPath(dataDir, "logs")
	}
	if logDirectory != "" {
		logger, closer, err := runtimelog.ConfigureStandard(logDirectory, "backend")
		if err != nil {
			log.Printf("runtime logging unavailable: %v", err)
		} else {
			defer closer.Close()
			logger.Printf("level=info event=runtime_start component=backend version=%q", runtimeVersion())
		}
	}
	hermesHome := os.Getenv("A_STOCK_HERMES_HOME")
	if hermesHome == "" {
		hermesHome = dataPath(dataDir, "hermes-home")
	}
	hermesWorkDir := os.Getenv("A_STOCK_HERMES_WORKDIR")
	if hermesWorkDir == "" {
		hermesWorkDir, _ = os.Getwd()
	}
	codexHome := os.Getenv("A_STOCK_CODEX_HOME")
	if codexHome == "" {
		codexHome = dataPath(dataDir, "codex-home")
	}
	codexRoot := os.Getenv("A_STOCK_CODEX_RUNTIME_ROOT")
	if codexRoot == "" {
		codexRoot = filepath.Join(filepath.Dir(resolveHermesRuntimeRoot()), "codex-runtime")
	}
	agentGateway := agent.NewService(agent.ServiceConfig{Hermes: agent.HermesConfig{
		RuntimeRoot: resolveHermesRuntimeRoot(),
		Home:        hermesHome,
		WorkDir:     hermesWorkDir,
		PythonPath:  os.Getenv("A_STOCK_HERMES_PYTHON"),
	}, Codex: agent.CodexConfig{RuntimeRoot: codexRoot, Home: codexHome, WorkDir: filepath.Join(codexHome, "workspace")}})
	masteryLibrary := methodology.NewLibrary(methodology.Config{
		CacheDir:   masteryCacheDir,
		HermesHome: hermesHome,
	})
	monitorDBPath := os.Getenv("A_STOCK_MONITOR_DB")
	if monitorDBPath == "" {
		monitorDBPath = dataPath(dataDir, "monitor.db")
	}
	server := httpapi.NewServer(httpapi.Config{
		MonitorDBPath:            monitorDBPath,
		Token:                    os.Getenv("A_STOCK_TOKEN"),
		ReviewDBPath:             reviewDBPath,
		PortfolioDBPath:          portfolioDBPath,
		StockResearchDBPath:      stockResearchDBPath,
		RemoteDailyReviewURL:     os.Getenv("A_STOCK_DAILY_REVIEW_BASE_URL"),
		MarketEmotionDBPath:      marketEmotionDBPath,
		ThemeRadarDBPath:         themeRadarDBPath,
		ThemeLibraryDBPath:       themeLibraryDBPath,
		ThemeLibraryRefreshAt:    themeLibraryRefreshAt,
		SectorFlowSampleDBPath:   sectorFlowSampleDBPath,
		SectorFlowSampleInterval: sectorFlowSampleInterval(),
		DuanxianxiaBaseURL:       os.Getenv("A_STOCK_DUANXIANXIA_BASE_URL"),
		SettingsPath:             settingsPath,
		AgentGateway:             agentGateway,
		MasteryLibrary:           masteryLibrary,
		Logger:                   log.Default(),
		StrictPersistence:        true,
	})
	if err := server.StartupError(); err != nil {
		log.Fatalf("persistent data startup failed: %v", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go server.RunReviewScheduler(ctx)
	go server.RunDailySummaryScheduler(ctx)
	go server.RunRemoteDailyReviewScheduler(ctx)
	go server.RunMarketEmotionScheduler(ctx)
	go server.RunSectorFlowSampler(ctx)
	go server.RunMasteryScheduler(ctx)
	go server.RunPortfolioScheduler(ctx)
	go server.RunCockpitPatrol(ctx)
	go server.RunMonitorEngine(ctx)
	go server.RunThemeLibraryDailyRefresh(ctx)
	httpServer := &http.Server{Addr: addr, Handler: server}
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	log.Printf("easy-stock data foundation listening on http://%s", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	if err := server.Close(); err != nil {
		log.Printf("close persistent data: %v", err)
	}
}

func runtimeVersion() string {
	if value := strings.TrimSpace(os.Getenv("A_STOCK_APP_VERSION")); value != "" {
		return value
	}
	return "development"
}

func preferredDataDir(configDir string) string {
	current := filepath.Join(configDir, "easy-stock")
	if isFile(filepath.Join(current, "settings.json")) {
		return current
	}
	legacy := filepath.Join(configDir, "a-stock-ai")
	if isFile(filepath.Join(legacy, "settings.json")) {
		return legacy
	}
	return current
}

func isFile(filePath string) bool {
	info, err := os.Stat(filePath)
	return err == nil && !info.IsDir()
}

func dataPath(dataDir, name string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, name)
}

// sectorFlowSampleInterval 允许通过环境变量调整采样间隔。
//
// 默认 3 分钟（与公开同类产品一致）；低于 30 秒的配置会被忽略，
// 避免误配之后把上游榜单打成突发流量。
func sectorFlowSampleInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("A_STOCK_SECTOR_FLOW_SAMPLE_INTERVAL"))
	if raw == "" {
		return 0
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed < 30*time.Second {
		return 0
	}
	return parsed
}

func resolveHermesRuntimeRoot() string {
	if configured := os.Getenv("A_STOCK_HERMES_RUNTIME_ROOT"); configured != "" {
		return configured
	}
	candidates := []string{}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "hermes-runtime")))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "desktop", "resources", "hermes-runtime"),
			filepath.Join(cwd, "..", "desktop", "resources", "hermes-runtime"),
			// 开发态的运行时缓存：.runtime/hermes-runtime-<version>。
			//
			// 必须排在 desktop/resources 之后：安装包用的是那份，它是正式契约；
			// 这里是"打包流程没跑，但本地有缓存"时的兜底。
			// 缺了它，开发机上 runtime 目录一旦被重装/清空，LLM 整条链路直接不可用，
			// 而症状是"运行时不可用"或"缺 providers 模块"——看起来像代码问题，
			// 排查成本很高（10-24 实际踩过一次）。
			latestRuntimeCache(cwd),
			latestRuntimeCache(filepath.Join(cwd, "..")),
		)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		// 只认"有 Python 解释器"的目录。
		//
		// desktop/resources/hermes-runtime 可能只剩一个空 venv（重装时残留），
		// 目录存在但跑不起来。按"目录存在就用"会让这种半成品被选中，
		// 然后报一个与真正原因无关的错。
		if hasRuntimePython(candidate) {
			return candidate
		}
	}
	return ""
}

// latestRuntimeCache 在 base/.runtime 下找版本号最大的 hermes-runtime-*。
//
// 按版本号排序而不是按修改时间：修改时间会被任意一次解压/触碰改变，
// 而目录名里的版本号才是"哪个更新、哪个更完整"的可靠依据。
func latestRuntimeCache(base string) string {
	pattern := filepath.Join(base, ".runtime", "hermes-runtime-*")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for _, match := range matches {
		if hasRuntimePython(match) {
			return match
		}
	}
	return ""
}

// hasRuntimePython 判断一个运行时目录里是否真的存在可执行的 Python。
//
// 判断标准必须与 agent.runtimePythonForOS 完全一致：那边在 Windows 找
// python/python.exe、在类 Unix 找 venv/bin/python。这里如果放宽到
// "只要有 venv/Scripts/python.exe 就算"，就会选中一个只剩空 venv 的
// 半成品目录——它真的能启动 Python，但里面没有 hermes 的依赖，
// 报出来的错是"缺 providers 模块"，与真正的原因（运行时没准备好）无关。
func hasRuntimePython(root string) bool {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return false
	}
	var candidate string
	if runtime.GOOS == "windows" {
		candidate = filepath.Join(root, "python", "python.exe")
	} else {
		candidate = filepath.Join(root, "venv", "bin", "python")
	}
	info, err = os.Stat(candidate)
	return err == nil && !info.IsDir()
}
