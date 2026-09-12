package sector

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

const realtimeStrengthQuoteBatchSize = 120

// radarThemeConstituentProvider lets the production Mapper load all mapped
// theme pools from one EastMoney catalog snapshot. Other fallback providers
// can keep implementing RadarFallback only and use the generic Build path.
type radarThemeConstituentProvider interface {
	radarThemeConstituents(ctx context.Context, themeIDs []string) (map[string][]foundation.BoardStock, error)
}

type stockStrengthChange struct {
	daily        float64
	fiveDay      float64
	dailyValid   bool
	fiveDayValid bool
}

func (p *RadarProvider) realtimeStrengthScores(ctx context.Context, themes []duanxianxia.Theme) map[string]themeStrengthScore {
	p.strengthMu.Lock()
	defer p.strengthMu.Unlock()

	now := p.now()
	if !p.strengthAttemptAt.IsZero() && now.Before(p.strengthAttemptAt.Add(p.strengthTTL)) {
		return cloneThemeStrengthScores(p.strengthCache)
	}

	// Record attempts as well as successful refreshes so a failing upstream
	// cannot trigger a recalculation on every page request.
	p.strengthAttemptAt = now
	scores, err := p.calculateRealtimeStrengthScores(ctx, themes)
	if err == nil {
		p.strengthCache = cloneThemeStrengthScores(scores)
	}
	return cloneThemeStrengthScores(p.strengthCache)
}

func (p *RadarProvider) calculateRealtimeStrengthScores(ctx context.Context, themes []duanxianxia.Theme) (map[string]themeStrengthScore, error) {
	pools, poolErr := p.loadRealtimeStrengthPools(ctx, themes)
	if poolErr != nil && !themePoolsHaveStocks(pools) {
		return nil, poolErr
	}

	symbols := uniqueSortedThemeSymbols(pools)
	changes := p.strengthChangeLookup(ctx, symbols, pools)

	scores := make(map[string]themeStrengthScore, len(themes))
	for _, theme := range themes {
		stocks := pools[theme.Code]
		scores[theme.Code] = themeStrengthScore{
			daily: calculateThemeStrength(stocks, changes, func(change stockStrengthChange) (float64, bool) {
				return change.daily, change.dailyValid
			}),
			fiveDay: calculateThemeStrength(stocks, changes, func(change stockStrengthChange) (float64, bool) {
				return change.fiveDay, change.fiveDayValid
			}),
		}
	}
	return scores, nil
}

func (p *RadarProvider) strengthChangeLookup(
	ctx context.Context,
	symbols []string,
	pools map[string][]foundation.BoardStock,
) map[string]stockStrengthChange {
	result := make(map[string]stockStrengthChange, len(symbols))
	for symbol, quote := range p.realtimeStrengthQuoteLookup(ctx, symbols) {
		if quote.Price <= 0 && quote.PreviousClose <= 0 && quote.ChangePercent == 0 {
			continue
		}
		result[symbol] = stockStrengthChange{daily: quote.ChangePercent, dailyValid: true}
	}

	// The mapped EastMoney constituent snapshot supplies the rolling five-day
	// return and is the daily fallback for any missing realtime quote.
	for _, stocks := range pools {
		for _, stock := range stocks {
			change := result[stock.Symbol]
			if !change.dailyValid && (stock.Price > 0 || stock.ChangePercent != 0) {
				change.daily = stock.ChangePercent
				change.dailyValid = true
			}
			if stock.Price > 0 || stock.FiveDayChangePercent != 0 {
				change.fiveDay = stock.FiveDayChangePercent
				change.fiveDayValid = true
			}
			result[stock.Symbol] = change
		}
	}
	return result
}

func (p *RadarProvider) loadRealtimeStrengthPools(ctx context.Context, themes []duanxianxia.Theme) (map[string][]foundation.BoardStock, error) {
	result := make(map[string][]foundation.BoardStock, len(themes))
	mappedByCode := make(map[string]string, len(themes))
	uniqueThemeIDs := make([]string, 0, len(themes))
	seenThemeIDs := map[string]struct{}{}
	for _, theme := range themes {
		themeID, _ := mappedFallbackThemeID(theme.Code, theme.Name)
		mappedByCode[theme.Code] = themeID
		if _, exists := seenThemeIDs[themeID]; !exists {
			seenThemeIDs[themeID] = struct{}{}
			uniqueThemeIDs = append(uniqueThemeIDs, themeID)
		}
	}

	poolsByThemeID := map[string][]foundation.BoardStock{}
	var loadErr error
	if provider, ok := p.fallback.(radarThemeConstituentProvider); ok {
		poolsByThemeID, loadErr = provider.radarThemeConstituents(ctx, uniqueThemeIDs)
	} else if p.fallback != nil {
		for _, themeID := range uniqueThemeIDs {
			sectorMap, err := p.fallback.Build(ctx, themeID)
			if err != nil {
				loadErr = err
				continue
			}
			poolsByThemeID[themeID] = stocksFromSectorMap(sectorMap)
		}
	} else {
		loadErr = fmt.Errorf("radar fallback provider is unavailable")
	}

	for _, theme := range themes {
		stocks := append([]foundation.BoardStock(nil), poolsByThemeID[mappedByCode[theme.Code]]...)
		for _, leader := range theme.Leaders {
			stocks = mergeBoardStock(stocks, foundation.BoardStock{Symbol: leader.Symbol, Name: leader.Name})
		}
		result[theme.Code] = uniqueBoardStocks(stocks)
	}
	return result, loadErr
}

func (p *RadarProvider) realtimeStrengthQuoteLookup(ctx context.Context, symbols []string) map[string]foundation.Quote {
	result := map[string]foundation.Quote{}
	if p.quotes == nil {
		return result
	}
	for start := 0; start < len(symbols); start += realtimeStrengthQuoteBatchSize {
		end := min(start+realtimeStrengthQuoteBatchSize, len(symbols))
		quotes, err := p.quotes.Realtime(ctx, symbols[start:end])
		if err != nil {
			continue
		}
		for _, quote := range quotes {
			result[quote.Symbol] = quote
		}
	}
	return result
}

// themeConstituentSamples is the sample size at which a theme's average
// return counts fully. Smaller pools are shrunk toward neutral because a
// three-stock board is trivially moved by one limit-up, which used to let
// thin boards outrank broad, genuinely strong ones.
const themeConstituentSamples = 12

// themeStrengthWeights lead with breadth: "how many members joined" is the
// question a short-term reader asks, and it is immune to a single outlier
// member distorting the average.
const (
	themeWeightBreadth = 0.50
	themeWeightReturn  = 0.25
	themeWeightStrong  = 0.15
	themeWeightLimit   = 0.10
)

func calculateThemeStrength(
	stocks []foundation.BoardStock,
	changes map[string]stockStrengthChange,
	selectChange func(stockStrengthChange) (float64, bool),
) int {
	values := make([]float64, 0, len(stocks))
	rising := 0
	strong := 0
	limitLike := 0
	for _, stock := range uniqueBoardStocks(stocks) {
		change, valid := selectChange(changes[stock.Symbol])
		if !valid || math.IsNaN(change) || math.IsInf(change, 0) {
			continue
		}
		// IPO and malformed outliers must not dominate a whole theme. Ratios
		// still retain the direction while the central tendency is robust.
		change = clampFloat(change, -30, 30)
		values = append(values, change)
		if change > 0 {
			rising++
		}
		if change >= 3 {
			strong++
		}
		if change >= nearLimitUpThreshold(stock) {
			limitLike++
		}
	}
	if len(values) == 0 {
		return 0
	}

	// Median instead of a winsorized mean: in a seven-stock board one limit-up
	// at +10% must not lift the whole theme's central reading. The confidence
	// factor then discounts thin pools, which otherwise score as strongly as
	// broad ones on the strength of a single member.
	valid := float64(len(values))
	confidence := constituentConfidence(len(values))
	returnScore := normalizeStrengthRange(medianChange(values), -3, 5) * confidence
	breadthScore := float64(rising) / valid * 100
	strongScore := float64(strong) / valid * 100
	// A lone limit-up in a four-stock board is 25% of the pool, far above the
	// 8% saturation point, so this ratio needs the same sample discount.
	limitRatio := float64(limitLike) / valid
	limitScore := math.Min(limitRatio/0.08, 1) * 100 * confidence
	score := returnScore*themeWeightReturn + breadthScore*themeWeightBreadth +
		strongScore*themeWeightStrong + limitScore*themeWeightLimit
	return int(math.Round(clampFloat(score, 0, 100)))
}

// constituentConfidence discounts a theme's return reading by how few members
// it is based on. 7 members keep ~37%, 30 ~71%, 100 ~89%: statistically the
// same average move means less when it rests on a handful of stocks.
func constituentConfidence(size int) float64 {
	if size <= 0 {
		return 0
	}
	n := float64(size)
	return n / (n + themeConstituentSamples)
}

// medianChange returns the median of the raw constituent changes. An even
// count averages the two central values.
func medianChange(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	items := make([]float64, len(values))
	copy(items, values)
	sort.Float64s(items)
	middle := len(items) / 2
	if len(items)%2 == 1 {
		return items[middle]
	}
	return (items[middle-1] + items[middle]) / 2
}

func calculateRealtimeThemeStrength(stocks []foundation.BoardStock, quotes map[string]foundation.Quote) int {
	changes := make(map[string]stockStrengthChange, len(stocks))
	for _, stock := range stocks {
		quote, exists := quotes[stock.Symbol]
		if exists && (quote.Price > 0 || quote.PreviousClose > 0 || quote.ChangePercent != 0) {
			changes[stock.Symbol] = stockStrengthChange{daily: quote.ChangePercent, dailyValid: true}
			continue
		}
		if stock.Price > 0 || stock.ChangePercent != 0 {
			changes[stock.Symbol] = stockStrengthChange{daily: stock.ChangePercent, dailyValid: true}
		}
	}
	return calculateThemeStrength(stocks, changes, func(change stockStrengthChange) (float64, bool) {
		return change.daily, change.dailyValid
	})
}

func normalizeStrengthRange(value float64, minimum float64, maximum float64) float64 {
	if maximum <= minimum {
		return 0
	}
	return clampFloat((value-minimum)/(maximum-minimum)*100, 0, 100)
}

func nearLimitUpThreshold(stock foundation.BoardStock) float64 {
	name := strings.ToUpper(strings.TrimSpace(stock.Name))
	if strings.HasPrefix(name, "ST") || strings.HasPrefix(name, "*ST") {
		return 4.5
	}
	code := strings.SplitN(strings.TrimSpace(stock.Symbol), ".", 2)[0]
	if strings.HasPrefix(code, "30") || strings.HasPrefix(code, "68") {
		return 18
	}
	if strings.HasPrefix(code, "4") || strings.HasPrefix(code, "8") || strings.HasPrefix(code, "92") {
		return 27
	}
	return 9
}

func uniqueSortedThemeSymbols(pools map[string][]foundation.BoardStock) []string {
	symbols := make([]string, 0, 512)
	seen := map[string]struct{}{}
	for _, stocks := range pools {
		for _, stock := range stocks {
			symbol := strings.TrimSpace(stock.Symbol)
			if symbol == "" {
				continue
			}
			if _, exists := seen[symbol]; exists {
				continue
			}
			seen[symbol] = struct{}{}
			symbols = append(symbols, symbol)
		}
	}
	sort.Strings(symbols)
	return symbols
}

func themePoolsHaveStocks(pools map[string][]foundation.BoardStock) bool {
	for _, stocks := range pools {
		if len(stocks) > 0 {
			return true
		}
	}
	return false
}

func stocksFromSectorMap(sectorMap foundation.SectorMap) []foundation.BoardStock {
	stocks := []foundation.BoardStock{}
	for _, group := range sectorMap.Groups {
		for _, node := range group.Nodes {
			stocks = append(stocks, node.Stocks...)
		}
	}
	return uniqueBoardStocks(stocks)
}

func uniqueBoardStocks(stocks []foundation.BoardStock) []foundation.BoardStock {
	result := make([]foundation.BoardStock, 0, len(stocks))
	indexBySymbol := make(map[string]int, len(stocks))
	for _, stock := range stocks {
		stock.Symbol = strings.TrimSpace(stock.Symbol)
		if stock.Symbol == "" {
			continue
		}
		if index, exists := indexBySymbol[stock.Symbol]; exists {
			merged := mergeBoardStock([]foundation.BoardStock{result[index]}, stock)
			result[index] = merged[0]
			continue
		}
		indexBySymbol[stock.Symbol] = len(result)
		result = append(result, stock)
	}
	return result
}

func cloneThemeStrengthScores(scores map[string]themeStrengthScore) map[string]themeStrengthScore {
	result := make(map[string]themeStrengthScore, len(scores))
	for key, value := range scores {
		result[key] = value
	}
	return result
}

func clampFloat(value float64, minimum float64, maximum float64) float64 {
	return math.Min(maximum, math.Max(minimum, value))
}
