package foundation

import (
	"math"
	"strings"
	"time"
)

// Valuation ratios retain the provider's trailing-earnings/book-value basis.
// Missing/nonpositive multiples cannot be interpreted as cheap valuations.
type StockValuation struct {
	Symbol    string     `json:"symbol"`
	PETTM     *float64   `json:"pe_ttm,omitempty"`
	PB        *float64   `json:"pb,omitempty"`
	TradeTime time.Time  `json:"trade_time"`
	Meta      SourceMeta `json:"meta"`
}

func PositiveMultiple(v *float64) bool {
	return v != nil && *v > 0 && !math.IsNaN(*v) && !math.IsInf(*v, 0)
}

func LatestAStockSession(now time.Time) string {
	day := now.In(AStockLocation)
	if day.Hour() < 9 || (day.Hour() == 9 && day.Minute() < 30) {
		day = day.AddDate(0, 0, -1)
	}
	for !IsAStockTradingDay(day) {
		day = day.AddDate(0, 0, -1)
	}
	return day.Format("2006-01-02")
}

func (v *StockValuation) Current(symbol string, now time.Time) bool {
	return v != nil && v.Symbol == symbol && strings.TrimSpace(v.Meta.Source) != "" && !v.Meta.Stale &&
		!now.IsZero() && !v.TradeTime.IsZero() && !v.TradeTime.After(now.Add(time.Minute)) &&
		v.TradeTime.In(AStockLocation).Format("2006-01-02") == LatestAStockSession(now)
}
