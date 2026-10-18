package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func sectorFlowRankPayloadFor(key string) string {
	switch key {
	case "f62":
		return `{"rc":0,"data":{"total":3,"diff":[
			{"f12":"BK0917","f13":90,"f14":"半导体概念","f62":3981310464},
			{"f12":"BK0490","f13":90,"f14":"军工","f62":3723490560},
			{"f12":"BK1137","f13":90,"f14":"存储芯片","f62":-1000}
		]}}`
	case "f3":
		return `{"rc":0,"data":{"total":3,"diff":[
			{"f12":"BK1137","f13":90,"f14":"存储芯片","f3":-16},
			{"f12":"BK0490","f13":90,"f14":"军工","f3":250},
			{"f12":"BK0917","f13":90,"f14":"半导体概念","f3":-18}
		]}}`
	case "f184":
		return `{"rc":0,"data":{"total":3,"diff":[
			{"f12":"BK0917","f13":90,"f14":"半导体概念","f184":1234},
			{"f12":"BK0490","f13":90,"f14":"军工","f184":900},
			{"f12":"BK1137","f13":90,"f14":"存储芯片","f184":-321}
		]}}`
	case "f66":
		return `{"rc":0,"data":{"total":3,"diff":[
			{"f12":"BK0917","f13":90,"f14":"半导体概念","f66":480172992},
			{"f12":"BK0490","f13":90,"f14":"军工","f66":100},
			{"f12":"BK1137","f13":90,"f14":"存储芯片","f66":-500}
		]}}`
	case "f72":
		return `{"rc":0,"data":{"total":3,"diff":[
			{"f12":"BK0917","f13":90,"f14":"半导体概念","f72":64987872},
			{"f12":"BK0490","f13":90,"f14":"军工","f72":200},
			{"f12":"BK1137","f13":90,"f14":"存储芯片","f72":-600}
		]}}`
	case "f84":
		return `{"rc":0,"data":{"total":3,"diff":[
			{"f12":"BK0917","f13":90,"f14":"半导体概念","f84":-181177728},
			{"f12":"BK0490","f13":90,"f14":"军工","f84":-300},
			{"f12":"BK1137","f13":90,"f14":"存储芯片","f84":1100}
		]}}`
	case "f164":
		return `{"rc":0,"data":{"total":3,"diff":[
			{"f12":"BK0917","f13":90,"f14":"半导体概念","f164":-26905739776},
			{"f12":"BK0490","f13":90,"f14":"军工","f164":-100000},
			{"f12":"BK1137","f13":90,"f14":"存储芯片","f164":-200000}
		]}}`
	case "f174":
		return `{"rc":0,"data":{"total":3,"diff":[
			{"f12":"BK0917","f13":90,"f14":"半导体概念","f174":-99373526272},
			{"f12":"BK0490","f13":90,"f14":"军工","f174":-300000},
			{"f12":"BK1137","f13":90,"f14":"存储芯片","f174":"-"}
		]}}`
	}
	return `{"rc":0,"data":{"total":0,"diff":[]}}`
}

func TestClientSectorFlowRankingMergesMetrics(t *testing.T) {
	var mu sync.Mutex
	requestedKeys := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dataapi/bkzj/getbkzj" {
			t.Fatalf("path = %s, want /dataapi/bkzj/getbkzj", r.URL.Path)
		}
		key := r.URL.Query().Get("key")
		mu.Lock()
		requestedKeys[key] = r.URL.Query().Get("code")
		mu.Unlock()
		_, _ = w.Write([]byte(sectorFlowRankPayloadFor(key)))
	}))
	defer server.Close()

	client := NewClient(WithDataBaseURL(server.URL))
	items, meta, err := client.SectorFlowRanking(context.Background(), "concept", 10)
	if err != nil {
		t.Fatalf("SectorFlowRanking failed: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("len = %d, want 3", len(items))
	}
	// 按主力净流入降序：半导体概念 > 军工 > 存储芯片。
	if items[0].Code != "BK0917" || items[1].Code != "BK0490" || items[2].Code != "BK1137" {
		t.Fatalf("unexpected order: %+v", items)
	}
	top := items[0]
	if top.Name != "半导体概念" || top.MainNetInflow != 3981310464 || top.MainNetInflowRatio != 12.34 {
		t.Fatalf("unexpected main fields: %+v", top)
	}
	if top.ChangePercent != -0.18 {
		t.Fatalf("change percent = %v, want -0.18", top.ChangePercent)
	}
	if top.FiveDayMainNet != -26905739776 || top.TenDayMainNet != -99373526272 {
		t.Fatalf("unexpected multi-day fields: %+v", top)
	}
	if top.SuperLargeNet != 480172992 || top.LargeNet != 64987872 || top.SmallNet != -181177728 {
		t.Fatalf("unexpected breakdown: %+v", top)
	}
	if want := -(top.SuperLargeNet + top.LargeNet + top.SmallNet); top.MediumNet != want {
		t.Fatalf("medium net = %v, want %v", top.MediumNet, want)
	}
	// 多日字段里的 "-" 应该降级成 0 而不是解析失败。
	if items[2].TenDayMainNet != 0 {
		t.Fatalf("missing ten-day value should degrade to zero: %+v", items[2])
	}
	if meta.Source != "eastmoney:bkzj" || !strings.Contains(meta.SourceURL, "/dataapi/bkzj/getbkzj") {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if meta.FallbackReason != "" {
		t.Fatalf("unexpected fallback reason: %s", meta.FallbackReason)
	}
	if items[0].Meta.LatencyMS < 0 {
		t.Fatalf("missing latency evidence: %+v", items[0].Meta)
	}
	if requestedKeys["f62"] != "m:90+t:3+f:!50" || requestedKeys["f174"] != "m:90+t:3+f:!50" {
		t.Fatalf("unexpected board filter for concept dimension: %+v", requestedKeys)
	}
	if len(requestedKeys) != len(sectorFlowRankMetricKeys) {
		t.Fatalf("requested keys = %d, want %d", len(requestedKeys), len(sectorFlowRankMetricKeys))
	}
}

func TestClientSectorFlowRankingUsesIndustryFilter(t *testing.T) {
	var mu sync.Mutex
	var filter string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		filter = r.URL.Query().Get("code")
		mu.Unlock()
		_, _ = w.Write([]byte(sectorFlowRankPayloadFor(r.URL.Query().Get("key"))))
	}))
	defer server.Close()

	client := NewClient(WithDataBaseURL(server.URL))
	if _, _, err := client.SectorFlowRanking(context.Background(), "industry", 5); err != nil {
		t.Fatalf("SectorFlowRanking failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if filter != "m:90+t:2+f:!50" {
		t.Fatalf("filter = %s, want m:90+t:2+f:!50", filter)
	}
	if _, _, err := client.SectorFlowRanking(context.Background(), "unknown", 5); err == nil {
		t.Fatalf("unsupported dimension should fail")
	}
}

func TestClientSectorFlowRankingDegradesWhenOptionalMetricFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "f164" {
			http.Error(w, "temporary upstream failure", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(sectorFlowRankPayloadFor(key)))
	}))
	defer server.Close()

	client := NewClient(WithDataBaseURL(server.URL))
	items, meta, err := client.SectorFlowRanking(context.Background(), "concept", 10)
	if err != nil {
		t.Fatalf("SectorFlowRanking should tolerate optional failures: %v", err)
	}
	if len(items) != 3 || items[0].FiveDayMainNet != 0 {
		t.Fatalf("unexpected items: %+v", items)
	}
	if !strings.Contains(meta.FallbackReason, "f164") {
		t.Fatalf("expected degraded field notice, got %q", meta.FallbackReason)
	}
}

func TestClientSectorFlowRankingFailsWhenMainMetricFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") == "f62" {
			http.Error(w, "temporary upstream failure", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(sectorFlowRankPayloadFor(r.URL.Query().Get("key"))))
	}))
	defer server.Close()

	client := NewClient(WithDataBaseURL(server.URL))
	if _, _, err := client.SectorFlowRanking(context.Background(), "concept", 10); err == nil {
		t.Fatalf("main metric failure should surface an error")
	}
}

func TestSectorFlowCooldownBlocksAfterRepeatedFailures(t *testing.T) {
	var cooldown sectorFlowCooldown
	if _, blocked := cooldown.blocked(); blocked {
		t.Fatal("fresh cooldown should not block")
	}
	cooldown.recordFailure()
	if _, blocked := cooldown.blocked(); blocked {
		t.Fatal("a single failure should not block the path")
	}
	cooldown.recordFailure()
	if _, blocked := cooldown.blocked(); !blocked {
		t.Fatal("repeated failures should trip the cooldown")
	}
	cooldown.reset()
	if _, blocked := cooldown.blocked(); blocked {
		t.Fatal("reset should clear the cooldown")
	}
}

func TestClientSectorFlowIntradaySkipsUpstreamWhileCoolingDown(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "upstream reset", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	// 只留一个上游地址，避免测试在镜像之间重试。
	client.quoteFallbackURLs = nil
	ctx := context.Background()
	for attempt := 0; attempt < 2; attempt++ {
		if _, _, err := client.SectorFlowIntraday(ctx, []string{"BK0917"}); err == nil {
			t.Fatalf("attempt %d should fail while the upstream is down", attempt)
		}
	}
	before := atomic.LoadInt32(&calls)
	if _, _, err := client.SectorFlowIntraday(ctx, []string{"BK0917"}); err == nil || !strings.Contains(err.Error(), "暂不可用") {
		t.Fatalf("expected cooldown error, got %v", err)
	}
	if after := atomic.LoadInt32(&calls); after != before {
		t.Fatalf("cooldown should skip upstream calls: calls %d -> %d", before, after)
	}
}

func TestClientSectorFlowIntradayParsesPoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/qt/stock/fflow/kline/get" {
			t.Fatalf("path = %s, want /api/qt/stock/fflow/kline/get", r.URL.Path)
		}
		if got := r.URL.Query().Get("secid"); got != "90.BK0917" {
			t.Fatalf("secid = %s, want 90.BK0917", got)
		}
		if got := r.URL.Query().Get("klt"); got != "1" {
			t.Fatalf("klt = %s, want 1", got)
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","market":90,"name":"半导体概念","klines":[
			"2026-10-16 09:31,545160864.0,-181177728.0,-363358256.0,64987872.0,480172992.0",
			"2026-10-16 09:32,640685616.0,-177674256.0,-463404112.0,-4672240.0,645357856.0",
			"2026-10-16 15:00,3981310464.0,-944193536.0,-3068276736.0,3151171584.0,830138880.0"
		]}}`))
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	series, meta, err := client.SectorFlowIntraday(context.Background(), []string{"90.bk0917"})
	if err != nil {
		t.Fatalf("SectorFlowIntraday failed: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("len = %d, want 1", len(series))
	}
	item := series[0]
	if item.Code != "BK0917" || item.Name != "半导体概念" || item.TradeDate != "2026-10-16" {
		t.Fatalf("unexpected series identity: %+v", item)
	}
	if len(item.Points) != 3 {
		t.Fatalf("points = %d, want 3", len(item.Points))
	}
	if item.Points[0].Time != "09:31" || item.Points[2].Time != "15:00" {
		t.Fatalf("unexpected point clock: %+v", item.Points)
	}
	if item.Points[0].Main != 545160864 || item.Points[0].Small != -181177728 || item.Points[0].SuperLarge != 480172992 {
		t.Fatalf("unexpected point fields: %+v", item.Points[0])
	}
	if item.LatestMain != 3981310464 {
		t.Fatalf("latest main = %v, want 3981310464", item.LatestMain)
	}
	if meta.Source != "eastmoney:board-intraday-flow" || meta.TradeDate != "2026-10-16" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if _, _, err := client.SectorFlowIntraday(context.Background(), []string{"not-a-board"}); err == nil {
		t.Fatalf("invalid board code should fail")
	}
}

func TestClientSectorFlowIntradaySkipsFailedBoard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("secid"), "BK0490") {
			_, _ = w.Write([]byte(`{"rc":102,"data":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","name":"半导体概念","klines":["2026-10-16 09:31,100.0,0.0,0.0,0.0,100.0"]}}`))
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	series, meta, err := client.SectorFlowIntraday(context.Background(), []string{"BK0917", "BK0490"})
	if err != nil {
		t.Fatalf("SectorFlowIntraday should tolerate single failures: %v", err)
	}
	if len(series) != 1 || series[0].Code != "BK0917" {
		t.Fatalf("unexpected series: %+v", series)
	}
	if !strings.Contains(meta.FallbackReason, "BK0490") {
		t.Fatalf("expected skipped board notice, got %q", meta.FallbackReason)
	}
}
