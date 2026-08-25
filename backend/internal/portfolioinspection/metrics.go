package portfolioinspection

import (
	"math"
	"sort"
	"strings"

	"easy-stock/backend/internal/stockanalysis"
)

func CalculateMetrics(request Request, results []HoldingResult, rules ProfileRules) Metrics {
	metrics := Metrics{ThemeExposures: []ThemeExposure{}, HighCorrelations: []CorrelationPair{}, RiskContributions: []RiskContribution{}, StyleBreaches: []string{}}
	weights := make([]int, 0, len(request.Holdings))
	for _, holding := range request.Holdings {
		metrics.TotalPositionPercent += holding.Weight
		weights = append(weights, holding.Weight)
		if holding.Weight > metrics.MaxSinglePercent {
			metrics.MaxSinglePercent = holding.Weight
		}
	}
	metrics.CashPercent = 100 - metrics.TotalPositionPercent
	sort.Sort(sort.Reverse(sort.IntSlice(weights)))
	for index := 0; index < len(weights) && index < 3; index++ {
		metrics.TopThreePercent += weights[index]
	}
	if metrics.TotalPositionPercent > 0 {
		for _, weight := range weights {
			share := float64(weight) / float64(metrics.TotalPositionPercent)
			metrics.HHI += share * share * 10_000
		}
	}

	analyses := make(map[string]HoldingResult, len(results))
	successWeight := 0
	themeWeights := map[string]int{}
	themeCounts := map[string]int{}
	for _, result := range results {
		if result.Status != "succeeded" || result.Analysis == nil {
			continue
		}
		analyses[result.Holding.Symbol] = result
		weight := result.Holding.Weight
		successWeight += weight
		metrics.WeightedScore += float64(weight * result.Analysis.Scorecard.Overall)
		metrics.WeightedRisk += float64(weight * result.Analysis.RiskControl.Score)
		if result.Analysis.ActionPlan.DecisionMode == "short_term" {
			metrics.ShortTermPercent += weight
		}
		if result.Analysis.Profile.PrimaryType == "new_listing" {
			metrics.NewListingPercent += weight
		}
		if result.Analysis.RiskControl.Score >= 70 {
			metrics.HighRiskPercent += weight
		}
		price := result.Analysis.Quote.Price
		if price <= 0 {
			price = result.Analysis.Trend.LatestClose
		}
		stop := result.Analysis.RiskControl.StopPrice
		if price > 0 && stop > 0 && stop < price {
			metrics.StopLossRiskPercent += float64(weight) * (price - stop) / price
		}
		theme := strings.TrimSpace(result.Analysis.Theme.Primary)
		if theme == "" {
			theme = strings.TrimSpace(result.Analysis.Theme.BusinessTheme)
		}
		if theme == "" {
			theme = "未明确题材"
		}
		themeWeights[theme] += weight
		themeCounts[theme]++
	}
	if metrics.TotalPositionPercent > 0 {
		metrics.CoveragePercent = float64(successWeight) / float64(metrics.TotalPositionPercent) * 100
	}
	if successWeight > 0 {
		metrics.WeightedScore /= float64(successWeight)
		metrics.WeightedRisk /= float64(successWeight)
	}
	for theme, weight := range themeWeights {
		metrics.ThemeExposures = append(metrics.ThemeExposures, ThemeExposure{Theme: theme, Weight: weight, Symbols: themeCounts[theme]})
	}
	sort.Slice(metrics.ThemeExposures, func(i, j int) bool { return metrics.ThemeExposures[i].Weight > metrics.ThemeExposures[j].Weight })

	metrics.HighCorrelations = calculateCorrelations(analyses)
	metrics.RiskContributions = calculateRiskContributions(results, metrics.HighCorrelations)
	metrics.StyleMatchScore, metrics.StyleBreaches = styleMatch(metrics, rules)
	metrics.HHI = round(metrics.HHI, 1)
	metrics.CoveragePercent = round(metrics.CoveragePercent, 1)
	metrics.WeightedScore = round(metrics.WeightedScore, 1)
	metrics.WeightedRisk = round(metrics.WeightedRisk, 1)
	metrics.StopLossRiskPercent = round(metrics.StopLossRiskPercent, 2)
	return metrics
}

func calculateCorrelations(analyses map[string]HoldingResult) []CorrelationPair {
	symbols := make([]string, 0, len(analyses))
	for symbol := range analyses {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	pairs := make([]CorrelationPair, 0)
	for left := 0; left < len(symbols); left++ {
		for right := left + 1; right < len(symbols); right++ {
			correlation, ok := chartCorrelation(analyses[symbols[left]].Analysis, analyses[symbols[right]].Analysis)
			if ok && correlation >= .7 {
				pairs = append(pairs, CorrelationPair{LeftSymbol: symbols[left], RightSymbol: symbols[right], Correlation: round(correlation, 2)})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Correlation > pairs[j].Correlation })
	if len(pairs) > 8 {
		pairs = pairs[:8]
	}
	return pairs
}

func chartCorrelation(left, right *stockanalysis.Analysis) (float64, bool) {
	if left == nil || right == nil {
		return 0, false
	}
	return chartReturnsCorrelation(left.Chart, right.Chart)
}

func chartReturnsCorrelation(left, right []stockanalysis.TrendPoint) (float64, bool) {
	leftReturns := returnsByDate(left)
	rightReturns := returnsByDate(right)
	x := make([]float64, 0, min(len(leftReturns), len(rightReturns)))
	y := make([]float64, 0, cap(x))
	for date, leftReturn := range leftReturns {
		if rightReturn, ok := rightReturns[date]; ok {
			x = append(x, leftReturn)
			y = append(y, rightReturn)
		}
	}
	if len(x) < 20 {
		return 0, false
	}
	meanX, meanY := average(x), average(y)
	numerator, varianceX, varianceY := 0.0, 0.0, 0.0
	for index := range x {
		deltaX := x[index] - meanX
		deltaY := y[index] - meanY
		numerator += deltaX * deltaY
		varianceX += deltaX * deltaX
		varianceY += deltaY * deltaY
	}
	denominator := math.Sqrt(varianceX * varianceY)
	if denominator == 0 {
		return 0, false
	}
	return numerator / denominator, true
}

func returnsByDate(points []stockanalysis.TrendPoint) map[string]float64 {
	returns := make(map[string]float64, len(points))
	for index := 1; index < len(points); index++ {
		previous := points[index-1].Close
		if previous <= 0 || points[index].Close <= 0 || strings.TrimSpace(points[index].Date) == "" {
			continue
		}
		returns[points[index].Date] = points[index].Close/previous - 1
	}
	return returns
}

func calculateRiskContributions(results []HoldingResult, correlations []CorrelationPair) []RiskContribution {
	averageCorrelation := map[string][]float64{}
	for _, pair := range correlations {
		averageCorrelation[pair.LeftSymbol] = append(averageCorrelation[pair.LeftSymbol], pair.Correlation)
		averageCorrelation[pair.RightSymbol] = append(averageCorrelation[pair.RightSymbol], pair.Correlation)
	}
	items := make([]RiskContribution, 0, len(results))
	total := 0.0
	for _, result := range results {
		if result.Status != "succeeded" || result.Analysis == nil {
			continue
		}
		volatility := result.Analysis.Trend.ATR14Percent
		if volatility <= 0 {
			volatility = result.Analysis.Trend.ObservedVolatility
		}
		if volatility <= 0 {
			volatility = math.Max(1, float64(result.Analysis.RiskControl.Score)/12)
		}
		corr := average(averageCorrelation[result.Holding.Symbol])
		score := float64(result.Holding.Weight) * volatility * (.7 + .3*math.Max(0, corr))
		items = append(items, RiskContribution{Symbol: result.Holding.Symbol, Name: result.Analysis.Name, Weight: result.Holding.Weight, Score: score})
		total += score
	}
	for index := range items {
		if total > 0 {
			items[index].Percent = round(items[index].Score/total*100, 1)
		}
		items[index].Score = round(items[index].Score, 2)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Percent > items[j].Percent })
	return items
}

func styleMatch(metrics Metrics, rules ProfileRules) (int, []string) {
	score := 100.0
	breaches := make([]string, 0, 6)
	if metrics.MaxSinglePercent > rules.MaxSinglePercent {
		delta := metrics.MaxSinglePercent - rules.MaxSinglePercent
		score -= float64(delta * 2)
		breaches = append(breaches, "单只股票仓位超过该风格参考上限")
	}
	if metrics.TopThreePercent > rules.MaxTopThreePercent {
		delta := metrics.TopThreePercent - rules.MaxTopThreePercent
		score -= float64(delta)
		breaches = append(breaches, "前三大持仓集中度偏高")
	}
	if metrics.CashPercent < rules.MinimumCashPercent {
		delta := rules.MinimumCashPercent - metrics.CashPercent
		score -= float64(delta * 2)
		breaches = append(breaches, "现金缓冲低于该风格参考值")
	}
	if metrics.HighRiskPercent > rules.MaxHighRiskPercent {
		delta := metrics.HighRiskPercent - rules.MaxHighRiskPercent
		score -= float64(delta)
		breaches = append(breaches, "高风险个股仓位偏高")
	}
	if metrics.ShortTermPercent > rules.PreferredShortTermMax {
		delta := metrics.ShortTermPercent - rules.PreferredShortTermMax
		score -= float64(delta) * .7
		breaches = append(breaches, "短线仓位超过该风格偏好")
	}
	if metrics.StopLossRiskPercent > rules.MaxStopLossRisk {
		delta := metrics.StopLossRiskPercent - rules.MaxStopLossRisk
		score -= delta * 6
		breaches = append(breaches, "组合止损风险预算偏高")
	}
	return int(math.Round(math.Max(0, math.Min(100, score)))), breaches
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

func round(value float64, digits int) float64 {
	factor := math.Pow10(digits)
	return math.Round(value*factor) / factor
}
