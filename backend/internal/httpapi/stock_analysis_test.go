package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestStockAIAnalysisEndpointBuildsTrendProfile(t *testing.T) {
	server := NewServer(Config{
		Realtime:       stockAnalysisRealtime{},
		KLinePrimary:   stockAnalysisKLines{},
		KLineFallback:  stockAnalysisKLines{},
		LimitUp:        stockAnalysisLimitUps{},
		StockConcept:   stockAnalysisCatalog{},
		SectorMap:      fakeSectorMapProvider{},
		ThemeOverview:  stockAnalysisThemes{},
		News:           stockAnalysisNews{},
		ReviewDBPath:   ":memory:",
		SettingsPath:   "",
		MasteryLibrary: nil,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stocks/ai-analysis", strings.NewReader(`{"symbol":"600519"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			Symbol string `json:"symbol"`
			Theme  struct {
				Primary string `json:"primary"`
				Source  string `json:"source"`
				Route   string `json:"route"`
			} `json:"theme"`
			Profile struct {
				PrimaryType string `json:"primary_type"`
			} `json:"profile"`
			Trend struct {
				Score int `json:"score"`
			} `json:"trend"`
			Scorecard struct {
				Overall int `json:"overall"`
			} `json:"scorecard"`
			Timeframes []struct {
				Key string `json:"key"`
			} `json:"timeframes"`
			Relative struct {
				Available       bool   `json:"available"`
				BenchmarkSymbol string `json:"benchmark_symbol"`
			} `json:"relative_strength"`
			StockNews struct {
				Available    bool `json:"available"`
				ArticleCount int  `json:"article_count"`
			} `json:"stock_news"`
			ThemeNews struct {
				Available    bool `json:"available"`
				ArticleCount int  `json:"article_count"`
			} `json:"theme_news"`
			NextDay struct {
				Scenarios []struct {
					Key string `json:"key"`
				} `json:"scenarios"`
			} `json:"next_day"`
			RiskControl struct {
				StopPrice float64 `json:"stop_price"`
			} `json:"risk_control"`
			AI struct {
				Status string `json:"status"`
			} `json:"ai"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Symbol != "600519.SH" || payload.Data.Profile.PrimaryType != "trend_capacity" || payload.Data.Trend.Score < 68 {
		t.Fatalf("unexpected analysis payload: %+v", payload.Data)
	}
	if payload.Data.Theme.Primary != "白酒消费" || !strings.Contains(payload.Data.Theme.Source, "kaipanla-theme-leader") || payload.Data.Theme.Route != "trend" {
		t.Fatalf("cached stock theme attribution missing: %+v", payload.Data.Theme)
	}
	if payload.Data.AI.Status != "unavailable" {
		t.Fatalf("AI status = %q, want unavailable", payload.Data.AI.Status)
	}
	if payload.Data.Scorecard.Overall <= 0 || len(payload.Data.Timeframes) != 5 || !payload.Data.Relative.Available || payload.Data.Relative.BenchmarkSymbol != "000001.SH" {
		t.Fatalf("complete analysis dimensions missing: %+v", payload.Data)
	}
	if len(payload.Data.NextDay.Scenarios) != 4 || payload.Data.RiskControl.StopPrice <= 0 {
		t.Fatalf("decision and risk plans missing: %+v", payload.Data)
	}
	if !payload.Data.StockNews.Available || payload.Data.StockNews.ArticleCount < 1 || !payload.Data.ThemeNews.Available || payload.Data.ThemeNews.ArticleCount < 1 {
		t.Fatalf("news analysis missing: stock=%+v theme=%+v", payload.Data.StockNews, payload.Data.ThemeNews)
	}
}

func TestStockAIAnalysisEndpointSupportsNewListingWithOneKLine(t *testing.T) {
	server := NewServer(Config{
		Realtime:      stockAnalysisNewListingRealtime{},
		KLinePrimary:  stockAnalysisNewListingKLines{},
		KLineFallback: stockAnalysisNewListingKLines{},
		ReviewDBPath:  ":memory:",
		SettingsPath:  "",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stocks/ai-analysis", strings.NewReader(`{"symbol":"688836"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			Profile struct {
				PrimaryType string `json:"primary_type"`
			} `json:"profile"`
			Trend struct {
				HistoryDays int     `json:"history_days"`
				MA20        float64 `json:"ma20"`
			} `json:"trend"`
			RiskControl struct {
				StopPrice            float64 `json:"stop_price"`
				SuggestedPositionMax int     `json:"suggested_position_max_percent"`
			} `json:"risk_control"`
			DataQuality []struct {
				Key    string `json:"key"`
				Status string `json:"status"`
			} `json:"data_quality"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Profile.PrimaryType != "new_listing" || payload.Data.Trend.HistoryDays != 1 || payload.Data.Trend.MA20 != 0 {
		t.Fatalf("unexpected new-listing response: %+v", payload.Data)
	}
	if payload.Data.RiskControl.StopPrice <= 0 || payload.Data.RiskControl.SuggestedPositionMax > 10 {
		t.Fatalf("new-listing risk controls missing: %+v", payload.Data.RiskControl)
	}
	if len(payload.Data.DataQuality) == 0 || payload.Data.DataQuality[0].Key != "kline" || payload.Data.DataQuality[0].Status != "limited" {
		t.Fatalf("new-listing data quality missing: %+v", payload.Data.DataQuality)
	}
}

type stockAnalysisRealtime struct{}

func (stockAnalysisRealtime) Realtime(context.Context, []string) ([]foundation.Quote, error) {
	return []foundation.Quote{{
		Symbol: "600519.SH", Name: "贵州茅台", Price: 25.2, ChangePercent: 1.8,
		Meta: foundation.SourceMeta{Source: "test", FetchedAt: time.Now()},
	}}, nil
}

type stockAnalysisNewListingRealtime struct{}

func (stockAnalysisNewListingRealtime) Realtime(context.Context, []string) ([]foundation.Quote, error) {
	return []foundation.Quote{{Symbol: "688836.SH", Name: "测试新股", Price: 88, ChangePercent: 22.22, Meta: foundation.SourceMeta{Source: "test", FetchedAt: time.Now()}}}, nil
}

type stockAnalysisNewListingKLines struct{}

func (stockAnalysisNewListingKLines) KLine(_ context.Context, symbol, _ string, _ int) ([]foundation.KLine, error) {
	return []foundation.KLine{{Symbol: symbol, Time: time.Date(2026, 8, 18, 0, 0, 0, 0, time.Local), Open: 72, High: 96, Low: 70, Close: 88, Volume: 42_000_000, Amount: 920_000_000, TurnoverRate: 58, ChangePercent: 22.22, Meta: foundation.SourceMeta{Source: "test", FetchedAt: time.Now()}}}, nil
}

type stockAnalysisKLines struct{}

func (stockAnalysisKLines) KLine(_ context.Context, symbol, _ string, limit int) ([]foundation.KLine, error) {
	items := make([]foundation.KLine, 0, min(limit, 180))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for index := 0; index < min(limit, 180); index++ {
		closePrice := 10 + float64(index)*0.085
		items = append(items, foundation.KLine{
			Symbol: symbol, Time: base.AddDate(0, 0, index), Open: closePrice - .05,
			High: closePrice + .2, Low: closePrice - .2, Close: closePrice,
			Volume: 40_000_000, Amount: 1_600_000_000, TurnoverRate: 3.2,
			Meta: foundation.SourceMeta{Source: "test", FetchedAt: time.Now()},
		})
	}
	return items, nil
}

type stockAnalysisLimitUps struct{}

func (stockAnalysisLimitUps) RecentLimitUps(context.Context, int) ([]foundation.LimitUpEvent, error) {
	return nil, nil
}

func (stockAnalysisLimitUps) StockThemes(context.Context, string, int) ([]foundation.StockThemeAttribution, error) {
	return []foundation.StockThemeAttribution{{
		Symbol: "600519.SH", Theme: "白酒消费", Source: "duanxianxia:kaipanla-theme-leader", TradeDate: "2026-08-07", Role: "龙一",
	}}, nil
}

type stockAnalysisCatalog struct{}

func (stockAnalysisCatalog) StockCatalog(context.Context) ([]foundation.StockCatalogEntry, error) {
	return []foundation.StockCatalogEntry{{
		BoardStock: foundation.BoardStock{Symbol: "600519.SH", Name: "贵州茅台"},
		Industry:   "食品饮料", Concepts: []string{"消费"},
	}}, nil
}

type stockAnalysisThemes struct{}

func (stockAnalysisThemes) Overviews(context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	return []foundation.ThemeOverview{{Theme: "consumer", Name: "消费", TrendScore: 72, TrendStage: "趋势推进", ActiveDays: 9}}, foundation.SourceMeta{Source: "test"}, nil
}

type stockAnalysisNews struct{}

func (stockAnalysisNews) LatestNews(context.Context, int) ([]foundation.NewsItem, error) {
	return []foundation.NewsItem{
		{ID: "stock-news", Title: "贵州茅台推进渠道合作", Content: "公司订单增长。", PublishedAt: time.Now(), Meta: foundation.SourceMeta{Source: "cls"}},
		{ID: "theme-news", Title: "白酒消费迎来政策支持", Content: "板块景气预期改善。", PublishedAt: time.Now().Add(-time.Hour), Meta: foundation.SourceMeta{Source: "cls"}},
	}, nil
}
