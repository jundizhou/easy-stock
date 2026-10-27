package monitor

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

// 异动预警窗口评估的场景测试：阈值随板块/ST 变化、上下不对称的
// 严重异动窗口、剩余空间与触发价的换算，以及逐日明细的指数对齐。

func TestEvalAbnormalWindowsMainBoard(t *testing.T) {
	// 主板 3 日偏离 +18：接近（0.9），还差 2 个百分点到 ±20。
	windows := evalAbnormalWindows(18, 0, 0, foundation.BoardMain, false, 10, 10)
	worst := worstAbnormalWindow(windows)
	if worst.Key != AbnormalWindow3d || worst.Threshold != 20 || worst.Direction != "up" {
		t.Fatalf("worst 应为 3 日上行窗口: %+v", worst)
	}
	if math.Abs(worst.Progress-0.9) > 1e-9 {
		t.Fatalf("progress 应为 0.9, got %v", worst.Progress)
	}
	if math.Abs(worst.Remaining-2) > 1e-9 {
		t.Fatalf("remaining 应为 2, got %v", worst.Remaining)
	}
	if math.Abs(worst.TriggerPrice-10.2) > 1e-9 {
		t.Fatalf("trigger price 应为 10.2, got %v", worst.TriggerPrice)
	}
	if level := abnormalLevel(windows); level != "near" {
		t.Fatalf("0.9 应判 near, got %q", level)
	}
}

func TestEvalAbnormalWindowsSTMainBoard(t *testing.T) {
	// 2026-07-06 新规后主板 ST 与普通股同为 20：+18 只到观察级。
	windows := evalAbnormalWindows(18, 0, 0, foundation.BoardMain, true, 5)
	worst := worstAbnormalWindow(windows)
	if worst.Threshold != 20 {
		t.Fatalf("ST 主板 3 日阈值应为 20, got %v", worst.Threshold)
	}
	if worst.Remaining != 2 {
		t.Fatalf("20-18 剩余空间应为 2, got %v", worst.Remaining)
	}
	if level := abnormalLevel(windows); level != "near" {
		t.Fatalf("+18/20 应判 near, got %q", level)
	}
	// +22 才越过阈值。
	over := evalAbnormalWindows(22, 0, 0, foundation.BoardMain, true, 5)
	if worst := worstAbnormalWindow(over); worst.Remaining > 0 {
		t.Fatalf("已越过阈值时 remaining 应为负, got %v", worst.Remaining)
	}
	if level := abnormalLevel(over); level != "triggered" {
		t.Fatalf("+22/20 应判 triggered, got %q", level)
	}
}

func TestEvalAbnormalWindowsChiNext(t *testing.T) {
	// 创业板 3 日阈值 30：+18 只是观察级（0.6）。
	windows := evalAbnormalWindows(18, 0, 0, foundation.BoardChiNext, false, 10)
	if worst := worstAbnormalWindow(windows); worst.Threshold != 30 {
		t.Fatalf("创业板 3 日阈值应为 30, got %v", worst.Threshold)
	}
	if level := abnormalLevel(windows); level != "watch" {
		t.Fatalf("18/30 应判 watch, got %q", level)
	}
}

func TestEvalAbnormalWindowsSevereDownside(t *testing.T) {
	// 严重异动上下不对称：10 日 -60 按下阈 50 判 triggered，
	// 30 日 -80 按下阈 70 判 triggered，worst 应取比例更大的窗口。
	windows := evalAbnormalWindows(0, -60, -80, foundation.BoardMain, false, 20)
	worst := worstAbnormalWindow(windows)
	if worst.Key != AbnormalWindow10d || worst.Direction != "down" {
		t.Fatalf("10 日下行 60/50 应为 worst: %+v", worst)
	}
	if worst.Threshold != 50 {
		t.Fatalf("10 日下阈应为 50, got %v", worst.Threshold)
	}
	if level := abnormalLevel(windows); level != "triggered" {
		t.Fatalf("应判 triggered, got %q", level)
	}
}

func TestEvalAbnormalWindowsUpBias(t *testing.T) {
	// 10 日 +60：上阈 100，比例 0.6 观察级；若误用下阈 50 会误判 triggered。
	windows := evalAbnormalWindows(0, 60, 0, foundation.BoardMain, false, 10)
	if level := abnormalLevel(windows); level != "watch" {
		t.Fatalf("60/100 应判 watch, got %q", level)
	}
}

func TestDeviationWindowDevAlignment(t *testing.T) {
	// 个股 3 日涨 10%，指数同步涨 4% → dev3 = 6。
	bars := testBars([]float64{100, 100, 100, 100, 104, 107, 110}, nil)
	aligned := []float64{1000, 1000, 1000, 1000, 1010, 1030, 1040}
	dev, ok := deviationWindowDev(bars, aligned, 3)
	if !ok {
		t.Fatalf("应有足够数据")
	}
	if math.Abs(dev-6) > 1e-6 {
		t.Fatalf("dev3 应为 6 (10-4), got %v", dev)
	}
}

func TestAbnormalCalcRows(t *testing.T) {
	// 5 根棒、指数按日对齐：逐日表应给 4 行，第 4 行起 dev3 才有值。
	bars := testBars([]float64{100, 110, 100, 110, 120}, nil)
	aligned := []float64{1000, 1050, 1000, 1050, 1100}
	rows := abnormalCalcRows(bars, aligned, 30)
	if len(rows) != 4 {
		t.Fatalf("应有 4 行, got %d", len(rows))
	}
	last := rows[len(rows)-1]
	if last.Date == "" || last.IndexChange == nil {
		t.Fatalf("末行应带日期与指数涨幅: %+v", last)
	}
	if math.Abs(last.StockChange-9.09) > 0.01 {
		t.Fatalf("末行个股涨幅应为 9.09, got %v", last.StockChange)
	}
	if last.Dev3 == nil || math.Abs(*last.Dev3-4.33) > 0.01 {
		t.Fatalf("末行 dev3 应为 4.33 (10-4.76 舍入), got %+v", last.Dev3)
	}
	if rows[0].Dev3 != nil {
		t.Fatalf("首行不足 3 日窗口, dev3 应为空")
	}
}

func TestAbnormalLevelBoundary(t *testing.T) {
	// 分级边界：0.5 进观察、0.49 沉默、1.0 触发。
	cases := []struct {
		dev3  float64
		level string
	}{
		{9.8, "normal"},
		{10.02, "watch"},
		{14.0, "near"},
		{20.0, "triggered"},
	}
	for _, tc := range cases {
		windows := evalAbnormalWindows(tc.dev3, 0, 0, foundation.BoardMain, false, 10)
		if level := abnormalLevel(windows); level != tc.level {
			t.Fatalf("dev3=%v 应判 %q, got %q", tc.dev3, tc.level, level)
		}
	}
}

// ---------------------------------------------------------------- 监控个股（侧栏加入）

// fakeIndexSeries 给一根平稳指数线：偏离值只由个股日线贡献。
type fakeIndexSeries struct{}

func (fakeIndexSeries) MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error) {
	return foundation.MarketIndexSeries{Lines: testBars(fill(1000, 40), nil)}, nil
}

// flatKline 恒定 100：3/10/30 日偏离都是 0，normal。
type flatKline struct{}

func (flatKline) KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	return testBars(fill(100, 40), nil), nil
}

// errKline 模拟日线源故障：调用方应安静跳过而不是硬造行。
type errKline struct{}

func (errKline) KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	return nil, errors.New("boom")
}

func newAbnormalEngine(t *testing.T, kline KLineProvider) *Engine {
	t.Helper()
	return &Engine{
		kline:       kline,
		index:       fakeIndexSeries{},
		indexSeries: map[string]cachedIndexSeries{},
		klines:      map[string]klineEntry{},
		now:         func() time.Time { return time.Now() },
	}
}

func TestMonitoredSymbolsFromWatchlist(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.AddWatchlist(ctx, "000001.SZ", "平安银行", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	if err := store.AddWatchlist(ctx, "600519.SH", "贵州茅台", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	engine := &Engine{store: store}
	monitored := engine.monitoredSymbols(ctx)
	if len(monitored) != 2 || !monitored["000001.SZ"] || !monitored["600519.SH"] {
		t.Fatalf("监控集合应等于关注仓：%+v", monitored)
	}
	// 没配 store 的引擎（独立部署退化模式）返回空集合，不 panic。
	if empty := (&Engine{}).monitoredSymbols(ctx); len(empty) != 0 {
		t.Fatalf("无 store 应返回空集合：%+v", empty)
	}
	// 移除后立刻退出监控集合。
	if err := store.RemoveWatchlist(ctx, "600519.SH"); err != nil {
		t.Fatalf("移除关注仓不应报错：%v", err)
	}
	if after := engine.monitoredSymbols(ctx); len(after) != 1 || after["600519.SH"] {
		t.Fatalf("移除后应只剩一只：%+v", after)
	}
}

func TestAbnormalItemForSymbolKeepsNormalWhenMonitored(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.AddWatchlist(ctx, "000001.SZ", "平安银行", "连板池"); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	engine := newAbnormalEngine(t, flatKline{})
	engine.store = store
	item, ok := engine.abnormalItemForSymbol(ctx, "000001.SZ", nil)
	if !ok {
		t.Fatalf("监控个股即使 normal 也应给出状态行")
	}
	if !item.Monitored {
		t.Fatalf("该行应带监控标记：%+v", item)
	}
	if item.Level != "normal" {
		t.Fatalf("恒定价的个股应判 normal，got %q", item.Level)
	}
	if len(item.Windows) != 3 {
		t.Fatalf("应带 3/10/30 三个窗口：%+v", item.Windows)
	}
	if item.Dev3 != 0 || item.Dev10 != 0 || item.Dev30 != 0 {
		t.Fatalf("偏离值应全为 0：%+v", item)
	}
	// 名称从关注仓回填：单票路径没有快照行。
	if item.Name != "平安银行" {
		t.Fatalf("名称应回填关注仓备注名，got %q", item.Name)
	}
}

func TestAbnormalItemForSymbolMissingBars(t *testing.T) {
	engine := newAbnormalEngine(t, errKline{})
	if _, ok := engine.abnormalItemForSymbol(context.Background(), "000001.SZ", nil); ok {
		t.Fatalf("日线拉不到时应安静返回 false，不能硬造行")
	}
}

// fakeRealtime 给实时行情名称兜底：标的没进关注仓也没进持仓时问它一次。
type fakeRealtime struct{}

func (fakeRealtime) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	quotes := make([]foundation.Quote, 0, len(symbols))
	for _, symbol := range symbols {
		quotes = append(quotes, foundation.Quote{Symbol: symbol, Name: "实时" + symbol})
	}
	return quotes, nil
}

func TestSymbolNamesFallbackToRealtime(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.AddWatchlist(ctx, "000001.SZ", "平安银行", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	engine := &Engine{store: store, realtime: fakeRealtime{}}
	names := engine.symbolNames(ctx, []string{"000001.SZ", "600000.SH"})
	// 关注仓自带名称优先，不再去问实时接口。
	if names["000001.SZ"] != "平安银行" {
		t.Fatalf("关注仓名称应优先，got %q", names["000001.SZ"])
	}
	// 剩下的走实时行情兜底：降级模式下列表不能只剩裸代码。
	if names["600000.SH"] != "实时600000.SH" {
		t.Fatalf("其余标的应走实时兜底，got %q", names["600000.SH"])
	}
	// 没有实时源时，清单外的标的安静地没有名字，不 panic 也不硬造。
	if plain := (&Engine{store: store}).symbolNames(ctx, []string{"600000.SH"}); plain["600000.SH"] != "" {
		t.Fatalf("无兜底源时未知标的应无名：%+v", plain)
	}
}

func TestLastAbnormalTriggerDate(t *testing.T) {
	f := func(value float64) *float64 { return &value }
	rows := []AbnormalCalcRow{
		{Date: "2026-09-01", Dev3: f(3), Dev10: f(9), Dev30: f(15)},
		// 主板 3 日偏离 +21 越过 ±20：这天算一次触发。
		{Date: "2026-09-04", Dev3: f(21), Dev10: f(30), Dev30: f(40)},
		{Date: "2026-09-08", Dev3: f(9), Dev10: f(88), Dev30: f(120)},
		// 10 日偏离 +101 越过 +100（严重异动），比 09-04 更近。
		{Date: "2026-09-10", Dev3: f(5), Dev10: f(101), Dev30: f(150)},
		// 30 日偏离 -71 越过 -70：取最近日期。
		{Date: "2026-09-15", Dev3: f(-4), Dev10: f(-20), Dev30: f(-71)},
	}
	if got := lastAbnormalTriggerDate(rows, foundation.BoardMain, false); got != "2026-09-15" {
		t.Fatalf("应取最近一次触发的交易日，got %q", got)
	}

	// 创业板阈值 30：+28 在 3 日窗口不算触发。
	growth := []AbnormalCalcRow{{Date: "2026-09-20", Dev3: f(28), Dev10: f(50), Dev30: f(60)}}
	if got := lastAbnormalTriggerDate(growth, foundation.BoardChiNext, false); got != "" {
		t.Fatalf("创业板 3 日 +28 未到 ±30，不应判触发，got %q", got)
	}
	if got := lastAbnormalTriggerDate(nil, foundation.BoardMain, false); got != "" {
		t.Fatalf("无明细时应返回空串，got %q", got)
	}
}

// deviationViewSnapshot 给一只票的实时快照（现价=昨收，涨跌幅 0），
// 让偏离值只由日线决定，断言好写。
type deviationViewSnapshot struct{}

func (deviationViewSnapshot) MarketSnapshot(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	return []foundation.MarketSnapshotRow{
		{Symbol: "600000.SH", Name: "浦发银行", Board: foundation.BoardMain, Price: 130, PreviousClose: 100, ChangePercent: 30},
	}, foundation.SourceMeta{}, nil
}

// risingKline：前 39 根 100，最后一根 130 —— 3/10/30 日偏离都是 +30，
// 主板阈值 ±20，判定为已触发。
type risingKline struct{}

func (risingKline) KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	return testBars(append(fill(100, 39), 130), nil), nil
}

// 页面不能使用陈旧缓存：引擎暂停时 e.deviations 没人刷新，若只读缓存，
// 偏离值会停在几小时前，而现价来自最新快照——两个数字对不上就是用户看到的"数据错了"。
func TestAbnormalViewUsesFreshDeviationNotStaleCache(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	// 加入监控：侧栏卡片要展示的「触发日期」只对监控个股计算。
	if err := store.AddWatchlist(ctx, "600000.SH", "浦发银行", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	lastBar := testBars(append(fill(100, 39), 130), nil)
	engine := &Engine{
		store:       store,
		kline:       risingKline{},
		index:       fakeIndexSeries{},
		indexSeries: map[string]cachedIndexSeries{},
		klines:      map[string]klineEntry{},
		snapshot:    deviationViewSnapshot{},
		deviations: map[string]deviationResult{
			// 两小时前的旧值：3 日 88 / 10 日 99 / 30 日 120。
			"600000.SH": {Dev3: 88, Dev10: 99, Dev30: 120, FetchedAt: time.Now().Add(-2 * time.Hour)},
		},
		highLow: map[string]highLowResult{},
		signals: map[string]signalResult{},
		now:     func() time.Time { return time.Now() },
	}
	view, err := engine.Abnormal(ctx, "deviation", 50)
	if err != nil {
		t.Fatalf("异动视图不应报错：%v", err)
	}
	if len(view.Items) != 1 {
		t.Fatalf("应有一行，got %d", len(view.Items))
	}
	item := view.Items[0]
	if math.Abs(item.Dev3-30) > 0.01 {
		t.Fatalf("偏离值应为现算的 30，不能用缓存里的 88，got %v", item.Dev3)
	}
	wantDate := lastBar[len(lastBar)-1].Time.In(foundation.AStockLocation).Format("2006-01-02")
	if item.LastTriggerDate != wantDate {
		t.Fatalf("触发日期应为 %s，got %q", wantDate, item.LastTriggerDate)
	}
	// 现算结果同时回写缓存，下一次读取才是新的。
	engine.mu.Lock()
	cached := engine.deviations["600000.SH"]
	engine.mu.Unlock()
	if math.Abs(cached.Dev3-30) > 0.01 {
		t.Fatalf("缓存应被现算值覆盖为 30，got %v", cached.Dev3)
	}
}

// 「观察级」不是异动：阈值走完 50%~70% 的票不该出现在预警列表里。
// 这条规则挡住的是用户实际看到的那一幕——持仓涨了 13.87%（3 日阈值 20%，
// 进度 69%）被当成异动挂在列表里，既没有触发，也没有入口删掉。
func TestAbnormalWorthListingScopesObservation(t *testing.T) {
	cases := []struct {
		level     string
		monitored bool
		want      bool
	}{
		{"normal", false, false},
		{"watch", false, false},
		{"near", false, true},
		{"triggered", false, true},
		// 用户自己加的监控：无论到哪一级都要留着看距离。
		{"normal", true, true},
		{"watch", true, true},
	}
	for _, item := range cases {
		if got := abnormalWorthListing(item.level, item.monitored); got != item.want {
			t.Fatalf("level=%s monitored=%v 应为 %v，got %v", item.level, item.monitored, item.want, got)
		}
	}
}

// 严重异动只能由 10 日 / 30 日两个长窗口判定：3 日越线属于普通异常波动。
func TestAbnormalSevereOnlyLongWindows(t *testing.T) {
	// 3 日 +21 越过主板 ±20，但 10/30 日离阈值很远。
	shortOnly := evalAbnormalWindows(21, 30, 40, foundation.BoardMain, false, 10)
	if abnormalSevere(shortOnly) {
		t.Fatal("3 日越线只是普通异常波动，不应判为严重异动")
	}
	// 10 日 +120 越过 +100（严重异常波动）。
	tenDay := evalAbnormalWindows(5, 120, 150, foundation.BoardMain, false, 10)
	if !abnormalSevere(tenDay) {
		t.Fatal("10 日 +120 应判为严重异动")
	}
	// 30 日 -71 越过 -70。
	thirtyDay := evalAbnormalWindows(1, 2, -71, foundation.BoardMain, false, 10)
	if !abnormalSevere(thirtyDay) {
		t.Fatal("30 日 -71 应判为严重异动")
	}
}

func TestAbnormalThresholdPrecisionDoesNotAdvanceState(t *testing.T) {
	cases := []struct {
		name               string
		dev3, dev10, dev30 float64
		level              string
		severe             bool
	}{
		{"below_near", 13.99, 0, 0, "watch", false},
		{"below_ordinary", 19.91, 0, 0, "near", false},
		{"almost_ordinary", 19.99, 0, 0, "near", false},
		{"at_ordinary", 20, 0, 0, "triggered", false},
		{"below_severe_up", 0, 99.60, 0, "near", false},
		{"almost_severe_up", 0, 99.99, 0, "near", false},
		{"at_severe_up", 0, 100, 0, "triggered", true},
		{"below_severe_down", 0, -49.99, 0, "near", false},
		{"at_severe_down", 0, -50, 0, "triggered", true},
		{"below_30d_up", 0, 0, 199.99, "near", false},
		{"at_30d_up", 0, 0, 200, "triggered", true},
		{"below_30d_down", 0, 0, -69.99, "near", false},
		{"at_30d_down", 0, 0, -70, "triggered", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			windows := evalAbnormalWindows(tc.dev3, tc.dev10, tc.dev30, foundation.BoardMain, false, 10)
			if got := abnormalLevel(windows); got != tc.level || abnormalSevere(windows) != tc.severe {
				t.Fatalf("level=%s severe=%v, want %s/%v; windows=%+v", got, abnormalSevere(windows), tc.level, tc.severe, windows)
			}
		})
	}
}

func TestAbnormalBSELongWindowThresholds(t *testing.T) {
	cases := []struct {
		name         string
		dev10, dev30 float64
		severe       bool
	}{
		{"below_10d_up", 120, 0, false}, {"at_10d_up", 150, 0, true},
		{"below_10d_down", -55, 0, false}, {"at_10d_down", -60, 0, true},
		{"below_30d_up", 0, 250, false}, {"at_30d_up", 0, 300, true},
		{"below_30d_down", 0, -72, false}, {"at_30d_down", 0, -75, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			windows := evalAbnormalWindows(0, tc.dev10, tc.dev30, foundation.BoardBSE, false, 10)
			if windows[1].ThresholdUp != 150 || windows[1].ThresholdDown != 60 || windows[2].ThresholdUp != 300 || windows[2].ThresholdDown != 75 {
				t.Fatalf("BSE thresholds are not board-specific: %+v", windows)
			}
			if got := abnormalSevere(windows); got != tc.severe {
				t.Fatalf("severe=%v, want %v; windows=%+v", got, tc.severe, windows)
			}
			rows := []AbnormalCalcRow{{Date: "2026-10-26", Dev10: &tc.dev10, Dev30: &tc.dev30}}
			if got := lastAbnormalTriggerDate(rows, foundation.BoardBSE, false); (got != "") != tc.severe {
				t.Fatalf("trigger date=%q must use the same board thresholds as severe=%v", got, tc.severe)
			}
		})
	}
}

type abnormalSampleKline struct{ bars []foundation.KLine }

func (source abnormalSampleKline) KLine(context.Context, string, string, int) ([]foundation.KLine, error) {
	return source.bars, nil
}

func TestAbnormalPartialWindowsDoNotFabricateZero(t *testing.T) {
	for _, count := range []int{3, 6, 15, 40} {
		bars := testBars(fill(100, count), nil)
		engine := newAbnormalEngine(t, abnormalSampleKline{bars: bars})
		engine.now = func() time.Time { return bars[len(bars)-1].Time.Add(72 * time.Hour) }
		view, err := engine.AbnormalCalc(context.Background(), "600000.SH")
		if err != nil {
			t.Fatal(err)
		}
		wantCount := 0
		for i, days := range []int{3, 10, 30} {
			key := []string{AbnormalWindow3d, AbnormalWindow10d, AbnormalWindow30d}[i]
			if view.WindowAvailability[key] != (count > days) {
				t.Fatalf("%d bars: incorrect availability for %s: %+v", count, key, view)
			}
			if count > days {
				wantCount++
			}
		}
		if len(view.Windows) != wantCount {
			t.Fatalf("%d bars: got %d windows, want %d", count, len(view.Windows), wantCount)
		}
		for _, window := range view.Windows {
			if !view.WindowAvailability[window.Key] || window.Dev != 0 {
				t.Fatalf("valid zero and unavailable windows must be distinguished: %+v", view)
			}
			if window.TriggerPrice != 0 {
				t.Fatalf("historical windows cannot project current prices: %+v", window)
			}
		}
		if wantCount == 0 && view.Level != "" {
			t.Fatalf("no valid windows must not be called normal: %+v", view)
		}
		if wantCount < 3 && view.Error == "" {
			t.Fatalf("partial windows require a warning: %+v", view)
		}
		if len(view.Rows) == 0 {
			t.Fatalf("available daily rows were discarded: %+v", view)
		}
	}
}
