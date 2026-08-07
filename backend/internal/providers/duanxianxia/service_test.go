package duanxianxia

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type fakeFetcher struct {
	count int
	now   *time.Time
}

type fakeBundleFetcher struct {
	themeCount int
	poolCount  int
	now        *time.Time
}

func (f *fakeBundleFetcher) Fetch(ctx context.Context, leaderThemeLimit int) (Snapshot, error) {
	f.themeCount++
	return Snapshot{TradeDate: "2026-08-07", FetchedAt: *f.now, Themes: []Theme{{Code: "801807", Name: "算力", Rank: 1}}}, nil
}

func (f *fakeBundleFetcher) FetchLimitUpPool(ctx context.Context) (LimitUpPoolSnapshot, error) {
	f.poolCount++
	return LimitUpPoolSnapshot{TradeDate: "2026-08-07", FetchedAt: *f.now, Events: []foundation.LimitUpEvent{{Symbol: "603629.SH", Name: "利通电子", Date: *f.now}}}, nil
}

func (f *fakeFetcher) Fetch(ctx context.Context, leaderThemeLimit int) (Snapshot, error) {
	f.count++
	return Snapshot{
		TradeDate: "2026-08-07",
		FetchedAt: *f.now,
		Themes: []Theme{{
			Code: "801807", Name: "算力", Rank: 1,
		}},
	}, nil
}

func TestServicePersistsFiveMinuteGateAcrossRestart(t *testing.T) {
	now := time.Date(2026, 8, 7, 9, 35, 0, 0, time.FixedZone("CST", 8*60*60))
	path := filepath.Join(t.TempDir(), "theme-radar.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	fetcher := &fakeFetcher{now: &now}
	service := NewService(fetcher, store, ServiceConfig{Now: func() time.Time { return now }})
	first, meta, err := service.Snapshot(context.Background())
	if err != nil || first.TradeDate != "2026-08-07" || !meta.Refreshed || fetcher.count != 1 {
		t.Fatalf("unexpected first refresh: snapshot=%+v meta=%+v count=%d err=%v", first, meta, fetcher.count, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	now = now.Add(4 * time.Minute)
	store, err = OpenStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()
	secondFetcher := &fakeFetcher{now: &now}
	service = NewService(secondFetcher, store, ServiceConfig{Now: func() time.Time { return now }})
	second, meta, err := service.Snapshot(context.Background())
	if err != nil || second.ID != first.ID || !meta.FromCache || secondFetcher.count != 0 {
		t.Fatalf("five-minute gate was not preserved: snapshot=%+v meta=%+v count=%d err=%v", second, meta, secondFetcher.count, err)
	}

	now = now.Add(2 * time.Minute)
	third, meta, err := service.Snapshot(context.Background())
	if err != nil || third.ID == first.ID || !meta.Refreshed || secondFetcher.count != 1 {
		t.Fatalf("refresh after gate failed: snapshot=%+v meta=%+v count=%d err=%v", third, meta, secondFetcher.count, err)
	}
}

func TestServiceSharesFiveMinuteGateBetweenThemesAndLimitUpPool(t *testing.T) {
	now := time.Date(2026, 8, 7, 9, 35, 0, 0, time.FixedZone("CST", 8*60*60))
	store, err := OpenStore(filepath.Join(t.TempDir(), "theme-radar.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	fetcher := &fakeBundleFetcher{now: &now}
	service := NewService(fetcher, store, ServiceConfig{Now: func() time.Time { return now }})
	if _, meta, err := service.Snapshot(context.Background()); err != nil || !meta.Refreshed {
		t.Fatalf("theme refresh failed: meta=%+v err=%v", meta, err)
	}
	pool, meta, err := service.LimitUpPool(context.Background())
	if err != nil || pool.TradeDate != "2026-08-07" || !meta.FromCache {
		t.Fatalf("pool cache read failed: pool=%+v meta=%+v err=%v", pool, meta, err)
	}
	if fetcher.themeCount != 1 || fetcher.poolCount != 1 {
		t.Fatalf("shared batch counts theme=%d pool=%d", fetcher.themeCount, fetcher.poolCount)
	}
	now = now.Add(4 * time.Minute)
	if _, _, err := service.LimitUpPool(context.Background()); err != nil {
		t.Fatalf("cached pool: %v", err)
	}
	if fetcher.themeCount != 1 || fetcher.poolCount != 1 {
		t.Fatalf("five-minute gate was bypassed theme=%d pool=%d", fetcher.themeCount, fetcher.poolCount)
	}
}
