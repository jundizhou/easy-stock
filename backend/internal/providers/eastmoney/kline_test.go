package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientKLineParsesEastMoneyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/qt/stock/kline/get" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("secid"); got != "0.000001" {
			t.Fatalf("secid = %q, want 0.000001", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"rc": 0,
			"data": {
				"klines": [
					"2026-06-12,10.00,10.50,10.80,9.90,123456,123456789.00,8.50,5.00,0.50,1.20"
				],
				"name": "平安银行",
				"code": "000001"
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	got, err := client.KLine(context.Background(), "000001.SZ", "day", 1)
	if err != nil {
		t.Fatalf("KLine returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(KLine) = %d, want 1", len(got))
	}
	if got[0].Symbol != "000001.SZ" || got[0].Close != 10.50 || got[0].Volume != 123456 {
		t.Fatalf("unexpected kline: %+v", got[0])
	}
	wantDate := time.Date(2026, 6, 12, 0, 0, 0, 0, time.Local)
	if !got[0].Time.Equal(wantDate) {
		t.Fatalf("Time = %v, want %v", got[0].Time, wantDate)
	}
	if got[0].Meta.Source != "eastmoney" || got[0].Meta.SourceURL == "" {
		t.Fatalf("unexpected meta: %+v", got[0].Meta)
	}
}

func TestClientKLineRetriesTransientHTTPFailure(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			http.Error(w, "temporary upstream error", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"rc": 0,
			"data": {"klines": ["2026-06-12,10.00,10.50,10.80,9.90,123456,123456789.00,8.50,5.00,0.50,1.20"]}
		}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	got, err := client.KLine(context.Background(), "000001.SZ", "day", 1)
	if err != nil {
		t.Fatalf("KLine returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(KLine) = %d, want 1", len(got))
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}
