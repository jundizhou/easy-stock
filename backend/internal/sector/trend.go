package sector

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/narrative"
)

type trendDayStats struct {
	stocks       map[string]foundation.LimitUpEvent
	boardCount   int
	maxStreak    int
	heightEnergy int
}

type trendLeader struct {
	event    foundation.LimitUpEvent
	dateRank int
}

type trendAggregate struct {
	name       string
	days       map[string]*trendDayStats
	symbols    map[string]struct{}
	leaders    map[string]trendLeader
	activeDays int
	raw        float64
}

type rankedTrendOverview struct {
	overview foundation.ThemeOverview
	raw      float64
}

func buildTrendOverviews(events []foundation.LimitUpEvent, catalog []foundation.StockCatalogEntry) []foundation.ThemeOverview {
	if len(events) == 0 || len(catalog) == 0 {
		return nil
	}
	catalogBySymbol := make(map[string]foundation.StockCatalogEntry, len(catalog))
	universeCounts := map[string]int{}
	for _, entry := range catalog {
		catalogBySymbol[entry.Symbol] = entry
		for name := range narrative.Memberships(entry.Concepts) {
			if !narrative.IsStructural(name) {
				universeCounts[name]++
			}
		}
	}

	dateSet := map[string]struct{}{}
	for _, event := range events {
		if !event.Date.IsZero() {
			dateSet[event.Date.Format("2006-01-02")] = struct{}{}
		}
	}
	dates := make([]string, 0, len(dateSet))
	for date := range dateSet {
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	if len(dates) > 5 {
		dates = dates[:5]
	}
	if len(dates) == 0 {
		return nil
	}
	dateRanks := make(map[string]int, len(dates))
	for index, date := range dates {
		dateRanks[date] = index
	}

	eventsByDate := map[string]map[string]foundation.LimitUpEvent{}
	dayThemeCounts := map[string]map[string]int{}
	dayThemeBoards := map[string]map[string]int{}
	for _, event := range events {
		date := event.Date.Format("2006-01-02")
		_, ok := dateRanks[date]
		if !ok || event.Symbol == "" || isTrendST(event.Name) {
			continue
		}
		_, ok = catalogBySymbol[event.Symbol]
		if !ok {
			continue
		}
		if eventsByDate[date] == nil {
			eventsByDate[date] = map[string]foundation.LimitUpEvent{}
			dayThemeCounts[date] = map[string]int{}
			dayThemeBoards[date] = map[string]int{}
		}
		previous, exists := eventsByDate[date][event.Symbol]
		if exists && previous.Streak >= event.Streak && previous.Amount >= event.Amount {
			continue
		}
		eventsByDate[date][event.Symbol] = event
	}
	for date, dayEvents := range eventsByDate {
		for symbol, event := range dayEvents {
			entry := catalogBySymbol[symbol]
			for name := range narrative.Memberships(entry.Concepts) {
				if name == "" || narrative.IsStructural(name) {
					continue
				}
				dayThemeCounts[date][name]++
				if event.Streak >= 2 {
					dayThemeBoards[date][name]++
				}
			}
		}
	}

	aggregates := map[string]*trendAggregate{}
	for date, dayEvents := range eventsByDate {
		dateRank := dateRanks[date]
		for symbol, event := range dayEvents {
			entry := catalogBySymbol[symbol]
			memberships := narrative.Memberships(entry.Concepts)
			name := primaryTrendNarrative(event, memberships, dayThemeCounts[date], dayThemeBoards[date], universeCounts, len(catalog))
			if name == "" || narrative.IsStructural(name) {
				continue
			}
			aggregate := aggregates[name]
			if aggregate == nil {
				aggregate = &trendAggregate{
					name:    name,
					days:    map[string]*trendDayStats{},
					symbols: map[string]struct{}{},
					leaders: map[string]trendLeader{},
				}
				aggregates[name] = aggregate
			}
			day := aggregate.days[date]
			if day == nil {
				day = &trendDayStats{stocks: map[string]foundation.LimitUpEvent{}}
				aggregate.days[date] = day
			}
			day.stocks[symbol] = event
			day.maxStreak = max(day.maxStreak, max(event.Streak, 1))
			if event.Streak >= 2 {
				day.boardCount++
				day.heightEnergy += event.Streak - 1
			}
			aggregate.symbols[symbol] = struct{}{}
			previous, exists := aggregate.leaders[symbol]
			if !exists || dateRank < previous.dateRank || (dateRank == previous.dateRank && event.Streak > previous.event.Streak) {
				aggregate.leaders[symbol] = trendLeader{event: event, dateRank: dateRank}
			}
		}
	}

	weights := []float64{1, 0.72, 0.52, 0.38, 0.28}
	maxRaw := 0.0
	for name, aggregate := range aggregates {
		for index, date := range dates {
			day := aggregate.days[date]
			if day == nil {
				continue
			}
			aggregate.activeDays++
			count := len(day.stocks)
			weight := weights[min(index, len(weights)-1)]
			aggregate.raw += weight * (float64(count)*2.2 + float64(day.boardCount)*6.5 + float64(day.heightEnergy)*1.8 + float64(day.maxStreak)*3)
		}
		members := universeCounts[name]
		specificity := trendSpecificity(members, len(catalog))
		aggregate.raw += float64(aggregate.activeDays)*2.8 + specificity*8
		currentCount := lenTrendDay(aggregate.days[dates[0]])
		previousCount := 0
		if len(dates) > 1 {
			previousCount = lenTrendDay(aggregate.days[dates[1]])
		}
		if currentCount > previousCount {
			aggregate.raw += float64(currentCount-previousCount) * 1.5
		}
		maxStreak := trendMaxStreak(aggregate)
		if len(aggregate.symbols) < 2 && maxStreak < 3 && !(maxStreak >= 2 && aggregate.activeDays >= 2) {
			delete(aggregates, name)
			continue
		}
		maxRaw = math.Max(maxRaw, aggregate.raw)
	}
	if maxRaw == 0 {
		return nil
	}

	ranked := make([]rankedTrendOverview, 0, len(aggregates))
	for name, aggregate := range aggregates {
		current := aggregate.days[dates[0]]
		previous := (*trendDayStats)(nil)
		if len(dates) > 1 {
			previous = aggregate.days[dates[1]]
		}
		trendScore := int(math.Round(25 + aggregate.raw/maxRaw*75))
		leaders := trendLeaderLabels(aggregate.leaders, 3)
		changePercent, rising, falling, matched := trendActivePerformance(aggregate.symbols, catalogBySymbol)
		topNode := ""
		topNodeChange := 0.0
		if len(leaders) > 0 {
			topNode = strings.Fields(leaders[0])[0]
			for symbol, leader := range aggregate.leaders {
				if leader.event.Name == topNode {
					topNodeChange = catalogBySymbol[symbol].ChangePercent
					break
				}
			}
		}
		overview := foundation.ThemeOverview{
			Theme:                narrative.ThemeID(name),
			Name:                 name,
			ChangePercent:        changePercent,
			RisingNodes:          rising,
			FallingNodes:         falling,
			MatchedNodes:         matched,
			TotalNodes:           universeCounts[name],
			TopNode:              topNode,
			TopNodeChangePercent: topNodeChange,
			TrendScore:           trendScore,
			TrendStage:           trendStage(current, previous, trendScore),
			LimitUpCount:         lenTrendDay(current),
			BoardCount:           trendBoardCount(current),
			PreviousCount:        lenTrendDay(previous),
			ActiveDays:           aggregate.activeDays,
			MaxStreak:            trendMaxStreak(aggregate),
			Leaders:              leaders,
			TradeDate:            dates[0],
		}
		ranked = append(ranked, rankedTrendOverview{overview: overview, raw: aggregate.raw})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].raw != ranked[j].raw {
			return ranked[i].raw > ranked[j].raw
		}
		return ranked[i].overview.Name < ranked[j].overview.Name
	})
	if len(ranked) > 16 {
		ranked = ranked[:16]
	}
	items := make([]foundation.ThemeOverview, len(ranked))
	for index := range ranked {
		items[index] = ranked[index].overview
	}
	return items
}

func primaryTrendNarrative(
	event foundation.LimitUpEvent,
	memberships map[string][]string,
	dayCounts map[string]int,
	dayBoards map[string]int,
	universeCounts map[string]int,
	universeSize int,
) string {
	bestName := ""
	bestScore := -1.0
	for name, labels := range memberships {
		if narrative.IsStructural(name) {
			continue
		}
		peerCount := max(dayCounts[name]-1, 0)
		peerBoard := dayBoards[name]
		if event.Streak >= 2 {
			peerBoard = max(peerBoard-1, 0)
		}
		score := (float64(peerCount)*2 + float64(peerBoard)*6 + trendSpecificity(universeCounts[name], universeSize)*10) * narrative.BreadthFactor(name)
		score += narrative.EvidenceBonus(name, labels)
		if score > bestScore || (score == bestScore && name < bestName) {
			bestName = name
			bestScore = score
		}
	}
	return bestName
}

func trendLeaderLabels(leaders map[string]trendLeader, limit int) []string {
	items := make([]trendLeader, 0, len(leaders))
	for _, leader := range leaders {
		items = append(items, leader)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].event.Streak != items[j].event.Streak {
			return items[i].event.Streak > items[j].event.Streak
		}
		if items[i].dateRank != items[j].dateRank {
			return items[i].dateRank < items[j].dateRank
		}
		if items[i].event.Amount != items[j].event.Amount {
			return items[i].event.Amount > items[j].event.Amount
		}
		return items[i].event.Name < items[j].event.Name
	})
	if len(items) > limit {
		items = items[:limit]
	}
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, fmt.Sprintf("%s %d板", item.event.Name, max(item.event.Streak, 1)))
	}
	return labels
}

func trendActivePerformance(symbols map[string]struct{}, catalog map[string]foundation.StockCatalogEntry) (float64, int, int, int) {
	total := 0.0
	count := 0
	rising := 0
	falling := 0
	for symbol := range symbols {
		entry, ok := catalog[symbol]
		if !ok {
			continue
		}
		total += entry.ChangePercent
		count++
		if entry.ChangePercent > 0 {
			rising++
		} else if entry.ChangePercent < 0 {
			falling++
		}
	}
	if count == 0 {
		return 0, 0, 0, 0
	}
	return total / float64(count), rising, falling, count
}

func trendStage(current *trendDayStats, previous *trendDayStats, score int) string {
	currentCount := lenTrendDay(current)
	previousCount := lenTrendDay(previous)
	if currentCount == 0 {
		return "退潮"
	}
	if score >= 72 && trendBoardCount(current) > 0 && currentCount >= previousCount {
		return "主升"
	}
	if currentCount > previousCount {
		return "扩散"
	}
	if currentCount < previousCount {
		return "分歧"
	}
	return "发酵"
}

func trendSpecificity(members int, universe int) float64 {
	if universe <= 1 || members <= 0 {
		return 0.5
	}
	value := math.Log(float64(universe+1)/float64(members+1)) / math.Log(float64(universe+1))
	return math.Max(0.05, math.Min(0.95, value))
}

func trendMaxStreak(aggregate *trendAggregate) int {
	maximum := 0
	for _, day := range aggregate.days {
		maximum = max(maximum, day.maxStreak)
	}
	return maximum
}

func lenTrendDay(day *trendDayStats) int {
	if day == nil {
		return 0
	}
	return len(day.stocks)
}

func trendBoardCount(day *trendDayStats) int {
	if day == nil {
		return 0
	}
	return day.boardCount
}

func isTrendST(name string) bool {
	upper := strings.ToUpper(strings.TrimSpace(name))
	return strings.HasPrefix(upper, "ST") || strings.HasPrefix(upper, "*ST")
}
