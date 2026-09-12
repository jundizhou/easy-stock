package screener

import "math"

// 技术指标库：纯 Go 实现 MA / EMA / MACD / RSI / KDJ / BOLL。
// 输入均为时间升序的收盘价（或成交量）切片，长度不足时返回 nil。

// SMA 简单移动平均，最后一个值为最新。
func SMA(values []float64, n int) []float64 {
	if n <= 0 || len(values) < n {
		return nil
	}
	out := make([]float64, len(values))
	sum := 0.0
	for i, v := range values {
		sum += v
		if i >= n {
			sum -= values[i-n]
		}
		if i >= n-1 {
			out[i] = sum / float64(n)
		}
	}
	return out
}

// EMA 指数移动平均（标准 α=2/(n+1)，从首个值起算）。
func EMA(values []float64, n int) []float64 {
	if n <= 0 || len(values) == 0 {
		return nil
	}
	alpha := 2.0 / (float64(n) + 1.0)
	out := make([]float64, len(values))
	out[0] = values[0]
	for i := 1; i < len(values); i++ {
		out[i] = alpha*values[i] + (1-alpha)*out[i-1]
	}
	return out
}

// MACD 返回 DIF / DEA / HIST 三条线。
func MACD(close []float64, fast, slow, signal int) (dif, dea, hist []float64) {
	emaFast := EMA(close, fast)
	emaSlow := EMA(close, slow)
	if emaFast == nil || emaSlow == nil {
		return nil, nil, nil
	}
	dif = make([]float64, len(close))
	for i := range close {
		dif[i] = emaFast[i] - emaSlow[i]
	}
	dea = EMA(dif, signal)
	if dea == nil {
		return nil, nil, nil
	}
	hist = make([]float64, len(close))
	for i := range close {
		hist[i] = (dif[i] - dea[i]) * 2
	}
	return dif, dea, hist
}

// RSI 相对强弱指标（Wilder 平滑）。
func RSI(close []float64, n int) []float64 {
	if n <= 0 || len(close) <= n {
		return nil
	}
	out := make([]float64, len(close))
	var avgGain, avgLoss float64
	for i := 1; i <= n; i++ {
		change := close[i] - close[i-1]
		if change > 0 {
			avgGain += change
		} else {
			avgLoss -= change
		}
	}
	avgGain /= float64(n)
	avgLoss /= float64(n)
	out[n] = rsiValue(avgGain, avgLoss)
	for i := n + 1; i < len(close); i++ {
		change := close[i] - close[i-1]
		gain, loss := 0.0, 0.0
		if change > 0 {
			gain = change
		} else {
			loss = -change
		}
		avgGain = (avgGain*float64(n-1) + gain) / float64(n)
		avgLoss = (avgLoss*float64(n-1) + loss) / float64(n)
		out[i] = rsiValue(avgGain, avgLoss)
	}
	return out
}

func rsiValue(avgGain, avgLoss float64) float64 {
	if avgLoss < 1e-12 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}

// KDJ 随机指标（9,3,3），返回 K / D / J。
func KDJ(high, low, close []float64, n, kSmooth, dSmooth int) (k, d, j []float64) {
	if n <= 0 || len(close) < n {
		return nil, nil, nil
	}
	k = make([]float64, len(close))
	d = make([]float64, len(close))
	j = make([]float64, len(close))
	var prevK, prevD = 50.0, 50.0
	for i := 0; i < len(close); i++ {
		start := i - n + 1
		if start < 0 {
			start = 0
		}
		hh, ll := high[i], low[i]
		for m := start; m <= i; m++ {
			if high[m] > hh {
				hh = high[m]
			}
			if low[m] < ll {
				ll = low[m]
			}
		}
		rsv := 50.0
		if hh-ll > 1e-12 {
			rsv = (close[i] - ll) / (hh - ll) * 100
		}
		prevK = (2.0/float64(kSmooth))*prevK + (1.0/float64(kSmooth))*rsv
		prevD = (2.0/float64(dSmooth))*prevD + (1.0/float64(dSmooth))*prevK
		k[i], d[i], j[i] = prevK, prevD, 3*prevK-2*prevD
	}
	return k, d, j
}

// BOLL 布林带，返回中轨 / 上轨 / 下轨。
func BOLL(close []float64, n int, bandwidth float64) (mid, upper, lower []float64) {
	mid = SMA(close, n)
	if mid == nil {
		return nil, nil, nil
	}
	upper = make([]float64, len(close))
	lower = make([]float64, len(close))
	for i := n - 1; i < len(close); i++ {
		variance := 0.0
		for m := i - n + 1; m <= i; m++ {
			diff := close[m] - mid[i]
			variance += diff * diff
		}
		sd := sqrt(variance / float64(n))
		upper[i] = mid[i] + bandwidth*sd
		lower[i] = mid[i] - bandwidth*sd
	}
	return mid, upper, lower
}

// 最高/最低：区间 [i-n+1, i] 内的最高价/最低价（不含可选项时含当前）。
func highest(values []float64, i, n int, includeCurrent bool) float64 {
	start := i - n + 1
	if !includeCurrent {
		i--
	}
	if start < 0 {
		start = 0
	}
	h := values[start]
	for m := start + 1; m <= i && m < len(values); m++ {
		if values[m] > h {
			h = values[m]
		}
	}
	return h
}

func lowest(values []float64, i, n int, includeCurrent bool) float64 {
	start := i - n + 1
	if !includeCurrent {
		i--
	}
	if start < 0 {
		start = 0
	}
	l := values[start]
	for m := start + 1; m <= i && m < len(values); m++ {
		if values[m] < l {
			l = values[m]
		}
	}
	return l
}

func sqrt(x float64) float64 {
	return math.Sqrt(x)
}

// crossedUp 判断序列 a 在最近 lookback 根内从下方上穿序列 b（a[i-1]<=b[i-1] 且 a[i]>b[i]）。
func crossedUp(a, b []float64, lookback int) (bool, int) {
	if a == nil || b == nil {
		return false, 0
	}
	for i := len(a) - 1; i > 0 && i >= len(a)-lookback; i-- {
		if a[i-1] <= b[i-1] && a[i] > b[i] {
			return true, len(a) - 1 - i
		}
	}
	return false, -1
}
