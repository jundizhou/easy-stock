package monitor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestLimitPriceRoundingPerBoard(t *testing.T) {
	cases := []struct {
		name          string
		symbol        string
		stockName     string
		previousClose float64
		wantLimit     float64
	}{
		{"主板10%四舍五入", "600519", "", 10.06, 11.07},
		{"主板普通", "600519", "", 10.03, 11.03},
		{"主板ST10%", "600100", "ST示例", 10.00, 11.00},
		{"创业板20%", "300750", "", 10.00, 12.00},
		{"科创板20%", "688111", "", 9.99, 11.99},
		{"北交所30%", "830799", "", 10.00, 13.00},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			board := foundation.DetectBoard(item.symbol, item.stockName)
			got := LimitPrice(board, foundation.IsST(item.stockName), item.previousClose)
			if got != item.wantLimit {
				t.Fatalf("LimitPrice(%s,%s,%v) = %.2f, want %.2f", item.symbol, item.stockName, item.previousClose, got, item.wantLimit)
			}
		})
	}
}

func TestLimitStateDetectsBroken(t *testing.T) {
	row := foundation.MarketSnapshotRow{Symbol: "300001", Name: "测试", Board: foundation.BoardChiNext, PreviousClose: 10, High: 12, Price: 11.5, Volume: 1}
	if state := limitState(row); state != 2 {
		t.Fatalf("炸板应判为 2，实际 %d", state)
	}
	row.Price = 12
	if state := limitState(row); state != 1 {
		t.Fatalf("涨停应判为 1，实际 %d", state)
	}
}

func TestEvaluateRuleAndLogicRequiresAllFields(t *testing.T) {
	rule := Rule{Type: RuleTypeVolume, Scope: ScopeSymbols, Symbols: []string{"300001"},
		Conditions: []Condition{{Field: "volume_ratio", Op: OpGTE, Value: 2}, {Field: "turnover_rate", Op: OpGTE, Value: 3}},
		Logic:      "and"}
	short := Observation{Price: 10, VolumeRatio: 3}
	if got := EvaluateRule(rule, short); len(got) != 0 {
		t.Fatalf("缺换手字段时 AND 规则不应触发，实际 %v", got)
	}
	full := short
	full.TurnoverRate = 5
	if got := EvaluateRule(rule, full); len(got) != 2 {
		t.Fatalf("AND 全部满足应给 2 条信号，实际 %d", len(got))
	}
	// OR 语义下缺失第二个字段，只要有一个成立即触发。
	rule.Logic = "or"
	if got := EvaluateRule(rule, short); len(got) != 1 {
		t.Fatalf("OR 语义缺失字段不应阻塞另一条成立条件，实际 %d", len(got))
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore("")
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestAllowTriggerStateMachine(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	rule := Rule{ID: "r1", Type: RuleTypePrice, Scope: ScopeSymbols, Symbols: []string{"600001"},
		Conditions: []Condition{{Field: "last", Op: OpGTE, Value: 10}}, CooldownSeconds: 0}
	now := time.Now()
	fired, err := store.allowTrigger(ctx, rule, "600001", rule.Conditions[0], 10.5, now, "2026-10-24")
	if err != nil || !fired {
		t.Fatalf("首次越过阈值应触发: fired=%v err=%v", fired, err)
	}
	// 冷却 30 秒（NormalizeRule 会补）——但这里 rule 原始零值，直接检查再次触发被拦。
	fired, err = store.allowTrigger(ctx, rule, "600001", rule.Conditions[0], 11.0, now.Add(time.Second), "2026-10-24")
	if err != nil || fired {
		t.Fatalf("仍在区间内且未重新武装不应触发: fired=%v err=%v", fired, err)
	}
	fired, _ = store.allowTrigger(ctx, rule, "600001", rule.Conditions[0], 9.9, now.Add(2*time.Second), "2026-10-24")
	fired, err = store.allowTrigger(ctx, rule, "600001", rule.Conditions[0], 10.2, now.Add(3*time.Second), "2026-10-24")
	if err != nil || !fired {
		t.Fatalf("回到区间外再进入应重新触发: fired=%v err=%v", fired, err)
	}
}

func TestLotDerivedRules(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	lot := Lot{ID: "lot_x", Symbol: "600001", Name: "示例", Quantity: 100, CostPrice: 10,
		TargetPct: 20, StopPct: 8, RemindDate: "2026-10-30", LeadDays: 3, Source: SourceManual, Enabled: true}
	if err := store.UpsertLot(ctx, lot); err != nil {
		t.Fatalf("保存批次失败: %v", err)
	}
	priceRule, ok := lotPriceRule(lot)
	if !ok {
		t.Fatal("止盈止损规则应生成")
	}
	if fmt.Sprint(priceRule.Conditions[0].Value) != "12" {
		t.Fatalf("止盈价应为 12，实际 %v", priceRule.Conditions[0].Value)
	}
	dateRule, ok := lotDateRule(lot)
	if !ok {
		t.Fatal("到期规则应生成")
	}
	if dateRule.ID != lotDateRuleID(lot.ID) {
		t.Fatalf("到期规则 ID = %s", dateRule.ID)
	}
	lot.TargetPct = 0
	lot.StopPct = 0
	if _, ok := lotPriceRule(lot); ok {
		t.Fatal("止盈止损清空后应无价格规则")
	}
}
