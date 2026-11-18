package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type testNationalTeamProvider struct {
	fail  bool
	calls int
}

func (p *testNationalTeamProvider) ETFSubscriptions(context.Context) (foundation.ETFSubscriptionDashboard, foundation.SourceMeta, error) {
	if p.fail {
		return foundation.ETFSubscriptionDashboard{}, foundation.SourceMeta{}, fmt.Errorf("subscription source failed")
	}
	return foundation.ETFSubscriptionDashboard{SourceDate: "2026-11-16", LatestCompleteDate: "2026-11-16", Expected: 22, Days: []foundation.ETFSubscriptionDay{{Date: "2026-11-16", Coverage: 21}}}, foundation.SourceMeta{Source: "test:etf-subscriptions", FetchedAt: time.Now()}, nil
}
func TestETFSubscriptionRouteKeepsDatesCoverageNullAndFallback(t *testing.T) {
	provider := &testNationalTeamProvider{}
	server := NewServer(Config{ETFSubscriptions: provider})
	defer server.Close()
	path := "/api/v1/market/national-team-etfs/subscriptions"
	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		server.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		return r
	}
	first := get()
	if first.Code != 200 || !strings.Contains(first.Body.String(), `"expected":22`) || !strings.Contains(first.Body.String(), `"estimated_net":null`) || !strings.Contains(first.Body.String(), `"source_date":"2026-11-16"`) {
		t.Fatalf("first=%d %s", first.Code, first.Body.String())
	}
	for _, removed := range []string{"/api/v1/market/national-team-etfs", "/api/v1/market/national-team-etfs/shares", "/api/v1/market/national-team-etfs/history"} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, removed, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("removed route %s returned %d", removed, rec.Code)
		}
	}
	server.marketSnapshots.mu.Lock()
	snapshot := server.marketSnapshots.items["etf-subscriptions"]
	snapshot.expiresAt = time.Now().Add(-time.Second)
	server.marketSnapshots.items["etf-subscriptions"] = snapshot
	server.marketSnapshots.mu.Unlock()
	provider.fail = true
	stale := get()
	if stale.Code != 200 || !strings.Contains(stale.Body.String(), `"stale":true`) {
		t.Fatalf("stale=%d %s", stale.Code, stale.Body.String())
	}
}
