package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/marketemotion"
	"easy-stock/backend/internal/methodology"
	"easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/providers/cls"
	"easy-stock/backend/internal/providers/duanxianxia"
	"easy-stock/backend/internal/providers/eastmoney"
	"easy-stock/backend/internal/providers/hotstock"
	marketoverviewprovider "easy-stock/backend/internal/providers/marketoverview"
	"easy-stock/backend/internal/providers/sina"
	"easy-stock/backend/internal/providers/tencent"
	"easy-stock/backend/internal/review"
	"easy-stock/backend/internal/runtimelog"
	"easy-stock/backend/internal/sector"
	"easy-stock/backend/internal/strategy/inflection"
)

type Server struct {
	mux                   *http.ServeMux
	token                 string
	realtimeProvider      RealtimeProvider
	kLinePrimary          KLineProvider
	kLineFallback         KLineProvider
	newsProvider          NewsProvider
	sectorMap             SectorMapProvider
	themeOverview         ThemeOverviewProvider
	limitUpProvider       LimitUpProvider
	marketPools           MarketPoolProvider
	stockConcepts         StockConceptProvider
	stockBusiness         StockBusinessProfileProvider
	stockDirectory        StockDirectoryProvider
	hotStockProvider      HotStockProvider
	marketOverview        MarketOverviewProvider
	inflection            InflectionEvaluator
	themeSnapshots        *themeSnapshotCache
	limitUpSnapshots      *limitUpLadderCache
	stockDirectories      *stockDirectoryCache
	hotStockRanks         *hotStockRankCache
	marketSnapshots       *marketOverviewCache
	marketEmotion         *marketEmotionEngine
	marketEmotionIntraday *marketEmotionIntradayCache
	reviewStore           *review.Store
	portfolioStore        *portfolioinspection.Store
	portfolioInspection   *portfolioinspection.Service
	reviewImporter        ReviewImporter
	wechatAPIURL          string
	settingsStore         *appsettings.Store
	reviewAutomation      *review.Automation
	remoteDailySync       *review.RemoteDailySync
	hermesGateway         hermes.Gateway
	masteryLibrary        *methodology.Library
	marketEmotionStore    *marketemotion.Store
	themeRadarStore       *duanxianxia.Store
	startupError          error
	logger                *log.Logger
}

func NewServer(config any) *Server {
	cfg := normalizeConfig(config)
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	var startupErrors []error
	sinaClient := sina.NewClient()
	eastMoneyClient := eastmoney.NewClient()
	tencentClient := tencent.NewClient()
	clsClient := cls.NewClient()
	if cfg.Realtime == nil {
		cfg.Realtime = sinaClient
	}
	if cfg.KLinePrimary == nil {
		cfg.KLinePrimary = eastMoneyClient
	}
	if cfg.KLineFallback == nil {
		cfg.KLineFallback = sinaClient
	}
	if cfg.News == nil {
		cfg.News = clsClient
	}
	var kaipanlaService *duanxianxia.Service
	if strings.TrimSpace(cfg.ThemeRadarDBPath) != "" {
		if store, err := duanxianxia.OpenStore(cfg.ThemeRadarDBPath); err == nil {
			client := duanxianxia.NewClient(duanxianxia.ClientConfig{BaseURL: cfg.DuanxianxiaBaseURL})
			kaipanlaService = duanxianxia.NewService(client, store, duanxianxia.ServiceConfig{
				RefreshInterval:  5 * time.Minute,
				LeaderThemeLimit: 3,
			})
		} else if cfg.StrictPersistence {
			startupErrors = append(startupErrors, fmt.Errorf("open theme radar database: %w", err))
		}
	}
	usingDefaultLimitUp := cfg.LimitUp == nil
	if usingDefaultLimitUp {
		if kaipanlaService != nil {
			cfg.LimitUp = duanxianxia.NewLimitUpProvider(kaipanlaService, eastMoneyClient)
		} else {
			cfg.LimitUp = eastMoneyClient
		}
	}
	if cfg.MarketPools == nil {
		cfg.MarketPools = eastMoneyClient
	}
	if cfg.StockConcept == nil && usingDefaultLimitUp {
		cfg.StockConcept = eastMoneyClient
	}
	if cfg.StockBusiness == nil {
		cfg.StockBusiness = eastMoneyClient
	}
	if cfg.StockDirectory == nil {
		cfg.StockDirectory = eastMoneyClient
	}
	if cfg.MarketOverview == nil {
		cfg.MarketOverview = marketoverviewprovider.New(eastMoneyClient, tencentClient, tencentClient, sinaClient)
	}
	if cfg.SectorMap == nil {
		mapper := sector.NewMapper(
			eastMoneyClient,
			sector.WithQuoteProvider(sinaClient),
			sector.WithLimitUpProvider(cfg.LimitUp),
			sector.WithStockCatalogProvider(eastMoneyClient),
		)
		var defaultSectorMap SectorMapProvider = mapper
		var defaultThemeOverview ThemeOverviewProvider = mapper
		var radarFallback sector.RadarFallback = mapper
		if cfg.ThemeRadarFallback != nil {
			radarFallback = cfg.ThemeRadarFallback
		}
		var radarSource sector.RadarSnapshotSource
		if kaipanlaService != nil {
			radarSource = kaipanlaService
		}
		radar := sector.NewRadarProvider(radarSource, radarFallback, cfg.Realtime, sector.RadarProviderConfig{
			IndustryMomentum:  cfg.MarketOverview,
			FallbackFillLimit: 16,
		})
		defaultSectorMap = radar
		defaultThemeOverview = radar
		cfg.SectorMap = defaultSectorMap
		if cfg.ThemeOverview == nil {
			cfg.ThemeOverview = defaultThemeOverview
		}
	}
	if cfg.Inflection == nil {
		cfg.Inflection = inflection.NewEngine(inflection.DefaultConfig())
	}
	if cfg.ReviewStore == nil {
		store, err := review.OpenStore(cfg.ReviewDBPath)
		if err == nil {
			cfg.ReviewStore = store
		} else if cfg.StrictPersistence {
			startupErrors = append(startupErrors, fmt.Errorf("open review database: %w", err))
			cfg.ReviewStore, _ = review.OpenStore(":memory:")
		} else {
			cfg.ReviewStore, _ = review.OpenStore(":memory:")
		}
	}
	if cfg.MarketEmotionStore == nil {
		store, err := marketemotion.OpenStore(cfg.MarketEmotionDBPath)
		if err == nil {
			cfg.MarketEmotionStore = store
		} else if cfg.StrictPersistence {
			startupErrors = append(startupErrors, fmt.Errorf("open market emotion database: %w", err))
			cfg.MarketEmotionStore, _ = marketemotion.OpenStore("")
		} else {
			cfg.MarketEmotionStore, _ = marketemotion.OpenStore("")
		}
	}
	if cfg.PortfolioStore == nil {
		store, err := portfolioinspection.OpenStore(cfg.PortfolioDBPath)
		if err == nil {
			cfg.PortfolioStore = store
		} else if cfg.StrictPersistence {
			startupErrors = append(startupErrors, fmt.Errorf("open portfolio inspection database: %w", err))
			cfg.PortfolioStore, _ = portfolioinspection.OpenStore(":memory:")
		} else {
			cfg.PortfolioStore, _ = portfolioinspection.OpenStore(":memory:")
		}
	}
	if cfg.ReviewHTTP == nil {
		cfg.ReviewHTTP = &http.Client{Timeout: 90 * time.Second}
	}
	if cfg.SettingsStore == nil {
		store, err := appsettings.Open(cfg.SettingsPath)
		if err == nil {
			cfg.SettingsStore = store
		} else if cfg.StrictPersistence {
			startupErrors = append(startupErrors, fmt.Errorf("open settings: %w", err))
			cfg.SettingsStore, _ = appsettings.Open("")
		} else {
			cfg.SettingsStore, _ = appsettings.Open("")
		}
	}
	if cfg.HotStocks == nil {
		cfg.HotStocks = hotstock.NewClient()
	}
	if cfg.HermesGateway != nil && (!cfg.StrictPersistence || len(startupErrors) == 0) {
		values := cfg.SettingsStore.Snapshot()
		var migratedKey *string
		if strings.TrimSpace(values.LLM.APIKey) != "" {
			key := strings.TrimSpace(values.LLM.APIKey)
			migratedKey = &key
			if updated, err := cfg.SettingsStore.Update(func(next *appsettings.Values) error {
				next.LLM.APIKey = ""
				return nil
			}); err == nil {
				values = updated
			}
		}
		if profileGateway, ok := cfg.HermesGateway.(hermes.ProfileGateway); ok {
			if migratedKey == nil {
				if key, err := cfg.HermesGateway.ModelAPIKey(); err == nil && strings.TrimSpace(key) != "" {
					migratedKey = &key
				}
			}
			_ = profileGateway.SyncLLMProfile(values.LLM, values.ActiveLLMProfileID, migratedKey)
		} else {
			_ = cfg.HermesGateway.SyncLLM(values.LLM, migratedKey)
		}
	}
	if cfg.ReviewImporter == nil {
		cfg.ReviewImporter = review.NewImporter(cfg.ReviewHTTP, cfg.WeChatAPIURL)
	}
	if cfg.ReviewAutomation == nil {
		cfg.ReviewAutomation = review.NewAutomation(cfg.ReviewStore, cfg.ReviewImporter, cfg.SettingsStore, cfg.ReviewHTTP, cfg.WeChatAPIURL, cfg.HermesGateway)
	}
	if cfg.RemoteDailySync == nil {
		cfg.RemoteDailySync = review.NewRemoteDailySync(cfg.ReviewStore, review.RemoteDailySyncConfig{
			BaseURL: cfg.RemoteDailyReviewURL,
			Client:  cfg.ReviewHTTP,
		})
	}
	s := &Server{
		mux:                   http.NewServeMux(),
		token:                 cfg.Token,
		realtimeProvider:      cfg.Realtime,
		kLinePrimary:          cfg.KLinePrimary,
		kLineFallback:         cfg.KLineFallback,
		newsProvider:          cfg.News,
		sectorMap:             cfg.SectorMap,
		themeOverview:         cfg.ThemeOverview,
		limitUpProvider:       cfg.LimitUp,
		marketPools:           cfg.MarketPools,
		stockConcepts:         cfg.StockConcept,
		stockBusiness:         cfg.StockBusiness,
		stockDirectory:        cfg.StockDirectory,
		hotStockProvider:      cfg.HotStocks,
		marketOverview:        cfg.MarketOverview,
		inflection:            cfg.Inflection,
		themeSnapshots:        newThemeSnapshotCache(30 * time.Second),
		limitUpSnapshots:      newLimitUpLadderCache(30 * time.Second),
		stockDirectories:      newStockDirectoryCache(6 * time.Hour),
		hotStockRanks:         newHotStockRankCache(2 * time.Minute),
		marketSnapshots:       newMarketOverviewCache(45 * time.Second),
		marketEmotionIntraday: newMarketEmotionIntradayCache(marketEmotionIntradayTTL),
		reviewStore:           cfg.ReviewStore,
		portfolioStore:        cfg.PortfolioStore,
		reviewImporter:        cfg.ReviewImporter,
		wechatAPIURL:          strings.TrimSpace(cfg.WeChatAPIURL),
		settingsStore:         cfg.SettingsStore,
		reviewAutomation:      cfg.ReviewAutomation,
		remoteDailySync:       cfg.RemoteDailySync,
		hermesGateway:         cfg.HermesGateway,
		masteryLibrary:        cfg.MasteryLibrary,
		marketEmotionStore:    cfg.MarketEmotionStore,
		startupError:          errors.Join(startupErrors...),
		logger:                cfg.Logger,
	}
	if kaipanlaService != nil {
		s.themeRadarStore = kaipanlaService.Store()
	}
	s.marketEmotion = newMarketEmotionEngine(
		cfg.MarketEmotionStore,
		s.limitUpProvider,
		s.marketPools,
		s.kLinePrimary,
		s.kLineFallback,
		s.stockConcepts,
	)
	s.portfolioInspection = portfolioinspection.NewService(cfg.PortfolioStore, cfg.HermesGateway, s.analyzeStock, cfg.Logger)
	s.routes()
	return s
}

func (s *Server) StartupError() error {
	if s == nil {
		return errors.New("server is nil")
	}
	return s.startupError
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	var closeErrors []error
	if s.reviewStore != nil {
		closeErrors = append(closeErrors, s.reviewStore.Close())
	}
	if s.marketEmotionStore != nil {
		closeErrors = append(closeErrors, s.marketEmotionStore.Close())
	}
	if s.portfolioStore != nil {
		closeErrors = append(closeErrors, s.portfolioStore.Close())
	}
	if s.themeRadarStore != nil {
		closeErrors = append(closeErrors, s.themeRadarStore.Close())
	}
	return errors.Join(closeErrors...)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	requestID := runtimeRequestID(r)
	loggedWriter := &requestLogWriter{ResponseWriter: w}
	loggedWriter.Header().Set("X-Request-ID", requestID)
	defer s.logRequest(r, loggedWriter, requestID, startedAt)

	s.withCORS(loggedWriter, r)
	if r.Method == http.MethodOptions {
		loggedWriter.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.authorized(r) {
		writeError(loggedWriter, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.mux.ServeHTTP(loggedWriter, r)
}

func (s *Server) RunReviewScheduler(ctx context.Context) {
	if s.reviewAutomation != nil {
		s.logSchedulerLifecycle(ctx, "reviews", "subscription_sync", func() {
			s.reviewAutomation.RunScheduler(ctx, s.logger)
		})
	}
}

func (s *Server) RunRemoteDailyReviewScheduler(ctx context.Context) {
	if s.remoteDailySync != nil {
		s.logSchedulerLifecycle(ctx, "reviews", "remote_daily_sync", func() {
			s.remoteDailySync.Run(ctx, s.logger)
		})
	}
}

func (s *Server) RunMarketEmotionScheduler(ctx context.Context) {
	if s.marketEmotion != nil {
		s.logSchedulerLifecycle(ctx, "short-term", "market_emotion", func() {
			s.marketEmotion.runScheduler(ctx, s.logger)
		})
	}
}

func (s *Server) RunMasteryScheduler(ctx context.Context) {
	if s.masteryLibrary == nil {
		return
	}
	if s.logger != nil {
		s.logger.Printf("level=info event=scheduler_start feature=trading-mastery task=mastery_snapshot")
		defer s.logger.Printf("level=info event=scheduler_stop feature=trading-mastery task=mastery_snapshot")
	}
	s.refreshMasterySnapshot(ctx, false)
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshMasterySnapshot(ctx, true)
		}
	}
}

func (s *Server) logSchedulerLifecycle(_ context.Context, feature, task string, run func()) {
	if s.logger != nil {
		s.logger.Printf("level=info event=scheduler_start feature=%s task=%s", feature, task)
		defer s.logger.Printf("level=info event=scheduler_stop feature=%s task=%s", feature, task)
	}
	run()
}

func (s *Server) refreshMasterySnapshot(ctx context.Context, force bool) {
	startedAt := time.Now()
	_, err := s.masteryLibrary.Snapshot(ctx, force)
	if s.logger == nil {
		return
	}
	if err != nil {
		s.logger.Printf("level=warn event=scheduler_error feature=trading-mastery task=mastery_snapshot force=%t duration_ms=%d error=%q", force, time.Since(startedAt).Milliseconds(), runtimelog.Redact(err.Error()))
		return
	}
	s.logger.Printf("level=info event=scheduler_run feature=trading-mastery task=mastery_snapshot force=%t duration_ms=%d", force, time.Since(startedAt).Milliseconds())
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/v1/sources", s.sources)
	s.mux.HandleFunc("GET /api/v1/quotes/realtime", s.realtime)
	s.mux.HandleFunc("GET /api/v1/quotes/kline", s.kline)
	s.mux.HandleFunc("GET /api/v1/quotes/kline/batch", s.klineBatch)
	s.mux.HandleFunc("GET /api/v1/market/news", s.news)
	s.mux.HandleFunc("GET /api/v1/market/indexes", s.marketIndexesHandler)
	s.mux.HandleFunc("GET /api/v1/market/index-series", s.marketIndexSeriesHandler)
	s.mux.HandleFunc("GET /api/v1/market/industries", s.marketIndustriesHandler)
	s.mux.HandleFunc("GET /api/v1/market/flows", s.marketFlowsHandler)
	s.mux.HandleFunc("GET /api/v1/market/margin-balance", s.marketMarginBalanceHandler)
	s.mux.HandleFunc("GET /api/v1/market/billboard", s.marketBillboardHandler)
	s.mux.HandleFunc("GET /api/v1/market/billboard/detail", s.marketBillboardDetailHandler)
	s.mux.HandleFunc("GET /api/v1/research/announcements", s.marketAnnouncementsHandler)
	s.mux.HandleFunc("GET /api/v1/research/institution-reports", s.marketInstitutionReportsHandler)
	s.mux.HandleFunc("GET /api/v1/research/industries", s.marketIndustryResearchHandler)
	s.mux.HandleFunc("GET /api/v1/themes/overview", s.themeOverviewHandler)
	s.mux.HandleFunc("GET /api/v1/themes/screen", s.themeScreenHandler)
	s.mux.HandleFunc("GET /api/v1/sector-map", s.sectorMapHandler)
	s.mux.HandleFunc("GET /api/v1/short-term/limit-up-ladder", s.limitUpLadderHandler)
	s.mux.HandleFunc("GET /api/v1/short-term/emotion-history", s.marketEmotionHistoryHandler)
	s.mux.HandleFunc("GET /api/v1/short-term/mastery", s.masteryIndex)
	s.mux.HandleFunc("GET /api/v1/short-term/mastery/trader", s.masteryTrader)
	s.mux.HandleFunc("POST /api/v1/short-term/mastery/refresh", s.masteryRefresh)
	s.mux.HandleFunc("POST /api/v1/stocks/ai-analysis", s.stockAIAnalysis)
	s.mux.HandleFunc("GET /api/v1/stocks/directory", s.stockDirectoryHandler)
	s.mux.HandleFunc("GET /api/v1/stocks/hot-ranks", s.hotStockRanksHandler)
	s.mux.HandleFunc("GET /api/v1/portfolio-inspections", s.portfolioInspectionList)
	s.mux.HandleFunc("POST /api/v1/portfolio-inspections", s.portfolioInspectionCreate)
	s.mux.HandleFunc("GET /api/v1/portfolio-inspections/{id}", s.portfolioInspectionGet)
	s.mux.HandleFunc("GET /api/v1/reviews/sources", s.reviewSources)
	s.mux.HandleFunc("GET /api/v1/reviews/authors", s.reviewAuthors)
	s.mux.HandleFunc("DELETE /api/v1/reviews/authors/{id}", s.reviewAuthorDelete)
	s.mux.HandleFunc("GET /api/v1/reviews/posts", s.reviewPosts)
	s.mux.HandleFunc("GET /api/v1/reviews/posts/{id}", s.reviewPost)
	s.mux.HandleFunc("DELETE /api/v1/reviews/posts/{id}", s.reviewPostDelete)
	s.mux.HandleFunc("GET /api/v1/reviews/daily-summary", s.reviewDailySummaryGet)
	s.mux.HandleFunc("POST /api/v1/reviews/daily-summary/anonymize", s.reviewDailySummaryAnonymize)
	s.mux.HandleFunc("GET /api/v1/reviews/daily-summary/window", s.reviewDailySummaryWindow)
	s.mux.HandleFunc("GET /api/v1/reviews/daily-summary/status", s.reviewDailySummaryStatus)
	s.mux.HandleFunc("POST /api/v1/reviews/daily-summary", s.reviewDailySummaryCreate)
	s.mux.HandleFunc("GET /api/v1/reviews/daily-validation", s.reviewDailyValidation)
	s.mux.HandleFunc("GET /api/v1/reviews/daily-validation/status", s.reviewDailyValidationStatus)
	s.mux.HandleFunc("POST /api/v1/reviews/daily-validation", s.reviewDailyValidationCreate)
	s.mux.HandleFunc("POST /api/v1/reviews/import", s.reviewImport)
	s.mux.HandleFunc("GET /api/v1/reviews/subscriptions", s.reviewSubscriptions)
	s.mux.HandleFunc("POST /api/v1/reviews/subscriptions", s.reviewSubscriptionCreate)
	s.mux.HandleFunc("DELETE /api/v1/reviews/subscriptions/{id}", s.reviewSubscriptionDelete)
	s.mux.HandleFunc("POST /api/v1/reviews/sync", s.reviewSyncAll)
	s.mux.HandleFunc("POST /api/v1/reviews/subscriptions/{id}/sync", s.reviewSyncOne)
	s.mux.HandleFunc("POST /api/v1/reviews/posts/{id}/analyze", s.reviewAnalyzePost)
	s.mux.HandleFunc("GET /api/v1/reviews/remote-daily/status", s.reviewRemoteDailyStatus)
	s.mux.HandleFunc("POST /api/v1/reviews/remote-daily/sync", s.reviewRemoteDailySync)
	s.mux.HandleFunc("GET /api/v1/settings", s.settingsGet)
	s.mux.HandleFunc("PUT /api/v1/settings", s.settingsUpdate)
	s.mux.HandleFunc("GET /api/v1/settings/agent", s.settingsAgentGet)
	s.mux.HandleFunc("PUT /api/v1/settings/agent", s.settingsAgentUpdate)
	s.mux.HandleFunc("POST /api/v1/settings/llm/models", s.settingsLLMModels)
	s.mux.HandleFunc("POST /api/v1/settings/llm/test", s.settingsLLMTest)
	s.mux.HandleFunc("GET /api/v1/ai/ws", s.aiChatWebSocket)
	s.mux.HandleFunc("POST /api/v1/strategy/inflections/evaluate", s.inflectionEvaluate)
	s.mux.HandleFunc("GET /api/v1/ws/stream", s.stream)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"name": "easy-stock data foundation",
		"time": time.Now(),
	})
}

func (s *Server) sources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"sources": []foundation.SourceHealth{
			{ID: "duanxianxia", Name: "短线侠 / 开盘啦", Category: "theme,leaders,limit-up,concept", OK: true, CheckedAt: time.Now()},
			{ID: "eastmoney", Name: "东方财富", Category: "quote,kline,f10,report", OK: true, CheckedAt: time.Now()},
			{ID: "sina", Name: "新浪财经", Category: "quote,kline,money-flow", OK: true, CheckedAt: time.Now()},
			{ID: "tencent", Name: "腾讯财经", Category: "quote,index,hk", OK: true, CheckedAt: time.Now()},
			{ID: "cls", Name: "财联社", Category: "news,calendar", OK: true, CheckedAt: time.Now()},
			{ID: "tradingview", Name: "TradingView", Category: "news", OK: true, CheckedAt: time.Now()},
			{ID: "tushare", Name: "Tushare", Category: "basic,daily,index", OK: false, Message: "requires token", CheckedAt: time.Now()},
		},
	})
}

func (s *Server) realtime(w http.ResponseWriter, r *http.Request) {
	symbolsParam := strings.TrimSpace(r.URL.Query().Get("symbols"))
	if symbolsParam == "" {
		writeError(w, http.StatusBadRequest, "symbols is required")
		return
	}
	symbols, err := foundation.SplitSymbols(symbolsParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	quotes, err := s.realtimeProvider.Realtime(r.Context(), symbols)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": quotes})
}

func (s *Server) kline(w http.ResponseWriter, r *http.Request) {
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	if symbol == "" {
		writeError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	period := firstNonEmpty(r.URL.Query().Get("period"), "day")
	limit := 120
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	lines, err := s.loadKLine(ctx, symbol, period, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": lines})
}

func (s *Server) klineBatch(w http.ResponseWriter, r *http.Request) {
	symbolsParam := strings.TrimSpace(r.URL.Query().Get("symbols"))
	if symbolsParam == "" {
		writeError(w, http.StatusBadRequest, "symbols is required")
		return
	}
	symbols, err := foundation.SplitSymbols(symbolsParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(symbols) > 30 {
		writeError(w, http.StatusBadRequest, "batch kline supports at most 30 symbols")
		return
	}
	period := firstNonEmpty(r.URL.Query().Get("period"), "day")
	limit := 40
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 || parsed > 240 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 240")
			return
		}
		limit = parsed
	}

	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	data := make(map[string][]foundation.KLine, len(symbols))
	errorsBySymbol := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4)
	for _, symbol := range symbols {
		symbol := symbol
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				mu.Lock()
				errorsBySymbol[symbol] = ctx.Err().Error()
				mu.Unlock()
				return
			}
			lines, loadErr := s.loadKLine(ctx, symbol, period, limit)
			mu.Lock()
			defer mu.Unlock()
			if loadErr != nil {
				errorsBySymbol[symbol] = loadErr.Error()
				return
			}
			data[symbol] = lines
		}()
	}
	wg.Wait()
	if len(data) == 0 {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "all batch kline requests failed", "errors": errorsBySymbol})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "errors": errorsBySymbol})
}

func (s *Server) loadKLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	lines, err := s.kLinePrimary.KLine(ctx, symbol, period, limit)
	if err == nil {
		return normalizeKLinePeriod(lines, period), nil
	}
	lines, err = s.kLineFallback.KLine(ctx, symbol, period, limit)
	if err != nil {
		return nil, err
	}
	return normalizeKLinePeriod(lines, period), nil
}

func normalizeKLinePeriod(lines []foundation.KLine, period string) []foundation.KLine {
	if strings.TrimSpace(period) != "1" || len(lines) == 0 {
		return lines
	}

	chinaTime := time.FixedZone("Asia/Shanghai", 8*60*60)
	latestTime := time.Time{}
	for _, line := range lines {
		if !line.Time.IsZero() && line.Time.After(latestTime) {
			latestTime = line.Time
		}
	}
	if latestTime.IsZero() {
		return lines
	}

	latestDate := latestTime.In(chinaTime).Format("2006-01-02")
	previousTime := time.Time{}
	previousClose := 0.0
	for _, line := range lines {
		if line.Time.IsZero() || line.Time.In(chinaTime).Format("2006-01-02") == latestDate {
			continue
		}
		if line.Close > 0 && line.Time.Before(latestTime) && line.Time.After(previousTime) {
			previousTime = line.Time
			previousClose = line.Close
		}
	}

	filtered := make([]foundation.KLine, 0, len(lines))
	for _, line := range lines {
		if line.Time.IsZero() || line.Time.In(chinaTime).Format("2006-01-02") != latestDate {
			continue
		}
		if line.PreviousClose <= 0 && previousClose > 0 {
			line.PreviousClose = previousClose
		}
		filtered = append(filtered, line)
	}
	if len(filtered) == 0 {
		return lines
	}
	return filtered
}

func (s *Server) news(w http.ResponseWriter, r *http.Request) {
	source := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
	if source == "" {
		source = "cls"
	}
	if source != "cls" {
		writeError(w, http.StatusBadRequest, "unsupported news source")
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	items, err := s.newsProvider.LatestNews(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (s *Server) sectorMapHandler(w http.ResponseWriter, r *http.Request) {
	theme := strings.TrimSpace(r.URL.Query().Get("theme"))
	if theme == "" {
		theme = "semiconductor_materials"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	sectorMap, err := s.sectorMap.Build(ctx, theme)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": sectorMap})
}

func (s *Server) themeOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if s.themeOverview == nil {
		writeError(w, http.StatusServiceUnavailable, "theme overview provider is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	items, meta, err := s.themeOverview.Overviews(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "meta": meta})
}

func (s *Server) inflectionEvaluate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request inflection.EvaluationRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid inflection request: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.inflection.Evaluate(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func writeError(w http.ResponseWriter, status int, message string) {
	if status >= http.StatusInternalServerError {
		log.Printf("level=error event=http_error status=%d message=%q", status, runtimelog.Redact(message))
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
