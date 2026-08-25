package httpapi

import (
	"context"
	"log"
	"net/http"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/marketemotion"
	"easy-stock/backend/internal/methodology"
	"easy-stock/backend/internal/review"
	"easy-stock/backend/internal/strategy/inflection"
)

type RealtimeProvider interface {
	Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error)
}

type KLineProvider interface {
	KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error)
}

type NewsProvider interface {
	LatestNews(ctx context.Context, limit int) ([]foundation.NewsItem, error)
}

type SectorMapProvider interface {
	Build(ctx context.Context, themeID string) (foundation.SectorMap, error)
}

type SnapshotSectorMapProvider interface {
	BuildSnapshot(ctx context.Context, themeID string, snapshotID string) (foundation.SectorMap, error)
}

type ThemeOverviewProvider interface {
	Overviews(ctx context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error)
}

type ThemeRadarFallback interface {
	SectorMapProvider
	ThemeOverviewProvider
}

type LimitUpProvider interface {
	RecentLimitUps(ctx context.Context, lookbackDays int) ([]foundation.LimitUpEvent, error)
}

type StockThemeAttributionProvider interface {
	StockThemes(ctx context.Context, symbol string, lookbackDays int) ([]foundation.StockThemeAttribution, error)
}

type MarketPoolProvider interface {
	BrokenLimitUpPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error)
	LimitDownPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error)
}

type StockConceptProvider interface {
	StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error)
}

type StockBusinessProfileProvider interface {
	StockBusinessProfile(ctx context.Context, symbol string) (foundation.StockBusinessProfile, error)
	StockFundamentals(ctx context.Context, symbol string) (foundation.StockFundamentals, error)
}

type StockDirectoryProvider interface {
	StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error)
}

type HotStockProvider interface {
	HotStockRanks(ctx context.Context, limit int) []foundation.HotStockRankList
}

type MarketOverviewProvider interface {
	MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error)
	MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error)
	IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error)
	MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error)
	MarketMarginSeries(ctx context.Context, limit int) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error)
	MarketBillboard(ctx context.Context, tradeDate string, limit int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error)
	MarketBillboardDetail(ctx context.Context, symbol string, tradeDate string, reason string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error)
	MarketAnnouncements(ctx context.Context, query string, symbol string, category string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error)
	MarketReports(ctx context.Context, kind string, query string, symbol string, industry string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error)
}

type InflectionEvaluator interface {
	Evaluate(request inflection.EvaluationRequest) (inflection.Evaluation, error)
}

type ReviewImporter interface {
	ImportURL(ctx context.Context, rawURL string) (review.Post, error)
}

type Config struct {
	Token                string
	Realtime             RealtimeProvider
	KLinePrimary         KLineProvider
	KLineFallback        KLineProvider
	News                 NewsProvider
	SectorMap            SectorMapProvider
	ThemeOverview        ThemeOverviewProvider
	ThemeRadarFallback   ThemeRadarFallback
	LimitUp              LimitUpProvider
	MarketPools          MarketPoolProvider
	StockConcept         StockConceptProvider
	StockBusiness        StockBusinessProfileProvider
	StockDirectory       StockDirectoryProvider
	HotStocks            HotStockProvider
	MarketOverview       MarketOverviewProvider
	Inflection           InflectionEvaluator
	ReviewDBPath         string
	MarketEmotionDBPath  string
	ThemeRadarDBPath     string
	DuanxianxiaBaseURL   string
	WeChatAPIURL         string
	ReviewHTTP           *http.Client
	ReviewStore          *review.Store
	MarketEmotionStore   *marketemotion.Store
	ReviewImporter       ReviewImporter
	SettingsPath         string
	SettingsStore        *appsettings.Store
	ReviewAutomation     *review.Automation
	RemoteDailyReviewURL string
	RemoteDailySync      *review.RemoteDailySync
	HermesGateway        hermes.Gateway
	MasteryLibrary       *methodology.Library
	Logger               *log.Logger
	StrictPersistence    bool
}

func normalizeConfig(value any) Config {
	switch cfg := value.(type) {
	case Config:
		return cfg
	case *Config:
		if cfg != nil {
			return *cfg
		}
	}
	return Config{}
}
