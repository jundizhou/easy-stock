package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"easy-stock/backend/internal/chananalysis"
	"easy-stock/backend/internal/chanscreener"
	"easy-stock/backend/internal/hermes"
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
	marketEmotionDBPath := os.Getenv("A_STOCK_MARKET_EMOTION_DB")
	themeRadarDBPath := os.Getenv("A_STOCK_THEME_RADAR_DB")
	dailyAnalysisDBPath := os.Getenv("A_STOCK_DAILY_ANALYSIS_DB")
	tradeJournalDBPath := os.Getenv("A_STOCK_TRADE_JOURNAL_DB")
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
	if marketEmotionDBPath == "" {
		marketEmotionDBPath = dataPath(dataDir, "market-emotion.db")
	}
	if themeRadarDBPath == "" {
		themeRadarDBPath = dataPath(dataDir, "theme-radar.db")
	}
	if dailyAnalysisDBPath == "" {
		dailyAnalysisDBPath = dataPath(dataDir, "daily-analysis.db")
	}
	if tradeJournalDBPath == "" {
		tradeJournalDBPath = dataPath(dataDir, "trade-journal.db")
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
	hermesGateway := hermes.NewRuntime(hermes.Config{
		RuntimeRoot: resolveHermesRuntimeRoot(),
		Home:        hermesHome,
		WorkDir:     hermesWorkDir,
		PythonPath:  os.Getenv("A_STOCK_HERMES_PYTHON"),
	})
	masteryLibrary := methodology.NewLibrary(methodology.Config{
		CacheDir:   masteryCacheDir,
		HermesHome: hermesHome,
	})
	// 缠论分析走独立的 Python 环境（a-stock-data venv + czsc），与 Hermes 的
	// 运行时解释器分开，避免两边的依赖互相污染。
	chanAnalysisService := chananalysis.NewService(chananalysis.Config{
		PythonPath: os.Getenv("A_STOCK_CZSC_PYTHON"),
		ScriptPath: os.Getenv("A_STOCK_CZSC_SCRIPT"),
		WorkDir:    os.Getenv("A_STOCK_CZSC_WORKDIR"),
	})
	// 脚本需要回调本后端拉 K 线，因此把实际监听地址回填给它。监听地址可能是
	// :20081 这类省略主机的形式，需补成可直连的地址。
	chanAnalysisService.SetBackendURL(backendSelfURL(addr))
	// 后端启用鉴权时，脚本的回调请求也必须带上同一个令牌。
	chanAnalysisService.SetToken(os.Getenv("A_STOCK_TOKEN"))

	// chan.py 缠论引擎（选股 + 买卖点分析）与 czsc 引擎并列，独立探测脚本与
	// 解释器；脚本同样回调本后端拉K线。
	chanScreenerService := chanscreener.NewService(chanscreener.Config{
		PythonPath: os.Getenv("A_STOCK_CHANPY_PYTHON"),
		ScriptPath: os.Getenv("A_STOCK_CHANPY_SCRIPT"),
		WorkDir:    os.Getenv("A_STOCK_CHANPY_WORKDIR"),
	})
	chanScreenerService.SetBackendURL(backendSelfURL(addr))
	chanScreenerService.SetToken(os.Getenv("A_STOCK_TOKEN"))
	server := httpapi.NewServer(httpapi.Config{
		Token:                os.Getenv("A_STOCK_TOKEN"),
		ReviewDBPath:         reviewDBPath,
		PortfolioDBPath:      portfolioDBPath,
		RemoteDailyReviewURL: os.Getenv("A_STOCK_DAILY_REVIEW_BASE_URL"),
		MarketEmotionDBPath:  marketEmotionDBPath,
		ThemeRadarDBPath:     themeRadarDBPath,
		DailyAnalysisDBPath:  dailyAnalysisDBPath,
		TradeJournalDBPath:   tradeJournalDBPath,
		DuanxianxiaBaseURL:   os.Getenv("A_STOCK_DUANXIANXIA_BASE_URL"),
		WeChatAPIURL:         os.Getenv("A_STOCK_WECHAT_API_URL"),
		SettingsPath:         settingsPath,
		HermesGateway:        hermesGateway,
		MasteryLibrary:       masteryLibrary,
		ChanAnalysis:         chanAnalysisService,
		ChanScreener:         chanScreenerService,
		Logger:               log.Default(),
		StrictPersistence:    true,
	})
	if err := server.StartupError(); err != nil {
		log.Fatalf("persistent data startup failed: %v", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go server.RunReviewScheduler(ctx)
	go server.RunRemoteDailyReviewScheduler(ctx)
	go server.RunMarketEmotionScheduler(ctx)
	go server.RunMasteryScheduler(ctx)
	go server.RunDailyAnalysisScheduler(ctx)
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

// backendSelfURL 把监听地址转成可被本机子进程回调的 URL。监听地址可能是
// ":20081" / "0.0.0.0:20081" 这类通配形式，直接访问会失败，因此统一补成
// 127.0.0.1。也允许通过 A_STOCK_SELF_URL 显式覆盖（例如反向代理后部署）。
func backendSelfURL(addr string) string {
	if configured := strings.TrimSpace(os.Getenv("A_STOCK_SELF_URL")); configured != "" {
		return strings.TrimRight(configured, "/")
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		// addr 可能是不含端口的纯端口或纯主机，退回默认端口。
		return "http://127.0.0.1:20081"
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
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
		)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}
