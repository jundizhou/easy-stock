package stockdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeStockDB 返回一个模拟 free-stockdb 的 HTTP 服务：根据 cmd=get 的 t 参数返回
// 与真实引擎同构的 JSON（[key, value] 数组）。仅覆盖测试需要的表。
func fakeStockDB(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		key := r.Form.Get("t")
		var body string
		switch {
		case key == "日k:600519":
			body = `[
				["日k:600519:20240102",{"date":20240102,"open":10,"high":11,"low":9,"close":10.5,"volume":100,"name":"TEST"}],
				["日k:600519:20240103",{"date":20240103,"open":10.5,"high":12,"low":10,"close":11.5,"volume":200,"name":"TEST"}],
				["日k:600519:20240108",{"date":20240108,"open":11,"high":13,"low":10.5,"close":12.5,"volume":300,"name":"TEST"}]
			]`
		case key == "复权:600519":
			body = `[
				["复权:600519:20240102",{"cum":1.0}],
				["复权:600519:20240103",{"cum":1.2}]
			]`
		case key == "日k:000001":
			// 停牌样本：四价相等、成交量为 0，应被 validBar 剔除
			body = `[["日k:000001:20240102",{"date":20240102,"open":5,"high":5,"low":5,"close":5,"volume":0,"name":"PINGAN"}]]`
		default:
			body = `[]`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	return httptest.NewServer(mux)
}

func TestResamplePeriodWeekly(t *testing.T) {
	daily := []Bar{
		{Date: 20240102, Open: 10, High: 11, Low: 9, Close: 10.5, Volume: 100},
		{Date: 20240103, Open: 10.5, High: 12, Low: 10, Close: 11.5, Volume: 200},
		{Date: 20240108, Open: 11, High: 13, Low: 10.5, Close: 12.5, Volume: 300}, // 下一 ISO 周
	}
	w := ResamplePeriod(daily, "week")
	if len(w) != 2 {
		t.Fatalf("weekly count = %d, want 2", len(w))
	}
	first := w[0]
	if first.Open != 10 || first.High != 12 || first.Low != 9 || first.Close != 11.5 || first.Volume != 300 {
		t.Errorf("first weekly = %+v, want O10 H12 L9 C11.5 V300", first)
	}
}

func TestResamplePeriodMonthly(t *testing.T) {
	daily := []Bar{
		{Date: 20240131, Open: 10, High: 11, Low: 9, Close: 10.5, Volume: 100},
		{Date: 20240201, Open: 10.5, High: 12, Low: 10, Close: 11.5, Volume: 200}, // 跨月
	}
	m := ResamplePeriod(daily, "month")
	if len(m) != 2 {
		t.Fatalf("monthly count = %d, want 2", len(m))
	}
	if m[0].Date != 20240131 || m[1].Date != 20240201 {
		t.Errorf("monthly dates = %d,%d want 20240131,20240201", m[0].Date, m[1].Date)
	}
}

func TestApplyQFQ(t *testing.T) {
	bars := []Bar{
		{Date: 20240102, Open: 10, High: 11, Low: 9, Close: 10.5, Volume: 100},
		{Date: 20240103, Open: 10.5, High: 12, Low: 10, Close: 11.5, Volume: 200},
	}
	recs := []factorRec{{Date: 20240102, Cum: 1.0}, {Date: 20240103, Cum: 1.2}}
	out := applyQFQ(bars, recs)
	// 最新一根 cum==cumLatest → qfq==raw
	if out[1].Close != 11.5 {
		t.Errorf("latest qfq close = %v, want 11.5", out[1].Close)
	}
	// 前一根 qfq = raw * 1.0/1.2
	want := 10.5 * 1.0 / 1.2
	if out[0].Close != want {
		t.Errorf("older qfq close = %v, want %v", out[0].Close, want)
	}
	// 无因子记录时原样返回
	if passthrough := applyQFQ(bars, nil); passthrough[0].Close != 10.5 {
		t.Errorf("no-records passthrough close = %v, want 10.5", passthrough[0].Close)
	}
}

func TestDailyBarsQFQAndValidBar(t *testing.T) {
	srv := fakeStockDB(t)
	defer srv.Close()
	c := NewClient(WithBaseURL(srv.URL))

	bars, err := c.DailyBars(context.Background(), "600519", 2000)
	if err != nil {
		t.Fatalf("DailyBars error: %v", err)
	}
	if len(bars) != 3 {
		t.Fatalf("bars count = %d, want 3", len(bars))
	}
	// 最新一根（20240108）无对应因子记录，取最新因子 cumLatest → qfq==raw
	last := bars[2]
	if last.Close != 12.5 {
		t.Errorf("last close = %v, want 12.5 (raw==qfq)", last.Close)
	}
	// 20240103 恰好是最新因子日期（cum==cumLatest）→ qfq==raw
	if bars[1].Close != 11.5 {
		t.Errorf("bar[1] qfq close = %v, want 11.5 (raw==qfq at latest factor date)", bars[1].Close)
	}
	// 20240102 使用 cum=1.0 → qfq = 10.5 * 1.0/1.2
	want := 10.5 / 1.2
	if bars[0].Close != want {
		t.Errorf("bar[0] qfq close = %v, want %v", bars[0].Close, want)
	}
}

func TestRawBarsSkipsSuspended(t *testing.T) {
	srv := fakeStockDB(t)
	defer srv.Close()
	c := NewClient(WithBaseURL(srv.URL))
	bars, err := c.RawBars(context.Background(), "日k", "000001", 2000)
	if err != nil {
		t.Fatalf("RawBars error: %v", err)
	}
	if len(bars) != 0 {
		t.Errorf("suspended bar should be skipped, got %d", len(bars))
	}
}

func TestWeeklyBarsViaResample(t *testing.T) {
	srv := fakeStockDB(t)
	defer srv.Close()
	c := NewClient(WithBaseURL(srv.URL))
	w, err := c.WeeklyBars(context.Background(), "600519", 2000)
	if err != nil {
		t.Fatalf("WeeklyBars error: %v", err)
	}
	if len(w) != 2 {
		t.Errorf("weekly count = %d, want 2", len(w))
	}
}

func TestCacheHitsAndExpires(t *testing.T) {
	q := newQueryCache(20 * time.Millisecond)
	q.put("k", []byte("v"))
	if b, ok := q.get("k"); !ok || string(b) != "v" {
		t.Fatalf("cache get after put failed: ok=%v", ok)
	}
	time.Sleep(25 * time.Millisecond)
	if _, ok := q.get("k"); ok {
		t.Fatalf("cache should have expired")
	}
}
