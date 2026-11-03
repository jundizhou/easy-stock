package eastmoney

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 合成测试页，不向运行中的行情源或用户数据写入任何内容。
func writeRollingMarketPage(w http.ResponseWriter, page, total int) {
	start := (page - 1) * marketSnapshotPageSize
	count := min(marketSnapshotPageSize, max(0, total-start))
	items := make([]map[string]any, count)
	for index := range items {
		items[index] = map[string]any{
			"f12": fmt.Sprintf("%06d", 600000+start+index), "f13": 1, "f14": "测试样本",
			"f2": 10, "f3": 1, "f17": 10, "f18": 9.9, "f5": 1000,
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"rc": 0, "data": map[string]any{"total": total, "diff": items}})
}

func TestMarketSnapshotSlowPageDoesNotBlockLaterPageRequests(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	var active, peak atomic.Int32
	const total = 900
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inFlight := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); inFlight > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, inFlight) {
				break
			}
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("pn"))
		if r.URL.Query().Get("fid") != "f12" || r.URL.Query().Get("po") != "1" {
			t.Error("snapshot must retain symbol-ascending pagination")
		}
		if page == 2 {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if page == marketSnapshotWorkers+2 {
			once.Do(func() { close(release) })
		}
		writeRollingMarketPage(w, page, total)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, meta, err := NewClient(WithQuoteBaseURL(server.URL)).MarketSnapshot(ctx)
	if err != nil || meta.Stale || len(rows) != total {
		t.Fatalf("slow page blocked later requests: rows=%d meta=%+v err=%v", len(rows), meta, err)
	}
	if peak.Load() > marketSnapshotWorkers {
		t.Fatalf("concurrency grew beyond %d: %d", marketSnapshotWorkers, peak.Load())
	}
	for index, row := range rows {
		if row.Symbol != fmt.Sprintf("%06d", 600000+index) {
			t.Fatalf("response order changed at %d: %s", index, row.Symbol)
		}
	}
}

func TestMarketSnapshotRetriesFailedPageWithoutRefetchingSuccessfulPages(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(strconv.FormatBool(persistent), func(t *testing.T) {
			var mu sync.Mutex
			calls := map[int]int{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page, _ := strconv.Atoi(r.URL.Query().Get("pn"))
				mu.Lock()
				calls[page]++
				attempt := calls[page]
				mu.Unlock()
				if page == 2 && (persistent || attempt == 1) {
					http.Error(w, "temporary page failure", http.StatusServiceUnavailable)
					return
				}
				writeRollingMarketPage(w, page, 300)
			}))
			defer server.Close()
			rows, meta, err := NewClient(WithQuoteBaseURL(server.URL)).MarketSnapshot(context.Background())
			wantRows, wantAttempts := 300, 2
			if persistent {
				wantRows, wantAttempts = 200, 3
			}
			if err != nil || len(rows) != wantRows || meta.Stale != persistent || (persistent && !strings.Contains(meta.FallbackReason, "503")) {
				t.Fatalf("page retry lost data or hid failure: rows=%d meta=%+v err=%v", len(rows), meta, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if calls[1] != 1 || calls[2] != wantAttempts || calls[3] != 1 {
				t.Fatalf("retry re-fetched successful pages: %+v", calls)
			}
		})
	}
}

func TestMarketSnapshotCancelledPoolDoesNotStartRequests(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeRollingMarketPage(w, 1, 100)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := NewClient(WithQuoteBaseURL(server.URL)).MarketSnapshot(ctx)
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("cancelled pool issued requests or lost cancellation: calls=%d err=%v", calls.Load(), err)
	}
}
