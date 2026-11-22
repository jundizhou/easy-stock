package monitor

import (
	"context"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

// 港股基准必须切到恒生指数：A 股宽基与港股交易日/成分不同，
// 拿深证成指对照会把两个市场的系统性差异算成个股偏离。
func TestDeviationIndexIDUsesHangSengForHongKong(t *testing.T) {
	for _, symbol := range []string{"00700.HK", "hk09988", "09988"} {
		if got := deviationIndexID("", symbol); got != "hsi" {
			t.Fatalf("deviationIndexID(%q) = %q, want hsi", symbol, got)
		}
	}
	if got := deviationIndexID("BoardMain", "600519.SH"); got == "hsi" {
		t.Fatalf("A 股不应使用恒生指数作为基准: %q", got)
	}
}

// 异动明细要能显示港股基准名称，否则页面上只剩一个 hsi 代码。
func TestAbnormalBenchmarkLabelsCoverHangSeng(t *testing.T) {
	if label := abnormalBenchmarkLabels[deviationIndexID("", "00700.HK")]; label != "恒生指数" {
		t.Fatalf("hk benchmark label = %q, want 恒生指数", label)
	}
}

// 港股没有交易所异常波动认定，必须带上口径说明；A 股不应出现该提示。
func TestAbnormalThresholdNoteOnlyForHongKong(t *testing.T) {
	if note := abnormalThresholdNote("00700.HK"); note == "" {
		t.Fatal("港股异动阈值必须说明口径")
	}
	if note := abnormalThresholdNote("600519.SH"); note != "" {
		t.Fatalf("A 股不该带港股口径提示：%q", note)
	}
}

func TestHongKongDeviationRuleWithoutMainlandSnapshot(t *testing.T) {
	const symbol = "00700.HK"
	for _, tc := range []struct {
		name, clock string
		stale       bool
		wantEvents  int
	}{
		{"joint session", "2026-10-26 10:00", false, 1},
		{"mainland holiday", "2026-10-02 15:30", false, 1},
		{"stale daily bars", "2026-10-02 15:30", true, 0},
		{"Hong Kong holiday", "2026-10-19 10:00", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := auditAbnormalSampleEngine(t, 40)
			now, err := time.ParseInLocation("2006-01-02 15:04", tc.clock, foundation.AStockLocation)
			if err != nil {
				t.Fatal(err)
			}
			e.now = func() time.Time { return now }
			index := e.indexSeries["sse_a"]
			stock := e.kline.(abnormalSampleKline)
			cursor := now
			if tc.stale {
				cursor = cursor.AddDate(0, 0, -1)
			}
			for i := len(stock.bars) - 1; i >= 0; i-- {
				for !foundation.IsHongKongTradingDay(cursor) {
					cursor = cursor.AddDate(0, 0, -1)
				}
				stock.bars[i].Time, index.bars[i].Time = cursor, cursor
				cursor = cursor.AddDate(0, 0, -1)
			}
			stock.bars[len(stock.bars)-1].Close = 13
			index.FetchedAt = now
			e.kline, e.indexSeries["hsi"] = stock, index
			rule := Rule{ID: "hk-dev", Name: "三日偏离", Type: RuleTypeDeviation, Enabled: true, Severity: SeverityInfo,
				Scope: ScopeSymbols, Symbols: []string{symbol}, Logic: "or", Conditions: []Condition{{Field: "dev_3d", Op: OpGTE, Value: 20}}}
			ctx := context.Background()
			if err := e.store.UpsertRule(ctx, rule); err != nil {
				t.Fatal(err)
			}
			e.runDeviationTask(ctx, now, now.Format("2006-01-02"))
			events, err := e.store.ListEvents(ctx, EventFilter{RuleID: rule.ID})
			if err != nil || len(events) != tc.wantEvents {
				t.Fatalf("events=%v deviation=%+v err=%v", events, e.deviations[symbol], err)
			}
			if tc.wantEvents > 0 && (events[0].Symbol != symbol || e.deviationEventCounts(ctx, 1)[symbol] != 1) {
				t.Fatalf("event missing from HK session: %+v", events)
			}
		})
	}
}

func TestHongKongLimitRulesUnavailable(t *testing.T) {
	obs := observationFromQuote(foundation.Quote{Symbol: "00700.HK", PreviousClose: 100, High: 112, Price: 112}, true, foundation.MarketSnapshotRow{}, false, 0)
	for _, field := range []string{"limit_up", "broken_limit", "limit_down"} {
		if _, ok := observationValue(obs, field); ok {
			t.Errorf("%s must be unavailable for Hong Kong equities", field)
		}
	}
}
