package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"easy-stock/backend/internal/foundation"
)

func requireLive(t *testing.T) {
	t.Helper()
	if os.Getenv("A_STOCK_LIVE_TEST") != "1" {
		t.Skip("set A_STOCK_LIVE_TEST=1 to run live data-source tests")
	}
}

func TestLiveAPIRealtimeReturnsRealQuotesAcrossMarkets(t *testing.T) {
	requireLive(t)
	server := NewServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotes/realtime?symbols=000001.SZ,600000.SH,300750.SZ", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []foundation.Quote `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data) != 3 {
		t.Fatalf("quotes len = %d, want 3: %+v", len(payload.Data), payload.Data)
	}
	for _, quote := range payload.Data {
		if quote.Symbol == "" || quote.Name == "" || quote.Price <= 0 {
			t.Fatalf("unusable quote: %+v", quote)
		}
		if quote.Meta.Source != "sina" || quote.Meta.SourceURL == "" {
			t.Fatalf("missing realtime source evidence: %+v", quote.Meta)
		}
	}
}

func TestLiveAPIKLineReturnsRealBarsWithFallback(t *testing.T) {
	requireLive(t)
	server := NewServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000001.SZ&period=day&limit=2", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []foundation.KLine `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data) == 0 {
		t.Fatal("kline returned no bars")
	}
	last := payload.Data[len(payload.Data)-1]
	if last.Symbol != "000001.SZ" || last.Close <= 0 || last.Meta.Source == "" || last.Meta.SourceURL == "" {
		t.Fatalf("unusable kline bar: %+v", last)
	}
}

func TestLiveAPINewsReturnsRealItems(t *testing.T) {
	requireLive(t)
	server := NewServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/market/news?source=cls&limit=2", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []foundation.NewsItem `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data) == 0 {
		t.Fatal("news returned no items")
	}
	if payload.Data[0].Title == "" || payload.Data[0].Meta.Source != "cls" || payload.Data[0].Meta.SourceURL == "" {
		t.Fatalf("unusable news item: %+v", payload.Data[0])
	}
}
