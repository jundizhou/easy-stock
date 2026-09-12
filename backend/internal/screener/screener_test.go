package screener

import (
	"context"
	"fmt"
	"testing"

	"easy-stock/backend/internal/foundation"
)

func TestSMACalculatesCorrectly(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5}
	got := SMA(values, 3)
	if got == nil || len(got) != 5 {
		t.Fatalf("SMA length = %d", len(got))
	}
	if got[2] != 2 || got[3] != 3 || got[4] != 4 {
		t.Fatalf("SMA values = %v", got[2:])
	}
	if SMA(values, 6) != nil {
		t.Fatal("SMA should return nil when data shorter than window")
	}
}

func TestEMACONvergesToValue(t *testing.T) {
	constant := make([]float64, 50)
	for i := range constant {
		constant[i] = 10
	}
	ema := EMA(constant, 12)
	if ema[len(ema)-1] < 9.999 || ema[len(ema)-1] > 10.001 {
		t.Fatalf("EMA of constant series should converge to 10, got %v", ema[len(ema)-1])
	}
}

func TestMACDBullDetectsUptrend(t *testing.T) {
	// 单边上涨序列应有正 DIF
	closes := make([]float64, 60)
	for i := range closes {
		closes[i] = 10 + float64(i)*0.1
	}
	dif, dea, hist := MACD(closes, 12, 26, 9)
	if dif == nil || dea == nil || hist == nil {
		t.Fatal("MACD returned nil for sufficient data")
	}
	last := len(closes) - 1
	if dif[last] <= 0 || dea[last] <= 0 {
		t.Fatalf("uptrend should give positive dif/dea, got %v/%v", dif[last], dea[last])
	}
}

func TestRSIBoundsAndOverbought(t *testing.T) {
	up := make([]float64, 30)
	for i := range up {
		up[i] = float64(i) + 1
	}
	rsi := RSI(up, 14)
	if rsi == nil {
		t.Fatal("RSI nil")
	}
	if rsi[len(up)-1] < 95 {
		t.Fatalf("pure uptrend RSI should be ~100, got %v", rsi[len(up)-1])
	}
}

func TestCrossedUpDetectsRecentCross(t *testing.T) {
	a := []float64{0, 0, -1, 1}
	b := []float64{0, 0, 0, 0}
	ok, ago := crossedUp(a, b, 3)
	if !ok || ago != 0 {
		t.Fatalf("crossedUp = %v/%d, want true/0", ok, ago)
	}
}

func snapshotRows() []foundation.MarketQuoteRow {
	return []foundation.MarketQuoteRow{
		{Symbol: "600519", Name: "贵州茅台", Close: 1500, ChangePercent: 3.2, Amount: 3e9, TurnoverRate: 1.2, VolumeRatio: 2.5, FloatCapYi: 18000, MainInflow: 2e8, FiveDayPct: 4, SixtyDayPct: 35, PB: 8},
		{Symbol: "300750", Name: "宁德时代", Close: 380, ChangePercent: 4.5, Amount: 9e9, TurnoverRate: 6, VolumeRatio: 2.2, FloatCapYi: 5000, MainInflow: 6e8, FiveDayPct: 8, SixtyDayPct: 40},
		{Symbol: "000001", Name: "平安银行", Close: 11, ChangePercent: -1.0, Amount: 1.5e9, TurnoverRate: 0.8, VolumeRatio: 0.6, FloatCapYi: 2000, MainInflow: -1e8, FiveDayPct: -2, SixtyDayPct: 5, PB: 0.6},
		{Symbol: "000003", Name: "*ST测试", Close: 2, ChangePercent: 5, Amount: 5e7, TurnoverRate: 12, VolumeRatio: 3},
		{Symbol: "600600", Name: "停牌股", Close: 0, ChangePercent: 0, Amount: 0},
	}
}

type fakeSnapshots struct{ rows []foundation.MarketQuoteRow }

func (f fakeSnapshots) MarketSnapshotRows(ctx context.Context) ([]foundation.MarketQuoteRow, error) {
	return f.rows, nil
}

// fakeKlines 生成 60 根单边上涨日K，保证均线多头等策略可命中。
func fakeKlines(ctx context.Context, symbol string, limit int) ([]foundation.KLine, error) {
	if symbol == "999999" {
		return nil, fmt.Errorf("kline unavailable")
	}
	bars := make([]foundation.KLine, 60)
	price := 10.0
	for i := range bars {
		price *= 1.01
		bars[i] = foundation.KLine{
			Symbol: symbol, Open: price / 1.005, Close: price, High: price * 1.01, Low: price / 1.01,
			Volume: float64(1e6 + i), Amount: price * 1e6,
		}
	}
	return bars, nil
}

func TestRunSnapshotStrategiesFilterAndMatch(t *testing.T) {
	svc, err := NewService(fakeSnapshots{rows: snapshotRows()}, fakeKlines, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	result, err := svc.Run(context.Background(), Request{StrategyIDs: []string{"vol_surge_up"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Scanned != 4 { // 停牌行被剔除：5 - 1
		t.Fatalf("scanned = %d, want 4", result.Scanned)
	}
	// 600519（量比2.5、涨3.2%）与 300750（量比2.2、涨4.5%）命中；*ST 未排除时也命中（量比3、涨5%）
	if result.Matched < 2 {
		t.Fatalf("matched = %d, want >= 2: %+v", result.Matched, result.Hits)
	}
	if len(result.Hits[0].Strategies) == 0 || result.Hits[0].Strategies[0] != "vol_surge_up" {
		t.Fatalf("hit strategies = %+v", result.Hits[0].Strategies)
	}
}

func TestRunExcludeSTAndSuspended(t *testing.T) {
	svc, _ := NewService(fakeSnapshots{rows: snapshotRows()}, fakeKlines, nil)
	result, err := svc.Run(context.Background(), Request{
		StrategyIDs: []string{"vol_surge_up"},
		Options:     Options{ExcludeST: true, MinAmountYi: 0.1},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, hit := range result.Hits {
		if hit.Symbol == "000003" {
			t.Fatal("*ST should be excluded")
		}
	}
	if result.Scanned != 3 {
		t.Fatalf("scanned = %d, want 3 (suspended dropped + ST excluded)", result.Scanned)
	}
}

func TestRunKlineStrategyWithUniverseCap(t *testing.T) {
	rows := snapshotRows()
	// 追加一批成交额递减的股票，验证保底池按成交额选取
	for i := 0; i < 10; i++ {
		rows = append(rows, foundation.MarketQuoteRow{
			Symbol: fmt.Sprintf("60%04d", i), Name: fmt.Sprintf("股%d", i),
			Close: 10, ChangePercent: 1, Amount: float64(10-i) * 1e8, TurnoverRate: 2,
		})
	}
	svc, _ := NewService(fakeSnapshots{rows: rows}, fakeKlines, nil)
	result, err := svc.Run(context.Background(), Request{
		StrategyIDs: []string{"ma_bull_align"},
		Options:     Options{KlineUniverseLimit: 5},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.KlineCount != 5 {
		t.Fatalf("kline count = %d, want 5 (capped)", result.KlineCount)
	}
	if result.KlineFailed != 0 {
		t.Fatalf("kline failed = %d", result.KlineFailed)
	}
	// fakeKlines 生成单边上涨序列，池内股票应全部命中均线多头
	if result.Matched != 5 {
		t.Fatalf("matched = %d, want 5", result.Matched)
	}
	for _, hit := range result.Hits {
		if hit.Indicators["ma60"] == 0 {
			t.Fatalf("ma60 indicator missing: %+v", hit.Indicators)
		}
	}
}

func TestRunRejectsUnknownAndEmptyStrategies(t *testing.T) {
	svc, _ := NewService(fakeSnapshots{rows: snapshotRows()}, fakeKlines, nil)
	if _, err := svc.Run(context.Background(), Request{StrategyIDs: []string{"nope"}}); err == nil {
		t.Fatal("unknown strategy should error")
	}
	if _, err := svc.Run(context.Background(), Request{StrategyIDs: nil}); err == nil {
		t.Fatal("empty strategies should error")
	}
}

func TestStrategiesCatalogComplete(t *testing.T) {
	catalog := Strategies()
	if len(catalog) < 15 {
		t.Fatalf("catalog size = %d, want >= 15", len(catalog))
	}
	ids := map[string]bool{}
	for _, def := range catalog {
		if ids[def.ID] {
			t.Fatalf("duplicate strategy id %s", def.ID)
		}
		ids[def.ID] = true
		if strategyByID(def.ID) == nil {
			t.Fatalf("strategy %s missing in registry", def.ID)
		}
	}
}
