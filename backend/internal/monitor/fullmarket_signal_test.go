package monitor

import (
	"context"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

// fullMarketRows 三只票：主板、创业板、ST 主板各一只，用来验证
// 全市场信号规则的候选展开只吃 Filters 放行的行。
func fullMarketRows() []foundation.MarketSnapshotRow {
	return []foundation.MarketSnapshotRow{
		{Symbol: "600000.SH", Name: "浦发银行", Board: foundation.BoardMain, Price: 10, PreviousClose: 10, Amount: 5e8},
		{Symbol: "300001.SZ", Name: "特锐德", Board: foundation.BoardChiNext, Price: 20, PreviousClose: 20, Amount: 2e8},
		{Symbol: "600666.SH", Name: "ST环球", Board: foundation.BoardMain, Price: 5, PreviousClose: 5, IsST: true, Amount: 1e8},
		{Symbol: "600999.SH", Name: "招商证券", Board: foundation.BoardMain, Price: 15, PreviousClose: 15, Suspended: true},
	}
}

// TestFullMarketSignalRule 覆盖"技术信号 + 全市场"这条链路的三件事：
// 校验放行、默认扫描上限、候选展开按 Filters 收窄且覆盖率一致。
func TestFullMarketSignalRule(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	engine := &Engine{
		store: store,
		now:   func() time.Time { return time.Now() },
	}
	rule, err := engine.SaveRule(ctx, Rule{
		Name: "全市场金叉", Type: RuleTypeSignal, Scope: ScopeAll, Enabled: true,
		Filters:    RuleFilters{Boards: []string{foundation.BoardMain}, ExcludeST: true},
		Conditions: []Condition{{Field: SignalMACDGolden, Op: OpGTE, Value: 1}},
	})
	if err != nil {
		t.Fatalf("全市场信号规则应能保存：%v", err)
	}
	if rule.Filters.MaxSymbols != scanRowLimit {
		t.Fatalf("全市场规则应补默认扫描上限 %d，实际 %d", scanRowLimit, rule.Filters.MaxSymbols)
	}

	rows := fullMarketRows()
	// 只有主板、非 ST、未停牌的 600000 进候选池。
	symbols := engine.signalRuleSymbols(ctx, rows)
	if len(symbols) != 1 || symbols[0] != "600000.SH" {
		t.Fatalf("候选应只含过滤放行的标的：%v", symbols)
	}

	// 覆盖率与候选同口径：engine 评估时靠它判断某只票该不该套这条规则。
	engine.snapBySymbol = map[string]foundation.MarketSnapshotRow{}
	for _, row := range rows {
		engine.snapBySymbol[row.Symbol] = row
	}
	for symbol, want := range map[string]bool{
		"600000.SH": true,  // 主板 + 非 ST + 有成交
		"300001.SZ": false, // 板块不符
		"600666.SH": false, // ST
		"600999.SH": false, // 停牌
	} {
		covered, err := engine.ruleCoversSymbol(ctx, rule, symbol)
		if err != nil {
			t.Fatalf("判断 %s 覆盖率不应报错：%v", symbol, err)
		}
		if covered != want {
			t.Fatalf("%s 覆盖率应为 %v，实际 %v", symbol, want, covered)
		}
	}
}

// TestFullMarketSignalRuleNeverCoversUnknownSymbol 快照里没有的票不参与评估：
// 覆盖率必须安静返回 false，不能把停牌/退市标的当成命中。
func TestFullMarketSignalRuleNeverCoversUnknownSymbol(t *testing.T) {
	engine := &Engine{}
	covered, err := engine.ruleCoversSymbol(context.Background(), Rule{Scope: ScopeAll}, "600000.SH")
	if err != nil || covered {
		t.Fatalf("无快照时覆盖率应为 false 且不报错：%v %v", covered, err)
	}
}
