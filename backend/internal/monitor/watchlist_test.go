package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

// fakeWatchRealtime 记录调用次数：验证兜底通道有 TTL 缓存，不会每次轮询都打实时源。
type fakeWatchRealtime struct {
	calls int
	fail  bool
}

func (f *fakeWatchRealtime) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	f.calls++
	if f.fail {
		return nil, errors.New("实时源不可用")
	}
	quotes := make([]foundation.Quote, 0, len(symbols))
	for _, symbol := range symbols {
		quotes = append(quotes, foundation.Quote{Symbol: symbol, Name: "测试" + symbol, Price: 12.34, ChangePercent: 1.23})
	}
	return quotes, nil
}

func TestWatchlistFallsBackToRealtime(t *testing.T) {
	store := newTestStore(t)
	if err := store.AddWatchlist(context.Background(), "000001.SZ", "平安银行", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	// 快照为空（周末源不可用）时应走实时接口兜底。
	rt := &fakeWatchRealtime{}
	engine := &Engine{store: store, realtime: rt, now: func() time.Time { return time.Now() }}
	items, err := engine.Watchlist(context.Background())
	if err != nil {
		t.Fatalf("关注仓不应报错：%v", err)
	}
	if len(items) != 1 || !items[0].HaveQuote || items[0].Last != 12.34 || items[0].ChangePercent == nil || *items[0].ChangePercent != 1.23 {
		t.Fatalf("快照缺标的应回退实时行情：%+v", items)
	}
	if items[0].Name != "平安银行" {
		t.Fatalf("已有名称不应被实时数据覆盖：%q", items[0].Name)
	}
	// 15 秒 TTL 内的第二次请求命中缓存，不再打实时源。
	if _, err := engine.Watchlist(context.Background()); err != nil {
		t.Fatalf("第二次查询不应报错：%v", err)
	}
	if rt.calls != 1 {
		t.Fatalf("TTL 内应命中缓存，实时源调用次数=%d", rt.calls)
	}
}

func TestWatchlistSnapshotWinsOverRealtime(t *testing.T) {
	store := newTestStore(t)
	if err := store.AddWatchlist(context.Background(), "000001.SZ", "平安银行", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	// 快照有行情时直接用快照，实时源即使挂了也不影响。
	engine := &Engine{
		store:    store,
		realtime: &fakeWatchRealtime{fail: true},
		snapBySymbol: map[string]foundation.MarketSnapshotRow{
			"000001.SZ": {Symbol: "000001.SZ", Name: "平安银行", Price: 10.5, ChangePercent: -0.8},
		},
		now: func() time.Time { return time.Now() },
	}
	items, err := engine.Watchlist(context.Background())
	if err != nil {
		t.Fatalf("关注仓不应报错：%v", err)
	}
	if len(items) != 1 || !items[0].HaveQuote || items[0].Last != 10.5 || items[0].ChangePercent == nil || *items[0].ChangePercent != -0.8 {
		t.Fatalf("快照行情应优先：%+v", items)
	}
}

// watchKline 给关注仓测试一条指定收盘序列：只关心 5 日累计怎么算。
type watchKline struct{ closes []float64 }

func (k watchKline) KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	return testBars(k.closes, nil), nil
}

func TestWatchlistMomentum5UsesLastFiveSessions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.AddWatchlist(ctx, "000001.SZ", "平安银行", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	// 末根 12 对倒数第 6 根 10.5：近 5 日 +14.29%。
	engine := &Engine{
		store:  store,
		kline:  watchKline{closes: []float64{10, 10.5, 11, 10.8, 11, 11.2, 12}},
		klines: map[string]klineEntry{},
		now:    func() time.Time { return time.Now() },
	}
	items, err := engine.Watchlist(ctx)
	if err != nil {
		t.Fatalf("关注仓不应报错：%v", err)
	}
	if len(items) != 1 {
		t.Fatalf("应有 1 条关注仓：%+v", items)
	}
	if items[0].Momentum5 == nil {
		t.Fatalf("日线够 6 根时应算出 5 日涨跌：%+v", items[0])
	}
	if got, want := *items[0].Momentum5, round2((12.0/10.5-1)*100); got != want {
		t.Fatalf("5 日涨跌 = %.2f，应为 %.2f", got, want)
	}
}

func TestWatchlistMomentum5SkipsShortSeries(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.AddWatchlist(ctx, "000001.SZ", "平安银行", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	// 只有 3 根日线：算不出 5 日，留空让前端显示 —，不能拿更短的窗口凑数。
	engine := &Engine{
		store:  store,
		kline:  watchKline{closes: []float64{10, 10.4, 10.8}},
		klines: map[string]klineEntry{},
		now:    func() time.Time { return time.Now() },
	}
	items, err := engine.Watchlist(ctx)
	if err != nil {
		t.Fatalf("关注仓不应报错：%v", err)
	}
	if len(items) != 1 || items[0].Momentum5 != nil {
		t.Fatalf("日线不足时应留空：%+v", items)
	}
}
