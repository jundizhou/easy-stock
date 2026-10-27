package monitor

import (
	"context"
	"testing"
	"time"
)

// fakeInspectionHoldings 模拟巡检组合：两只票，其中一只与持仓重叠，
// 用来验证「巡检持仓」作用域与雷达扫描范围的合并去重。
type fakeInspectionHoldings struct{}

func (fakeInspectionHoldings) LatestInspectionHoldings(ctx context.Context) ([]ScopeTarget, error) {
	return []ScopeTarget{
		{Symbol: "600000", Name: "浦发银行"},
		{Symbol: "600519", Name: "贵州茅台"},
	}, nil
}

func TestResolveRuleSymbolsInspection(t *testing.T) {
	engine := &Engine{
		portfolio:          fakeRadarPortfolio{},
		inspectionHoldings: fakeInspectionHoldings{},
	}
	rule := Rule{Scope: ScopeInspection}
	symbols, err := engine.resolveRuleSymbols(context.Background(), rule)
	if err != nil {
		t.Fatalf("解析巡检作用域不应报错：%v", err)
	}
	if len(symbols) != 2 || symbols[0] != "600000" || symbols[1] != "600519" {
		t.Fatalf("巡检作用域应取巡检组合的代码：%v", symbols)
	}
	// 没有巡检源时返回空切片而不是报错：独立部署模式不该因此挡住规则。
	empty := &Engine{}
	if got, err := empty.resolveRuleSymbols(context.Background(), rule); err != nil || len(got) != 0 {
		t.Fatalf("无巡检源应安静返回空：%v %v", got, err)
	}
}

func TestScopeTargetsIncludesInspection(t *testing.T) {
	store := newTestStore(t)
	if err := store.AddWatchlist(context.Background(), "000001.SZ", "平安银行", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	engine := &Engine{
		store:              store,
		portfolio:          fakeRadarPortfolio{},
		inspectionHoldings: fakeInspectionHoldings{},
	}
	targets := engine.ScopeTargets(context.Background())
	if len(targets[ScopeWatchlist]) != 1 || targets[ScopeWatchlist][0].Symbol != "000001.SZ" {
		t.Fatalf("关注仓应来自监控自己的清单：%+v", targets[ScopeWatchlist])
	}
	// 巡检持仓与 AI持仓已合并：600000（巡检+持仓重叠只算一次）、600519（巡检）。
	merged := targets[ScopePositions]
	if len(merged) != 2 || merged[0].Symbol != "600000" || merged[1].Symbol != "600519" {
		t.Fatalf("持仓应为巡检+AI 的并集去重：%+v", merged)
	}
	// inspection 键与 positions 同一份合并结果（历史规则兼容）。
	if len(targets[ScopeInspection]) != 2 {
		t.Fatalf("inspection 兼容键应为同一份合并结果：%+v", targets[ScopeInspection])
	}
	// 名称优先用数据源自带的，取不到再从快照补。
	if merged[1].Name != "贵州茅台" {
		t.Fatalf("持仓名称应保留：%q", merged[1].Name)
	}
}

func TestRadarScanIncludesInspectionHoldings(t *testing.T) {
	store := newTestStore(t)
	if err := store.AddWatchlist(context.Background(), "000001.SZ", "平安银行", ""); err != nil {
		t.Fatalf("写入关注仓不应报错：%v", err)
	}
	engine := &Engine{
		store:              store,
		kline:              fakeKline{},
		portfolio:          fakeRadarPortfolio{},
		inspectionHoldings: fakeInspectionHoldings{},
		klines:             map[string]klineEntry{},
		signals:            map[string]signalResult{},
		now:                func() time.Time { return time.Now() },
	}
	report := engine.RadarScan(context.Background())
	if report.Error != "" {
		t.Fatalf("扫描不应报错：%s", report.Error)
	}
	// 持仓 600000 + 关注仓 000001 + 巡检 600519，重叠的 600000 只算一次。
	if report.Scanned != 3 {
		t.Fatalf("扫描范围应含巡检持仓共 3 只，实际 %d", report.Scanned)
	}
}

func TestWatchlistStoreCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.AddWatchlist(ctx, "600519.SH", "贵州茅台", "等回踩"); err != nil {
		t.Fatalf("加入不应报错：%v", err)
	}
	// 重复加入是幂等的，只更新名称、保留原备注。
	if err := store.AddWatchlist(ctx, "600519.SH", "贵州茅台", ""); err != nil {
		t.Fatalf("重复加入不应报错：%v", err)
	}
	if err := store.AddWatchlist(ctx, "000858.SZ", "五粮液", ""); err != nil {
		t.Fatalf("加入第二支不应报错：%v", err)
	}
	entries, err := store.ListWatchlist(ctx)
	if err != nil {
		t.Fatalf("列出不应报错：%v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("应有 2 支：%+v", entries)
	}
	if entries[0].Symbol != "000858.SZ" {
		t.Fatalf("新加的应排在前面：%+v", entries)
	}
	if entries[1].Note != "等回踩" {
		t.Fatalf("重复加入不应清掉原备注：%+v", entries[1])
	}
	if err := store.RemoveWatchlist(ctx, "600519.SH"); err != nil {
		t.Fatalf("移除不应报错：%v", err)
	}
	// 移除不存在的同样安静。
	if err := store.RemoveWatchlist(ctx, "600519.SH"); err != nil {
		t.Fatalf("重复移除不应报错：%v", err)
	}
	if err := store.ClearWatchlist(ctx); err != nil {
		t.Fatalf("清空不应报错：%v", err)
	}
	if entries, _ := store.ListWatchlist(ctx); len(entries) != 0 {
		t.Fatalf("清空后应为空：%+v", entries)
	}
	if err := store.AddWatchlist(ctx, "  ", "", ""); err == nil {
		t.Fatalf("空代码应被拒绝")
	}
}

func TestInspectionTargetsPrefersSnapshot(t *testing.T) {
	store := newTestStore(t)
	engine := &Engine{
		store:              store,
		inspectionHoldings: fakeInspectionHoldings{},
	}
	// 没有快照：退回历史报告（2 只）。
	if got := engine.inspectionTargets(context.Background()); len(got) != 2 {
		t.Fatalf("无快照应回退报告：%+v", got)
	}
	// 前端推来当前草稿（1 只）：以后者为准，历史报告不再混进来。
	if _, _, err := engine.SetInspectionHoldings(context.Background(), []ScopeTarget{{Symbol: "300058.SZ", Name: "蓝色光标"}}); err != nil {
		t.Fatalf("写入快照不应报错：%v", err)
	}
	got := engine.inspectionTargets(context.Background())
	if len(got) != 1 || got[0].Symbol != "300058.SZ" || got[0].Name != "蓝色光标" {
		t.Fatalf("快照应覆盖历史报告：%+v", got)
	}
	// 清空草稿后不再回退报告：用户把计划删空了就该没持仓，不能又冒出老清单。
	if _, _, err := engine.SetInspectionHoldings(context.Background(), nil); err != nil {
		t.Fatalf("清空快照不应报错：%v", err)
	}
	if got := engine.inspectionTargets(context.Background()); len(got) != 0 {
		t.Fatalf("清空后不应回退历史报告：%+v", got)
	}
}

func TestResolveRuleSymbolsMergesPositions(t *testing.T) {
	store := newTestStore(t)
	engine := &Engine{
		store:              store,
		portfolio:          fakeRadarPortfolio{},
		inspectionHoldings: fakeInspectionHoldings{},
	}
	// positions 与 inspection 都解析为并集：600000 在两边都有只算一次，
	// AI 独有的没了（fake 只有 600000），巡检独有的 600519 并进来。
	for _, scope := range []string{ScopePositions, ScopeInspection} {
		symbols, err := engine.resolveRuleSymbols(context.Background(), Rule{Scope: scope})
		if err != nil {
			t.Fatalf("解析 %s 不应报错：%v", scope, err)
		}
		if len(symbols) != 2 || symbols[0] != "600000" || symbols[1] != "600519" {
			t.Fatalf("%s 应为合并去重结果：%v", scope, symbols)
		}
	}
	// 无数据源时安静返回空。
	empty := &Engine{}
	symbols, err := empty.resolveRuleSymbols(context.Background(), Rule{Scope: ScopePositions})
	if err != nil || len(symbols) != 0 {
		t.Fatalf("无数据源应安静返回空：%v %v", symbols, err)
	}
}
