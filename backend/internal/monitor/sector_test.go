package monitor

import (
	"context"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestShiftClock(t *testing.T) {
	cases := []struct {
		in     string
		offset int
		want   string
	}{
		{"10:05", -5, "10:00"},
		{"09:31", -31, "09:00"},
		{"09:31", -60, "08:31"},
		{"13:00", 0, "13:00"},
		{"bad", 5, ""},
	}
	for _, item := range cases {
		if got := shiftClock(item.in, item.offset); got != item.want {
			t.Errorf("shiftClock(%q,%d)=%q want %q", item.in, item.offset, got, item.want)
		}
	}
}

func TestSeriesSpeed(t *testing.T) {
	points := []foundation.SectorPricePoint{
		{Time: "09:30", ChangePercent: 0.2},
		{Time: "09:35", ChangePercent: 0.4},
		{Time: "10:00", ChangePercent: 1.0},
		{Time: "10:05", ChangePercent: 2.0},
	}
	speed, ok := seriesSpeed(points, 5)
	if !ok {
		t.Fatalf("应有速度值")
	}
	if diff := speed - 1.0; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("speed=%.2f want 1.00（10:05 相对 10:00）", speed)
	}
	// 刚开盘只有一个点：没有可比的过去，保持沉默。
	if _, ok := seriesSpeed(points[:1], 5); ok {
		t.Fatalf("单点不应给出速度")
	}
}

func TestSeriesDelta(t *testing.T) {
	points := []foundation.SectorFlowPoint{
		{Time: "09:30", Main: 1e8},
		{Time: "10:00", Main: 2e8},
		{Time: "10:30", Main: 3.5e8},
	}
	delta, ok := seriesDelta(points, 3.5e8, 30)
	if !ok {
		t.Fatalf("应有净额变化")
	}
	if diff := delta - 1.5e8; diff < -1 || diff > 1 {
		t.Fatalf("delta=%.0f want 1.5e8", delta)
	}
	if _, ok := seriesDelta(points, 0, 30); ok {
		t.Fatalf("latest=0 不应给出变化")
	}
}

func TestSectorBreadth(t *testing.T) {
	rows := []foundation.MarketSnapshotRow{
		{Symbol: "600001", Price: 11, PreviousClose: 10, High: 11.05, ChangePercent: 10, Board: foundation.BoardMain},   // 涨停
		{Symbol: "600002", Price: 10.5, PreviousClose: 10, High: 10.6, ChangePercent: 5, Board: foundation.BoardMain},   // 普通上涨
		{Symbol: "600003", Price: 9.8, PreviousClose: 10, High: 10.1, ChangePercent: -2, Board: foundation.BoardMain},   // 下跌
		{Symbol: "600004", Price: 8.8, PreviousClose: 9.78, High: 9.9, ChangePercent: -10, Board: foundation.BoardMain}, // 跌停
		{Symbol: "600005", Price: 0, PreviousClose: 10, High: 10, Board: foundation.BoardMain, Suspended: true},
	}
	snap := map[string]foundation.MarketSnapshotRow{}
	for _, row := range rows {
		snap[row.Symbol] = row
	}
	stat := SectorStats{}
	stat.SectorBreadth(snap, []string{"600001", "600002", "600003", "600004", "600005"})
	if !stat.HaveBreadth {
		t.Fatalf("应有广度")
	}
	if stat.Members != 4 {
		t.Fatalf("members=%d want 4（停牌剔除）", stat.Members)
	}
	if stat.LimitUp != 1 || stat.LimitDown != 1 {
		t.Fatalf("limit up=%d down=%d want 1/1", stat.LimitUp, stat.LimitDown)
	}
	if stat.Breadth < 49.9 || stat.Breadth > 50.1 {
		t.Fatalf("breadth=%.1f want 50.0", stat.Breadth)
	}
	// 领涨股：只记上涨成员，按涨幅降序取前三（这里只有两只上涨）。
	if len(stat.TopMovers) != 2 {
		t.Fatalf("top movers=%d want 2", len(stat.TopMovers))
	}
	if stat.TopMovers[0].Symbol != "600001" || stat.TopMovers[1].Symbol != "600002" {
		t.Fatalf("movers=%+v want 600001,600002", stat.TopMovers)
	}
	// 没有成分：保持沉默而不是 0%。
	empty := SectorStats{}
	empty.SectorBreadth(snap, nil)
	if empty.HaveBreadth {
		t.Fatalf("无成分不应有广度")
	}
	if text := sectorMoversText([]SectorMover{{Symbol: "688256", Name: "寒武纪", ChangePercent: 9.9}, {Symbol: "688981", Name: "中芯国际", ChangePercent: 7.2}}); text != "寒武纪（688256）+9.90%、中芯国际（688981）+7.20%" {
		t.Fatalf("movers text=%q", text)
	}
	if text := sectorMoversText(nil); text != "" {
		t.Fatalf("empty movers text=%q", text)
	}
}

func TestNormalizeSectorRule(t *testing.T) {
	base := Rule{Name: "板块异动", Type: RuleTypeSector, Conditions: []Condition{{Field: "sector_change", Op: OpGTE, Value: 2}}}
	if _, err := NormalizeRule(base); err == nil {
		t.Fatalf("无板块应报错")
	}
	tooMany := base
	for i := 0; i <= sectorMaxPerRule; i++ {
		tooMany.Symbols = append(tooMany.Symbols, "BK1000")
	}
	if _, err := NormalizeRule(tooMany); err == nil {
		t.Fatalf("超过上限应报错")
	}
	bad := base
	bad.Symbols = []string{"600000"}
	if _, err := NormalizeRule(bad); err == nil {
		t.Fatalf("非 BK 代码应报错")
	}
	rule, err := NormalizeRule(func() Rule {
		copyRule := base
		copyRule.Scope = ScopeAll
		copyRule.Symbols = []string{"bk0448"}
		return copyRule
	}())
	if err != nil {
		t.Fatalf("合法规则不应报错：%v", err)
	}
	if rule.Scope != ScopeSymbols || rule.Symbols[0] != "BK0448" {
		t.Fatalf("scope 应强制 symbols、代码应大写：%+v", rule)
	}
}

func TestEvaluateSectorRule(t *testing.T) {
	rule, err := NormalizeRule(Rule{
		Name: "板块加速", Type: RuleTypeSector, Scope: ScopeSymbols, Symbols: []string{"BK0448"},
		Conditions: []Condition{
			{Field: "sector_change", Op: OpGTE, Value: 1},
			{Field: "sector_breadth", Op: OpGTE, Value: 80},
		},
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	obs := Observation{Sector: SectorStats{
		Have: true, ChangePercent: 2.5,
		HaveBreadth: true, Breadth: 85, Members: 20, LimitUp: 3,
	}}
	signals := EvaluateRule(rule, obs)
	if len(signals) != 2 {
		t.Fatalf("AND 语义两条都应成立，got %d", len(signals))
	}
	// 广度缺失：AND 规则保持沉默。
	obs.Sector.HaveBreadth = false
	if got := EvaluateRule(rule, obs); len(got) != 0 {
		t.Fatalf("缺广度不应触发")
	}
}

func TestSectorObservationValues(t *testing.T) {
	obs := Observation{Sector: SectorStats{Have: true, HaveFlow: true, MainNetInflow: 2.5e8}}
	if value, ok := observationValue(obs, "sector_flow"); !ok || value < 2.49 || value > 2.51 {
		t.Fatalf("sector_flow 应换算成亿，got %v ok=%v", value, ok)
	}
	if _, ok := observationValue(obs, "sector_speed"); ok {
		t.Fatalf("分时缺失时 speed 应沉默")
	}
}

// fakeThemeCatalog 提供题材库维度测试数据：TH5「算电协同」两个成员。
type fakeThemeCatalog struct{}

func (fakeThemeCatalog) Themes(context.Context) ([]ThemeSummary, error) {
	return []ThemeSummary{{ID: 5, Name: "算电协同", ChangePercent: 1.2, StockCount: 2}}, nil
}

func (fakeThemeCatalog) ThemeMembers(_ context.Context, id int) ([]string, error) {
	if id == 5 {
		return []string{"600001", "600002"}, nil
	}
	return nil, nil
}

func TestIsThemeCode(t *testing.T) {
	cases := map[string]bool{"TH5": true, "TH123": true, "BK1036": false, "TH": false, "THx5": false, "th5": false, "TH0": false}
	for code, want := range cases {
		if got := isThemeCode(code); got != want {
			t.Fatalf("isThemeCode(%q) = %v, want %v", code, got, want)
		}
	}
	if _, ok := themeID("TH5"); !ok {
		t.Fatalf("TH5 应能解析出题材 id")
	}
}

func TestNormalizeSectorRuleAcceptsTheme(t *testing.T) {
	rule, err := NormalizeRule(Rule{
		Name: "题材监控", Type: RuleTypeSector, Scope: ScopeSymbols, Symbols: []string{"th5"},
		Conditions: []Condition{{Field: "sector_change", Op: OpGTE, Value: 1}},
	})
	if err != nil {
		t.Fatalf("题材代码应合法：%v", err)
	}
	if rule.Symbols[0] != "TH5" {
		t.Fatalf("题材代码应大写归一：%+v", rule.Symbols)
	}
	bad := rule
	bad.Symbols = []string{"THx"}
	if _, err := NormalizeRule(bad); err == nil {
		t.Fatalf("THx 应报错")
	}
}

func TestSectorStatsThemeDimension(t *testing.T) {
	engine := &Engine{
		themeCatalog: fakeThemeCatalog{},
		snapRows: []foundation.MarketSnapshotRow{
			{Symbol: "600001", Board: foundation.BoardMain, Price: 11, PreviousClose: 10, High: 11.05, ChangePercent: 10}, // 涨停
			{Symbol: "600002", Board: foundation.BoardMain, Price: 10.4, PreviousClose: 10, High: 10.5, ChangePercent: 4}, // +4%
		},
		snapFetchedAt: time.Now(),
		now:           time.Now,
	}
	stats := engine.sectorStats(context.Background(), []string{"TH5"}, map[string]foundation.SectorFlowRank{}, time.Now())
	stat, ok := stats["TH5"]
	if !ok || !stat.Have {
		t.Fatalf("TH5 应有观测：%+v", stats)
	}
	if stat.Name != "算电协同" {
		t.Fatalf("名字应来自题材库：%q", stat.Name)
	}
	if !stat.HaveBreadth {
		t.Fatalf("成员×快照应产出广度")
	}
	if stat.Breadth != 100 || stat.LimitUp != 1 || stat.Members != 2 {
		t.Fatalf("广度/涨停/成员数不符：%+v", stat)
	}
	if stat.ChangePercent < 6.9 || stat.ChangePercent > 7.1 {
		t.Fatalf("涨跌幅应为成员均值 7%%（(10+4)/2），实际 %v", stat.ChangePercent)
	}
	// 题材没有 BK 代码：分时与主力净额维度必须沉默。
	if stat.HaveFlow || stat.HaveSpeed || stat.HaveFlowDelta {
		t.Fatalf("题材维度不应有资金流/分时能力：%+v", stat)
	}
}
