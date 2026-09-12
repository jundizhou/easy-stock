package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
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

func TestParseKLineSupportsIntradayTime(t *testing.T) {
	got, err := parseKLine("2026-08-12 14:35,10.00,10.50,10.80,9.90,123,456,8.5,5,0.5,1.2", "000001.SZ", foundation.SourceMeta{})
	if err != nil {
		t.Fatalf("parseKLine returned error: %v", err)
	}
	if got.Time.Hour() != 14 || got.Time.Minute() != 35 {
		t.Fatalf("Time = %v, want 14:35", got.Time)
	}
}

func TestQuoteAvailabilityBatchParsesAmountAndTurnover(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/qt/ulist.np/get" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		secids := r.URL.Query().Get("secids")
		if !strings.Contains(secids, "1.600150") || !strings.Contains(secids, "0.000001") {
			t.Fatalf("secids = %q, want both symbols", secids)
		}
		if fields := r.URL.Query().Get("fields"); !strings.Contains(fields, "f6") || !strings.Contains(fields, "f8") {
			t.Fatalf("fields = %q, want f6 and f8", fields)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"rc": 0,
			"data": {
				"total": 2,
				"diff": [
					{"f12":"600150","f13":1,"f14":"中国船舶","f6":7144558405.0,"f8":2.34},
					{"f12":"000001","f13":0,"f14":"平安银行","f6":1022543914.24,"f8":0.45}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	got, err := client.QuoteAvailabilityBatch(context.Background(), []string{"600150.SH", "000001.SZ"})
	if err != nil {
		t.Fatalf("QuoteAvailabilityBatch returned error: %v", err)
	}
	ship, ok := got["600150.SH"]
	if !ok {
		t.Fatalf("missing 600150.SH in %+v", got)
	}
	if ship.Amount != 7144558405.0 || ship.TurnoverRate != 2.34 {
		t.Fatalf("unexpected 600150.SH snapshot: %+v", ship)
	}
	bank, ok := got["000001.SZ"]
	if !ok {
		t.Fatalf("missing 000001.SZ in %+v", got)
	}
	if bank.Amount != 1022543914.24 || bank.TurnoverRate != 0.45 {
		t.Fatalf("unexpected 000001.SZ snapshot: %+v", bank)
	}
}

func TestQuoteAvailabilityBatchSkipsUnknownAndForeignMarkets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"rc": 0,
			"data": {
				"total": 3,
				"diff": [
					{"f12":"600150","f13":1,"f6":100,"f8":1.5},
					{"f12":"00700","f13":116,"f6":200,"f8":2.5},
					{"f12":"999999","f13":0,"f6":300,"f8":3.5}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	got, err := client.QuoteAvailabilityBatch(context.Background(), []string{"600150.SH"})
	if err != nil {
		t.Fatalf("QuoteAvailabilityBatch returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want only the requested A-share: %+v", len(got), got)
	}
	if _, ok := got["600150.SH"]; !ok {
		t.Fatalf("missing 600150.SH in %+v", got)
	}
}

func TestQuoteAvailabilityBatchReturnsEmptyForNoSymbols(t *testing.T) {
	client := NewClient()
	got, err := client.QuoteAvailabilityBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("QuoteAvailabilityBatch returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d entries, want 0", len(got))
	}
}
