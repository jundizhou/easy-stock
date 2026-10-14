package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"easy-stock/backend/internal/portfolioinspection"
)

func TestPortfolioInspectionDeleteAPI(t *testing.T) {
	server := NewServer(Config{ReviewDBPath: ":memory:", PortfolioDBPath: ":memory:", SettingsPath: ""})
	t.Cleanup(func() { server.Close() })
	ctx := context.Background()
	seedPortfolioResearch(t, server)
	for _, status := range []string{"succeeded", "partial", "failed", "interrupted", "cancelled", "running"} {
		if _, err := server.portfolioStore.Save(ctx, portfolioinspection.Job{ID: status, Status: status}); err != nil {
			t.Fatal(err)
		}
		res := httptest.NewRecorder()
		server.ServeHTTP(res, httptest.NewRequest(http.MethodDelete, "/api/v1/portfolio-inspections/"+status, nil))
		want := http.StatusNoContent
		if status == "running" {
			want = http.StatusConflict
		}
		if res.Code != want {
			t.Fatalf("delete %s: %d %s", status, res.Code, res.Body.String())
		}
		get := httptest.NewRecorder()
		server.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/portfolio-inspections/"+status, nil))
		want = http.StatusNotFound
		if status == "running" {
			want = http.StatusOK
		}
		if get.Code != want {
			t.Fatalf("get %s after delete: %d", status, get.Code)
		}
	}
	for _, id := range []string{"missing", "succeeded"} {
		res := httptest.NewRecorder()
		server.ServeHTTP(res, httptest.NewRequest(http.MethodDelete, "/api/v1/portfolio-inspections/"+id, nil))
		if res.Code != http.StatusNotFound {
			t.Fatalf("delete missing %s: %d", id, res.Code)
		}
	}
	if _, err := server.stockResearchStore.Get(ctx, "reuse-http"); err != nil {
		t.Fatalf("shared research deleted: %v", err)
	}
}
