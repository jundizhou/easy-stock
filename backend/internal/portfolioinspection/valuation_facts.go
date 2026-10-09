package portfolioinspection

import (
	"easy-stock/backend/internal/foundation"
)

// These are frozen market inputs shared by both configurations. No ratio is
// inferred from price drawdowns, prose or annualizing a cumulative report.
func InvestmentValuationFacts(r HoldingResult) map[string]Fact {
	out := map[string]Fact{}
	q := r.CurrentQuote
	if q == nil || q.Valuation == nil {
		return out
	}
	v := q.Valuation
	at := q.Meta.FetchedAt
	if at.IsZero() {
		at = q.TradeTime
	}
	valid := !q.Meta.Stale && v.Current(r.Holding.Symbol, at) &&
		q.TradeTime.In(foundation.AStockLocation).Format("2006-01-02") == v.TradeTime.In(foundation.AStockLocation).Format("2006-01-02")
	for key, value := range map[string]*float64{"pe_ttm": v.PETTM, "pb": v.PB} {
		available := valid && foundation.PositiveMultiple(value)
		var number any
		if available {
			number = *value
		}
		out[r.Holding.Symbol+".valuation."+key] = Fact{Value: number, Available: available, AsOf: v.TradeTime,
			Method: v.Meta.Source + "；冻结行情估值", Limitation: "PE为滚动归母盈利口径，PB为账面净资产口径；非扣非PE、行业/历史分位或公允价值"}
	}
	var date any
	if valid {
		date = v.TradeTime.In(foundation.AStockLocation).Format("2006-01-02")
	}
	out[r.Holding.Symbol+".valuation.trade_date"] = Fact{Value: date, Available: valid, AsOf: v.TradeTime, Method: v.Meta.Source}
	return out
}
