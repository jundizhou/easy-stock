package sector

import (
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

// 「当日强度」必须由当日信息主导。开盘啦快照是日频的，盘中拿不到当天数据，
// 所以它的排名只应作背景参考；真正决定当日分数的是成分股实时强弱。
// 这条测试钉住权重意图，防止有人把 rank 权重改回主导地位。
func TestKaipanlaDailyScoreIsDrivenByTodayNotYesterdayRank(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	// 快照是昨天(09-09)的：tradeAge=1，触发盘中校准路径。
	snapshot := duanxianxia.Snapshot{
		ID: "snap", TradeDate: "2026-09-09", FetchedAt: now,
		Themes: []duanxianxia.Theme{
			{Code: "1", Name: "昨日第一名", Rank: 1, Strength: 9000, History: repeatRankPoints(5)},
			{Code: "2", Name: "今日真强", Rank: 30, Strength: 1000, History: repeatRankPoints(1)},
		},
	}
	provider := NewRadarProvider(
		fakeRadarSource{snapshot: snapshot},
		fakeRadarFallback{items: nil},
		nil,
		RadarProviderConfig{Now: func() time.Time { return now }, FallbackFillLimit: 6},
	)
	items, _, err := provider.Overviews(t.Context())
	if err != nil {
		t.Fatalf("Overviews: %v", err)
	}
	byName := map[string]foundation.ThemeOverview{}
	for _, item := range items {
		byName[item.Name] = item
	}

	yesterdayLeader, ok := byName["昨日第一名"]
	if !ok {
		t.Fatalf("缺少「昨日第一名」: %+v", items)
	}
	todayStrong, ok := byName["今日真强"]
	if !ok {
		t.Fatalf("缺少「今日真强」: %+v", items)
	}

	// 两条题材都没有映射到东财板块（无 themeBoards），走纯权重融合：
	// 昨日第一名只靠 rank/strength/persistence 得分，今日真强同样如此。
	// 关键断言：昨日第一名的当日分不得因为"昨天第一"就被推到高位。
	if yesterdayLeader.DailyStrengthScore > 60 {
		t.Fatalf("仅凭昨日排名就让当日强度达到 %d，昨日信息仍然主导", yesterdayLeader.DailyStrengthScore)
	}
	if todayStrong.DailyStrengthScore > yesterdayLeader.DailyStrengthScore {
		t.Fatalf("无当日数据的题材不应超过历史强题材（今日真强 %d > 昨日第一名 %d）",
			todayStrong.DailyStrengthScore, yesterdayLeader.DailyStrengthScore)
	}
}

// 权重常量本身必须满足「当日项占大头」的不变量，避免后续被误调。
func TestKaipanlaDailyWeightsFavorTodayStrength(t *testing.T) {
	todayShare := kaipanlaDailyWeightStrength
	backgroundShare := kaipanlaDailyWeightRank + kaipanlaDailyWeightSourceScore + kaipanlaDailyWeightPersistence
	if todayShare <= backgroundShare {
		t.Fatalf("当日强度权重 %.2f 未超过背景项合计 %.2f", todayShare, backgroundShare)
	}

	// 五日口径相反：延续性信息在这里是有效的，应继续保持多日主导。
	fiveDayBackground := kaipanlaFiveDayWeightRank + kaipanlaFiveDayWeightSourceScore + kaipanlaFiveDayWeightPersistence
	if fiveDayBackground <= kaipanlaFiveDayWeightStrength {
		t.Fatalf("五日口径背景项合计 %.2f 未超过强度项 %.2f，延续性被削弱",
			fiveDayBackground, kaipanlaFiveDayWeightStrength)
	}
}

// 盘中校准权重也应以当日数据为主。
func TestIntradayBlendFavorsTodayReading(t *testing.T) {
	if radarIntradayBlend <= 0.5 {
		t.Fatalf("盘中校准权重 %.2f 未过半，昨日口径仍占主导", radarIntradayBlend)
	}
	if radarIntradayBlend >= 1 {
		t.Fatalf("盘中校准权重 %.2f 达到 1，丢失了衰减快照的兜底作用", radarIntradayBlend)
	}
}

func repeatRankPoints(count int) []duanxianxia.RankPoint {
	points := make([]duanxianxia.RankPoint, 0, count)
	for index := 0; index < count; index++ {
		points = append(points, duanxianxia.RankPoint{
			TradeDate: "2026-09-09", Rank: index + 1, Strength: 9000,
		})
	}
	return points
}
