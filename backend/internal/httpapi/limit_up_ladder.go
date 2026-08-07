package httpapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

type limitUpLadderStock struct {
	Symbol               string   `json:"symbol"`
	Name                 string   `json:"name"`
	Price                float64  `json:"price"`
	ChangePercent        float64  `json:"change_percent"`
	CurrentChangePercent *float64 `json:"current_change_percent,omitempty"`
	Amount               float64  `json:"amount"`
	FloatMarketCap       float64  `json:"float_market_cap"`
	TurnoverRate         float64  `json:"turnover_rate"`
	Streak               int      `json:"streak"`
	FirstLimitTime       string   `json:"first_limit_time,omitempty"`
	LastLimitTime        string   `json:"last_limit_time,omitempty"`
	OpenCount            int      `json:"open_count"`
	Industry             string   `json:"industry,omitempty"`
	Days                 int      `json:"days"`
	Count                int      `json:"count"`
	StreakLabel          string   `json:"streak_label,omitempty"`
	BoardType            string   `json:"board_type,omitempty"`
	IsST                 bool     `json:"is_st"`
	LimitRegime          string   `json:"limit_regime"`
	RawConcepts          []string `json:"raw_concepts,omitempty"`
	PrimaryTheme         string   `json:"primary_theme,omitempty"`
	SecondaryThemes      []string `json:"secondary_themes,omitempty"`
	ThemeConfidence      float64  `json:"theme_confidence,omitempty"`
	ThemeEvidence        []string `json:"theme_evidence,omitempty"`
	ThemeSource          string   `json:"theme_source,omitempty"`
	ThemeRank            int      `json:"theme_rank,omitempty"`
	ThemeLeaderRole      string   `json:"theme_leader_role,omitempty"`
	Source               string   `json:"source,omitempty"`
}

type limitUpLadderLevel struct {
	Level  int                  `json:"level"`
	Label  string               `json:"label"`
	Count  int                  `json:"count"`
	Stocks []limitUpLadderStock `json:"stocks"`
}

type limitUpLadderDay struct {
	TradeDate       string               `json:"trade_date"`
	LimitUpCount    int                  `json:"limit_up_count"`
	BoardCount      int                  `json:"board_count"`
	FirstBoardCount int                  `json:"first_board_count"`
	MaxStreak       int                  `json:"max_streak"`
	ReopenedCount   int                  `json:"reopened_count"`
	STCount         int                  `json:"st_count"`
	TotalAmount     float64              `json:"total_amount"`
	Levels          []limitUpLadderLevel `json:"levels"`
}

type limitUpAdvanceStep struct {
	FromLevel int     `json:"from_level"`
	ToLevel   int     `json:"to_level"`
	Base      int     `json:"base"`
	Success   int     `json:"success"`
	Rate      float64 `json:"rate"`
}

type limitUpIndustryHeat struct {
	Name       string  `json:"name"`
	Count      int     `json:"count"`
	BoardCount int     `json:"board_count"`
	MaxStreak  int     `json:"max_streak"`
	Heat       float64 `json:"heat"`
}

type limitUpConceptHeat struct {
	Name          string   `json:"name"`
	Count         int      `json:"count"`
	BoardCount    int      `json:"board_count"`
	MaxStreak     int      `json:"max_streak"`
	PreviousCount int      `json:"previous_count"`
	Heat          float64  `json:"heat"`
	Leaders       []string `json:"leaders,omitempty"`
}

type limitUpLadderData struct {
	SessionStatus string                `json:"session_status"`
	Current       limitUpLadderDay      `json:"current"`
	Previous      limitUpLadderDay      `json:"previous"`
	Advance       []limitUpAdvanceStep  `json:"advance"`
	IndustryHeat  []limitUpIndustryHeat `json:"industry_heat"`
	ConceptHeat   []limitUpConceptHeat  `json:"concept_heat"`
	ConceptStatus string                `json:"concept_status"`
	ConceptError  string                `json:"concept_error,omitempty"`
	ConceptMeta   foundation.SourceMeta `json:"concept_meta"`
	Meta          foundation.SourceMeta `json:"meta"`
}

type limitUpLadderSnapshot struct {
	data      limitUpLadderData
	expiresAt time.Time
}

type limitUpLadderFlight struct {
	done chan struct{}
	data limitUpLadderData
	err  error
}

type limitUpLadderCache struct {
	mu       sync.Mutex
	ttl      time.Duration
	snapshot limitUpLadderSnapshot
	inflight *limitUpLadderFlight
}

func newLimitUpLadderCache(ttl time.Duration) *limitUpLadderCache {
	return &limitUpLadderCache{ttl: ttl}
}

func (c *limitUpLadderCache) load(ctx context.Context, provider LimitUpProvider, conceptProvider StockConceptProvider, realtimeProvider RealtimeProvider) (limitUpLadderData, error) {
	now := time.Now()
	c.mu.Lock()
	if !c.snapshot.expiresAt.IsZero() && now.Before(c.snapshot.expiresAt) {
		data := c.snapshot.data
		c.mu.Unlock()
		return data, nil
	}
	if c.inflight != nil {
		flight := c.inflight
		c.mu.Unlock()
		select {
		case <-flight.done:
			return flight.data, flight.err
		case <-ctx.Done():
			return limitUpLadderData{}, ctx.Err()
		}
	}
	flight := &limitUpLadderFlight{done: make(chan struct{})}
	c.inflight = flight
	c.mu.Unlock()

	var events []foundation.LimitUpEvent
	var catalog []foundation.StockCatalogEntry
	var limitUpErr error
	var conceptErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		events, limitUpErr = provider.RecentLimitUps(ctx, 8)
	}()
	if conceptProvider != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			catalog, conceptErr = conceptProvider.StockCatalog(ctx)
		}()
	}
	wg.Wait()
	if limitUpErr == nil {
		flight.data, limitUpErr = buildLimitUpLadder(events, catalog, time.Now())
		if limitUpErr == nil {
			enrichPreviousCurrentChanges(ctx, &flight.data.Previous, realtimeProvider)
			hasPrimaryConcepts := limitUpDayHasRawConcepts(flight.data.Current)
			switch {
			case hasPrimaryConcepts:
				flight.data.ConceptStatus = "ready"
			case conceptProvider == nil:
				flight.data.ConceptStatus = "unavailable"
			case conceptErr != nil:
				flight.data.ConceptStatus = "degraded"
				flight.data.ConceptError = conceptErr.Error()
			case len(catalog) > 0:
				flight.data.ConceptStatus = "ready"
			default:
				flight.data.ConceptStatus = "unavailable"
			}
		}
	}
	flight.err = limitUpErr

	c.mu.Lock()
	c.inflight = nil
	if limitUpErr == nil {
		c.snapshot = limitUpLadderSnapshot{data: flight.data, expiresAt: time.Now().Add(c.ttl)}
	}
	close(flight.done)
	c.mu.Unlock()
	return flight.data, flight.err
}

func (s *Server) limitUpLadderHandler(w http.ResponseWriter, r *http.Request) {
	if s.limitUpProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "limit-up provider is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	data, err := s.limitUpSnapshots.load(ctx, s.limitUpProvider, s.stockConcepts, s.realtimeProvider)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func enrichPreviousCurrentChanges(ctx context.Context, previous *limitUpLadderDay, provider RealtimeProvider) {
	if previous == nil || provider == nil {
		return
	}
	symbolSet := make(map[string]struct{})
	for _, level := range previous.Levels {
		for _, stock := range level.Stocks {
			if stock.Symbol != "" {
				symbolSet[stock.Symbol] = struct{}{}
			}
		}
	}
	if len(symbolSet) == 0 {
		return
	}
	symbols := make([]string, 0, len(symbolSet))
	for symbol := range symbolSet {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	quotes, err := provider.Realtime(ctx, symbols)
	if err != nil {
		return
	}
	changes := make(map[string]float64, len(quotes))
	for _, quote := range quotes {
		if quote.Symbol != "" {
			changes[quote.Symbol] = quote.ChangePercent
		}
	}
	for levelIndex := range previous.Levels {
		for stockIndex := range previous.Levels[levelIndex].Stocks {
			stock := &previous.Levels[levelIndex].Stocks[stockIndex]
			if change, ok := changes[stock.Symbol]; ok {
				stock.CurrentChangePercent = &change
			}
		}
	}
}

func buildLimitUpLadder(events []foundation.LimitUpEvent, catalog []foundation.StockCatalogEntry, now time.Time) (limitUpLadderData, error) {
	byDate := map[string][]foundation.LimitUpEvent{}
	var fallbackMeta foundation.SourceMeta
	for _, event := range events {
		if event.Date.IsZero() {
			continue
		}
		date := event.Date.Format("2006-01-02")
		byDate[date] = append(byDate[date], event)
		if fallbackMeta.Source == "" && event.Meta.Source != "" {
			fallbackMeta = event.Meta
		}
	}
	if len(byDate) == 0 {
		return limitUpLadderData{}, fmt.Errorf("recent limit-up pool returned no trading-day data")
	}
	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	meta := fallbackMeta
	for _, event := range byDate[dates[0]] {
		if event.Meta.Source != "" {
			meta = event.Meta
			break
		}
	}
	catalogBySymbol := stockConceptCatalog(catalog)
	current := buildLimitUpDay(dates[0], byDate[dates[0]], catalogBySymbol)
	previous := limitUpLadderDay{}
	if len(dates) > 1 {
		previous = buildLimitUpDay(dates[1], byDate[dates[1]], catalogBySymbol)
	}
	conceptHeat := attributeLimitUpThemes(&current, &previous, catalog)
	conceptMeta := limitUpConceptSourceMeta(byDate[dates[0]], catalog)
	if meta.Source == "" {
		meta.Source = "eastmoney:limit-up-pool"
	}
	meta.FetchedAt = now
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	localNow := now.In(location)
	status := "最近交易日"
	if current.TradeDate == localNow.Format("2006-01-02") {
		status = "收盘快照"
		if localNow.Hour() < 15 || (localNow.Hour() == 15 && localNow.Minute() < 5) {
			status = "盘中快照"
		}
	}
	return limitUpLadderData{
		SessionStatus: status,
		Current:       current,
		Previous:      previous,
		Advance:       buildAdvanceSteps(previous, current),
		IndustryHeat:  buildIndustryHeat(current),
		ConceptHeat:   conceptHeat,
		ConceptMeta:   conceptMeta,
		Meta:          meta,
	}, nil
}

func buildLimitUpDay(date string, events []foundation.LimitUpEvent, catalog map[string]foundation.StockCatalogEntry) limitUpLadderDay {
	bySymbol := map[string]foundation.LimitUpEvent{}
	for _, event := range events {
		if event.Symbol == "" {
			continue
		}
		previous, exists := bySymbol[event.Symbol]
		if !exists || event.Streak > previous.Streak || event.Amount > previous.Amount {
			bySymbol[event.Symbol] = event
		}
	}
	levels := map[int][]limitUpLadderStock{}
	day := limitUpLadderDay{TradeDate: date}
	for _, event := range bySymbol {
		level := max(event.Streak, 1)
		stock := limitUpStockFromEvent(event, level, catalog[event.Symbol])
		levels[level] = append(levels[level], stock)
		if stock.IsST {
			day.STCount++
			continue
		}
		day.LimitUpCount++
		day.TotalAmount += event.Amount
		day.MaxStreak = max(day.MaxStreak, level)
		if level >= 2 {
			day.BoardCount++
		} else {
			day.FirstBoardCount++
		}
		if event.OpenCount > 0 {
			day.ReopenedCount++
		}
	}
	levelValues := make([]int, 0, len(levels))
	for level := range levels {
		levelValues = append(levelValues, level)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(levelValues)))
	for _, level := range levelValues {
		stocks := levels[level]
		sort.SliceStable(stocks, func(i, j int) bool {
			if stocks[i].IsST != stocks[j].IsST {
				return !stocks[i].IsST
			}
			if stocks[i].OpenCount != stocks[j].OpenCount {
				return stocks[i].OpenCount < stocks[j].OpenCount
			}
			if stocks[i].FirstLimitTime != stocks[j].FirstLimitTime {
				return stocks[i].FirstLimitTime < stocks[j].FirstLimitTime
			}
			return stocks[i].Amount > stocks[j].Amount
		})
		day.Levels = append(day.Levels, limitUpLadderLevel{
			Level:  level,
			Label:  fmt.Sprintf("%d板", level),
			Count:  len(stocks),
			Stocks: stocks,
		})
	}
	return day
}

func limitUpStockFromEvent(event foundation.LimitUpEvent, level int, catalog foundation.StockCatalogEntry) limitUpLadderStock {
	industry := strings.TrimSpace(event.Industry)
	if industry == "" {
		industry = strings.TrimSpace(catalog.Industry)
	}
	rawConcepts := append([]string(nil), event.Concepts...)
	if len(rawConcepts) == 0 {
		rawConcepts = append([]string(nil), catalog.Concepts...)
	} else if isKaipanlaThemeLeaderSource(event.ThemeSource) {
		for _, concept := range catalog.Concepts {
			if !containsConceptLabel(rawConcepts, concept) {
				rawConcepts = append(rawConcepts, concept)
			}
		}
	}
	return limitUpLadderStock{
		Symbol:          event.Symbol,
		Name:            event.Name,
		Price:           event.Price,
		ChangePercent:   event.ChangePercent,
		Amount:          event.Amount,
		FloatMarketCap:  event.FloatMarketCap,
		TurnoverRate:    event.TurnoverRate,
		Streak:          level,
		FirstLimitTime:  event.FirstLimitTime,
		LastLimitTime:   event.LastLimitTime,
		OpenCount:       event.OpenCount,
		Industry:        industry,
		Days:            event.Days,
		Count:           event.Count,
		StreakLabel:     event.StreakLabel,
		BoardType:       event.BoardType,
		IsST:            isSTStockName(event.Name),
		LimitRegime:     stockLimitRegime(event.Symbol),
		RawConcepts:     rawConcepts,
		PrimaryTheme:    event.PrimaryTheme,
		ThemeSource:     event.ThemeSource,
		ThemeRank:       event.ThemeRank,
		ThemeLeaderRole: event.ThemeLeaderRole,
		Source:          event.Meta.Source,
	}
}

func limitUpDayHasRawConcepts(day limitUpLadderDay) bool {
	for _, level := range day.Levels {
		for _, stock := range level.Stocks {
			if len(stock.RawConcepts) > 0 {
				return true
			}
		}
	}
	return false
}

func limitUpConceptSourceMeta(events []foundation.LimitUpEvent, catalog []foundation.StockCatalogEntry) foundation.SourceMeta {
	for _, event := range events {
		if len(event.Concepts) > 0 {
			meta := event.Meta
			if len(catalog) > 0 {
				meta.Source += "+eastmoney:stock-selection-fallback"
			}
			return meta
		}
	}
	if len(catalog) > 0 {
		return catalog[0].Meta
	}
	return foundation.SourceMeta{}
}

func buildAdvanceSteps(previous limitUpLadderDay, current limitUpLadderDay) []limitUpAdvanceStep {
	currentBySymbol := map[string]limitUpLadderStock{}
	for _, level := range current.Levels {
		for _, stock := range level.Stocks {
			if !stock.IsST {
				currentBySymbol[stock.Symbol] = stock
			}
		}
	}
	baseByLevel := map[int]int{}
	successByLevel := map[int]int{}
	for _, level := range previous.Levels {
		for _, stock := range level.Stocks {
			if stock.IsST {
				continue
			}
			from := max(stock.Streak, 1)
			baseByLevel[from]++
			if currentStock, ok := currentBySymbol[stock.Symbol]; ok && currentStock.Streak >= from+1 {
				successByLevel[from]++
			}
		}
	}
	levels := make([]int, 0, len(baseByLevel))
	for level := range baseByLevel {
		levels = append(levels, level)
	}
	sort.Ints(levels)
	steps := make([]limitUpAdvanceStep, 0, len(levels))
	for _, level := range levels {
		base := baseByLevel[level]
		success := successByLevel[level]
		steps = append(steps, limitUpAdvanceStep{
			FromLevel: level,
			ToLevel:   level + 1,
			Base:      base,
			Success:   success,
			Rate:      float64(success) / float64(max(base, 1)),
		})
	}
	return steps
}

func buildIndustryHeat(day limitUpLadderDay) []limitUpIndustryHeat {
	byIndustry := map[string]*limitUpIndustryHeat{}
	for _, level := range day.Levels {
		for _, stock := range level.Stocks {
			if stock.IsST {
				continue
			}
			name := strings.TrimSpace(stock.Industry)
			if name == "" {
				name = "未分类"
			}
			item := byIndustry[name]
			if item == nil {
				item = &limitUpIndustryHeat{Name: name}
				byIndustry[name] = item
			}
			item.Count++
			item.MaxStreak = max(item.MaxStreak, stock.Streak)
			if stock.Streak >= 2 {
				item.BoardCount++
			}
		}
	}
	items := make([]limitUpIndustryHeat, 0, len(byIndustry))
	maxRaw := 0.0
	for _, item := range byIndustry {
		raw := float64(item.Count*10 + item.BoardCount*8 + item.MaxStreak*6)
		item.Heat = raw
		maxRaw = math.Max(maxRaw, raw)
		items = append(items, *item)
	}
	for i := range items {
		if maxRaw > 0 {
			items[i].Heat = math.Round(items[i].Heat / maxRaw * 100)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Heat != items[j].Heat {
			return items[i].Heat > items[j].Heat
		}
		if items[i].MaxStreak != items[j].MaxStreak {
			return items[i].MaxStreak > items[j].MaxStreak
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func isSTStockName(name string) bool {
	return strings.Contains(strings.ToUpper(strings.TrimSpace(name)), "ST")
}
