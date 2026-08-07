package httpapi

import (
	"context"
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

type MarketPoolProvider interface {
	BrokenLimitUpPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error)
	LimitDownPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error)
}

type StockConceptProvider interface {
	StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error)
}

type InflectionEvaluator interface {
	Evaluate(request inflection.EvaluationRequest) (inflection.Evaluation, error)
}

type ReviewImporter interface {
	ImportURL(ctx context.Context, rawURL string) (review.Post, error)
}

type Config struct {
	Token               string
	Realtime            RealtimeProvider
	KLinePrimary        KLineProvider
	KLineFallback       KLineProvider
	News                NewsProvider
	SectorMap           SectorMapProvider
	ThemeOverview       ThemeOverviewProvider
	ThemeRadarFallback  ThemeRadarFallback
	LimitUp             LimitUpProvider
	MarketPools         MarketPoolProvider
	StockConcept        StockConceptProvider
	Inflection          InflectionEvaluator
	ReviewDBPath        string
	MarketEmotionDBPath string
	ThemeRadarDBPath    string
	DuanxianxiaBaseURL  string
	WeChatAPIURL        string
	ReviewHTTP          *http.Client
	ReviewStore         *review.Store
	MarketEmotionStore  *marketemotion.Store
	ReviewImporter      ReviewImporter
	SettingsPath        string
	SettingsStore       *appsettings.Store
	ReviewAutomation    *review.Automation
	HermesGateway       hermes.Gateway
	MasteryLibrary      *methodology.Library
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
