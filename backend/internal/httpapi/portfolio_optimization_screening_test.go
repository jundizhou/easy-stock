package httpapi

import (
	"context"
	"easy-stock/backend/internal/foundation"
	pi "easy-stock/backend/internal/portfolioinspection"
	po "easy-stock/backend/internal/portfoliooptimization"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type expandedScreenFixture struct {
	optimizationScreenFixture
	fakeMarketOverviewProvider
	total, failFirst int
	active, maximum  atomic.Int32
	slowAfter        int
	singleBoard      bool
}

func (p *expandedScreenFixture) StockCatalog(context.Context) ([]foundation.StockCatalogEntry, error) {
	out := []foundation.StockCatalogEntry{{BoardStock: foundation.BoardStock{Symbol: "600519.SH", Name: "原股"}}}
	for i := 0; i < p.total; i++ {
		out = append(out, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: fmt.Sprintf("600%03d.SH", 100+i), Name: "候选", Amount: float64(1000 - i)}, Industry: fmt.Sprintf("行业%d", i%8)})
	}
	return out, nil
}
func (p *expandedScreenFixture) IndustryMomentum(context.Context, int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	out := []foundation.MarketIndustryMomentum{}
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("板块%d", i)
		if p.singleBoard {
			name = "同一板块"
		}
		out = append(out, foundation.MarketIndustryMomentum{Code: strconv.Itoa(i), Name: name, ChangePercent: 1, FiveDayChangePercent: 3, TwentyDayChange: 4, Meta: foundation.SourceMeta{Source: "fixture", AvailableFields: []string{"change_percent", "five_day_change_percent", "twenty_day_change_percent"}}})
	}
	return out, foundation.SourceMeta{}, nil
}
func (p *expandedScreenFixture) IndustryStocks(_ context.Context, code string, _ int) ([]foundation.BoardStock, foundation.SourceMeta, error) {
	group, _ := strconv.Atoi(code)
	entries, _ := p.StockCatalog(context.Background())
	out := []foundation.BoardStock{}
	for i, e := range entries[1:] {
		if i%8 == group {
			out = append(out, e.BoardStock)
		}
	}
	return out, foundation.SourceMeta{}, nil
}
func (p *expandedScreenFixture) StockFinancialHistory(ctx context.Context, symbol string, n int) ([]foundation.StockFundamentals, error) {
	active := p.active.Add(1)
	defer p.active.Add(-1)
	for old := p.maximum.Load(); active > old && !p.maximum.CompareAndSwap(old, active); old = p.maximum.Load() {
	}
	number, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(symbol, "600"), ".SH"))
	index := number - 100
	if p.slowAfter > 0 && index >= p.slowAfter {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	fs, err := p.optimizationScreenFixture.StockFinancialHistory(ctx, symbol, n)
	if index < p.failFirst {
		fs[0].RevenueYearOverYear = -10
	}
	return fs, err
}
func screeningServer(p *expandedScreenFixture) *Server {
	return &Server{stockDirectory: p, stockBusiness: p, realtimeProvider: p, kLinePrimary: p, marketOverview: p, industryStocks: p}
}
func screeningReport() pi.Report {
	return pi.Report{Request: pi.Request{Holdings: []pi.Holding{{Symbol: "600519.SH", Weight: 80}}}}
}
func TestExpandedCodeScreeningReplenishesAfterFirstEightFail(t *testing.T) {
	p := &expandedScreenFixture{optimizationScreenFixture: optimizationScreenFixture{now: time.Date(2026, 10, 5, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))}, total: 32, failFirst: 8}
	u, err := screeningServer(p).collectOptimizationUniverse(context.Background(), screeningReport(), po.Request{}, p.now)
	if err != nil {
		t.Fatal(err)
	}
	if u.ScreeningAudit.Checked != 16 || len(u.Candidates) != 8 || u.ScreeningAudit.Diverse != 6 || u.ScreeningAudit.StopReason != "candidates_ready" {
		t.Fatal(u.ScreeningAudit)
	}
	if p.maximum.Load() > 4 {
		t.Fatal("unbounded screening concurrency")
	}
	for _, c := range u.Candidates {
		if !c.Screening.Qualified {
			t.Fatal("failed candidate entered pool")
		}
	}
}
func TestExpandedCodeScreeningStopsAtEighty(t *testing.T) {
	p := &expandedScreenFixture{optimizationScreenFixture: optimizationScreenFixture{now: time.Date(2026, 10, 5, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))}, total: 160, failFirst: 160}
	u, err := screeningServer(p).collectOptimizationUniverse(context.Background(), screeningReport(), po.Request{}, p.now)
	if err != nil || u.ScreeningAudit.Checked != 80 || len(u.Candidates) != 0 || u.ScreeningAudit.StopReason != "check_limit" {
		t.Fatal(err, u.ScreeningAudit)
	}
}
func TestCodeScreeningTimeoutPreservesQualifiedAndCancellationStops(t *testing.T) {
	p := &expandedScreenFixture{optimizationScreenFixture: optimizationScreenFixture{now: time.Date(2026, 10, 5, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))}, total: 32, failFirst: 4, slowAfter: 8}
	u, err := screeningServer(p).collectOptimizationUniverseWithin(context.Background(), screeningReport(), po.Request{}, p.now, 150*time.Millisecond)
	if err != nil || len(u.Candidates) != 4 || u.ScreeningAudit.StopReason != "time_limit" {
		t.Fatal(err, u.ScreeningAudit)
	}
	if u.ScreeningAudit.Checked > 12 {
		t.Fatal("counted queued work as checked", u.ScreeningAudit.Checked)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = screeningServer(p).collectOptimizationUniverseWithin(ctx, screeningReport(), po.Request{}, p.now, time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("swallowed parent cancellation", err)
	}
}

func TestCodeScreeningStopsAtAvailableIndustryCapacity(t *testing.T) {
	p := &expandedScreenFixture{optimizationScreenFixture: optimizationScreenFixture{now: time.Date(2026, 10, 5, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))}, total: 80, singleBoard: true}
	u, err := screeningServer(p).collectOptimizationUniverse(context.Background(), screeningReport(), po.Request{}, p.now)
	if err != nil || u.ScreeningAudit.Checked != 8 || u.ScreeningAudit.Diverse != 1 || u.ScreeningAudit.StopReason != "industry_limit" {
		t.Fatal(err, u.ScreeningAudit)
	}
}

type valueScreenFixture struct {
	optimizationScreenFixture
	fail  bool
	calls int
}

func (p *valueScreenFixture) StockFinancialHistory(ctx context.Context, symbol string, limit int) ([]foundation.StockFundamentals, error) {
	rows, err := p.optimizationScreenFixture.StockFinancialHistory(ctx, symbol, limit)
	for i := range rows {
		rows[i].NetProfitYearOverYear = 3
		rows[i].DeductedNetProfitYearOverYear = 2
	}
	return rows, err
}
func (p *valueScreenFixture) StockValuations(ctx context.Context, symbols []string) ([]foundation.StockValuation, error) {
	p.calls++
	if p.fail {
		return nil, fmt.Errorf("valuation source unavailable")
	}
	values := []foundation.StockValuation{}
	for _, symbol := range symbols {
		pe, pb := 20.0, 2.0
		values = append(values, foundation.StockValuation{Symbol: symbol, PETTM: &pe, PB: &pb, TradeTime: foundation.LatestCompletedAStockSession(p.now).Add(15 * time.Hour), Meta: foundation.SourceMeta{Source: "fixture-valuation"}})
	}
	return values, nil
}
func TestCollectionAdmitsStableValueAndToleratesOptionalSourceFailure(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, foundation.AStockLocation)
	for _, fail := range []bool{false, true} {
		p := &valueScreenFixture{optimizationScreenFixture: optimizationScreenFixture{now: now}, fail: fail}
		s := &Server{stockDirectory: p, stockBusiness: p, realtimeProvider: p, kLinePrimary: p, marketOverview: &optimizationIndustryFixture{}, industryStocks: p, stockValuation: p}
		u, err := s.collectOptimizationUniverse(context.Background(), pi.Report{Request: pi.Request{Holdings: []pi.Holding{{Symbol: "600519.SH", Weight: 80}}}}, po.Request{CandidateSymbols: []string{"600001.SH"}}, now)
		if err != nil {
			t.Fatal(err)
		}
		admitted := false
		for _, r := range u.ScreeningAudit.Records {
			if r.Symbol == "600001.SH" {
				admitted = r.Qualified
			}
		}
		if admitted == fail {
			t.Fatal("stable value admission/fallback incorrect", fail, u.ScreeningAudit)
		}
		if len(u.Candidates) != 8 || p.calls > 4 {
			t.Fatal("unbounded per-stock valuation or growth fallback failed", p.calls, len(u.Candidates))
		}
		if !fail {
			found := false
			for _, q := range u.Quotes {
				if q.Symbol == "600001.SH" {
					found = q.Valuation != nil
				}
			}
			if !found {
				t.Fatal("valuation not frozen alongside retained candidate")
			}
		}
	}
}
