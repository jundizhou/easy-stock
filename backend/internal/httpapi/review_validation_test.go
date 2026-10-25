package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/review"
)

type validationMarketOverview struct{ fakeUSMarketOverview }

func (validationMarketOverview) MarketIndexes(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	return []foundation.MarketIndexSnapshot{
		{ID: "sse", Market: "CN", Meta: foundation.SourceMeta{TradeDate: "2026-10-23"}},
		{ID: "dow", Market: "US", Meta: foundation.SourceMeta{TradeDate: "2026-10-22"}},
	}, foundation.SourceMeta{}, nil
}

func TestReviewValidationCollectorUsesAStockSessionsOnly(t *testing.T) {
	server := &Server{marketOverview: validationMarketOverview{}}
	snapshot, err := server.collectDailyValidationSnapshot(context.Background(), review.DailySummary{TradeDate: "2026-10-22"})
	if err != nil || len(snapshot.Indexes) != 1 || snapshot.Indexes[0].ID != "sse" || snapshot.TradeDate != "2026-10-23" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestReviewValidationUsesSelectedSavedSummary(t *testing.T) {
	store, err := review.OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, date := range []string{"2026-10-19", "2026-10-23", "2099-10-23"} {
		if _, err := store.SaveDailySummary(context.Background(), review.DailySummary{TradeDate: date}); err != nil {
			t.Fatal(err)
		}
	}
	server := NewServer(Config{ReviewStore: store})
	for _, test := range []struct{ date, status string }{{"2026-10-19", "idle"}, {"2099-10-23", "waiting"}, {"2026-10-18", "idle"}} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/reviews/daily-validation?summary_date="+test.date, nil))
		var payload struct {
			SummaryDate string                    `json:"summary_date"`
			Job         review.DailyValidationJob `json:"job"`
		}
		if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &payload) != nil || payload.SummaryDate != test.date || payload.Job.Status != test.status {
			t.Fatalf("selected date=%s status=%d body=%s", test.date, recorder.Code, recorder.Body.String())
		}
	}
	summary, err := server.reviewAutomation.LatestDailySummaryBefore(context.Background(), "2026-10-25")
	if err != nil || summary.TradeDate != "2026-10-23" {
		t.Fatalf("weekend latest summary=%+v err=%v", summary, err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/reviews/daily-validation", strings.NewReader(`{"summary_date":"2099-10-23"}`)))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "收盘后") {
		t.Fatalf("future validation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestReviewValidationCollectorRejectsUndatedMarketData(t *testing.T) {
	server := &Server{marketOverview: fakeUSMarketOverview{}}
	if _, err := server.collectDailyValidationSnapshot(context.Background(), review.DailySummary{TradeDate: "2026-10-23"}); err == nil || !strings.Contains(err.Error(), "交易日期") {
		t.Fatalf("undated snapshot error=%v", err)
	}
}
