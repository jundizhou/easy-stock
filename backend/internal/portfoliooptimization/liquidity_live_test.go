package portfoliooptimization

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/sina"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Run manually against the public quote API; regular tests remain offline.
// Exercises the real provider -> eligibility path that the amount bug broke.
func TestLiveSinaOptimizationLiquidity(t *testing.T) {
	if os.Getenv("EASY_STOCK_LIVE_OPTIMIZATION") != "1" {
		t.Skip("set EASY_STOCK_LIVE_OPTIMIZATION=1 to verify public realtime liquidity")
	}
	names := map[string]string{
		"301536.SZ": "星宸科技", "688220.SH": "翱捷科技", "300209.SZ": "行云科技", "688512.SH": "慧智微",
		"688185.SH": "康希诺", "688265.SH": "南模生物", "601118.SH": "海南橡胶", "688356.SH": "键凯科技",
		"600418.SH": "江淮汽车", "603919.SH": "金徽酒", "600059.SH": "古越龙山", "000911.SZ": "*ST广糖",
		"002242.SZ": "九阳股份", "600547.SH": "山东黄金", "300308.SZ": "中际旭创", "688256.SH": "寒武纪",
		"300750.SZ": "宁德时代", "001246.SZ": "力勤资源", "300394.SZ": "天孚通信", "603259.SH": "药明康德",
	}
	symbols := make([]string, 0, len(names))
	for s := range names {
		symbols = append(symbols, s)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	quotes, err := sina.NewClient().Realtime(ctx, symbols)
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != len(symbols) {
		t.Fatalf("quotes=%d, requested=%d", len(quotes), len(symbols))
	}
	now := time.Now()
	rows := []Eligibility{}
	allowed := 0
	for _, q := range quotes {
		if !QuoteCurrent(q, now) || !finitePositive(q.Volume) || !finitePositive(q.Amount) {
			t.Errorf("public quote missing latest session totals: %s date=%s volume=%v amount=%v", q.Symbol, q.TradeTime, q.Volume, q.Amount)
		}
		// Match the production fallback: Sina daily volume exists, amount does not.
		entry := foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: q.Symbol, Name: names[q.Symbol],
			Volume: q.Volume, Amount: 0, Meta: foundation.SourceMeta{Source: "sina", TradeDate: LatestSession(now)}}}
		e := TradingEligibility(entry, q, now)
		rows = append(rows, e)
		if len(e.MissingFields) > 0 || e.LiquiditySource != "sina:realtime" {
			t.Errorf("real quote still rejected due to daily amount: %+v", e)
		}
		if e.CanIncrease {
			allowed++
		}
		switch q.Symbol {
		case "301536.SZ", "688220.SH", "300209.SZ", "688512.SH":
			if e.Locked || !e.CanIncrease {
				t.Errorf("original holding incorrectly locked: %+v", e)
			}
		case "000911.SZ":
			if e.CanIncrease {
				t.Errorf("ST restriction lost: %+v", e)
			}
		}
		t.Logf("%s %s date=%s volume=%.0f amount=%.2f can_increase=%v locked=%v", q.Symbol, q.Name, e.LiquidityTradeDate, e.Volume, e.Amount, e.CanIncrease, e.Locked)
	}
	if dest := os.Getenv("EASY_STOCK_LIVE_OPTIMIZATION_OUTPUT"); dest != "" {
		payload := struct {
			VerifiedAt       time.Time     `json:"verified_at"`
			LatestSession    string        `json:"latest_session"`
			QuoteCount       int           `json:"quote_count"`
			CanIncreaseCount int           `json:"can_increase_count"`
			Eligibility      []Eligibility `json:"eligibility"`
		}{now, LatestSession(now), len(quotes), allowed, rows}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
