package httpapi

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceHealthUsesProbeResultsAndKeepsCacheTimestamp(t *testing.T) {
	var calls atomic.Int32
	cache := &sourceHealthCache{checks: map[string][]sourceProbe{
		"sina":      {probe("实时行情", func(context.Context) error { calls.Add(1); return nil })},
		"eastmoney": {probe("日 K 线", func(context.Context) error { return errors.New("network unavailable") })},
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
	cache := &sourceHealthCache{checks: map[string][]sourceProbe{"sina": {probe("实时行情", func(ctx context.Context) error { return ctx.Err() })}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cache.load(ctx)
	if !cache.expires.IsZero() {
		t.Fatal("cancelled request cached a false outage")
	}
}

func TestEastMoneyReportsEachProbeIndependently(t *testing.T) {
	failure := errors.New("empty response")
	for _, tc := range []struct {
		kline, profile  error
		status, summary string
	}{
		{nil, nil, "healthy", "日 K 线通过 · 公司资料通过"},
		{failure, nil, "partial", "日 K 线失败 · 公司资料通过"},
		{nil, failure, "partial", "日 K 线通过 · 公司资料失败"},
		{failure, failure, "unhealthy", "日 K 线失败 · 公司资料失败"},
	} {
		cache := sourceHealthCache{checks: map[string][]sourceProbe{"eastmoney": {
			probe("日 K 线", func(context.Context) error { return tc.kline }),
			probe("公司资料", func(context.Context) error { return tc.profile }),
		}}}
		for _, source := range cache.load(context.Background()) {
			if source.ID != "eastmoney" {
				continue
			}
			if source.Status != tc.status || source.OK != (tc.status == "healthy") || source.ProbeSummary != tc.summary {
				t.Errorf("unexpected health: %+v", source)
			}
			if !source.OK && !strings.Contains(source.Message, failure.Error()) {
				t.Errorf("missing diagnostic: %s", source.Message)
			}
		}
	}
}

func TestDailyBarHealthReportsServingSourceAfterFallback(t *testing.T) {
	failed := &stubKLine{err: errors.New("empty response")}
	fallback := &stubKLine{bars: okBars(2)}
	for i := range fallback.bars {
		fallback.bars[i].Meta.Source = "sina"
	}
	cache := newSourceHealthCache("", nil, nil, nil, nil, newKLineChain(failed, fallback))
	check := cache.checks["cn-kline"]
	cache.checks = map[string][]sourceProbe{"cn-kline": check, "eastmoney": {probe("公司资料", func(context.Context) error { return errors.New("F10 unavailable") })}}
	for _, s := range cache.load(context.Background()) {
		if s.ID == "cn-kline" && (!s.OK || !strings.Contains(s.ProbeSummary, "新浪财经")) {
			t.Fatalf("daily bars: %+v", s)
		}
		if s.ID == "eastmoney" && s.Status != "unhealthy" {
			t.Fatalf("company profile: %+v", s)
		}
	}
}
