package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

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
