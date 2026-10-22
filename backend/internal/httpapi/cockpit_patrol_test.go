package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	server := NewServer(Config{SettingsPath: settingsPath, StrictPersistence: false})
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
