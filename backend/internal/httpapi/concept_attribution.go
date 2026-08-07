package httpapi

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/narrative"
)

const kaipanlaThemeLeaderSource = "duanxianxia:kaipanla-theme-leader"

type conceptAggregate struct {
	count        int
	boardCount   int
	maxStreak    int
	heightEnergy int
	streakCounts map[int]int
	leaders      []conceptLeader
}

type conceptLeader struct {
	name           string
	streak         int
	firstLimitTime string
}

type scoredStockTheme struct {
	name          string
	rawLabels     []string
	score         float64
	peerCount     int
	peerBoard     int
	peerMaxStreak int
	previousCount int
}

func stockConceptCatalog(entries []foundation.StockCatalogEntry) map[string]foundation.StockCatalogEntry {
	result := make(map[string]foundation.StockCatalogEntry, len(entries))
	for _, entry := range entries {
		if entry.Symbol == "" {
			continue
		}
		entry.Concepts = append([]string(nil), entry.Concepts...)
		result[entry.Symbol] = entry
	}
	return result
}

func attributeLimitUpThemes(current *limitUpLadderDay, previous *limitUpLadderDay, catalog []foundation.StockCatalogEntry) []limitUpConceptHeat {
	if current == nil {
		return []limitUpConceptHeat{}
	}
	currentStats := aggregateDayThemes(*current)
	previousStats := map[string]*conceptAggregate{}
	if previous != nil {
		previousStats = aggregateDayThemes(*previous)
	}
	universeCounts := aggregateUniverseThemes(catalog)
	universeSize := len(catalog)
	if universeSize == 0 {
		universeCounts, universeSize = aggregateLadderThemeUniverse(current, previous)
	}
	attributeDayThemes(current, currentStats, previousStats, universeCounts, universeSize)
	if previous != nil {
		attributeDayThemes(previous, previousStats, nil, universeCounts, universeSize)
		alignConsecutiveStockThemes(current, previous)
	}
	return buildConceptHeat(currentStats, previousStats, universeCounts, universeSize)
}

func aggregateLadderThemeUniverse(days ...*limitUpLadderDay) (map[string]int, int) {
	counts := map[string]int{}
	symbols := map[string]struct{}{}
	for _, day := range days {
		if day == nil {
			continue
		}
		for _, level := range day.Levels {
			for _, stock := range level.Stocks {
				if stock.Symbol != "" {
					symbols[stock.Symbol] = struct{}{}
				}
				for theme := range themesForRawConcepts(stock.RawConcepts) {
					counts[theme]++
				}
			}
		}
	}
	return counts, len(symbols)
}

func aggregateUniverseThemes(catalog []foundation.StockCatalogEntry) map[string]int {
	counts := map[string]int{}
	for _, entry := range catalog {
		for theme := range themesForRawConcepts(entry.Concepts) {
			counts[theme]++
		}
	}
	return counts
}

func aggregateDayThemes(day limitUpLadderDay) map[string]*conceptAggregate {
	stats := map[string]*conceptAggregate{}
	for _, level := range day.Levels {
		for _, stock := range level.Stocks {
			if stock.IsST {
				continue
			}
			for theme := range themesForRawConcepts(stock.RawConcepts) {
				item := stats[theme]
				if item == nil {
					item = &conceptAggregate{streakCounts: map[int]int{}}
					stats[theme] = item
				}
				item.count++
				item.maxStreak = max(item.maxStreak, stock.Streak)
				item.streakCounts[stock.Streak]++
				if stock.Streak >= 2 {
					item.boardCount++
					item.heightEnergy += stock.Streak - 1
				}
				item.leaders = append(item.leaders, conceptLeader{name: stock.Name, streak: stock.Streak, firstLimitTime: stock.FirstLimitTime})
			}
		}
	}
	return stats
}

func attributeDayThemes(
	day *limitUpLadderDay,
	stats map[string]*conceptAggregate,
	previousStats map[string]*conceptAggregate,
	universeCounts map[string]int,
	universeSize int,
) {
	if day == nil {
		return
	}
	for levelIndex := range day.Levels {
		for stockIndex := range day.Levels[levelIndex].Stocks {
			stock := &day.Levels[levelIndex].Stocks[stockIndex]
			authoritativeTheme := strings.TrimSpace(stock.PrimaryTheme)
			authoritativeSource := strings.TrimSpace(stock.ThemeSource)
			candidates := themesForRawConcepts(stock.RawConcepts)
			scored := make([]scoredStockTheme, 0, len(candidates))
			for theme, rawLabels := range candidates {
				item := stats[theme]
				if item == nil {
					continue
				}
				previousCount := 0
				if previousStats != nil && previousStats[theme] != nil {
					previousCount = previousStats[theme].count
				}
				peerCount := max(item.count-1, 0)
				peerBoard := item.boardCount
				peerEnergy := item.heightEnergy
				if stock.Streak >= 2 {
					peerBoard = max(peerBoard-1, 0)
					peerEnergy = max(peerEnergy-(stock.Streak-1), 0)
				}
				peerMax := peerMaxStreak(item, stock.Streak)
				specificity := conceptSpecificity(universeCounts[theme], universeSize)
				score := float64(min(peerCount, 12))*2 +
					float64(min(peerBoard, 5))*7 +
					float64(min(peerEnergy, 12))*2 +
					float64(min(peerMax, 4))*4 +
					float64(min(previousCount, 8)) +
					specificity*8
				score *= broadThemeFactor(theme, universeCounts[theme], universeSize)
				score += narrative.EvidenceBonus(theme, rawLabels)
				scored = append(scored, scoredStockTheme{
					name:          theme,
					rawLabels:     rawLabels,
					score:         score,
					peerCount:     peerCount,
					peerBoard:     peerBoard,
					peerMaxStreak: peerMax,
					previousCount: previousCount,
				})
			}
			sort.SliceStable(scored, func(i, j int) bool {
				if scored[i].score != scored[j].score {
					return scored[i].score > scored[j].score
				}
				return scored[i].name < scored[j].name
			})
			if isKaipanlaThemeLeaderSource(authoritativeSource) && authoritativeTheme != "" {
				preferScoredTheme(scored, authoritativeTheme)
			}
			if len(scored) == 0 {
				continue
			}
			top := scored[0]
			stock.PrimaryTheme = top.name
			stock.ThemeConfidence = themeConfidence(scored)
			stock.SecondaryThemes = nil
			if isKaipanlaThemeLeaderSource(authoritativeSource) && top.name == authoritativeTheme {
				stock.ThemeConfidence = math.Max(stock.ThemeConfidence, 0.9)
				stock.ThemeEvidence = kaipanlaThemeLeaderEvidence(top, stock.ThemeRank, stock.ThemeLeaderRole)
				stock.ThemeSource = authoritativeSource
			} else {
				stock.ThemeEvidence = themeEvidence(top, stock.Source)
				stock.ThemeSource = stock.Source
			}
			secondaryReferenceScore := math.Max(top.score-narrative.EvidenceBonus(top.name, top.rawLabels), 0)
			secondaryCutoff := math.Max(14, secondaryReferenceScore*0.42)
			for _, candidate := range scored[1:] {
				if len(stock.SecondaryThemes) >= 2 {
					break
				}
				if candidate.score >= secondaryCutoff {
					stock.SecondaryThemes = append(stock.SecondaryThemes, candidate.name)
				}
			}
		}
	}
}

func preferScoredTheme(scored []scoredStockTheme, theme string) {
	for index := range scored {
		if scored[index].name != theme {
			continue
		}
		preferred := scored[index]
		copy(scored[1:index+1], scored[0:index])
		scored[0] = preferred
		return
	}
}

func kaipanlaThemeLeaderEvidence(theme scoredStockTheme, rank int, role string) []string {
	label := "开盘啦趋势题材：" + theme.name
	if rank > 0 {
		label = fmt.Sprintf("开盘啦趋势题材第%d名：%s", rank, theme.name)
	}
	if strings.TrimSpace(role) != "" {
		label += "（" + strings.TrimSpace(role) + "）"
	}
	evidence := []string{label}
	context := themeEvidence(theme, kaipanlaThemeLeaderSource)
	if len(context) > 1 {
		evidence = append(evidence, context[1:]...)
	}
	return evidence
}

func alignConsecutiveStockThemes(current *limitUpLadderDay, previous *limitUpLadderDay) {
	if current == nil || previous == nil {
		return
	}
	previousBySymbol := map[string]*limitUpLadderStock{}
	for levelIndex := range previous.Levels {
		for stockIndex := range previous.Levels[levelIndex].Stocks {
			stock := &previous.Levels[levelIndex].Stocks[stockIndex]
			previousBySymbol[stock.Symbol] = stock
		}
	}
	for levelIndex := range current.Levels {
		for stockIndex := range current.Levels[levelIndex].Stocks {
			currentStock := &current.Levels[levelIndex].Stocks[stockIndex]
			previousStock := previousBySymbol[currentStock.Symbol]
			if previousStock == nil || currentStock.Streak != previousStock.Streak+1 {
				continue
			}

			authority := currentStock
			target := previousStock
			authorityDay := "今日"
			targetDay := "昨日"
			if !hasAuthoritativeKaipanlaTheme(currentStock) {
				if !hasAuthoritativeKaipanlaTheme(previousStock) {
					continue
				}
				authority = previousStock
				target = currentStock
				authorityDay = "昨日"
				targetDay = "今日"
			}
			theme := strings.TrimSpace(authority.PrimaryTheme)
			if theme == "" || target.PrimaryTheme == theme {
				continue
			}
			rawLabels := themesForRawConcepts(target.RawConcepts)[theme]
			if len(rawLabels) == 0 {
				continue
			}

			oldPrimary := strings.TrimSpace(target.PrimaryTheme)
			target.PrimaryTheme = theme
			target.ThemeConfidence = math.Round(math.Min(0.95, math.Max(target.ThemeConfidence, authority.ThemeConfidence*0.95))*100) / 100
			target.SecondaryThemes = reorderedSecondaryThemes(theme, oldPrimary, target.SecondaryThemes)
			target.ThemeEvidence = []string{
				fmt.Sprintf("跨日一致性：%s开盘啦逐股题材为%s；该股由昨日%d板晋级今日%d板，%s沿用同一主炒线索", authorityDay, theme, previousStock.Streak, currentStock.Streak, targetDay),
				fmt.Sprintf("%s原始概念包含：%s", targetDay, strings.Join(rawLabels, "、")),
			}
			target.ThemeSource = authority.Source + ":cross-day"
		}
	}
}

func isKaipanlaLimitUpSource(source string) bool {
	return strings.Contains(source, "duanxianxia:kaipanla-limit-up")
}

func isKaipanlaThemeLeaderSource(source string) bool {
	return strings.Contains(source, "duanxianxia:kaipanla-theme-leader")
}

func hasAuthoritativeKaipanlaTheme(stock *limitUpLadderStock) bool {
	return stock != nil && (isKaipanlaLimitUpSource(stock.Source) || isKaipanlaThemeLeaderSource(stock.ThemeSource))
}

func reorderedSecondaryThemes(primary, oldPrimary string, existing []string) []string {
	result := make([]string, 0, 2)
	appendTheme := func(theme string) {
		theme = strings.TrimSpace(theme)
		if theme == "" || theme == primary || containsConceptLabel(result, theme) || len(result) >= 2 {
			return
		}
		result = append(result, theme)
	}
	appendTheme(oldPrimary)
	for _, theme := range existing {
		appendTheme(theme)
	}
	return result
}

func buildConceptHeat(
	current map[string]*conceptAggregate,
	previous map[string]*conceptAggregate,
	universeCounts map[string]int,
	universeSize int,
) []limitUpConceptHeat {
	type rankedHeat struct {
		item limitUpConceptHeat
		raw  float64
	}
	ranked := make([]rankedHeat, 0, len(current))
	maxRaw := 0.0
	for theme, stats := range current {
		if stats.count < 2 && stats.boardCount == 0 {
			continue
		}
		previousCount := 0
		if previous[theme] != nil {
			previousCount = previous[theme].count
		}
		specificity := conceptSpecificity(universeCounts[theme], universeSize)
		raw := (float64(min(stats.count, 30))*1.5 +
			float64(stats.boardCount)*7 +
			float64(min(stats.heightEnergy, 20))*3 +
			float64(stats.maxStreak)*5 +
			float64(min(previousCount, 12))*0.8 +
			specificity*10) * broadThemeFactor(theme, universeCounts[theme], universeSize)
		leaders := append([]conceptLeader(nil), stats.leaders...)
		sort.SliceStable(leaders, func(i, j int) bool {
			if leaders[i].streak != leaders[j].streak {
				return leaders[i].streak > leaders[j].streak
			}
			if leaders[i].firstLimitTime != leaders[j].firstLimitTime {
				return leaders[i].firstLimitTime < leaders[j].firstLimitTime
			}
			return leaders[i].name < leaders[j].name
		})
		leaderNames := make([]string, 0, 3)
		for _, leader := range leaders {
			if len(leaderNames) >= 3 {
				break
			}
			leaderNames = append(leaderNames, fmt.Sprintf("%s %d板", leader.name, leader.streak))
		}
		maxRaw = math.Max(maxRaw, raw)
		ranked = append(ranked, rankedHeat{item: limitUpConceptHeat{
			Name:          theme,
			Count:         stats.count,
			BoardCount:    stats.boardCount,
			MaxStreak:     stats.maxStreak,
			PreviousCount: previousCount,
			Leaders:       leaderNames,
		}, raw: raw})
	}
	for index := range ranked {
		if maxRaw > 0 {
			ranked[index].item.Heat = math.Round(ranked[index].raw / maxRaw * 100)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].item.Heat != ranked[j].item.Heat {
			return ranked[i].item.Heat > ranked[j].item.Heat
		}
		if ranked[i].item.BoardCount != ranked[j].item.BoardCount {
			return ranked[i].item.BoardCount > ranked[j].item.BoardCount
		}
		if ranked[i].item.MaxStreak != ranked[j].item.MaxStreak {
			return ranked[i].item.MaxStreak > ranked[j].item.MaxStreak
		}
		return ranked[i].item.Name < ranked[j].item.Name
	})
	items := make([]limitUpConceptHeat, len(ranked))
	for index := range ranked {
		items[index] = ranked[index].item
	}
	return items
}

func themesForRawConcepts(rawConcepts []string) map[string][]string {
	return narrative.Memberships(rawConcepts)
}

func canonicalTheme(raw string) string {
	return narrative.Canonical(raw)
}

func ignoredConcept(raw string) bool {
	return narrative.IsIgnored(raw)
}

func peerMaxStreak(item *conceptAggregate, ownStreak int) int {
	for level := item.maxStreak; level >= 1; level-- {
		count := item.streakCounts[level]
		if level == ownStreak {
			count--
		}
		if count > 0 {
			return level
		}
	}
	return 0
}

func conceptSpecificity(members int, universe int) float64 {
	if universe <= 1 || members <= 0 {
		return 0.5
	}
	value := math.Log(float64(universe+1)/float64(members+1)) / math.Log(float64(universe+1))
	return math.Max(0.05, math.Min(0.95, value))
}

func broadThemeFactor(theme string, members int, universe int) float64 {
	factor := narrative.BreadthFactor(theme)
	if universe > 0 {
		ratio := float64(members) / float64(universe)
		if ratio >= 0.2 {
			factor *= 0.78
		} else if ratio >= 0.1 {
			factor *= 0.88
		}
	}
	return factor
}

func themeConfidence(scored []scoredStockTheme) float64 {
	if len(scored) == 0 {
		return 0
	}
	top := scored[0]
	quality := math.Min(top.score/85, 1)
	separation := 1.0
	if len(scored) > 1 && top.score > 0 {
		separation = math.Min(math.Max((top.score-scored[1].score)/top.score, 0)*2, 1)
	}
	confidence := 0.38 + quality*0.34 + separation*0.18
	if top.peerBoard > 0 {
		confidence += 0.06
	}
	if top.previousCount > 0 {
		confidence += 0.03
	}
	return math.Round(math.Max(0.35, math.Min(0.95, confidence))*100) / 100
}

func themeEvidence(theme scoredStockTheme, source string) []string {
	evidence := make([]string, 0, 3)
	if len(theme.rawLabels) > 0 {
		labels := theme.rawLabels
		if len(labels) > 2 {
			labels = labels[:2]
		}
		label := "东财概念"
		if isKaipanlaLimitUpSource(source) {
			label = "开盘啦逐股题材"
		} else if isKaipanlaThemeLeaderSource(source) {
			label = "开盘啦趋势题材"
		}
		evidence = append(evidence, label+"："+strings.Join(labels, "、"))
	}
	if theme.peerCount > 0 {
		evidence = append(evidence, fmt.Sprintf("剔除本股后，%s仍有%d只涨停、%d只连板，最高%d板", theme.name, theme.peerCount, theme.peerBoard, theme.peerMaxStreak))
	} else {
		evidence = append(evidence, "同题材暂未形成独立涨停梯队，归因置信度较低")
	}
	if theme.previousCount > 0 {
		evidence = append(evidence, fmt.Sprintf("昨日同题材有%d只涨停，具备延续性证据", theme.previousCount))
	}
	return evidence
}

func containsConceptLabel(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
