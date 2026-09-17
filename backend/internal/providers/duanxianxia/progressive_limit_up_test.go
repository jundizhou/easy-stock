package duanxianxia

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type slowThemeFetcher struct {
	themes     chan struct{}
	poolCalls  atomic.Int32
	themeCalls atomic.Int32
	now        time.Time
}

func (f *slowThemeFetcher) Fetch(ctx context.Context, _ int) (Snapshot, error) {
	f.themeCalls.Add(1)
	select {
	case <-f.themes:
		return Snapshot{ID: "theme", TradeDate: "2026-09-17", FetchedAt: f.now}, nil
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}
func (f *slowThemeFetcher) FetchLimitUpPool(ctx context.Context) (LimitUpPoolSnapshot, error) {
	f.poolCalls.Add(1)
	return LimitUpPoolSnapshot{ID: "pool", TradeDate: "2026-09-17", FetchedAt: f.now, Events: []foundation.LimitUpEvent{{Symbol: "600001.SH", Date: f.now}}}, nil
}

func TestEarlyPoolDoesNotWaitForThemeLeadersAndDoesNotRefreshTwice(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	f := &slowThemeFetcher{themes: make(chan struct{}), now: time.Now()}
	s := NewService(f, store, ServiceConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	finished := make(chan struct{})
	go func() { defer close(finished); s.Snapshots(ctx, 1) }()
	// The theme task is deliberately blocked throughout both reads; the second
	// subscriber joins after the pool publication and must still return promptly.
	for i := 0; i < 2; i++ {
		pools, err := s.EarlyLimitUpPools(ctx, 2)
		if err != nil || len(pools) != 1 || pools[0].Events[0].Symbol != "600001.SH" {
			t.Fatalf("pool waited for leaders: pools=%+v err=%v", pools, err)
		}
	}
	close(f.themes)
	<-finished
	// Wait for any joining call to finish before the store is closed.
	if _, _, err := s.LimitUpPools(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if f.poolCalls.Load() != 1 || f.themeCalls.Load() != 1 {
		t.Fatalf("duplicate upstream batch: pool=%d theme=%d", f.poolCalls.Load(), f.themeCalls.Load())
	}
}

func TestRefreshGateWaitCanBeCancelled(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := NewService(&slowThemeFetcher{}, store, ServiceConfig{})
	s.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = s.LimitUpPools(ctx, 2)
	<-s.gate
	if err != context.Canceled {
		t.Fatalf("gate ignored cancellation: %v", err)
	}
}
