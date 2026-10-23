package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/sina"
)

type unavailableKLineProvider struct{}

func (unavailableKLineProvider) KLine(context.Context, string, string, int) ([]foundation.KLine, error) {
	return nil, errors.New("primary kline source unavailable")
}

func TestMonthlyKLineFallsBackToSina(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("scale") != "7200" || query.Get("datalen") != "1" || (query.Get("symbol") != "sh603278" && query.Get("symbol") != "sz003029") {
			t.Errorf("unexpected Sina monthly request: %s", r.URL)
			http.Error(w, "invalid monthly request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`callback([{"day":"2026-09-30","open":"10.00","high":"12.00","low":"9.00","close":"11.00","volume":"10000"}]);`))
	}))
	defer upstream.Close()
	server := NewServer(Config{
		KLinePrimary:  unavailableKLineProvider{},
		KLineFallback: sina.NewClient(sina.WithKLineBaseURL(upstream.URL)),
	})
	for _, symbol := range []string{"603278.SH", "003029.SZ"} {
		t.Run(symbol, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol="+symbol+"&period=month&limit=1", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("monthly fallback status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var payload struct {
				Data []foundation.KLine `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Data) != 1 || payload.Data[0].Symbol != symbol || payload.Data[0].Close != 11 || payload.Data[0].Time.Format("2006-01-02") != "2026-09-30" || payload.Data[0].Meta.Source != "sina" {
				t.Fatalf("unexpected monthly fallback bars: %+v", payload.Data)
			}
		})
	}
}
