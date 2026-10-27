package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"easy-stock/backend/internal/monitor"
)

func TestMonitorEventsAscendingCursorDoesNotSkipBurst(t *testing.T) {
	store, err := monitor.OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 0; i < 26; i++ {
		if _, err := store.AppendEvent(context.Background(), monitor.Event{RuleID: "fixture", Severity: monitor.SeverityWarning}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{monitor: monitor.NewEngine(monitor.EngineConfig{Store: store})}
	read := func(query string) ([]monitor.Event, int64, int) {
		t.Helper()
		w := httptest.NewRecorder()
		s.monitorEventsList(w, httptest.NewRequest(http.MethodGet, "/api/v1/monitor/events?"+query, nil))
		if w.Code != 200 {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var payload struct {
			Items        []monitor.Event `json:"items"`
			SinceID      int64           `json:"since_id"`
			MatchedTotal int             `json:"matched_total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Items, payload.SinceID, payload.MatchedTotal
	}
	items, cursor, total := read("since_id=1&limit=10&order=asc")
	if len(items) != 10 || items[0].ID != 2 || items[9].ID != 11 || cursor != 11 || total != 25 {
		t.Fatalf("first page: items=%+v cursor=%d total=%d", items, cursor, total)
	}
	items, cursor, _ = read("since_id=11&limit=10&order=asc")
	if len(items) != 10 || items[0].ID != 12 || cursor != 21 {
		t.Fatalf("second page: items=%+v cursor=%d", items, cursor)
	}
	items, cursor, _ = read("since_id=21&limit=10&order=asc")
	if len(items) != 5 || items[0].ID != 22 || cursor != 26 {
		t.Fatalf("last page: items=%+v cursor=%d", items, cursor)
	}
	items, cursor, _ = read("limit=1")
	if len(items) != 1 || items[0].ID != 26 || cursor != 26 {
		t.Fatal("default descending baseline changed")
	}
}
