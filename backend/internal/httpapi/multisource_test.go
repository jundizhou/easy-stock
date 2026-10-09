package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

// ---- fakes ----

type fakeRealtime struct {
	name   string
	quotes []foundation.Quote
	err    error
	calls  int
}

func (f *fakeRealtime) Realtime(_ context.Context, _ []string) ([]foundation.Quote, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.quotes, nil
}

type fakeNews struct {
	items []foundation.NewsItem
	err   error
	calls int
}

func (f *fakeNews) LatestNews(_ context.Context, _ int) ([]foundation.NewsItem, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

func q(symbol, source string) foundation.Quote {
	return foundation.Quote{Symbol: symbol, Price: 1, Meta: foundation.SourceMeta{Source: source}}
}

// ---- Realtime ----

func TestMultiRealtimePrimaryWins(t *testing.T) {
	primary := &fakeRealtime{quotes: []foundation.Quote{q("600519.SH", "sina")}}
	secondary := &fakeRealtime{quotes: []foundation.Quote{q("600519.SH", "tencent")}}
	m := NewMultiRealtime(primary, secondary)
	out, err := m.Realtime(context.Background(), []string{"600519.SH"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out[0].Meta.Source != "sina" {
		t.Fatalf("expected sina primary, got %q", out[0].Meta.Source)
	}
	if secondary.calls != 0 {
		t.Fatalf("secondary should not be called when primary ok")
	}
}

func TestMultiRealtimeFallbackOnPrimaryError(t *testing.T) {
	primary := &fakeRealtime{err: errors.New("blocked")}
	secondary := &fakeRealtime{quotes: []foundation.Quote{q("600519.SH", "tencent")}}
	m := NewMultiRealtime(primary, secondary)
	out, err := m.Realtime(context.Background(), []string{"600519.SH"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out[0].Meta.FallbackReason == "" {
		t.Fatalf("fallback reason should be set")
	}
	if primary.calls != 1 || secondary.calls != 1 {
		t.Fatalf("both should be called once, got %d/%d", primary.calls, secondary.calls)
	}
}

func TestMultiRealtimeAllFail(t *testing.T) {
	primary := &fakeRealtime{err: errors.New("blocked")}
	secondary := &fakeRealtime{err: errors.New("blocked2")}
	m := NewMultiRealtime(primary, secondary)
	if _, err := m.Realtime(context.Background(), []string{"600519.SH"}); err == nil {
		t.Fatalf("expected error when all sources fail")
	}
}

func TestMultiRealtimeCooldown(t *testing.T) {
	primary := &fakeRealtime{err: errors.New("blocked")}
	secondary := &fakeRealtime{quotes: []foundation.Quote{q("600519.SH", "tencent")}}
	m := NewMultiRealtime(primary, secondary)
	// 第一次：primary 失败 -> 计数 1，不冷却，secondary 服务
	if _, err := m.Realtime(context.Background(), nil); err != nil {
		t.Fatalf("first: %v", err)
	}
	// 连续 2 次失败后 primary 进入冷却，第二次 primary 不会被调用
	if primary.calls != 1 {
		t.Fatalf("primary should be called once before cooldown, got %d", primary.calls)
	}
	// 强制连续第二次失败以触发冷却
	if _, err := m.Realtime(context.Background(), nil); err != nil {
		t.Fatalf("second: %v", err)
	}
	if primary.calls != 2 {
		t.Fatalf("primary should be called twice to trigger cooldown, got %d", primary.calls)
	}
	// 现在 primary 应处于冷却，不会被调用
	before := primary.calls
	_, _ = m.Realtime(context.Background(), nil)
	if primary.calls != before {
		t.Fatalf("primary should be skipped during cooldown, calls %d -> %d", before, primary.calls)
	}
}

// ---- News ----

func TestMultiNewsMergeAndSort(t *testing.T) {
	now := time.Now()
	cls := &fakeNews{items: []foundation.NewsItem{
		{ID: "cls-1", Title: "快讯A", PublishedAt: now.Add(-time.Hour), Meta: foundation.SourceMeta{Source: "cls"}},
	}}
	em := &fakeNews{items: []foundation.NewsItem{
		{ID: "em-1", Title: "公告B", PublishedAt: now, Meta: foundation.SourceMeta{Source: "eastmoney"}},
	}}
	m := NewMultiNews(cls, em)
	out, err := m.LatestNews(context.Background(), 10)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 merged, got %d", len(out))
	}
	// 按时间倒序：最新的（公告B）应在前
	if out[0].ID != "em-1" {
		t.Fatalf("expected newest first, got %q", out[0].ID)
	}
}

func TestMultiNewsFallbackOnPrimaryError(t *testing.T) {
	cls := &fakeNews{err: errors.New("blocked")}
	em := &fakeNews{items: []foundation.NewsItem{
		{ID: "em-1", Title: "公告B", Meta: foundation.SourceMeta{Source: "eastmoney"}},
	}}
	m := NewMultiNews(cls, em)
	out, err := m.LatestNews(context.Background(), 10)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) != 1 || out[0].ID != "em-1" {
		t.Fatalf("expected eastmoney fallback, got %+v", out)
	}
}
