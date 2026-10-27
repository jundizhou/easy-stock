package monitor

// 技术信号库：规则类型 signal 的字段来源。
//
// 与参考工程（tick-stock-panel）的"用户写表达式→全市场算历史列"不同，
// 这里是引擎内置的日线事件信号：纯函数、只依赖日线缓存（与 60 日高低
// 同一条链路），规则里像选字段一样选信号，多条信号还能用 and/or 组合。
//
// 触发语义是"今日事件"：比较最近两根已收盘日线，信号只在事件发生
// 当天为真，次日自动归零。配合状态机的布尔分支（一天最多一次），
// 不会每轮快照重复触发，也不需要用户理解"上升沿"是什么。

import (
	"math"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// 信号 ID 与前端、事件消息共用。新增信号只改这个清单与 computeSignal。
const (
	SignalMA5UpMA20      = "ma5_up_ma20"      // MA5 上穿 MA20（短线走强）
	SignalMA5DownMA20    = "ma5_down_ma20"    // MA5 下穿 MA20（短线走弱）
	SignalMA20UpMA60     = "ma20_up_ma60"     // MA20 上穿 MA60（中线走强）
	SignalMA20DownMA60   = "ma20_down_ma60"   // MA20 下穿 MA60（中线走弱）
	SignalMACDGolden     = "macd_golden"      // MACD 金叉
	SignalMACDDead       = "macd_dead"        // MACD 死叉
	SignalBollUpperBreak = "boll_upper_break" // 收盘突破布林上轨
	SignalBollLowerBreak = "boll_lower_break" // 收盘跌破布林下轨
	SignalVolDouble      = "vol_double"       // 放量（成交量≥前5日均量2倍）
	SignalVolShrink      = "vol_shrink"       // 缩量（成交量≤前5日均量一半）
)

// signalCatalog 是信号的中文名与一句话说明。消息文案与前端下拉共用，
// 改这里同时改两处。
var signalCatalog = map[string]string{
	SignalMA5UpMA20:      "MA5 上穿 MA20",
	SignalMA5DownMA20:    "MA5 下穿 MA20",
	SignalMA20UpMA60:     "MA20 上穿 MA60",
	SignalMA20DownMA60:   "MA20 下穿 MA60",
	SignalMACDGolden:     "MACD 金叉",
	SignalMACDDead:       "MACD 死叉",
	SignalBollUpperBreak: "突破布林上轨",
	SignalBollLowerBreak: "跌破布林下轨",
	SignalVolDouble:      "放量（≥5日均量2倍）",
	SignalVolShrink:      "缩量（≤5日均量一半）",
}

// signalDescriptions 给前端下拉的补充说明，缺省回落到中文名。
var signalDescriptions = map[string]string{
	SignalMA5UpMA20:      "短线转强，常见于回调结束",
	SignalMA5DownMA20:    "短线转弱，警惕回撤",
	SignalMA20UpMA60:     "中线趋势转多",
	SignalMA20DownMA60:   "中线趋势转空",
	SignalMACDGolden:     "动能由弱转强",
	SignalMACDDead:       "动能由强转弱",
	SignalBollUpperBreak: "波动放大，情绪偏亢奋",
	SignalBollLowerBreak: "超卖边缘，注意止跌信号",
	SignalVolDouble:      "资金异动，注意方向",
	SignalVolShrink:      "交投转淡，等待方向",
}

// signalIDs 返回稳定排序的信号清单（给字段白名单与前端目录用）。
func signalIDs() []string {
	order := []string{
		SignalMA5UpMA20, SignalMA5DownMA20, SignalMA20UpMA60, SignalMA20DownMA60,
		SignalMACDGolden, SignalMACDDead, SignalBollUpperBreak, SignalBollLowerBreak,
		SignalVolDouble, SignalVolShrink,
	}
	return order
}

func isSignalField(field string) bool {
	_, ok := signalCatalog[field]
	return ok
}

func signalLabel(field string) string {
	if label, ok := signalCatalog[field]; ok {
		return label
	}
	return field
}

// signalMinBars 是计算全部信号所需的最少日线根数（含当日）。
// MA60 交叉需要 61 根收盘价作比较，布林与 MACD 都更少。
const signalMinBars = 62

// computeSignals 从日线序列算"今天发生了哪些技术信号"。
// 返回的 map 只包含成立的信号；没算出的信号（数据不足）不出现，
// 让 AND 语义的规则保持沉默而不是误报。
func computeSignals(bars []foundation.KLine) map[string]bool {
	closes, volumes := closedSeries(bars)
	if len(closes) < 2 {
		return nil
	}
	flags := map[string]bool{}
	// 均线交叉：最近一根与前一根的快慢线相对位置互换。
	crossPairs := []struct {
		signal string
		fast   int
		slow   int
	}{
		{SignalMA5UpMA20, 5, 20},
		{SignalMA5DownMA20, 5, 20},
		{SignalMA20UpMA60, 20, 60},
		{SignalMA20DownMA60, 20, 60},
	}
	for _, pair := range crossPairs {
		need := pair.slow + 1
		if len(closes) < need {
			continue
		}
		fastNow := sma(closes, pair.fast, 0)
		slowNow := sma(closes, pair.slow, 0)
		fastPrev := sma(closes, pair.fast, 1)
		slowPrev := sma(closes, pair.slow, 1)
		if fastNow == 0 || slowNow == 0 || fastPrev == 0 || slowPrev == 0 {
			continue
		}
		switch pair.signal {
		case SignalMA5UpMA20, SignalMA20UpMA60:
			flags[pair.signal] = fastNow > slowNow && fastPrev <= slowPrev
		default:
			flags[pair.signal] = fastNow < slowNow && fastPrev >= slowPrev
		}
	}
	if values := macdSeries(closes); len(values.dif) >= 2 {
		dif := values.dif[len(values.dif)-1]
		dea := values.dea[len(values.dea)-1]
		prevDif := values.dif[len(values.dif)-2]
		prevDea := values.dea[len(values.dea)-2]
		flags[SignalMACDGolden] = dif > dea && prevDif <= prevDea
		flags[SignalMACDDead] = dif < dea && prevDif >= prevDea
	}
	if upper, lower, ok := bollBands(closes); ok {
		prevUpper, prevLower := bollBandsPrev(closes)
		if prevUpper != nil && prevLower != nil {
			last := closes[len(closes)-1]
			flags[SignalBollUpperBreak] = last > upper && closes[len(closes)-2] <= *prevUpper
			flags[SignalBollLowerBreak] = last < lower && closes[len(closes)-2] >= *prevLower
		}
	}
	if len(volumes) >= 6 {
		today := volumes[len(volumes)-1]
		avg5 := (volumes[len(volumes)-6] + volumes[len(volumes)-5] + volumes[len(volumes)-4] +
			volumes[len(volumes)-3] + volumes[len(volumes)-2]) / 5
		if avg5 > 0 {
			flags[SignalVolDouble] = today >= avg5*2
			flags[SignalVolShrink] = today <= avg5*0.5
		}
	}
	for id, on := range flags {
		if !on {
			delete(flags, id)
		}
	}
	if len(flags) == 0 {
		return nil
	}
	return flags
}

// closedSeries 取已收盘的收盘价与成交量序列。最后一根是今天盘中
// 未定格的棒（与 highLowFlags 同一判断），剔除后才参与比较。
func closedSeries(bars []foundation.KLine) (closes []float64, volumes []float64) {
	historic := bars
	if len(bars) > 0 && barDate(bars[len(bars)-1]) == foundation.LatestAStockSession(time.Now()) {
		historic = bars[:len(bars)-1]
	}
	for _, bar := range historic {
		if bar.Close <= 0 {
			continue
		}
		closes = append(closes, bar.Close)
		volumes = append(volumes, bar.Volume)
	}
	return closes, volumes
}

// sma 计算 closes 末尾第 offset 天前的 n 日简单均线；offset=0 是最新值。
// 数据不足或窗口里有非正数时返回 0（0 不会被当成有效均线参与比较）。
func sma(closes []float64, n, offset int) float64 {
	end := len(closes) - offset
	if end < n || n <= 0 {
		return 0
	}
	sum := 0.0
	for _, close := range closes[end-n : end] {
		if close <= 0 {
			return 0
		}
		sum += close
	}
	return sum / float64(n)
}

// ema 指数均线，标准 2/(n+1) 平滑；从第一根开始暖机。
func ema(values []float64, n int) []float64 {
	if len(values) == 0 || n <= 0 {
		return nil
	}
	alpha := 2.0 / float64(n+1)
	out := make([]float64, len(values))
	out[0] = values[0]
	for index := 1; index < len(values); index++ {
		out[index] = alpha*values[index] + (1-alpha)*out[index-1]
	}
	return out
}

type macdResult struct {
	dif []float64
	dea []float64
}

func macdSeries(closes []float64) macdResult {
	fast := ema(closes, 12)
	slow := ema(closes, 26)
	if fast == nil || slow == nil {
		return macdResult{}
	}
	dif := make([]float64, len(closes))
	for index := range closes {
		dif[index] = fast[index] - slow[index]
	}
	dea := ema(dif, 9)
	return macdResult{dif: dif, dea: dea}
}

// bollBands 布林带 (20, 2)：中轨 20 日均线，上下轨 ±2 倍标准差。
func bollBands(closes []float64) (upper, lower float64, ok bool) {
	window := closes
	if len(closes) < 20 {
		return 0, 0, false
	}
	window = closes[len(closes)-20:]
	mid := 0.0
	for _, close := range window {
		if close <= 0 {
			return 0, 0, false
		}
		mid += close
	}
	mid /= 20
	variance := 0.0
	for _, close := range window {
		variance += (close - mid) * (close - mid)
	}
	dev := sqrtOf(variance / 20)
	return mid + 2*dev, mid - 2*dev, true
}

// bollBandsPrev 返回前一日的布林上下轨；数据不足返回 nil。
func bollBandsPrev(closes []float64) (prevUpper, prevLower *float64) {
	if len(closes) < 21 {
		return nil, nil
	}
	upper, lower, ok := bollBands(closes[:len(closes)-1])
	if !ok {
		return nil, nil
	}
	return &upper, &lower
}

func sqrtOf(value float64) float64 {
	if value <= 0 {
		return 0
	}
	return math.Sqrt(value)
}

// barDate 取一根日线的交易日（本地时区 YYYY-MM-DD）。
func barDate(bar foundation.KLine) string {
	return bar.Time.In(foundation.AStockLocation).Format("2006-01-02")
}

// signalMessage 把成立的信号翻译成事件里的一句话。
func signalMessage(field string) string {
	label := signalLabel(field)
	if strings.TrimSpace(label) == "" {
		label = field
	}
	return label
}
