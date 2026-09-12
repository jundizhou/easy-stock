package sector

import (
	"context"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

type fakeConceptMomentumSource struct {
	items []foundation.MarketIndustryMomentum
	meta  foundation.SourceMeta
	err   error
}

func (f fakeConceptMomentumSource) ConceptMomentum(context.Context, int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	return append([]foundation.MarketIndustryMomentum(nil), f.items...), f.meta, f.err
}

func TestConceptBoardIndexMatchesAbbreviatedAndExactNames(t *testing.T) {
	index := newBoardIndex([]foundation.MarketIndustryMomentum{
		{Code: "BK1", Name: "船舶制造", ChangePercent: 1.2, RisingCount: 10, FallingCount: 4},
		{Code: "BK2", Name: "通信设备", ChangePercent: 0.8, RisingCount: 20, FallingCount: 30},
		{Code: "BK3", Name: "电力", ChangePercent: -0.3, RisingCount: 5, FallingCount: 9},
		{Code: "BK4", Name: "光通信模块", ChangePercent: 2.4, RisingCount: 12, FallingCount: 3},
	})

	if board, ok := index.find("通信设备"); !ok || board.Code != "BK2" {
		t.Fatalf("精确名应命中: ok=%v board=%+v", ok, board)
	}
	if board, ok := index.find("电力"); !ok || board.Code != "BK3" {
		t.Fatalf("两字名应精确命中: ok=%v board=%+v", ok, board)
	}
	if board, ok := index.find("光通信"); !ok || board.Code != "BK4" {
		t.Fatalf("三字以上应支持包含匹配（截断名/简称）: ok=%v board=%+v", ok, board)
	}
	if _, ok := index.find("电力设备"); ok {
		t.Fatalf("两字板块名不应污染到电力设备")
	}
	if _, ok := index.find("船舶"); ok {
		t.Fatalf("两字名不做模糊匹配，避免误命中")
	}
	if _, ok := index.find("不存在的概念"); ok {
		t.Fatalf("未命中时不应返回板块")
	}
}

func TestIntradayThemeSignalUsesMappedConceptBoards(t *testing.T) {
	index := newBoardIndex([]foundation.MarketIndustryMomentum{
		{Code: "BK1", Name: "通信设备", ChangePercent: 3, RisingCount: 80, FallingCount: 20},
		{Code: "BK2", Name: "通信服务", ChangePercent: 1, RisingCount: 20, FallingCount: 20},
	})
	// 通信 的映射关键词包含 "通信设备"/"通信服务"
	theme := duanxianxia.Theme{Code: "801660", Name: "通信", Rank: 1}

	daily, change, breadth, ok := intradayThemeSignal(theme, index)
	if !ok {
		t.Fatal("已映射题材应能算出当日信号")
	}
	// 家数加权：change=(3*100 + 1*40)/140 ≈ 2.43，breadth=100/140 ≈ 0.714
	if change < 2.4 || change > 2.5 {
		t.Fatalf("加权涨幅应为 ~2.43，实际 %.4f", change)
	}
	if breadth < 0.70 || breadth > 0.73 {
		t.Fatalf("广度应为 ~0.714，实际 %.4f", breadth)
	}
	if daily < 60 || daily > 100 {
		t.Fatalf("当日分数应在合理区间，实际 %d", daily)
	}

	if _, _, _, ok := intradayThemeSignal(duanxianxia.Theme{Code: "nope", Name: "无映射题材"}, index); ok {
		t.Fatal("没有对应板块时不应给出信号")
	}
	if _, _, _, ok := intradayThemeSignal(theme, nil); ok {
		t.Fatal("索引为空时不应给出信号")
	}
}

func TestRadarUsesConceptCalibrationWhileSnapshotIsStale(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	// 快照是上一交易日（09-09），盘中情形
	snapshot := duanxianxia.Snapshot{
		ID: "kpl-2026-09-09", TradeDate: "2026-09-09", FetchedAt: now.Add(-20 * time.Hour),
		Themes: []duanxianxia.Theme{{Code: "801660", Name: "通信", Rank: 6, Strength: 1000}},
	}
	concepts := fakeConceptMomentumSource{items: []foundation.MarketIndustryMomentum{
		{Code: "BK1", Name: "通信设备", ChangePercent: 4, RisingCount: 40, FallingCount: 10},
	}}
	provider := NewRadarProvider(
		fakeRadarSource{snapshot: snapshot},
		fakeRadarFallback{},
		nil,
		RadarProviderConfig{Now: func() time.Time { return now }, ConceptMomentum: concepts, FallbackFillLimit: 6},
	)
	items, meta, err := provider.Overviews(context.Background())
	if err != nil {
		t.Fatalf("Overviews: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("应至少返回一个题材: %+v", items)
	}
	calibrated := items[0]
	if !calibrated.Provisional {
		t.Fatalf("盘中应标记为已按当日数据校准: %+v", calibrated)
	}
	if calibrated.DailyStrengthScore == 0 {
		t.Fatalf("校准后当日强度不应为 0: %+v", calibrated)
	}
	if !strings.Contains(meta.FallbackReason, "东财概念") {
		t.Fatalf("meta 应说明盘中校准: %q", meta.FallbackReason)
	}

	// 快照就是今天（收盘后）：不应再叠加盘中校准
	today := snapshot
	today.TradeDate = "2026-09-10"
	providerToday := NewRadarProvider(
		fakeRadarSource{snapshot: today},
		fakeRadarFallback{},
		nil,
		RadarProviderConfig{Now: func() time.Time { return now }, ConceptMomentum: concepts, FallbackFillLimit: 6},
	)
	itemsToday, _, err := providerToday.Overviews(context.Background())
	if err != nil {
		t.Fatalf("Overviews(today): %v", err)
	}
	if len(itemsToday) == 0 {
		t.Fatal("快照为当天时应返回题材")
	}
	if itemsToday[0].Provisional {
		t.Fatalf("快照为当天时不应标记为盘中校准: %+v", itemsToday[0])
	}
}
