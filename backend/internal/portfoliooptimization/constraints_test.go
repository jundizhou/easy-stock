package portfoliooptimization

import (
	"easy-stock/backend/internal/foundation"
	pi "easy-stock/backend/internal/portfolioinspection"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

func holds(a, b int) []pi.Holding {
	out := []pi.Holding{}
	if a > 0 {
		out = append(out, pi.Holding{Symbol: "600519.SH", Weight: a})
	}
	if b > 0 {
		out = append(out, pi.Holding{Symbol: "000858.SZ", Weight: b})
	}
	return out
}
func TestReplacementConservationAndExactBoundaries(t *testing.T) {
	for _, tc := range []struct {
		total, sold int
		valid       bool
	}{{80, 56, true}, {80, 57, false}, {50, 35, true}, {50, 36, false}, {1, 0, true}, {1, 1, false}, {100, 70, true}, {100, 71, false}} {
		c := Check(holds(tc.total, 0), holds(tc.total-tc.sold, tc.sold))
		if c.Valid != tc.valid {
			t.Fatalf("%+v -> %+v", tc, c)
		}
		if c.Valid && (c.Sold != tc.sold || c.Sold != c.Bought || c.Retained != tc.total-tc.sold) {
			t.Fatal(c)
		}
	}
	if Check(holds(60, 20), holds(50, 20)).Valid {
		t.Fatal("total decrease accepted")
	}
	if Check(holds(60, 20), holds(60, 21)).Valid {
		t.Fatal("total increase accepted")
	}
	if Check(holds(60, 20), []pi.Holding{{Symbol: "600519.SH", Weight: 40}, {Symbol: "600519.SH", Weight: 40}}).Valid {
		t.Fatal("duplicate bypass")
	}
	if Check(holds(80, 0), []pi.Holding{{Symbol: "600519.SH", Weight: 20}, {Symbol: "000001.SZ", Weight: 20}, {Symbol: "000002.SZ", Weight: 20}, {Symbol: "000858.SZ", Weight: 20}}).Valid {
		t.Fatal("three added")
	}
}
func TestCumulativeBudgetIsInitialNotPrevious(t *testing.T) {
	root := holds(80, 0)
	first := holds(24, 56)
	second := holds(0, 80)
	if !Check(root, first).Valid || !Check(first, second).Valid {
		t.Fatal("fixture")
	}
	if Check(root, second).Valid {
		t.Fatal("cumulative 100% replacement accepted")
	}
}
func TestLatestTradingSessionHolidayAndUnknownEligibility(t *testing.T) {
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, zone)
	trade := time.Date(2026, 9, 30, 15, 0, 0, 0, zone)
	q := foundation.Quote{Symbol: "600519.SH", Price: 100, TradeTime: trade}
	if LatestSession(now) != "2026-09-30" || !QuoteCurrent(q, now) {
		t.Fatal("holiday quote rejected")
	}
	if LatestSession(time.Date(2026, 10, 8, 9, 0, 0, 0, zone)) != "2026-09-30" {
		t.Fatal("preopen")
	}
	entry := foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Name: "测试股", Volume: 100, Amount: 1000, Meta: foundation.SourceMeta{TradeDate: "2026-09-30"}}}
	if !TradingEligibility(entry, q, now).CanIncrease {
		t.Fatal("known session eligibility rejected")
	}
	entry.Meta.TradeDate = ""
	entry.Meta.FetchedAt = now
	if TradingEligibility(entry, q, now).CanIncrease {
		t.Fatal("fetch date used as holiday trade date")
	}
	entry.Name = "*ST测试"
	if TradingEligibility(entry, q, now).CanIncrease {
		t.Fatal("ST accepted")
	}
	entry.Name = "测试"
	entry.Meta.TradeDate = "2026-09-30"
	entry.Volume = 0
	if !TradingEligibility(entry, q, now).Locked {
		t.Fatal("unknown/suspended not locked")
	}
}

func TestTradingEligibilityUsesTimestampedRealtimeTotalsWhenDailyAmountMissing(t *testing.T) {
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, zone)
	q := foundation.Quote{Symbol: "301536.SZ", Name: "星宸科技", Price: 123.36,
		TradeTime: time.Date(2026, 9, 30, 16, 29, 45, 0, zone), Volume: 9534584, Amount: 1176000000,
		Meta: foundation.SourceMeta{Source: "sina"}}
	entry := foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: q.Symbol, Name: q.Name,
		Volume: q.Volume, Amount: 0, Meta: foundation.SourceMeta{Source: "sina", TradeDate: "2026-09-30"}}}
	for _, dated := range []bool{true, false} {
		if !dated {
			entry.Meta.TradeDate = ""
			entry.Meta.Stale = true
		}
		e := TradingEligibility(entry, q, now)
		if e.Locked || !e.CanIncrease || !e.Conditional || e.Volume != q.Volume || e.Amount != q.Amount ||
			e.LiquiditySource != "sina:realtime" || e.LiquidityTradeDate != "2026-09-30" || len(e.MissingFields) > 0 {
			t.Fatalf("complete quote blocked by incomplete catalog (dated=%v): %+v", dated, e)
		}
	}
}

func TestTradingEligibilityKeepsUnknownAndUnsafeDataRestricted(t *testing.T) {
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, zone)
	quote := foundation.Quote{Symbol: "600519.SH", Name: "测试股", Price: 100, Volume: 100, Amount: 10000,
		TradeTime: time.Date(2026, 9, 30, 15, 0, 0, 0, zone), Meta: foundation.SourceMeta{Source: "sina"}}
	catalog := foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: quote.Symbol, Name: quote.Name,
		Volume: 100, Meta: foundation.SourceMeta{TradeDate: "2026-09-30"}}}
	for _, tc := range []struct {
		name    string
		mutate  func(*foundation.StockCatalogEntry, *foundation.Quote)
		locked  bool
		missing string
		reason  string
	}{
		{"amount omitted", func(_ *foundation.StockCatalogEntry, q *foundation.Quote) { q.Amount = 0 }, true, "amount", "未提供有效成交额"},
		{"no confirmed trades", func(e *foundation.StockCatalogEntry, q *foundation.Quote) { e.Volume = 0; q.Volume = 0; q.Amount = 0 }, true, "volume", "需核验停牌"},
		{"undated catalog cannot be trusted", func(e *foundation.StockCatalogEntry, q *foundation.Quote) {
			e.Amount = 10000
			e.Meta.TradeDate = ""
			e.Meta.FetchedAt = now
			q.Amount = 0
		}, true, "liquidity_trade_date", "无法确认"},
		{"stale quote", func(_ *foundation.StockCatalogEntry, q *foundation.Quote) { q.Meta.Stale = true }, true, "current_quote", "过期"},
		{"previous session quote", func(_ *foundation.StockCatalogEntry, q *foundation.Quote) {
			q.TradeTime = q.TradeTime.AddDate(0, 0, -1)
		}, true, "current_quote", "行情交易日2026-09-29"},
		{"missing quote", func(_ *foundation.StockCatalogEntry, q *foundation.Quote) { q.Price = 0 }, true, "current_quote", "行情缺失"},
		{"invalid price", func(_ *foundation.StockCatalogEntry, q *foundation.Quote) { q.Price = math.Inf(1) }, true, "current_quote", "行情缺失"},
		{"future timestamp", func(_ *foundation.StockCatalogEntry, q *foundation.Quote) { q.TradeTime = now.Add(24 * time.Hour) }, true, "current_quote", "行情交易日"},
		{"catalog absent", func(e *foundation.StockCatalogEntry, _ *foundation.Quote) { e.Name = "" }, true, "catalog_entry", "真实股票目录"},
		{"ST quote overrides catalog name", func(_ *foundation.StockCatalogEntry, q *foundation.Quote) { q.Name = "*ST测试" }, false, "", "ST或退市风险"},
		{"daily limit", func(_ *foundation.StockCatalogEntry, q *foundation.Quote) { q.ChangePercent = 9.9 }, true, "", "接近涨跌停"},
		{"nonfinite amount", func(_ *foundation.StockCatalogEntry, q *foundation.Quote) { q.Amount = math.NaN() }, true, "amount", "未提供有效成交额"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, q := catalog, quote
			tc.mutate(&e, &q)
			got := TradingEligibility(e, q, now)
			if got.CanIncrease || got.Locked != tc.locked || !strings.Contains(got.Reason, tc.reason) ||
				(tc.missing != "" && !slices.Contains(got.MissingFields, tc.missing)) {
				t.Fatalf("restriction lost: %+v", got)
			}
		})
	}
	// Alternate providers may omit realtime totals if complete dated daily totals
	// are available. Both pieces must refer to the same valid trading session.
	quote.Volume, quote.Amount = 0, 0
	catalog.Amount = 10000
	catalog.Meta.Source = "eastmoney"
	got := TradingEligibility(catalog, quote, now)
	if got.Locked || !got.CanIncrease || got.LiquiditySource != "eastmoney:daily" || got.Amount != 10000 {
		t.Fatalf("complete daily totals rejected: %+v", got)
	}
}
func TestSolvePreservesCashLocksAndRequiresInvestmentJudgment(t *testing.T) {
	j := fixtureJob()
	alt := fixtureProposal().Alternatives[0]
	target, err := Solve(j, alt)
	if err != nil {
		t.Fatal(err)
	}
	c := Check(j.Baseline, target)
	if c.Total != 80 || c.Cash != 20 || !c.Valid {
		t.Fatal(c)
	}
	if target[1].CostPrice != nil {
		t.Fatal("old blended cost copied to increase")
	}
	j.Eligibility[1].CanIncrease = false
	if _, err := Solve(j, alt); err == nil {
		t.Fatal("unknown eligibility permitted increase")
	}
	j = fixtureJob()
	j.Results[1].Analysis.ResearchReport.Decision.Status = "no_plan"
	if _, err := Solve(j, alt); err != nil {
		t.Fatal("no_plan alone must not block investment judgment", err)
	}
	alt.Allocations[1].Investment.PriorOpinion = ""
	if _, err := Solve(j, alt); err == nil {
		t.Fatal("original objection silently ignored")
	}
	alt = fixtureProposal().Alternatives[0]
	j = fixtureJob()
	j.Results[1].Analysis.ResearchReport.Decision.Horizon = "short"
	alt.Allocations[1].Investment.PeriodSuitability = ""
	if _, err := Solve(j, alt); err == nil {
		t.Fatal("different horizon authorized increase")
	}
	j = fixtureJob()
	j.Eligibility[0].Locked = true
	if _, err := Solve(j, alt); err == nil {
		t.Fatal("locked decreased")
	}
}
func TestFactsRequiredForImprovementAndStyleCashRemains(t *testing.T) {
	j := fixtureJob()
	p := fixtureProposal()
	j.Proposal = &p
	if len(measureImprovements(j, holds(45, 35))) == 0 {
		t.Fatal("actual concentration improvement missing")
	}
	j.Source.Request.Holdings = holds(30, 30)
	j.Baseline = holds(30, 30)
	if len(measureImprovements(j, holds(31, 29))) != 0 {
		t.Fatal("arbitrary reshuffle called improvement")
	}
	j.Source.Request.TraderProfile = pi.ProfileSteady
	j.Source.Request.Holdings = holds(60, 40)
	r := pi.OptimizationReport(j.Source.Request, j.Results)
	if r.Metrics.CashPercent != 0 || r.Profile.MinimumCashPercent != 10 {
		t.Fatal("cash deficiency erased")
	}
}

func TestFundingFlowsCountEachReleasedUnitOnceAndSeparateChainBudget(t *testing.T) {
	root := holds(60, 20)
	current := holds(45, 35)
	target := holds(50, 30)
	flows, sold, bought := FundingFlows(current, target)
	if sold != 5 || bought != 5 || len(flows) != 1 || flows[0].FromSymbol != "000858.SZ" || flows[0].ToSymbol != "600519.SH" {
		t.Fatal("step funding", flows, sold, bought)
	}
	if c := Check(root, target); c.Sold != 10 {
		t.Fatal("cumulative funding fixture", c)
	}
	target = []pi.Holding{{Symbol: "600519.SH", Weight: 40}, {Symbol: "000001.SZ", Weight: 20}, {Symbol: "000002.SZ", Weight: 20}}
	flows, sold, bought = FundingFlows(root, target)
	outgoing := map[string]int{}
	incoming := map[string]int{}
	for _, f := range flows {
		outgoing[f.FromSymbol] += f.Weight
		incoming[f.ToSymbol] += f.Weight
	}
	if sold != 40 || bought != 40 || outgoing["600519.SH"] != 20 || outgoing["000858.SZ"] != 20 || incoming["000001.SZ"] != 20 || incoming["000002.SZ"] != 20 {
		t.Fatal("funding double count", flows)
	}
}

func TestFundingFlowsComparedMatchesActualInvestmentAndConservesEachLeg(t *testing.T) {
	current := []pi.Holding{{Symbol: "300209.SZ", Weight: 16}, {Symbol: "688512.SH", Weight: 34}, {Symbol: "301536.SZ", Weight: 26}, {Symbol: "688220.SH", Weight: 24}}
	target := []pi.Holding{{Symbol: "301536.SZ", Weight: 35}, {Symbol: "688220.SH", Weight: 24}, {Symbol: "300209.SZ", Weight: 10}, {Symbol: "688512.SH", Weight: 11}, {Symbol: "601169.SH", Weight: 20}}
	comparisons := []InvestmentComparison{{FromSymbol: "688512.SH", ToSymbol: "301536.SZ"}, {FromSymbol: "688512.SH", ToSymbol: "601169.SH"}, {FromSymbol: "300209.SZ", ToSymbol: "601169.SH"}}
	flows, sold, bought := FundingFlowsCompared(current, target, comparisons)
	expected := map[string]int{"300209.SZ/601169.SH": 6, "688512.SH/301536.SZ": 9, "688512.SH/601169.SH": 14}
	if sold != 29 || bought != 29 || len(flows) != 3 {
		t.Fatal("funding conservation lost", flows, sold, bought)
	}
	for _, f := range flows {
		if expected[f.FromSymbol+"/"+f.ToSymbol] != f.Weight {
			t.Fatal("investment direction not preserved", flows)
		}
	}
}
func TestFundingComparedUsesReversePathsAndLeavesUnmatchedFundingVisible(t *testing.T) {
	current := []pi.Holding{{Symbol: "a", Weight: 30}, {Symbol: "b", Weight: 30}}
	target := []pi.Holding{{Symbol: "a", Weight: 20}, {Symbol: "b", Weight: 20}, {Symbol: "c", Weight: 10}, {Symbol: "d", Weight: 10}}
	pairs := []InvestmentComparison{{FromSymbol: "a", ToSymbol: "c"}, {FromSymbol: "a", ToSymbol: "d"}, {FromSymbol: "b", ToSymbol: "c"}, {FromSymbol: "unknown", ToSymbol: "d"}}
	flows, sold, bought := FundingFlowsCompared(current, target, pairs)
	if sold != 20 || bought != 20 || len(flows) != 2 {
		t.Fatal(flows, sold, bought)
	}
	for _, f := range flows {
		if (f.FromSymbol == "a" && f.ToSymbol != "d") || (f.FromSymbol == "b" && f.ToSymbol != "c") || f.Weight != 10 {
			t.Fatal("greedy pairing lost valid directions", flows)
		}
	}
	// No evidence-supported path to d; it must still be disclosed, not omitted.
	flows, sold, bought = FundingFlowsCompared(current, target, pairs[:1])
	total, unsupported := 0, 0
	for _, f := range flows {
		total += f.Weight
		if f.ToSymbol == "d" {
			unsupported += f.Weight
		}
	}
	if total != sold || sold != bought || unsupported != 10 {
		t.Fatal("unmatched money hidden", flows)
	}
}
