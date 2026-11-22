package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestPortfolioFXUsesDatedReciprocalRates(t *testing.T) {
	date := time.Now().UTC().Format("2006-01-02")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, `{"base":"CNY","date":"%s","rates":{"HKD":1.25,"USD":0.125}}`, date)
	}))
	defer server.Close()
	cache := portfolioFXRates{url: server.URL}
	quotes := []foundation.Quote{{Currency: "HKD"}, {Currency: "USD"}, {Currency: "CNY"}, {}}
	cache.apply(context.Background(), quotes)
	cache.apply(context.Background(), quotes)
	if calls != 1 {
		t.Fatalf("fetch count %d", calls)
	}
	for i, want := range []float64{0.8, 8} {
		v := quotes[i].CNYConversion
		if v == nil || v.Rate != want || v.Date != date {
			t.Fatalf("quote[%d] conversion=%+v", i, v)
		}
	}
	if quotes[2].CNYConversion != nil || quotes[3].CNYConversion != nil {
		t.Fatal("unsupported conversion", quotes)
	}
}

func TestPortfolioFXRejectsOldAndMismatchedRates(t *testing.T) {
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ date, base string }{
		{"2026-11-15", "CNY"}, {"2026-11-23", "CNY"}, {"2026-11-20", "USD"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"date":"%s","base":"%s","rates":{"HKD":1.25}}`, tc.date, tc.base)
		}))
		cache := portfolioFXRates{url: server.URL}
		got := cache.latest(context.Background(), now)
		server.Close()
		if len(got.Rates) != 0 {
			t.Fatalf("invalid rate accepted %+v", got)
		}
	}
}
