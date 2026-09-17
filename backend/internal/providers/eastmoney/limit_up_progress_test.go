package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestProgressiveHistoryPublishesRecentDayBeforeSlowHistory(t *testing.T) {
	gate := make(chan struct{})
	var calls, active, peak atomic.Int32
	now := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		if r.URL.Query().Get("date") != now.Format("20060102") {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		w.Write([]byte(`{"rc":0,"data":{"pool":[{"c":"600001","n":"测试股","p":10000,"lbc":2}]}}`))
	}))
	defer server.Close()
	c := NewClient(WithTopicBaseURL(server.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	partial := make(chan []foundation.LimitUpEvent, 8)
	done := make(chan error, 1)
	go func() {
		_, err := c.ProgressiveRecentLimitUps(ctx, 8, func(events []foundation.LimitUpEvent) { partial <- events })
		done <- err
	}()
	select {
	case first := <-partial:
		if len(first) != 1 || first[0].Date.Format("20060102") != now.Format("20060102") {
			t.Fatalf("wrong first batch: %+v", first)
		}
	case <-ctx.Done():
		t.Fatal("fast current day blocked on old history")
	}
	close(gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 3 || peak.Load() < 2 {
		t.Fatalf("unexpected parallelism %d", peak.Load())
	}
	before := calls.Load()
	items, err := c.RecentLimitUps(ctx, 8)
	if err != nil || len(items) != 8 || calls.Load() != before {
		t.Fatalf("history cache not reused: items=%d calls=%d err=%v", len(items), calls.Load(), err)
	}
	items[0].Name = "mutated"
	again, _ := c.RecentLimitUps(ctx, 8)
	if again[0].Name == "mutated" {
		t.Fatal("consumer mutated cached pool")
	}
}

func TestLimitUpDayDeduplicatesConcurrentReads(t *testing.T) {
	gate, started := make(chan struct{}), make(chan struct{}, 4)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		w.Write([]byte(`{"rc":0,"data":{"pool":[]}}`))
	}))
	defer server.Close()
	c := NewClient(WithTopicBaseURL(server.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := c.LimitUpPool(ctx, time.Now()); done <- err }()
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("not started")
	}
	close(gate)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate day requests: %d", calls.Load())
	}
}
