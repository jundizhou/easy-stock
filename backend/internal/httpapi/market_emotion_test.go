package httpapi

import (
	"context"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
)

func TestMarketEmotionBootstrapsSevenDaysOnlyOnce(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	dates := []time.Time{
		time.Date(2026, 7, 28, 0, 0, 0, 0, location),
		time.Date(2026, 7, 29, 0, 0, 0, 0, location),
		time.Date(2026, 7, 30, 0, 0, 0, 0, location),
		time.Date(2026, 7, 31, 0, 0, 0, 0, location),
		time.Date(2026, 8, 3, 0, 0, 0, 0, location),
		time.Date(2026, 8, 4, 0, 0, 0, 0, location),
		time.Date(2026, 8, 5, 0, 0, 0, 0, location),
		time.Date(2026, 8, 6, 0, 0, 0, 0, location),
	}
	events := []foundation.LimitUpEvent{}
	lines := map[string][]foundation.KLine{"600001.SH": {}, "300001.SZ": {}}
	for index, date := range dates {
		events = append(events,
			foundation.LimitUpEvent{Symbol: "600001.SH", Name: "主板核心", Date: date, Streak: min(index+1, 5), Amount: 100 + float64(index)},
			foundation.LimitUpEvent{Symbol: "300001.SZ", Name: "创业板核心", Date: date, Streak: 1, Amount: 80 + float64(index)},
		)
		for symbol := range lines {
			lines[symbol] = append(lines[symbol], foundation.KLine{
				Symbol: symbol, Time: date, Open: 10.2, Close: 10.5, High: 10.8, Low: 10,
				ChangePercent: 5,
			})
		}
	}
	limitUps := &countingEmotionLimitUpProvider{events: events}
	pools := &countingMarketPoolProvider{}
	kLines := &countingEmotionKLineProvider{lines: lines}
	store, err := marketemotion.OpenStore("")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	engine := newMarketEmotionEngine(store, limitUps, pools, kLines, nil, nil)
	engine.now = func() time.Time { return time.Date(2026, 8, 6, 20, 0, 0, 0, location) }

	first, err := engine.load(context.Background())
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(first.Points) != 7 || first.Points[0].TradeDate != "2026-07-29" || first.Latest.TradeDate != "2026-08-06" {
		t.Fatalf("unexpected bootstrap history: %+v", first)
	}
	if first.Cache.Mode != "local-sqlite" || first.Cache.LastExternalSync != "2026-08-06" {
		t.Fatalf("unexpected cache metadata: %+v", first.Cache)
	}
	if limitUps.calls != 1 || pools.brokenCalls != 7 || pools.downCalls != 7 {
		t.Fatalf("unexpected external calls: limit=%d broken=%d down=%d", limitUps.calls, pools.brokenCalls, pools.downCalls)
	}
	if kLines.calls != 2 {
		t.Fatalf("kline calls = %d, want one call per unique previous-day stock", kLines.calls)
	}

	second, err := engine.load(context.Background())
	if err != nil || len(second.Points) != 7 {
		t.Fatalf("second load = %+v, err=%v", second, err)
	}
	if limitUps.calls != 1 || pools.brokenCalls != 7 || kLines.calls != 2 {
		t.Fatalf("second load should be cache-only: limit=%d broken=%d kline=%d", limitUps.calls, pools.brokenCalls, kLines.calls)
	}
}

func TestIntradayEmotionDetectsHighLevelRetreat(t *testing.T) {
	changes := []float64{-4.03, -6.25, -4.94, -10.06}
	data := limitUpLadderData{
		SessionStatus: "盘中快照",
		Current: limitUpLadderDay{
			TradeDate:       "2026-08-07",
			LimitUpCount:    42,
			BoardCount:      11,
			FirstBoardCount: 31,
			MaxStreak:       4,
		},
		Previous: limitUpLadderDay{
			TradeDate: "2026-08-06",
			MaxStreak: 10,
			Levels: []limitUpLadderLevel{
				{Level: 10, Stocks: []limitUpLadderStock{{Symbol: "603221.SH", Name: "爱丽家居", Streak: 10, LimitRegime: "10cm", CurrentChangePercent: &changes[0]}}},
				{Level: 5, Stocks: []limitUpLadderStock{{Symbol: "002963.SZ", Name: "豪尔赛", Streak: 5, LimitRegime: "10cm", CurrentChangePercent: &changes[1]}}},
				{Level: 4, Stocks: []limitUpLadderStock{
					{Symbol: "300615.SZ", Name: "欣天科技", Streak: 4, LimitRegime: "20cm", CurrentChangePercent: &changes[2]},
					{Symbol: "601700.SH", Name: "风范股份", Streak: 4, LimitRegime: "10cm", CurrentChangePercent: &changes[3]},
				}},
			},
		},
		Advance: []limitUpAdvanceStep{
			{FromLevel: 4, ToLevel: 5, Base: 2},
			{FromLevel: 5, ToLevel: 6, Base: 1},
			{FromLevel: 10, ToLevel: 11, Base: 1},
		},
	}
	latest := marketemotion.Snapshot{TradeDate: "2026-08-06", Phase: "高潮", EmotionScore: 77.77}

	snapshot := buildMarketEmotionIntraday(data, &latest)
	if snapshot.Status != "高位退潮" || snapshot.Breadth != "低位活跃" {
		t.Fatalf("unexpected intraday state: %+v", snapshot)
	}
	if snapshot.RiskScore < 75 || snapshot.Metrics.HighDownRate != 1 || snapshot.Metrics.HighSevereCount != 2 {
		t.Fatalf("high-level risk was understated: %+v", snapshot)
	}
	if snapshot.Metrics.HeightCollapse != 6 || snapshot.Metrics.HighAdvanceBase != 4 || snapshot.Metrics.HighAdvanceCount != 0 {
		t.Fatalf("unexpected height/advance metrics: %+v", snapshot.Metrics)
	}
	if got := snapshot.Metrics.HighLevels; len(got) != 3 || got[0] != 10 || got[1] != 5 || got[2] != 4 {
		t.Fatalf("high levels = %v, want [10 5 4]", got)
	}
}

func TestMarketEmotionIntradayCacheRecomputesAtMostEveryTenMinutes(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, 8, 7, 10, 40, 0, 0, location)
	cache := newMarketEmotionIntradayCache(10 * time.Minute)
	cache.now = func() time.Time { return now }
	calls := 0
	loader := func(context.Context) (marketemotion.IntradaySnapshot, error) {
		calls++
		return marketemotion.IntradaySnapshot{TradeDate: "2026-08-07", Status: "高位退潮"}, nil
	}

	first, err := cache.load(context.Background(), loader)
	if err != nil || calls != 1 {
		t.Fatalf("first load calls=%d snapshot=%+v err=%v", calls, first, err)
	}
	now = now.Add(9 * time.Minute)
	second, err := cache.load(context.Background(), loader)
	if err != nil || calls != 1 || !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("cached load calls=%d snapshot=%+v err=%v", calls, second, err)
	}
	now = now.Add(2 * time.Minute)
	third, err := cache.load(context.Background(), loader)
	if err != nil || calls != 2 || !third.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("expired load calls=%d snapshot=%+v err=%v", calls, third, err)
	}
}

func TestHighLevelRiskVetoPreventsClimax(t *testing.T) {
	raw := marketemotion.RawMetrics{
		HighSampleCount:   4,
		HighWeakCount:     4,
		HighKill:          2,
		HighLimitDown:     1,
		HighAverageReturn: -6.32,
		HighDownRate:      1,
		HighAdvanceRate:   0,
		HeightCollapse:    6,
		HighRiskScore:     86,
	}
	scores := marketemotion.Scores{Heat: 82, Profit: 80, Structure: 65, Total: 78}
	if phase := classifyMarketEmotion(scores, raw, nil); phase != "退潮" {
		t.Fatalf("phase = %s, want 退潮", phase)
	}
}

type countingEmotionLimitUpProvider struct {
	mu     sync.Mutex
	events []foundation.LimitUpEvent
	calls  int
}

func (p *countingEmotionLimitUpProvider) RecentLimitUps(context.Context, int) ([]foundation.LimitUpEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return append([]foundation.LimitUpEvent(nil), p.events...), nil
}

type countingMarketPoolProvider struct {
	mu          sync.Mutex
	brokenCalls int
	downCalls   int
}

func (p *countingMarketPoolProvider) BrokenLimitUpPool(context.Context, time.Time) ([]foundation.MarketLimitEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.brokenCalls++
	return nil, nil
}

func (p *countingMarketPoolProvider) LimitDownPool(context.Context, time.Time) ([]foundation.MarketLimitEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.downCalls++
	return nil, nil
}

type countingEmotionKLineProvider struct {
	mu    sync.Mutex
	lines map[string][]foundation.KLine
	calls int
}

func (p *countingEmotionKLineProvider) KLine(_ context.Context, symbol string, _ string, _ int) ([]foundation.KLine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return append([]foundation.KLine(nil), p.lines[symbol]...), nil
}
