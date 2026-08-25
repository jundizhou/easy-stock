package sector

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/narrative"
	"easy-stock/backend/internal/providers/duanxianxia"
)

const (
	radarIndustrySource = "market:industry-momentum"
	radarFusionSource   = "theme-radar:fusion"
	radarSinglePenalty  = 3
	radarScoreGap       = 10
)

type radarWindowScores struct {
	daily   int
	fiveDay int
}

func (p *RadarProvider) fusedOverviews(
	ctx context.Context,
	snapshot duanxianxia.Snapshot,
	fetchMeta duanxianxia.FetchMeta,
	snapshotErr error,
	industries []foundation.MarketIndustryMomentum,
	industryMeta foundation.SourceMeta,
	industryErr error,
) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	if snapshotErr == nil && len(snapshot.Themes) == 0 {
		snapshotErr = fmt.Errorf("开盘啦暂无题材快照")
	}
	if industryErr == nil && len(industries) == 0 {
		industryErr = fmt.Errorf("行业趋势强度暂无数据")
	}

	tradeAge := tradingDayAge(snapshot.TradeDate, p.now())
	if snapshotErr == nil && tradeAge > 2 {
		snapshotErr = fmt.Errorf("开盘啦题材快照已超过两个交易日")
	}

	industryItems := []foundation.ThemeOverview{}
	if industryErr == nil {
		industryItems = buildIndustryRadarOverviews(industries, industryMeta, p.now())
	}

	kaipanlaItems := []foundation.ThemeOverview{}
	if snapshotErr == nil {
		themes := append([]duanxianxia.Theme(nil), snapshot.Themes...)
		candidateLimit := max(24, p.fallbackFill)
		if len(themes) > candidateLimit {
			themes = themes[:candidateLimit]
		}
		quoteLookup := p.quoteLookup(ctx, themes)
		strengthScores := p.realtimeStrengthScores(ctx, themes)
		kaipanlaItems = p.buildKaipanlaRadarOverviews(snapshot, themes, quoteLookup, strengthScores, tradeAge)
	}

	items := mergeRadarOverviews(industryItems, kaipanlaItems)
	items = rankAndSelectRadarOverviews(items, p.fallbackFill)
	meta := fusedRadarMeta(p.now(), snapshot, fetchMeta, snapshotErr, industryMeta, industryErr, len(kaipanlaItems) > 0, len(industryItems) > 0)
	if len(items) == 0 {
		return nil, meta, fmt.Errorf("趋势题材雷达不可用：%s", joinRadarErrors(snapshotErr, industryErr))
	}
	return items, meta, nil
}

func buildIndustryRadarOverviews(items []foundation.MarketIndustryMomentum, meta foundation.SourceMeta, now time.Time) []foundation.ThemeOverview {
	if len(items) == 0 {
		return nil
	}
	dailyChange := industryMetricPercentiles(items, func(item foundation.MarketIndustryMomentum) (float64, bool) {
		return item.ChangePercent, industryFieldAvailable(item.Meta, "change_percent")
	})
	fiveDayChange := industryMetricPercentiles(items, func(item foundation.MarketIndustryMomentum) (float64, bool) {
		return item.FiveDayChangePercent, industryFieldAvailable(item.Meta, "five_day_change_percent")
	})
	twentyDayChange := industryMetricPercentiles(items, func(item foundation.MarketIndustryMomentum) (float64, bool) {
		return item.TwentyDayChange, industryFieldAvailable(item.Meta, "twenty_day_change_percent")
	})
	breadth := industryMetricPercentiles(items, func(item foundation.MarketIndustryMomentum) (float64, bool) {
		total := item.RisingCount + item.FallingCount
		return float64(item.RisingCount-item.FallingCount) / float64(max(total, 1)), total > 0
	})
	leader := industryMetricPercentiles(items, func(item foundation.MarketIndustryMomentum) (float64, bool) {
		return item.LeaderChangePercent, strings.TrimSpace(item.LeaderName) != ""
	})

	result := make([]foundation.ThemeOverview, 0, len(items))
	for index, item := range items {
		dailyComposite, dailyOK := weightedRadarScore(
			radarMetric{dailyChange[index], dailyChange[index] >= 0, .45},
			radarMetric{fiveDayChange[index], fiveDayChange[index] >= 0, .25},
			radarMetric{twentyDayChange[index], twentyDayChange[index] >= 0, .15},
			radarMetric{breadth[index], breadth[index] >= 0, .10},
			radarMetric{leader[index], leader[index] >= 0, .05},
		)
		fiveDayComposite, fiveDayOK := weightedRadarScore(
			radarMetric{dailyChange[index], dailyChange[index] >= 0, .15},
			radarMetric{fiveDayChange[index], fiveDayChange[index] >= 0, .45},
			radarMetric{twentyDayChange[index], twentyDayChange[index] >= 0, .25},
			radarMetric{breadth[index], breadth[index] >= 0, .10},
			radarMetric{leader[index], leader[index] >= 0, .05},
		)
		dailyScore := blendProviderAndComposite(item.Score, dailyComposite, dailyOK)
		fiveDayScore := blendProviderAndComposite(item.Score, fiveDayComposite, fiveDayOK)
		tradeDate := firstNonEmptyRadar(item.Meta.TradeDate, meta.TradeDate, shanghaiDate(now))
		matched := item.RisingCount + item.FallingCount
		result = append(result, foundation.ThemeOverview{
			Theme:                radarIndustryThemeID(item.Code, item.Name),
			Name:                 item.Name,
			ChangePercent:        item.ChangePercent,
			MainNetInflow:        item.MainNetInflow,
			RisingNodes:          item.RisingCount,
			FallingNodes:         item.FallingCount,
			MatchedNodes:         matched,
			TotalNodes:           matched,
			TopNode:              item.LeaderName,
			TopNodeChangePercent: item.LeaderChangePercent,
			TrendStage:           radarTrendStage(dailyScore),
			Source:               radarIndustrySource,
			ProviderRank:         index + 1,
			SourceStrength:       item.Score,
			IndustryDailyScore:   dailyScore,
			IndustryFiveDayScore: fiveDayScore,
			TradeDate:            tradeDate,
		})
	}
	return uniqueIndustryOverviews(result)
}

func (p *RadarProvider) buildKaipanlaRadarOverviews(
	snapshot duanxianxia.Snapshot,
	themes []duanxianxia.Theme,
	quotes map[string]foundation.Quote,
	strengths map[string]themeStrengthScore,
	tradeAge int,
) []foundation.ThemeOverview {
	if len(themes) == 0 {
		return nil
	}
	rankPercentiles := themeMetricPercentiles(themes, func(theme duanxianxia.Theme) float64 {
		return -float64(max(theme.Rank, 1))
	})
	strengthPercentiles := themeMetricPercentiles(themes, func(theme duanxianxia.Theme) float64 {
		return theme.Strength
	})
	hasSourceStrength := false
	for _, theme := range themes {
		if theme.Strength != 0 {
			hasSourceStrength = true
			break
		}
	}
	freshness := math.Pow(.9, float64(max(tradeAge, 0)))
	result := make([]foundation.ThemeOverview, 0, len(themes))
	for index, theme := range themes {
		strength, strengthAvailable := strengths[theme.Code]
		rankAbsolute := clampFloat(100-float64(max(theme.Rank-1, 0))*4, 0, 100)
		rankScore := rankAbsolute*.6 + rankPercentiles[index]*.4
		persistence := float64(min(len(theme.History), 5)) * 20
		daily, _ := weightedRadarScore(
			radarMetric{rankScore, true, .45},
			radarMetric{strengthPercentiles[index], hasSourceStrength, .15},
			radarMetric{persistence, len(theme.History) > 0, .15},
			radarMetric{float64(strength.daily), strengthAvailable, .25},
		)
		fiveDay, _ := weightedRadarScore(
			radarMetric{rankScore, true, .35},
			radarMetric{strengthPercentiles[index], hasSourceStrength, .15},
			radarMetric{persistence, len(theme.History) > 0, .20},
			radarMetric{float64(strength.fiveDay), strengthAvailable, .30},
		)
		dailyScore := roundedRadarScore(daily * freshness)
		fiveDayScore := roundedRadarScore(fiveDay * freshness)
		overview := p.themeOverview(snapshot, theme, quotes, tradeAge > 0, strength)
		overview.TrendScore = dailyScore
		overview.KaipanlaDailyScore = dailyScore
		overview.KaipanlaFiveDayScore = fiveDayScore
		result = append(result, overview)
	}
	return result
}

type radarMetric struct {
	value  float64
	valid  bool
	weight float64
}

func weightedRadarScore(metrics ...radarMetric) (float64, bool) {
	total := 0.0
	weights := 0.0
	for _, metric := range metrics {
		if !metric.valid || math.IsNaN(metric.value) || math.IsInf(metric.value, 0) {
			continue
		}
		total += clampFloat(metric.value, 0, 100) * metric.weight
		weights += metric.weight
	}
	if weights == 0 {
		return 0, false
	}
	return total / weights, true
}

func blendProviderAndComposite(provider float64, composite float64, compositeOK bool) int {
	provider = clampFloat(provider, 0, 100)
	if !compositeOK {
		return roundedRadarScore(provider)
	}
	return roundedRadarScore(provider*.6 + composite*.4)
}

func industryMetricPercentiles(
	items []foundation.MarketIndustryMomentum,
	metric func(foundation.MarketIndustryMomentum) (float64, bool),
) []float64 {
	values := make([]float64, len(items))
	valid := make([]bool, len(items))
	for index, item := range items {
		values[index], valid[index] = metric(item)
	}
	return percentileRanks(values, valid)
}

func themeMetricPercentiles(items []duanxianxia.Theme, metric func(duanxianxia.Theme) float64) []float64 {
	values := make([]float64, len(items))
	valid := make([]bool, len(items))
	for index, item := range items {
		values[index] = metric(item)
		valid[index] = true
	}
	return percentileRanks(values, valid)
}

func percentileRanks(values []float64, valid []bool) []float64 {
	result := make([]float64, len(values))
	for index := range result {
		result[index] = -1
	}
	validCount := 0
	for index, value := range values {
		if index < len(valid) && valid[index] && !math.IsNaN(value) && !math.IsInf(value, 0) {
			validCount++
		}
	}
	if validCount == 0 {
		return result
	}
	if validCount == 1 {
		for index := range values {
			if valid[index] {
				result[index] = 50
			}
		}
		return result
	}
	for index, value := range values {
		if !valid[index] {
			continue
		}
		less := 0
		equal := 0
		for otherIndex, other := range values {
			if !valid[otherIndex] {
				continue
			}
			if other < value {
				less++
			} else if other == value {
				equal++
			}
		}
		result[index] = (float64(less) + float64(equal-1)/2) / float64(validCount-1) * 100
	}
	return result
}

func industryFieldAvailable(meta foundation.SourceMeta, field string) bool {
	if len(meta.AvailableFields) == 0 {
		return true
	}
	for _, available := range meta.AvailableFields {
		if available == field {
			return true
		}
	}
	return false
}

func uniqueIndustryOverviews(items []foundation.ThemeOverview) []foundation.ThemeOverview {
	result := make([]foundation.ThemeOverview, 0, len(items))
	byName := map[string]int{}
	for _, item := range items {
		key := normalizeThemeName(item.Name)
		if key == "" {
			continue
		}
		if index, exists := byName[key]; exists {
			if item.IndustryDailyScore+item.IndustryFiveDayScore > result[index].IndustryDailyScore+result[index].IndustryFiveDayScore {
				result[index] = item
			}
			continue
		}
		byName[key] = len(result)
		result = append(result, item)
	}
	return result
}

func mergeRadarOverviews(industryItems []foundation.ThemeOverview, kaipanlaItems []foundation.ThemeOverview) []foundation.ThemeOverview {
	usedIndustry := make([]bool, len(industryItems))
	result := make([]foundation.ThemeOverview, 0, len(industryItems)+len(kaipanlaItems))
	for _, kaipanla := range kaipanlaItems {
		match := bestIndustryMatch(kaipanla, industryItems, usedIndustry)
		if match >= 0 {
			usedIndustry[match] = true
			result = append(result, mergeRadarPair(industryItems[match], kaipanla))
			continue
		}
		kaipanla.DailyStrengthScore = max(0, kaipanla.KaipanlaDailyScore-radarSinglePenalty)
		kaipanla.FiveDayStrengthScore = max(0, kaipanla.KaipanlaFiveDayScore-radarSinglePenalty)
		kaipanla.TrendScore = kaipanla.DailyStrengthScore
		result = append(result, kaipanla)
	}
	for index, industry := range industryItems {
		if usedIndustry[index] {
			continue
		}
		industry.DailyStrengthScore = max(0, industry.IndustryDailyScore-radarSinglePenalty)
		industry.FiveDayStrengthScore = max(0, industry.IndustryFiveDayScore-radarSinglePenalty)
		industry.TrendScore = industry.DailyStrengthScore
		industry.TrendStage = radarTrendStage(industry.TrendScore)
		result = append(result, industry)
	}
	return result
}

func mergeRadarPair(industry foundation.ThemeOverview, kaipanla foundation.ThemeOverview) foundation.ThemeOverview {
	result := kaipanla
	if industryRef, ok := parseRadarIndustryThemeID(industry.Theme); ok {
		result.Theme = radarFusionThemeID(strings.TrimPrefix(kaipanla.Theme, "kpl:"), industryRef)
	}
	result.Source = radarFusionSource
	result.ChangePercent = industry.ChangePercent
	result.MainNetInflow = industry.MainNetInflow
	if industry.MatchedNodes > 0 {
		result.RisingNodes = industry.RisingNodes
		result.FallingNodes = industry.FallingNodes
		result.MatchedNodes = industry.MatchedNodes
		result.TotalNodes = industry.TotalNodes
	}
	if result.TopNode == "" {
		result.TopNode = industry.TopNode
		result.TopNodeChangePercent = industry.TopNodeChangePercent
	}
	result.IndustryDailyScore = industry.IndustryDailyScore
	result.IndustryFiveDayScore = industry.IndustryFiveDayScore
	result.DailyStrengthScore = fusedRadarScore(industry.IndustryDailyScore, kaipanla.KaipanlaDailyScore)
	result.FiveDayStrengthScore = fusedRadarScore(industry.IndustryFiveDayScore, kaipanla.KaipanlaFiveDayScore)
	result.TrendScore = result.DailyStrengthScore
	result.TrendStage = radarTrendStage(result.TrendScore)
	return result
}

func fusedRadarScore(industry int, kaipanla int) int {
	bonus := 0
	if industry >= 60 && kaipanla >= 60 && absRadar(industry-kaipanla) <= 20 {
		bonus = 3
	}
	return min(100, roundedRadarScore(float64(industry+kaipanla)/2)+bonus)
}

func bestIndustryMatch(kaipanla foundation.ThemeOverview, industries []foundation.ThemeOverview, used []bool) int {
	best := -1
	bestScore := 0
	for index, industry := range industries {
		if used[index] {
			continue
		}
		score := radarIndustryMatchScore(kaipanla, industry)
		if score > bestScore || (score == bestScore && score > 0 && industry.IndustryDailyScore > industries[best].IndustryDailyScore) {
			best = index
			bestScore = score
		}
	}
	if bestScore < 70 {
		return -1
	}
	return best
}

func radarIndustryMatchScore(kaipanla foundation.ThemeOverview, industry foundation.ThemeOverview) int {
	kaipanlaName := normalizeRadarMatchName(kaipanla.Name)
	industryName := normalizeRadarMatchName(industry.Name)
	if kaipanlaName == "" || industryName == "" {
		return 0
	}
	if kaipanlaName == industryName {
		return 100
	}
	if normalizeRadarMatchName(narrative.Canonical(kaipanla.Name)) == normalizeRadarMatchName(narrative.Canonical(industry.Name)) {
		return 96
	}
	mapping, exists := lookupRadarThemeMapping(strings.TrimPrefix(kaipanla.Theme, "kpl:"), kaipanla.Name)
	if !exists {
		return 0
	}
	if normalizeRadarMatchName(mapping.EastMoneyName) == industryName {
		return 98
	}
	if mapping.StaticThemeID != "" {
		if theme, ok := FindTheme(mapping.StaticThemeID); ok && mappedRadarNameMatch(theme.Name, industry.Name) {
			return 94
		}
	}
	for _, term := range mapping.EastMoneyTerms {
		if mappedRadarNameMatch(term, industry.Name) {
			return 90
		}
	}
	if mappedRadarNameMatch(mapping.EastMoneyName, industry.Name) {
		return 88
	}
	return 0
}

func mappedRadarNameMatch(mapped string, industry string) bool {
	a := normalizeRadarMatchName(mapped)
	b := normalizeRadarMatchName(industry)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	shorter, longer := a, b
	if len([]rune(shorter)) > len([]rune(longer)) {
		shorter, longer = longer, shorter
	}
	return len([]rune(shorter)) >= 2 && strings.Contains(longer, shorter)
}

func normalizeRadarMatchName(name string) string {
	value := normalizeThemeName(name)
	for _, suffix := range []string{"行业", "ⅰ", "ⅱ", "Ⅰ", "Ⅱ"} {
		value = strings.TrimSuffix(value, suffix)
	}
	return value
}

func rankAndSelectRadarOverviews(items []foundation.ThemeOverview, limit int) []foundation.ThemeOverview {
	if len(items) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 16
	}
	dailyOrder := interleavedRadarOrder(items, func(item foundation.ThemeOverview) int { return item.DailyStrengthScore })
	fiveDayOrder := interleavedRadarOrder(items, func(item foundation.ThemeOverview) int { return item.FiveDayStrengthScore })
	for rank, index := range dailyOrder {
		items[index].DailyRank = rank + 1
		items[index].SourceRank = rank + 1
	}
	for rank, index := range fiveDayOrder {
		items[index].FiveDayRank = rank + 1
	}
	selected := make(map[int]struct{}, min(len(items), limit*2))
	for _, order := range [][]int{dailyOrder, fiveDayOrder} {
		for rank, index := range order {
			if rank >= limit {
				break
			}
			selected[index] = struct{}{}
		}
	}
	result := make([]foundation.ThemeOverview, 0, len(selected))
	for _, index := range dailyOrder {
		if _, exists := selected[index]; exists {
			result = append(result, items[index])
		}
	}
	return result
}

func interleavedRadarOrder(items []foundation.ThemeOverview, score func(foundation.ThemeOverview) int) []int {
	dual := []int{}
	industry := []int{}
	kaipanla := []int{}
	for index, item := range items {
		switch item.Source {
		case radarFusionSource:
			dual = append(dual, index)
		case radarIndustrySource:
			industry = append(industry, index)
		default:
			kaipanla = append(kaipanla, index)
		}
	}
	sortRadarIndexes(dual, items, score)
	sortRadarIndexes(industry, items, score)
	sortRadarIndexes(kaipanla, items, score)
	exclusive := balancedExclusiveOrder(industry, kaipanla, items, score)

	result := make([]int, 0, len(items))
	dualIndex := 0
	exclusiveIndex := 0
	for dualIndex < len(dual) || exclusiveIndex < len(exclusive) {
		if dualIndex >= len(dual) {
			result = append(result, exclusive[exclusiveIndex:]...)
			break
		}
		if exclusiveIndex >= len(exclusive) {
			result = append(result, dual[dualIndex:]...)
			break
		}
		if score(items[dual[dualIndex]]) >= score(items[exclusive[exclusiveIndex]]) {
			result = append(result, dual[dualIndex])
			dualIndex++
		} else {
			result = append(result, exclusive[exclusiveIndex])
			exclusiveIndex++
		}
	}
	return result
}

func balancedExclusiveOrder(industry []int, kaipanla []int, items []foundation.ThemeOverview, score func(foundation.ThemeOverview) int) []int {
	result := make([]int, 0, len(industry)+len(kaipanla))
	i, k := 0, 0
	industryCount, kaipanlaCount := 0, 0
	lastSource := ""
	streak := 0
	for i < len(industry) || k < len(kaipanla) {
		chooseIndustry := false
		switch {
		case i >= len(industry):
			chooseIndustry = false
		case k >= len(kaipanla):
			chooseIndustry = true
		case lastSource == radarIndustrySource && streak >= 2:
			chooseIndustry = false
		case lastSource == duanxianxia.SourceID && streak >= 2:
			chooseIndustry = true
		case industryCount > kaipanlaCount:
			chooseIndustry = score(items[industry[i]]) > score(items[kaipanla[k]])+radarScoreGap
		case kaipanlaCount > industryCount:
			chooseIndustry = !(score(items[kaipanla[k]]) > score(items[industry[i]])+radarScoreGap)
		default:
			chooseIndustry = score(items[industry[i]]) >= score(items[kaipanla[k]])
		}

		source := duanxianxia.SourceID
		if chooseIndustry {
			result = append(result, industry[i])
			i++
			industryCount++
			source = radarIndustrySource
		} else {
			result = append(result, kaipanla[k])
			k++
			kaipanlaCount++
		}
		if source == lastSource {
			streak++
		} else {
			lastSource = source
			streak = 1
		}
	}
	return result
}

func sortRadarIndexes(indexes []int, items []foundation.ThemeOverview, score func(foundation.ThemeOverview) int) {
	sort.SliceStable(indexes, func(i, j int) bool {
		left := items[indexes[i]]
		right := items[indexes[j]]
		if score(left) != score(right) {
			return score(left) > score(right)
		}
		if left.ProviderRank != right.ProviderRank {
			return left.ProviderRank < right.ProviderRank
		}
		return left.Name < right.Name
	})
}

func fusedRadarMeta(
	now time.Time,
	snapshot duanxianxia.Snapshot,
	fetchMeta duanxianxia.FetchMeta,
	snapshotErr error,
	industryMeta foundation.SourceMeta,
	industryErr error,
	hasKaipanla bool,
	hasIndustry bool,
) foundation.SourceMeta {
	source := radarFusionSource
	if !hasIndustry {
		source = duanxianxia.SourceID
	} else if !hasKaipanla {
		source = radarIndustrySource
	}
	fetchedAt := snapshot.FetchedAt
	if industryMeta.FetchedAt.After(fetchedAt) {
		fetchedAt = industryMeta.FetchedAt
	}
	tradeDate := firstNonEmptyRadar(industryMeta.TradeDate, snapshot.TradeDate, shanghaiDate(now))
	carryForward := hasKaipanla && snapshot.TradeDate != shanghaiDate(now)
	reasons := []string{}
	if snapshotErr != nil {
		reasons = append(reasons, snapshotErr.Error())
	}
	if industryErr != nil {
		reasons = append(reasons, industryErr.Error())
	}
	if strings.TrimSpace(fetchMeta.RefreshError) != "" {
		reasons = append(reasons, fetchMeta.RefreshError)
	}
	if carryForward {
		reasons = append(reasons, "开盘啦尚未更新，已按交易日衰减")
	}
	return foundation.SourceMeta{
		Source:         source,
		SourceURL:      duanxianxia.DefaultBaseURL + "/web/platerotat",
		FetchedAt:      fetchedAt,
		Stale:          len(reasons) > 0,
		TradeDate:      tradeDate,
		SnapshotID:     snapshot.ID,
		NextRefreshAt:  timePointer(fetchMeta.NextAllowedAt),
		FallbackReason: strings.Join(uniqueRadarStrings(reasons), "；"),
		CarryForward:   carryForward,
	}
}

func tradingDayAge(tradeDate string, now time.Time) int {
	location := now.Location()
	date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(tradeDate), location)
	if err != nil {
		return 0
	}
	today, err := time.ParseInLocation("2006-01-02", shanghaiDate(now), location)
	if err != nil || !date.Before(today) {
		return 0
	}
	age := 0
	for current := date.AddDate(0, 0, 1); !current.After(today); current = current.AddDate(0, 0, 1) {
		if current.Weekday() != time.Saturday && current.Weekday() != time.Sunday {
			age++
		}
	}
	return age
}

func radarTrendStage(score int) string {
	switch {
	case score >= 75:
		return "主升"
	case score >= 60:
		return "扩散"
	case score >= 45:
		return "分歧"
	default:
		return "退潮"
	}
}

func roundedRadarScore(value float64) int {
	return int(math.Round(clampFloat(value, 0, 100)))
}

func absRadar(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func firstNonEmptyRadar(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func joinRadarErrors(errors ...error) string {
	values := []string{}
	for _, err := range errors {
		if err != nil {
			values = append(values, err.Error())
		}
	}
	if len(values) == 0 {
		return "暂无可用数据"
	}
	return strings.Join(values, "；")
}

func uniqueRadarStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
