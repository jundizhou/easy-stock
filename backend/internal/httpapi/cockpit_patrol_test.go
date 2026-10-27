package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/cockpit"
	"easy-stock/backend/internal/foundation"
)

func TestCockpitAutoPatrolEndpointPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	newServer := func() *Server {
		server := NewServer(Config{SettingsPath: path})
		t.Cleanup(func() { _ = server.Close() })
		return server
	}
	server := newServer()
	for _, enabled := range []bool{false, true} {
		body := `{"enabled":false}`
		if enabled {
			body = `{"enabled":true}`
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/cockpit/auto-patrol", strings.NewReader(body)))
		if response.Code != http.StatusOK {
			t.Fatalf("update code=%d body=%s", response.Code, response.Body.String())
		}
		var payload struct {
			Data cockpit.Status `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Data.AutoPatrolEnabled != enabled || (!enabled && payload.Data.NextRunAt != nil) {
			t.Fatalf("unexpected update status: %+v", payload.Data)
		}
		server = newServer()
		response = httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/cockpit/status", nil))
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || payload.Data.AutoPatrolEnabled != enabled {
			t.Fatalf("setting lost on restart: code=%d status=%+v", response.Code, payload.Data)
		}
	}
}

func TestCockpitAutoPatrolEndpointRejectsInvalidUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	server := NewServer(Config{SettingsPath: path})
	defer server.Close()
	for _, body := range []string{`{}`, `null`, `{"enabled":null}`, `{"enabled":"false"}`, `{"enabled":false,"unknown":true}`, `{"enabled":false} {}`, `{"enabled":`, `{"interval_minutes":0}`, `{"interval_minutes":-5}`, `{"interval_minutes":7}`, `{"interval_minutes":120}`, `{"interval_minutes":null}`, `{"interval_minutes":"15"}`, `{"enabled":false,"interval_minutes":12}`} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/cockpit/auto-patrol", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest || !server.cockpit.Status().AutoPatrolEnabled || server.cockpit.Status().IntervalMinutes != 30 {
			t.Fatalf("invalid body %q: code=%d, must not change settings", body, response.Code)
		}
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/cockpit/auto-patrol", strings.NewReader(`{"enabled":false}`)))
	if response.Code != http.StatusInternalServerError || !server.cockpit.Status().AutoPatrolEnabled {
		t.Fatalf("failed persistence code=%d, must retain enabled state", response.Code)
	}
	server.cockpit = nil
	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/cockpit/auto-patrol", strings.NewReader(`{"enabled":false}`)))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable service code=%d", response.Code)
	}
}

func TestCockpitAutoPatrolIntervalEndpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	server := NewServer(Config{SettingsPath: path})
	defer server.Close()
	for _, tc := range []struct {
		body     string
		enabled  bool
		interval int
	}{
		{`{"interval_minutes":5}`, true, 5},
		{`{"interval_minutes":10}`, true, 10},
		{`{"interval_minutes":15}`, true, 15},
		{`{"enabled":false}`, false, 15},
		{`{"interval_minutes":60}`, false, 60},
		{`{"enabled":true,"interval_minutes":30}`, true, 30},
	} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/cockpit/auto-patrol", strings.NewReader(tc.body)))
		var payload struct {
			Data cockpit.Status `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || payload.Data.IntervalMinutes != tc.interval || payload.Data.AutoPatrolEnabled != tc.enabled {
			t.Fatalf("update %s: code=%d status=%+v", tc.body, response.Code, payload.Data)
		}
		reopened := NewServer(Config{SettingsPath: path})
		status := reopened.cockpit.Status()
		_ = reopened.Close()
		if status.IntervalMinutes != tc.interval || status.AutoPatrolEnabled != tc.enabled {
			t.Fatalf("restart lost settings: %+v", status)
		}
	}
}

func TestCockpitIndexQuotesSplitByMarket(t *testing.T) {
	items := []foundation.MarketIndexSnapshot{
		{Name: "上证指数", Market: "CN"},
		{Name: "恒生指数", Market: "HK"},
		{Name: "道琼斯", Code: ".DJI", Market: "US"},
		{Name: "富时100", Code: "UKX", Market: "UK"},
		{Name: "德国DAX30", Code: "GDAXI", Market: "DE"},
		{Name: "法国CAC40", Code: "FCHI", Market: "FR"},
	}
	mainland := cockpitIndexQuotes(items, true)
	overseas := cockpitIndexQuotes(items, false)
	if len(mainland) != 1 || mainland[0].Name != "上证指数" {
		t.Fatalf("mainland = %+v", mainland)
	}
	if len(overseas) != 2 || overseas[0].Name != "恒生指数" || overseas[1].Name != "道琼斯" {
		t.Fatalf("overseas = %+v", overseas)
	}
}

func TestCockpitEndpointsReportRealStateWithoutFabrication(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	// Exercise the real endpoint and patrol service with local market fixtures.
	// Live providers can outlast cleanup on CI even when the endpoint is correct.
	rows, _ := cockpitTemperatureRows(time.Now())
	server := NewServer(Config{
		SettingsPath: settingsPath, StrictPersistence: false,
		MarketOverview:  &fakeMarketOverviewProvider{},
		MonitorSnapshot: chainFakeSnapshot{rows: rows},
		News:            stockAnalysisNews{},
		LimitUp:         fakeLimitUpLadderProvider{},
		MarketPools:     &countingMarketPoolProvider{},
		Realtime:        fakeRealtimeProvider{},
		KLinePrimary:    &countingEmotionKLineProvider{},
		KLineFallback:   &countingEmotionKLineProvider{},
	})
	defer server.Close()
	// The manual endpoint starts a background task. Finish it before TempDir cleanup.
	defer func() {
		deadline := time.Now().Add(30 * time.Second)
		for server.cockpit.Status().Running && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if server.cockpit.Status().Running {
			t.Error("manual patrol did not finish before cleanup")
		}
	}()

	status := httptest.NewRecorder()
	server.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/cockpit/status", nil))
	if status.Code != http.StatusOK {
		t.Fatalf("status code = %d body=%s", status.Code, status.Body.String())
	}
	var payload struct {
		Data struct {
			Phase       string `json:"phase"`
			AIAvailable bool   `json:"ai_available"`
		} `json:"data"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Phase == "" || payload.Data.AIAvailable {
		t.Fatalf("status = %+v; AI must not report available without a configured gateway", payload.Data)
	}

	invalid := httptest.NewRecorder()
	server.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/v1/cockpit/briefs?phase=lunch", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid phase code = %d", invalid.Code)
	}

	run := httptest.NewRecorder()
	server.ServeHTTP(run, httptest.NewRequest(http.MethodPost, "/api/v1/cockpit/briefs", strings.NewReader(`{"phase":"post_close"}`)))
	if run.Code != http.StatusAccepted && run.Code != http.StatusConflict {
		t.Fatalf("manual run code = %d body=%s", run.Code, run.Body.String())
	}

	live := httptest.NewRecorder()
	server.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/api/v1/cockpit/facts", nil))
	if live.Code != http.StatusOK && live.Code != http.StatusServiceUnavailable {
		t.Fatalf("facts code = %d body=%s", live.Code, live.Body.String())
	}
	if live.Code == http.StatusOK && !strings.Contains(live.Body.String(), `"facts"`) {
		t.Fatalf("facts body = %s", live.Body.String())
	}
}

func cockpitTemperatureRows(now time.Time) ([]foundation.MarketSnapshotRow, foundation.SourceMeta) {
	rows := make([]foundation.MarketSnapshotRow, 1000)
	for i := range rows {
		rows[i] = foundation.MarketSnapshotRow{
			Symbol: strconv.Itoa(600000+i) + ".SH", Price: 98, PreviousClose: 100,
			Open: 100, Volume: 1000, TradeDate: now.In(foundation.AStockLocation).Format("2006-01-02"),
		}
	}
	return rows, foundation.SourceMeta{Source: "test:temperature-snapshot", TradeDate: rows[0].TradeDate, FetchedAt: now}
}

func TestCockpitTemperatureCachePreservesRealFactsOnSourceFailure(t *testing.T) {
	now := time.Date(2026, 10, 27, 14, 0, 0, 0, foundation.AStockLocation)
	rows, meta := cockpitTemperatureRows(now)
	var cache cockpitTemperatureCache
	fresh, err := cache.resolve(rows, meta, nil, now)
	if err != nil || fresh == nil || fresh.Stale || fresh.Value != 0 {
		t.Fatalf("real zero temperature must be available: %+v, %v", fresh, err)
	}
	sourceErr := errors.New("snapshot EOF")
	partial := meta
	partial.Stale, partial.FallbackReason = true, sourceErr.Error()
	var cached *cockpit.Temperature
	facts := cockpit.CollectFacts(context.Background(), cockpit.PhaseIntraday, cockpit.Feed{
		Temperature: func(context.Context) (*cockpit.Temperature, error) {
			var err error
			cached, err = cache.resolve(rows[:20], partial, sourceErr, now.Add(time.Minute))
			return cached, err
		},
	})
	if cached == nil || cached.Value != 0 || !cached.Stale || cached.FetchedAt != fresh.FetchedAt || cached.SampleCount != 1000 {
		t.Fatalf("complete cached facts lost: %+v", cached)
	}
	if !strings.Contains(cached.Warning, fresh.FetchedAt) || !strings.Contains(cached.Warning, sourceErr.Error()) {
		t.Fatalf("cache warning must retain provenance and error: %s", cached.Warning)
	}
	if facts.Temperature == nil || len(facts.Sources) != 1 || facts.Sources[0].OK {
		t.Fatalf("cache must reach AI facts while source failure remains visible: %+v", facts)
	}
	if fresh.Stale || fresh.Warning != "" || cache.latest.Stale {
		t.Fatal("fallback mutated successful snapshot")
	}
	cached.Value = 88
	again, err := cache.resolve(nil, foundation.SourceMeta{}, sourceErr, now.Add(2*time.Minute))
	if !errors.Is(err, sourceErr) || again == nil || again.Value != 0 {
		t.Fatalf("consumer mutated shared cache: %+v, %v", again, err)
	}
	meta.FetchedAt = now.Add(2 * time.Minute)
	for i := range rows {
		rows[i].Price = 100
	}
	recovered, err := cache.resolve(rows, meta, nil, meta.FetchedAt)
	if err != nil || recovered == nil || recovered.Stale || math.Abs(recovered.Value-50) > 1e-9 {
		t.Fatalf("recovered source did not replace cache: %+v, %v", recovered, err)
	}
	again, _ = cache.resolve(nil, foundation.SourceMeta{}, sourceErr, now.Add(150*time.Second))
	if again == nil || math.Abs(again.Value-50) > 1e-9 || again.FetchedAt != recovered.FetchedAt {
		t.Fatalf("fallback did not use latest successful snapshot: %+v", again)
	}
}

func TestCockpitTemperatureCacheRejectsUnusableFallbacks(t *testing.T) {
	base := time.Date(2026, 10, 27, 23, 59, 0, 0, foundation.AStockLocation)
	for _, scenario := range []string{"expired", "previous day", "different model", "future", "bad time", "no successful snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			rows, meta := cockpitTemperatureRows(base)
			var cache cockpitTemperatureCache
			if _, err := cache.resolve(rows, meta, nil, base); err != nil {
				t.Fatal(err)
			}
			now := base.Add(30 * time.Second)
			switch scenario {
			case "expired":
				cache.latest.TradeDate = now.Format("2006-01-02")
				cache.latest.FetchedAt = now.Add(-cockpitTemperatureCacheTTL - time.Nanosecond).Format(time.RFC3339Nano)
			case "previous day":
				now = base.Add(time.Minute)
			case "different model":
				cache.latest.Model = "other-model"
			case "future":
				cache.latest.FetchedAt = now.Add(time.Minute).Format(time.RFC3339)
			case "bad time":
				cache.latest.FetchedAt = "unavailable"
			case "no successful snapshot":
				cache.latest = nil
			}
			sourceErr := errors.New("snapshot unavailable")
			got, err := cache.resolve(nil, foundation.SourceMeta{}, sourceErr, now)
			if got != nil || !errors.Is(err, sourceErr) {
				t.Fatalf("unusable cache was returned: %+v, %v", got, err)
			}
		})
	}
}

func TestCockpitTemperatureCacheBoundsExistingSnapshotOnRefreshError(t *testing.T) {
	base := time.Date(2026, 10, 27, 14, 0, 0, 0, foundation.AStockLocation)
	rows, meta := cockpitTemperatureRows(base)
	for _, age := range []time.Duration{cockpitTemperatureCacheTTL, cockpitTemperatureCacheTTL + time.Second} {
		var cache cockpitTemperatureCache
		got, err := cache.resolve(rows, meta, errors.New("refresh failed"), base.Add(age))
		if err == nil || (got != nil) != (age <= cockpitTemperatureCacheTTL) {
			t.Fatalf("cache age %s: %+v, %v", age, got, err)
		}
		if got != nil && (!got.Stale || got.FetchedAt != meta.FetchedAt.Format(time.RFC3339)) {
			t.Fatalf("original snapshot time lost: %+v", got)
		}
		if got != nil {
			again, _ := cache.resolve(nil, foundation.SourceMeta{}, errors.New("partial snapshot"), base.Add(age))
			if again == nil || again.FetchedAt != got.FetchedAt {
				t.Fatal("validated complete snapshot was not retained for subsequent partial responses")
			}
		}
	}
}

func TestCockpitTemperatureFallbackExpiryBypassesLiveFactsCache(t *testing.T) {
	for _, expired := range []bool{false, true} {
		now := time.Now()
		age := time.Minute
		if expired {
			age = cockpitTemperatureCacheTTL + time.Second
		}
		cached := &cockpit.Temperature{
			Value: 0, Stale: true, Model: cockpit.TemperatureModel, SampleCount: 1000,
			FetchedAt: now.Add(-age).Format(time.RFC3339), TradeDate: now.In(foundation.AStockLocation).Format("2006-01-02"),
		}
		server := &Server{cockpit: &cockpit.Service{}, cockpitLive: cockpitLiveFacts{
			at: now, phase: cockpit.PhaseAt(now).Phase, seeded: true,
			facts: cockpit.Facts{Temperature: cached, Sources: []cockpit.SourceResult{{Name: "市场温度", OK: false}}},
		}}
		recorder := httptest.NewRecorder()
		server.cockpitFacts(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/cockpit/facts", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("expired=%v: %s", expired, recorder.Body.String())
		}
		var result struct {
			Data struct {
				Facts cockpit.Facts `json:"facts"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if (result.Data.Facts.Temperature == nil) != expired {
			t.Fatalf("expired=%v, live cache must respect fallback deadline: %s", expired, recorder.Body.String())
		}
	}
}
