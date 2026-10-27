package monitor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func auditEngine(t *testing.T) (*Engine, time.Time) {
	t.Helper()
	e := NewEngine(EngineConfig{Store: newTestStore(t)})
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, foundation.AStockLocation)
	e.now = func() time.Time { return now }
	return e, now
}

func auditPriceRule(op string) Rule {
	return Rule{ID: "audit", Name: "audit fixture", Type: RuleTypePrice, Severity: SeverityInfo, Enabled: true,
		Scope: ScopeSymbols, Symbols: []string{"600000.SH"}, Logic: "and", CooldownSeconds: 30,
		Conditions: []Condition{{Field: "last", Op: op, Value: 10}}}
}

func auditAbnormalSampleEngine(t *testing.T, count int) (*Engine, time.Time) {
	t.Helper()
	e, now := auditEngine(t)
	bars, indexBars, closes := make([]foundation.KLine, count), make([]foundation.KLine, count), make([]float64, count)
	cursor := now
	for i := count - 1; i >= 0; i-- {
		for !foundation.IsAStockTradingDay(cursor) {
			cursor = cursor.AddDate(0, 0, -1)
		}
		bars[i] = foundation.KLine{Time: cursor, Open: 10, High: 10, Low: 10, Close: 10}
		indexBars[i], closes[i] = foundation.KLine{Time: cursor, Close: 100}, 100
		cursor = cursor.AddDate(0, 0, -1)
	}
	e.kline = abnormalSampleKline{bars: bars}
	e.indexSeries["sse_a"] = cachedIndexSeries{bars: indexBars, closes: closes, FetchedAt: now}
	row := foundation.MarketSnapshotRow{Symbol: "600000.SH", Name: "样本验证", Board: foundation.BoardMain, Price: 13, PreviousClose: 10, ChangePercent: 30}
	e.snapRows, e.snapBySymbol[row.Symbol], e.snapFetchedAt = []foundation.MarketSnapshotRow{row}, row, now
	return e, now
}

func TestAudit_AbnormalPartialWindowsRemainVisibleAndEvaluateAvailableWindows(t *testing.T) {
	e, now := auditAbnormalSampleEngine(t, 15)
	ctx := context.Background()
	results := e.computeCandidates(ctx, []string{"600000.SH"}, now)
	partial, ok := results.abnormal["600000.SH"]
	if !ok || partial.Available != [3]bool{true, true, false} {
		t.Fatalf("valid short windows disappeared: %+v", results.abnormal)
	}
	if len(results.dev) != 0 {
		t.Fatalf("partial page data leaked into complete rule results: %+v", results.dev)
	}
	view, err := e.Abnormal(ctx, "deviation", 200)
	if err != nil || len(view.Items) != 1 {
		t.Fatalf("15-bar triggered stock was dropped: view=%+v err=%v", view, err)
	}
	item := view.Items[0]
	calc, err := e.AbnormalCalc(ctx, item.Symbol)
	if err != nil || len(item.Windows) != 2 || len(calc.Windows) != 2 || item.Level != calc.Level || item.WindowAvailability[AbnormalWindow30d] {
		t.Fatalf("list and detail availability disagree: item=%+v calc=%+v err=%v", item, calc, err)
	}
	for i, window := range item.Windows {
		if window.Key == AbnormalWindow30d || window.Dev != calc.Windows[i].Dev || window.Progress != calc.Windows[i].Progress {
			t.Fatalf("list/detail values disagree: %+v / %+v", item.Windows, calc.Windows)
		}
	}
	if len(e.deviations) != 0 {
		t.Fatalf("partial values changed the background deviation cache: %+v", e.deviations)
	}
	rule := Rule{ID: "partial-window-guard", Name: "partial guard", Type: RuleTypeDeviation, Enabled: true, Severity: SeverityInfo,
		Scope: ScopeSymbols, Symbols: []string{item.Symbol}, Logic: "or", Conditions: []Condition{{Field: "dev_3d", Op: OpGTE, Value: 20}}}
	if err := e.store.UpsertRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	e.runDeviationTask(ctx, now, "2026-10-26")
	events, err := e.store.ListEvents(ctx, EventFilter{RuleID: rule.ID, Limit: 10})
	if err != nil || len(events) != 1 {
		t.Fatalf("valid 3d rule must not depend on unavailable 30d data: events=%+v err=%v", events, err)
	}
	// Snapshot fallback must preserve the same valid windows, rather than require all 30 days.
	fallback := AbnormalView{}
	if !e.fillAbnormalWithoutSnapshot(ctx, &fallback) || len(fallback.Items) != 1 || len(fallback.Items[0].Windows) != 2 {
		t.Fatalf("fallback lost partial windows: %+v", fallback)
	}
}

func TestAudit_AbnormalMissingIndexEndpointIsNotReportedAsZero(t *testing.T) {
	e, now := auditAbnormalSampleEngine(t, 40)
	series := e.indexSeries["sse_a"]
	series.bars[len(series.bars)-4].Close = 0
	e.indexSeries["sse_a"] = series
	results := e.computeCandidates(context.Background(), []string{"600000.SH"}, now)
	if got := results.abnormal["600000.SH"].Available; got != [3]bool{false, true, true} {
		t.Fatalf("index gap availability=%v", got)
	}
	calc, err := e.AbnormalCalc(context.Background(), "600000.SH")
	if err != nil || len(calc.Windows) != 2 || calc.WindowAvailability[AbnormalWindow3d] || !strings.Contains(calc.Error, "基准数据缺失") {
		t.Fatalf("index gap must leave 3d unavailable and preserve longer windows: %+v err=%v", calc, err)
	}
	for _, window := range calc.Windows {
		if window.Key == AbnormalWindow3d {
			t.Fatalf("missing 3d endpoint fabricated zero: %+v", window)
		}
	}
}

func TestAudit_CrossUpMustSeeBelowThresholdObservation(t *testing.T) {
	e, now := auditEngine(t)
	rule := auditPriceRule(OpCrossUp)
	e.evaluateRule(context.Background(), rule, Observation{Price: 9}, "600000.SH", "fixture", now, "2026-10-26")
	got := e.evaluateRule(context.Background(), rule, Observation{Price: 11}, "600000.SH", "fixture", now.Add(time.Minute), "2026-10-26")
	if got != 1 {
		t.Fatalf("9 -> 11 across threshold 10: want 1 event, got %d", got)
	}
}

func TestAudit_ThresholdMustRearmOutsideRange(t *testing.T) {
	e, now := auditEngine(t)
	rule := auditPriceRule(OpGTE)
	got := 0
	for i, price := range []float64{11, 9, 11} {
		got += e.evaluateRule(context.Background(), rule, Observation{Price: price}, "600000.SH", "fixture", now.Add(time.Duration(i)*time.Minute), "2026-10-26")
	}
	if got != 2 {
		t.Fatalf("11 -> 9 -> 11 with expired cooldown: want 2 events, got %d", got)
	}
}

func TestAudit_CrossDownCurrentValueUsesDownwardComparison(t *testing.T) {
	rule := auditPriceRule(OpCrossDown)
	if got := len(EvaluateRule(rule, Observation{Price: 9})); got != 1 {
		t.Fatalf("price 9 is below threshold 10: want 1 downward signal, got %d", got)
	}
}

func TestAudit_EqualMustNotMatchHigherValue(t *testing.T) {
	rule := auditPriceRule(OpEqual)
	if got := len(EvaluateRule(rule, Observation{Price: 11})); got != 0 {
		t.Fatalf("11 must not equal 10: got %d signals", got)
	}
}

func TestAudit_LotStopLossWithTakeProfit(t *testing.T) {
	e, now := auditEngine(t)
	rule, ok := lotPriceRule(Lot{ID: "audit-lot", Symbol: "600000.SH", Name: "fixture", CostPrice: 10, Quantity: 100, Enabled: true, TargetPct: 20, StopPct: 8})
	if !ok {
		t.Fatal("lot did not produce a rule")
	}
	if got := e.evaluateRule(context.Background(), rule, Observation{Price: 9}, "600000.SH", "fixture", now, "2026-10-26"); got != 1 {
		t.Fatalf("price 9 crossed stop 9.2 (target 12 also configured): want 1 stop event, got %d", got)
	}
}

func TestAudit_HighLowRuleMustHonorSpecifiedSymbol(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	rule := Rule{ID: "audit-high", Name: "only A", Enabled: true, Type: RuleTypeAbnormal, Source: SourceManual,
		Scope: ScopeSymbols, Symbols: []string{"600000.SH"}, Severity: SeverityInfo, Logic: "or", Conditions: []Condition{{Field: "high_60d", Op: OpGTE, Value: 1}}}
	if err := e.store.UpsertRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	for _, symbol := range []string{"600000.SH", "600001.SH"} {
		row := foundation.MarketSnapshotRow{Symbol: symbol, Board: foundation.BoardMain, Price: 11, PreviousClose: 10, ChangePercent: 10}
		e.snapRows = append(e.snapRows, row)
		e.snapBySymbol[symbol] = row
		bars := make([]foundation.KLine, 120)
		for i := range bars {
			bars[i] = foundation.KLine{Time: now.AddDate(0, 0, i-119), Close: 10, High: 10, Low: 9, Open: 10}
		}
		bars[len(bars)-1].Close = 11
		e.klines[symbol] = klineEntry{bars: bars, day: "2026-10-26", FetchedAt: now}
	}
	e.snapFetchedAt = now
	e.runDeviationTask(ctx, now, "2026-10-26")
	events, err := e.store.ListEvents(ctx, EventFilter{RuleID: rule.ID, Symbol: "600001.SH", Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("rule explicitly limited to 600000.SH emitted %d events for 600001.SH", len(events))
	}
}

func TestAudit_DeviationEventCountMustReadBeyond100(t *testing.T) {
	e, _ := auditEngine(t)
	ctx := context.Background()
	for i := 0; i < 150; i++ {
		if _, err := e.store.AppendEvent(ctx, Event{RuleID: "count", RuleType: RuleTypeDeviation, Symbol: "600000.SH", TradeDate: "2026-10-26"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := e.deviationEventCounts(ctx, 10)["600000.SH"]; got != 150 {
		t.Fatalf("stored 150 deviation events; count got %d", got)
	}
}

func TestAudit_DeviationCountsUseTradingSessionsNotEventDates(t *testing.T) {
	e, _ := auditEngine(t)
	ctx := context.Background()
	// Monday and the preceding Friday are the latest two sessions, even if
	// Friday has no events. Older events must not fill that empty session.
	for _, date := range []string{"2026-10-22", "2026-10-26", "2026-10-27"} {
		if _, err := e.store.AppendEvent(ctx, Event{RuleID: "fixture", RuleType: RuleTypeDeviation, Symbol: "600000.SH", TradeDate: date}); err != nil {
			t.Fatal(err)
		}
	}
	if got := e.deviationEventCounts(ctx, 2)["600000.SH"]; got != 1 {
		t.Fatalf("only Monday belongs to the latest two sessions, got %d", got)
	}
}

type auditDegradedSnapshot struct {
	stale bool
}

func (source *auditDegradedSnapshot) MarketSnapshot(context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	meta := foundation.SourceMeta{Source: "fixture", Stale: source.stale}
	if source.stale {
		meta.FallbackReason = "fixture partial coverage"
	}
	return []foundation.MarketSnapshotRow{{Symbol: "600000.SH", Price: 10}}, meta, nil
}

func TestAudit_SnapshotStatusRetainsDegradationAndRealAge(t *testing.T) {
	e, now := auditEngine(t)
	source := &auditDegradedSnapshot{stale: true}
	e.snapshot = source
	if _, err := e.snapshotRows(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := e.CurrentStatus()
	if !status.SnapshotStale || status.SnapshotSource != "fixture" || status.SnapshotWarning != "fixture partial coverage" || !status.SessionOpen {
		t.Fatalf("source degradation disappeared: %+v", status)
	}
	source.stale = false
	now = now.Add(11 * time.Second)
	e.now = func() time.Time { return now }
	_, _ = e.snapshotRows(context.Background())
	if status := e.CurrentStatus(); status.SnapshotStale || status.SnapshotWarning != "" {
		t.Fatalf("fresh source did not clear degradation: %+v", status)
	}
	now = now.Add(61 * time.Second)
	if status := e.CurrentStatus(); !status.SnapshotStale || status.SnapshotAgeSeconds != 61 {
		t.Fatalf("stalled stream must keep its real age: %+v", status)
	}
}

func TestAudit_OverviewCountsMustNotCapAt500(t *testing.T) {
	e, _ := auditEngine(t)
	ctx := context.Background()
	date := time.Now().In(foundation.AStockLocation).Format("2006-01-02")
	for i := 0; i < 550; i++ {
		if _, err := e.store.AppendEvent(ctx, Event{RuleID: fmt.Sprint(i % 3), RuleType: RuleTypePrice, Severity: SeverityCritical, TradeDate: date}); err != nil {
			t.Fatal(err)
		}
	}
	view, err := e.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.EventsToday != 550 || view.CriticalToday != 550 {
		t.Fatalf("stored 550 critical events; overview events=%d critical=%d", view.EventsToday, view.CriticalToday)
	}
}

func TestAudit_FailedDigestMustNotBeMarkedMerged(t *testing.T) {
	e, now := auditEngine(t)
	e.notify = func(context.Context, Event) (string, error) { return "", errors.New("test delivery failed") }
	e.settings = func() Settings { return Settings{DigestSeconds: 30} }
	ctx := context.Background()
	e.dispatch(ctx, Event{RuleID: "digest-test", Severity: SeverityWarning, Symbol: "600000.SH", Message: "fixture", TradeDate: "2026-10-26"})
	e.flushDigest(ctx, now.Add(time.Minute))
	events, err := e.store.ListEvents(ctx, EventFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].NotifyStatus != NotifyFailed {
		t.Fatalf("failed digest delivery must surface failure; events=%+v", events)
	}
}

func TestAudit_WarningDigestMustContainTriggerDetails(t *testing.T) {
	e, now := auditEngine(t)
	var sent Event
	e.notify = func(_ context.Context, event Event) (string, error) { sent = event; return NotifyQueued, nil }
	e.settings = func() Settings { return Settings{DigestSeconds: 30} }
	ctx := context.Background()
	e.dispatch(ctx, Event{RuleID: "digest-test", Severity: SeverityWarning, Symbol: "600000.SH", Message: "fixture symbol crossed threshold", TradeDate: "2026-10-26"})
	e.flushDigest(ctx, now.Add(time.Minute))
	if sent.Message == "1 条提醒已合并发送" {
		t.Fatalf("digest drops all symbols/rules/trigger values: %q", sent.Message)
	}
}

func TestAudit_PriceProjectionUsesWindowBase(t *testing.T) {
	// Flat benchmark, window start 10, current 11.80 => 18%; threshold 20% => 12.00.
	windows := evalAbnormalWindows(18, 0, 0, foundation.BoardMain, false, 11.8, 10)
	if windows[0].TriggerPrice != 12 {
		t.Fatalf("base 10, current 11.8, deviation 18%% => threshold price 12; got %.2f", windows[0].TriggerPrice)
	}
}

func TestAudit_DeviationRuleMustHonorSpecifiedSymbol(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	rule := Rule{ID: "audit-dev-scope", Name: "only A", Enabled: true, Type: RuleTypeDeviation, Source: SourceManual,
		Scope: ScopeSymbols, Symbols: []string{"600000.SH"}, Severity: SeverityInfo, Logic: "or", Conditions: []Condition{{Field: "dev_3d", Op: OpGTE, Value: 20}}}
	if err := e.store.UpsertRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	indexBars := make([]foundation.KLine, 120)
	closes := make([]float64, 120)
	for i := range indexBars {
		indexBars[i] = foundation.KLine{Time: now.AddDate(0, 0, i-119), Close: 100}
		closes[i] = 100
	}
	e.indexSeries["sse_a"] = cachedIndexSeries{bars: indexBars, closes: closes, FetchedAt: now}
	for _, symbol := range []string{"600000.SH", "600001.SH"} {
		row := foundation.MarketSnapshotRow{Symbol: symbol, Name: "fixture", Board: foundation.BoardMain, Price: 13, PreviousClose: 10, ChangePercent: 30}
		e.snapRows = append(e.snapRows, row)
		e.snapBySymbol[symbol] = row
		bars := make([]foundation.KLine, 120)
		for i := range bars {
			bars[i] = foundation.KLine{Time: indexBars[i].Time, Close: 10, High: 10, Low: 9, Open: 10}
		}
		e.klines[symbol] = klineEntry{bars: bars, day: "2026-10-26", FetchedAt: now}
	}
	e.snapFetchedAt = now
	e.runDeviationTask(ctx, now, "2026-10-26")
	events, err := e.store.ListEvents(ctx, EventFilter{RuleID: rule.ID, Symbol: "600001.SH", Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("deviation rule only covers 600000.SH but emitted %d events for 600001.SH", len(events))
	}
}

func TestAudit_TodaysQuoteMustNotRewriteFridaysBar(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	const symbol = "600000.SH"
	row := foundation.MarketSnapshotRow{Symbol: symbol, Name: "fixture", Board: foundation.BoardMain, Price: 13, PreviousClose: 10, ChangePercent: 30}
	e.snapRows = []foundation.MarketSnapshotRow{row}
	e.snapBySymbol[symbol] = row
	e.snapFetchedAt = now
	bars := make([]foundation.KLine, 120)
	indexBars := make([]foundation.KLine, 121)
	closes := make([]float64, 121)
	cursor := now.AddDate(0, 0, -3)
	for i := len(bars) - 1; i >= 0; i-- {
		for cursor.Weekday() == time.Saturday || cursor.Weekday() == time.Sunday {
			cursor = cursor.AddDate(0, 0, -1)
		}
		bars[i] = foundation.KLine{Time: cursor, Close: 10, Open: 10, High: 10, Low: 10}
		indexBars[i] = foundation.KLine{Time: cursor, Close: 100}
		closes[i] = 100
		cursor = cursor.AddDate(0, 0, -1)
	}
	indexBars[120] = foundation.KLine{Time: now, Close: 100}
	closes[120] = 100
	e.klines[symbol] = klineEntry{bars: bars, day: "2026-10-26", FetchedAt: now}
	e.indexSeries["sse_a"] = cachedIndexSeries{bars: indexBars, closes: closes, FetchedAt: now}
	view, err := e.AbnormalCalc(ctx, symbol)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.Error, "历史收盘") {
		t.Fatalf("missing historical-data warning: %+v", view)
	}
	last := view.Rows[len(view.Rows)-1]
	if last.Date != "2026-10-23" || last.StockChange != 0 {
		t.Fatalf("Monday quote rewrote historical Friday: last_date=%s, Friday_change=%.2f%%, trigger_date=%s", last.Date, last.StockChange, view.LastTriggerDate)
	}
	for _, window := range view.Windows {
		if window.TriggerPrice != 0 {
			t.Fatalf("historical window must not project from Monday quote: %+v", window)
		}
	}
	// A historically triggered window is not a fresh Monday signal.
	bars[len(bars)-1].Close = 13
	rule := Rule{ID: "stale-dev", Name: "fixture", Enabled: true, Type: RuleTypeDeviation, Source: SourceManual,
		Scope: ScopeSymbols, Symbols: []string{symbol}, Severity: SeverityInfo, Logic: "or", Conditions: []Condition{{Field: "dev_3d", Op: OpGTE, Value: 20}}}
	if err := e.store.UpsertRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	e.runDeviationTask(ctx, now, "2026-10-26")
	events, _ := e.store.ListEvents(ctx, EventFilter{RuleID: rule.ID, Limit: 10})
	if len(events) != 0 {
		t.Fatalf("stale Friday series generated current-session events: %+v", events)
	}
}

func TestAudit_MainBoardSTMustUseCurrentTenPercentLimit(t *testing.T) {
	row := foundation.MarketSnapshotRow{Symbol: "600000.SH", Board: foundation.BoardMain, IsST: true, Price: 10.6, High: 10.6, PreviousClose: 10}
	if got := LimitPrice(row.Board, row.IsST, row.PreviousClose); got != 11 {
		t.Fatalf("2026-10-26 main-board ST previous close 10: current 10%% limit must be 11.00, got %.2f; +6%% quote limitState=%d", got, limitState(row))
	}
}

func TestAudit_CrossingFromZeroAndCooldown(t *testing.T) {
	e, now := auditEngine(t)
	rule := auditPriceRule(OpCrossUp)
	rule.Conditions = []Condition{{Field: "change_pct", Op: OpCrossUp, Value: 1}}
	for i, value := range []float64{0, 2, 0, 2} {
		got := e.evaluateRule(context.Background(), rule, Observation{ChangePercent: value}, "600000.SH", "fixture", now.Add(time.Duration(i)*time.Second), "2026-10-26")
		want := 0
		if i == 1 {
			want = 1
		}
		if got != want {
			t.Fatalf("step %d got %d want %d", i, got, want)
		}
	}
}

func TestAudit_ANDDoesNotConsumePartiallySatisfiedConditions(t *testing.T) {
	e, now := auditEngine(t)
	rule := auditPriceRule(OpGTE)
	rule.Conditions = append(rule.Conditions, Condition{Field: "change_pct", Op: OpGTE, Value: 2})
	ctx := context.Background()
	if n := e.evaluateRule(ctx, rule, Observation{Price: 11, ChangePercent: 1}, "600000.SH", "fixture", now, "2026-10-26"); n != 0 {
		t.Fatal("partial AND fired")
	}
	if n := e.evaluateRule(ctx, rule, Observation{Price: 11, ChangePercent: 3}, "600000.SH", "fixture", now.Add(time.Minute), "2026-10-26"); n != 2 {
		t.Fatalf("all conditions should remain armed; got %d", n)
	}
}

func TestAudit_StopThenTargetHaveIndependentStateAndDetails(t *testing.T) {
	e, now := auditEngine(t)
	rule, _ := lotPriceRule(Lot{ID: "independent", Symbol: "600000.SH", CostPrice: 10, Quantity: 100, Enabled: true, TargetPct: 20, StopPct: 8})
	rule.Severity = SeverityInfo
	ctx := context.Background()
	for i, price := range []float64{9, 13} {
		if n := e.evaluateRule(ctx, rule, Observation{Price: price}, "600000.SH", "fixture", now.Add(time.Duration(i)*time.Minute), "2026-10-26"); n != 1 {
			t.Fatalf("step%d got%d", i, n)
		}
	}
	items, _ := e.store.ListEvents(ctx, EventFilter{Limit: 10, OldestFirst: true})
	if len(items) != 2 || items[0].Detail["op"] != OpLTE || items[1].Detail["op"] != OpGTE {
		t.Fatalf("wrong condition identity: %+v", items)
	}
}

func TestAudit_DigestRetriesAndHonorsSkipped(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	calls := 0
	e.notify = func(_ context.Context, event Event) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("temporary failure")
		}
		if !strings.Contains(event.Message, "threshold") {
			t.Fatal("digest dropped details")
		}
		return NotifyQueued, nil
	}
	e.dispatch(ctx, Event{RuleID: "retry", Severity: SeverityWarning, Symbol: "600000.SH", Message: "fixture crossed threshold"})
	e.flushDigest(ctx, now.Add(time.Hour))
	e.flushDigest(ctx, now.Add(time.Hour+time.Minute))
	items, _ := e.store.ListEvents(ctx, EventFilter{Limit: 10})
	if calls != 2 || items[0].NotifyStatus != NotifyMerged || items[0].NotifyError != "" {
		t.Fatalf("retry outcome: calls=%d items=%+v", calls, items)
	}
	e.notify = func(context.Context, Event) (string, error) { return NotifySkipped, nil }
	e.dispatch(ctx, Event{RuleID: "disabled", Severity: SeverityWarning, Message: "fixture"})
	e.flushDigest(ctx, now.Add(2*time.Hour))
	items, _ = e.store.ListEvents(ctx, EventFilter{Limit: 10})
	if items[0].NotifyStatus != NotifySkipped {
		t.Fatal("disabled notification was marked merged")
	}
}

func TestAudit_RadarInvalidationForceAndWatchCoverage(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	e.radarReport = &RadarReport{Summary: "old cache"}
	e.radarFetchedAt = now
	if got := e.RadarScan(ctx).Summary; got != "old cache" {
		t.Fatal("regular call did not use cache")
	}
	if got := e.RadarScan(ctx, true).Summary; got == "old cache" {
		t.Fatal("forced refresh reused cache")
	}
	e.radarReport = &RadarReport{Summary: "old cache"}
	e.radarFetchedAt = now
	if err := e.AddWatchlist(ctx, "600000.SH", "fixture", ""); err != nil {
		t.Fatal(err)
	}
	if !e.radarFetchedAt.IsZero() {
		t.Fatal("watch mutation did not invalidate radar")
	}
	rows := make([]foundation.MarketSnapshotRow, 500)
	for i := range rows {
		rows[i] = foundation.MarketSnapshotRow{Symbol: fmt.Sprintf("%06d.SH", i+1), Price: 10, PreviousClose: 10, ChangePercent: float64(i)}
	}
	pool := e.deviationCandidates(ctx, rows)
	found := false
	for _, s := range pool {
		if s == "600000.SH" {
			found = true
		}
	}
	if !found {
		t.Fatal("monitored symbol omitted from candidates")
	}
}
