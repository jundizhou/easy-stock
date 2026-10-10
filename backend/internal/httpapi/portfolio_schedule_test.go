package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/portfolioinspection"
)

func TestPortfolioScheduleAPI(t *testing.T) {
	s := NewServer(Config{PortfolioDBPath: ":memory:", ReviewDBPath: ":memory:", SettingsPath: ""})
	defer s.Close()
	request := `{"portfolio_plan_id":"plan","portfolio_plan_name":"测试","trader_profile":"balanced","holdings":[{"symbol":"600519.SH","weight_percent":50}]}`
	body := `{"enabled":true,"interval":3,"unit":"days","start_date":"2026-10-11","time":"15:30","channels":[],"request":` + request + `}`
	call := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRecorder()
		s.ServeHTTP(r, httptest.NewRequest(method, "/api/v1/portfolio-schedules/plan", strings.NewReader(body)))
		return r
	}
	if r := call("GET", ""); r.Code != 200 || !strings.Contains(r.Body.String(), `"data":null`) {
		t.Fatal(r.Code, r.Body.String())
	}
	r := call("PUT", body)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var result struct{ Data portfolioinspection.Schedule }
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.NextRunAt.IsZero() || result.Data.Request.PortfolioPlanID != "plan" {
		t.Fatal("missing saved schedule")
	}
	for _, invalid := range []string{
		strings.Replace(body, `"interval":3`, `"interval":0`, 1),
		strings.Replace(body, `"days"`, `"hours"`, 1),
		strings.Replace(body, `"plan"`, `"other"`, 1),
		strings.Replace(body, `"channels":[]`, `"channels":["dingtalk"]`, 1),
		strings.Replace(body, `"time":"15:30"`, `"time":"25:30"`, 1),
		strings.Replace(body, `"enabled":true`, `"enabled":true,"next_run_at":"2026-10-10T00:00:00Z"`, 1),
		body + `{}`,
	} {
		if r := call("PUT", invalid); r.Code != 400 {
			t.Fatalf("accepted invalid request: %s: %d %s", invalid, r.Code, r.Body.String())
		}
	}
	if r := call("GET", ""); r.Code != 200 || !strings.Contains(r.Body.String(), `"interval":3`) {
		t.Fatal("invalid update modified stored schedule")
	}
	if r := call("DELETE", ""); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := call("GET", ""); !strings.Contains(r.Body.String(), `"data":null`) {
		t.Fatal("delete failed")
	}
}

// A second connection represents another task/process writing the same file.
// A sustained lock must report an unsaved setting, without replacing the plan.
func TestPortfolioScheduleBusyIsRetryableAndPreservesSavedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portfolio.db")
	s := NewServer(Config{PortfolioDBPath: path})
	defer s.Close()
	body := `{"enabled":false,"interval":3,"unit":"days","start_date":"2026-10-11","time":"15:30","channels":[],"request":{"portfolio_plan_id":"plan"}}`
	call := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("PUT", "/api/v1/portfolio-schedules/plan", strings.NewReader(body)))
		return w
	}
	if w := call(body); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE portfolio_inspection_schedules SET content_json=content_json WHERE plan_id='plan'`); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	w := call(strings.Replace(body, `"interval":3`, `"interval":7`, 1))
	if w.Code != 503 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "尚未保存") || strings.Contains(w.Body.String(), "SQLITE_BUSY") {
		t.Fatal(w.Code, w.Body.String())
	}
	if time.Since(started) < time.Second || time.Since(started) > 10*time.Second {
		t.Fatal("busy wait was not bounded", time.Since(started))
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	saved, err := s.portfolioStore.GetSchedule(context.Background(), "plan")
	if err != nil || saved.Interval != 3 {
		t.Fatal("failed save changed settings", saved.Interval, err)
	}
	if w = call(strings.Replace(body, `"interval":3`, `"interval":7`, 1)); w.Code != 200 {
		t.Fatal("save did not recover", w.Code, w.Body.String())
	}
}
