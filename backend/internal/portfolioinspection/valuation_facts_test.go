package portfolioinspection

import (
	"easy-stock/backend/internal/foundation"
	"testing"
	"time"
)

func TestValuationFactsAreFrozenAndSharedByBothConfigurations(t *testing.T) {
	request, results, _, _ := scoreFixture()
	pe, pb := 18.0, 1.8
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, foundation.AStockLocation)
	for i := range results {
		symbol := results[i].Holding.Symbol
		results[i].CurrentQuote = &foundation.Quote{Symbol: symbol, Price: 100, TradeTime: at,
			Valuation: &foundation.StockValuation{Symbol: symbol, PETTM: &pe, PB: &pb, TradeTime: at, Meta: foundation.SourceMeta{Source: "tencent:stock-valuation"}}}
	}
	symbol := results[0].Holding.Symbol
	key := symbol + ".valuation.pe_ttm"
	before := OptimizationUnionReport(request, results)
	request.Holdings[0].Weight = 20
	after := OptimizationReport(request, results)
	for _, r := range []Report{before, after} {
		f := r.Facts[key]
		if !f.Available || f.Value != pe || !f.AsOf.Equal(at) {
			t.Fatal(f)
		}
		if !OptimizationComparisonFacts(r)[key].Available {
			t.Fatal("review lost valuation evidence")
		}
	}
	for _, mutation := range []func(*foundation.Quote){
		func(q *foundation.Quote) { q.Valuation.Meta.Stale = true },
		func(q *foundation.Quote) { q.Valuation.TradeTime = q.TradeTime.AddDate(0, 0, -1) },
		func(q *foundation.Quote) { q.Valuation.Symbol = "wrong" },
		func(q *foundation.Quote) { q.Valuation.TradeTime = q.TradeTime.AddDate(0, 0, 1) },
		func(q *foundation.Quote) { q.Valuation.PETTM = nil },
	} {
		r := results[0]
		q := *r.CurrentQuote
		v := *q.Valuation
		q.Valuation = &v
		r.CurrentQuote = &q
		mutation(&q)
		f := InvestmentValuationFacts(r)[key]
		if f.Available || f.Value != nil {
			t.Fatal("invalid valuation accepted", f)
		}
	}
}
