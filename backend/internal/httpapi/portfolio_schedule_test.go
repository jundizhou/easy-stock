package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

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
