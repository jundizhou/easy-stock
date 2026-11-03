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
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
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
	quoteTime := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	items := []foundation.MarketIndexSnapshot{
		{Name: "上证指数", Market: "CN"},
		{Name: "恒生指数", Market: "HK"},
		{Name: "道琼斯", Code: ".DJI", Market: "US", Status: "closed", TradeTime: quoteTime, Price: 12345, ChangePercent: -1.25},
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
	if overseas[1].Market != "US" || overseas[1].Status != "closed" || overseas[1].TradeTime != "2026-10-27 16:00" || overseas[1].Price != 12345 || overseas[1].ChangePercent != -1.25 {
		t.Fatalf("overseas quote provenance or values changed: %+v", overseas[1])
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

func TestCockpitBriefReadsRevalidateHistoricalNarrativesWithoutChangingStore(t *testing.T) {
	at := time.Date(2026, 10, 28, 11, 10, 0, 0, foundation.AStockLocation)
	makeBrief := func(id string, started time.Time, temperature float64) cockpit.Brief {
		return cockpit.Brief{
			ID: id, Phase: cockpit.PhaseIntraday, SessionDate: "2026-10-28", Status: "ready",
			NarrativeStatus: "ready", StartedAt: started,
			Facts: cockpit.Facts{Temperature: &cockpit.Temperature{Value: temperature, UpPercent: 70.47468977742761}},
		}
	}
	prior := makeBrief("prior", at.Add(-30*time.Minute), 71.65396185034605)
	older := makeBrief("older", at.Add(-time.Hour), 90.12)
	future := makeBrief("future", at.Add(time.Hour), 93.21)
	failed := makeBrief("failed", at.Add(-time.Minute), 88.12)
	failed.Status = "failed"
	otherPhase := makeBrief("pre-open", at.Add(-2*time.Minute), 77.12)
	otherPhase.Phase = cockpit.PhasePreOpen
	otherDate := makeBrief("other-date", at.Add(-3*time.Minute), 76.12)
	otherDate.SessionDate = "2026-10-27"
	history := []cockpit.Brief{prior, older, future, failed, otherPhase, otherDate}

	for _, tc := range []struct {
		name        string
		narrative   string
		missing     bool
		stale       bool
		preOpen     bool
		unavailable bool
		original    []string
		want        []string
	}{
		{name: "missing current temperature cannot reuse earlier cache", missing: true,
			narrative: "数据缺口：市场温度刷新失败，目前沿用缓存快照（71.654 度、上涨占比 70.4747%）。", want: []string{"71.654", "70.4747%"}},
		{name: "explicit comparison uses the nearest earlier record outside the result page",
			narrative: "市场状态：本次市场温度 81.35 度。\n较上次变化：上次市场温度 71.654 度。"},
		{name: "future record cannot validate historical comparison", narrative: "较上次变化：上次市场温度 93.21 度。", want: []string{"93.21"}},
		{name: "older record cannot replace actual predecessor", narrative: "较上次变化：上次市场温度 90.12 度。", want: []string{"90.12"}},
		{name: "failed record is not a predecessor", narrative: "较上次变化：上次市场温度 88.12 度。", want: []string{"88.12"}},
		{name: "another phase is not a predecessor", narrative: "较上次变化：上次市场温度 77.12 度。", want: []string{"77.12"}},
		{name: "another session is not a predecessor", narrative: "较上次变化：上次市场温度 76.12 度。", want: []string{"76.12"}},
		{name: "pre-open does not acquire an intraday comparison", preOpen: true, narrative: "上次市场温度 71.654 度。", want: []string{"71.654"}},
		{name: "valid cache saved with current facts stays verifiable", stale: true, narrative: "数据缺口：本次市场温度 81.35 度，刷新失败但使用有效缓存。"},
		{name: "preserves original flags without duplicates or shared slice writes", missing: true,
			original: []string{"71.654", "legacy-check"}, narrative: "数据缺口：目前沿用缓存 71.654 度、上涨占比 70.4747%。", want: []string{"71.654", "legacy-check", "70.4747%"}},
		{name: "corrects saved neutral baseline false positive only",
			original: []string{"50", "legacy-check"}, narrative: "温度读数是本地统计，50 度为中性，本次温度 81.35 度。若温度回落至 50 度中性以下，则转弱。", want: []string{"legacy-check"}},
		{name: "neutral definition cannot clear a saved false current reading", missing: true,
			original: []string{"50"}, narrative: "50 度为中性。当前市场温度 50 度。", want: []string{"50"}},
		{name: "corrected baseline still reports new unsupported numbers",
			original: []string{"50"}, narrative: "50 度为中性。当前市场温度 99.99 度。", want: []string{"99.99"}},
		{name: "corrects saved scale false positive only",
			original: []string{"100", "legacy-check"}, narrative: "市场温度本次读数 81.35（0–100 度，50 度为中性）。", want: []string{"legacy-check"}},
		{name: "corrects saved pre-open scale false positive", preOpen: true,
			original: []string{"100"}, narrative: "市场温度本次读数 81.35（0–100 度，50 度为中性）。"},
		{name: "fixed scale does not require a current reading", missing: true,
			original: []string{"100"}, narrative: "市场温度量程为 0–100 度，50 度为中性。"},
		{name: "scale cannot clear a saved unsupported reading",
			original: []string{"100"}, narrative: "市场温度量程为 0–100 度，当前读数 100 度。", want: []string{"100"}},
		{name: "scale cannot clear an earlier unsupported reading",
			original: []string{"100"}, narrative: "当前市场温度 100 度，市场温度量程为 0–100 度。", want: []string{"100"}},
		{name: "scale cannot clear a saved unrelated reading",
			original: []string{"100"}, narrative: "市场温度 81.35 度。设备温度为 0–100 度。", want: []string{"100"}},
		{name: "corrected scale still reports new unsupported numbers",
			original: []string{"100"}, narrative: "市场温度量程为 0–100 度，当前读数 99.99 度。", want: []string{"99.99"}},
		{name: "corrects a saved decimal scale endpoint",
			original: []string{"100.0"}, narrative: "市场温度量程为 0–100.0 度，本次 81.35 度。"},
		{name: "corrects ASCII scale separator mistaken for a sign",
			original: []string{"-100"}, narrative: "市场温度量程为 0-100 度，本次 81.35 度。"},
		{name: "unfinished narrative retains its existing status", missing: true, unavailable: true, narrative: "温度 99.99 度。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storePath := filepath.Join(t.TempDir(), "briefs.json")
			store, err := cockpit.OpenStore(storePath)
			if err != nil {
				t.Fatal(err)
			}
			for _, brief := range history {
				if err := store.Put(brief); err != nil {
					t.Fatal(err)
				}
			}
			current := makeBrief("selected", at, 81.35)
			current.Narrative = tc.narrative
			current.UnverifiedNumbers = append(make([]string, 0, 8), tc.original...)
			if tc.missing {
				current.Facts.Temperature = nil
			} else if tc.stale {
				current.Facts.Temperature.Stale = true
				current.Facts.Temperature.FetchedAt = at.Add(-time.Minute).Format(time.RFC3339)
			}
			if tc.preOpen {
				current.Phase = cockpit.PhasePreOpen
			}
			if tc.unavailable {
				current.NarrativeStatus = "unavailable"
			}
			if err := store.Put(current); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(storePath)
			if err != nil {
				t.Fatal(err)
			}
			settings, err := appsettings.Open(filepath.Join(t.TempDir(), "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			server := &Server{cockpit: cockpit.NewService(store, cockpit.Feed{}, nil, nil, settings)}
			for _, single := range []bool{false, true} {
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodGet, "/api/v1/cockpit/briefs?limit=1", nil)
				if single {
					request.SetPathValue("id", current.ID)
					server.cockpitBrief(response, request)
				} else {
					server.cockpitBriefs(response, request)
				}
				if response.Code != http.StatusOK {
					t.Fatalf("single=%v: %s", single, response.Body.String())
				}
				var payload struct {
					Data json.RawMessage `json:"data"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
					t.Fatal(err)
				}
				var got cockpit.Brief
				if single {
					err = json.Unmarshal(payload.Data, &got)
				} else {
					var list []cockpit.Brief
					err = json.Unmarshal(payload.Data, &list)
					if len(list) != 1 {
						t.Fatalf("limit=1 returned %d records", len(list))
					}
					got = list[0]
				}
				if err != nil {
					t.Fatal(err)
				}
				if got.ID != current.ID || !slices.Equal(got.UnverifiedNumbers, tc.want) {
					t.Fatalf("single=%v: id=%s numbers=%v want=%v", single, got.ID, got.UnverifiedNumbers, tc.want)
				}
				if got.Narrative != current.Narrative || got.NarrativeStatus != current.NarrativeStatus {
					t.Fatal("read rewrote the saved narrative or generation status")
				}
				gotFacts, _ := json.Marshal(got.Facts)
				wantFacts, _ := json.Marshal(current.Facts)
				if string(gotFacts) != string(wantFacts) {
					t.Fatal("read changed the collected facts")
				}
			}
			after, err := os.ReadFile(storePath)
			if err != nil || string(before) != string(after) {
				t.Fatalf("read changed stored history: %v", err)
			}
			stored, _ := store.Get(current.ID)
			if !slices.Equal(stored.UnverifiedNumbers, tc.original) || !slices.Equal(current.UnverifiedNumbers[:cap(current.UnverifiedNumbers)][len(tc.original):], make([]string, cap(current.UnverifiedNumbers)-len(tc.original))) {
				t.Fatal("read mutated the in-memory store or its shared slice")
			}
		})
	}
}

type cockpitIndustryFlowFixture struct {
	fakeSectorFlowProvider
	items     []foundation.SectorFlowRank
	err       error
	dimension string
	limit     int
}

func (f *cockpitIndustryFlowFixture) SectorFlowRanking(_ context.Context, dimension string, limit int) ([]foundation.SectorFlowRank, foundation.SourceMeta, error) {
	f.dimension, f.limit = dimension, limit
	return f.items, foundation.SourceMeta{Source: "test:main-fund-flow"}, f.err
}

func (f *cockpitIndustryFlowFixture) MarketFundFlows(context.Context, string, string, int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, errors.New("must not mix total fund flow with main fund flow")
}

func TestCockpitIndustryFlowKeepsBothEndsOfMainFundRanking(t *testing.T) {
	// More than 200 positive boards must not exclude the negative tail.
	provider := &cockpitIndustryFlowFixture{}
	for i := range 212 {
		provider.items = append(provider.items, foundation.SectorFlowRank{Name: "测试流入", MainNetInflow: float64(300-i) * 1e6, ChangePercent: 1.25})
	}
	provider.items = append(provider.items,
		foundation.SectorFlowRank{Name: "测试流出甲", MainNetInflow: -400e6, ChangePercent: -0.5},
		foundation.SectorFlowRank{Name: "测试流出乙", MainNetInflow: -900e6, ChangePercent: -1.5},
	)
	server := &Server{marketOverview: provider}
	items, err := server.cockpitFeed().Industry(context.Background())
	if err != nil || provider.dimension != "industry" || provider.limit != 600 || len(items) != 214 {
		t.Fatalf("industry feed lost the full main-fund ranking: count=%d dimension=%s limit=%d err=%v", len(items), provider.dimension, provider.limit, err)
	}
	board := cockpit.BuildFlowBoard(items, 8)
	if board == nil || len(board.Inflow) != 8 || len(board.Outflow) != 2 {
		t.Fatalf("both flow directions must remain available: %+v", board)
	}
	if board.Inflow[0].NetInflow != 300e6 || board.Outflow[0].Name != "测试流出乙" || board.Outflow[0].NetInflow != -900e6 || board.Outflow[0].ChangePercent != -1.5 {
		t.Fatalf("main-fund values, signs, or ranking changed: %+v", board)
	}
	provider.items = provider.items[:212]
	items, err = server.cockpitFeed().Industry(context.Background())
	board = cockpit.BuildFlowBoard(items, 8)
	if err != nil || board == nil || len(board.Outflow) != 0 {
		t.Fatalf("an all-positive source must not fabricate outflows: %+v, %v", board, err)
	}
}

func TestCockpitIndustryFlowFailureDoesNotMixSources(t *testing.T) {
	provider := &cockpitIndustryFlowFixture{err: errors.New("main-fund source unavailable")}
	server := &Server{marketOverview: provider}
	items, err := server.cockpitFeed().Industry(context.Background())
	if items != nil || !errors.Is(err, provider.err) {
		t.Fatalf("source failure must stay explicit: %+v, %v", items, err)
	}
}

type cockpitNewsFixture struct {
	items []foundation.NewsItem
	err   error
	limit int
}

func (f *cockpitNewsFixture) LatestNews(_ context.Context, limit int) ([]foundation.NewsItem, error) {
	f.limit = limit
	return f.items, f.err
}

func TestCockpitNewsFeedPreservesOriginalEvidenceAndLocalPublishDate(t *testing.T) {
	provider := &cockpitNewsFixture{items: []foundation.NewsItem{
		{Title: "测试事件", Content: "测试新闻原文", PublishedAt: time.Date(2026, 10, 27, 23, 40, 0, 0, time.UTC), URL: "https://example.invalid/news/1", Tags: []string{"测试题材"}, Meta: foundation.SourceMeta{Source: "test:news"}},
		{Title: "日期未知的测试新闻"},
	}}
	server := &Server{newsProvider: provider}
	lines, err := server.cockpitFeed().News(context.Background())
	if err != nil || len(lines) != 2 || provider.limit != 50 {
		t.Fatalf("news feed failed or fabricated coverage: %+v, %v, limit %d", lines, err, provider.limit)
	}
	first := lines[0]
	if first.PublishedAt != "07:40" || first.PublishedDate != "2026-10-28" || first.Content != provider.items[0].Content || first.Source != "test:news" || first.URL != provider.items[0].URL || first.Tags[0] != "测试题材" {
		t.Fatalf("news provenance lost: %+v", first)
	}
	if lines[1].PublishedAt != "" || lines[1].PublishedDate != "" || lines[1].Source != "" || lines[1].Content != "" {
		t.Fatalf("unknown news provenance must stay unknown: %+v", lines[1])
	}
	first.Tags[0] = "consumer edit"
	if provider.items[0].Tags[0] != "测试题材" {
		t.Fatal("news conversion must not mutate provider tags")
	}
	provider.err = errors.New("news unavailable")
	lines, err = server.cockpitFeed().News(context.Background())
	if !errors.Is(err, provider.err) || lines != nil {
		t.Fatalf("source failures must remain explicit: %+v, %v", lines, err)
	}
}
