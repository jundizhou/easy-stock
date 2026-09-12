package tradejournal

import (
	"fmt"
	"sort"
	"time"
)

// Analyze 执行完整分析管道：解析 → FIFO 配对 → 画像统计 → 行为偏差诊断。
func Analyze(request AnalyzeRequest) (AnalyzeResult, error) {
	trades, skipped, err := parseCSV(request.CSVContent)
	if err != nil {
		return AnalyzeResult{}, err
	}
	if len(trades) == 0 {
		return AnalyzeResult{}, fmt.Errorf("未解析到有效的买卖成交记录，请确认 CSV 包含日期、代码、操作、价格、数量列")
	}
	sort.SliceStable(trades, func(i, j int) bool {
		di, erri := parseDate(trades[i].OccurDate)
		dj, errj := parseDate(trades[j].OccurDate)
		if erri != nil || errj != nil {
			return false
		}
		if !di.Equal(dj) {
			return di.Before(dj)
		}
		return false
	})
	roundTrips, openPositions, unmatched := matchFIFO(trades)
	skipped += unmatched
	if len(roundTrips) == 0 {
		return AnalyzeResult{}, fmt.Errorf("没有可配对的完整买卖回合（可能全部为单边记录）")
	}
	sort.SliceStable(roundTrips, func(i, j int) bool {
		di, _ := parseDate(roundTrips[i].CloseDate)
		dj, _ := parseDate(roundTrips[j].CloseDate)
		return di.Before(dj)
	})
	stats := buildStatistics(roundTrips, trades)
	result := AnalyzeResult{
		AlgorithmVersion: AlgorithmVersion,
		Filename:         request.Filename,
		Imported:         len(trades),
		Skipped:          skipped,
		ParsedRange:      [2]string{roundTrips[0].OpenDate, roundTrips[len(roundTrips)-1].CloseDate},
		Trades:           roundTrips,
		Statistics:       stats,
		Biases:           diagnoseBiases(roundTrips, stats),
		OpenPositions:    openPositions,
		AnalyzedAt:       time.Now().UTC(),
	}
	return result, nil
}

func buildStatistics(trips []RoundTrip, raw []RawTrade) Statistics {
	stats := Statistics{TotalTrades: len(trips)}
	var cumulative, peak, maxNotional float64
	dates := map[string]struct{}{}
	var totalWin, totalLoss float64
	for i, trip := range trips {
		if _, err := parseDate(trip.CloseDate); err == nil {
			dates[trip.CloseDate] = struct{}{}
		}
		if notional := trip.OpenPrice * trip.Volume; notional > maxNotional {
			maxNotional = notional
		}
		switch trip.Outcome {
		case "win":
			stats.WinCount++
			totalWin += trip.NetProfit
		case "loss":
			stats.LossCount++
			totalLoss += -trip.NetProfit
		}
		stats.TotalNetProfit += trip.NetProfit
		cumulative += trip.NetProfit
		stats.ProfitCurve = append(stats.ProfitCurve, CurvePoint{Index: i + 1, CloseDate: trip.CloseDate, Cumulative: cumulative})
		if cumulative > peak {
			peak = cumulative
		}
		if drawdown := peak - cumulative; drawdown > stats.MaxDrawdown {
			stats.MaxDrawdown = drawdown
			// 用户初始本金不可知，用单笔最大投入资金 + 峰值盈利估算权益基数：
			// 盈利期直接用峰值，未盈利过时用最大投入资金兜底，避免回撤比例恒为 0。
			if base := peak + maxNotional; base > 0 {
				stats.MaxDrawdownPct = drawdown / base * 100
			}
		}
		stats.AvgHoldingDays += trip.HoldingDays
		if trip.HoldingDays > stats.MaxHoldingDays {
			stats.MaxHoldingDays = trip.HoldingDays
		}
		if stats.BestTrade == nil || trip.NetProfit > stats.BestTrade.NetProfit {
			best := trip
			stats.BestTrade = &best
		}
		if stats.WorstTrade == nil || trip.NetProfit < stats.WorstTrade.NetProfit {
			worst := trip
			stats.WorstTrade = &worst
		}
	}
	closed := stats.WinCount + stats.LossCount
	if closed > 0 {
		stats.WinRate = round2(float64(stats.WinCount) / float64(closed) * 100)
	}
	if stats.WinCount > 0 {
		stats.AvgWin = round2(totalWin / float64(stats.WinCount))
	}
	if stats.LossCount > 0 {
		stats.AvgLoss = round2(totalLoss / float64(stats.LossCount))
	}
	if stats.AvgLoss > 0 {
		stats.PayoffRatio = round2(stats.AvgWin / stats.AvgLoss)
	}
	if totalLoss > 0 {
		stats.ProfitFactor = round2(totalWin / totalLoss)
	}
	if len(trips) > 0 {
		stats.AvgHoldingDays = round2(stats.AvgHoldingDays / float64(len(trips)))
	}
	stats.Expectancy = round2((float64(stats.WinCount)/float64(maxInt(closed, 1)))*stats.AvgWin - (float64(stats.LossCount)/float64(maxInt(closed, 1)))*stats.AvgLoss)
	stats.ActiveDays = len(dates)
	if stats.ActiveDays >= 2 {
		var firstDate, lastDate time.Time
		for _, trip := range trips {
			if parsed, err := parseDate(trip.CloseDate); err == nil {
				if firstDate.IsZero() {
					firstDate = parsed
				}
				lastDate = parsed
			}
		}
		if !firstDate.IsZero() && lastDate.After(firstDate) {
			weeks := lastDate.Sub(firstDate).Hours() / 24 / 7
			if weeks > 0 {
				stats.TradePerWeek = round2(float64(len(trips)) / weeks)
			}
		}
	}
	_ = raw
	return stats
}

// diagnoseBiases 输出四类行为偏差诊断，阈值参考 Vibe-Trading trade-journal 模块。
func diagnoseBiases(trips []RoundTrip, stats Statistics) []Bias {
	biases := []Bias{}
	// 1. 处置效应：赢家持有时间显著短于输家（急于兑现盈利、不肯止损）。
	var winDays, lossDays float64
	var winN, lossN int
	for _, trip := range trips {
		switch trip.Outcome {
		case "win":
			winDays += trip.HoldingDays
			winN++
		case "loss":
			lossDays += trip.HoldingDays
			lossN++
		}
	}
	if winN >= 3 && lossN >= 3 {
		avgWin, avgLoss := winDays/float64(winN), lossDays/float64(lossN)
		if avgWin < avgLoss*0.65 {
			biases = append(biases, Bias{
				ID: "disposition_effect", Label: "处置效应倾向", Severity: severityFor(avgWin / avgLoss),
				Score:      round2(avgWin / avgLoss),
				Evidence:   fmt.Sprintf("盈利单平均持有 %.1f 天，亏损单平均 %.1f 天——赚小钱就跑、亏了死扛", avgWin, avgLoss),
				Suggestion: "制定统一的离场规则（止损位/移动止盈），让盈利单奔跑、亏损单及时了结",
			})
		}
	}
	// 2. 过度交易。
	if stats.TradePerWeek > 10 {
		biases = append(biases, Bias{
			ID: "overtrading", Label: "过度交易", Severity: severityThreshold(stats.TradePerWeek, 10, 20, 35),
			Score:      stats.TradePerWeek,
			Evidence:   fmt.Sprintf("每周平均 %.1f 笔完整交易，频率偏高，交易成本侵蚀利润", stats.TradePerWeek),
			Suggestion: "为每笔交易设置入场理由清单，减少无计划交易；可尝试降低一半频率对比收益变化",
		})
	}
	// 3. 追涨：买入当日或次日即大涨后买入的回合（用开仓日涨幅近似不可得，用短持有+高亏损率代理）。
	quickLoss, quickTotal := 0, 0
	for _, trip := range trips {
		if trip.HoldingDays <= 3 {
			quickTotal++
			if trip.Outcome == "loss" {
				quickLoss++
			}
		}
	}
	if quickTotal >= 5 {
		quickLossRate := float64(quickLoss) / float64(quickTotal) * 100
		if quickLossRate > 60 {
			biases = append(biases, Bias{
				ID: "chasing", Label: "追涨杀跌倾向", Severity: severityThreshold(quickLossRate, 60, 70, 85),
				Score:      round2(quickLossRate),
				Evidence:   fmt.Sprintf("3 日内的超短交易共 %d 笔，其中亏损 %d 笔（%.0f%%）——疑似情绪化追高入场", quickTotal, quickLoss, quickLossRate),
				Suggestion: "避免在异动当日追入，等回踩关键支撑或分时企稳后再决策",
			})
		}
	}
	// 4. 锚定效应：单笔大幅亏损未及时止损。
	if stats.WorstTrade != nil && stats.WorstTrade.ProfitRate < -15 {
		biases = append(biases, Bias{
			ID: "anchoring", Label: "止损缺失（锚定）", Severity: severityThreshold(-stats.WorstTrade.ProfitRate, 15, 25, 40),
			Score:      round2(-stats.WorstTrade.ProfitRate),
			Evidence:   fmt.Sprintf("最差一笔 %s 亏损 %.1f%%（持有 %.0f 天），超出常规止损范围", stats.WorstTrade.Symbol, stats.WorstTrade.ProfitRate, stats.WorstTrade.HoldingDays),
			Suggestion: "严格执行 -8%~-10% 硬止损；亏损单加仓前必须重新评估原始逻辑",
		})
	}
	return biases
}

func severityFor(ratio float64) string {
	switch {
	case ratio < 0.4:
		return "high"
	case ratio < 0.65:
		return "medium"
	default:
		return "low"
	}
}

func severityThreshold(value, medium, high, critical float64) string {
	switch {
	case value >= critical:
		return "high"
	case value >= high:
		return "medium"
	case value >= medium:
		return "low"
	default:
		return "none"
	}
}

func round2(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
