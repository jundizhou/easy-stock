package httpapi

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceHealthUsesProbeResultsAndKeepsCacheTimestamp(t *testing.T) {
	var calls atomic.Int32
	cache := &sourceHealthCache{checks: map[string]func(context.Context) error{
		"sina":      func(context.Context) error { calls.Add(1); return nil },
		"eastmoney": func(context.Context) error { return errors.New("network unavailable") },
	}}
	first := cache.load(context.Background())
	second := cache.load(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("cached probe calls=%d", calls.Load())
	}
	for index, item := range first {
		if item.ID == "sina" && (!item.OK || item.Status != "healthy" || item.CheckedAt.IsZero() || !item.CheckedAt.Equal(second[index].CheckedAt)) {
			t.Fatalf("healthy source=%+v", item)
		}
		if item.ID == "eastmoney" && (item.OK || item.Status != "unhealthy") {
			t.Fatalf("failed source=%+v", item)
		}
		if item.ID == "tushare" || item.ID == "tradingview" {
			if item.Enabled || item.Status != "disabled" || !item.CheckedAt.IsZero() {
				t.Fatalf("reserved source=%+v", item)
			}
		}
	}
	cache.expires = time.Now().Add(-time.Second)
	cache.load(context.Background())
	if calls.Load() != 2 {
		t.Fatal("expired source checks were not refreshed")
	}
}

func TestCancelledSourceCheckDoesNotPoisonCache(t *testing.T) {
	cache := &sourceHealthCache{checks: map[string]func(context.Context) error{"sina": func(ctx context.Context) error { return ctx.Err() }}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cache.load(ctx)
	if !cache.expires.IsZero() {
		t.Fatal("cancelled request cached a false outage")
	}
}
