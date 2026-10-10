package review

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExchangeCalendarFetchCacheAndFailClosed(t *testing.T) {
	ctx := context.Background()
	store := scheduleStore(t)
	calls := 0
	broken := false
	incomplete := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if broken {
			w.WriteHeader(503)
			return
		}
		rows := []map[string]string{}
		for i := 1; i <= 31; i++ {
			if incomplete && i == 20 {
				continue
			}
			date := time.Date(2026, 10, i, 0, 0, 0, 0, shanghaiLocation())
			open := "1"
			if i <= 7 || date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
				open = "0"
			}
			rows = append(rows, map[string]string{"jyrq": date.Format("2006-01-02"), "jybz": open})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": rows})
	}))
	defer server.Close()
	now := scheduleDate(t, "2026-10-10 12:00")
	c := NewExchangeCalendar(store, server.Client())
	c.endpoint = server.URL
	c.now = func() time.Time { return now }
	if v, e := c.IsTradingDay(ctx, scheduleDate(t, "2026-10-07 22:00")); e != nil || v {
		t.Fatal("holiday must be closed", e)
	}
	if v, e := c.IsTradingDay(ctx, scheduleDate(t, "2026-10-08 22:00")); e != nil || !v || calls != 1 {
		t.Fatal("cache not reused", e, calls)
	}
	// A new instance reads the persisted month, including non-trading weekends.
	c = NewExchangeCalendar(store, server.Client())
	c.endpoint = server.URL
	c.now = func() time.Time { return now }
	if v, e := c.IsTradingDay(ctx, now); e != nil || v || calls != 1 {
		t.Fatal("restart cache failed")
	}
	broken = true
	now = now.Add(2 * 24 * time.Hour)
	if _, e := c.IsTradingDay(ctx, now); e != nil {
		t.Fatal("recent validated cache should survive outage")
	}
	now = now.Add(8 * 24 * time.Hour)
	if _, e := c.IsTradingDay(ctx, now); e == nil {
		t.Fatal("stale calendar should fail closed")
	}
	broken = false
	incomplete = true
	if _, e := c.IsTradingDay(ctx, now); e == nil {
		t.Fatal("missing day must not mean a closed day")
	}
}
