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

// 2026-10-22 的真实盘面：涨停 33、跌停 49、上涨占比 25%，高位股 16 只里 10 只走弱、
// 8 只被闷杀、6 只跌停，昨涨停 -1.57%、昨连板 -2.74%。这种日子按旧刻度算出 35.7 分
// （落在「混沌/过渡」），与实际体感（接近冰点）不符，所以把两个偏松的刻度钉住。
func TestMarketEmotionScoresFreezeDayBelowCriticalZone(t *testing.T) {
	raw := marketemotion.RawMetrics{
		LimitUpCount:       33,
		LimitDownCount:     49,
		BrokenCount:        23,
		FirstBoardCount:    24,
		BoardCount:         9,
		MaxStreak:          5,
		FinalBreakRate:     0.4107,
		PreviousLimitUpRet: -1.5737,
		PreviousBoardRet:   -2.7354,
		OpenPremium:        1.7789,
		CoreReturn:         -0.8059,
		AdvanceRate:        0.25,
		ThemeFocus:         0.2921,
		LadderContinuity:   0.75,
		HighSampleCount:    16,
		HighWeakCount:      10,
		HighKill:           8,
		HighLimitDown:      6,
		HighAverageReturn:  -2.7354,
		HighDownRate:       0.6875,
		HighAdvanceRate:    0.25,
		HighRiskScore:      46.07,
		LowKill:            7,
		QuoteCoverage:      0.92,
	}
	snapshot := scoreMarketEmotion("2026-10-22", raw, nil, time.Now())
	if snapshot.Scores.Total >= 26 {
		t.Fatalf("total = %v, want a freeze-level score below 26 (%+v)", snapshot.Scores.Total, snapshot.Scores)
	}
	if snapshot.Scores.Profit >= 32 {
		t.Fatalf("profit = %v, losing days should not score near neutral (%+v)", snapshot.Scores.Profit, snapshot.Scores)
	}
	if snapshot.Phase != "冰点" {
		t.Fatalf("phase = %s, want 冰点", snapshot.Phase)
	}
}

// 强市不能被新刻度误伤：涨停 92、跌停 4、上涨 72%、昨涨停 +4.5% 仍应是高分。
func TestMarketEmotionScoresStrongDayHigh(t *testing.T) {
	raw := marketemotion.RawMetrics{
		LimitUpCount:       92,
		LimitDownCount:     4,
		BrokenCount:        18,
		FirstBoardCount:    62,
		BoardCount:         30,
		MaxStreak:          7,
		FinalBreakRate:     0.16,
		PreviousLimitUpRet: 4.5,
		PreviousBoardRet:   5.2,
		OpenPremium:        3.2,
		CoreReturn:         4.1,
		AdvanceRate:        0.72,
		ThemeFocus:         0.55,
		LadderContinuity:   1.0,
		HighSampleCount:    12,
		HighWeakCount:      2,
		HighKill:           1,
		HighAverageReturn:  2.5,
		HighDownRate:       0.17,
		HighAdvanceRate:    0.8,
		HighRiskScore:      20,
		QuoteCoverage:      0.95,
	}
	snapshot := scoreMarketEmotion("2026-10-23", raw, nil, time.Now())
	if snapshot.Scores.Total < 70 {
		t.Fatalf("total = %v, want a strong score (%+v)", snapshot.Scores.Total, snapshot.Scores)
	}
	if phase := snapshot.Phase; phase == "冰点" {
		t.Fatalf("strong day should not be classified as 冰点: %+v", snapshot.Scores)
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

func TestScoreIntradayEmotionTracksLiveInputs(t *testing.T) {
	snapshot := marketemotion.IntradaySnapshot{
		RiskScore: 15,
		SealRate:  44.0 / 61.0,
		Metrics: marketemotion.IntradayMetrics{
			LimitUpCount:              44,
			BoardCount:                15,
			CurrentMaxStreak:          4,
			PreviousMaxStreak:         5,
			HeightCollapse:            1,
			BrokenCount:               17,
			LimitDownCount:            6,
			PoolAvailable:             true,
			PreviousLimitUpReturn:     3.68,
			PreviousLimitUpQuoteCount: 30,
			HighAverageReturn:         1,
			HighQuoteCount:            8,
			HighDownRate:              0.375,
		},
	}
	scores, ok := scoreIntradayEmotion(snapshot)
	if !ok {
		t.Fatal("expected an intraday score")
	}
	if want := round2(0.35*scores.Heat + 0.40*scores.Profit + 0.25*scores.Structure); scores.Total != want {
		t.Fatalf("total = %v, want %v (published weights)", scores.Total, want)
	}
	if scores.Profit < 70 {
		t.Fatalf("yesterday's limit-up return should lift profit: %+v", scores)
	}
	if scores.Heat < 45 || scores.Heat > 55 {
		t.Fatalf("heat out of expected range: %+v", scores)
	}
}

func TestApplyIntradayPoolsSkipsMissingFactors(t *testing.T) {
	base := marketemotion.IntradaySnapshot{
		RiskScore: 20,
		Metrics: marketemotion.IntradayMetrics{
			LimitUpCount:              30,
			BoardCount:                8,
			CurrentMaxStreak:          3,
			PreviousMaxStreak:         3,
			PreviousLimitUpReturn:     1,
			PreviousLimitUpQuoteCount: 12,
			HighAverageReturn:         0.5,
			HighQuoteCount:            5,
			HighDownRate:              0.2,
		},
	}
	withoutPools := applyIntradayPools(base, marketEmotionSidePools{}, false)
	if !withoutPools.ScoreAvailable || withoutPools.Metrics.PoolAvailable || withoutPools.Metrics.BrokenCount != 0 {
		t.Fatalf("missing pools should not be written as zero: %+v", withoutPools)
	}
	withPools := applyIntradayPools(base, marketEmotionSidePools{
		broken: make([]foundation.MarketLimitEvent, 17),
		down:   make([]foundation.MarketLimitEvent, 6),
	}, true)
	if !withPools.ScoreAvailable || !withPools.Metrics.PoolAvailable {
		t.Fatalf("expected a score with pools: %+v", withPools)
	}
	if withPools.Metrics.BrokenCount != 17 || withPools.Metrics.LimitDownCount != 6 || withPools.SealRate <= 0.6 {
		t.Fatalf("pool inputs not applied: %+v", withPools)
	}
	if withPools.Scores.Heat == withoutPools.Scores.Heat {
		t.Fatalf("seal/limit-down factors should change heat: %+v vs %+v", withPools.Scores, withoutPools.Scores)
	}
}
