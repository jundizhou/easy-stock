package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestLiveBusConcurrentPublishAndUnsubscribe(t *testing.T) {
	bus := newLiveBus()
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := 0; i < 2000; i++ {
				bus.publish(nil)
			}
		}()
	}
	for i := 0; i < 2000; i++ {
		_, cancel := bus.subscribe()
		cancel()
		cancel()
	}
	workers.Wait()
	if bus.hasSubscribers() {
		t.Fatal("unsubscribed clients remain registered")
	}
}

func TestLiveBusSlowSubscriberDoesNotBlockFanout(t *testing.T) {
	bus := newLiveBus()
	first, cancelFirst := bus.subscribe()
	defer cancelFirst()
	second, cancelSecond := bus.subscribe()
	defer cancelSecond()
	bus.publish([]LiveTick{{Symbol: "600000.SH", Price: 10, FetchedAt: 100}})
	bus.publish([]LiveTick{{Symbol: "600000.SH", Price: 11, FetchedAt: 200}})
	for _, ch := range []<-chan struct{}{first, second} {
		select {
		case <-ch:
		default:
			t.Fatal("subscriber did not receive a notification")
		}
	}
	if ticks := bus.snapshot(map[string]bool{"600000.SH": true}); len(ticks) != 1 || ticks[0].Price != 11 {
		t.Fatalf("expected the latest snapshot, got %+v", ticks)
	}
}

type liveTestSnapshot struct {
	calls int
	fail  bool
}

func (source *liveTestSnapshot) MarketSnapshot(context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	source.calls++
	if source.fail {
		return nil, foundation.SourceMeta{}, errors.New("source unavailable")
	}
	return []foundation.MarketSnapshotRow{{Symbol: "600000.SH", Price: 12.34, ChangePercent: 1.25}}, foundation.SourceMeta{}, nil
}

func TestLiveQuotesShareSnapshotAndPreserveTimestampOnFailure(t *testing.T) {
	source := &liveTestSnapshot{}
	engine := NewEngine(EngineConfig{Snapshot: source, Settings: func() Settings {
		settings := DefaultSettings()
		settings.Enabled = true
		return settings
	}})
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, foundation.AStockLocation)
	engine.now = func() time.Time { return now }
	_, firstOff := engine.SubscribeLiveTicks()
	defer firstOff()
	second, secondOff := engine.SubscribeLiveTicks()
	defer secondOff()
	engine.refreshLiveQuotes(context.Background())
	engine.refreshLiveQuotes(context.Background())
	if source.calls != 1 {
		t.Fatalf("clients must share the same refresh: calls=%d", source.calls)
	}
	select {
	case <-second:
	default:
		t.Fatal("fresh snapshot was not broadcast")
	}
	for _, symbol := range []string{"600000.SH", "600000"} {
		ticks := engine.LiveTicks(map[string]bool{symbol: true})
		if len(ticks) != 1 || ticks[0].Price != 12.34 || ticks[0].FetchedAt != now.UnixMilli() {
			t.Fatalf("symbol aliases must carry the real snapshot: %+v", ticks)
		}
	}
	oldTimestamp := now.UnixMilli()
	now = now.Add(liveSnapshotInterval)
	source.fail = true
	engine.refreshLiveQuotes(context.Background())
	if source.calls != 2 || engine.LiveTicks(nil)[0].FetchedAt != oldTimestamp {
		t.Fatal("a failed refresh must not invent a fresh timestamp")
	}
	select {
	case <-second:
		t.Fatal("failed source must not emit a fresh quote frame")
	default:
	}
}

func TestLiveQuotesGateNonTradingHoursAndInactiveClients(t *testing.T) {
	for _, test := range []struct {
		name       string
		day        int
		hour       int
		minute     int
		enabled    bool
		subscribed bool
	}{
		{"no clients", 26, 10, 0, true, false},
		{"disabled", 26, 10, 0, false, true},
		{"lunch break", 26, 12, 0, true, true},
		{"after close", 26, 16, 0, true, true},
		{"weekend", 24, 10, 0, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := &liveTestSnapshot{}
			engine := NewEngine(EngineConfig{Snapshot: source, Settings: func() Settings {
				settings := DefaultSettings()
				settings.Enabled = test.enabled
				return settings
			}})
			engine.now = func() time.Time {
				return time.Date(2026, 10, test.day, test.hour, test.minute, 0, 0, foundation.AStockLocation)
			}
			if test.subscribed {
				_, cancel := engine.SubscribeLiveTicks()
				defer cancel()
			}
			engine.refreshLiveQuotes(context.Background())
			if source.calls != 0 {
				t.Fatal("inactive market/client must not fetch upstream")
			}
		})
	}
}

type canceledLiveSnapshot struct{ started chan struct{} }

func (source canceledLiveSnapshot) MarketSnapshot(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	close(source.started)
	<-ctx.Done()
	return nil, foundation.SourceMeta{}, ctx.Err()
}

func TestLiveQuoteRefreshCancelsInFlightFetch(t *testing.T) {
	source := canceledLiveSnapshot{started: make(chan struct{})}
	engine := NewEngine(EngineConfig{Snapshot: source, Settings: func() Settings {
		settings := DefaultSettings()
		settings.Enabled = true
		return settings
	}})
	engine.now = func() time.Time { return time.Date(2026, 10, 26, 10, 0, 0, 0, foundation.AStockLocation) }
	_, off := engine.SubscribeLiveTicks()
	defer off()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); engine.refreshLiveQuotes(ctx) }()
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("in-flight refresh did not stop with the engine context")
	}
	if len(engine.LiveTicks(nil)) != 0 {
		t.Fatal("canceled refresh must not invent a quote")
	}
}

func TestLiveQuoteLoopStopsWithContext(t *testing.T) {
	engine := NewEngine(EngineConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); engine.runLiveQuotes(ctx) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("live loop did not stop on cancellation")
	}
}
