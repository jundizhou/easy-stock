package monitor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

// All providers and notifications below are isolated fixtures. Stores use t.TempDir.
type reliabilitySnapshot struct {
	rows []foundation.MarketSnapshotRow
	meta foundation.SourceMeta
	err  error
}

func (p reliabilitySnapshot) MarketSnapshot(context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	return p.rows, p.meta, p.err
}

type reliabilityQuotes struct {
	rows  []foundation.Quote
	calls [][]string
	err   error
}

func (p *reliabilityQuotes) Realtime(_ context.Context, symbols []string) ([]foundation.Quote, error) {
	p.calls = append(p.calls, append([]string(nil), symbols...))
	var rows []foundation.Quote
	for _, row := range p.rows {
		if containsString(symbols, row.Symbol) {
			rows = append(rows, row)
		}
	}
	return rows, p.err
}

type reliabilityPortfolio struct {
	rows []Holding
	err  error
}

func (p *reliabilityPortfolio) Holdings(context.Context) ([]Holding, error)    { return p.rows, p.err }
func (p *reliabilityPortfolio) WatchSymbols(context.Context) ([]string, error) { return nil, nil }

func reliabilityExec(t *testing.T, e *Engine, query string) {
	t.Helper()
	if _, err := e.store.db.ExecContext(context.Background(), query); err != nil {
		t.Fatal(err)
	}
}

func reliabilityEvents(t *testing.T, e *Engine) []Event {
	t.Helper()
	events, err := e.Events(context.Background(), EventFilter{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestReliability_SnapshotFreshnessGuardsCurrentAlerts(t *testing.T) {
	for _, mode := range []string{"fresh", "offline", "partial-error", "stale", "carry", "old-source-time", "wrong-meta-day", "wrong-row-day"} {
		t.Run(mode, func(t *testing.T) {
			e, now := auditEngine(t)
			row := foundation.MarketSnapshotRow{Symbol: "600000.SH", Price: 9, PreviousClose: 10, TradeDate: "2026-10-26"}
			provider := reliabilitySnapshot{rows: []foundation.MarketSnapshotRow{row}, meta: foundation.SourceMeta{FetchedAt: now, TradeDate: "2026-10-26"}}
			e.snapRows, e.snapBySymbol[row.Symbol], e.snapFetchedAt = provider.rows, row, now.Add(-24*time.Hour)
			switch mode {
			case "offline":
				provider.rows, provider.err = nil, errors.New("fixture offline")
			case "partial-error":
				provider.err = errors.New("fixture partial fetch")
			case "stale":
				provider.meta.Stale = true
			case "carry":
				provider.meta.CarryForward = true
			case "old-source-time":
				provider.meta.FetchedAt = now.Add(-2 * time.Minute)
			case "wrong-meta-day":
				provider.meta.TradeDate = "2026-10-23"
			case "wrong-row-day":
				provider.rows[0].TradeDate = "2026-10-23"
			}
			e.snapshot = provider
			rule := auditPriceRule(OpLTE)
			rule.Scope, rule.Symbols = ScopeAll, nil
			if _, err := e.SaveRule(context.Background(), rule); err != nil {
				t.Fatal(err)
			}
			e.runSnapshotTask(context.Background(), now, "2026-10-26")
			want := 0
			if mode == "fresh" {
				want = 1
			}
			if got := len(reliabilityEvents(t, e)); got != want {
				t.Fatalf("events=%d want=%d", got, want)
			}
			if mode != "fresh" {
				runs, err := e.Runs(context.Background(), 10)
				if err != nil || len(runs) == 0 || runs[0].Error == "" {
					t.Fatalf("failure must be visible: runs=%+v err=%v", runs, err)
				}
			}
		})
	}
}

func TestReliability_FreshQuotesWorkWhenSnapshotIsInvalid(t *testing.T) {
	for _, mode := range []string{"fresh", "stale", "carry", "old-fetch", "wrong-meta-day", "wrong-trade-day"} {
		t.Run(mode, func(t *testing.T) {
			e, now := auditEngine(t)
			e.snapRows = []foundation.MarketSnapshotRow{{Symbol: "600000.SH", Price: 1}}
			e.snapBySymbol["600000.SH"] = e.snapRows[0]
			e.snapFetchedAt = now.Add(-time.Hour)
			quote := foundation.Quote{Symbol: "600000.SH", Price: 11, Meta: foundation.SourceMeta{FetchedAt: now}}
			switch mode {
			case "stale":
				quote.Meta.Stale = true
			case "carry":
				quote.Meta.CarryForward = true
			case "old-fetch":
				quote.Meta.FetchedAt = now.Add(-2 * time.Minute)
			case "wrong-meta-day":
				quote.Meta.TradeDate = "2026-10-23"
			case "wrong-trade-day":
				quote.TradeTime = now.AddDate(0, 0, -3)
			}
			e.realtime = &reliabilityQuotes{rows: []foundation.Quote{quote}}
			if _, err := e.SaveRule(context.Background(), auditPriceRule(OpGTE)); err != nil {
				t.Fatal(err)
			}
			e.runQuotesTask(context.Background(), now, "2026-10-26")
			want := 1
			if mode != "fresh" {
				want = 0
			}
			if got := len(reliabilityEvents(t, e)); got != want {
				t.Fatalf("events=%d want=%d", got, want)
			}
		})
	}
}

func TestReliability_QuoteOnlyUsesActualBoardLimit(t *testing.T) {
	for _, sample := range []struct {
		symbol string
		price  float64
		want   int
	}{
		{"300001.SZ", 11.1, 0}, {"688001.SH", 11.1, 0}, {"920001.BJ", 11.1, 0},
		{"300001.SZ", 12, 1}, {"688001.SH", 12, 1}, {"920001.BJ", 13, 1}, {"600000.SH", 11, 1},
	} {
		t.Run(fmt.Sprintf("%s/%.2f", sample.symbol, sample.price), func(t *testing.T) {
			e, now := auditEngine(t)
			rule := Rule{ID: "board", Name: "fixture", Type: RuleTypeAbnormal, Scope: ScopeSymbols, Symbols: []string{sample.symbol}, Enabled: true, Severity: SeverityInfo, Logic: "and", Conditions: []Condition{{Field: "limit_up", Op: OpGTE, Value: 1}}}
			if _, err := e.SaveRule(context.Background(), rule); err != nil {
				t.Fatal(err)
			}
			e.realtime = &reliabilityQuotes{rows: []foundation.Quote{{Symbol: sample.symbol, Name: "fixture", Price: sample.price, High: sample.price, PreviousClose: 10, ChangePercent: (sample.price/10 - 1) * 100}}}
			e.runQuotesTask(context.Background(), now, "2026-10-26")
			if got := len(reliabilityEvents(t, e)); got != sample.want {
				t.Fatalf("events=%d want=%d", got, sample.want)
			}
		})
	}
}

func TestReliability_OldTradingDayRowsCannotDistortMarketBreadth(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	e.snapFetchedAt = now
	e.snapRows = []foundation.MarketSnapshotRow{
		{Symbol: "600000.SH", Price: 9, ChangePercent: -10, TradeDate: "2026-10-23"},
		{Symbol: "600001.SH", Price: 11, ChangePercent: 10, TradeDate: "2026-10-26"},
	}
	rule := Rule{ID: "breadth", Name: "fixture", Type: RuleTypeMarket, Enabled: true, Severity: SeverityInfo, Scope: ScopeAll, Logic: "and", Conditions: []Condition{{Field: "down_count", Op: OpGTE, Value: 1}}}
	if _, err := e.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	e.runSnapshotTask(ctx, now, "2026-10-26")
	if len(reliabilityEvents(t, e)) != 0 {
		t.Fatal("old-session row produced market-breadth alert")
	}
}

func TestReliability_DateAlertCanRetryAfterFailedPersistence(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	rule := Rule{ID: "date-fixture", Name: "fixture", Type: RuleTypeDate, Enabled: true, Severity: SeverityWarning, Scope: ScopeSymbols, Symbols: []string{"600000.SH"}, RemindDate: "2026-10-26", Conditions: []Condition{{Field: "days_to_remind", Op: OpLTE, Value: 0}}}
	if _, err := e.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	reliabilityExec(t, e, `CREATE TRIGGER reliability_date_fail BEFORE INSERT ON monitor_events BEGIN SELECT RAISE(ABORT,'fixture date write failure'); END`)
	e.runDateRules(ctx, now)
	if len(reliabilityEvents(t, e)) != 0 || len(e.pendingDigest) != 0 {
		t.Fatal("failed date event leaked")
	}
	reliabilityExec(t, e, `DROP TRIGGER reliability_date_fail`)
	e.runDateRules(ctx, now.Add(time.Second))
	e.runDateRules(ctx, now.Add(2*time.Second))
	if len(reliabilityEvents(t, e)) != 1 || len(e.pendingDigest) != 1 {
		t.Fatal("date retry should produce one persisted event and one pending notification")
	}
}

func TestReliability_FailedPersistencePreservesTriggerAndNotificationOrder(t *testing.T) {
	for _, op := range []string{OpGTE, OpCrossUp} {
		for _, failingTable := range []string{"event", "rule-count"} {
			t.Run(op+"/"+failingTable, func(t *testing.T) {
				e, now := auditEngine(t)
				ctx := context.Background()
				rule := auditPriceRule(op)
				rule.Severity = SeverityCritical
				if _, err := e.SaveRule(ctx, rule); err != nil {
					t.Fatal(err)
				}
				if op == OpCrossUp {
					e.evaluateRule(ctx, rule, Observation{Price: 9}, "600000.SH", "fixture", now, "2026-10-26")
				}
				notified := 0
				e.notify = func(_ context.Context, event Event) (string, error) {
					notified++
					if event.ID <= 0 || len(reliabilityEvents(t, e)) != 1 {
						t.Error("notification ran before event commit")
					}
					return "", errors.New("fixture notification failure")
				}
				query := `CREATE TRIGGER reliability_fail BEFORE INSERT ON monitor_events BEGIN SELECT RAISE(ABORT,'fixture write failure'); END`
				if failingTable == "rule-count" {
					query = `CREATE TRIGGER reliability_fail BEFORE UPDATE OF trigger_count ON monitor_rules BEGIN SELECT RAISE(ABORT,'fixture count failure'); END`
				}
				reliabilityExec(t, e, query)
				if got := e.evaluateRule(ctx, rule, Observation{Price: 11}, "600000.SH", "fixture", now, "2026-10-26"); got != 0 {
					t.Fatalf("failed write counted %d", got)
				}
				if len(reliabilityEvents(t, e)) != 0 || notified != 0 {
					t.Fatal("failed transaction leaked event/notification")
				}
				reliabilityExec(t, e, `DROP TRIGGER reliability_fail`)
				if got := e.evaluateRule(ctx, rule, Observation{Price: 11}, "600000.SH", "fixture", now.Add(time.Second), "2026-10-26"); got != 1 {
					t.Fatalf("retry after rollback=%d", got)
				}
				events := reliabilityEvents(t, e)
				rules, err := e.store.ListRules(ctx, "", false)
				if err != nil || notified != 1 || len(events) != 1 || events[0].NotifyStatus != NotifyFailed || len(rules) != 1 || rules[0].TriggerCount != 1 {
					t.Fatalf("events=%+v rules=%+v notified=%d err=%v", events, rules, notified, err)
				}
			})
		}
	}
}

func TestReliability_ClosedPositionsArchiveOnlySyncedLotsAndCanReopen(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	source := &reliabilityPortfolio{rows: []Holding{{Symbol: "600000.SH", Quantity: 100, AverageCost: 10, StopLossPrice: 9, AccountID: "fixture"}}}
	e.portfolio = source
	if _, err := e.SyncPositions(ctx); err != nil {
		t.Fatal(err)
	}
	id := positionLotID("fixture", "600000.SH")
	lot, _, _ := e.store.GetLot(ctx, id)
	lot.RemindDate = "2026-10-26"
	if err := e.ensureLotRules(ctx, lot); err != nil {
		t.Fatal(err)
	}
	manual := Lot{ID: "manual", Source: SourceManual, Symbol: "600001.SH", Quantity: 100, Enabled: true}
	if err := e.store.UpsertLot(ctx, manual); err != nil {
		t.Fatal(err)
	}
	source.rows = nil
	source.err = errors.New("fixture holdings unavailable")
	if _, err := e.SyncPositions(ctx); err == nil {
		t.Fatal("source error swallowed")
	}
	lot, _, _ = e.store.GetLot(ctx, id)
	if !lot.Enabled || lot.Quantity != 100 {
		t.Fatal("source failure archived holding")
	}
	source.err = nil
	if _, err := e.SyncPositions(ctx); err != nil {
		t.Fatal(err)
	}
	lot, _, _ = e.store.GetLot(ctx, id)
	rules, err := e.store.ListRules(ctx, "", true)
	if err != nil || lot.Enabled || lot.Quantity != 0 || len(rules) != 0 {
		t.Fatalf("closed lot=%+v rules=%+v err=%v", lot, rules, err)
	}
	manual, _, _ = e.store.GetLot(ctx, "manual")
	if !manual.Enabled || manual.Quantity != 100 {
		t.Fatal("manual lot changed")
	}
	e.realtime = &reliabilityQuotes{rows: []foundation.Quote{{Symbol: "600000.SH", Price: 8.5}}}
	e.runQuotesTask(ctx, now, "2026-10-26")
	e.runDateRules(ctx, now)
	if len(reliabilityEvents(t, e)) != 0 {
		t.Fatal("archived lot emitted alert")
	}
	source.rows = []Holding{{Symbol: "600000.SH", Quantity: 200, AverageCost: 10, StopLossPrice: 9, AccountID: "fixture"}}
	if _, err := e.SyncPositions(ctx); err != nil {
		t.Fatal(err)
	}
	lot, _, _ = e.store.GetLot(ctx, id)
	if !lot.Enabled || lot.Quantity != 200 {
		t.Fatal("new holding not monitored")
	}
}

func TestReliability_AvailableDeviationWindowsWorkInPreviewAndEngine(t *testing.T) {
	for _, logic := range []string{"and", "or"} {
		t.Run(logic, func(t *testing.T) {
			e, now := auditAbnormalSampleEngine(t, 15)
			ctx := context.Background()
			rule := Rule{ID: "windows", Name: "fixture", Type: RuleTypeDeviation, Enabled: true, Severity: SeverityInfo, Scope: ScopeSymbols, Symbols: []string{"600000.SH"}, Logic: logic, Conditions: []Condition{{Field: "dev_3d", Op: OpGTE, Value: 20}, {Field: "dev_30d", Op: OpGTE, Value: 20}}}
			if _, err := e.SaveRule(ctx, rule); err != nil {
				t.Fatal(err)
			}
			trial, err := e.TestRule(ctx, rule)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if logic == "or" {
				want = 1
			}
			if trial.WouldFire != want {
				t.Fatalf("preview=%+v want=%d", trial, want)
			}
			e.runDeviationTask(ctx, now, "2026-10-26")
			if got := len(reliabilityEvents(t, e)); got != want {
				t.Fatalf("events=%d want=%d", got, want)
			}
		})
	}
}

func TestReliability_DeviationPreviewDoesNotReuseOldCacheWhenDataIsUnavailable(t *testing.T) {
	e, _ := auditEngine(t)
	e.realtime = &reliabilityQuotes{rows: []foundation.Quote{{Symbol: "600000.SH", Price: 13}}}
	e.deviations["600000.SH"] = deviationResult{Dev3: 30}
	rule := Rule{ID: "missing-window", Name: "fixture", Type: RuleTypeDeviation, Enabled: true, Scope: ScopeSymbols,
		Symbols: []string{"600000.SH"}, Conditions: []Condition{{Field: "dev_3d", Op: OpGTE, Value: 20}}}
	trial, err := e.TestRule(context.Background(), rule)
	if err != nil || trial.WouldFire != 0 {
		t.Fatalf("missing data reused an old calculation: trial=%+v err=%v", trial, err)
	}
}

func TestReliability_RuleEditResetsOnlyChangedTriggerDefinition(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	rule, err := e.SaveRule(ctx, auditPriceRule(OpGTE))
	if err != nil {
		t.Fatal(err)
	}
	if e.evaluateRule(ctx, rule, Observation{Price: 11}, "600000.SH", "fixture", now, "2026-10-26") != 1 {
		t.Fatal("initial hit missing")
	}
	rule.Name = "rename only"
	rule, err = e.SaveRule(ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.evaluateRule(ctx, rule, Observation{Price: 11}, "600000.SH", "fixture", now.Add(time.Minute), "2026-10-26"); got != 0 {
		t.Fatal("rename rearmed old threshold")
	}
	rule.Conditions[0].Value = 12
	reliabilityExec(t, e, `CREATE TRIGGER reliability_state_fail BEFORE DELETE ON monitor_state BEGIN SELECT RAISE(ABORT,'fixture reset failure'); END`)
	if _, err := e.SaveRule(ctx, rule); err == nil {
		t.Fatal("failed reset should fail save")
	}
	rules, _ := e.store.ListRules(ctx, "", false)
	if rules[0].Conditions[0].Value != 10 {
		t.Fatal("failed transaction changed threshold")
	}
	reliabilityExec(t, e, `DROP TRIGGER reliability_state_fail`)
	rule, err = e.SaveRule(ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.evaluateRule(ctx, rule, Observation{Price: 13}, "600000.SH", "fixture", now.Add(time.Second), "2026-10-26"); got != 1 {
		t.Fatalf("new threshold suppressed=%d", got)
	}
}

func TestReliability_MaxSymbolsMatchesPreviewAndActualScan(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	e.snapRows = []foundation.MarketSnapshotRow{{Symbol: "600000.SH", Price: 11}, {Symbol: "600001.SH", Price: 12}, {Symbol: "600002.SH", Price: 13}}
	e.snapFetchedAt = now
	rule := auditPriceRule(OpGTE)
	rule.Scope, rule.Symbols, rule.Filters.MaxSymbols = ScopeAll, nil, 1
	saved, err := e.SaveRule(ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	trial, err := e.TestRule(ctx, saved)
	if err != nil {
		t.Fatal(err)
	}
	e.runSnapshotTask(ctx, now, "2026-10-26")
	if trial.Total != 1 || len(reliabilityEvents(t, e)) != 1 {
		t.Fatalf("preview=%+v events=%+v", trial, reliabilityEvents(t, e))
	}
}

func TestReliability_QuoteCacheFetchesNewSymbolsAndExpiresPerSymbol(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	provider := &reliabilityQuotes{rows: []foundation.Quote{{Symbol: "600000.SH", Price: 10}, {Symbol: "600001.SH", Price: 20}}}
	e.realtime = provider
	if q := e.realtimeQuoteIndex(ctx, []string{"600000.SH"}); len(q) != 1 {
		t.Fatal(q)
	}
	now = now.Add(10 * time.Second)
	e.now = func() time.Time { return now }
	if q := e.realtimeQuoteIndex(ctx, []string{"600000.SH", "600001.SH"}); len(q) != 2 {
		t.Fatalf("new symbol missing: %v", q)
	}
	if len(provider.calls) != 2 || len(provider.calls[1]) != 1 || provider.calls[1][0] != "600001.SH" {
		t.Fatalf("unnecessary fetches: %v", provider.calls)
	}
	now = now.Add(6 * time.Second)
	e.realtimeQuoteIndex(ctx, []string{"600000.SH", "600001.SH"})
	if len(provider.calls) != 3 || len(provider.calls[2]) != 1 || provider.calls[2][0] != "600000.SH" {
		t.Fatalf("cache age refreshed by unrelated symbol: %v", provider.calls)
	}
	provider.err = errors.New("fixture offline")
	provider.rows = nil
	now = now.Add(20 * time.Second)
	if q := e.realtimeQuoteIndex(ctx, []string{"600000.SH"}); len(q) != 1 {
		t.Fatal("short fallback lost")
	}
	now = now.Add(time.Minute)
	if q := e.realtimeQuoteIndex(ctx, []string{"600000.SH"}); len(q) != 0 {
		t.Fatal("expired fallback retained")
	}
}
