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
	"easy-stock/backend/internal/marketemotion"
)

const marketEmotionBootstrapDays = 7
const currentMarketEmotionModelVersion = 2
const marketEmotionIntradayTTL = 10 * time.Minute

var shanghaiLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type marketEmotionEngine struct {
	mu            sync.Mutex
	store         *marketemotion.Store
	limitUps      LimitUpProvider
	pools         MarketPoolProvider
	kLines        KLineProvider
	kLineFallback KLineProvider
	concepts      StockConceptProvider
	now           func() time.Time
}

type marketEmotionSidePools struct {
	broken []foundation.MarketLimitEvent
	down   []foundation.MarketLimitEvent
}

type marketEmotionIntradayFlight struct {
	done     chan struct{}
	snapshot marketemotion.IntradaySnapshot
	err      error
}

type marketEmotionIntradayCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	now       func() time.Time
	snapshot  marketemotion.IntradaySnapshot
	expiresAt time.Time
	lastErr   error
	inflight  *marketEmotionIntradayFlight
}

func newMarketEmotionIntradayCache(ttl time.Duration) *marketEmotionIntradayCache {
	return &marketEmotionIntradayCache{ttl: ttl, now: time.Now}
}

func (c *marketEmotionIntradayCache) load(
	ctx context.Context,
	loader func(context.Context) (marketemotion.IntradaySnapshot, error),
) (marketemotion.IntradaySnapshot, error) {
	now := c.now()
	c.mu.Lock()
	if !c.expiresAt.IsZero() && now.Before(c.expiresAt) {
		snapshot := c.snapshot
		err := c.lastErr
		c.mu.Unlock()
		if !snapshot.UpdatedAt.IsZero() {
			return snapshot, nil
		}
		return marketemotion.IntradaySnapshot{}, err
	}
	if c.inflight != nil {
		flight := c.inflight
		c.mu.Unlock()
		select {
		case <-flight.done:
			return flight.snapshot, flight.err
		case <-ctx.Done():
			return marketemotion.IntradaySnapshot{}, ctx.Err()
		}
	}
	flight := &marketEmotionIntradayFlight{done: make(chan struct{})}
	c.inflight = flight
	c.mu.Unlock()

	snapshot, err := loader(ctx)
	computedAt := c.now()
	if err == nil {
		snapshot.UpdatedAt = computedAt
		snapshot.NextRefreshAt = computedAt.Add(c.ttl)
		snapshot.CacheTTLSecond = int(c.ttl / time.Second)
	}

	c.mu.Lock()
	c.inflight = nil
	c.expiresAt = computedAt.Add(c.ttl)
	if err == nil {
		c.snapshot = snapshot
		c.lastErr = nil
		flight.snapshot = snapshot
	} else if !c.snapshot.UpdatedAt.IsZero() {
		stale := c.snapshot
		stale.Stale = true
		stale.NextRefreshAt = c.expiresAt
		c.snapshot = stale
		c.lastErr = err
		flight.snapshot = stale
		flight.err = nil
	} else {
		c.lastErr = err
		flight.err = err
	}
	close(flight.done)
	c.mu.Unlock()
	return flight.snapshot, flight.err
}

func newMarketEmotionEngine(
	store *marketemotion.Store,
	limitUps LimitUpProvider,
	pools MarketPoolProvider,
	kLines KLineProvider,
	kLineFallback KLineProvider,
	concepts StockConceptProvider,
) *marketEmotionEngine {
	if store == nil || limitUps == nil || pools == nil || kLines == nil {
		return nil
	}
	return &marketEmotionEngine{
		store:         store,
		limitUps:      limitUps,
		pools:         pools,
		kLines:        kLines,
		kLineFallback: kLineFallback,
		concepts:      concepts,
		now:           time.Now,
	}
}

func (s *Server) marketEmotionHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if s.marketEmotion == nil {
		writeError(w, http.StatusServiceUnavailable, "market emotion service is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	history, err := s.marketEmotion.load(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if s.marketEmotionIntraday != nil && s.limitUpSnapshots != nil && s.limitUpProvider != nil {
		intraday, intradayErr := s.marketEmotionIntraday.load(ctx, func(loadCtx context.Context) (marketemotion.IntradaySnapshot, error) {
			ladder, loadErr := s.limitUpSnapshots.load(loadCtx, s.limitUpProvider, s.stockConcepts, s.realtimeProvider)
			if loadErr != nil {
				return marketemotion.IntradaySnapshot{}, loadErr
			}
			return buildMarketEmotionIntraday(ladder, history.Latest), nil
		})
		if intradayErr != nil {
			history.IntradayError = intradayErr.Error()
		} else {
			history.Intraday = &intraday
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": history})
}

func (e *marketEmotionEngine) load(ctx context.Context) (marketemotion.History, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	points, err := e.store.List(ctx, 120)
	if err != nil {
		return marketemotion.History{}, err
	}
	state, err := e.store.SyncState(ctx)
	if err != nil {
		return marketemotion.History{}, err
	}
	if e.shouldSync(points, state, e.now()) {
		syncErr := e.sync(ctx, points, state)
		points, err = e.store.List(ctx, 120)
		if err != nil {
			return marketemotion.History{}, err
		}
		state, _ = e.store.SyncState(ctx)
		if syncErr != nil && len(points) == 0 {
			return marketemotion.History{}, syncErr
		}
	}
	return buildMarketEmotionHistory(points, state), nil
}

func (e *marketEmotionEngine) shouldSync(points []marketemotion.Snapshot, state marketemotion.SyncState, now time.Time) bool {
	if len(points) == 0 {
		return true
	}
	if points[len(points)-1].ModelVersion < currentMarketEmotionModelVersion {
		return true
	}
	localNow := now.In(shanghaiLocation)
	today := localNow.Format("2006-01-02")
	if state.LastAttemptDate == today {
		return false
	}
	latest := points[len(points)-1].TradeDate
	cutoff := completedMarketDate(localNow)
	return latest < cutoff
}

func completedMarketDate(now time.Time) string {
	date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shanghaiLocation)
	if now.Weekday() == time.Saturday {
		date = date.AddDate(0, 0, -1)
	} else if now.Weekday() == time.Sunday {
		date = date.AddDate(0, 0, -2)
	} else if now.Hour() < 15 || (now.Hour() == 15 && now.Minute() < 5) {
		date = previousWeekday(date)
	}
	return date.Format("2006-01-02")
}

func previousWeekday(date time.Time) time.Time {
	for {
		date = date.AddDate(0, 0, -1)
		if date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
			return date
		}
	}
}

func (e *marketEmotionEngine) sync(
	ctx context.Context,
	existing []marketemotion.Snapshot,
	state marketemotion.SyncState,
) (syncErr error) {
	now := e.now().In(shanghaiLocation)
	state.LastAttemptDate = now.Format("2006-01-02")
	state.UpdatedAt = now
	defer func() {
		if syncErr != nil {
			state.LastError = syncErr.Error()
		} else {
			state.LastError = ""
		}
		_ = e.store.SaveSyncState(context.Background(), state)
	}()

	events, err := e.limitUps.RecentLimitUps(ctx, 24)
	if err != nil {
		return fmt.Errorf("load recent limit-up history: %w", err)
	}
	eventsByDate := map[string][]foundation.LimitUpEvent{}
	for _, event := range events {
		if event.Date.IsZero() {
			continue
		}
		date := event.Date.In(shanghaiLocation).Format("2006-01-02")
		if date <= completedMarketDate(now) {
			eventsByDate[date] = append(eventsByDate[date], event)
		}
	}
	dates := make([]string, 0, len(eventsByDate))
	for date := range eventsByDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	if len(dates) == 0 {
		return fmt.Errorf("recent limit-up history returned no completed trading day")
	}

	latestCached := ""
	rebuild := len(existing) > 0 && existing[len(existing)-1].ModelVersion < currentMarketEmotionModelVersion
	if rebuild {
		existing = nil
	}
	if len(existing) > 0 {
		latestCached = existing[len(existing)-1].TradeDate
	}
	targetDates := make([]string, 0, marketEmotionBootstrapDays)
	for _, date := range dates {
		if latestCached == "" || date > latestCached {
			targetDates = append(targetDates, date)
		}
	}
	if len(targetDates) > marketEmotionBootstrapDays {
		targetDates = targetDates[len(targetDates)-marketEmotionBootstrapDays:]
	}
	if len(targetDates) == 0 {
		state.LastSuccessDate = latestCached
		return nil
	}

	catalog := []foundation.StockCatalogEntry{}
	if e.concepts != nil {
		catalog, _ = e.concepts.StockCatalog(ctx)
	}
	catalogBySymbol := stockConceptCatalog(catalog)
	days := make(map[string]*limitUpLadderDay, len(dates))
	for _, date := range dates {
		day := buildLimitUpDay(date, eventsByDate[date], catalogBySymbol)
		days[date] = &day
	}
	for index, date := range dates {
		var previous *limitUpLadderDay
		if index > 0 {
			previous = days[dates[index-1]]
		}
		attributeLimitUpThemes(days[date], previous, catalog)
	}

	sidePools, err := e.fetchSidePools(ctx, targetDates)
	if err != nil {
		return err
	}
	previousDate := map[string]string{}
	for index := 1; index < len(dates); index++ {
		previousDate[dates[index]] = dates[index-1]
	}
	quoteSymbols := map[string]struct{}{}
	for _, date := range targetDates {
		if previous := days[previousDate[date]]; previous != nil {
			for _, stock := range visibleLimitUpStocks(*previous) {
				quoteSymbols[stock.Symbol] = struct{}{}
			}
		}
	}
	lines := e.fetchDailyLines(ctx, quoteSymbols)
	history := append([]marketemotion.Snapshot(nil), existing...)
	newSnapshots := make([]marketemotion.Snapshot, 0, len(targetDates))
	for _, date := range targetDates {
		current := days[date]
		if current == nil {
			continue
		}
		var previous *limitUpLadderDay
		if previousDate[date] != "" {
			previous = days[previousDate[date]]
		}
		raw := calculateMarketEmotionRaw(*current, previous, sidePools[date], lines[date])
		if previous != nil && len(visibleLimitUpStocks(*previous)) > 0 && raw.QuoteCoverage < 0.6 {
			return fmt.Errorf("%s historical quote coverage %.1f%% is too low; cache was not updated", date, raw.QuoteCoverage*100)
		}
		snapshot := scoreMarketEmotion(date, raw, history, now)
		history = append(history, snapshot)
		newSnapshots = append(newSnapshots, snapshot)
		state.LastSuccessDate = date
	}
	for _, snapshot := range newSnapshots {
		if err := e.store.Upsert(ctx, snapshot); err != nil {
			return err
		}
	}
	return nil
}

func (e *marketEmotionEngine) fetchSidePools(ctx context.Context, dates []string) (map[string]marketEmotionSidePools, error) {
	result := make(map[string]marketEmotionSidePools, len(dates))
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4)
	for _, rawDate := range dates {
		rawDate := rawDate
		date, err := time.ParseInLocation("2006-01-02", rawDate, shanghaiLocation)
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			broken, brokenErr := e.pools.BrokenLimitUpPool(ctx, date)
			down, downErr := e.pools.LimitDownPool(ctx, date)
			mu.Lock()
			defer mu.Unlock()
			if brokenErr != nil && firstErr == nil {
				firstErr = fmt.Errorf("load %s broken-board pool: %w", rawDate, brokenErr)
			}
			if downErr != nil && firstErr == nil {
				firstErr = fmt.Errorf("load %s limit-down pool: %w", rawDate, downErr)
			}
			result[rawDate] = marketEmotionSidePools{broken: broken, down: down}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return result, firstErr
}

func (e *marketEmotionEngine) fetchDailyLines(ctx context.Context, symbols map[string]struct{}) map[string]map[string]foundation.KLine {
	result := map[string]map[string]foundation.KLine{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 12)
	for symbol := range symbols {
		symbol := symbol
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			items, err := e.kLines.KLine(ctx, symbol, "day", 40)
			if (err != nil || len(items) == 0) && e.kLineFallback != nil {
				items, err = e.kLineFallback.KLine(ctx, symbol, "day", 40)
			}
			if err != nil || len(items) == 0 {
				return
			}
			sort.Slice(items, func(i, j int) bool { return items[i].Time.Before(items[j].Time) })
			for index := 1; index < len(items); index++ {
				if items[index].ChangePercent == 0 && items[index-1].Close > 0 {
					items[index].ChangePercent = (items[index].Close/items[index-1].Close - 1) * 100
				}
			}
			mu.Lock()
			defer mu.Unlock()
			for _, item := range items {
				date := item.Time.In(shanghaiLocation).Format("2006-01-02")
				if result[date] == nil {
					result[date] = map[string]foundation.KLine{}
				}
				result[date][symbol] = item
			}
		}()
	}
	wg.Wait()
	return result
}

func visibleLimitUpStocks(day limitUpLadderDay) []limitUpLadderStock {
	stocks := []limitUpLadderStock{}
	for _, level := range day.Levels {
		for _, stock := range level.Stocks {
			if !stock.IsST {
				stocks = append(stocks, stock)
			}
		}
	}
	return stocks
}

func calculateMarketEmotionRaw(
	current limitUpLadderDay,
	previous *limitUpLadderDay,
	pools marketEmotionSidePools,
	lines map[string]foundation.KLine,
) marketemotion.RawMetrics {
	currentStocks := visibleLimitUpStocks(current)
	previousStocks := []limitUpLadderStock{}
	if previous != nil {
		previousStocks = visibleLimitUpStocks(*previous)
	}
	currentBySymbol := make(map[string]limitUpLadderStock, len(currentStocks))
	for _, stock := range currentStocks {
		currentBySymbol[stock.Symbol] = stock
	}
	broken := filterMarketLimitEvents(pools.broken)
	down := filterMarketLimitEvents(pools.down)
	downSymbols := make(map[string]struct{}, len(down))
	for _, item := range down {
		downSymbols[item.Symbol] = struct{}{}
	}

	raw := marketemotion.RawMetrics{
		LimitUpCount:    len(currentStocks),
		LimitDownCount:  len(down),
		BrokenCount:     len(broken),
		FirstBoardCount: current.FirstBoardCount,
		BoardCount:      current.BoardCount,
		MaxStreak:       current.MaxStreak,
		ReopenedCount:   current.ReopenedCount,
	}
	raw.FinalBreakRate = divide(float64(raw.BrokenCount), float64(raw.LimitUpCount+raw.BrokenCount))
	raw.ReopenSuccessRate = divide(float64(raw.ReopenedCount), float64(raw.ReopenedCount+raw.BrokenCount))

	returns := []float64{}
	boardReturns := []float64{}
	openPremiums := []float64{}
	covered := 0
	for _, stock := range previousStocks {
		line, ok := lines[stock.Symbol]
		if !ok {
			continue
		}
		covered++
		returns = append(returns, line.ChangePercent)
		if stock.Streak >= 2 {
			boardReturns = append(boardReturns, line.ChangePercent)
		}
		if previousClose := kLinePreviousClose(line); previousClose > 0 && line.Open > 0 {
			openPremiums = append(openPremiums, (line.Open/previousClose-1)*100)
		}
	}
	raw.QuoteCoverage = divide(float64(covered), float64(len(previousStocks)))
	raw.PreviousLimitUpRet = average(returns)
	raw.PreviousBoardRet = average(boardReturns)
	raw.OpenPremium = average(openPremiums)
	raw.CoreReturn = previousCoreReturn(previousStocks, lines)

	eligible := 0
	success := 0
	for _, stock := range previousStocks {
		if stock.Streak < 2 {
			continue
		}
		eligible++
		if next, ok := currentBySymbol[stock.Symbol]; ok && next.Streak >= stock.Streak+1 {
			success++
		}
	}
	raw.AdvanceRate = divide(float64(success), float64(eligible))
	raw.ThemeFocus = weightedThemeFocus(currentStocks)
	raw.LeaderGap = leaderGap(currentStocks)
	raw.LadderContinuity = ladderContinuity(currentStocks)
	if previous != nil {
		raw.HeightCollapse = max(previous.MaxStreak-current.MaxStreak, 0)
	}

	highLevels, midLevels, lowLevels := feedbackLevelSets(previousStocks)
	highFeedback := calculateFeedbackStats(previousStocks, highLevels, lines, downSymbols, currentBySymbol)
	midFeedback := calculateFeedbackStats(previousStocks, midLevels, lines, downSymbols, currentBySymbol)
	lowFeedback := calculateFeedbackStats(previousStocks, lowLevels, lines, downSymbols, currentBySymbol)
	raw.HighSampleCount = highFeedback.sample
	raw.HighWeakCount = highFeedback.weak
	raw.HighKill = highFeedback.severe
	raw.HighLimitDown = highFeedback.limitDown
	raw.HighAverageReturn = highFeedback.averageReturn
	raw.HighDownRate = divide(float64(highFeedback.down), float64(highFeedback.quoted))
	raw.HighAdvanceRate = divide(float64(highFeedback.advance), float64(highFeedback.sample))
	raw.MidKill = midFeedback.severe
	raw.LowKill = lowFeedback.severe
	raw.HighRiskScore = calculateHighRiskScore(raw)
	for _, stock := range currentStocks {
		switch stock.LimitRegime {
		case "20cm":
			raw.LimitUp20CM++
			raw.MaxStreak20CM = max(raw.MaxStreak20CM, stock.Streak)
		case "30cm":
			raw.LimitUp30CM++
			raw.MaxStreak30CM = max(raw.MaxStreak30CM, stock.Streak)
		default:
			raw.LimitUp10CM++
			raw.MaxStreak10CM = max(raw.MaxStreak10CM, stock.Streak)
		}
	}
	return raw
}

func filterMarketLimitEvents(items []foundation.MarketLimitEvent) []foundation.MarketLimitEvent {
	filtered := make([]foundation.MarketLimitEvent, 0, len(items))
	for _, item := range items {
		if item.Symbol != "" && !isSTStockName(item.Name) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func kLinePreviousClose(line foundation.KLine) float64 {
	denominator := 1 + line.ChangePercent/100
	if line.Close <= 0 || denominator <= 0.01 {
		return 0
	}
	return line.Close / denominator
}

func previousCoreReturn(stocks []limitUpLadderStock, lines map[string]foundation.KLine) float64 {
	ordered := append([]limitUpLadderStock(nil), stocks...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Streak != ordered[j].Streak {
			return ordered[i].Streak > ordered[j].Streak
		}
		if ordered[i].Amount != ordered[j].Amount {
			return ordered[i].Amount > ordered[j].Amount
		}
		return ordered[i].FirstLimitTime < ordered[j].FirstLimitTime
	})
	seenRegime := map[string]bool{}
	values := []float64{}
	for _, stock := range ordered {
		regime := stock.LimitRegime
		if regime == "" {
			regime = "10cm"
		}
		if seenRegime[regime] {
			continue
		}
		line, ok := lines[stock.Symbol]
		if !ok {
			continue
		}
		seenRegime[regime] = true
		values = append(values, line.ChangePercent)
		if len(values) >= 3 {
			break
		}
	}
	return average(values)
}

func weightedThemeFocus(stocks []limitUpLadderStock) float64 {
	weights := map[string]float64{}
	total := 0.0
	for _, stock := range stocks {
		theme := strings.TrimSpace(stock.PrimaryTheme)
		if theme == "" {
			continue
		}
		confidence := stock.ThemeConfidence
		if confidence <= 0 {
			confidence = 0.5
		}
		weight := math.Pow(float64(max(stock.Streak, 1)), 2) * (0.7 + 0.3*confidence)
		weights[theme] += weight
		total += weight
	}
	maximum := 0.0
	for _, weight := range weights {
		maximum = math.Max(maximum, weight)
	}
	return divide(maximum, total)
}

func leaderGap(stocks []limitUpLadderStock) float64 {
	heights := make([]int, 0, len(stocks))
	for _, stock := range stocks {
		heights = append(heights, stock.Streak)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(heights)))
	if len(heights) == 0 {
		return 0
	}
	if len(heights) == 1 {
		return float64(heights[0])
	}
	return float64(heights[0] - heights[1])
}

func ladderContinuity(stocks []limitUpLadderStock) float64 {
	occupied := map[int]struct{}{}
	maximum := 0
	for _, stock := range stocks {
		if stock.Streak < 2 {
			continue
		}
		occupied[stock.Streak] = struct{}{}
		maximum = max(maximum, stock.Streak)
	}
	if maximum < 2 {
		return 0
	}
	return divide(float64(len(occupied)), float64(maximum-1))
}

func feedbackLevelSets(stocks []limitUpLadderStock) ([]int, []int, []int) {
	occupied := map[int]struct{}{}
	for _, stock := range stocks {
		if stock.Streak >= 2 {
			occupied[stock.Streak] = struct{}{}
		}
	}
	levels := make([]int, 0, len(occupied))
	for level := range occupied {
		levels = append(levels, level)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(levels)))
	highCount := min(3, len(levels))
	high := append([]int(nil), levels[:highCount]...)
	mid := append([]int(nil), levels[highCount:]...)
	return high, mid, []int{1}
}

type marketEmotionFeedbackStats struct {
	sample        int
	quoted        int
	down          int
	weak          int
	severe        int
	limitDown     int
	advance       int
	averageReturn float64
}

func calculateFeedbackStats(
	stocks []limitUpLadderStock,
	levels []int,
	lines map[string]foundation.KLine,
	downSymbols map[string]struct{},
	currentBySymbol map[string]limitUpLadderStock,
) marketEmotionFeedbackStats {
	levelSet := make(map[int]struct{}, len(levels))
	for _, level := range levels {
		levelSet[level] = struct{}{}
	}
	stats := marketEmotionFeedbackStats{}
	returns := []float64{}
	for _, stock := range stocks {
		if _, ok := levelSet[stock.Streak]; !ok {
			continue
		}
		stats.sample++
		if current, ok := currentBySymbol[stock.Symbol]; ok && current.Streak >= stock.Streak+1 {
			stats.advance++
		}
		_, limitDown := downSymbols[stock.Symbol]
		if limitDown {
			stats.limitDown++
		}
		line, hasLine := lines[stock.Symbol]
		if !hasLine {
			if limitDown {
				stats.severe++
			}
			continue
		}
		stats.quoted++
		returns = append(returns, line.ChangePercent)
		if line.ChangePercent < 0 {
			stats.down++
		}
		if line.ChangePercent <= -3 {
			stats.weak++
		}
		if limitDown || line.ChangePercent <= -6 {
			stats.severe++
		}
	}
	stats.averageReturn = average(returns)
	return stats
}

func calculateHighRiskScore(raw marketemotion.RawMetrics) float64 {
	if raw.HighSampleCount <= 0 {
		return 0
	}
	weakRate := divide(float64(raw.HighWeakCount), float64(raw.HighSampleCount)) * 100
	severeRate := divide(float64(raw.HighKill), float64(raw.HighSampleCount)) * 100
	limitDownRate := divide(float64(raw.HighLimitDown), float64(raw.HighSampleCount)) * 100
	averageLoss := scale(-raw.HighAverageReturn, 0, 8)
	heightRisk := scale(float64(raw.HeightCollapse), 0, 4)
	advanceFailure := (1 - clamp(raw.HighAdvanceRate, 0, 1)) * 100
	risk := 0.15*raw.HighDownRate*100 +
		0.15*weakRate +
		0.20*severeRate +
		0.10*limitDownRate +
		0.15*averageLoss +
		0.15*heightRisk +
		0.10*advanceFailure
	return round2(clamp(risk, 0, 100))
}

func scoreMarketEmotion(
	date string,
	raw marketemotion.RawMetrics,
	history []marketemotion.Snapshot,
	now time.Time,
) marketemotion.Snapshot {
	heat := 0.25*blendedFactor(float64(raw.LimitUpCount), historyValues(history, func(item marketemotion.RawMetrics) float64 { return float64(item.LimitUpCount) }), scale(float64(raw.LimitUpCount), 10, 100), false) +
		0.20*blendedFactor(float64(raw.MaxStreak), historyValues(history, func(item marketemotion.RawMetrics) float64 { return float64(item.MaxStreak) }), scale(float64(raw.MaxStreak), 1, 8), false) +
		0.15*blendedFactor(float64(raw.BoardCount), historyValues(history, func(item marketemotion.RawMetrics) float64 { return float64(item.BoardCount) }), scale(float64(raw.BoardCount), 2, 35), false) +
		0.20*blendedFactor(float64(raw.LimitDownCount), historyValues(history, func(item marketemotion.RawMetrics) float64 { return float64(item.LimitDownCount) }), inverseScale(float64(raw.LimitDownCount), 0, 30), true) +
		0.20*blendedFactor(raw.FinalBreakRate, historyValues(history, func(item marketemotion.RawMetrics) float64 { return item.FinalBreakRate }), inverseScale(raw.FinalBreakRate, 0.08, 0.5), true)

	profit := 0.35*blendedFactor(raw.PreviousLimitUpRet, historyValues(history, func(item marketemotion.RawMetrics) float64 { return item.PreviousLimitUpRet }), centeredScore(raw.PreviousLimitUpRet, 10), false) +
		0.25*blendedFactor(raw.PreviousBoardRet, historyValues(history, func(item marketemotion.RawMetrics) float64 { return item.PreviousBoardRet }), centeredScore(raw.PreviousBoardRet, 8), false) +
		0.15*blendedFactor(raw.OpenPremium, historyValues(history, func(item marketemotion.RawMetrics) float64 { return item.OpenPremium }), centeredScore(raw.OpenPremium, 12), false) +
		0.25*blendedFactor(raw.CoreReturn, historyValues(history, func(item marketemotion.RawMetrics) float64 { return item.CoreReturn }), centeredScore(raw.CoreReturn, 8), false)

	structure := 0.30*blendedFactor(raw.AdvanceRate, historyValues(history, func(item marketemotion.RawMetrics) float64 { return item.AdvanceRate }), scale(raw.AdvanceRate, 0.05, 0.55), false) +
		0.20*blendedFactor(raw.ThemeFocus, historyValues(history, func(item marketemotion.RawMetrics) float64 { return item.ThemeFocus }), scale(raw.ThemeFocus, 0.15, 0.7), false) +
		0.20*blendedFactor(raw.LadderContinuity, historyValues(history, func(item marketemotion.RawMetrics) float64 { return item.LadderContinuity }), scale(raw.LadderContinuity, 0.2, 1), false) +
		0.20*blendedFactor(raw.HighRiskScore, historyValues(history, func(item marketemotion.RawMetrics) float64 { return item.HighRiskScore }), inverseScale(raw.HighRiskScore, 15, 80), true) +
		0.10*blendedFactor(float64(raw.MidKill), historyValues(history, func(item marketemotion.RawMetrics) float64 { return float64(item.MidKill) }), inverseScale(float64(raw.MidKill), 0, 5), true)

	scores := marketemotion.Scores{
		Heat:      round2(heat),
		Profit:    round2(profit),
		Structure: round2(structure),
	}
	scores.Total = round2(0.35*scores.Heat + 0.40*scores.Profit + 0.25*scores.Structure)
	return marketemotion.Snapshot{
		ModelVersion:   currentMarketEmotionModelVersion,
		TradeDate:      date,
		EmotionScore:   scores.Total,
		Phase:          classifyMarketEmotion(scores, raw, history),
		Confidence:     emotionConfidence(len(history), raw.QuoteCoverage),
		HistorySamples: len(history),
		Raw:            raw,
		Scores:         scores,
		Source:         "东方财富涨停/炸板/跌停池 + 东方财富日K + 动态概念归因",
		UpdatedAt:      now,
	}
}

func historyValues(history []marketemotion.Snapshot, getter func(marketemotion.RawMetrics) float64) []float64 {
	start := max(len(history)-120, 0)
	values := make([]float64, 0, len(history)-start)
	for _, item := range history[start:] {
		values = append(values, getter(item.Raw))
	}
	return values
}

func blendedFactor(current float64, history []float64, absolute float64, reverse bool) float64 {
	if len(history) == 0 {
		return clamp(absolute, 0, 100)
	}
	percentile := percentileRank(current, history)
	if reverse {
		percentile = 100 - percentile
	}
	historyWeight := math.Min(float64(len(history))/120, 1)
	return clamp(absolute*(1-historyWeight)+percentile*historyWeight, 0, 100)
}

func percentileRank(current float64, history []float64) float64 {
	less := 0.0
	equal := 0.0
	for _, value := range history {
		if value < current {
			less++
		} else if math.Abs(value-current) < 1e-9 {
			equal++
		}
	}
	return 100 * (less + 0.5*equal) / float64(len(history))
}

func classifyMarketEmotion(scores marketemotion.Scores, raw marketemotion.RawMetrics, history []marketemotion.Snapshot) string {
	if scores.Total < 25 && scores.Profit < 25 {
		return "冰点"
	}
	if severeHighLevelRetreat(raw) {
		return "退潮"
	}
	retreatHits := 0
	if raw.HighRiskScore >= 65 {
		retreatHits++
	}
	if raw.MidKill >= 2 {
		retreatHits++
	}
	if raw.PreviousBoardRet < 0 {
		retreatHits++
	}
	if raw.LimitUpCount >= 40 && raw.PreviousLimitUpRet < 0 {
		retreatHits++
	}
	if len(history) >= 2 {
		previous := history[len(history)-1]
		previous2 := history[len(history)-2]
		if scores.Profit < previous.Scores.Profit && previous.Scores.Profit < previous2.Scores.Profit {
			retreatHits++
		}
		if raw.FinalBreakRate > previous.Raw.FinalBreakRate && previous.Raw.FinalBreakRate > previous2.Raw.FinalBreakRate {
			retreatHits++
		}
	}
	if retreatHits >= 3 {
		return "退潮"
	}
	highRiskVeto := highLevelRiskVeto(raw)
	improving := false
	if len(history) > 0 {
		previous := history[len(history)-1]
		improving = scores.Total-previous.Scores.Total >= 3 || scores.Profit-previous.Scores.Profit >= 3
	}
	if scores.Total < 45 && improving {
		return "启动/修复"
	}
	if scores.Total >= 70 && scores.Heat >= 75 && !improving && !highRiskVeto {
		return "高潮"
	}
	if highRiskVeto && scores.Heat >= 55 {
		return "强分歧"
	}
	if scores.Total >= 45 && scores.Profit > 55 && scores.Structure > 50 {
		return "发酵/主升"
	}
	return "混沌/过渡"
}

func highLevelRiskVeto(raw marketemotion.RawMetrics) bool {
	if raw.HighSampleCount <= 0 {
		return false
	}
	weakRate := divide(float64(raw.HighWeakCount), float64(raw.HighSampleCount))
	return raw.HighRiskScore >= 55 ||
		(raw.HighDownRate >= 0.75 && raw.HighAverageReturn <= -3) ||
		(raw.HeightCollapse >= 3 && raw.HighAdvanceRate < 0.2) ||
		(raw.HighLimitDown > 0 && weakRate >= 0.75)
}

func severeHighLevelRetreat(raw marketemotion.RawMetrics) bool {
	if raw.HighSampleCount <= 0 {
		return false
	}
	weakRate := divide(float64(raw.HighWeakCount), float64(raw.HighSampleCount))
	severeRate := divide(float64(raw.HighKill), float64(raw.HighSampleCount))
	return (raw.HighDownRate >= 0.75 && raw.HighAverageReturn <= -4 && (severeRate >= 0.5 || raw.HeightCollapse >= 3)) ||
		(raw.HeightCollapse >= 3 && raw.HighAdvanceRate == 0 && raw.HighAverageReturn <= -4) ||
		(raw.HighLimitDown > 0 && weakRate >= 0.75)
}

func buildMarketEmotionIntraday(data limitUpLadderData, latest *marketemotion.Snapshot) marketemotion.IntradaySnapshot {
	previousStocks := visibleLimitUpStocks(data.Previous)
	highLevels, _, _ := feedbackLevelSets(previousStocks)
	highLevelSet := make(map[int]struct{}, len(highLevels))
	for _, level := range highLevels {
		highLevelSet[level] = struct{}{}
	}

	metrics := marketemotion.IntradayMetrics{
		PreviousMaxStreak: data.Previous.MaxStreak,
		CurrentMaxStreak:  data.Current.MaxStreak,
		HeightCollapse:    max(data.Previous.MaxStreak-data.Current.MaxStreak, 0),
		HighLevels:        append([]int(nil), highLevels...),
		LimitUpCount:      data.Current.LimitUpCount,
		BoardCount:        data.Current.BoardCount,
		FirstBoardCount:   data.Current.FirstBoardCount,
	}
	returns := []float64{}
	for _, stock := range previousStocks {
		if _, ok := highLevelSet[stock.Streak]; !ok {
			continue
		}
		metrics.HighSampleCount++
		if stock.CurrentChangePercent == nil {
			continue
		}
		change := *stock.CurrentChangePercent
		metrics.HighQuoteCount++
		returns = append(returns, change)
		if change < 0 {
			metrics.HighDownCount++
		}
		if change <= -3 {
			metrics.HighWeakCount++
		}
		if change <= -6 {
			metrics.HighSevereCount++
		}
		if nearLimitDown(change, stock.LimitRegime) {
			metrics.HighLimitDown++
		}
	}
	metrics.HighAverageReturn = average(returns)
	metrics.HighDownRate = divide(float64(metrics.HighDownCount), float64(metrics.HighQuoteCount))
	metrics.HighWeakRate = divide(float64(metrics.HighWeakCount), float64(metrics.HighQuoteCount))
	metrics.HighSevereRate = divide(float64(metrics.HighSevereCount), float64(metrics.HighQuoteCount))
	for _, step := range data.Advance {
		if _, ok := highLevelSet[step.FromLevel]; !ok {
			continue
		}
		metrics.HighAdvanceBase += step.Base
		metrics.HighAdvanceCount += step.Success
	}
	metrics.HighAdvanceRate = divide(float64(metrics.HighAdvanceCount), float64(metrics.HighAdvanceBase))
	riskScore := calculateIntradayHighRisk(metrics)
	status := classifyIntradayHighLevel(metrics, riskScore)
	breadth := classifyIntradayBreadth(metrics)
	confidence := "盘中试算"
	if metrics.HighSampleCount == 0 {
		confidence = "缺少昨日高位样本"
	} else if divide(float64(metrics.HighQuoteCount), float64(metrics.HighSampleCount)) < 0.75 {
		confidence = "高位行情覆盖不足"
	}
	baseTradeDate := ""
	if latest != nil {
		baseTradeDate = latest.TradeDate
	}
	return marketemotion.IntradaySnapshot{
		TradeDate:     data.Current.TradeDate,
		BaseTradeDate: baseTradeDate,
		SessionStatus: data.SessionStatus,
		Status:        status,
		Breadth:       breadth,
		Summary:       status + " · " + breadth,
		RiskScore:     riskScore,
		Confidence:    confidence,
		Metrics:       metrics,
	}
}

func nearLimitDown(change float64, regime string) bool {
	threshold := -9.5
	switch regime {
	case "20cm":
		threshold = -19
	case "30cm":
		threshold = -28.5
	}
	return change <= threshold
}

func calculateIntradayHighRisk(metrics marketemotion.IntradayMetrics) float64 {
	if metrics.HighSampleCount <= 0 {
		return 0
	}
	averageLoss := scale(-metrics.HighAverageReturn, 0, 8)
	heightRisk := scale(float64(metrics.HeightCollapse), 0, 4)
	advanceFailure := 0.0
	if metrics.HighAdvanceBase > 0 {
		advanceFailure = (1 - clamp(metrics.HighAdvanceRate, 0, 1)) * 100
	}
	limitDownRate := divide(float64(metrics.HighLimitDown), float64(metrics.HighQuoteCount)) * 100
	risk := 0.15*metrics.HighDownRate*100 +
		0.15*metrics.HighWeakRate*100 +
		0.20*metrics.HighSevereRate*100 +
		0.10*limitDownRate +
		0.15*averageLoss +
		0.15*heightRisk +
		0.10*advanceFailure
	return round2(clamp(risk, 0, 100))
}

func classifyIntradayHighLevel(metrics marketemotion.IntradayMetrics, riskScore float64) string {
	if metrics.HighSampleCount == 0 {
		return "高位样本不足"
	}
	quoteCoverage := divide(float64(metrics.HighQuoteCount), float64(metrics.HighSampleCount))
	hasReliableQuotes := quoteCoverage >= 0.6
	allHighAdvanceFailed := metrics.HighAdvanceBase > 0 && metrics.HighAdvanceCount == 0
	if hasReliableQuotes && ((metrics.HighDownRate >= 0.75 && metrics.HighAverageReturn <= -4 && (metrics.HighSevereRate >= 0.5 || metrics.HeightCollapse >= 3)) ||
		(metrics.HeightCollapse >= 3 && allHighAdvanceFailed && metrics.HighAverageReturn <= -3) ||
		(metrics.HighLimitDown > 0 && metrics.HighWeakRate >= 0.75)) {
		return "高位退潮"
	}
	if riskScore >= 60 || (hasReliableQuotes && metrics.HighDownRate >= 0.75 && metrics.HighAverageReturn <= -2) {
		return "强分歧"
	}
	if riskScore >= 40 || (hasReliableQuotes && metrics.HighAverageReturn < 0) {
		return "分歧"
	}
	if metrics.HighAdvanceRate >= 0.35 && metrics.HighAverageReturn >= 2 {
		return "高位延续"
	}
	return "高位震荡"
}

func classifyIntradayBreadth(metrics marketemotion.IntradayMetrics) string {
	switch {
	case metrics.FirstBoardCount >= 30 || metrics.LimitUpCount >= 60:
		return "低位活跃"
	case metrics.FirstBoardCount >= 15 || metrics.LimitUpCount >= 30:
		return "低位一般"
	default:
		return "低位低迷"
	}
}

func emotionConfidence(historySamples int, quoteCoverage float64) string {
	label := "试运行"
	switch {
	case historySamples >= 120:
		label = "正式"
	case historySamples >= 60:
		label = "中等"
	case historySamples >= 20:
		label = "较低"
	}
	if quoteCoverage > 0 && quoteCoverage < 0.75 {
		return label + "·行情覆盖不足"
	}
	return label
}

func buildMarketEmotionHistory(points []marketemotion.Snapshot, state marketemotion.SyncState) marketemotion.History {
	history := marketemotion.History{
		Points: points,
		Cache: marketemotion.CacheStatus{
			Mode:             "local-sqlite",
			CachedDays:       len(points),
			BootstrapDays:    marketEmotionBootstrapDays,
			LastExternalSync: state.LastSuccessDate,
			LastError:        state.LastError,
			UpdatedAt:        state.UpdatedAt,
		},
	}
	if len(points) > 0 {
		latest := points[len(points)-1]
		history.Latest = &latest
	}
	return history
}

func (e *marketEmotionEngine) runScheduler(ctx context.Context) {
	run := func() {
		checkCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()
		_, _ = e.load(checkCtx)
	}
	run()
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			run()
		case <-ctx.Done():
			return
		}
	}
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func divide(numerator float64, denominator float64) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / denominator
}

func scale(value float64, low float64, high float64) float64 {
	if high <= low {
		return 50
	}
	return clamp((value-low)/(high-low)*100, 0, 100)
}

func inverseScale(value float64, low float64, high float64) float64 {
	return 100 - scale(value, low, high)
}

func centeredScore(value float64, multiplier float64) float64 {
	return clamp(50+value*multiplier, 0, 100)
}

func clamp(value float64, minimum float64, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}
