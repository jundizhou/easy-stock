package dailyanalysis

import (
	"math"

	"easy-stock/backend/internal/foundation"
)

// 本文件实现 A 股常用技术指标的确定性计算，口径与主流行情软件一致：
// MACD 柱 = 2*(DIF-DEA)，KDJ 用 9 日 RSV 三日平滑，RSI 用威尔斯威尔德 SMA 平滑。

func closes(lines []foundation.KLine) []float64 {
	values := make([]float64, len(lines))
	for i, line := range lines {
		values[i] = line.Close
	}
	return values
}

func sma(values []float64, window int) float64 {
	if window <= 0 || len(values) < window {
		return 0
	}
	sum := 0.0
	for _, value := range values[len(values)-window:] {
		sum += value
	}
	return sum / float64(window)
}

func emaSeries(values []float64, period int) []float64 {
	if len(values) == 0 || period <= 0 {
		return nil
	}
	alpha := 2.0 / float64(period+1)
	series := make([]float64, len(values))
	previous := values[0]
	for i, value := range values {
		if i == 0 {
			previous = value
		} else {
			previous = previous*(1-alpha) + value*alpha
		}
		series[i] = previous
	}
	return series
}

func highest(lines []foundation.KLine, window int) float64 {
	if len(lines) == 0 {
		return 0
	}
	if window <= 0 || window > len(lines) {
		window = len(lines)
	}
	result := math.Inf(-1)
	for _, line := range lines[len(lines)-window:] {
		if line.High > result {
			result = line.High
		}
	}
	if math.IsInf(result, -1) {
		return 0
	}
	return result
}

func lowest(lines []foundation.KLine, window int) float64 {
	if len(lines) == 0 {
		return 0
	}
	if window <= 0 || window > len(lines) {
		window = len(lines)
	}
	result := math.Inf(1)
	for _, line := range lines[len(lines)-window:] {
		if line.Low < result {
			result = line.Low
		}
	}
	if math.IsInf(result, 1) {
		return 0
	}
	return result
}

func stdDev(values []float64, window int) float64 {
	if window <= 1 || len(values) < window {
		return 0
	}
	segment := values[len(values)-window:]
	mean := 0.0
	for _, value := range segment {
		mean += value
	}
	mean /= float64(window)
	variance := 0.0
	for _, value := range segment {
		variance += (value - mean) * (value - mean)
	}
	return math.Sqrt(variance / float64(window))
}

// rsi 按国内软件惯例：SMA(涨幅,n,1) / SMA( abs(涨跌幅),n,1 ) * 100。
func rsi(values []float64, period int) float64 {
	if len(values) < period+1 || period <= 0 {
		return 0
	}
	var upAvg, totalAvg float64
	for i := 1; i < len(values); i++ {
		change := values[i] - values[i-1]
		up, total := math.Max(change, 0), math.Abs(change)
		if i <= period {
			upAvg += up
			totalAvg += total
			if i == period {
				upAvg /= float64(period)
				totalAvg /= float64(period)
			}
		} else {
			upAvg = (upAvg*float64(period-1) + up) / float64(period)
			totalAvg = (totalAvg*float64(period-1) + total) / float64(period)
		}
	}
	if totalAvg == 0 {
		return 100
	}
	return upAvg / totalAvg * 100
}

func kdj(lines []foundation.KLine) (k, d, j float64) {
	if len(lines) < 9 {
		return 0, 0, 0
	}
	var prevK, prevD float64
	for i := 8; i < len(lines); i++ {
		window := lines[i-8 : i+1]
		high, low := highest(window, 9), lowest(window, 9)
		if high <= low {
			rsv := 50.0
			prevK, prevD = rsvOn(prevK, prevD, rsv)
			continue
		}
		rsv := (lines[i].Close - low) / (high - low) * 100
		prevK = prevK*2.0/3.0 + rsv/3.0
		prevD = prevD*2.0/3.0 + prevK/3.0
	}
	if prevK == 0 && prevD == 0 {
		return 0, 0, 0
	}
	return prevK, prevD, 3*prevK - 2*prevD
}

func rsvOn(prevK, prevD, rsv float64) (float64, float64) {
	nextK := prevK*2.0/3.0 + rsv/3.0
	nextD := prevD*2.0/3.0 + nextK/3.0
	return nextK, nextD
}

// ComputeIndicators 基于日 K（建议至少 120 根）计算全部指标快照。
func ComputeIndicators(lines []foundation.KLine) Indicators {
	result := Indicators{}
	if len(lines) < 6 {
		return result
	}
	values := closes(lines)
	ema12 := emaSeries(values, 12)
	ema26 := emaSeries(values, 26)
	difSeries := make([]float64, len(values))
	for i := range values {
		difSeries[i] = ema12[i] - ema26[i]
	}
	deaSeries := emaSeries(difSeries, 9)
	result.MACDDif = difSeries[len(difSeries)-1]
	result.MACDDea = deaSeries[len(deaSeries)-1]
	result.MACDHist = 2 * (result.MACDDif - result.MACDDea)
	result.MA5 = sma(values, 5)
	result.MA10 = sma(values, 10)
	result.MA20 = sma(values, 20)
	result.MA60 = sma(values, 60)
	result.KDJk, result.KDJd, result.KDJj = kdj(lines)
	result.Rsi6 = rsi(values, 6)
	result.Rsi14 = rsi(values, 14)
	result.BollMid = sma(values, 20)
	band := 2 * stdDev(values, 20)
	result.BollUpper = result.BollMid + band
	result.BollLower = result.BollMid - band
	latest := lines[len(lines)-1]
	result.TwentyDayHigh = highest(lines[:len(lines)-1], 20)
	result.TwentyDayLow = lowest(lines[:len(lines)-1], 20)
	result.Support = math.Max(result.TwentyDayLow, result.BollLower)
	result.Resistance = result.TwentyDayHigh
	if result.Resistance <= latest.Close {
		result.Resistance = result.BollUpper
	}
	if len(lines) >= 6 {
		var recentVolume float64
		for _, line := range lines[len(lines)-6 : len(lines)-1] {
			recentVolume += line.Volume
		}
		recentVolume /= 5
		if recentVolume > 0 {
			result.VolumeRatio = latest.Volume / recentVolume
		}
	}
	return result
}

// macdCrossState 返回 "golden"（刚金叉）、"dead"（刚死叉）或 ""。
func macdCrossState(lines []foundation.KLine) string {
	values := closes(lines)
	if len(values) < 40 {
		return ""
	}
	ema12 := emaSeries(values, 12)
	ema26 := emaSeries(values, 26)
	difSeries := make([]float64, len(values))
	for i := range values {
		difSeries[i] = ema12[i] - ema26[i]
	}
	deaSeries := emaSeries(difSeries, 9)
	n := len(values)
	histNow := 2 * (difSeries[n-1] - deaSeries[n-1])
	histPrev := 2 * (difSeries[n-2] - deaSeries[n-2])
	if histPrev <= 0 && histNow > 0 {
		return "golden"
	}
	if histPrev >= 0 && histNow < 0 {
		return "dead"
	}
	return ""
}

// maCrossState 返回 MA5 上穿/下穿 MA10 的信号。
func maCrossState(lines []foundation.KLine) string {
	values := closes(lines)
	if len(values) < 12 {
		return ""
	}
	ma5Now, ma10Now := sma(values, 5), sma(values, 10)
	previous := values[:len(values)-1]
	ma5Prev, ma10Prev := sma(previous, 5), sma(previous, 10)
	if ma5Prev <= ma10Prev && ma5Now > ma10Now {
		return "golden"
	}
	if ma5Prev >= ma10Prev && ma5Now < ma10Now {
		return "dead"
	}
	return ""
}
