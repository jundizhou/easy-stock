package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/appsettings"
)

func newArchiveConfigServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	settingsPath := filepath.Join(root, "settings.json")
	store, err := appsettings.Open(settingsPath)
	if err != nil {
		t.Fatalf("open settings store: %v", err)
	}
	srv := NewServer(Config{
		ReviewDBPath:     ":memory:",
		PortfolioDBPath:  ":memory:",
		SettingsStore:    store,
		SettingsPath:     "",
		ArchiveDir:       root,
		Realtime:         stubRealtime{},
		News:             stubNews{},
		ArchiveCollector: ArchiveCollectorConfig{Symbols: []string{"600519.SH"}, NewsLimit: 5, TradingOnly: false},
	})
	return srv, settingsPath
}

func TestArchiveConfigGetReturnsDefaults(t *testing.T) {
	srv, _ := newArchiveConfigServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/archive/config", nil)
	rec := httptest.NewRecorder()
	srv.archiveConfigGet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data archiveConfigView `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if payload.Data.IntervalSeconds != appsettings.DefaultArchiveIntervalSeconds {
		t.Errorf("interval = %d, want default %d", payload.Data.IntervalSeconds, appsettings.DefaultArchiveIntervalSeconds)
	}
	if payload.Data.SymbolsSource != appsettings.ArchiveSymbolsSourceManual {
		t.Errorf("symbols_source = %q, want manual", payload.Data.SymbolsSource)
	}
	if len(payload.Data.ResolvedSymbols) == 0 {
		t.Errorf("resolved_symbols should not be empty")
	}
}

func TestArchiveConfigUpdatePersistsToSettingsFile(t *testing.T) {
	srv, settingsPath := newArchiveConfigServer(t)
	body := `{"enabled":true,"interval_seconds":15,"dir":"D:/quant_archive_test","symbols_source":"manual","symbols":["600519.SH","000001.SZ"],"trading_only":false,"news_limit":33}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/archive/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.archiveConfigUpdate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	// 重新打开磁盘上的 settings.json，确认配置确实落盘。
	reopened, err := appsettings.Open(settingsPath)
	if err != nil {
		t.Fatalf("reopen settings: %v", err)
	}
	saved := appsettings.NormalizeArchive(reopened.Snapshot().Archive)
	if !saved.Enabled {
		t.Errorf("enabled not persisted")
	}
	if saved.IntervalSeconds != 15 {
		t.Errorf("interval = %d, want 15", saved.IntervalSeconds)
	}
	if saved.Dir != "D:/quant_archive_test" {
		t.Errorf("dir = %q", saved.Dir)
	}
	if len(saved.Symbols) != 2 || saved.Symbols[0] != "600519.SH" || saved.Symbols[1] != "000001.SZ" {
		t.Errorf("symbols = %v", saved.Symbols)
	}
	if saved.NewsLimit != 33 {
		t.Errorf("news_limit = %d, want 33", saved.NewsLimit)
	}
	if saved.TradingOnly {
		t.Errorf("trading_only should be false")
	}
}

func TestArchiveConfigUpdateRejectsUnknownFields(t *testing.T) {
	srv, _ := newArchiveConfigServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/archive/config", strings.NewReader(`{"unknown_field":1}`))
	rec := httptest.NewRecorder()
	srv.archiveConfigUpdate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
}

func TestArchiveConfigUpdateNormalizesInvalidValues(t *testing.T) {
	srv, _ := newArchiveConfigServer(t)
	// 周期过小、条数过大、清单来源非法、清单重复 —— 都应在规范化里被纠正。
	body := `{"interval_seconds":1,"news_limit":9999,"symbols_source":"bogus","symbols":["600519.SH","600519.SH"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/archive/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.archiveConfigUpdate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data archiveConfigView `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Data.IntervalSeconds < appsettings.MinArchiveIntervalSeconds {
		t.Errorf("interval = %d, want >= %d", payload.Data.IntervalSeconds, appsettings.MinArchiveIntervalSeconds)
	}
	if payload.Data.SymbolsSource != appsettings.ArchiveSymbolsSourceManual {
		t.Errorf("symbols_source = %q, want manual fallback", payload.Data.SymbolsSource)
	}
	seen := map[string]int{}
	for _, s := range payload.Data.Symbols {
		seen[s]++
	}
	if seen["600519.SH"] != 1 {
		t.Errorf("symbols should be de-duplicated, got %v", payload.Data.Symbols)
	}
}

func TestArchiveConfigUpdatePartialPatchKeepsOtherFields(t *testing.T) {
	srv, _ := newArchiveConfigServer(t)
	// 第一次把 interval 改为 20；第二次只改 dir，interval 必须保留。
	first := httptest.NewRequest(http.MethodPut, "/api/v1/archive/config", strings.NewReader(`{"interval_seconds":20}`))
	srv.archiveConfigUpdate(httptest.NewRecorder(), first)
	second := httptest.NewRequest(http.MethodPut, "/api/v1/archive/config", strings.NewReader(`{"dir":"D:/only_dir"}`))
	rec := httptest.NewRecorder()
	srv.archiveConfigUpdate(rec, second)

	var payload struct {
		Data archiveConfigView `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Data.IntervalSeconds != 20 {
		t.Errorf("interval = %d, want 20 (partial patch must keep previous)", payload.Data.IntervalSeconds)
	}
	if payload.Data.Dir != "D:/only_dir" {
		t.Errorf("dir = %q", payload.Data.Dir)
	}
}

func TestResolveArchiveSymbolsFallsBackToDefaults(t *testing.T) {
	srv, _ := newArchiveConfigServer(t)
	cfg := appsettings.Archive{SymbolsSource: appsettings.ArchiveSymbolsSourceHoldings}
	got := srv.resolveArchiveSymbols(cfg)
	if len(got) != len(DefaultArchiveSymbols) {
		t.Errorf("holdings source without holdings/manual list should fall back to defaults, got %v", got)
	}
}
