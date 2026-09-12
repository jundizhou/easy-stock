package dailyanalysis

import (
	"fmt"
	"math"
	"strings"

	"easy-stock/backend/internal/foundation"
)

// scoreBreakdown 决策评分的分项，权重设计参考 daily_stock_analysis 的决策仪表盘：
// 趋势 30 + 动量 25 + 强弱平衡 15 + 通道位置 10 + 量能 10 + 当日强度 10。
type scoreBreakdown struct {
	Total    int
	Trend    float64
	Momentum float64
	Strength float64
	Channel  float64
	Volume   float64
	Pulse    float64
}

func round(value float64, digits int) float64 {
	shift := math.Pow10(digits)
	return math.Round(value*shift) / shift
}

func buildStockReport(lines []foundation.KLine, quote *foundation.Quote, symbol string) StockReport {
	report := StockReport{Symbol: symbol, Status: "succeeded"}
	if quote != nil && strings.TrimSpace(quote.Name) != "" {
		report.Name = quote.Name
	}
	if len(lines) == 0 {
		report.Status = "failed"
		report.Error = "未获取到日 K 数据"
		return report
	}
	latest := lines[len(lines)-1]
	report.Indicators = ComputeIndicators(lines)
	ind := report.Indicators
	if latest.Time.IsZero() {
		report.TradeDate = ""
	} else {
		report.TradeDate = latest.Time.Format("2006-01-02")
	}
	if quote != nil && quote.Price > 0 {
		report.Price = quote.Price
		report.ChangePercent = quote.ChangePercent
	} else {
		report.Price = latest.Close
		report.ChangePercent = changePercentOf(latest)
	}

	breakdown := scoreStock(lines, ind, report.ChangePercent)
	report.Score = breakdown.Total
	report.Action = actionForScore(breakdown.Total)
	report.Trend, report.TrendDetail = classifyTrend(lines, ind)
	report.Signals = collectSignals(lines, ind, report.ChangePercent)
	report.Risks = collectRisks(lines, ind, report.Price)
	report.Checklist = buildChecklist(report)
	return report
}

func changePercentOf(line foundation.KLine) float64 {
	if line.PreviousClose > 0 {
		return (line.Close - line.PreviousClose) / line.PreviousClose * 100
	}
	return line.ChangePercent
}

func scoreStock(lines []foundation.KLine, ind Indicators, changePercent float64) scoreBreakdown {
	breakdown := scoreBreakdown{}
	if len(lines) == 0 {
		return breakdown
	}
	latest := lines[len(lines)-1].Close
	if latest <= 0 {
		return breakdown
	}

	// 趋势（30）：价格与均线的相对位置（±8% 区间线性映射）+ 均线排列修正。
	if ind.MA20 > 0 {
		gap := (latest - ind.MA20) / ind.MA20 * 100
		breakdown.Trend += clamp((gap+8)/16, 0, 1) * 15
	}
	if ind.MA60 > 0 {
		gap := (latest - ind.MA60) / ind.MA60 * 100
		breakdown.Trend += clamp((gap+8)/16, 0, 1) * 15
	}
	if ind.MA5 > ind.MA10 && ind.MA10 > ind.MA20 && ind.MA20 > 0 {
		breakdown.Trend = clamp(breakdown.Trend+3, 0, 30)
	}
	if ind.MA5 < ind.MA10 && ind.MA10 < ind.MA20 && ind.MA20 > 0 {
		breakdown.Trend = clamp(breakdown.Trend-3, 0, 30)
	}

	// 动量（25）：MACD 柱方向与幅度（中性收敛时只给中性分）+ KDJ 方向。
	if ind.MACDDea != 0 {
		histRatio := ind.MACDHist / math.Abs(ind.MACDDea)
		breakdown.Momentum += clamp(10+histRatio*15, 0, 20)
	} else if ind.MACDHist > 0 {
		breakdown.Momentum += 12
	}
	if ind.KDJk >= ind.KDJd {
		breakdown.Momentum += 5
	}
	if ind.KDJj > ind.KDJk {
		breakdown.Momentum += 2.5
	}
	breakdown.Momentum = clamp(breakdown.Momentum, 0, 25)

	// 强弱平衡（15）：RSI 处于 45-70 区间最优，极端超买扣分、超卖留反弹分。
	switch {
	case ind.Rsi14 >= 45 && ind.Rsi14 <= 70:
		breakdown.Strength = 15
	case ind.Rsi14 > 70 && ind.Rsi14 <= 85:
		breakdown.Strength = 9
	case ind.Rsi14 > 85:
		breakdown.Strength = 3
	case ind.Rsi14 >= 30 && ind.Rsi14 < 45:
		breakdown.Strength = 8
	default:
		breakdown.Strength = 6
	}

	// 通道位置（10）：BOLL 位置，中轨上方加分，贴上轨适度降分防追高。
	if ind.BollUpper > ind.BollLower {
		position := (latest - ind.BollLower) / (ind.BollUpper - ind.BollLower)
		switch {
		case position >= 0.5 && position <= 0.95:
			breakdown.Channel = 10
		case position > 0.95:
			breakdown.Channel = 4
		case position >= 0.3:
			breakdown.Channel = 7
		default:
			breakdown.Channel = 4
		}
	}

	// 量能（10）：温和放量加分，异常巨量或极度缩量减分。
	switch {
	case ind.VolumeRatio >= 0.9 && ind.VolumeRatio <= 2.5:
		breakdown.Volume = 10
	case ind.VolumeRatio > 2.5 && ind.VolumeRatio <= 5:
		breakdown.Volume = 5
	case ind.VolumeRatio > 5:
		breakdown.Volume = 2
	case ind.VolumeRatio >= 0.5:
		breakdown.Volume = 6
	default:
		breakdown.Volume = 3
	}

	// 当日强度（10）：温和上涨最优，大跌扣分。
	switch {
	case changePercent >= 1 && changePercent <= 7:
		breakdown.Pulse = 10
	case changePercent > 7:
		breakdown.Pulse = 5
	case changePercent >= 0:
		breakdown.Pulse = 6
	case changePercent >= -3:
		breakdown.Pulse = 4
	default:
		breakdown.Pulse = 1
	}

	total := breakdown.Trend + breakdown.Momentum + breakdown.Strength + breakdown.Channel + breakdown.Volume + breakdown.Pulse
	breakdown.Total = int(math.Round(clamp(total, 0, 100)))
	return breakdown
}

func actionForScore(score int) string {
	switch {
	case score >= 75:
		return "买入参考"
	case score >= 60:
		return "持有关注"
	case score >= 45:
		return "观望"
	case score >= 30:
		return "减仓考虑"
	default:
		return "回避"
	}
}

func classifyTrend(lines []foundation.KLine, ind Indicators) (string, string) {
	latest := lines[len(lines)-1].Close
	aboveMA20 := ind.MA20 > 0 && latest > ind.MA20
	aboveMA60 := ind.MA60 > 0 && latest > ind.MA60
	bullStack := ind.MA5 > ind.MA10 && ind.MA10 > ind.MA20 && ind.MA20 > 0
	bearStack := ind.MA5 < ind.MA10 && ind.MA10 < ind.MA20 && ind.MA20 > 0
	switch {
	case bullStack && aboveMA20 && aboveMA60:
		return "多头排列", "均线多头排列，价格站上中期与长期均线，趋势结构完好"
	case bearStack && !aboveMA20:
		return "空头排列", "均线空头排列，价格位于中期均线下方，反弹宜谨慎"
	case aboveMA20:
		return "趋势偏多", "价格运行在中期均线上方，短期均线反复，趋势尚未走坏"
	case !aboveMA60 && !aboveMA20:
		return "趋势偏弱", "价格跌破中期与长期均线，等待企稳信号"
	default:
		return "震荡整理", "均线粘合，价格围绕中期均线反复，方向待选择"
	}
}

func collectSignals(lines []foundation.KLine, ind Indicators, changePercent float64) []string {
	signals := make([]string, 0, 8)
	switch maCrossState(lines) {
	case "golden":
		signals = append(signals, "MA5 上穿 MA10（短线均线金叉）")
	case "dead":
		signals = append(signals, "MA5 下穿 MA10（短线均线死叉）")
	}
	// 交叉信号已表达动能方向，避免再输出同向的柱体描述。
	switch macdCrossState(lines) {
	case "golden":
		signals = append(signals, "MACD 金叉，红柱扩大")
	case "dead":
		signals = append(signals, "MACD 死叉，绿柱扩大")
	default:
		if ind.MACDHist > 0 {
			signals = append(signals, fmt.Sprintf("MACD 红柱 %.3f，多头动能在场", ind.MACDHist))
		} else if ind.MACDHist < 0 {
			signals = append(signals, fmt.Sprintf("MACD 绿柱 %.3f，空头动能占优", ind.MACDHist))
		}
	}
	if ind.KDJj > 90 {
		signals = append(signals, fmt.Sprintf("KDJ J 值 %.1f 超买区，注意短线回撤", ind.KDJj))
	} else if ind.KDJj < 10 {
		signals = append(signals, fmt.Sprintf("KDJ J 值 %.1f 超卖区，存在修复需求", ind.KDJj))
	}
	if ind.Rsi14 > 85 {
		signals = append(signals, fmt.Sprintf("RSI14 %.1f 严重超买", ind.Rsi14))
	} else if ind.Rsi14 < 25 {
		signals = append(signals, fmt.Sprintf("RSI14 %.1f 严重超卖", ind.Rsi14))
	}
	if ind.VolumeRatio >= 2 {
		signals = append(signals, fmt.Sprintf("量比 %.2f 明显放量", ind.VolumeRatio))
	} else if ind.VolumeRatio <= 0.6 {
		signals = append(signals, fmt.Sprintf("量比 %.2f 明显缩量", ind.VolumeRatio))
	}
	latest := lines[len(lines)-1]
	if ind.TwentyDayHigh > 0 && latest.Close > ind.TwentyDayHigh {
		signals = append(signals, "收盘创 20 日新高，突破形态")
	}
	if ind.TwentyDayLow > 0 && latest.Close < ind.TwentyDayLow {
		signals = append(signals, "收盘创 20 日新低，破位形态")
	}
	if changePercent >= 9.5 {
		signals = append(signals, "当日接近涨停，情绪高涨")
	}
	return signals
}

func collectRisks(lines []foundation.KLine, ind Indicators, price float64) []string {
	risks := make([]string, 0, 6)
	if ind.MA20 > 0 && price < ind.MA20 {
		risks = append(risks, fmt.Sprintf("价格跌破 20 日均线（%.2f），中期趋势转弱", ind.MA20))
	}
	if macdCrossState(lines) == "dead" {
		risks = append(risks, "MACD 刚形成死叉，短期动能转空")
	}
	if ind.Rsi14 > 85 {
		risks = append(risks, fmt.Sprintf("RSI14 高达 %.1f，追高风险显著", ind.Rsi14))
	}
	if ind.TwentyDayLow > 0 && price < ind.TwentyDayLow {
		risks = append(risks, fmt.Sprintf("跌破 20 日低点 %.2f，技术破位", ind.TwentyDayLow))
	}
	if ind.VolumeRatio > 3 && priceChange(lines) < 0 {
		risks = append(risks, "放量下跌，抛压沉重")
	}
	if ind.MA5 < ind.MA10 && ind.MA10 < ind.MA20 && ind.MA20 < ind.MA60 {
		risks = append(risks, "均线系统全面空头排列，下行趋势尚未扭转")
	}
	if len(risks) == 0 {
		risks = append(risks, "暂无明显技术性风险信号，仍需关注大盘环境与个股消息面")
	}
	return risks
}

func buildChecklist(report StockReport) []string {
	ind := report.Indicators
	checklist := make([]string, 0, 5)
	switch report.Action {
	case "买入参考":
		checklist = append(checklist, fmt.Sprintf("介入前确认量能是否延续（当前量比 %.2f）", ind.VolumeRatio))
		checklist = append(checklist, fmt.Sprintf("若跌破支撑位 %.2f 则执行止损", round(ind.Support, 2)))
		checklist = append(checklist, fmt.Sprintf("首个压力位 %.2f，突破可持股待涨", round(ind.Resistance, 2)))
	case "持有关注":
		checklist = append(checklist, fmt.Sprintf("持股观察，收盘跌破 20 日线 %.2f 再考虑减仓", round(ind.MA20, 2)))
		checklist = append(checklist, fmt.Sprintf("压力位 %.2f 附近注意逢高兑现", round(ind.Resistance, 2)))
	case "观望":
		checklist = append(checklist, "等待方向选择，突破压力位或回踩支撑位后再决策")
		checklist = append(checklist, fmt.Sprintf("关键区间：支撑 %.2f / 压力 %.2f", round(ind.Support, 2), round(ind.Resistance, 2)))
	case "减仓考虑":
		checklist = append(checklist, "逢反弹逐步降低仓位，避免一次性清仓")
		checklist = append(checklist, fmt.Sprintf("若快速收复 %.2f（20 日线）可暂缓减仓", round(ind.MA20, 2)))
	default:
		checklist = append(checklist, "回避观望，等待趋势重建后再关注")
		checklist = append(checklist, fmt.Sprintf("重新评估条件：站稳 %.2f 且 MACD 回到零轴上方", round(ind.MA60, 2)))
	}
	checklist = append(checklist, "结合大盘情绪与个股消息面交叉验证，本评分仅为技术面参考")
	return checklist
}

func priceChange(lines []foundation.KLine) float64 {
	if len(lines) == 0 {
		return 0
	}
	latest := lines[len(lines)-1]
	return changePercentOf(latest)
}

func clamp(value, minValue, maxValue float64) float64 {
	return math.Max(minValue, math.Min(maxValue, value))
}
