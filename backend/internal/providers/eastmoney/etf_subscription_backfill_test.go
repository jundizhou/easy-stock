package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/nationalteam"
)

func TestSubscriptionBackfillsYesterdayWhenSnapshotLagsTwoDays(t *testing.T) {
	now := time.Date(2026, 11, 18, 13, 50, 0, 0, nationalteam.China)
	var mu sync.Mutex
	queried := map[string]int{}
	var closeRequests atomic.Int32
	var allowMissingClose atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/meta.json":
			fmt.Fprint(w, `{"trade_date":"2026-11-16","generated_at_iso":"2026-11-17T17:22:40+08:00"}`)
		case strings.HasPrefix(r.URL.Path, "/industry/"):
			fmt.Fprint(w, `{"dates":["2026-11-13","2026-11-16"]}`)
		case strings.HasPrefix(r.URL.Path, "/series/"):
			fmt.Fprint(w, `{"shares":[["2026-11-13",100],["2026-11-16",120]],"prices":[["2026-11-16",2]]}`)
		case r.URL.Path == "/sse":
			day := q.Get("STAT_DATE")
			mu.Lock()
			queried[day]++
			mu.Unlock()
			rows := []any{}
			for _, item := range nationalteam.SubscriptionCatalog() {
				if item.Exchange != "SSE" {
					continue
				}
				value := map[string]float64{"2026-11-13": .01, "2026-11-16": .012, "2026-11-17": .015}[day]
				if value > 0 {
					rows = append(rows, map[string]any{"SEC_CODE": item.Code, "STAT_DATE": day, "TOT_VOL": value})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"result": rows})
		case r.URL.Path == "/szse":
			rows := []any{}
			for day, value := range map[string]string{"2026-11-13": ".01", "2026-11-16": ".012", "2026-11-17": ".015"} {
				rows = append(rows, map[string]any{"fund_code": q.Get("txtDm"), "size_date": day, "current_size": value})
			}
			json.NewEncoder(w).Encode([]any{map[string]any{"metadata": map[string]any{"pagecount": 1}, "data": rows}})
		case r.URL.Path == "/api/qt/ulist.np/get":
			rows := []any{}
			for _, item := range nationalteam.SubscriptionCatalog() {
				rows = append(rows, map[string]any{"f12": item.Code, "f2": 999, "f124": now.Unix()})
			}
			json.NewEncoder(w).Encode(map[string]any{"rc": 0, "data": map[string]any{"diff": rows}})
		case r.URL.Path == "/api/qt/stock/kline/get":
			closeRequests.Add(1)
			if q.Get("lmt") != "400" {
				t.Errorf("daily close limit must cover a full year: %s", r.URL)
			}
			if q.Get("fqt") != "0" || q.Get("klt") != "101" || q.Get("beg") != "20261117" || q.Get("end") != "20261117" || q.Get("fields2") != "f51,f53" {
				t.Errorf("incorrect historical close request: %s", r.URL)
			}
			parts := strings.Split(q.Get("secid"), ".")
			market := 1
			if parts[0] == "0" {
				market = 0
			}
			lines := []string{"2026-11-18,999", "2026-11-17,4"}
			if parts[1] == "510300" && !allowMissingClose.Load() {
				lines = []string{"2026-11-18,999"}
			}
			json.NewEncoder(w).Encode(map[string]any{"rc": 0, "data": map[string]any{"code": parts[1], "market": market, "klines": lines}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	c := NewClient(WithETFSubscriptionSources(upstream.URL, upstream.URL+"/sse", upstream.URL+"/szse"), WithBaseURL(upstream.URL), WithQuoteBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	partial, _, err := c.etfSubscriptionsAt(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	last := partial.Days[len(partial.Days)-1]
	if last.Date != "2026-11-17" || last.Coverage != 21 || last.EstimatedNet != nil || partial.LatestCompleteDate != "2026-11-16" {
		t.Fatalf("partial price coverage misrepresented as complete: %+v", last)
	}
	if partial.Series[0].Points[1].Close != nil {
		t.Fatal("today's quote or K-line was used for yesterday")
	}
	if queried["2026-11-17"] == 0 {
		t.Fatal("skipped yesterday between source date and today")
	}
	if closeRequests.Load() != 22 {
		t.Fatalf("backfill requests=%d", closeRequests.Load())
	}
	allowMissingClose.Store(true)
	full, _, err := c.etfSubscriptionsAt(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	last = full.Days[len(full.Days)-1]
	if last.EstimatedNet == nil || *last.EstimatedNet != 2640 || last.Coverage != 22 || last.PreviousDate != "2026-11-16" || full.LatestCompleteDate != "2026-11-17" {
		t.Fatalf("yesterday not backfilled: %+v", last)
	}
	if full.SourceDate != "2026-11-16" || full.TodayTrial.BaselineCoverage != 22 || full.TodayTrial.ShareCoverage != 0 || full.TodayTrial.EstimatedNet != nil {
		t.Fatalf("source or today trial relabelled: %+v", full.TodayTrial)
	}
	if closeRequests.Load() != 23 {
		t.Fatalf("successful historical closes should be cached, missing date retried: %d", closeRequests.Load())
	}
	if *full.Series[0].LatestPrice != 999 || *full.Series[0].Points[1].Close != 4 {
		t.Fatal("latest quote and historical close were not kept separate")
	}
}

func TestSubscriptionDailyCloseRejectsWrongIdentityAndInvalidDates(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantErr    bool
		count      int
	}{
		{"wrong code", `{"rc":0,"data":{"code":"510050","market":1,"klines":["2026-11-17,4"]}}`, true, 0},
		{"wrong market", `{"rc":0,"data":{"code":"510300","market":0,"klines":["2026-11-17,4"]}}`, true, 0},
		{"invalid values", `{"rc":0,"data":{"code":"510300","market":1,"klines":["2026-11-18,999","2026-11-17,NaN","2026-11-17,0","bad,4"]}}`, false, 0},
		{"valid close", `{"rc":0,"data":{"code":"510300","market":1,"klines":["2026-11-17,4.042"]}}`, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer s.Close()
			c := NewClient(WithBaseURL(s.URL), WithHTTPClient(s.Client()))
			prices, err := c.subscriptionDailyCloses(context.Background(), "510300", "SSE", "2026-11-17", "2026-11-17")
			if (err != nil) != tc.wantErr || len(prices) != tc.count {
				t.Fatalf("prices=%v err=%v", prices, err)
			}
		})
	}
}
