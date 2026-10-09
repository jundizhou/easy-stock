package themeindex

import (
	"math"
	"sort"
	"time"

	"easy-stock/backend/internal/foundation"
)

var location = time.FixedZone("Asia/Shanghai", 8*60*60)

const baseValue = 1000.0

// CleanBars rejects malformed, duplicate and non-session bars. Normalizing all
// dates to exchange-local midnight also aligns providers running in UTC.
func CleanBars(input []foundation.KLine, now time.Time) []foundation.KLine {
	localNow := now.In(location)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	result := make([]foundation.KLine, 0, len(input))
	for _, bar := range input {
		if bar.Time.IsZero() || !foundation.IsAStockTradingDay(bar.Time) {
			continue
		}
		local := bar.Time.In(location)
		day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
		if day.After(today) || !validPrice(bar.Open) || !validPrice(bar.High) || !validPrice(bar.Low) || !validPrice(bar.Close) || bar.High < math.Max(bar.Open, bar.Close) || bar.Low > math.Min(bar.Open, bar.Close) || !finiteNonnegative(bar.Volume) || !finiteNonnegative(bar.Amount) {
			continue
		}
		bar.Time = day
		result = append(result, bar)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Time.Before(result[j].Time) })
	written := 0
	for _, bar := range result {
		if written > 0 && result[written-1].Time.Equal(bar.Time) {
			result[written-1] = bar
			continue
		}
		result[written] = bar
		written++
	}
	return result[:written]
}

func validPrice(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finiteNonnegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

type dailySum struct {
	n                       int
	open, high, low, close  float64
	returns, squaredReturns float64
	volume, amount          float64
}

// Compose uses daily rebalanced equal-weight returns, not average share prices.
// Histories must use the same forward-adjusted price basis within each stock.
// High/low are envelopes of constituent extrema, not observed index extrema.
func Compose(theme string, histories map[string][]foundation.KLine, population int, now time.Time) ([]foundation.KLine, foundation.ThemeIndexQuality) {
	sums := map[string]*dailySum{}
	paths := make([][]foundation.KLine, 0, len(histories))
	symbols := make([]string, 0, len(histories))
	for symbol := range histories {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	for _, symbol := range symbols {
		input := histories[symbol]
		bars := CleanBars(input, now)
		if len(bars) < 2 {
			continue
		}
		paths = append(paths, bars)
		previous := bars[0]
		for _, bar := range bars[1:] {
			// An internal missing session is carried flat, as for a suspension.
			// Never carry past a history's last date: that may be a stale fetch.
			for day := previous.Time.AddDate(0, 0, 1); day.Before(bar.Time); day = day.AddDate(0, 0, 1) {
				if foundation.IsAStockTradingDay(day) {
					addReturn(sums, day, 1, 1, 1, 1, 0, 0)
				}
			}
			base := previous.Close
			addReturn(sums, bar.Time, bar.Open/base, bar.High/base, bar.Low/base, bar.Close/base, bar.Volume, bar.Amount)
			previous = bar
		}
	}
	dates := make([]string, 0, len(sums))
	for date := range sums {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	quality := foundation.ThemeIndexQuality{Constituents: population, Loaded: len(paths), MinimumCoverage: 1, EstimatedExtrema: true, SamplingEstimateAvailable: population > len(paths) && len(paths) > 1}
	result := make([]foundation.KLine, 0, len(dates))
	level := baseValue
	for index, date := range dates {
		sum := sums[date]
		day, _ := time.ParseInLocation("2006-01-02", date, location)
		eligible := 0
		for _, bars := range paths {
			if day.After(bars[0].Time) {
				eligible++
			}
		}
		coverage := float64(sum.n) / float64(max(eligible, 1))
		quality.MinimumCoverage = math.Min(quality.MinimumCoverage, coverage)
		// Drop a low-coverage suffix entirely. Rejoining after an omitted return
		// would splice an incorrect cumulative path into the same index.
		if coverage < .8 {
			quality.SkippedDays = len(dates) - index
			quality.SamplingEstimateAvailable = false
			break
		}
		count := float64(sum.n)
		bar := foundation.KLine{Symbol: theme, Time: day, PreviousClose: level, Open: level * sum.open / count, High: level * sum.high / count, Low: level * sum.low / count, Close: level * sum.close / count, Volume: sum.volume, Amount: sum.amount}
		if !validPrice(bar.Open) || !validPrice(bar.High) || !validPrice(bar.Low) || !validPrice(bar.Close) || !finiteNonnegative(bar.Volume) || !finiteNonnegative(bar.Amount) {
			quality.SkippedDays = len(dates) - index
			quality.SamplingEstimateAvailable = false
			break
		}
		bar.ChangePercent = (bar.Close/level - 1) * 100
		result = append(result, bar)
		level = bar.Close
		// Finite-population standard error for uniform sampling without replacement.
		// This describes daily return sampling, not cumulative tracking error.
		if index >= len(dates)-20 && sum.n != len(paths) {
			quality.SamplingEstimateAvailable = false
		}
		if sum.n > 1 && population > sum.n && index >= len(dates)-20 {
			variance := math.Max(0, (sum.squaredReturns-sum.returns*sum.returns/count)/(count-1))
			if !finiteNonnegative(variance) {
				quality.SamplingEstimateAvailable = false
				continue
			}
			correction := float64(population-sum.n) / float64(max(population-1, 1))
			errorPercent := 1.96 * math.Sqrt(variance/count*correction) * 100
			quality.SamplingErrorPercent = math.Max(quality.SamplingErrorPercent, errorPercent)
		}
	}
	if !quality.SamplingEstimateAvailable {
		quality.SamplingErrorPercent = 0
	}
	return result, quality
}

func addReturn(sums map[string]*dailySum, day time.Time, open, high, low, close, volume, amount float64) {
	if !validPrice(open) || !validPrice(high) || !validPrice(low) || !validPrice(close) {
		return
	}
	key := day.Format("2006-01-02")
	if sums[key] == nil {
		sums[key] = &dailySum{}
	}
	s := sums[key]
	s.n++
	s.open += open
	s.high += high
	s.low += low
	s.close += close
	r := close - 1
	s.returns += r
	s.squaredReturns += r * r
	s.volume += volume
	s.amount += amount
}
