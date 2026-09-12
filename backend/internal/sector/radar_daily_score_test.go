package sector

import (
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

// 「当日强度」必须由当日驱动：一个今天走平但五日大涨的板块不应压过今天的领涨板块，
// 反之五日口径里应体现多日强势。
func TestIndustryDailyScoreIsDrivenByTodayNotMultiDayRun(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	items := []foundation.MarketIndustryMomentum{
		{
			Code: "BK1", Name: "今日领涨", ChangePercent: 3.2,
			FiveDayChangePercent: 0.4, TwentyDayChange: 0.2,
			RisingCount: 8, FallingCount: 1,
			LeaderName: "龙头A", LeaderChangePercent: 9.9, Score: 70,
		},
		{
			Code: "BK2", Name: "走平但五日大涨", ChangePercent: 0.05,
			FiveDayChangePercent: 12, TwentyDayChange: 15,
			RisingCount: 5, FallingCount: 5,
			LeaderName: "龙头B", LeaderChangePercent: 0.2, Score: 100,
		},
	}

	overviews := buildIndustryRadarOverviews(items, foundation.SourceMeta{Source: "test"}, now, nil, nil)
	byName := map[string]foundation.ThemeOverview{}
	for _, item := range overviews {
		byName[item.Name] = item
	}
	todayLeader, ok := byName["今日领涨"]
	if !ok {
		t.Fatalf("missing 今日领涨: %+v", overviews)
	}
	flat := byName["走平但五日大涨"]

	if todayLeader.IndustryDailyScore <= flat.IndustryDailyScore {
		t.Fatalf("当日强度应由当日主导：今日领涨=%d 走平=%d", todayLeader.IndustryDailyScore, flat.IndustryDailyScore)
	}
	if flat.IndustryFiveDayScore <= todayLeader.IndustryFiveDayScore {
		t.Fatalf("五日强度应由五日主导：走平=%d 今日领涨=%d", flat.IndustryFiveDayScore, todayLeader.IndustryFiveDayScore)
	}
	// 对外的 DailyStrengthScore 由合并阶段按 IndustryDailyScore 派生（见 mergeRadarOverviews），
	// 因此这里只需确认 IndustryDailyScore 已经当日主导。
	if todayLeader.TrendStage == "" || flat.TrendStage == "" {
		t.Fatalf("趋势阶段应已计算: %+v / %+v", todayLeader.TrendStage, flat.TrendStage)
	}
}

func TestRadarDailyProviderScoreIgnoresMultiDayGains(t *testing.T) {
	if got := radarDailyProviderScore(0); got != 50 {
		t.Fatalf("flat day should score 50, got %v", got)
	}
	if got := radarDailyProviderScore(3); got != 74 {
		t.Fatalf("+3%% should score 74, got %v", got)
	}
	if got := radarDailyProviderScore(-8); got != 0 {
		t.Fatalf("large drop should clamp to 0, got %v", got)
	}
	if got := radarDailyProviderScore(20); got != 100 {
		t.Fatalf("large gain should clamp to 100, got %v", got)
	}
}
