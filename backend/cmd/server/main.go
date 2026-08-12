package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/httpapi"
	"easy-stock/backend/internal/methodology"
)

func main() {
	addr := os.Getenv("A_STOCK_ADDR")
	if addr == "" {
		addr = "127.0.0.1:20081"
	}
	reviewDBPath := os.Getenv("A_STOCK_REVIEW_DB")
	marketEmotionDBPath := os.Getenv("A_STOCK_MARKET_EMOTION_DB")
	themeRadarDBPath := os.Getenv("A_STOCK_THEME_RADAR_DB")
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
	if marketEmotionDBPath == "" {
		marketEmotionDBPath = dataPath(dataDir, "market-emotion.db")
	}
	if themeRadarDBPath == "" {
		themeRadarDBPath = dataPath(dataDir, "theme-radar.db")
	}
	if masteryCacheDir == "" {
		masteryCacheDir = dataPath(dataDir, "trading-mastery")
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
	server := httpapi.NewServer(httpapi.Config{
		Token:                os.Getenv("A_STOCK_TOKEN"),
		ReviewDBPath:         reviewDBPath,
		RemoteDailyReviewURL: os.Getenv("A_STOCK_DAILY_REVIEW_BASE_URL"),
		MarketEmotionDBPath:  marketEmotionDBPath,
		ThemeRadarDBPath:     themeRadarDBPath,
		DuanxianxiaBaseURL:   os.Getenv("A_STOCK_DUANXIANXIA_BASE_URL"),
		WeChatAPIURL:         os.Getenv("A_STOCK_WECHAT_API_URL"),
		SettingsPath:         settingsPath,
		HermesGateway:        hermesGateway,
		MasteryLibrary:       masteryLibrary,
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
