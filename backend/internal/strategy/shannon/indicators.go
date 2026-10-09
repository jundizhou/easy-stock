package shannon

import (
	"math"
	"strconv"

	"easy-stock/backend/internal/providers/stockdb"
)

// Bar 是一根标准 K 线（与 Python core/stockdb.py 的取数口径一致）。
// 类型定义已统一在 providers/stockdb 包，这里以别名复用，避免重复维护。
type Bar = stockdb.Bar

// ClosesOf / HighsOf / LowsOf 提取序列。
func ClosesOf(bars []Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Close
	}
	return out
}

func HighsOf(bars []Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.High
	}
	return out
}

func LowsOf(bars []Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Low
	}
	return out
}

// SMA 简单移动平均。返回与输入等长数组，前 n-1 位为 NaN。
// 与 Python indicators.sma 同口径：窗口遇无效值即清空。
func SMA(values []float64, n int) []float64 {
	out := make([]float64, len(values))
	if n <= 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	for i := range out {
		out[i] = math.NaN()
	}
	window := make([]float64, 0, n)
	for i, v := range values {
		if math.IsNaN(v) {
			window = window[:0]
			continue
		}
		window = append(window, v)
		if len(window) > n {
			window = window[1:]
		}
		if len(window) == n {
			sum := 0.0
			for _, x := range window {
				sum += x
			}
			out[i] = sum / float64(n)
		}
	}
	return out
}

// SwingPoint 是摆动点（索引 + 价格）。
type SwingPoint struct {
	Idx   int     `json:"idx"`
	Price float64 `json:"price"`
}

// FindSwings 识别摆动高点与低点。
//
// 定义：某根 K 线的高点严格大于左右各 k 根的高点 → 摆动高点（低点同理）。
// 与 Python indicators.find_swings 同口径：左右两侧都必须凑满 k 个有效值。
func FindSwings(highs, lows []float64, k int) ([]SwingPoint, []SwingPoint) {
	n := len(highs)
	var swH, swL []SwingPoint
	if n != len(lows) || k <= 0 {
		return swH, swL
	}
	for i := k; i < n-k; i++ {
		h := highs[i]
		l := lows[i]
		if math.IsNaN(h) || math.IsNaN(l) {
			continue
		}
		leftH := validSide(highs, i-k, i)
		rightH := validSide(highs, i+1, i+k+1)
		if len(leftH) == k && len(rightH) == k && h > maxOf(leftH) && h > maxOf(rightH) {
			swH = append(swH, SwingPoint{Idx: i, Price: h})
		}
		leftL := validSide(lows, i-k, i)
		rightL := validSide(lows, i+1, i+k+1)
		if len(leftL) == k && len(rightL) == k && l < minOf(leftL) && l < minOf(rightL) {
			swL = append(swL, SwingPoint{Idx: i, Price: l})
		}
	}
	return swH, swL
}

func validSide(values []float64, from, to int) []float64 {
	out := make([]float64, 0, to-from)
	for j := from; j < to && j < len(values); j++ {
		if !math.IsNaN(values[j]) {
			out = append(out, values[j])
		}
	}
	return out
}

func maxOf(values []float64) float64 {
	m := math.Inf(-1)
	for _, v := range values {
		if v > m {
			m = v
		}
	}
	return m
}

func minOf(values []float64) float64 {
	m := math.Inf(1)
	for _, v := range values {
		if v < m {
			m = v
		}
	}
	return m
}

// StructureState 趋势结构判定结果。
type StructureState struct {
	State       string    `json:"state"`
	HigherHighs bool      `json:"higher_highs"`
	HigherLows  bool      `json:"higher_lows"`
	LowerHighs  bool      `json:"lower_highs"`
	LowerLows   bool      `json:"lower_lows"`
	RecentHighs []float64 `json:"recent_highs"`
	RecentLows  []float64 `json:"recent_lows"`
}

// ClassifyStructure 判定趋势结构（HH+HL / LH+LL）。
// 与 Python indicators.classify_structure 同口径。
func ClassifyStructure(swH, swL []SwingPoint, n int) StructureState {
	hs := tailPrices(swH, n)
	ls := tailPrices(swL, n)
	hh := len(hs) >= 2 && strictlyIncreasing(hs)
	hl := len(ls) >= 2 && strictlyIncreasing(ls)
	lh := len(hs) >= 2 && strictlyDecreasing(hs)
	ll := len(ls) >= 2 && strictlyDecreasing(ls)

	state := "RANGE"
	switch {
	case hh && hl:
		state = "UPTREND"
	case lh && ll:
		state = "DOWNTREND"
	case hh && !hl:
		state = "TRANSITION"
	case lh && !ll:
		state = "TRANSITION"
	}
	return StructureState{
		State:       state,
		HigherHighs: hh,
		HigherLows:  hl,
		LowerHighs:  lh,
		LowerLows:   ll,
		RecentHighs: hs,
		RecentLows:  ls,
	}
}

func tailPrices(points []SwingPoint, n int) []float64 {
	if n <= 0 || len(points) == 0 {
		return nil
	}
	from := len(points) - n
	if from < 0 {
		from = 0
	}
	out := make([]float64, 0, len(points)-from)
	for _, p := range points[from:] {
		out = append(out, p.Price)
	}
	return out
}

func strictlyIncreasing(values []float64) bool {
	for i := 1; i < len(values); i++ {
		if !(values[i] > values[i-1]) {
			return false
		}
	}
	return true
}

func strictlyDecreasing(values []float64) bool {
	for i := 1; i < len(values); i++ {
		if !(values[i] < values[i-1]) {
			return false
		}
	}
	return true
}

// roundTo 按指定小数位做「正确的十进制四舍五入」。
//
// Python 的 round(x, n) 是对该浮点数的精确二进制值做 round-half-even，
// 而不是 math.Round(x*10^n)/10^n（后者会被二进制误差带偏，例如 2.675）。
// Go 的 strconv.FormatFloat 走的是精确十进制转换，语义与 Python 一致，
// 因此双端对拍必须用这里的实现，否则会在 .005 边界上出现 1 分钱级差异。
func roundTo(v float64, n int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	f, err := strconv.ParseFloat(strconv.FormatFloat(v, 'f', n, 64), 64)
	if err != nil {
		return v
	}
	return f
}

// Round2 保留两位小数（与 Python round(x, 2) 严格同语义）。
func Round2(v float64) float64 { return roundTo(v, 2) }

// Round4 保留四位小数（与 Python round(x, 4) 严格同语义）。
func Round4(v float64) float64 { return roundTo(v, 4) }
