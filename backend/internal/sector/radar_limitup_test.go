package sector

import (
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func radarTestDate(value string) time.Time {
	parsed, err := time.Parse(radarDateLayout, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestAggregateIndustryLimitUpsCountsStreakAndPreviousDay(t *testing.T) {
	events := []foundation.LimitUpEvent{
		{Symbol: "000001.SZ", Name: "农业一", Date: radarTestDate("2026-09-08"), Streak: 1, Industry: "农业"},
		{Symbol: "000002.SZ", Name: "农业二", Date: radarTestDate("2026-09-09"), Streak: 1, Industry: "农业"},
		{Symbol: "000003.SZ", Name: "农业三", Date: radarTestDate("2026-09-09"), Streak: 2, Industry: "农业"},
		{Symbol: "000010.SZ", Name: "电力一", Date: radarTestDate("2026-09-09"), Streak: 1, Industry: "电力"},
		{Symbol: "000004.SZ", Name: "农业四", Date: radarTestDate("2026-09-10"), Streak: 1, Industry: "农业"},
		{Symbol: "000005.SZ", Name: "农业五", Date: radarTestDate("2026-09-10"), Streak: 2, Industry: "农业"},
		{Symbol: "000006.SZ", Name: "农业六", Date: radarTestDate("2026-09-10"), Streak: 3, Industry: "农业Ⅱ"},
		{Symbol: "000020.SZ", Name: "元件一", Date: radarTestDate("2026-09-10"), Streak: 1, Industry: "元件"},
	}

	stats := aggregateIndustryLimitUps(events)
	if len(stats) != 2 {
		t.Fatalf("industries=%d want 2 (电力 has no limit-up on the latest session)", len(stats))
	}

	agri, ok := stats["农业"]
	if !ok {
		t.Fatalf("missing 农业 stats: %+v", stats)
	}
	if agri.TradeDate != "2026-09-10" || agri.LimitUpCount != 3 || agri.MaxStreak != 3 || agri.PreviousCount != 2 || agri.ActiveDays != 3 {
		t.Fatalf("unexpected 农业 stats: %+v", agri)
	}

	element, ok := stats["元件"]
	if !ok {
		t.Fatalf("missing 元件 stats: %+v", stats)
	}
	if element.LimitUpCount != 1 || element.MaxStreak != 1 || element.PreviousCount != 0 || element.ActiveDays != 1 {
		t.Fatalf("unexpected 元件 stats: %+v", element)
	}
}

func TestLookupIndustryLimitUpHandlesAbbreviatedBoardNames(t *testing.T) {
	stats := map[string]industryLimitUpStats{
		"航海装备":  {LimitUpCount: 1, MaxStreak: 1},
		"农产品加":  {LimitUpCount: 4, MaxStreak: 2},
		"电力":    {LimitUpCount: 5, MaxStreak: 2},
		"一般零售":  {LimitUpCount: 3, MaxStreak: 5},
	}

	if got, ok := lookupIndustryLimitUp(stats, "航海装备Ⅱ"); !ok || got.LimitUpCount != 1 {
		t.Fatalf("后缀 Ⅱ 应能匹配航海装备: ok=%v got=%+v", ok, got)
	}
	if got, ok := lookupIndustryLimitUp(stats, "农产品加工"); !ok || got.LimitUpCount != 4 {
		t.Fatalf("涨停池截断名应能前缀匹配: ok=%v got=%+v", ok, got)
	}
	if got, ok := lookupIndustryLimitUp(stats, "一般零售"); !ok || got.MaxStreak != 5 {
		t.Fatalf("精确名应能匹配: ok=%v got=%+v", ok, got)
	}
	if _, ok := lookupIndustryLimitUp(stats, "电力设备"); ok {
		t.Fatalf("两字板块名不应前缀污染到电力设备")
	}
	if _, ok := lookupIndustryLimitUp(stats, "半导体"); ok {
		t.Fatalf("无对应板块时不应返回统计")
	}
}

func TestEnrichIndustryLimitUpFillsOverviewAndKeepsEmptyRowsClean(t *testing.T) {
	stats := map[string]industryLimitUpStats{
		"航海装备": {TradeDate: "2026-09-10", LimitUpCount: 2, MaxStreak: 3, PreviousCount: 1, ActiveDays: 2},
	}
	enriched := enrichIndustryLimitUp(
		foundation.ThemeOverview{Name: "航海装备Ⅱ"},
		foundation.MarketIndustryMomentum{Code: "pt01801711", Name: "航海装备Ⅱ"},
		stats,
	)
	if enriched.LimitUpCount != 2 || enriched.BoardCount != 3 || enriched.MaxStreak != 3 || enriched.PreviousCount != 1 || enriched.ActiveDays != 2 {
		t.Fatalf("unexpected enriched overview: %+v", enriched)
	}

	untouched := enrichIndustryLimitUp(
		foundation.ThemeOverview{Name: "半导体"},
		foundation.MarketIndustryMomentum{Code: "pt01801081", Name: "半导体"},
		stats,
	)
	if untouched.LimitUpCount != 0 || untouched.MaxStreak != 0 || untouched.PreviousCount != 0 || untouched.ActiveDays != 0 {
		t.Fatalf("industry without pool data must stay empty: %+v", untouched)
	}
}
