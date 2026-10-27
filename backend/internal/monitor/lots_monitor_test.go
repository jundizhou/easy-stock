package monitor

import (
	"context"
	"testing"
)

// fakePositionPortfolio 给同步测试提供一笔 600000 的持仓。
type fakePositionPortfolio struct{}

func (fakePositionPortfolio) Holdings(ctx context.Context) ([]Holding, error) {
	return []Holding{{
		Symbol: "600000", Name: "浦发银行", Quantity: 100, AverageCost: 10,
		StopLossPrice: 9.2, TakeProfitPrice: 12, AccountID: "paper-main",
	}}, nil
}

func (fakePositionPortfolio) WatchSymbols(ctx context.Context) ([]string, error) {
	return nil, nil
}

// 批次托管规则删除 = 该批次退出监控：规则清掉、批次保留但标记不再托管。
func TestRemoveLotRuleStopsMonitoring(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	engine := NewEngine(EngineConfig{Store: store})
	lot := Lot{
		ID: positionLotID("paper-main", "600000"), Symbol: "600000", Name: "浦发银行",
		Quantity: 100, CostPrice: 10, TargetPct: 20, StopPct: 8,
		Source: SourcePosition, PositionAccount: "paper-main", Enabled: true,
	}
	if err := store.UpsertLot(ctx, lot); err != nil {
		t.Fatalf("保存批次失败: %v", err)
	}
	if err := engine.ensureLotRules(ctx, lot); err != nil {
		t.Fatalf("派生规则失败: %v", err)
	}
	rules, _ := store.ListRules(ctx, "", false)
	if len(rules) != 1 {
		t.Fatalf("应只有一条止盈止损规则，实际 %d", len(rules))
	}

	if err := engine.RemoveRule(ctx, lotPriceRuleID(lot.ID)); err != nil {
		t.Fatalf("删除批次托管规则应成功，实际 %v", err)
	}
	if rules, _ := store.ListRules(ctx, "", false); len(rules) != 0 {
		t.Fatalf("删除后不应留下规则，实际 %d 条", len(rules))
	}
	updated, ok, err := store.GetLot(ctx, lot.ID)
	if err != nil || !ok {
		t.Fatalf("批次应保留：ok=%v err=%v", ok, err)
	}
	if !updated.Detached || updated.Enabled {
		t.Fatalf("批次应标记为退出监控：detached=%v enabled=%v", updated.Detached, updated.Enabled)
	}
	// 再跑一次派生逻辑也不能把规则生回来。
	if err := engine.ensureLotRules(ctx, updated); err != nil {
		t.Fatalf("重新派生应静默跳过: %v", err)
	}
	if rules, _ := store.ListRules(ctx, "", false); len(rules) != 0 {
		t.Fatalf("退出监控后不应重新生成规则，实际 %d 条", len(rules))
	}
}

// 删除规则后再次同步持仓：只刷新数量成本，不把托管状态和规则生回来。
func TestSyncPositionsKeepsStoppedLotDetached(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	engine := NewEngine(EngineConfig{Store: store, Portfolio: fakePositionPortfolio{}})
	if _, err := engine.SyncPositions(ctx); err != nil {
		t.Fatalf("同步持仓失败: %v", err)
	}
	id := positionLotID("paper-main", "600000")
	if rules, _ := store.ListRules(ctx, "", false); len(rules) != 1 {
		t.Fatalf("同步后应派生一条规则，实际 %d", len(rules))
	}
	if err := engine.RemoveRule(ctx, lotPriceRuleID(id)); err != nil {
		t.Fatalf("删除规则失败: %v", err)
	}
	if _, err := engine.SyncPositions(ctx); err != nil {
		t.Fatalf("二次同步失败: %v", err)
	}
	lot, ok, err := store.GetLot(ctx, id)
	if err != nil || !ok {
		t.Fatalf("批次应存在：ok=%v err=%v", ok, err)
	}
	if !lot.Detached || lot.Enabled {
		t.Fatalf("同步不应恢复托管：detached=%v enabled=%v", lot.Detached, lot.Enabled)
	}
	if lot.Quantity != 100 || lot.CostPrice != 10 {
		t.Fatalf("同步应继续刷新持仓事实：数量 %d 成本 %v", lot.Quantity, lot.CostPrice)
	}
	if rules, _ := store.ListRules(ctx, "", false); len(rules) != 0 {
		t.Fatalf("同步后不应复活规则，实际 %d 条", len(rules))
	}
}

// 批次托管规则的启停走批次 ID（规则 ID 形如 lot:<批次ID>:price，传错会报批次不存在）。
func TestToggleLotRuleUpdatesLot(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	engine := NewEngine(EngineConfig{Store: store})
	lot := Lot{
		ID: positionLotID("paper-main", "600000"), Symbol: "600000", Name: "浦发银行",
		Quantity: 100, CostPrice: 10, StopPct: 8,
		Source: SourcePosition, PositionAccount: "paper-main", Enabled: true,
	}
	if err := store.UpsertLot(ctx, lot); err != nil {
		t.Fatalf("保存批次失败: %v", err)
	}
	if err := engine.ensureLotRules(ctx, lot); err != nil {
		t.Fatalf("派生规则失败: %v", err)
	}
	if err := engine.ToggleRule(ctx, lotPriceRuleID(lot.ID), false); err != nil {
		t.Fatalf("停用批次托管规则应成功，实际 %v", err)
	}
	updated, _, _ := store.GetLot(ctx, lot.ID)
	if updated.Enabled {
		t.Fatal("停用后批次应同步为 disabled")
	}
	if rules, _ := store.ListRules(ctx, "", false); len(rules) != 1 || rules[0].Enabled {
		t.Fatalf("规则应停用：%+v", rules)
	}
}
