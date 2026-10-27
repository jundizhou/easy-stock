package monitor

import (
	"context"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

// 雷达计算核的场景测试：急涨给机会、急跌给风险、横盘沉默。

func TestRadarFindingOpportunity(t *testing.T) {
	// 62 根 80 后一根暴涨到 200：MA5 上穿 + MACD 金叉 + 多头排列，
	// 净分必然过「机会」门槛，并带 60 日高位标签。
	bars := testBars(append(fill(80, 62), 200), nil)
	finding, ok := radarFindingFromBars("600000", bars, nil, false, nil)
	if !ok {
		t.Fatalf("急涨应给出结论")
	}
	if finding.Kind != "opportunity" {
		t.Fatalf("kind = %q，应为 opportunity（score=%.2f）", finding.Kind, finding.Score)
	}
	if float64(finding.Score) < radarScoreMin {
		t.Fatalf("score = %.2f，应 ≥ %d", finding.Score, radarScoreMin)
	}
	if len(finding.Signals) == 0 {
		t.Fatalf("机会结论应带技术信号：%+v", finding)
	}
	if !containsString(finding.Tags, "多头排列") {
		t.Fatalf("急涨应识别多头排列：%v", finding.Tags)
	}
	if !containsString(finding.Tags, "60日高位") {
		t.Fatalf("收盘创区间新高应标 60日高位：%v", finding.Tags)
	}
	if len(finding.Reasons) == 0 {
		t.Fatalf("结论必须有可读理由：%+v", finding)
	}
}

func TestRadarFindingRisk(t *testing.T) {
	bars := testBars(append(fill(80, 62), 20), nil)
	finding, ok := radarFindingFromBars("600000", bars, nil, true, nil)
	if !ok {
		t.Fatalf("急跌应给出结论")
	}
	if finding.Kind != "risk" {
		t.Fatalf("kind = %q，应为 risk（score=%.2f）", finding.Kind, finding.Score)
	}
	if !finding.IsPosition {
		t.Fatalf("传入持仓标记应保留")
	}
}

func TestRadarFindingQuietSkipped(t *testing.T) {
	// 长期横盘：无信号、无形态、无位置极值——雷达应沉默而不是硬凑数。
	if _, ok := radarFindingFromBars("600000", testBars(fill(100, 70), nil), nil, false, nil); ok {
		t.Fatalf("横盘标的应被跳过")
	}
}

func TestRadarFindingSuggestedRule(t *testing.T) {
	// 机会级：预填一条 signal 规则（info 级）。
	bars := testBars(append(fill(80, 62), 200), nil)
	finding, _ := radarFindingFromBars("600000", bars, nil, false, nil)
	if finding.SuggestedRule == nil {
		t.Fatalf("机会结论应带规则预填")
	}
	seed := finding.SuggestedRule
	if seed.Type != RuleTypeSignal || seed.Scope != ScopeSymbols || len(seed.Symbols) != 1 {
		t.Fatalf("预填规则结构错误：%+v", seed)
	}
	if len(seed.Conditions) != 1 || !isSignalField(seed.Conditions[0].Field) {
		t.Fatalf("预填条件应是合法信号字段：%+v", seed.Conditions)
	}
	// 回归：row=nil（快照行缺失）时预填规则也必须带代码，否则一键转规则
	// 会被后端以「标的范围至少需要一个代码」拒绝。
	if len(seed.Symbols) != 1 || seed.Symbols[0] != "600000" {
		t.Fatalf("预填标的代码缺失：%+v", seed.Symbols)
	}
	if seed.Severity != SeverityInfo {
		t.Fatalf("机会级预填应为 info，实际 %q", seed.Severity)
	}
	// 风险级：预填规则的严重级别升为 warning。
	risk, _ := radarFindingFromBars("600000", testBars(append(fill(80, 62), 20), nil), nil, false, nil)
	if risk.SuggestedRule == nil || risk.SuggestedRule.Severity != SeverityWarning {
		t.Fatalf("风险级预填应为 warning：%+v", risk.SuggestedRule)
	}
}

func TestRadarFindingSuggestedRuleForWatch(t *testing.T) {
	// 缓慢抬升：有均线多头排列（观察级，score=1.5）但没有事件级信号。
	// 这种结论以前拿不到规则预填，操作列就少一个「转为规则」；现在必须
	// 用当时下判断的技术事实（60 日位置）补一条等价规则。
	closes := make([]float64, 0, 70)
	for index := 0; index < 70; index++ {
		closes = append(closes, 80*(1+0.004*float64(index)))
	}
	finding, ok := radarFindingFromBars("600000", testBars(closes, nil), nil, false, nil)
	if !ok {
		t.Fatalf("温和上行应给出观察结论")
	}
	if finding.Kind != "watch" {
		t.Fatalf("kind = %q，应为 watch（score=%.2f）", finding.Kind, finding.Score)
	}
	if len(finding.Signals) != 0 {
		t.Fatalf("本场景不应有事件级信号：%v", finding.Signals)
	}
	seed := finding.SuggestedRule
	if seed == nil {
		t.Fatalf("观察级结论也必须带规则预填")
	}
	if seed.Type != RuleTypeAbnormal {
		t.Fatalf("60 日高位应预填 abnormal 规则，实际 %q", seed.Type)
	}
	if len(seed.Conditions) != 1 || seed.Conditions[0].Field != "high_60d" {
		t.Fatalf("预填条件应是 high_60d：%+v", seed.Conditions)
	}
	if len(seed.Symbols) != 1 || seed.Symbols[0] != "600000" {
		t.Fatalf("预填标的代码缺失：%+v", seed.Symbols)
	}
	// 预填内容必须能通过后端校验，否则按钮点了也会被拒。
	if _, err := NormalizeRule(Rule{
		Name: seed.Name, Type: seed.Type, Severity: seed.Severity, Scope: seed.Scope,
		Symbols: seed.Symbols, Conditions: seed.Conditions, Logic: seed.Logic,
		CooldownSeconds: seed.CooldownSeconds,
	}); err != nil {
		t.Fatalf("预填规则未通过校验：%v", err)
	}
}

func TestRadarFindingUsesSnapshotRow(t *testing.T) {
	// 快照行存在时名称与现价以它为准，信号结论不受影响。
	bars := testBars(append(fill(80, 62), 200), nil)
	row := foundation.MarketSnapshotRow{Symbol: "600000", Name: "浦发银行", Price: 199.5, ChangePercent: 3.2}
	finding, ok := radarFindingFromBars("600000", bars, &row, false, nil)
	if !ok {
		t.Fatalf("应给出结论")
	}
	if finding.Name != "浦发银行" || finding.Symbol != "600000" {
		t.Fatalf("名称/代码应来自快照行：%+v", finding)
	}
	if finding.Price != 199.5 || finding.ChangePercent != 3.2 {
		t.Fatalf("现价/涨跌幅应来自快照行：%.2f %.2f", finding.Price, finding.ChangePercent)
	}
}

// fakeRadarPortfolio 提供一持仓一自选，验证扫描范围装配。
type fakeRadarPortfolio struct{}

func (fakeRadarPortfolio) Holdings(ctx context.Context) ([]Holding, error) {
	return []Holding{{Symbol: "600000", Name: "浦发银行", Quantity: 100}}, nil
}

func (fakeRadarPortfolio) WatchSymbols(ctx context.Context) ([]string, error) {
	return []string{"000001"}, nil
}

func TestRadarFindingInspectionConclusion(t *testing.T) {
	bars := testBars(fill(100, 70), nil)
	// 普通优先级（保持/观察）：只做事实补充，技术面安静的横盘股
	// 仍然沉默，避免持仓全量刷屏。
	inspection := &InspectionConclusion{Symbol: "600000", Conclusion: "组合内作为底仓持有", Action: "600000：保持观察", ActionPriority: "保持", EvaluatedAt: "2026-10-23"}
	if _, ok := radarFindingFromBars("600000", bars, nil, false, inspection); ok {
		t.Fatalf("普通巡检结论不应让横盘股进入雷达")
	}
	// 有技术发现的标的：巡检文本并入理由区。
	active := testBars(append(fill(80, 62), 200), nil)
	finding, ok := radarFindingFromBars("600000", active, nil, false, inspection)
	if !ok {
		t.Fatalf("应有结论")
	}
	if !containsString(finding.Tags, "AI巡检") {
		t.Fatalf("巡检标签缺失：%v", finding.Tags)
	}
	found := false
	for _, reason := range finding.Reasons {
		if strings.Contains(reason, "持仓AI巡检（2026-10-23）") {
			found = true
		}
	}
	if !found {
		t.Fatalf("巡检理由缺失：%v", finding.Reasons)
	}
	// 「优先处理」：技术面安静也要抬进观察级。
	urgent := &InspectionConclusion{Symbol: "600000", Conclusion: "持有依据需要复核", ActionPriority: "优先处理", EvaluatedAt: "2026-10-23"}
	watch, ok := radarFindingFromBars("600000", bars, nil, false, urgent)
	if !ok {
		t.Fatalf("优先处理应让横盘持仓进观察级")
	}
	if watch.Kind != "watch" || !containsString(watch.Tags, "巡检优先处理") {
		t.Fatalf("优先处理标签/级别错误：%q %v", watch.Kind, watch.Tags)
	}
}

func TestRadarFindingSparkline(t *testing.T) {
	// 近 30 日收盘序列随结论返回：前端行内迷你走势图的数据源。
	closes := append(fill(80, 62), 200)
	bars := testBars(closes, nil)
	finding, ok := radarFindingFromBars("600000", bars, nil, false, nil)
	if !ok {
		t.Fatalf("应给出结论")
	}
	if len(finding.Closes) != 30 {
		t.Fatalf("迷你走势应取近 30 日，实际 %d 根", len(finding.Closes))
	}
	if finding.Closes[len(finding.Closes)-1] != 200 {
		t.Fatalf("序列末位应是最新收盘 %.2f", finding.Closes[len(finding.Closes)-1])
	}
	if finding.Closes[0] != closes[len(closes)-30] {
		t.Fatalf("序列首位应与日线对齐 %.2f vs %.2f", finding.Closes[0], closes[len(closes)-30])
	}
}

type fakeRadarSnapshot struct{}

func (fakeRadarSnapshot) MarketSnapshot(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	return []foundation.MarketSnapshotRow{
		{Symbol: "600000", Name: "浦发银行", Price: 10, ChangePercent: 3.2, VolumeRatio: 2.1},
		{Symbol: "000001", Name: "平安银行", Price: 12, ChangePercent: -1.5, VolumeRatio: 0.4},
	}, foundation.SourceMeta{}, nil
}

func TestRadarScanMarketAndMovers(t *testing.T) {
	store := newTestStore(t)
	if err := store.AddWatchlist(context.Background(), "000001.SZ", "平安银行", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	engine := &Engine{
		store:     store,
		kline:     fakeKline{},
		portfolio: fakeRadarPortfolio{},
		snapshot:  fakeRadarSnapshot{},
		klines:    map[string]klineEntry{},
		signals:   map[string]signalResult{},
		now:       func() time.Time { return time.Now() },
	}
	report := engine.RadarScan(context.Background())
	if report.Error != "" {
		t.Fatalf("扫描不应报错：%s", report.Error)
	}
	if report.Market == nil {
		t.Fatalf("大盘语境应随报告返回")
	}
	if !report.Market.HaveBreadth || report.Market.UpCount != 1 || report.Market.DownCount != 1 {
		t.Fatalf("宽度统计错误：%+v", report.Market)
	}
	if len(report.Movers) != 2 || report.Movers[0].Symbol != "600000" || report.Movers[0].ChangePercent != 3.2 {
		t.Fatalf("池内异动应按涨跌绝对值排序：%+v", report.Movers)
	}
}

func TestRadarScanEngine(t *testing.T) {
	store := newTestStore(t)
	// 关注仓现在来自监控自己的清单：写入一支，与 portfolio 持仓凑成 2 只。
	if err := store.AddWatchlist(context.Background(), "000001.SZ", "平安银行", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	engine := &Engine{
		store:     store,
		kline:     fakeKline{},
		portfolio: fakeRadarPortfolio{},
		klines:    map[string]klineEntry{},
		signals:   map[string]signalResult{},
		now:       func() time.Time { return time.Now() },
	}
	report := engine.RadarScan(context.Background())
	if report.Error != "" {
		t.Fatalf("扫描不应报错：%s", report.Error)
	}
	if report.Scanned != 2 {
		t.Fatalf("扫描范围应为持仓+自选共 2 只，实际 %d", report.Scanned)
	}
	if len(report.Findings) == 0 {
		t.Fatalf("fakeKline 全部急涨，应至少有一条结论：%s", report.Summary)
	}
	if report.Findings[0].Kind != "opportunity" {
		t.Fatalf("排序首位应是机会级：%+v", report.Findings[0])
	}
	if report.Counts["opportunity"]+report.Counts["risk"]+report.Counts["watch"] != len(report.Findings) {
		t.Fatalf("counts 与 findings 不一致：%v vs %d", report.Counts, len(report.Findings))
	}
	// 「来自」列：持仓票必须标成持仓，关注仓票必须标成关注仓——两者都在池里时，
	// 持仓优先级更高，不能因为同时被关注就变成关注仓。
	wantSource := map[string]string{"600000": radarSourcePosition, "000001": radarSourceWatch}
	for _, finding := range report.Findings {
		code := sixDigitCode(finding.Symbol)
		want, ok := wantSource[code]
		if !ok {
			t.Fatalf("结论里出现池外标的：%+v", finding)
		}
		if finding.Source != want {
			t.Fatalf("%s 的来源 = %q，应为 %q", finding.Symbol, finding.Source, want)
		}
	}
	// 60 秒缓存：第二次调用复用同一份结论（Scanned 一致且不发新请求）。
	second := engine.RadarScan(context.Background())
	if second.Scanned != report.Scanned || len(second.Findings) != len(report.Findings) {
		t.Fatalf("缓存失效：%d vs %d", second.Scanned, report.Scanned)
	}
}
