package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/monitor"
)

func TestMonitorPreferencesDoNotModifySystemSettings(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	settings, err := appsettings.Open(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Update(func(*appsettings.Values) error { return nil }); err != nil {
		t.Fatal(err)
	}
	config := Config{SettingsPath: settingsPath, SettingsStore: settings, MonitorDBPath: filepath.Join(dir, "monitor.db")}
	server := NewServer(config)
	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/monitor/settings", bytes.NewBufferString(`{"enabled":true,"quote_interval_sec":8,"notify_enabled":true}`))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update failed: %d %s", recorder.Code, recorder.Body.String())
	}
	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("monitor preferences changed the upstream system settings file")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := NewServer(config)
	defer restarted.Close()
	got := restarted.monitorPreferences.Snapshot()
	if !got.Settings.Enabled || got.Settings.QuoteIntervalSec != 8 || !got.NotifyEnabled {
		t.Fatalf("restart lost monitor preferences: %+v", got)
	}
}

func TestPublicPagesDoNotRegisterAITraderRoutes(t *testing.T) {
	server := NewServer(Config{})
	defer server.Close()
	for _, path := range []string{"/api/v1/trading/account", "/api/v1/trading/control", "/api/v1/trading/positions", "/api/v1/trading/plans", "/api/v1/trading/autopilot"} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("excluded route %s returned %d", path, recorder.Code)
		}
	}
	if result, err := server.NotifyMonitorAlert(context.Background(), monitor.Event{Message: "test event"}); err != nil || result != monitor.NotifySkipped {
		t.Fatalf("monitor notifications must default off: %s %v", result, err)
	}
}
