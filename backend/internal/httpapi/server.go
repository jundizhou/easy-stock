package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/archive"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
	"easy-stock/backend/internal/methodology"
	"easy-stock/backend/internal/notification"
	"easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/portfoliooptimization"
	"easy-stock/backend/internal/providers/cls"
	"easy-stock/backend/internal/providers/duanxianxia"
	"easy-stock/backend/internal/providers/eastmoney"
	futurespositionprovider "easy-stock/backend/internal/providers/futuresposition"
	"easy-stock/backend/internal/providers/hotstock"
	marketoverviewprovider "easy-stock/backend/internal/providers/marketoverview"
	"easy-stock/backend/internal/providers/quant"
	"easy-stock/backend/internal/providers/sina"
	"easy-stock/backend/internal/providers/stockdb"
	"easy-stock/backend/internal/providers/tencent"
	"easy-stock/backend/internal/review"
	"easy-stock/backend/internal/runtimelog"
	"easy-stock/backend/internal/sector"
	"easy-stock/backend/internal/stockanalysis"
	"easy-stock/backend/internal/strategy/inflection"
	"easy-stock/backend/internal/strategy/shannon"
	"easy-stock/backend/internal/themeindex"
)

type Server struct {
	ladderThemeAI            *ladderThemeAI
	mux                      *http.ServeMux
	token                    string
	realtimeProvider         RealtimeProvider
	kLinePrimary             KLineProvider
	kLineFallback            KLineProvider
	newsProvider             NewsProvider
	stockNewsSearch          StockNewsSearchProvider
	sectorMap                SectorMapProvider
	themeOverview            ThemeOverviewProvider
	themeIndex               *themeindex.Service
	limitUpProvider          LimitUpProvider
	marketPools              MarketPoolProvider
	stockConcepts            StockConceptProvider
	stockValuation           StockValuationProvider
	stockBusiness            StockBusinessProfileProvider
	stockFinancialSupplement StockFinancialSupplementProvider
	stockDirectory           StockDirectoryProvider
	industryStocks           IndustryStocksProvider
	hotStockProvider         HotStockProvider
	futuresPosition          FuturesPositionProvider
	marketOverview           MarketOverviewProvider
	// ---- 本地：Free-StockDB / Quant 侧车与归档 ----
	stockDBProvider     *stockdb.Client
	quantProvider       *quant.Client
	realtimeArchiver    *archive.RealtimeArchiver
	newsArchiver        *archive.NewsArchiver
	archiveCollector    ArchiveCollectorConfig
	archiveBaseDir      string
	archiveMu           sync.Mutex
	archiveActiveDir    string
	realtimeProviderRaw RealtimeProvider
	newsProviderRaw     NewsProvider
	inflection          InflectionEvaluator
	shannonFeed         *shannon.Feed
	// ---- 缓存 / 引擎 ----
	themeSnapshots        *themeSnapshotCache
	limitUpSnapshots      *limitUpLadderCache
	limitUpProgress       *shortTermCache[limitUpLadderData]
	emotionProgress       *shortTermCache[marketemotion.History]
	stockDirectories      *stockDirectoryCache
	hotStockRanks         *hotStockRankCache
	marketSnapshots       *marketOverviewCache
	marketEmotion         *marketEmotionEngine
	marketEmotionIntraday *marketEmotionIntradayCache
	reviewStore           *review.Store
	portfolioStore        *portfolioinspection.Store
	portfolioInspection   *portfolioinspection.Service
	portfolioOptimization *portfoliooptimization.Service
	portfolioExpectation  *portfolioinspection.ExpectationService
	stockResearchStore    *stockanalysis.ResearchStore
	stockResearch         *stockanalysis.ResearchService
	reviewImporter        ReviewImporter
	wechatAPIURL          string
	settingsStore         *appsettings.Store
	notificationSender    notificationSender
	notifications         *notification.Dispatcher
	reviewAutomation      *review.Automation
	remoteDailySync       *review.RemoteDailySync
	agentGateway          agent.Gateway
	usageGateway          agent.Gateway
	masteryLibrary        *methodology.Library
	marketEmotionStore    *marketemotion.Store
	themeRadarStore       *duanxianxia.Store
	themeProgress         *themeProgressCache
	startupError          error
	logger                *log.Logger
	tokenUsage            *tokenUsageStore
}

func NewServer(config any) *Server {
	cfg := normalizeConfig(config)
	tokenUsage := newTokenUsageStore(cfg.SettingsPath)
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	var startupErrors []error
	sinaClient := sina.NewClient()
	eastMoneyClient := eastmoney.NewClient()
	tencentClient := tencent.NewClient()
	clsClient := cls.NewClient()
	stockDBProvider := stockdb.NewClient(
		stockdb.WithBaseURL(os.Getenv("A_STOCK_STOCKDB_URL")),
		stockdb.WithCache(30*time.Second),
		// 多接口共用 / 负载均衡 / 故障转移（镜像 quant-stock 的多源抗封设计）：
		// 本地 stockdb 7899 为主源；腾讯 fqkline 与东方财富 push2his 为远程备用源，
		// 三者前复权口径一致。主源降级时远程源按健康 + 轮询分摊流量，任一失败自动
		// 切换到下一个，从架构上消除「单接口被封被限制」的单点风险。
		stockdb.WithDailySource(
			stockdb.NewTencentDailySource(""),
			stockdb.NewEastmoneyDailySource(""),
		),
	)
	quantProvider := quant.NewClient(quant.WithBaseURL(os.Getenv("A_STOCK_QUANT_URL")))
	if cfg.Realtime == nil {
		// 上游 v1.5.0：未显式配置估值源时，默认复用腾讯 client。
		if cfg.StockValuation == nil {
			cfg.StockValuation = tencentClient
		}
		// 本地：多源抗封：sina（主）+ tencent（备）。腾讯 qt.gtimg 实时报价作为
		// sina 的互补第二源，sina 被封/被限流时自动切换并保持 30s 冷却。
		cfg.Realtime = NewMultiRealtime(sinaClient, tencentClient)
	}
	if cfg.KLinePrimary == nil {
		cfg.KLinePrimary = eastMoneyClient
	}
	if cfg.KLineFallback == nil {
		cfg.KLineFallback = sinaClient
	}
	if cfg.News == nil {
		// 上游 v1.5.0：未显式配置个股新闻源时，默认复用东方财富 client。
		if cfg.StockNews == nil {
			cfg.StockNews = eastMoneyClient
		}
		// 本地：多源抗封：cls 财联社快讯（主）+ eastmoney 公告（备）。
		// 公告与快讯互补，单源被封时由另一源顶上。
		cfg.News = NewMultiNews(clsClient, eastMoneyClient)
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
		if cfg.StockFinancialSupplement == nil {
			cfg.StockFinancialSupplement = sinaClient
		}
	}
	if cfg.StockDirectory == nil {
		cfg.StockDirectory = eastMoneyClient
	}
	if cfg.IndustryStocks == nil {
		cfg.IndustryStocks = tencentClient
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
			IndustryStocks:    tencentClient,
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
	if cfg.StockResearchStore == nil {
		store, err := stockanalysis.OpenResearchStore(cfg.StockResearchDBPath)
		if err != nil {
			if cfg.StrictPersistence {
				startupErrors = append(startupErrors, fmt.Errorf("open stock research database: %w", err))
			}
			store, _ = stockanalysis.OpenResearchStore(":memory:")
		}
		cfg.StockResearchStore = store
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
	if cfg.FuturesPosition == nil {
		cfg.FuturesPosition = futurespositionprovider.NewClient()
	}
	if cfg.AgentGateway != nil && (!cfg.StrictPersistence || len(startupErrors) == 0) {
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
		if profileGateway, ok := cfg.AgentGateway.(agent.ProfileGateway); ok {
			if migratedKey == nil {
				if key, err := cfg.AgentGateway.ModelAPIKey(); err == nil && strings.TrimSpace(key) != "" {
					migratedKey = &key
				}
			}
			_ = profileGateway.SyncLLMProfile(values.LLM, values.ActiveLLMProfileID, migratedKey)
		} else {
			_ = cfg.AgentGateway.SyncLLM(values.LLM, migratedKey)
		}
	}
	usageGateway := newTokenUsageGateway(cfg.AgentGateway, tokenUsage)
	if service, ok := cfg.AgentGateway.(*agent.Service); ok {
		service.RestoreSelection(cfg.SettingsStore.Snapshot().AgentRuntime)
	}
	if cfg.ReviewImporter == nil {
		cfg.ReviewImporter = review.NewImporter(cfg.ReviewHTTP, cfg.WeChatAPIURL)
	}
	if cfg.ReviewAutomation == nil {
		cfg.ReviewAutomation = review.NewAutomation(cfg.ReviewStore, cfg.ReviewImporter, cfg.SettingsStore, cfg.ReviewHTTP, cfg.WeChatAPIURL, usageGateway)
	}
	if dailyMarketProvider := newReviewDailyMarketProvider(cfg.MarketOverview); dailyMarketProvider != nil {
		cfg.ReviewAutomation.SetDailyMarketProvider(dailyMarketProvider)
	}
	if cfg.RemoteDailySync == nil {
		cfg.RemoteDailySync = review.NewRemoteDailySync(cfg.ReviewStore, review.RemoteDailySyncConfig{
			BaseURL: cfg.RemoteDailyReviewURL,
			Client:  cfg.ReviewHTTP,
		})
	}
	// 实时报价/资讯落盘：成功拉取后归档到本地单一专用文件夹（默认 <dataDir>/data-archive）。
	// 落盘失败只记录不阻断主流程。
	var realtimeArchiver *archive.RealtimeArchiver
	var newsArchiver *archive.NewsArchiver
	// 记录归档器包装前的原始 provider，供运行时目录热切换（rebindArchivers）复用。
	realtimeProviderRaw := cfg.Realtime
	newsProviderRaw := cfg.News
	if strings.TrimSpace(cfg.ArchiveDir) != "" && cfg.Realtime != nil {
		if ar, err := archive.NewRealtimeArchiver(cfg.Realtime, cfg.ArchiveDir, archive.Options{}); err == nil {
			realtimeArchiver = ar
			cfg.Realtime = ar
		} else if cfg.Logger != nil {
			cfg.Logger.Printf("level=warn event=archive_init kind=realtime err=%q", err.Error())
		}
	}
	if strings.TrimSpace(cfg.ArchiveDir) != "" && cfg.News != nil {
		if ar, err := archive.NewNewsArchiver(cfg.News, cfg.ArchiveDir, archive.Options{}); err == nil {
			newsArchiver = ar
			cfg.News = ar
		} else if cfg.Logger != nil {
			cfg.Logger.Printf("level=warn event=archive_init kind=news err=%q", err.Error())
		}
	}
	s := &Server{
		mux:                      http.NewServeMux(),
		token:                    cfg.Token,
		realtimeProvider:         cfg.Realtime,
		kLinePrimary:             cfg.KLinePrimary,
		kLineFallback:            cfg.KLineFallback,
		newsProvider:             cfg.News,
		stockNewsSearch:          cfg.StockNews,
		sectorMap:                cfg.SectorMap,
		themeOverview:            cfg.ThemeOverview,
		limitUpProvider:          cfg.LimitUp,
		marketPools:              cfg.MarketPools,
		stockConcepts:            cfg.StockConcept,
		stockValuation:           cfg.StockValuation,
		stockBusiness:            cfg.StockBusiness,
		stockFinancialSupplement: cfg.StockFinancialSupplement,
		stockDirectory:           cfg.StockDirectory,
		industryStocks:           cfg.IndustryStocks,
		hotStockProvider:         cfg.HotStocks,
		futuresPosition:          cfg.FuturesPosition,
		marketOverview:           cfg.MarketOverview,
		stockDBProvider:          stockDBProvider,
		quantProvider:            quantProvider,
		realtimeArchiver:         realtimeArchiver,
		newsArchiver:             newsArchiver,
		archiveCollector:         cfg.ArchiveCollector,
		archiveBaseDir:           cfg.ArchiveDir,
		realtimeProviderRaw:      realtimeProviderRaw,
		newsProviderRaw:          newsProviderRaw,
		inflection:               cfg.Inflection,
		shannonFeed:              shannon.NewFeed(stockDBProvider),
		themeSnapshots:           newThemeSnapshotCache(30 * time.Second),
		themeProgress:            newThemeProgressCache(),
		limitUpSnapshots:         newLimitUpLadderCache(30 * time.Second),
		limitUpProgress:          &shortTermCache[limitUpLadderData]{},
		emotionProgress:          &shortTermCache[marketemotion.History]{},
		stockDirectories:         newStockDirectoryCache(6 * time.Hour),
		hotStockRanks:            newHotStockRankCache(2 * time.Minute),
		marketSnapshots:          newMarketOverviewCache(45 * time.Second),
		marketEmotionIntraday:    newMarketEmotionIntradayCache(marketEmotionIntradayTTL),
		reviewStore:              cfg.ReviewStore,
		portfolioStore:           cfg.PortfolioStore,
		stockResearchStore:       cfg.StockResearchStore,
		reviewImporter:           cfg.ReviewImporter,
		wechatAPIURL:             strings.TrimSpace(cfg.WeChatAPIURL),
		settingsStore:            cfg.SettingsStore,
		notificationSender:       notification.NewSender(),
		ladderThemeAI:            newLadderThemeAI(cfg.SettingsPath),
		reviewAutomation:         cfg.ReviewAutomation,
		remoteDailySync:          cfg.RemoteDailySync,
		agentGateway:             cfg.AgentGateway,
		usageGateway:             usageGateway,
		masteryLibrary:           cfg.MasteryLibrary,
		marketEmotionStore:       cfg.MarketEmotionStore,
		startupError:             errors.Join(startupErrors...),
		logger:                   cfg.Logger,
		tokenUsage:               tokenUsage,
	}
	if kaipanlaService != nil {
		s.themeRadarStore = kaipanlaService.Store()
		if payload, err := s.themeRadarStore.LoadOverview(context.Background()); err == nil && len(payload) > 0 {
			var cached foundation.ThemeProgress
			if json.Unmarshal(payload, &cached) == nil {
				cached.Refreshing = false
				cached.Meta.Stale = true
				s.themeProgress.value = cached
			}
		}
	}
	if s.themeRadarStore != nil {
		if payload, err := s.themeRadarStore.LoadLadder(context.Background()); err == nil && len(payload) > 0 {
			var cached shortTermProgress[limitUpLadderData]
			if json.Unmarshal(payload, &cached) == nil && cached.Data != nil {
				cached.Refreshing, cached.Stale = false, true
				cached.Data.Meta.Stale = true
				if cached.Data.Intraday != nil {
					cached.Data.Intraday.Stale = true
				}
				s.limitUpProgress.value = cached
			}
		}
	}
	s.marketEmotion = newMarketEmotionEngine(
		cfg.MarketEmotionStore,
		s.limitUpProvider,
		s.marketPools,
		s.kLinePrimary,
		s.kLineFallback,
		s.stockConcepts,
	)
	s.notifications = notification.NewDispatcher(func(ctx context.Context, channel string, cfg appsettings.NotificationChannel, message notification.Message) error {
		return s.notificationSender.Send(ctx, channel, cfg, message)
	}, func() appsettings.Notifications { return s.settingsStore.Snapshot().Notifications }, cfg.Logger)
	s.stockResearch = stockanalysis.NewResearchServiceWithPreparation(cfg.StockResearchStore, s.runStockResearch, s.prepareStockResearch, s.notifyStockResearch)
	s.portfolioInspection = portfolioinspection.NewService(cfg.PortfolioStore, usageGateway, s.analyzeStock, cfg.Logger, s.analyzeHoldingResearch)
	s.portfolioInspection.ConfigureCompletion(s.notifyPortfolioInspection)
	s.portfolioInspection.ConfigureResearch(s.resolvePortfolioResearch, s.refreshPortfolioQuotes)
	s.portfolioOptimization = portfoliooptimization.NewService(cfg.PortfolioStore, usageGateway, portfoliooptimization.Dependencies{Collect: s.collectOptimizationUniverse, Research: s.resolvePortfolioResearch, Quotes: s.refreshOptimizationQuotes})
	s.portfolioExpectation = portfolioinspection.NewExpectationService(cfg.PortfolioStore, cfg.ReviewStore, usageGateway, s.analyzeStock, cfg.Logger, s.analyzeHoldingResearch)
	s.themeIndex = cfg.ThemeIndex
	if s.themeIndex == nil {
		history := &themeIndexHistory{primary: eastMoneyClient, fallback: tencentClient}
		s.themeIndex = themeindex.New(themeindex.Config{
			Boards: eastMoneyClient,
			Resolve: func(ctx context.Context, theme, snapshot string) (foundation.ThemeIndexTarget, error) {
				if resolver, ok := s.sectorMap.(interface {
					ResolveThemeIndex(context.Context, string, string) (foundation.ThemeIndexTarget, error)
				}); ok {
					return resolver.ResolveThemeIndex(ctx, theme, snapshot)
				}
				value, err := s.loadThemeIndexMembers(ctx, theme, snapshot)
				return foundation.ThemeIndexTarget{Name: value.Name, Names: []string{value.Name}}, err
			},
			Members: s.loadThemeIndexMembers,
			KLines:  history.load,
		})
	}
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
	if s != nil && s.themeProgress != nil {
		s.themeProgress.close()
	}
	if s == nil {
		return nil
	}
	if s.limitUpProgress != nil {
		s.limitUpProgress.close()
	}
	if s.emotionProgress != nil {
		s.emotionProgress.close()
	}
	var closeErrors []error
	if s.notifications != nil {
		s.notifications.Close()
	}
	if s.portfolioOptimization != nil {
		s.portfolioOptimization.Close()
	}
	if s.portfolioInspection != nil {
		s.portfolioInspection.Close()
	}
	if s.stockResearch != nil {
		s.stockResearch.Close()
	}
	if s.stockResearchStore != nil {
		closeErrors = append(closeErrors, s.stockResearchStore.Close())
	}
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
	s.mux.HandleFunc("GET /api/v1/archive/status", s.archiveStatus)
	s.mux.HandleFunc("POST /api/v1/archive/collect", s.archiveCollectNow)
	s.mux.HandleFunc("GET /api/v1/archive/config", s.archiveConfigGet)
	s.mux.HandleFunc("PUT /api/v1/archive/config", s.archiveConfigUpdate)
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
	s.mux.HandleFunc("GET /api/v1/market/futures-position", s.marketFuturesPositionHandler)
	s.mux.HandleFunc("GET /api/v1/market/futures-members", s.marketFuturesMembersHandler)
	s.mux.HandleFunc("GET /api/v1/market/futures-consensus", s.marketFuturesConsensusHandler)
	s.mux.HandleFunc("GET /api/v1/research/announcements", s.marketAnnouncementsHandler)
	s.mux.HandleFunc("GET /api/v1/research/institution-reports", s.marketInstitutionReportsHandler)
	s.mux.HandleFunc("GET /api/v1/research/industries", s.marketIndustryResearchHandler)
	s.mux.HandleFunc("GET /api/v1/themes/overview", s.themeOverviewHandler)
	s.mux.HandleFunc("GET /api/v1/themes/index", s.themeIndexHandler)
	s.mux.HandleFunc("GET /api/v1/themes/screen", s.themeScreenHandler)
	s.mux.HandleFunc("GET /api/v1/sector-map", s.sectorMapHandler)
	s.mux.HandleFunc("GET /api/v1/short-term/limit-up-ladder", s.limitUpLadderHandler)
	s.mux.HandleFunc("POST /api/v1/short-term/ladder-theme-ai", s.ladderThemeAIIdentify)
	s.mux.HandleFunc("GET /api/v1/short-term/ladder-theme-ai", s.ladderThemeAIResults)
	s.mux.HandleFunc("GET /api/v1/short-term/emotion-history", s.marketEmotionHistoryHandler)
	s.mux.HandleFunc("GET /api/v1/short-term/mastery", s.masteryIndex)
	s.mux.HandleFunc("GET /api/v1/short-term/mastery/trader", s.masteryTrader)
	s.mux.HandleFunc("POST /api/v1/short-term/mastery/refresh", s.masteryRefresh)
	s.mux.HandleFunc("POST /api/v1/stocks/ai-analysis", s.stockAIAnalysis)
	s.mux.HandleFunc("POST /api/v1/stocks/research", s.stockResearchCreate)
	s.mux.HandleFunc("GET /api/v1/stocks/research", s.stockResearchList)
	s.mux.HandleFunc("GET /api/v1/stocks/research/{id}", s.stockResearchGet)
	s.mux.HandleFunc("DELETE /api/v1/stocks/research/{id}", s.stockResearchDelete)
	s.mux.HandleFunc("POST /api/v1/stocks/research/{id}/cancel", s.stockResearchCancel)
	s.mux.HandleFunc("POST /api/v1/stocks/research/{id}/resume", s.stockResearchResume)
	s.mux.HandleFunc("POST /api/v1/stocks/research/{id}/verify", s.stockResearchVerify)
	s.mux.HandleFunc("GET /api/v1/stocks/research/{id}/snapshot", s.stockResearchSnapshot)
	s.mux.HandleFunc("GET /api/v1/stocks/directory", s.stockDirectoryHandler)
	s.mux.HandleFunc("GET /api/v1/stocks/hot-ranks", s.hotStockRanksHandler)

	// ---- 上游 v1.5.0：AI 优化持仓 ----
	s.mux.HandleFunc("POST /api/v1/portfolio-inspections/{id}/optimizations", s.portfolioOptimizationCreate)
	s.mux.HandleFunc("GET /api/v1/portfolio-inspections/{id}/optimizations", s.portfolioOptimizationList)
	s.mux.HandleFunc("GET /api/v1/portfolio-optimizations/{id}", s.portfolioOptimizationGet)
	s.mux.HandleFunc("POST /api/v1/portfolio-optimizations/{id}/cancel", s.portfolioOptimizationCancel)
	s.mux.HandleFunc("POST /api/v1/portfolio-optimizations/{id}/resume", s.portfolioOptimizationResume)

	// ---- Free-StockDB 数据/计算底座 (7899) ----
	s.mux.HandleFunc("GET /api/v1/stockdb/query", s.stockDBQuery)
	s.mux.HandleFunc("GET /api/v1/stockdb/kline", s.stockDBKLine)
	s.mux.HandleFunc("GET /api/v1/stockdb/adjustment", s.stockDBAdjustment)
	s.mux.HandleFunc("GET /api/v1/stockdb/codes", s.stockDBCodes)
	s.mux.HandleFunc("GET /api/v1/stockdb/status", s.stockDBStatus)
	s.mux.HandleFunc("GET /api/v1/stockdb/symbol-boards", s.stockDBSymbolBoards)

	// ---- 运行时 sidecar 配置（前端 iframe 直挂用） ----
	s.mux.HandleFunc("GET /api/v1/runtime/sidecars", s.runtimeSidecars)

	// ---- Quant Terminal 策略层 (8766) ----
	s.mux.HandleFunc("GET /api/v1/quant/proxy", s.quantProxy)
	s.mux.HandleFunc("GET /api/v1/quant/kline", s.quantKLine)
	s.mux.HandleFunc("GET /api/v1/quant/sys", s.quantSys)
	s.mux.HandleFunc("GET /api/v1/quant/hub-status", s.quantHubStatus)
	s.mux.HandleFunc("GET /api/v1/quant/track", s.quantTrackHandler)

	s.mux.HandleFunc("GET /api/v1/portfolio-inspections", s.portfolioInspectionList)
	s.mux.HandleFunc("POST /api/v1/portfolio-inspections", s.portfolioInspectionCreate)
	s.mux.HandleFunc("GET /api/v1/portfolio-inspections/{id}", s.portfolioInspectionGet)
	s.mux.HandleFunc("POST /api/v1/portfolio-inspections/{id}/resume", s.portfolioInspectionResume)
	s.mux.HandleFunc("POST /api/v1/portfolio-inspections/{id}/cancel", s.portfolioInspectionCancel)
	s.mux.HandleFunc("POST /api/v1/reviews/portfolio-expectations", s.portfolioExpectationCreate)
	s.mux.HandleFunc("GET /api/v1/reviews/portfolio-expectations/latest", s.portfolioExpectationLatest)
	s.mux.HandleFunc("GET /api/v1/reviews/portfolio-expectations/{id}", s.portfolioExpectationGet)
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
	s.mux.HandleFunc("GET /api/v1/settings/token-usage", s.tokenUsageSummary)
	s.mux.HandleFunc("POST /api/v1/settings/token-usage", s.tokenUsageRecord)
	s.mux.HandleFunc("PUT /api/v1/settings", s.settingsUpdate)
	s.mux.HandleFunc("GET /api/v1/settings/agent", s.settingsAgentGet)
	s.mux.HandleFunc("PUT /api/v1/settings/agent", s.settingsAgentUpdate)
	s.mux.HandleFunc("POST /api/v1/settings/agent/skills/import", s.settingsAgentSkillImport)
	s.mux.HandleFunc("POST /api/v1/settings/agent/skills/delete", s.settingsAgentSkillDelete)
	s.mux.HandleFunc("POST /api/v1/settings/agent/skills/install-git", s.settingsAgentSkillInstallGit)
	s.mux.HandleFunc("GET /api/v1/settings/agent/skills/market", s.settingsAgentSkillMarket)
	s.mux.HandleFunc("GET /api/v1/settings/agent/skills/market/sources", s.settingsAgentSkillMarketSources)
	s.mux.HandleFunc("POST /api/v1/settings/llm/models", s.settingsLLMModels)
	s.mux.HandleFunc("POST /api/v1/settings/llm/api-key/reveal", s.settingsLLMAPIKeyReveal)
	s.mux.HandleFunc("POST /api/v1/settings/llm/test", s.settingsLLMTest)
	s.mux.HandleFunc("GET /api/v1/settings/notifications", s.settingsNotificationsGet)
	s.mux.HandleFunc("PUT /api/v1/settings/notifications", s.settingsNotificationsUpdate)
	s.mux.HandleFunc("POST /api/v1/settings/notifications/test", s.settingsNotificationsTest)
	s.mux.HandleFunc("GET /api/v1/ai/ws", s.aiChatWebSocket)
	s.mux.HandleFunc("POST /api/v1/strategy/inflections/evaluate", s.inflectionEvaluate)
	s.mux.HandleFunc("POST /api/v1/strategy/inflections/auto", s.inflectionAutoHandler)
	s.mux.HandleFunc("GET /api/v1/strategy/core2/paper", s.core2PaperHandler)
	s.mux.HandleFunc("GET /api/v1/strategy/resonance/board", s.resonanceBoardHandler)
	s.mux.HandleFunc("POST /api/v1/strategy/shannon/scan", s.shannonScan)
	s.mux.HandleFunc("POST /api/v1/strategy/shannon/analyze", s.shannonAnalyze)
	s.mux.HandleFunc("GET /api/v1/strategy/shannon/boards", s.shannonBoards)
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
	sources := []foundation.SourceHealth{
		{ID: "duanxianxia", Name: "短线侠 / 开盘啦", Category: "theme,leaders,limit-up,concept", OK: true, CheckedAt: time.Now()},
		{ID: "eastmoney", Name: "东方财富", Category: "quote,kline,f10,report,news", OK: true, CheckedAt: time.Now()},
		{ID: "sina", Name: "新浪财经", Category: "quote,kline,money-flow", OK: true, CheckedAt: time.Now()},
		{ID: "tencent", Name: "腾讯财经", Category: "quote,index,hk", OK: true, CheckedAt: time.Now()},
		{ID: "cls", Name: "财联社", Category: "news,calendar", OK: true, CheckedAt: time.Now()},
		{ID: "tradingview", Name: "TradingView", Category: "news", OK: true, CheckedAt: time.Now()},
		{ID: "tushare", Name: "Tushare", Category: "basic,daily,index", OK: false, Message: "requires token", CheckedAt: time.Now()},
	}
	// 暴露实时报价 / 资讯多源子源的健康（抗封冷却状态）
	if mr, ok := s.realtimeProvider.(*multiRealtime); ok {
		sources = append(sources, mr.Statuses()...)
	}
	if mn, ok := s.newsProvider.(*multiNews); ok {
		sources = append(sources, mn.Statuses()...)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sources": sources,
	})
}

// archiveStatus 暴露实时报价/资讯的本地归档目录、文件数、累计行数与最近写入时间。
//
//	GET /api/v1/archive/status
func (s *Server) archiveStatus(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{"enabled": s.realtimeArchiver != nil || s.newsArchiver != nil}
	if s.realtimeArchiver != nil {
		payload["realtime"] = s.realtimeArchiver.Report()
	}
	if s.newsArchiver != nil {
		payload["news"] = s.newsArchiver.Report()
	}
	cfg := s.archiveCollector
	payload["collector"] = map[string]any{
		"interval_seconds": int(cfg.Interval.Seconds()),
		"symbols":          cfg.Symbols,
		"symbol_count":     len(cfg.Symbols),
		"news_limit":       cfg.NewsLimit,
		"trading_only":     cfg.TradingOnly,
	}
	writeJSON(w, http.StatusOK, payload)
}

// archiveCollectNow 手动触发一次采集（忽略交易时段限制），用于即时验证/补采。
//
//	POST /api/v1/archive/collect
func (s *Server) archiveCollectNow(w http.ResponseWriter, r *http.Request) {
	if s.realtimeArchiver == nil && s.newsArchiver == nil {
		writeError(w, http.StatusServiceUnavailable, "archive is not enabled")
		return
	}
	cfg := s.archiveCollector
	cfg.TradingOnly = false // 手动触发强制采集
	s.collectOnce(r.Context(), cfg)
	payload := map[string]any{"ok": true}
	if s.realtimeArchiver != nil {
		payload["realtime"] = s.realtimeArchiver.Report()
	}
	if s.newsArchiver != nil {
		payload["news"] = s.newsArchiver.Report()
	}
	writeJSON(w, http.StatusOK, payload)
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
	if r.URL.Query().Get("delivery") == "progressive" {
		s.progressiveThemeOverview(w, r)
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

func (s *Server) shannonScan(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request shannon.ScanRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid shannon scan request: "+err.Error())
		return
	}
	ctx := r.Context()
	resp, err := s.shannonFeed.Scan(ctx, request, shannon.DefaultConfig())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (s *Server) shannonAnalyze(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request struct {
		Code   string  `json:"code"`
		Equity float64 `json:"equity"`
	}
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid shannon analyze request: "+err.Error())
		return
	}
	if strings.TrimSpace(request.Code) == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	ctx := r.Context()
	result, msg := s.shannonFeed.ScanOne(ctx, strings.TrimSpace(request.Code), shannon.DefaultConfig(), request.Equity)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) shannonBoards(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	boards, err := s.shannonFeed.ListBoards(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": boards})
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
