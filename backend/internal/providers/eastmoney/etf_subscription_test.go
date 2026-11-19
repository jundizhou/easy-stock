package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/nationalteam"
)

func TestSubscriptionPairsNullAndUnits(t *testing.T) {
	var pairs [][]json.RawMessage
	json.Unmarshal([]byte(`[["2026-11-16",null],["2026-11-13",100],["2026-11-12",-1],["bad",999]]`), &pairs)
	result := subscriptionPairs(pairs, true)
	if len(result) != 1 || result["2026-11-13"] != 100 {
		t.Fatalf("null treated as zero or units changed: %+v", result)
	}
}
func TestSubscriptionProviderVerifiesBothExchangesAndNeverUsesLivePriceForHistory(t *testing.T) {
	var files atomic.Int32
	var newDay atomic.Bool
	now := time.Date(2026, 11, 17, 17, 0, 0, 0, nationalteam.China)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/meta.json":
			fmt.Fprint(w, `{"trade_date":"2026-11-16","generated_at_iso":"2026-11-17T16:00:00+08:00","report_date":"2025-12-31"}`)
		case strings.HasPrefix(r.URL.Path, "/industry/"):
			if strings.HasSuffix(r.URL.Path, "/2025.json") {
				fmt.Fprint(w, `{"dates":["2025-11-14","2025-11-17"]}`)
				return
			}
			fmt.Fprint(w, `{"dates":["2026-10-16","2026-10-19","2026-11-13","2026-11-16"]}`)
		case strings.HasPrefix(r.URL.Path, "/series/"):
			files.Add(1)
			if strings.HasSuffix(r.URL.Path, "/2025.json") {
				fmt.Fprint(w, `{"shares":[["2025-11-14",800],["2025-11-17",900]],"prices":[["2025-11-17",2]]}`)
				return
			}
			fmt.Fprint(w, `{"shares":[["2026-10-16",1000],["2026-10-19",1100],["2026-11-13",1200],["2026-11-16",1300]],"prices":[["2026-10-19",5],["2026-11-13",3],["2026-11-16",2]]}`)
		case r.URL.Path == "/api/qt/ulist.np/get":
			if len(strings.Split(r.URL.Query().Get("secids"), ",")) != 22 {
				t.Error("wrong sample pool")
			}
			rows := []map[string]any{}
			for _, item := range nationalteam.SubscriptionCatalog() {
				rows = append(rows, map[string]any{"f12": item.Code, "f2": 99, "f3": 1, "f124": now.Unix()})
			}
			json.NewEncoder(w).Encode(map[string]any{"rc": 0, "data": map[string]any{"diff": rows}})
		case r.URL.Path == "/api/qt/stock/kline/get":
			secid := strings.Split(r.URL.Query().Get("secid"), ".")
			market := 1
			if secid[0] == "0" {
				market = 0
			}
			json.NewEncoder(w).Encode(map[string]any{"rc": 0, "data": map[string]any{"code": secid[1], "market": market, "klines": []string{"2026-11-17,5"}}})
		case r.URL.Path == "/sse":
			day := r.URL.Query().Get("STAT_DATE")
			rows := []map[string]any{}
			if day == "2026-11-16" || day == "2026-11-13" || (day == "2026-11-17" && newDay.Load()) {
				for _, item := range nationalteam.SubscriptionCatalog() {
					if item.Exchange != "SSE" {
						continue
					}
					shares := .13
					if day == "2026-11-17" {
						shares = .15
					}
					if day == "2026-11-13" {
						shares = .12
					}
					if day == "2026-11-16" && item.Code == "510300" {
						shares = .14
					}
					rows = append(rows, map[string]any{"SEC_CODE": item.Code, "STAT_DATE": day, "TOT_VOL": shares})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"result": rows})
		case r.URL.Path == "/szse":
			if r.URL.Query().Get("CATALOGID") != "fund_jjgm" {
				t.Error("wrong SZSE report")
			}
			code := r.URL.Query().Get("txtDm")
			rows := []any{map[string]any{"fund_code": code, "size_date": "2026-11-16", "current_size": "0.13"}, map[string]any{"fund_code": code, "size_date": "2026-11-13", "current_size": "0.12"}, map[string]any{"fund_code": "other", "size_date": "2026-11-17", "current_size": "999"}}
			if newDay.Load() {
				rows = append(rows, map[string]any{"fund_code": code, "size_date": "2026-11-17", "current_size": "0.15"})
			}
			json.NewEncoder(w).Encode([]any{map[string]any{"metadata": map[string]any{"pagecount": 1}, "data": rows}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	client := NewClient(WithETFSubscriptionSources(upstream.URL, upstream.URL+"/sse", upstream.URL+"/szse"), WithBaseURL(upstream.URL), WithQuoteBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	result, meta, err := client.etfSubscriptionsAt(context.Background(), now)
	if err != nil || result.Expected != 22 || result.LatestCompleteDate != "2026-11-16" || result.ReportDate != "2025-12-31" || meta.Stale {
		t.Fatalf("bad source result %+v %+v %v", result, meta, err)
	}
	if files.Load() != 44 {
		t.Fatalf("source files=%d", files.Load())
	}
	first := result.Days[0]
	if result.StartDate != "2025-11-17" || first.Date != "2025-11-17" || first.PreviousDate != "2025-11-14" || first.EstimatedNet == nil || *first.EstimatedNet != 4400 {
		t.Fatalf("year history omitted or lost the first day's baseline: start=%s first=%+v", result.StartDate, first)
	}
	if result.TodayTrial == nil || result.TodayTrial.ShareCoverage != 0 || result.TodayTrial.EstimatedNet != nil || result.TodayTrial.PriceCoverage != 22 {
		t.Fatalf("today's quote must not make yesterday's share total into a same-day estimate: %+v", result.TodayTrial)
	}

	latest := result.Days[len(result.Days)-1]
	if latest.EstimatedNet == nil || *latest.EstimatedNet != 4600 {
		t.Fatalf("wrong price, share units or amendment %+v", latest)
	}
	point := result.Series[0].Points[len(result.Series[0].Points)-1]
	if *point.Close != 2 || *result.Series[0].LatestPrice != 99 || !strings.Contains(point.Verification, "修订") || *point.Shares != 1400 {
		t.Fatalf("live price replaced historical close or correction ignored %+v", point)
	}
	client.etfSubscriptionsAt(context.Background(), now)
	if files.Load() != 44 {
		t.Fatalf("unchanged snapshot refetched historical files: %d", files.Load())
	}
	newDay.Store(true)
	updated, _, err := client.etfSubscriptionsAt(context.Background(), now)
	last := updated.Days[len(updated.Days)-1]
	if err != nil || updated.SourceDate != "2026-11-16" || updated.LatestCompleteDate != "2026-11-17" || last.EstimatedNet == nil || *last.EstimatedNet != 21500 {
		t.Fatalf("new exchange disclosure was not synchronized independently of the public snapshot: day=%+v err=%v", last, err)
	}
	if updated.TodayTrial == nil || updated.TodayTrial.EstimatedNet == nil || *updated.TodayTrial.EstimatedNet != 425700 {
		t.Fatalf("published-day trial unavailable: %+v", updated.TodayTrial)
	}
	now = time.Date(2026, 11, 17, 10, 30, 0, 0, nationalteam.China)
	intraday, _, err := client.etfSubscriptionsAt(context.Background(), now)
	if err != nil || intraday.TodayTrial == nil || intraday.TodayTrial.EstimatedNet == nil || *intraday.TodayTrial.EstimatedNet != 425700 || intraday.LatestCompleteDate != "2026-11-16" {
		t.Fatalf("intraday trial must work before close while historical closing-price totals remain unchanged: trial=%+v complete=%s err=%v", intraday.TodayTrial, intraday.LatestCompleteDate, err)
	}

}
