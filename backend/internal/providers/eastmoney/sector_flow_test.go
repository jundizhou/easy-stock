package eastmoney

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	case "f165":
		return `{"rc":0,"data":{"total":3,"diff":[
			{"f12":"BK0917","f13":90,"f14":"半导体概念","f165":-1234},
			{"f12":"BK0490","f13":90,"f14":"军工","f165":-56},
			{"f12":"BK1137","f13":90,"f14":"存储芯片","f165":"-"}
		]}}`
	case "f174":
		return `{"rc":0,"data":{"total":3,"diff":[
			{"f12":"BK0917","f13":90,"f14":"半导体概念","f174":-99373526272},
			{"f12":"BK0490","f13":90,"f14":"军工","f174":-300000},
			{"f12":"BK1137","f13":90,"f14":"存储芯片","f174":"-"}
		]}}`
	case "f175":
		return `{"rc":0,"data":{"total":3,"diff":[
			{"f12":"BK0917","f13":90,"f14":"半导体概念","f175":2500},
			{"f12":"BK0490","f13":90,"f14":"军工","f175":100},
			{"f12":"BK1137","f13":90,"f14":"存储芯片","f175":"-"}
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
	if top.FiveDayMainRatio != -12.34 || top.TenDayMainRatio != 25 {
		t.Fatalf("unexpected multi-day ratios: %+v", top)
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
	if items[2].FiveDayMainRatio != 0 || items[2].TenDayMainRatio != 0 {
		t.Fatalf("missing multi-day ratio should degrade to zero: %+v", items[2])
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

func TestSectorFlowIntradayHostsOrderAndDedupe(t *testing.T) {
	client := NewClient()
	client.quoteBaseURL = "https://push2.eastmoney.com/"
	client.quoteFallbackURLs = []string{"https://push2delay.eastmoney.com", "https://82.push2.eastmoney.com", "https://push2.eastmoney.com", ""}

	got := client.sectorFlowIntradayHosts()
	want := []string{
		"https://push2.eastmoney.com",
		"https://push2delay.eastmoney.com",
		"https://82.push2.eastmoney.com",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts = %v, want %v", got, want)
	}
}

func TestClientSectorFlowIntradayFallsBackToMirror(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// push2 系列对 fflow/kline 会直接断连，这里用 502 模拟不可用的常规入口。
		http.Error(w, "upstream reset", http.StatusBadGateway)
	}))
	defer primary.Close()

	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","name":"半导体概念","klines":["2026-10-16 09:31,100.0,0.0,0.0,0.0,100.0"]}}`))
	}))
	defer mirror.Close()

	client := NewClient(WithQuoteBaseURL(primary.URL))
	client.quoteFallbackURLs = []string{mirror.URL}
	series, meta, err := client.SectorFlowIntraday(context.Background(), []string{"BK0917"})
	if err != nil {
		t.Fatalf("SectorFlowIntraday failed: %v", err)
	}
	if len(series) != 1 || series[0].LatestMain != 100 {
		t.Fatalf("unexpected series: %+v", series)
	}
	if !strings.HasPrefix(meta.SourceURL, mirror.URL) {
		t.Fatalf("meta.SourceURL = %q, want mirror %s", meta.SourceURL, mirror.URL)
	}
}

func TestClientSectorFlowIntradaySkipsMirrorReturningEmptyData(t *testing.T) {
	var emptyCalls int32
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&emptyCalls, 1)
		// push2his 会返回 rc=0 但 klines 为空。
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","name":"半导体概念","klines":[]}}`))
	}))
	defer empty.Close()

	regular := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","name":"半导体概念","klines":["2026-10-16 09:31,100.0,0.0,0.0,0.0,100.0"]}}`))
	}))
	defer regular.Close()

	client := NewClient(WithQuoteBaseURL(empty.URL))
	client.quoteFallbackURLs = []string{regular.URL}
	series, meta, err := client.SectorFlowIntraday(context.Background(), []string{"BK0917"})
	if err != nil {
		t.Fatalf("SectorFlowIntraday failed: %v", err)
	}
	if len(series) != 1 || series[0].LatestMain != 100 {
		t.Fatalf("unexpected series: %+v", series)
	}
	if calls := atomic.LoadInt32(&emptyCalls); calls != 1 {
		t.Fatalf("空数据镜像应只被请求一次，calls = %d", calls)
	}
	if !strings.HasPrefix(meta.SourceURL, regular.URL) {
		t.Fatalf("meta.SourceURL = %q, want %s", meta.SourceURL, regular.URL)
	}
}

// stubSessionProvider 是 SessionProvider 的测试替身。
type stubSessionProvider struct {
	cookies         []string
	refreshedCookie string
	refreshes       int32
	returnsError    bool
}

func (s *stubSessionProvider) SessionCookie(_ context.Context, refresh bool) (string, error) {
	if refresh {
		atomic.AddInt32(&s.refreshes, 1)
	}
	if s.returnsError {
		return "", context.Canceled
	}
	if refresh {
		return s.refreshedCookie, nil
	}
	if len(s.cookies) == 0 {
		return "", nil
	}
	return s.cookies[0], nil
}

func TestClientSectorFlowIntradaySendsHandedOverSessionCookie(t *testing.T) {
	var gotCookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","name":"半导体概念","klines":["2026-10-16 09:31,100.0,0.0,0.0,0.0,100.0"]}}`))
	}))
	defer server.Close()

	provider := &stubSessionProvider{cookies: []string{"nid18=from-browser"}}
	client := NewClient(WithQuoteBaseURL(server.URL), WithSessionProvider(provider))
	if _, _, err := client.SectorFlowIntraday(context.Background(), []string{"BK0917"}); err != nil {
		t.Fatalf("SectorFlowIntraday failed: %v", err)
	}
	if gotCookie != "nid18=from-browser" {
		t.Fatalf("Cookie header = %q, want nid18=from-browser", gotCookie)
	}
	if atomic.LoadInt32(&provider.refreshes) != 0 {
		t.Fatalf("healthy session should not be refreshed")
	}
}

func TestClientSectorFlowIntradayRefreshesMissingSessionOnce(t *testing.T) {
	var cookies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie"))
		if r.Header.Get("Cookie") == "" {
			// 首轮没有会话：上游直接断连（这里用连接错误等价表达）。
			http.Error(w, "upstream reset", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","name":"半导体概念","klines":["2026-10-16 09:31,100.0,0.0,0.0,0.0,100.0"]}}`))
	}))
	defer server.Close()

	// 首次取会话失败（空会话），刷新后拿到会话。
	provider := &stubSessionProvider{cookies: []string{""}, refreshedCookie: "nid18=fresh"}
	client := NewClient(WithQuoteBaseURL(server.URL), WithSessionProvider(provider))
	series, _, err := client.SectorFlowIntraday(context.Background(), []string{"BK0917"})
	if err != nil {
		t.Fatalf("SectorFlowIntraday should retry once after the session becomes available: %v", err)
	}
	if len(series) != 1 || series[0].Code != "BK0917" {
		t.Fatalf("unexpected series: %+v", series)
	}
	if len(cookies) != 2 || cookies[0] != "" || cookies[1] != "nid18=fresh" {
		t.Fatalf("cookies sent = %v, want empty then nid18=fresh", cookies)
	}
	if atomic.LoadInt32(&provider.refreshes) != 1 {
		t.Fatalf("refresh count = %d, want 1", atomic.LoadInt32(&provider.refreshes))
	}
}

func TestClientSectorFlowIntradayRefreshesSessionAndRetriesPrimaryHost(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "upstream reset", http.StatusBadGateway)
	}))
	defer server.Close()

	// 带着会话仍然被上游断连时，重新取一次会话并只打主入口再试一次：
	// nid18 值不变，起作用的是桌面端重新访问页面后的现场状态。
	provider := &stubSessionProvider{cookies: []string{"nid18=stale"}, refreshedCookie: "nid18=stale"}
	client := NewClient(WithQuoteBaseURL(server.URL), WithSessionProvider(provider))
	if _, _, err := client.SectorFlowIntraday(context.Background(), []string{"BK0917"}); err == nil {
		t.Fatal("expected failure while the upstream is down")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls = %d, want 首次 + 换会话后主入口各一次", got)
	}
	if atomic.LoadInt32(&provider.refreshes) != 1 {
		t.Fatalf("refresh count = %d, want 1", atomic.LoadInt32(&provider.refreshes))
	}
}

func TestClientSectorFlowIntradayRefreshesSessionAtMostOncePerWindow(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "upstream reset", http.StatusBadGateway)
	}))
	defer server.Close()

	provider := &stubSessionProvider{cookies: []string{"nid18=stale"}, refreshedCookie: "nid18=stale"}
	client := NewClient(WithQuoteBaseURL(server.URL), WithSessionProvider(provider))
	if _, _, err := client.SectorFlowIntraday(context.Background(), []string{"BK0917", "BK0490"}); err == nil {
		t.Fatal("expected failure while the upstream is down")
	}
	// 第二个板块失败时仍在去重窗口内：只有首轮 + 第一次重试共 3 次上游请求。
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("upstream calls = %d, want 3", got)
	}
	if atomic.LoadInt32(&provider.refreshes) != 1 {
		t.Fatalf("refresh count = %d, want 1", atomic.LoadInt32(&provider.refreshes))
	}
}

func TestClientSectorFlowIntradayReusesPerBoardCache(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.URL.Query().Get("secid"), "90.")
		requested = append(requested, code)
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"` + code + `","name":"` + code + `","klines":["2026-10-16 09:31,100.0,0.0,0.0,0.0,100.0"]}}`))
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	ctx := context.Background()
	if _, _, err := client.SectorFlowIntraday(ctx, []string{"BK0917"}); err != nil {
		t.Fatalf("first batch failed: %v", err)
	}
	// 前端逐个勾选板块时，请求会从 [A] 变成 [A,B]；A 应直接命中缓存。
	series, meta, err := client.SectorFlowIntraday(ctx, []string{"BK0917", "BK0490"})
	if err != nil {
		t.Fatalf("second batch failed: %v", err)
	}
	if len(series) != 2 || series[0].Code != "BK0917" || series[1].Code != "BK0490" {
		t.Fatalf("unexpected series order: %+v", series)
	}
	if !reflect.DeepEqual(requested, []string{"BK0917", "BK0490"}) {
		t.Fatalf("upstream requests = %v, want one request per board", requested)
	}
	// 同一组板块在缓存有效期内再次请求时不应再打上游，并沿用缓存记录的来源地址。
	series, meta, err = client.SectorFlowIntraday(ctx, []string{"BK0917", "BK0490"})
	if err != nil {
		t.Fatalf("cached batch failed: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("cached series = %+v, want 2", series)
	}
	if !reflect.DeepEqual(requested, []string{"BK0917", "BK0490"}) {
		t.Fatalf("cached batch should not hit upstream, requests = %v", requested)
	}
	if !strings.HasPrefix(meta.SourceURL, server.URL) {
		t.Fatalf("meta.SourceURL = %q, want the cached mirror %s", meta.SourceURL, server.URL)
	}
}

func TestClientSectorFlowIntradaySkipsRefreshWithoutSessionProvider(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("Cookie") != "" {
			t.Fatalf("Cookie header = %q, want empty without a session provider", r.Header.Get("Cookie"))
		}
		http.Error(w, "upstream reset", http.StatusBadGateway)
	}))
	defer server.Close()

	// 没有会话来源（网页版）时不做换会话重试，保持一次请求即返回。
	client := NewClient(WithQuoteBaseURL(server.URL))
	if _, _, err := client.SectorFlowIntraday(context.Background(), []string{"BK0917"}); err == nil {
		t.Fatal("expected failure while the upstream is down")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

// eofTransport 模拟“默认地址族被对端直接断开”：任何请求都返回 EOF，
// 对应东财部分域名只对一个地址族放行、默认拨号被断开的实测现象。
type eofTransport struct{}

func (eofTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, io.EOF
}

// 分时价格走 trends2：字段是「时间,开,收,高,低,量,额,均价」，涨跌幅按昨收换算。
func TestClientSectorPriceIntradayParsesTrends(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/qt/stock/trends2/get" {
			t.Fatalf("path = %s, want /api/qt/stock/trends2/get", r.URL.Path)
		}
		query := r.URL.Query()
		if got := query.Get("secid"); got != "90.BK0917" {
			t.Fatalf("secid = %s, want 90.BK0917", got)
		}
		if got := query.Get("ndays"); got != "1" {
			t.Fatalf("ndays = %s, want 1", got)
		}
		if got := query.Get("fields2"); got != "f51,f52,f53,f54,f55,f56,f57,f58" {
			t.Fatalf("fields2 = %s, want the intraday price fields", got)
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","market":90,"name":"半导体概念","decimal":2,"preClose":2419.01,"trends":[
			"2026-10-16 09:30,2468.24,2468.24,2468.24,2461.28,623298,2815619168.00,2468.238",
			"2026-10-16 15:00,2414.01,2414.76,2414.76,2414.01,943129,3266730016.00,2439.120"
		]}}`))
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	series, meta, err := client.SectorPriceIntraday(context.Background(), []string{"90.bk0917"})
	if err != nil {
		t.Fatalf("SectorPriceIntraday failed: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("len = %d, want 1", len(series))
	}
	item := series[0]
	if item.Code != "BK0917" || item.Name != "半导体概念" || item.TradeDate != "2026-10-16" || item.PreClose != 2419.01 {
		t.Fatalf("unexpected series identity: %+v", item)
	}
	if len(item.Points) != 2 {
		t.Fatalf("points = %d, want 2", len(item.Points))
	}
	if item.Points[0].Time != "09:30" || item.Points[0].Price != 2468.24 || item.Points[0].Average != 2468.238 {
		t.Fatalf("unexpected first point: %+v", item.Points[0])
	}
	// 最后一分钟收 2414.76、昨收 2419.01：涨跌幅应与榜单的 -0.18% 同口径。
	if math.Abs(item.Points[1].ChangePercent-(-0.1757)) > 0.001 {
		t.Fatalf("change percent = %v, want about -0.1757", item.Points[1].ChangePercent)
	}
	if item.LatestChange != item.Points[1].ChangePercent {
		t.Fatalf("latest change = %v, want the last point", item.LatestChange)
	}
	if meta.Source != "eastmoney:board-intraday-trend" || meta.TradeDate != "2026-10-16" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if _, _, err := client.SectorPriceIntraday(context.Background(), []string{"not-a-board"}); err == nil {
		t.Fatalf("invalid board code should fail")
	}
}

func TestClientSectorPriceIntradaySkipsFailedBoard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("secid"), "BK0490") {
			_, _ = w.Write([]byte(`{"rc":102,"data":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","name":"半导体概念","preClose":100,"trends":["2026-10-16 09:30,100.0,100.0,100.0,100.0,10,1000,100.0"]}}`))
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	series, meta, err := client.SectorPriceIntraday(context.Background(), []string{"BK0917", "BK0490"})
	if err != nil {
		t.Fatalf("SectorPriceIntraday should tolerate single failures: %v", err)
	}
	if len(series) != 1 || series[0].Code != "BK0917" {
		t.Fatalf("unexpected series: %+v", series)
	}
	if !strings.Contains(meta.FallbackReason, "BK0490") {
		t.Fatalf("expected skipped board notice, got %q", meta.FallbackReason)
	}
}

// 镜像返回 rc=0 但 trends 为空时不算成功（与分时资金流的 klines 为空一致），继续尝试下一个入口。
func TestClientSectorPriceIntradayFallsBackToMirrorOnEmptyTrends(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","name":"半导体概念","trends":[]}}`))
	}))
	defer primary.Close()

	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","name":"半导体概念","preClose":100,"trends":["2026-10-16 09:30,100.0,101.0,101.0,99.0,10,1000,100.5"]}}`))
	}))
	defer mirror.Close()

	client := NewClient(WithQuoteBaseURL(primary.URL))
	client.quoteFallbackURLs = []string{mirror.URL}
	series, meta, err := client.SectorPriceIntraday(context.Background(), []string{"BK0917"})
	if err != nil {
		t.Fatalf("SectorPriceIntraday failed: %v", err)
	}
	if len(series) != 1 || series[0].Points[0].Price != 101 || math.Abs(series[0].LatestChange-1) > 1e-9 {
		t.Fatalf("unexpected series: %+v", series)
	}
	if !strings.HasPrefix(meta.SourceURL, mirror.URL) {
		t.Fatalf("meta.SourceURL = %q, want mirror %s", meta.SourceURL, mirror.URL)
	}
}

// 默认地址族失败时换 IPv4 客户端重试同一个镜像：实测 90.push2 默认拨号 EOF、强制 IPv4 返回 200。
func TestClientSectorPriceIntradayRetriesWithIPv4ClientOnTransientError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"BK0917","name":"半导体概念","preClose":100,"trends":["2026-10-16 09:30,100.0,101.0,101.0,99.0,10,1000,100.5"]}}`))
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	client.httpClient = &http.Client{Transport: eofTransport{}}
	client.fallbackHTTPClient = server.Client()

	series, _, err := client.SectorPriceIntraday(context.Background(), []string{"BK0917"})
	if err != nil {
		t.Fatalf("SectorPriceIntraday should retry with the IPv4 client: %v", err)
	}
	if len(series) != 1 || len(series[0].Points) != 1 || series[0].Points[0].Price != 101 {
		t.Fatalf("unexpected series: %+v", series)
	}
}

// 前端逐个勾选板块时，缓存命中的板块不应重复打上游；全部命中时沿用缓存记录的来源地址。
func TestClientSectorPriceIntradayReusesPerBoardCache(t *testing.T) {
	var mu sync.Mutex
	requested := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.URL.Query().Get("secid"), "90.")
		mu.Lock()
		requested[code]++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"rc":0,"data":{"code":"` + code + `","name":"` + code + `","preClose":100,"trends":["2026-10-16 09:30,100.0,100.0,100.0,100.0,10,1000,100.0"]}}`))
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	ctx := context.Background()
	if _, _, err := client.SectorPriceIntraday(ctx, []string{"BK0917"}); err != nil {
		t.Fatalf("first batch failed: %v", err)
	}
	series, _, err := client.SectorPriceIntraday(ctx, []string{"BK0917", "BK0490"})
	if err != nil {
		t.Fatalf("second batch failed: %v", err)
	}
	if len(series) != 2 || series[0].Code != "BK0917" || series[1].Code != "BK0490" {
		t.Fatalf("unexpected series order: %+v", series)
	}
	mu.Lock()
	first := map[string]int{"BK0917": requested["BK0917"], "BK0490": requested["BK0490"]}
	mu.Unlock()
	if first["BK0917"] != 1 || first["BK0490"] != 1 {
		t.Fatalf("upstream requests = %v, want one request per board", first)
	}

	series, meta, err := client.SectorPriceIntraday(ctx, []string{"BK0917", "BK0490"})
	if err != nil {
		t.Fatalf("cached batch failed: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("cached series = %+v, want 2", series)
	}
	mu.Lock()
	second := map[string]int{"BK0917": requested["BK0917"], "BK0490": requested["BK0490"]}
	mu.Unlock()
	if second["BK0917"] != 1 || second["BK0490"] != 1 {
		t.Fatalf("cached batch should not hit upstream, requests = %v", second)
	}
	if !strings.HasPrefix(meta.SourceURL, server.URL) {
		t.Fatalf("meta.SourceURL = %q, want the cached mirror %s", meta.SourceURL, server.URL)
	}
}
