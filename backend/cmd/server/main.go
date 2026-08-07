package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

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
	if reviewDBPath == "" {
		if configDir, err := os.UserConfigDir(); err == nil {
			reviewDBPath = filepath.Join(configDir, "easy-stock", "reviews.db")
		}
	}
	if settingsPath == "" {
		if configDir, err := os.UserConfigDir(); err == nil {
			settingsPath = filepath.Join(configDir, "easy-stock", "settings.json")
		}
	}
	if marketEmotionDBPath == "" {
		if configDir, err := os.UserConfigDir(); err == nil {
			marketEmotionDBPath = filepath.Join(configDir, "easy-stock", "market-emotion.db")
		}
	}
	if themeRadarDBPath == "" {
		if configDir, err := os.UserConfigDir(); err == nil {
			themeRadarDBPath = filepath.Join(configDir, "easy-stock", "theme-radar.db")
		}
	}
	if masteryCacheDir == "" {
		if configDir, err := os.UserConfigDir(); err == nil {
			masteryCacheDir = filepath.Join(configDir, "easy-stock", "trading-mastery")
		}
	}
	hermesHome := os.Getenv("A_STOCK_HERMES_HOME")
	if hermesHome == "" {
		if configDir, err := os.UserConfigDir(); err == nil {
			hermesHome = filepath.Join(configDir, "easy-stock", "hermes-home")
		}
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
		Token:               os.Getenv("A_STOCK_TOKEN"),
		ReviewDBPath:        reviewDBPath,
		MarketEmotionDBPath: marketEmotionDBPath,
		ThemeRadarDBPath:    themeRadarDBPath,
		DuanxianxiaBaseURL:  os.Getenv("A_STOCK_DUANXIANXIA_BASE_URL"),
		WeChatAPIURL:        os.Getenv("A_STOCK_WECHAT_API_URL"),
		SettingsPath:        settingsPath,
		HermesGateway:       hermesGateway,
		MasteryLibrary:      masteryLibrary,
	})
	go server.RunReviewScheduler(context.Background())
	go server.RunMarketEmotionScheduler(context.Background())
	go server.RunMasteryScheduler(context.Background())
	log.Printf("easy-stock data foundation listening on http://%s", addr)
	if err := http.ListenAndServe(addr, server); err != nil {
		log.Fatal(err)
	}
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
