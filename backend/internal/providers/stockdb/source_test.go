package stockdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDailySource 是可控的测试用 DailySource。
type fakeDailySource struct {
	name      string
	qfq       bool
	failUntil int64 // unixnano；在此之前返回错误（模拟临时故障/被封）
	calls     int64
	latency   time.Duration
	bar       Bar
}

func (f *fakeDailySource) Name() string { return f.name }
func (f *fakeDailySource) QFQ() bool    { return f.qfq }
func (f *fakeDailySource) Health() (bool, float64) {
	if time.Now().UnixNano() < atomic.LoadInt64(&f.failUntil) {
		return false, 0
	}
	return true, float64(f.latency.Milliseconds())
}
func (f *fakeDailySource) DailyBars(_ context.Context, _ string, _ int) ([]Bar, error) {
	atomic.AddInt64(&f.calls, 1)
	if time.Now().UnixNano() < atomic.LoadInt64(&f.failUntil) {
		return nil, errors.New("blocked")
	}
	return []Bar{f.bar}, nil
}

func TestMultiSourcePrefersPrimary(t *testing.T) {
	primary := &fakeDailySource{name: "primary", qfq: true, bar: Bar{Date: 20240101}}
	remote := &fakeDailySource{name: "remote", qfq: true, bar: Bar{Date: 20240102}}
	ms := NewMultiSource(primary, remote)

	bars, err := ms.DailyBars(context.Background(), "600519", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bars[0].Date != 20240101 {
		t.Fatalf("expected primary served, got %v", bars[0].Date)
	}
	if atomic.LoadInt64(&remote.calls) != 0 {
		t.Fatalf("remote should not be called when primary healthy; calls=%d", remote.calls)
	}
}

func TestMultiSourceFallsBackOnPrimaryFailure(t *testing.T) {
	// 主源持续故障，远程源健康
	primary := &fakeDailySource{name: "primary", qfq: true, bar: Bar{Date: 20240101}}
	atomic.StoreInt64(&primary.failUntil, time.Now().Add(time.Hour).UnixNano())
	remote := &fakeDailySource{name: "remote", qfq: true, bar: Bar{Date: 20240102}}
	ms := NewMultiSource(primary, remote)

	bars, err := ms.DailyBars(context.Background(), "600519", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bars[0].Date != 20240102 {
		t.Fatalf("expected remote fallback served, got %v", bars[0].Date)
	}
}

func TestMultiSourceLoadBalancesRemotes(t *testing.T) {
	primary := &fakeDailySource{name: "primary", qfq: true}
	atomic.StoreInt64(&primary.failUntil, time.Now().Add(time.Hour).UnixNano())
	a := &fakeDailySource{name: "a", qfq: true, bar: Bar{Date: 1}}
	b := &fakeDailySource{name: "b", qfq: true, bar: Bar{Date: 2}}
	ms := NewMultiSource(primary, a, b)

	for i := 0; i < 10; i++ {
		if _, err := ms.DailyBars(context.Background(), "x", 1); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	// 两个远程源都应被调用（轮询负载均衡），且调用次数接近
	if got := atomic.LoadInt64(&a.calls); got == 0 || got > 8 {
		t.Fatalf("remote a load-balance off: calls=%d", got)
	}
	if got := atomic.LoadInt64(&b.calls); got == 0 || got > 8 {
		t.Fatalf("remote b load-balance off: calls=%d", got)
	}
}

func TestMultiSourceStatus(t *testing.T) {
	primary := &fakeDailySource{name: "local", qfq: true}
	remote := &fakeDailySource{name: "remote", qfq: true}
	ms := NewMultiSource(primary, remote)
	st := ms.Status()
	if len(st) != 2 {
		t.Fatalf("expected 2 source statuses, got %d", len(st))
	}
	if st[0].Name != "local" || st[1].Name != "remote" {
		t.Fatalf("unexpected status order: %+v", st)
	}
	if !st[0].QFQ || !st[1].QFQ {
		t.Fatalf("expected qfq=true: %+v", st)
	}
}

// TestEastmoneySourceParsing 用假端点验证东财 push2his 响应解析与前复权字段映射。
func TestEastmoneySourceParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("secid"); got != "1.600519" {
			t.Errorf("secid = %q, want 1.600519", got)
		}
		if got := r.URL.Query().Get("fqt"); got != "1" {
			t.Errorf("fqt = %q, want 1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rc":0,"data":{"klines":[
			"2024-01-02,10.0,10.5,10.8,9.9,1000",
			"2024-01-03,10.5,11.0,11.2,10.4,1200"
		]}}`))
	}))
	defer srv.Close()

	src := NewEastmoneyDailySource(srv.URL)
	bars, err := src.DailyBars(context.Background(), "600519", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bars) != 2 {
		t.Fatalf("expected 2 bars, got %d", len(bars))
	}
	if bars[0].Date != 20240102 || bars[1].Date != 20240103 {
		t.Fatalf("date parse wrong: %+v", bars)
	}
	if bars[0].Close != 10.5 || bars[1].Open != 10.5 {
		t.Fatalf("ohlc mapping wrong: %+v", bars)
	}
	if !src.QFQ() {
		t.Fatalf("eastmoney source should report qfq=true")
	}
}

func TestEastmoneySourceBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	src := NewEastmoneyDailySource(srv.URL)
	// 连续两次失败（达到降级阈值 2）后源进入 unhealthy
	for i := 0; i < 2; i++ {
		if _, err := src.DailyBars(context.Background(), "600519", 10); err == nil {
			t.Fatalf("expected error on 500 (iter %d)", i)
		}
	}
	if ok, _ := src.Health(); ok {
		t.Fatalf("source should be unhealthy after consecutive failures")
	}
}

func TestEastmoneySecID(t *testing.T) {
	cases := map[string]string{"600519": "1.600519", "000001": "0.000001", "300750": "0.300750", "8xxxxx": "0.8xxxxx"}
	for code, want := range cases {
		if got := eastmoneySecID(code); got != want {
			t.Errorf("eastmoneySecID(%q) = %q, want %q", code, got, want)
		}
	}
}

// TestClientMultiSourceIntegration 验证 Client 在配置远程源后，DailyBars 仍走本地
// （本地健康时），且 DailySources() 暴露多源健康。
func TestClientMultiSourceIntegration(t *testing.T) {
	c := NewClient(WithDailySource(&fakeDailySource{name: "remote", qfq: true}))
	if c.multi == nil {
		t.Fatalf("expected multi-source pool built")
	}
	st := c.DailySources()
	if len(st) != 2 {
		t.Fatalf("expected 2 daily sources, got %d (%+v)", len(st), st)
	}
	if st[0].Name != "stockdb-local" {
		t.Fatalf("first source should be local primary, got %q", st[0].Name)
	}
}
