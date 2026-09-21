package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
)

type stagedLadderProvider struct {
	history, themes chan struct{}
	calls           atomic.Int32
}

func ladderEvents() []foundation.LimitUpEvent {
	date := time.Date(2026, 9, 17, 0, 0, 0, 0, shanghaiLocation)
	return []foundation.LimitUpEvent{
		{Symbol: "600001.SH", Name: "基础梯队", Date: date, Streak: 3, Concepts: []string{"算力"}, Meta: foundation.SourceMeta{Source: "test", FetchedAt: date}},
		{Symbol: "600001.SH", Name: "基础梯队", Date: date.AddDate(0, 0, -1), Streak: 2},
	}
}

func (p *stagedLadderProvider) RecentLimitUps(ctx context.Context, _ int) ([]foundation.LimitUpEvent, error) {
	p.calls.Add(1)
	select {
	case <-p.history:
		return ladderEvents(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (p *stagedLadderProvider) ProgressiveLimitUps(ctx context.Context, _ int, publish func([]foundation.LimitUpEvent, string, error)) {
	p.calls.Add(1)
	publish(ladderEvents(), "primary", nil)
	select {
	case <-p.history:
	case <-ctx.Done():
		return
	}
	publish(ladderEvents(), "history", nil)
	select {
	case <-p.themes:
	case <-ctx.Done():
		return
	}
	publish(ladderEvents(), "themes", nil)
}

type gatedLadderConcepts struct{ gate chan struct{} }

func (p gatedLadderConcepts) StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error) {
	select {
	case <-p.gate:
		return nil, errors.New("目录暂不可用")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type datedLadderQuotes struct {
	gate chan struct{}
	date time.Time
}

func (p datedLadderQuotes) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	select {
	case <-p.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []foundation.Quote{{Symbol: symbols[0], ChangePercent: 9.9, TradeTime: p.date}}, nil
}

func shortTermResponse[T any](t *testing.T, server *Server, path string) shortTermProgress[T] {
	t.Helper()
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var value shortTermProgress[T]
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func awaitLadder(t *testing.T, s *Server, id string, ready func(shortTermProgress[limitUpLadderData]) bool) shortTermProgress[limitUpLadderData] {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		value := shortTermResponse[limitUpLadderData](t, s, "/api/v1/short-term/limit-up-ladder?delivery=progressive&refresh_id="+id)
		if ready(value) {
			return value
		}
		if time.Now().After(deadline) {
			t.Fatalf("state never arrived: %+v", value)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLadderPublishesBeforeConceptsQuotesAndThemes(t *testing.T) {
	p := &stagedLadderProvider{history: make(chan struct{}), themes: make(chan struct{})}
	concepts, quotes := make(chan struct{}), make(chan struct{})
	s := NewServer(Config{LimitUp: p, StockConcept: gatedLadderConcepts{concepts}, Realtime: datedLadderQuotes{quotes, ladderEvents()[0].Date}})
	defer s.Close()
	first := shortTermResponse[limitUpLadderData](t, s, "/api/v1/short-term/limit-up-ladder?delivery=progressive")
	partial := awaitLadder(t, s, first.RefreshID, func(v shortTermProgress[limitUpLadderData]) bool { return v.Data != nil })
	if !partial.Refreshing || partial.Data.Current.LimitUpCount != 1 || partial.Data.ComparisonReady || partial.Data.Intraday != nil {
		t.Fatalf("bad first batch: %+v", partial)
	}
	close(p.history)
	awaitLadder(t, s, first.RefreshID, func(v shortTermProgress[limitUpLadderData]) bool { return v.Data.ComparisonReady })
	// Hold an actual published object while the quote worker modifies its own copy.
	s.limitUpProgress.mu.Lock()
	published := s.limitUpProgress.value.Data
	s.limitUpProgress.mu.Unlock()
	close(quotes)
	withQuotes := awaitLadder(t, s, first.RefreshID, func(v shortTermProgress[limitUpLadderData]) bool { return v.Data.Intraday != nil })
	if !withQuotes.Refreshing || withQuotes.Steps["themes"] != "loading" || withQuotes.Steps["concepts"] != "loading" {
		t.Fatalf("unrelated stages blocked feedback: %+v", withQuotes)
	}
	if published.Previous.Levels[0].Stocks[0].CurrentChangePercent != nil {
		t.Fatal("worker mutated an already published snapshot")
	}
	if withQuotes.Data.Intraday.BaseTradeDate != "2026-09-16" {
		t.Fatal("wrong comparison date")
	}
	close(p.themes)
	close(concepts)
	final := awaitLadder(t, s, first.RefreshID, func(v shortTermProgress[limitUpLadderData]) bool { return !v.Refreshing })
	if final.Data.Intraday == nil || final.Errors["concepts"] == "" || final.Data.Current.LimitUpCount != 1 {
		t.Fatalf("failed enrichment discarded usable data: %+v", final)
	}
	for i := 0; i < 3; i++ {
		shortTermResponse[limitUpLadderData](t, s, "/api/v1/short-term/limit-up-ladder?delivery=progressive&refresh_id="+first.RefreshID)
	}
	if p.calls.Load() != 1 {
		t.Fatal("polling restarted remote work")
	}
}

func TestLadderCancellationPersistsPartialSnapshotAndRestoresIt(t *testing.T) {
	db := filepath.Join(t.TempDir(), "radar.db")
	p := &stagedLadderProvider{history: make(chan struct{}), themes: make(chan struct{})}
	s := NewServer(Config{LimitUp: p, ThemeRadarDBPath: db})
	first := shortTermResponse[limitUpLadderData](t, s, "/api/v1/short-term/limit-up-ladder?delivery=progressive")
	awaitLadder(t, s, first.RefreshID, func(v shortTermProgress[limitUpLadderData]) bool { return v.Data != nil })
	s.limitUpProgress.mu.Lock()
	s.limitUpProgress.cancel()
	s.limitUpProgress.mu.Unlock()
	final := awaitLadder(t, s, first.RefreshID, func(v shortTermProgress[limitUpLadderData]) bool { return !v.Refreshing })
	if final.Data == nil || final.Steps["history"] != "error" || final.Errors["quotes"] == "" {
		t.Fatalf("not terminal: %+v", final)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restored := NewServer(Config{LimitUp: p, ThemeRadarDBPath: db})
	defer restored.Close()
	value := shortTermResponse[limitUpLadderData](t, restored, "/api/v1/short-term/limit-up-ladder?delivery=progressive")
	if value.Data == nil || !value.Stale || value.Data.Current.TradeDate != "2026-09-17" {
		t.Fatalf("snapshot not immediately restored: %+v", value)
	}
}

func TestEmotionLocalHistoryDoesNotWaitForSchedulerOrLadder(t *testing.T) {
	store, err := marketemotion.OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(context.Background(), marketemotion.Snapshot{TradeDate: "2026-09-15", ModelVersion: currentMarketEmotionModelVersion}); err != nil {
		t.Fatal(err)
	}
	p := &stagedLadderProvider{history: make(chan struct{}), themes: make(chan struct{})}
	s := NewServer(Config{MarketEmotionStore: store, LimitUp: p, MarketPools: &countingMarketPoolProvider{}, KLinePrimary: &countingEmotionKLineProvider{}})
	defer s.Close()
	s.marketEmotion.now = func() time.Time { return time.Date(2026, 9, 17, 10, 0, 0, 0, shanghaiLocation) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.marketEmotion.load(ctx) }()
	deadline := time.Now().Add(time.Second)
	for p.calls.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("sync did not start")
		}
		time.Sleep(time.Millisecond)
	}
	started := time.Now()
	value := shortTermResponse[marketemotion.History](t, s, "/api/v1/short-term/emotion-history?delivery=progressive")
	if time.Since(started) > 200*time.Millisecond || value.Data == nil || len(value.Data.Points) != 1 || !value.Refreshing {
		t.Fatalf("local history blocked: %+v", value)
	}
	if value.Data.Intraday != nil {
		t.Fatal("history should not compute intraday data")
	}
	cancel()
	<-done
}

func TestLadderRejectsQuotesFromDifferentTradingDay(t *testing.T) {
	day := limitUpLadderDay{Levels: []limitUpLadderLevel{{Stocks: []limitUpLadderStock{{Symbol: "600001.SH"}}}}}
	gate := make(chan struct{})
	close(gate)
	err := enrichPreviousChangesForDate(context.Background(), &day, "2026-09-16", datedLadderQuotes{gate, ladderEvents()[0].Date})
	if err == nil || day.Levels[0].Stocks[0].CurrentChangePercent != nil {
		t.Fatal("mixed different trading days")
	}
}

func TestLadderWeekendUsesFridayPoolAndQuotes(t *testing.T) {
	friday := time.Date(2026, 9, 18, 0, 0, 0, 0, shanghaiLocation)
	sunday := friday.AddDate(0, 0, 2)
	events := []foundation.LimitUpEvent{
		{Symbol: "600001.SH", Date: friday.AddDate(0, 0, -1), Streak: 2},
		{Symbol: "600001.SH", Date: friday, Streak: 3},
		// A provider may repeat Friday's pool under the requested Sunday date.
		{Symbol: "600001.SH", Date: sunday, Streak: 3},
	}
	ladder, err := buildLimitUpLadder(events, nil, sunday)
	if err != nil || ladder.Current.TradeDate != "2026-09-18" || ladder.Previous.TradeDate != "2026-09-17" || ladder.SessionStatus != "最近交易日" {
		t.Fatalf("wrong weekend dates: %+v %v", ladder, err)
	}
	gate := make(chan struct{})
	close(gate)
	if err := enrichPreviousChangesForDate(context.Background(), &ladder.Previous, ladder.Current.TradeDate, datedLadderQuotes{gate, friday}); err != nil {
		t.Fatalf("Friday quotes should match the weekend snapshot: %v", err)
	}
}
