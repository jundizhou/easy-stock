package tencent

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHongKongKLineUsesAdjustedHongKongEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/appstock/app/hkfqkline/get" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("param"); got != "hk00700,day,,,300,qfq" {
			t.Errorf("unexpected param: %s", got)
		}
		fmt.Fprint(w, `{"code":0,"data":{"hk00700":{"qfqday":[["2026-11-18","420.0","431.2","435.0","418.4","29605485",{},"0.45","1408999.287"]],"qt":{}}}}`)
	}))
	client := NewClient(WithKLineBaseURL(server.URL + "/appstock/app/fqkline/get"))
	lines, err := client.HongKongKLine(context.Background(), "00700.HK", "day", 300)
	server.Close()
	if err != nil || len(lines) != 1 {
		t.Fatalf("hk kline: %+v %v", lines, err)
	}
	line := lines[0]
	if line.Symbol != "00700.HK" || line.Close != 431.2 || line.Open != 420 || line.High != 435 || line.Low != 418.4 || line.Volume != 29605485 || line.TurnoverRate != .45 || math.Abs(line.Amount-14089992870) > .01 {
		t.Fatalf("unexpected bar: %+v", line)
	}
	if line.Meta.Source != "tencent:hk-qfq-kline" || !strings.Contains(line.Meta.SourceURL, "/hkfqkline/get?") {
		t.Fatalf("unexpected meta: %+v", line.Meta)
	}
}

func TestHongKongKLinePeriodSeriesRequiresAdjustedBars(t *testing.T) {
	payload := `{"code":0,"data":{"hk00700":{"qfqweek":[["2026-11-14","400","410","420","390","100"]],"month":[["2026-10-31","380","390","400","370","200"]]}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("param"); !strings.HasPrefix(got, "hk00700,week,") && !strings.HasPrefix(got, "hk00700,month,") {
			t.Errorf("unexpected param: %s", got)
		}
		fmt.Fprint(w, payload)
	}))
	client := NewClient(WithKLineBaseURL(server.URL))
	defer server.Close()
	if bars, err := client.HongKongKLine(context.Background(), "hk00700", "month", 10); err == nil || len(bars) > 0 {
		t.Fatal("unadjusted bars reported as adjusted", bars)
	}
	lines, err := client.HongKongKLine(context.Background(), "hk00700", "week", 10)
	if err != nil || len(lines) != 1 || lines[0].Close != 410 {
		t.Fatalf("hk week kline: %+v %v", lines, err)
	}
}

func TestHongKongKLineRejectsAShareSymbol(t *testing.T) {
	client := NewClient()
	if _, err := client.HongKongKLine(context.Background(), "600519.SH", "day", 10); err == nil {
		t.Fatal("a-share symbol accepted by hong kong kline")
	}
}
