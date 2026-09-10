package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
)

func TestStockAIAnalysisEndpointBuildsTrendProfile(t *testing.T) {
	var logs bytes.Buffer
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
		Logger:         log.New(&logs, "", 0),
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
	if len(payload.Data.NextDay.Scenarios) != 0 || payload.Data.RiskControl.StopPrice != 0 {
		t.Fatalf("quantitative fallback must not masquerade as an AI execution plan: %+v", payload.Data)
	}
	if !payload.Data.StockNews.Available || payload.Data.StockNews.ArticleCount < 1 || !payload.Data.ThemeNews.Available || payload.Data.ThemeNews.ArticleCount < 1 {
		t.Fatalf("news analysis missing: stock=%+v theme=%+v", payload.Data.StockNews, payload.Data.ThemeNews)
	}
	logOutput := logs.String()
	for _, stage := range []string{"data_collection", "local_analysis"} {
		if !strings.Contains(logOutput, `event=stock_analysis_stage`) || !strings.Contains(logOutput, `stage="`+stage+`"`) {
			t.Fatalf("stock analysis stage %q missing from logs: %s", stage, logOutput)
		}
	}
	if !strings.Contains(logOutput, "duration_ms=") || strings.Contains(logOutput, `stage="synthesizing"`) {
		t.Fatalf("stage duration or skip status missing from logs: %s", logOutput)
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
	if payload.Data.RiskControl.StopPrice != 0 || payload.Data.RiskControl.SuggestedPositionMax != 0 {
		t.Fatalf("unresearched limited sample produced execution advice: %+v", payload.Data.RiskControl)
	}
	if len(payload.Data.DataQuality) == 0 || payload.Data.DataQuality[0].Key != "kline" || payload.Data.DataQuality[0].Status != "limited" {
		t.Fatalf("new-listing data quality missing: %+v", payload.Data.DataQuality)
	}
}

func TestStockAIAnalysisQuickModeSkipsAllModelCalls(t *testing.T) {
	gateway := &fakeHermesGateway{
		status: hermes.Status{Available: true, Configured: true, APIKeyConfigured: true},
		promptFunc: func(context.Context, string) (hermes.PromptResult, error) {
			return hermes.PromptResult{}, errors.New("quick mode must not call the model")
		},
	}
	server := NewServer(Config{
		Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{},
		LimitUp: stockAnalysisLimitUps{}, StockConcept: stockAnalysisCatalog{}, StockBusiness: stockAnalysisBusiness{},
		ThemeOverview: stockAnalysisThemes{}, News: stockAnalysisNews{}, MarketOverview: &fakeMarketOverviewProvider{},
		ReviewDBPath: ":memory:", SettingsPath: "", HermesGateway: gateway,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stocks/ai-analysis", strings.NewReader(`{"symbol":"600519","mode":"quick"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			AI struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"ai"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.AI.Status != "rules" || !strings.Contains(payload.Data.AI.Message, "快速分析") {
		t.Fatalf("quick mode AI status = %+v", payload.Data.AI)
	}
	if len(gateway.prompts) != 0 || len(gateway.promptOptions) != 0 {
		t.Fatalf("quick mode reached Hermes: prompts=%d options=%d", len(gateway.prompts), len(gateway.promptOptions))
	}
}

func TestStockAIAnalysisFullModeUsesCurrentModelWithToolFreeStages(t *testing.T) {
	var logs bytes.Buffer
	gateway := &fakeHermesGateway{
		status: hermes.Status{Available: true, Configured: true, APIKeyConfigured: true},
		promptFunc: func(_ context.Context, prompt string) (hermes.PromptResult, error) {
			if strings.Contains(prompt, "独立提出需要核实的问题") {
				return hermes.PromptResult{Content: `{"questions":[],"hypotheses":[],"missing_facts":[]}`}, nil
			}
			return hermes.PromptResult{Content: validHTTPResearchJSON}, nil
		},
	}
	server := NewServer(Config{
		Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{},
		LimitUp: stockAnalysisLimitUps{}, StockConcept: stockAnalysisCatalog{}, StockBusiness: stockAnalysisBusiness{},
		ThemeOverview: stockAnalysisThemes{}, News: stockAnalysisNews{}, MarketOverview: &fakeMarketOverviewProvider{},
		ReviewDBPath: ":memory:", SettingsPath: "", HermesGateway: gateway, Logger: log.New(&logs, "", 0),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stocks/ai-analysis", strings.NewReader(`{"symbol":"600519","mode":"full"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			Conclusion struct {
				Headline string `json:"headline"`
			} `json:"conclusion"`
			AI struct {
				Status string `json:"status"`
			} `json:"ai"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.AI.Status != "ready" || payload.Data.Conclusion.Headline != "完整研判完成" {
		t.Fatalf("full mode result = %+v", payload.Data)
	}
	if len(gateway.prompts) != 3 || len(gateway.promptOptions) != 3 {
		t.Fatalf("full mode calls = prompts:%d options:%d, want three stages", len(gateway.prompts), len(gateway.promptOptions))
	}
	for _, options := range gateway.promptOptions {
		if !options.DisableTools || !options.Sandbox || !options.AutoApprove || len(options.Toolsets) != 0 {
			t.Fatalf("full mode did not inherit the current model in a tool-free sandbox: %+v", options)
		}
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, `stage="researching"`) || !strings.Contains(logOutput, `stage="synthesizing"`) || !strings.Contains(logOutput, `stage="validating"`) {
		t.Fatalf("research diagnostics missing: %s", logOutput)
	}
}

const validHTTPResearchJSON = `{"headline":"完整研判完成","thesis":{"text":"量价结构需要进一步验证","kind":"inference","source_ids":["m-price"]},"support":[{"text":"统计来自历史日线","kind":"fact","source_ids":["m-price"]},{"text":"实时价格单独记录","kind":"fact","source_ids":["m-quote"]}],"counter":[],"alternatives":[],"main_conflict":"历史趋势不等同于未来表现","evidence_level":"limited","limitations":[],"conditions":[{"id":"c1","text":"等待后续公告","metric":"disclosure","window":"next_disclosure","source_ids":["m-price"]}],"invalidation_ids":["c1"],"scenarios":[],"decision":{"status":"no_plan","mode":"non_short","horizon":"swing","new_position":"观察","existing_position":"核实风险","reason":"缺少直接事件证据","price_plan":null},"baseline_relation":"insufficient","baseline_reason":"基线不包含未来事件"}`

func TestStockAIAnalysisRejectsUnknownMode(t *testing.T) {
	server := NewServer(Config{ReviewDBPath: ":memory:", SettingsPath: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stocks/ai-analysis", strings.NewReader(`{"symbol":"600519","mode":"flash"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "quick or full") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
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

type stockAnalysisBusiness struct{}

func (stockAnalysisBusiness) StockBusinessProfile(context.Context, string) (foundation.StockBusinessProfile, error) {
	return foundation.StockBusinessProfile{MainBusiness: "白酒生产与销售", Description: "公司从事白酒产品生产和销售", Meta: foundation.SourceMeta{Source: "test:business"}}, nil
}

func (stockAnalysisBusiness) StockFundamentals(context.Context, string) (foundation.StockFundamentals, error) {
	return foundation.StockFundamentals{ReportDate: "2026-06-30", Revenue: 100, NetProfit: 30, ROE: 18, GrossMargin: 80, Meta: foundation.SourceMeta{Source: "test:fundamental"}}, nil
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
