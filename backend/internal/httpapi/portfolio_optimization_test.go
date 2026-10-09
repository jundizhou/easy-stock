package httpapi

import (
	"context"
	"easy-stock/backend/internal/foundation"
	pi "easy-stock/backend/internal/portfolioinspection"
	po "easy-stock/backend/internal/portfoliooptimization"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOptimizationRoutesAndProvenanceValidation(t *testing.T) {
	store, err := pi.OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{PortfolioStore: store})
	defer s.Close()
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/v1/portfolio-inspections/missing/optimizations", "", 200},
		{"GET", "/api/v1/portfolio-optimizations/missing", "", 404},
		{"POST", "/api/v1/portfolio-inspections/missing/optimizations", `{"unknown":true}`, 400},
		{"POST", "/api/v1/portfolio-optimizations/missing/cancel", "", 409},
		{"POST", "/api/v1/portfolio-inspections", `{"source_optimization_id":"missing","trader_profile":"balanced","horizon":"swing","holdings":[{"symbol":"600519.SH","weight_percent":80}]}`, 400},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	var j po.Job
	j.ID = "snapshot"
	j.SourceID = "old-source"
	j.Fingerprint = "persisted-input"
	j.Status = "succeeded"
	j.Outcome = "unchanged"
	j.ModelPromptBytes = 23144
	j.CheckpointProgress = &po.CheckpointProgress{SavedStocks: 3, TotalStocks: 4, PendingParts: 1}
	j.ModelLoops = map[string]*po.ModelLoopState{"private": {Outputs: []string{"private-saved-output"}, NextPrompt: "private-repair-prompt"}}
	j.ModelPromptVersion = po.ModelPromptVersion
	j.Needs = []string{"补充不同主营盈利角色"}
	j.ModelStageDurationMS = map[string]int64{"assessing": 230000}
	j.Source.Request.Holdings = []pi.Holding{{Symbol: "600519.SH", Weight: 80}}
	raw, _ := json.Marshal(j)
	if err := store.SaveOptimization(context.Background(), j.ID, j.SourceID, j.Fingerprint, raw, time.Now().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	summary := httptest.NewRecorder()
	s.ServeHTTP(summary, httptest.NewRequest(http.MethodGet, "/api/v1/portfolio-inspections/old-source/optimizations", nil))
	if summary.Code != 200 || !strings.Contains(summary.Body.String(), "snapshot") || strings.Contains(summary.Body.String(), "source_report") {
		t.Fatal("history must load metadata, not every research snapshot", summary.Body.String())
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/portfolio-optimizations/snapshot", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "old-source") {
		t.Fatal("stored snapshot broken", w.Body.String())
	}
	progress := httptest.NewRecorder()
	s.ServeHTTP(progress, httptest.NewRequest(http.MethodGet, "/api/v1/portfolio-optimizations/snapshot?view=progress", nil))
	if progress.Code != 200 || strings.Contains(progress.Body.String(), "source_report") || strings.Contains(progress.Body.String(), "research_report") || strings.Contains(progress.Body.String(), "private-") || !strings.Contains(progress.Body.String(), "completed_research") {
		t.Fatal("polling must return only compact progress", progress.Body.String())
	}
	var payload struct {
		Data struct {
			Checkpoint *po.CheckpointProgress `json:"checkpoint_progress"`
			Input      int                    `json:"model_prompt_bytes"`
			Version    string                 `json:"model_prompt_version"`
			Durations  map[string]int64       `json:"model_stage_duration_ms"`
			Needs      []string               `json:"portfolio_needs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(progress.Body.Bytes(), &payload); err != nil || payload.Data.Input != j.ModelPromptBytes || payload.Data.Version != j.ModelPromptVersion || payload.Data.Durations["assessing"] != 230000 {
		t.Fatal("compact progress lost model input or stage budget", err, payload)
	}
	if payload.Data.Checkpoint == nil || payload.Data.Checkpoint.SavedStocks != 3 || payload.Data.Checkpoint.PendingParts != 1 {
		t.Fatal("checkpoint summary lost")
	}
	if len(payload.Data.Needs) != 1 || payload.Data.Needs[0] != j.Needs[0] {
		t.Fatal("compact progress lost portfolio needs")
	}
}

type optimizationScreenFixture struct{ now time.Time }

func (p optimizationScreenFixture) StockCatalog(context.Context) ([]foundation.StockCatalogEntry, error) {
	out := []foundation.StockCatalogEntry{{BoardStock: foundation.BoardStock{Symbol: "600519.SH", Name: "原持仓"}}}
	for i := 1; i <= 10; i++ {
		out = append(out, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: fmt.Sprintf("600%03d.SH", i), Name: fmt.Sprintf("候选%d", i), Amount: float64(100 - i)}, Industry: "测试行业"})
	}
	out = append(out, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: "600099.SH", Name: "*ST测试"}})
	return out, nil
}
func (p optimizationScreenFixture) IndustryStocks(context.Context, string, int) ([]foundation.BoardStock, foundation.SourceMeta, error) {
	entries, _ := p.StockCatalog(context.Background())
	out := []foundation.BoardStock{}
	for _, e := range entries {
		out = append(out, e.BoardStock)
	}
	return out, foundation.SourceMeta{}, nil
}
func (p optimizationScreenFixture) Realtime(_ context.Context, symbols []string) ([]foundation.Quote, error) {
	day, _ := time.ParseInLocation("2006-01-02", po.LatestSession(p.now), time.FixedZone("Asia/Shanghai", 8*3600))
	out := []foundation.Quote{}
	for _, s := range symbols {
		out = append(out, foundation.Quote{Symbol: s, Name: "测试股", Price: 140, Volume: 100, Amount: 14000, TradeTime: day, Meta: foundation.SourceMeta{Source: "sina"}})
	}
	return out, nil
}
func (p optimizationScreenFixture) KLine(_ context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	if period != "day" || limit < 21 {
		panic("screening needs daily history")
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	day, _ := time.ParseInLocation("2006-01-02", po.LatestCompletedSession(p.now), zone)
	bars := make([]foundation.KLine, 21)
	for i := 20; i >= 0; i-- {
		bars[i] = foundation.KLine{Symbol: symbol, Time: day, Close: 100 + float64(i)*2, Volume: 100, Meta: foundation.SourceMeta{Source: "sina"}}
		day = day.AddDate(0, 0, -1)
		for !foundation.IsAStockTradingDay(day) {
			day = day.AddDate(0, 0, -1)
		}
	}
	if symbol == "600002.SH" {
		bars[0].Close = 90
	}
	return bars, nil
}
func (p optimizationScreenFixture) StockBusinessProfile(context.Context, string) (foundation.StockBusinessProfile, error) {
	return foundation.StockBusinessProfile{}, nil
}
func (p optimizationScreenFixture) StockFundamentals(context.Context, string) (foundation.StockFundamentals, error) {
	return foundation.StockFundamentals{}, nil
}
func (p optimizationScreenFixture) StockFinancialHistory(_ context.Context, symbol string, _ int) ([]foundation.StockFundamentals, error) {
	out := []foundation.StockFundamentals{}
	for _, date := range []string{"2026-06-30", "2026-03-31"} {
		pub, _ := time.Parse("2006-01-02", date)
		out = append(out, foundation.StockFundamentals{Symbol: symbol, ReportDate: date, PublishedAt: pub.AddDate(0, 1, 0), Revenue: 1000, RevenueYearOverYear: 10, NetProfit: 100, DeductedNetProfit: 80, DeductedNetProfitAvailable: true, DeductedNetProfitReportDate: date, ROE: 5, OperatingCashFlowPerShare: 1, Meta: foundation.SourceMeta{Source: "test-financial"}})
	}
	if symbol == "600001.SH" {
		out[0].RevenueYearOverYear = -1
	}
	if symbol == "600003.SH" {
		out[0].RevenueYearOverYear = 30
	}
	return out, nil
}

type optimizationIndustryFixture struct {
	fakeMarketOverviewProvider
	hot bool
}

func (p optimizationIndustryFixture) IndustryMomentum(context.Context, int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	five := 3.0
	if p.hot {
		five = 20
	}
	return []foundation.MarketIndustryMomentum{{Code: "fixture", Name: "初启行业", ChangePercent: 1, FiveDayChangePercent: five, TwentyDayChange: 4, Meta: foundation.SourceMeta{Source: "tencent:industry-rank", AvailableFields: []string{"change_percent", "five_day_change_percent", "twenty_day_change_percent"}}}}, foundation.SourceMeta{}, nil
}
func TestCandidateCollectionReplenishesQualifiedPool(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	p := optimizationScreenFixture{now: now}
	s := &Server{stockDirectory: p, stockBusiness: p, realtimeProvider: p, kLinePrimary: p, marketOverview: &optimizationIndustryFixture{}, industryStocks: p}
	u, err := s.collectOptimizationUniverse(context.Background(), pi.Report{Request: pi.Request{Holdings: []pi.Holding{{Symbol: "600519.SH", Weight: 80}}}}, po.Request{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Candidates) != 8 || u.Candidates[0].Symbol != "600003.SH" {
		t.Fatal("bound/ranking", u.Candidates)
	}
	qualified := 0
	for _, c := range u.Candidates {
		if c.Screening == nil {
			t.Fatal("unrecorded code screening")
		}
		if c.Symbol == "600099.SH" || c.Symbol == "600519.SH" {
			t.Fatal("ST or original consumes candidate slot")
		}
		if c.Screening.Qualified {
			qualified++
		}
		if c.Selected {
			t.Fatal("collector must not decide AI outcome")
		}
	}
	if qualified != 8 {
		t.Fatal("risk filters bypassed", qualified)
	}
	if u.ScreeningAudit.Checked != 10 || u.ScreeningAudit.Qualified != 8 || len(u.ScreeningAudit.Records) != 10 || u.ScreeningAudit.Diverse != 1 {
		t.Fatal("audit missing", u.ScreeningAudit)
	}
	if len(u.Quotes) != 9 {
		t.Fatal("retained quote snapshot missing")
	}
	for _, c := range u.Candidates {
		if c.Symbol == "600001.SH" && c.Screening.Qualified {
			t.Fatal("weak growth passed")
		}
		if c.Symbol == "600002.SH" && c.Screening.Qualified {
			t.Fatal("50% return passed")
		}
	}
}
func TestOverheatedIndustryDoesNotFallBackToUnscreenedStocks(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	p := optimizationScreenFixture{now: now}
	s := &Server{stockDirectory: p, stockBusiness: p, realtimeProvider: p, kLinePrimary: p, marketOverview: &optimizationIndustryFixture{hot: true}, industryStocks: p}
	u, err := s.collectOptimizationUniverse(context.Background(), pi.Report{Request: pi.Request{Holdings: []pi.Holding{{Symbol: "600519.SH", Weight: 80}}}}, po.Request{CandidateSymbols: []string{"600001.SH"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Candidates) != 0 || u.ScreeningAudit.Checked != 0 {
		t.Fatal("user override bypassed early industry filter")
	}
}
