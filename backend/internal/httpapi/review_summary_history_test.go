package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"easy-stock/backend/internal/review"
)

func TestReviewSummaryHistoryReadsSavedDocumentsWithoutAutomation(t *testing.T) {
	store, err := review.OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Historical reviews can have old prompts, custom windows, or no job row.
	summary := review.DailySummary{
		TradeDate: "2026-10-20", PromptVersion: "old-prompt", FreshnessRule: "旧版规则",
		WindowStart: time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC), WindowEnd: time.Date(2026, 10, 20, 22, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, 10, 20, 23, 0, 0, 0, time.UTC), ArticleCount: 12, AuthorCount: 3,
		ExecutiveSummary: "旧版复盘也应完整展示", Partial: true, GenerationErrors: []string{"某阶段未完成"},
		AuthorViews: []review.DailyAuthorView{{Author: "作者甲", CoreView: "独立观点", Evidence: []string{"原始证据"}}},
	}
	if _, err := store.SaveDailySummary(context.Background(), summary); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveDailySummary(context.Background(), review.DailySummary{TradeDate: "2026-10-21", GeneratedAt: summary.GeneratedAt}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{ReviewStore: store})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/reviews/daily-summary?trade_date=2026-10-20", nil))
	var detail struct {
		Data review.DailySummary `json:"data"`
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(detail.Data, summary) {
		t.Fatalf("saved review changed: got=%+v want=%+v", detail.Data, summary)
	}

	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/reviews/daily-summary/history?limit=1&offset=1", nil))
	var history struct {
		Data   []review.DailySummaryHistoryItem `json:"data"`
		Total  int                              `json:"total"`
		Limit  int                              `json:"limit"`
		Offset int                              `json:"offset"`
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if history.Total != 2 || history.Limit != 1 || history.Offset != 1 || len(history.Data) != 1 || history.Data[0].TradeDate != summary.TradeDate || history.Data[0].AuthorCount != 3 || history.Data[0].ArticleCount != 12 {
		t.Fatalf("history=%+v", history)
	}
}

func TestReviewSummaryHistoryValidationAndEmptyState(t *testing.T) {
	store, err := review.OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := NewServer(Config{ReviewStore: store})
	for _, tc := range []struct {
		query string
		code  int
	}{
		{"?trade_date=2026-10-19", http.StatusNotFound},
		{"?trade_date=", http.StatusBadRequest},
		{"?trade_date=2026-02-30", http.StatusBadRequest},
		{"?trade_date=2026-1-1", http.StatusBadRequest},
		{"?trade_date=2026-10-19&window_start=", http.StatusBadRequest},
		{"/history?limit=0", http.StatusBadRequest},
		{"/history?limit=101", http.StatusBadRequest},
		{"/history?offset=-1", http.StatusBadRequest},
		{"/history?offset=invalid", http.StatusBadRequest},
	} {
		t.Run(tc.query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/reviews/daily-summary"+tc.query, nil))
			if recorder.Code != tc.code {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tc.code, recorder.Body.String())
			}
		})
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/reviews/daily-summary/history", nil))
	var empty struct {
		Data  []review.DailySummaryHistoryItem `json:"data"`
		Total int                              `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || empty.Data == nil || len(empty.Data) != 0 || empty.Total != 0 {
		t.Fatalf("empty history status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
