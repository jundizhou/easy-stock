package cockpit

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/foundation"
)

func at(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, foundation.AStockLocation)
}

func TestPhaseAtTradingDay(t *testing.T) {
	cases := []struct {
		name  string
		now   time.Time
		phase Phase
	}{
		{"before open", at(20, 8, 0), PhasePreOpen},
		{"just before open", at(20, 9, 24), PhasePreOpen},
		{"opening", at(20, 9, 25), PhaseIntraday},
		{"midday", at(20, 12, 0), PhaseIntraday},
		{"closing", at(20, 15, 4), PhaseIntraday},
		{"after close", at(20, 15, 5), PhasePostClose},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := PhaseAt(tc.now)
			if state.Phase != tc.phase || state.SessionDate != "2026-10-20" || !state.TradingDay {
				t.Fatalf("PhaseAt(%v) = %+v, want phase %s on 2026-10-20", tc.now, state, tc.phase)
			}
		})
	}
}

func TestPhaseAtRestDayPointsToNextSession(t *testing.T) {
	// 2026-10-24 is a Saturday; the next exchange session is Monday 2026-10-26.
	state := PhaseAt(at(24, 10, 0))
	if state.Phase != PhaseRest || state.TradingDay || state.SessionDate != "2026-10-26" {
		t.Fatalf("saturday state = %+v", state)
	}
}

func TestPhaseAtSkipsExchangeHoliday(t *testing.T) {
	// 2026-10-01 falls inside the National Day closure (10-01 to 10-07).
	state := PhaseAt(at(1, 10, 0))
	if state.TradingDay || state.Phase != PhaseRest {
		t.Fatalf("national day should be rest, got %+v", state)
	}
}

func TestDueSlotRunsLatestDueIntradaySlotOnly(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		key  string
	}{
		{"pre-open run", at(20, 8, 45), "pre"},
		{"first intraday slot", at(20, 9, 40), "09:40"},
		{"catch up to latest slot", at(20, 10, 50), "10:40"},
		{"lunch break keeps last slot", at(20, 12, 0), "11:10"},
		{"post close", at(20, 15, 40), "post"},
		{"weekend evening", at(24, 20, 30), "weekend"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slot, ok := DueSlot(tc.now, 30)
			if !ok || slot.Key != tc.key {
				t.Fatalf("DueSlot(%v) = %+v, %v; want key %s", tc.now, slot, ok, tc.key)
			}
		})
	}
	for _, quiet := range []time.Time{at(20, 8, 30), at(20, 9, 30), at(24, 10, 0)} {
		if slot, ok := DueSlot(quiet, 30); ok {
			t.Fatalf("DueSlot(%v) unexpectedly returned %+v", quiet, slot)
		}
	}
}

func TestNextRunSkipsToNextSession(t *testing.T) {
	next, name := NextRun(at(20, 9, 0), 30)
	if name != "盘中巡查 09:40" || !next.Equal(at(20, 9, 40)) {
		t.Fatalf("intraday next = %v %q", next, name)
	}
	next, name = NextRun(at(20, 15, 40), 30)
	if name != "盘前巡查" || !next.Equal(at(21, 8, 40)) {
		t.Fatalf("after close next = %v %q", next, name)
	}
	next, name = NextRun(at(23, 16, 0), 30)
	if name != "周末外围巡查" || !next.Equal(at(24, 20, 0)) {
		t.Fatalf("friday evening next = %v %q", next, name)
	}
}

func TestBuildFlowBoardKeepsDirectionalExtremes(t *testing.T) {
	board := BuildFlowBoard([]FlowItem{
		{Name: "A", NetInflow: 9}, {Name: "B", NetInflow: -7}, {Name: "C", NetInflow: 3}, {Name: "D", NetInflow: -2}, {Name: "E", NetInflow: 0},
	}, 2)
	if len(board.Inflow) != 2 || board.Inflow[0].Name != "A" || board.Inflow[1].Name != "C" {
		t.Fatalf("inflow = %+v", board.Inflow)
	}
	if len(board.Outflow) != 2 || board.Outflow[0].Name != "B" || board.Outflow[1].Name != "D" {
		t.Fatalf("outflow = %+v", board.Outflow)
	}
	if BuildFlowBoard(nil, 3) != nil {
		t.Fatal("empty input should produce no board")
	}
}

func TestCollectFactsIsolatesFailingSource(t *testing.T) {
	feed := Feed{
		Overseas: func(context.Context) ([]IndexQuote, error) { return nil, errors.New("upstream down") },
		CNIndexes: func(context.Context) ([]IndexQuote, error) {
			return []IndexQuote{{Name: "上证指数", ChangePercent: -0.4}}, nil
		},
		News: func(context.Context) ([]NewsLine, error) { panic("boom") },
	}
	facts := CollectFacts(context.Background(), PhasePreOpen, feed)
	if len(facts.CNIndexes) != 1 {
		t.Fatalf("healthy source lost: %+v", facts)
	}
	failed := map[string]string{}
	for _, source := range facts.Sources {
		if !source.OK {
			failed[source.Name] = source.Error
		}
	}
	if !strings.Contains(failed["海外指数"], "upstream down") || !strings.Contains(failed["快讯"], "boom") {
		t.Fatalf("failures not recorded: %+v", failed)
	}
}

func TestCollectFactsSkipsIntradayFeedBeforeSession(t *testing.T) {
	called := false
	facts := CollectFacts(context.Background(), PhasePreOpen, Feed{Intraday: func(context.Context) (*IntradayView, error) {
		called = true
		return &IntradayView{}, nil
	}})
	if called || facts.Intraday != nil {
		t.Fatal("intraday feed must not run before the session")
	}
}

func TestCollectFactsKeepsIntradayFeedAfterClose(t *testing.T) {
	facts := CollectFacts(context.Background(), PhasePostClose, Feed{Intraday: func(context.Context) (*IntradayView, error) {
		return &IntradayView{Status: "收盘快照"}, nil
	}})
	if facts.Intraday == nil || facts.Intraday.Status != "收盘快照" {
		t.Fatalf("post-close intraday facts missing: %+v", facts.Intraday)
	}
}

type fakeNarrator struct {
	available bool
	release   chan struct{}
	mu        sync.Mutex
	prompts   []string
	text      string
	err       error
}

func (f *fakeNarrator) Available() bool { return f.available }

func (f *fakeNarrator) Narrate(ctx context.Context, prompt string) (string, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, prompt)
	f.mu.Unlock()
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return f.text, f.err
}

func (f *fakeNarrator) lastPrompt() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.prompts) == 0 {
		return ""
	}
	return f.prompts[len(f.prompts)-1]
}

func newTestService(t *testing.T, narrator Narrator, now time.Time) (*Service, *Store) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "cockpit-briefs.json"))
	if err != nil {
		t.Fatal(err)
	}
	feed := Feed{
		CNIndexes: func(context.Context) ([]IndexQuote, error) {
			return []IndexQuote{{Name: "上证指数", ChangePercent: -1.3}}, nil
		},
		Intraday: func(context.Context) (*IntradayView, error) {
			return &IntradayView{Status: "退潮风险", Summary: "高位股跌幅扩大"}, nil
		},
	}
	settings, err := appsettings.Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, feed, narrator, nil, settings)
	service.now = func() time.Time { return now }
	return service, store
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestTickRunsOncePerSlotWithFactsOnlyWhenAIUnavailable(t *testing.T) {
	narrator := &fakeNarrator{available: false}
	service, store := newTestService(t, narrator, at(20, 10, 15))
	service.Tick()
	waitFor(t, func() bool { return len(store.List("", 0)) == 1 && store.List("", 0)[0].Status == "ready" })
	service.Tick()
	briefs := store.List("", 0)
	if len(briefs) != 1 {
		t.Fatalf("slot should not repeat, got %d briefs", len(briefs))
	}
	brief := briefs[0]
	if brief.Key != "2026-10-20|10:10" || brief.NarrativeStatus != "unavailable" || brief.Narrative != "" {
		t.Fatalf("unexpected brief %+v", brief)
	}
	if len(brief.Facts.CNIndexes) != 1 || brief.Facts.Intraday == nil {
		t.Fatalf("facts missing: %+v", brief.Facts)
	}
	if narrator.lastPrompt() != "" {
		t.Fatal("model must not be called when AI is unavailable")
	}
}

func TestManualRunPassesPreviousIntradayForChangeDetection(t *testing.T) {
	narrator := &fakeNarrator{available: true, text: "## 正在发生什么\n指数走弱（盘中情绪）"}
	service, store := newTestService(t, narrator, at(20, 10, 15))
	service.Tick()
	waitFor(t, func() bool { return store.List("", 0)[0].Status == "ready" })

	service.now = func() time.Time { return at(20, 11, 0) }
	if _, err := service.RunManual(PhaseIntraday); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(store.List("", 0)) == 2 && store.List("", 0)[0].Status == "ready" })
	prompt := narrator.lastPrompt()
	for _, want := range []string{"盘中巡查", "上一次巡查（用于对比变化）", "上一次巡查", "不构成投资建议", "指数走弱"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
	if got := store.List("", 0)[0]; got.NarrativeStatus != "ready" || got.Narrative == "" {
		t.Fatalf("narrative not stored: %+v", got)
	}
}

func TestOnlyOneBriefRunsAtATime(t *testing.T) {
	narrator := &fakeNarrator{available: true, release: make(chan struct{}), text: "ok"}
	service, store := newTestService(t, narrator, at(20, 15, 40))
	if _, err := service.RunManual(PhasePostClose); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunManual(PhasePostClose); !errors.Is(err, ErrBusy) {
		t.Fatalf("second run error = %v, want ErrBusy", err)
	}
	if !service.Status().Running {
		t.Fatal("status should report a running patrol")
	}
	close(narrator.release)
	waitFor(t, func() bool { return !service.Status().Running && store.List("", 0)[0].Status == "ready" })
}

func TestNarratorFailureKeepsFacts(t *testing.T) {
	narrator := &fakeNarrator{available: true, err: errors.New("model timeout")}
	service, store := newTestService(t, narrator, at(20, 15, 40))
	if _, err := service.RunManual(PhasePostClose); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return store.List("", 0)[0].Status == "ready" })
	brief := store.List("", 0)[0]
	if brief.NarrativeStatus != "failed" || !strings.Contains(brief.Error, "model timeout") || len(brief.Facts.CNIndexes) == 0 {
		t.Fatalf("failure not isolated: %+v", brief)
	}
}

func TestStoreMarksInterruptedRunsAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cockpit-briefs.json")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(Brief{ID: "a", Key: "k1", Phase: PhasePreOpen, Status: "running"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	brief, ok := reopened.Get("a")
	if !ok || brief.Status != "failed" || brief.Error != "上次巡查已中断" {
		t.Fatalf("interrupted run not recovered: %+v", brief)
	}
	if !reopened.HasKey("k1") {
		t.Fatal("dedupe key lost across restart")
	}
}

func TestBuildMoveBoardRanksByChangePercent(t *testing.T) {
	board := BuildMoveBoard([]FlowItem{
		{Name: "A", NetInflow: -9, ChangePercent: 3.1},
		{Name: "B", NetInflow: 9, ChangePercent: -2.4},
		{Name: "C", NetInflow: 1, ChangePercent: 1.2},
		{Name: "D", NetInflow: -1, ChangePercent: -0.6},
		{Name: "E", NetInflow: 5, ChangePercent: 0},
	}, 2)
	if len(board.Inflow) != 2 || board.Inflow[0].Name != "A" || board.Inflow[1].Name != "C" {
		t.Fatalf("inflow = %+v", board.Inflow)
	}
	// 净流入为正但当日下跌的板块也必须进领跌侧，这是资金榜口径取不到的部分；
	// 流出侧与资金榜一致，跌得最多的排在最前。
	if len(board.Outflow) != 2 || board.Outflow[0].Name != "B" || board.Outflow[1].Name != "D" {
		t.Fatalf("outflow = %+v", board.Outflow)
	}
	if BuildMoveBoard(nil, 3) != nil {
		t.Fatal("empty input should produce no board")
	}
}

func TestCarryOverBoardsKeepsLastKnownData(t *testing.T) {
	prev := Facts{Industry: &FlowBoard{Inflow: []FlowItem{{Name: "银行"}}}, Concepts: &FlowBoard{Inflow: []FlowItem{{Name: "AI芯片"}}}, ConceptMoves: &FlowBoard{Inflow: []FlowItem{{Name: "WiFi"}}}, News: []NewsLine{{Title: "旧快讯"}}}
	next := CarryOverBoards(prev, Facts{Industry: &FlowBoard{Inflow: []FlowItem{{Name: "新行业"}}}})
	if next.Industry.Inflow[0].Name != "新行业" {
		t.Fatal("fresh data must win")
	}
	if next.Concepts == nil || next.Concepts.Inflow[0].Name != "AI芯片" || next.ConceptMoves == nil || next.News == nil {
		t.Fatalf("empty fields must carry over: %+v", next)
	}
}

func TestSeedCarryFromBriefsPicksLatestAvailable(t *testing.T) {
	old := Brief{ID: "old", Status: "ready", Facts: Facts{Concepts: &FlowBoard{Inflow: []FlowItem{{Name: "旧概念"}}}, News: []NewsLine{{Title: "旧快讯"}}}}
	recent := Brief{ID: "new", Status: "ready", Facts: Facts{ConceptMoves: &FlowBoard{Inflow: []FlowItem{{Name: "新题材"}}}}}
	carry := SeedCarryFromBriefs([]Brief{recent, old})
	if carry.ConceptMoves == nil || carry.Concepts == nil || carry.News == nil {
		t.Fatalf("seed should merge fields across briefs: %+v", carry)
	}
}

func TestBuildFlowBoardReturnsNilForAllZeroSamples(t *testing.T) {
	if BuildFlowBoard([]FlowItem{{Name: "A", NetInflow: 0}, {Name: "B", NetInflow: 0}}, 8) != nil {
		t.Fatal("all-zero sample should be treated as no data")
	}
	if BuildMoveBoard([]FlowItem{{Name: "A", ChangePercent: 0}}, 5) != nil {
		t.Fatal("all-flat sample should be treated as no data")
	}
	prev := Facts{Concepts: &FlowBoard{Inflow: []FlowItem{{Name: "昨日概念"}}}}
	next := CarryOverBoards(prev, Facts{Concepts: &FlowBoard{}})
	if next.Concepts == nil || next.Concepts.Inflow[0].Name != "昨日概念" {
		t.Fatalf("empty board must carry over: %+v", next.Concepts)
	}
}
