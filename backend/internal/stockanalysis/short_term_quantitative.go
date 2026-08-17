package stockanalysis

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"easy-stock/backend/internal/foundation"
)

func buildShortTermQuantitativePlan(input Input, short ShortTermAnalysis, theme ThemeAnalysis, market *MarketContext) ShortTermQuantitativePlan {
	lines := normalizeKLines(input.KLines)
	benchmarkLines := normalizeKLines(input.BenchmarkKLines)
	missing := make([]string, 0, 3)

	referenceClose := 0.0
	baselineDate := ""
	if len(lines) > 0 {
		latest := lines[len(lines)-1]
		referenceClose = latest.Close
		baselineDate = latest.Time.Format("2006-01-02")
	}
	if referenceClose <= 0 {
		referenceClose = input.Quote.Price
	}

	limitPercent := limitUpThreshold(input.Symbol)
	boardScale := limitPercent / 10
	auctionChangeMin := -1 * boardScale
	switch {
	case short.LatestStreak >= 3:
		auctionChangeMin = 0
	case short.LatestStreak == 2:
		auctionChangeMin = -.5 * boardScale
	}
	if short.State == "退潮" {
		auctionChangeMin = math.Max(auctionChangeMin, .5*boardScale)
	}
	if market != nil && (market.Phase == "退潮" || market.Phase == "冰点") {
		auctionChangeMin = math.Max(auctionChangeMin, .5*boardScale)
	}
	auctionChangeMax := clamp(limitPercent*.55, 5, 15)
	if short.State == "加速" {
		auctionChangeMax = clamp(limitPercent*.45, 4.5, 12)
	}

	averageAmount := short.AverageAmount20
	if averageAmount <= 0 && len(lines) > 0 {
		averageAmount = averageKLineAmount(lines, 20)
	}
	auctionAmountRate := .025
	if short.LatestTurnover >= 15 {
		auctionAmountRate = .035
	}
	openingAmountRate := .08
	if averageAmount <= 0 {
		missing = append(missing, "缺少20日成交额，竞价与9:35成交额阈值不可用")
	}

	benchmarkReference := 0.0
	if len(benchmarkLines) > 0 {
		benchmarkReference = benchmarkLines[len(benchmarkLines)-1].Close
	}
	benchmarkVolatility := averageAbsoluteReturn(benchmarkLines, 20)
	if benchmarkVolatility <= 0 {
		benchmarkVolatility = 1
		missing = append(missing, "基准指数波动率不足，指数阈值使用1%默认日波动")
	}
	benchmarkAuctionMin := -clamp(benchmarkVolatility*.55, .3, 1.2)
	benchmarkOpeningMin := -clamp(benchmarkVolatility*.9, .6, 2)
	relativeIndexMin := .5
	if market != nil && (market.Phase == "退潮" || market.Phase == "冰点") {
		benchmarkAuctionMin = math.Max(benchmarkAuctionMin, -.2)
		benchmarkOpeningMin = math.Max(benchmarkOpeningMin, -.5)
		relativeIndexMin = 1
	}

	overview, overviewOK := shortTermThemeOverview(input.Themes, theme.Primary)
	peers := shortTermPeerReferences(input, theme.Primary, overview, overviewOK, 4)
	minimumPositivePeers := min(len(peers), max(1, int(math.Ceil(float64(len(peers))*.6))))
	maximumWeakPeers := 0
	if len(peers) >= 3 {
		maximumWeakPeers = 1
	}
	if len(peers) == 0 {
		missing = append(missing, "未匹配到同题材核心个股，需先补充题材龙头列表")
	}

	themeName := firstNonEmpty(strings.TrimSpace(theme.Primary), "题材待确认")
	if overviewOK {
		themeName = firstNonEmpty(strings.TrimSpace(overview.Name), strings.TrimSpace(overview.Theme), themeName)
	}
	themeThreshold := ShortTermThemeThresholds{
		Name:                 themeName,
		MinimumPositivePeers: minimumPositivePeers,
		MaximumWeakPeers:     maximumWeakPeers,
		PositiveThreshold:    0,
		WeakThreshold:        -3,
	}
	if overviewOK {
		themeThreshold.LimitUpCount = overview.LimitUpCount
		themeThreshold.BoardCount = overview.BoardCount
		themeThreshold.MaxStreak = overview.MaxStreak
		themeThreshold.ActiveDays = overview.ActiveDays
		themeThreshold.Source = overview.Source
		if overview.TradeDate != "" {
			baselineDate = overview.TradeDate
		}
	} else {
		fillThemeThresholdFromEvents(&themeThreshold, input.LimitUps, themeName)
		if themeThreshold.LimitUpCount == 0 {
			missing = append(missing, "缺少题材涨停与连板基线")
		}
	}

	stockThreshold := ShortTermStockThresholds{
		ReferenceClose:     round2(referenceClose),
		LimitUpPercent:     round2(limitPercent),
		AuctionChangeMin:   round2(auctionChangeMin),
		AuctionChangeMax:   round2(auctionChangeMax),
		AuctionAmountMin:   round2(averageAmount * auctionAmountRate),
		AuctionAmountMax:   round2(averageAmount * .15),
		OpeningDrawdownMax: round2(clamp(limitPercent*.2, 2, 5)),
		OpeningAmountMin:   round2(averageAmount * openingAmountRate),
		RelativeIndexMin:   round2(relativeIndexMin),
	}
	if referenceClose > 0 {
		stockThreshold.AuctionPriceMin = round2(referenceClose * (1 + auctionChangeMin/100))
		stockThreshold.AuctionPriceMax = round2(referenceClose * (1 + auctionChangeMax/100))
	}

	return ShortTermQuantitativePlan{
		BaselineDate: baselineDate,
		Stock:        stockThreshold,
		Benchmark: ShortTermBenchmarkThresholds{
			Symbol:           input.BenchmarkSymbol,
			Name:             firstNonEmpty(input.BenchmarkName, input.BenchmarkSymbol, "基准指数"),
			ReferenceClose:   round2(benchmarkReference),
			AuctionChangeMin: round2(benchmarkAuctionMin),
			OpeningChangeMin: round2(benchmarkOpeningMin),
			FailureChange:    round2(benchmarkOpeningMin - .5),
		},
		Theme:   themeThreshold,
		Peers:   peers,
		Missing: uniqueStrings(missing, 3),
	}
}

func averageAbsoluteReturn(lines []foundation.KLine, window int) float64 {
	lines = normalizeKLines(lines)
	if len(lines) < 2 {
		return 0
	}
	start := max(1, len(lines)-window)
	total := 0.0
	count := 0
	for index := start; index < len(lines); index++ {
		previous := lines[index-1].Close
		if previous <= 0 {
			continue
		}
		total += math.Abs(percentChange(previous, lines[index].Close))
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func shortTermThemeOverview(items []foundation.ThemeOverview, theme string) (foundation.ThemeOverview, bool) {
	best := foundation.ThemeOverview{}
	found := false
	for _, item := range items {
		if !themeLabelsMatch(theme, firstNonEmpty(item.Name, item.Theme)) {
			continue
		}
		if !found || item.TrendScore > best.TrendScore || (item.TrendScore == best.TrendScore && item.SourceRank < best.SourceRank) {
			best = item
			found = true
		}
	}
	return best, found
}

func shortTermPeerReferences(input Input, theme string, overview foundation.ThemeOverview, overviewOK bool, limit int) []ShortTermPeerReference {
	if limit <= 0 {
		return nil
	}
	catalogBySymbol := make(map[string]foundation.StockCatalogEntry, len(input.Catalog))
	catalogByName := make(map[string]foundation.StockCatalogEntry, len(input.Catalog))
	for _, item := range input.Catalog {
		catalogBySymbol[item.Symbol] = item
		catalogByName[strings.TrimSpace(item.Name)] = item
	}

	baseDate := ""
	if overviewOK {
		baseDate = overview.TradeDate
	}
	if baseDate == "" {
		baseDate = latestTargetLimitDate(input.Symbol, input.LimitUps)
	}
	if baseDate == "" {
		baseDate = latestLimitDate(input.LimitUps)
	}
	events := make([]foundation.LimitUpEvent, 0, 8)
	for _, event := range input.LimitUps {
		if event.Symbol == input.Symbol || event.Date.Format("2006-01-02") != baseDate || !limitEventMatchesTheme(event, theme) {
			continue
		}
		events = append(events, event)
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Streak != events[j].Streak {
			return events[i].Streak > events[j].Streak
		}
		if themeRank(events[i].ThemeRank) != themeRank(events[j].ThemeRank) {
			return themeRank(events[i].ThemeRank) < themeRank(events[j].ThemeRank)
		}
		if events[i].Amount != events[j].Amount {
			return events[i].Amount > events[j].Amount
		}
		return events[i].Name < events[j].Name
	})

	result := make([]ShortTermPeerReference, 0, limit)
	seen := map[string]bool{input.Symbol: true}
	appendPeer := func(peer ShortTermPeerReference) {
		key := peer.Symbol
		if key == "" {
			key = peer.Name
		}
		if key == "" || seen[key] || len(result) >= limit {
			return
		}
		seen[key] = true
		result = append(result, peer)
	}
	for _, event := range events {
		peer := ShortTermPeerReference{Symbol: event.Symbol, Name: event.Name, Role: event.ThemeLeaderRole, Streak: event.Streak}
		if peer.Role == "" {
			if event.Streak >= 2 {
				peer.Role = fmt.Sprintf("%d板", event.Streak)
			} else {
				peer.Role = "首板"
			}
		}
		if item, ok := catalogBySymbol[event.Symbol]; ok {
			peer.ChangePercent = round2(item.ChangePercent)
			peer.HasQuote = item.Price > 0
		}
		appendPeer(peer)
	}

	if overviewOK {
		for index, label := range overview.Leaders {
			name := strings.Fields(strings.TrimSpace(label))
			if len(name) == 0 || name[0] == input.Quote.Name {
				continue
			}
			item, ok := catalogByName[name[0]]
			peer := ShortTermPeerReference{Name: name[0], Role: fmt.Sprintf("题材核心%d", index+1), Streak: streakFromLeaderLabel(label)}
			if ok {
				peer.Symbol = item.Symbol
				peer.ChangePercent = round2(item.ChangePercent)
				peer.HasQuote = item.Price > 0
				if peer.Streak == 0 {
					peer.Streak = item.LimitUpStreak
				}
			}
			appendPeer(peer)
		}
	}

	if len(result) < limit {
		members := make([]foundation.StockCatalogEntry, 0, 16)
		for _, item := range input.Catalog {
			if item.Symbol == input.Symbol || !catalogEntryMatchesTheme(item, theme) {
				continue
			}
			members = append(members, item)
		}
		sort.SliceStable(members, func(i, j int) bool {
			if members[i].LimitUpStreak != members[j].LimitUpStreak {
				return members[i].LimitUpStreak > members[j].LimitUpStreak
			}
			if members[i].RankScore != members[j].RankScore {
				return members[i].RankScore > members[j].RankScore
			}
			if members[i].ChangePercent != members[j].ChangePercent {
				return members[i].ChangePercent > members[j].ChangePercent
			}
			return members[i].Amount > members[j].Amount
		})
		for _, item := range members {
			role := firstNonEmpty(item.RankRole, "题材强势股")
			appendPeer(ShortTermPeerReference{Symbol: item.Symbol, Name: item.Name, Role: role, Streak: item.LimitUpStreak, ChangePercent: round2(item.ChangePercent), HasQuote: item.Price > 0})
		}
	}
	return result
}

func latestTargetLimitDate(symbol string, events []foundation.LimitUpEvent) string {
	latest := ""
	for _, event := range events {
		if event.Symbol == symbol && event.Date.Format("2006-01-02") > latest {
			latest = event.Date.Format("2006-01-02")
		}
	}
	return latest
}

func latestLimitDate(events []foundation.LimitUpEvent) string {
	latest := ""
	for _, event := range events {
		if date := event.Date.Format("2006-01-02"); date > latest {
			latest = date
		}
	}
	return latest
}

func limitEventMatchesTheme(event foundation.LimitUpEvent, theme string) bool {
	labels := append([]string{event.PrimaryTheme, event.Industry}, event.Concepts...)
	for _, label := range labels {
		if themeLabelsMatch(theme, label) {
			return true
		}
	}
	return false
}

func catalogEntryMatchesTheme(entry foundation.StockCatalogEntry, theme string) bool {
	labels := append([]string{entry.Industry}, entry.Concepts...)
	for _, label := range labels {
		if themeLabelsMatch(theme, label) {
			return true
		}
	}
	return false
}

func themeLabelsMatch(left, right string) bool {
	left = canonicalTheme(left)
	right = canonicalTheme(right)
	if left == "" || right == "" {
		return false
	}
	leftCompact := compactTheme(left)
	rightCompact := compactTheme(right)
	return leftCompact == rightCompact || strings.Contains(leftCompact, rightCompact) || strings.Contains(rightCompact, leftCompact)
}

func streakFromLeaderLabel(label string) int {
	fields := strings.Fields(strings.TrimSpace(label))
	for _, field := range fields[1:] {
		field = strings.TrimSuffix(field, "板")
		if value, err := strconv.Atoi(field); err == nil {
			return value
		}
	}
	return 0
}

func fillThemeThresholdFromEvents(target *ShortTermThemeThresholds, events []foundation.LimitUpEvent, theme string) {
	if target == nil {
		return
	}
	date := latestLimitDate(events)
	for _, event := range events {
		if event.Date.Format("2006-01-02") != date || !limitEventMatchesTheme(event, theme) {
			continue
		}
		target.LimitUpCount++
		if event.Streak >= 2 {
			target.BoardCount++
		}
		target.MaxStreak = max(target.MaxStreak, event.Streak)
	}
	if target.LimitUpCount > 0 {
		target.Source = "limit-up-events"
	}
}

func formatSignedThreshold(value float64) string {
	if value > 0 {
		return fmt.Sprintf("+%.1f%%", value)
	}
	return fmt.Sprintf("%.1f%%", value)
}

func shortTermPeerLabel(peers []ShortTermPeerReference, limit int) string {
	if len(peers) == 0 {
		return "同题材核心股（当前缺少名单）"
	}
	labels := make([]string, 0, min(limit, len(peers)))
	for _, peer := range peers {
		label := peer.Name
		if peer.Symbol != "" {
			label += "（" + peer.Symbol + "）"
		}
		if peer.Streak > 0 {
			label += fmt.Sprintf("%d板", peer.Streak)
		} else if peer.Role != "" {
			label += peer.Role
		}
		labels = append(labels, label)
		if len(labels) >= limit {
			break
		}
	}
	return strings.Join(labels, "、")
}

func quantifiedPeerCondition(peerLabel string, minimumPositive, maximumWeak int, moment string) string {
	if minimumPositive <= 0 {
		return "同题材核心股名单缺失，补齐前不允许用板块联动作为参与依据"
	}
	return fmt.Sprintf("%s：%s中至少%d只涨幅≥0%%，且跌幅≤-3%%的不超过%d只", moment, peerLabel, minimumPositive, maximumWeak)
}
