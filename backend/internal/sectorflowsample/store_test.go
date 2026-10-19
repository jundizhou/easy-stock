package sectorflowsample

import (
	"context"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestStoreUpsertsSameMinuteAndOrdersSamples(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if _, err := store.Save(ctx, []Sample{
		{TradeDate: "2026-10-19", Minute: "09:31", Code: "BK0917", Name: "半导体概念", Dimension: "concept", Main: 100, ChangePercent: 0.5},
		{TradeDate: "2026-10-19", Minute: "09:33", Code: "BK0917", Name: "半导体概念", Dimension: "concept", Main: 220, ChangePercent: 0.8},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// 同一分钟内重复采样（例如手动触发）只保留最新值。
	if _, err := store.Save(ctx, []Sample{
		{TradeDate: "2026-10-19", Minute: "09:33", Code: "BK0917", Name: "半导体概念", Dimension: "concept", Main: 260, ChangePercent: 0.9},
	}); err != nil {
		t.Fatalf("save duplicate: %v", err)
	}

	samples, err := store.DaySamples(ctx, "2026-10-19", []string{"BK0917"})
	if err != nil {
		t.Fatalf("day samples: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2: %+v", len(samples), samples)
	}
	if samples[0].Minute != "09:31" || samples[1].Minute != "09:33" {
		t.Fatalf("unexpected order: %+v", samples)
	}
	if samples[1].Main != 260 {
		t.Fatalf("same-minute upsert kept %v, want 260", samples[1].Main)
	}
}

func TestStoreFiltersByCodeAndTracksLatestDate(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if _, err := store.Save(ctx, []Sample{
		{TradeDate: "2026-10-16", Minute: "15:00", Code: "BK0917", Main: 1},
		{TradeDate: "2026-10-19", Minute: "09:31", Code: "BK0917", Main: 2},
		{TradeDate: "2026-10-19", Minute: "09:31", Code: "BK0490", Main: 3},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	samples, err := store.DaySamples(ctx, "2026-10-19", []string{"BK0490"})
	if err != nil {
		t.Fatalf("day samples: %v", err)
	}
	if len(samples) != 1 || samples[0].Code != "BK0490" {
		t.Fatalf("unexpected samples: %+v", samples)
	}

	latest, err := store.LatestTradeDate(ctx)
	if err != nil {
		t.Fatalf("latest date: %v", err)
	}
	if latest != "2026-10-19" {
		t.Fatalf("latest = %q, want 2026-10-19", latest)
	}

	if err := store.PurgeBefore(ctx, "2026-10-19"); err != nil {
		t.Fatalf("purge: %v", err)
	}
	latest, err = store.LatestTradeDate(ctx)
	if err != nil {
		t.Fatalf("latest date after purge: %v", err)
	}
	if latest != "2026-10-19" {
		t.Fatalf("purge removed wrong rows, latest = %q", latest)
	}
	older, err := store.DaySamples(ctx, "2026-10-16", nil)
	if err != nil {
		t.Fatalf("older samples: %v", err)
	}
	if len(older) != 0 {
		t.Fatalf("older rows = %d, want 0", len(older))
	}
}

func TestSampleMinuteClampsToSession(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	cases := []struct {
		hour, minute int
		want         string
	}{
		{9, 20, "09:31"},
		{9, 31, "09:31"},
		{11, 30, "11:30"},
		{12, 10, "13:01"},
		{13, 1, "13:01"},
		{15, 0, "15:00"},
		{15, 4, "15:00"},
	}
	for _, item := range cases {
		value := time.Date(2026, 10, 19, item.hour, item.minute, 0, 0, location)
		if got := SampleMinute(value); got != item.want {
			t.Errorf("%02d:%02d -> %s, want %s", item.hour, item.minute, got, item.want)
		}
	}
}

func TestInTradingWindow(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	cases := []struct {
		day          int
		hour, minute int
		want         bool
	}{
		{19, 9, 20, false}, // 竞价前
		{19, 9, 30, true},
		{19, 11, 40, false}, // 午休
		{19, 13, 0, true},
		{19, 15, 5, true},
		{19, 15, 6, false},
		{17, 10, 0, false}, // 周六
	}
	for _, item := range cases {
		value := time.Date(2026, 10, item.day, item.hour, item.minute, 0, 0, location)
		if got := InTradingWindow(value); got != item.want {
			t.Errorf("%s %02d:%02d -> %v, want %v", value.Format("2006-01-02"), item.hour, item.minute, got, item.want)
		}
	}
}
