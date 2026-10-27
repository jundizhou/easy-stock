package monitor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type sidebarKline struct{}

func (sidebarKline) KLine(_ context.Context, symbol, _ string, _ int) ([]foundation.KLine, error) {
	switch sixDigitCode(symbol) {
	case "600090":
		return testBars(append(fill(80, 62), 20), nil), nil
	case "600091":
		return testBars(fill(80, 3), nil), nil
	case "600092":
		return testBars(fill(80, 70), nil), nil
	default:
		return testBars(append(fill(80, 62), 200), nil), nil
	}
}

func TestSidebarRadarRetainsFullCountsAndEveryTarget(t *testing.T) {
	ctx, store := context.Background(), newTestStore(t)
	for index := 0; index < 42; index++ {
		if err := store.AddWatchlist(ctx, fmt.Sprintf("%06d.SH", 600000+index), "测试夹具", ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, code := range []string{"600090.SH", "600091.SH", "600092.SH"} {
		if err := store.AddWatchlist(ctx, code, "测试夹具", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpsertRule(ctx, Rule{ID: "alias", Type: RuleTypePrice, Scope: ScopeSymbols, Symbols: []string{"600001"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	engine := &Engine{store: store, kline: sidebarKline{}, portfolio: fakeRadarPortfolio{}, klines: map[string]klineEntry{}, now: time.Now}
	report := engine.RadarScan(ctx)
	if report.Scanned != 45 || len(report.Targets) != 45 {
		t.Fatalf("同一标的不同代码形式必须去重且保留逐票状态: scanned=%d targets=%d", report.Scanned, len(report.Targets))
	}
	if len(report.Findings) != 40 || report.TotalFindings != 43 || report.TotalCounts["opportunity"] != 42 || report.TotalCounts["risk"] != 1 {
		t.Fatalf("完整统计不得受前40条展示限制影响: findings=%d total=%d counts=%v", len(report.Findings), report.TotalFindings, report.TotalCounts)
	}
	if report.Counts["opportunity"]+report.Counts["risk"]+report.Counts["watch"] != 40 {
		t.Fatal("原 counts 字段必须保持返回列表口径")
	}
	if report.SourceCounts[radarSourcePosition] != 1 || report.SourceCounts[radarSourceWatch] != 44 || report.Skipped != 1 {
		t.Fatalf("来源应互斥，短日线应计为不足: sources=%v skipped=%d", report.SourceCounts, report.Skipped)
	}
	for _, target := range report.Targets {
		switch sixDigitCode(target.Symbol) {
		case "600091":
			if target.Status != "insufficient_data" || target.Kind != "" {
				t.Fatalf("样本不足不能伪装成无风险: %+v", target)
			}
		case "600092":
			if target.Status != "evaluated" || target.Kind != "" || target.DailyAsOf == "" {
				t.Fatalf("无信号与未扫描必须区分: %+v", target)
			}
		case "600001":
			if target.Source != radarSourceWatch {
				t.Fatalf("不同代码形式的关注仓来源必须优先于规则来源: %+v", target)
			}
		}
	}
}

func TestSidebarAbnormalMetadataUsesActualDailyDate(t *testing.T) {
	ctx := context.Background()
	bars := testBars(append(fill(100, 39), 130), nil)
	lastDate := barDate(bars[len(bars)-1])
	now := bars[len(bars)-1].Time.Add(72 * time.Hour)
	engine := newAbnormalEngine(t, risingKline{})
	engine.store = newTestStore(t)
	engine.now = func() time.Time { return now }
	if err := engine.store.AddWatchlist(ctx, "600000.SH", "测试夹具", ""); err != nil {
		t.Fatal(err)
	}
	item, ok := engine.abnormalItemForSymbol(ctx, "600000.SH", nil)
	if !ok || item.DailyAsOf != lastDate || item.CalculatedAt == nil || !item.CalculatedAt.Equal(now) || !item.Historical {
		t.Fatalf("单票元数据必须保留实际日线日和历史计算标记: %+v", item)
	}
	computed := engine.computeCandidates(ctx, []string{"600000.SH"}, now)
	dev, ok := computed.dev["600000.SH"]
	if !ok || dev.DailyAsOf != lastDate || !dev.FetchedAt.Equal(now) || !dev.Stale {
		t.Fatalf("快照路径必须使用同一份计算时间与实际日线日期: %+v", dev)
	}
	view := AbnormalView{}
	if !engine.fillAbnormalWithoutSnapshot(ctx, &view) || len(view.Items) != 1 || view.Items[0].DailyAsOf != lastDate || !view.Items[0].Historical || view.Items[0].CalculatedAt == nil {
		t.Fatalf("缺快照降级路径不能丢失日线元数据: %+v", view)
	}
}

func TestSidebarCoverageUsesEnabledScopesAndActualFilters(t *testing.T) {
	ctx, store := context.Background(), newTestStore(t)
	rows := []foundation.MarketSnapshotRow{
		{Symbol: "600001.SH", Price: 10, IsST: true, Amount: 100},
		{Symbol: "600002.SH", Price: 10, Amount: 100},
		{Symbol: "600003.SH", Price: 10, Amount: 20},
	}
	engine := &Engine{store: store, now: time.Now, snapRows: rows, status: Status{Running: true}, settings: func() Settings { return Settings{Enabled: false} }}
	items := []WatchItem{{Symbol: "600001.SH"}, {Symbol: "600002.SH"}, {Symbol: "600003.SH"}, {Symbol: "600004.SH"}}
	rule := Rule{ID: "all", Name: "测试规则", Type: RuleTypePrice, Scope: ScopeAll, Enabled: true, Filters: RuleFilters{ExcludeST: true, MinAmount: 50}}
	if err := store.UpsertRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRule(ctx, Rule{ID: "disabled", Type: RuleTypePrice, Scope: ScopeSymbols, Symbols: []string{"600004.SH"}, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	coverage := engine.WatchCoverage(ctx, items)
	want := []string{"unconfigured", "covered", "unconfigured", "unknown"}
	for index, item := range coverage.Items {
		if item.Status != want[index] {
			t.Fatalf("覆盖必须使用过滤结果，缺快照必须未知: %+v want=%s", item, want[index])
		}
	}
	if coverage.Engine.Enabled || !coverage.Engine.Running || coverage.Items[1].RuleCount != 1 {
		t.Fatal("暂停状态必须与配置覆盖分开，不能伪装成提醒运行中")
	}
	// 非全市场规则的 Filters 按现有引擎语义不参与筛选，显示层不能另写一套判断。
	rule.Scope, rule.Symbols = ScopeSymbols, []string{"600001.SH"}
	if err := store.UpsertRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if got := engine.WatchCoverage(ctx, items); got.Items[0].Status != "covered" || got.Items[2].Status != "unconfigured" {
		t.Fatalf("必须复用指定标的作用域: %+v", got.Items)
	}
}

func TestSidebarCoverageHonorsTaskSelectionLimits(t *testing.T) {
	for _, ruleType := range []string{RuleTypePrice, RuleTypeSignal} {
		t.Run(ruleType, func(t *testing.T) {
			ctx, store := context.Background(), newTestStore(t)
			rows := make([]foundation.MarketSnapshotRow, scanRowLimit+1)
			for index := range rows {
				rows[index] = foundation.MarketSnapshotRow{Symbol: fmt.Sprintf("%06d.SH", 600000+index), Price: 10}
			}
			if err := store.UpsertRule(ctx, Rule{ID: "limit", Type: ruleType, Scope: ScopeAll, Enabled: true, Filters: RuleFilters{MaxSymbols: 1}}); err != nil {
				t.Fatal(err)
			}
			engine := &Engine{store: store, now: time.Now, snapRows: rows}
			got := engine.WatchCoverage(ctx, []WatchItem{{Symbol: rows[0].Symbol}, {Symbol: rows[1].Symbol}, {Symbol: rows[scanRowLimit].Symbol}})
			wantSecond := "covered"
			if ruleType == RuleTypeSignal {
				wantSecond = "unconfigured"
			}
			if got.Items[0].Status != "covered" || got.Items[1].Status != wantSecond || got.Items[2].Status != "unconfigured" {
				t.Fatalf("覆盖范围必须与任务的现有截断行为一致: %+v", got.Items)
			}
		})
	}
}
