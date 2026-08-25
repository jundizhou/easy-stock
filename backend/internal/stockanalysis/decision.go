package stockanalysis

import (
	"fmt"
	"math"
	"strings"

	"easy-stock/backend/internal/foundation"
)

// BenchmarkForSymbol returns a broad-market benchmark suited to the stock's board.
// It is deliberately board-based instead of attempting to infer an index constituent.
func BenchmarkForSymbol(symbol string) (string, string) {
	code := strings.Split(strings.ToUpper(strings.TrimSpace(symbol)), ".")[0]
	switch {
	case strings.HasSuffix(symbol, ".BJ") || strings.HasPrefix(code, "4") || strings.HasPrefix(code, "8"):
		return "899050.BJ", "北证50"
	case strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301"):
		return "399006.SZ", "创业板指"
	case strings.HasPrefix(code, "688"):
		return "000688.SH", "科创50"
	case strings.HasSuffix(symbol, ".SZ"):
		return "399001.SZ", "深证成指"
	default:
		return "000001.SH", "上证指数"
	}
}

func analyzeTimeframes(lines []foundation.KLine) []TimeframeAnalysis {
	type config struct {
		key    string
		label  string
		window int
	}
	configs := []config{
		{key: "ultra_short", label: "超短", window: 5},
		{key: "short", label: "短线", window: 10},
		{key: "swing", label: "波段", window: 20},
		{key: "medium", label: "中期", window: 60},
		{key: "long", label: "长期", window: 120},
	}
	last := lines[len(lines)-1].Close
	out := make([]TimeframeAnalysis, 0, len(configs))
	for _, item := range configs {
		series := movingAverageSeries(lines, item.window)
		ma := lastValue(series)
		lookback := max(item.window/3, 3)
		slope := seriesSlopePercent(series, lookback)
		periodReturn := windowReturn(closesOf(lines), item.window)
		high, low := rangeHighLow(lines, item.window)
		position := 50.0
		if high > low {
			position = clamp((last-low)/(high-low)*100, 0, 100)
		}
		score := 50.0
		if ma > 0 && last >= ma {
			score += 15
		} else if ma > 0 {
			score -= 15
		}
		switch {
		case slope >= 2:
			score += 15
		case slope > 0:
			score += 8
		case slope <= -2:
			score -= 15
		case slope < 0:
			score -= 8
		}
		switch {
		case periodReturn >= 8:
			score += 12
		case periodReturn > 0:
			score += 6
		case periodReturn <= -8:
			score -= 12
		case periodReturn < 0:
			score -= 6
		}
		if position >= 85 {
			score += 8
		} else if position <= 20 {
			score -= 6
		}
		score = clamp(score, 0, 100)
		state := "震荡"
		switch {
		case score >= 72:
			state = "强势上行"
		case score >= 58:
			state = "偏强"
		case score < 28:
			state = "弱势下行"
		case score < 43:
			state = "偏弱"
		}
		out = append(out, TimeframeAnalysis{
			Key: item.key, Label: item.label, Window: item.window,
			Score: int(math.Round(score)), State: state, Return: round2(periodReturn),
			MA: round2(ma), Slope: round2(slope), AboveMA: ma > 0 && last >= ma,
			Support: round2(firstPositive(ma, low)), Resistance: round2(high),
		})
	}
	return out
}

func analyzeRelativeStrength(input Input, lines []foundation.KLine) RelativeStrength {
	benchmark := normalizeKLines(input.BenchmarkKLines)
	result := RelativeStrength{
		BenchmarkSymbol: input.BenchmarkSymbol,
		BenchmarkName:   input.BenchmarkName,
		State:           "数据不足",
		Detail:          "未取得可比基准数据",
	}
	if len(benchmark) < 20 {
		return result
	}
	stockCloses := closesOf(lines)
	benchmarkCloses := closesOf(benchmark)
	result.Available = true
	result.StockReturn20 = round2(windowReturn(stockCloses, 20))
	result.StockReturn60 = round2(windowReturn(stockCloses, 60))
	result.BenchmarkReturn20 = round2(windowReturn(benchmarkCloses, 20))
	result.BenchmarkReturn60 = round2(windowReturn(benchmarkCloses, 60))
	result.ExcessReturn20 = round2(result.StockReturn20 - result.BenchmarkReturn20)
	result.ExcessReturn60 = round2(result.StockReturn60 - result.BenchmarkReturn60)
	score := clamp(50+result.ExcessReturn20*1.8+result.ExcessReturn60*0.8, 0, 100)
	if result.StockReturn20 > 0 && result.StockReturn60 > 0 {
		score += 5
	}
	result.Score = int(math.Round(clamp(score, 0, 100)))
	switch {
	case result.Score >= 72:
		result.State = "持续跑赢"
	case result.Score >= 58:
		result.State = "相对偏强"
	case result.Score < 30:
		result.State = "持续跑输"
	case result.Score < 43:
		result.State = "相对偏弱"
	default:
		result.State = "同步震荡"
	}
	result.Detail = fmt.Sprintf("20日超额%+.1f%%，60日超额%+.1f%%", result.ExcessReturn20, result.ExcessReturn60)
	return result
}

func buildRiskControl(profile Profile, trend TrendAnalysis, short ShortTermAnalysis, market *MarketContext) RiskControl {
	entry := trend.LatestClose
	atr := clamp(trend.ATR14Percent, 1.5, 10)
	bufferPercent := clamp(atr*0.18, 0.5, 1.8)
	structuralSupport := firstPositive(trend.Support, trend.MA20, trend.MA60)
	stop := structuralSupport * (1 - bufferPercent/100)
	if stop <= 0 || stop >= entry {
		stop = entry * (1 - clamp(atr*0.8, 2, 8)/100)
	}
	riskPerShare := math.Max(entry-stop, entry*0.01)
	firstTarget := entry + riskPerShare
	if trend.Resistance > entry && trend.Resistance < entry+riskPerShare*1.6 {
		firstTarget = trend.Resistance
	}
	secondTarget := entry + riskPerShare*2
	if trend.Resistance > firstTarget && trend.Resistance <= entry+riskPerShare*3 {
		secondTarget = trend.Resistance
	}

	riskScore := 18 + atr*6
	if trend.RangePosition60 >= 90 {
		riskScore += 10
	}
	if trend.VolumeRatio >= 1.6 && trend.Return20 < 4 {
		riskScore += 10
	}
	if profile.PrimaryType == "weak_risk" {
		riskScore += 25
	}
	if profile.PrimaryType == "emotion_leader" || short.MaxLimitStreak20 >= 2 {
		riskScore += 10
	}
	if market != nil && (market.Phase == "退潮" || market.Phase == "冰点") {
		riskScore += 15
	}
	riskScore = clamp(riskScore, 0, 100)
	level := "中等"
	singleTradeRisk := 0.8
	positionMin, positionMax := 10, 25
	switch profile.PrimaryType {
	case "trend_capacity":
		positionMin, positionMax = 20, 45
	case "trend_growth":
		positionMin, positionMax = 15, 35
	case "emotion_leader":
		positionMin, positionMax = 10, 25
	case "weak_risk":
		positionMin, positionMax = 0, 10
	}
	if riskScore >= 70 {
		level = "高"
		singleTradeRisk = 0.5
		positionMax = min(positionMax, 15)
	} else if riskScore < 38 {
		level = "较低"
		singleTradeRisk = 1.0
	}
	if market != nil && (market.Phase == "退潮" || market.Phase == "冰点") {
		positionMin = max(positionMin-5, 0)
		positionMax = max(positionMax-10, positionMin)
	}
	stopPercent := math.Abs(percentChange(entry, stop))
	rules := []string{
		fmt.Sprintf("计划止损参考%.2f，触发后以执行纪律为先，不用盘中想象替代条件", stop),
		fmt.Sprintf("单笔账户风险建议不超过%.1f%%，再由止损距离反推股数", singleTradeRisk),
		"未达到入场条件时不预支仓位；首次确认后仍保留加仓空间",
		"盈利达到1R后优先处理本金风险，趋势延续部分再跟随结构移动保护位",
		"禁止在失效位下方补仓摊薄成本",
	}
	return RiskControl{
		Level: level, Score: int(math.Round(riskScore)), EntryReference: round2(entry),
		StopPrice: round2(stop), StopPercent: round2(stopPercent),
		TakeProfitFirst: round2(firstTarget), TakeProfitSecond: round2(secondTarget),
		RiskReward:           round2(divide(secondTarget-entry, riskPerShare)),
		SuggestedPositionMin: positionMin, SuggestedPositionMax: positionMax,
		SingleTradeRisk: singleTradeRisk,
		PositionFormula: "可买股数 = min(账户允许亏损 ÷ 每股止损距离, 仓位上限金额 ÷ 计划买入价)，向下取整到100股",
		Rules:           rules,
	}
}

func buildNextDayPlan(lines []foundation.KLine, profile Profile, trend TrendAnalysis, short ShortTermAnalysis, theme ThemeAnalysis, market *MarketContext, relative RelativeStrength, risk RiskControl) NextDayPlan {
	latest := lines[len(lines)-1]
	marketScore := 50.0
	if market != nil {
		marketScore = market.Score
	}
	relativeScore := 50.0
	if relative.Available {
		relativeScore = float64(relative.Score)
	}
	themeScore := 50.0
	if theme.Resonance.Available {
		themeScore = float64(theme.Resonance.Score)
	}
	score := float64(trend.Score)*0.45 + relativeScore*0.2 + marketScore*0.2
	if theme.Resonance.Available {
		score += themeScore * .15
	} else {
		// Redistribute the missing theme weight instead of injecting a fake
		// neutral resonance score.
		score = float64(trend.Score)*.52 + relativeScore*.24 + marketScore*.24
	}
	switch short.State {
	case "修复", "启动", "发酵":
		score += 5
	case "退潮":
		score -= 10
	case "加速":
		score -= 3
	}
	score = clamp(score, 0, 100)
	bias := "中性等待"
	if score >= 72 {
		bias = "偏强延续"
	} else if score >= 58 {
		bias = "谨慎偏强"
	} else if score < 32 {
		bias = "防守回避"
	} else if score < 44 {
		bias = "谨慎偏弱"
	}
	if profile.PrimaryType == "weak_risk" {
		score = math.Min(score, 42)
		bias = "防守观察"
	}
	rangePercent := clamp(trend.ATR14Percent*0.7, 1.5, 8)
	expectedLow := trend.LatestClose * (1 - rangePercent/100)
	expectedHigh := trend.LatestClose * (1 + rangePercent/100)
	confirmation := math.Max(latest.High, trend.MA20)
	if profile.PrimaryType == "weak_risk" {
		confirmation = math.Max(confirmation, trend.MA60)
	}
	if confirmation <= 0 {
		confirmation = trend.LatestClose * 1.01
	}
	defense := firstPositive(trend.Support, risk.StopPrice)
	pressure := trend.Resistance
	if pressure <= trend.LatestClose {
		pressure = expectedHigh
	}
	expectation := fmt.Sprintf("基础预期为%s，先观察%.2f附近承接，再看能否有效站上%.2f；预估正常波动区间%.2f—%.2f。", bias, defense, confirmation, expectedLow, expectedHigh)
	if profile.PrimaryType == "emotion_leader" {
		expectation = fmt.Sprintf("基础预期为%s，核心不是开盘涨幅，而是分歧后的回封与板块梯队反馈；正常波动区间参考%.2f—%.2f。", bias, expectedLow, expectedHigh)
	}
	scenarios := []NextDayScenario{
		{Key: "strong_open", Name: "高开强承接", Priority: "次优", Trigger: fmt.Sprintf("高开后不快速跌回%.2f下方", confirmation), Confirmation: "首轮分歧缩量、回踩有承接，题材核心没有同步转弱", Action: "不追第一波，等待回踩确认后按计划仓验证", Invalidation: "高开低走并放量跌破开盘低点"},
		{Key: "flat_confirm", Name: "平开结构确认", Priority: "优先", Trigger: fmt.Sprintf("围绕%.2f震荡后主动站上%.2f", trend.LatestClose, confirmation), Confirmation: "成交额温和放大，分时高点和低点同步抬升", Action: "满足结构与量能双确认后分批介入", Invalidation: fmt.Sprintf("跌破%.2f且反抽无力", defense)},
		{Key: "weak_repair", Name: "低开修复", Priority: "观察", Trigger: fmt.Sprintf("低开测试%.2f附近但未形成放量破位", defense), Confirmation: "快速收回昨收或开盘价，板块与指数不继续下杀", Action: "只做右侧收回确认，不在下跌途中猜底", Invalidation: fmt.Sprintf("有效跌破%.2f", risk.StopPrice)},
		{Key: "breakdown", Name: "破位失效", Priority: "回避", Trigger: fmt.Sprintf("放量跌破%.2f或中期趋势同步转弱", risk.StopPrice), Confirmation: "反抽不能收回关键位，弱于题材与基准指数", Action: "取消买入计划；已有仓位执行减仓或止损", Invalidation: "重新站回失效位并形成新的量价结构后再评估"},
	}
	if profile.PrimaryType == "weak_risk" {
		scenarios[0].Priority = "观察"
		scenarios[0].Action = "只记录修复强度，不把单日高开视为趋势反转，不新增交易仓位"
		scenarios[1].Priority = "观察"
		scenarios[1].Action = "等待中期均线转平并重新站稳后再建立新计划，当前不新增仓位"
		scenarios[2].Action = "不抢反弹；已有仓位依据失效位和反抽力度纪律处理"
	}
	return NextDayPlan{
		Bias: bias, Score: int(math.Round(score)), Expectation: expectation,
		ExpectedLow: round2(expectedLow), ExpectedHigh: round2(expectedHigh),
		Levels: []PriceLevel{
			{Label: "承接观察", Price: round2(defense), Detail: "支撑附近只观察是否止跌，不直接等同买点"},
			{Label: "转强确认", Price: round2(confirmation), Detail: "价格、量能和相对强度需要共同确认"},
			{Label: "阶段压力", Price: round2(pressure), Detail: "临近压力位关注放量推进还是冲高回落"},
			{Label: "计划失效", Price: risk.StopPrice, Detail: "失守后执行风控，不延后解释"},
		},
		Scenarios:      scenarios,
		PreOpenChecks:  []string{"核对市场情绪阶段是否恶化", "确认题材核心与同梯队个股是否出现集体负反馈", "检查公告、停复牌和突发消息，避免仅依赖历史K线"},
		OpeningChecks:  []string{"开盘位置相对昨收、支撑与确认位处于哪个区间", "前15分钟成交额是否异常放大或明显缩量", "个股相对题材和基准指数是主动还是被动跟随"},
		IntradayChecks: []string{"上涨是否放量、回调是否缩量", "分时低点是否抬高，关键位跌破后能否快速收回", "冲击压力位时板块是否同步扩散"},
		CloseChecks:    []string{"收盘是否站稳短中期趋势线", "尾盘资金选择进攻还是兑现", "将新高、新低、成交额和相对强度写回次日计划"},
	}
}

func buildSignals(trend TrendAnalysis, short ShortTermAnalysis, theme ThemeAnalysis, market *MarketContext, relative RelativeStrength, risk RiskControl, timeframes []TimeframeAnalysis, fundamental *FundamentalAnalysis, research *ResearchAnalysis) []Signal {
	signals := make([]Signal, 0, 10)
	trendTone := "neutral"
	if trend.Score >= 65 {
		trendTone = "positive"
	} else if trend.Score < 40 {
		trendTone = "negative"
	}
	signals = append(signals, Signal{Key: "trend", Label: "趋势结构", Tone: trendTone, Strength: trend.Score, Detail: trend.Strength + " · " + trend.Phase})
	momentumScore := momentumScore(trend, short)
	momentumTone := scoreTone(momentumScore)
	signals = append(signals, Signal{Key: "momentum", Label: "价格动能", Tone: momentumTone, Strength: momentumScore, Detail: fmt.Sprintf("5日%+.1f%%，20日%+.1f%%", short.RecentReturn5, trend.Return20)})
	volumeScore := volumeScore(trend)
	signals = append(signals, Signal{Key: "volume", Label: "量价配合", Tone: scoreTone(volumeScore), Strength: volumeScore, Detail: fmt.Sprintf("5/20日量比%.2f，ATR%.1f%%", trend.VolumeRatio, trend.ATR14Percent)})
	if relative.Available {
		signals = append(signals, Signal{Key: "relative", Label: "相对强度", Tone: scoreTone(relative.Score), Strength: relative.Score, Detail: relative.Detail})
	}
	if theme.Resonance.Available {
		signals = append(signals, Signal{Key: "theme", Label: "题材共振", Tone: scoreTone(theme.Resonance.Score), Strength: theme.Resonance.Score, Detail: theme.Resonance.Detail})
	}
	if market != nil {
		marketScore := int(math.Round(market.Score))
		marketDetail := market.Phase + " · " + market.Confidence
		signals = append(signals, Signal{Key: "market", Label: "市场环境", Tone: scoreTone(marketScore), Strength: marketScore, Detail: marketDetail})
	}
	consistent := timeframeConsistency(timeframes)
	signals = append(signals, Signal{Key: "timeframe", Label: "周期一致性", Tone: scoreTone(consistent), Strength: consistent, Detail: timeframeSummary(timeframes)})
	if fundamental != nil && fundamental.Available {
		signals = append(signals, Signal{Key: "fundamental", Label: "基本面", Tone: scoreTone(fundamental.Score), Strength: fundamental.Score, Detail: fundamental.Summary})
	}
	if research != nil && research.Available {
		signals = append(signals, Signal{Key: "research", Label: "机构研报", Tone: scoreTone(research.Score), Strength: research.Score, Detail: research.Summary})
	}
	signals = append(signals, Signal{Key: "risk", Label: "风险约束", Tone: scoreTone(100 - risk.Score), Strength: 100 - risk.Score, Detail: fmt.Sprintf("%s风险 · 止损距离%.1f%%", risk.Level, risk.StopPercent)})
	return signals
}

func buildScorecard(profile Profile, signals []Signal) Scorecard {
	type scoreGroup struct {
		weight  float64
		members map[string]float64
	}
	groups := []scoreGroup{
		{weight: .30, members: map[string]float64{"trend": .70, "timeframe": .30}},
		{weight: .15, members: map[string]float64{"momentum": .55, "volume": .45}},
		{weight: .15, members: map[string]float64{"relative": 1}},
		{weight: .15, members: map[string]float64{"theme": .60, "market": .40}},
		{weight: .15, members: map[string]float64{"fundamental": .75, "research": .25}},
		{weight: .10, members: map[string]float64{"risk": 1}},
	}
	if profile.PrimaryType == "emotion_leader" {
		groups[0].weight, groups[1].weight, groups[2].weight, groups[3].weight, groups[4].weight, groups[5].weight = .25, .25, .10, .25, .05, .10
	} else if profile.PrimaryType == "weak_risk" {
		groups[0].weight, groups[1].weight, groups[2].weight, groups[3].weight, groups[4].weight, groups[5].weight = .35, .10, .15, .10, .15, .15
	}
	signalByKey := make(map[string]Signal, len(signals))
	for _, signal := range signals {
		signalByKey[signal.Key] = signal
	}
	type activeGroup struct {
		definition scoreGroup
		score      float64
		members    map[string]float64
	}
	active := make([]activeGroup, 0, len(groups))
	activeWeight := 0.0
	for _, group := range groups {
		memberTotal, scoreTotal := 0.0, 0.0
		available := map[string]float64{}
		for key, memberWeight := range group.members {
			if signal, ok := signalByKey[key]; ok {
				available[key] = memberWeight
				memberTotal += memberWeight
				scoreTotal += float64(signal.Strength) * memberWeight
			}
		}
		if memberTotal == 0 {
			continue
		}
		active = append(active, activeGroup{definition: group, score: scoreTotal / memberTotal, members: available})
		activeWeight += group.weight
	}
	dimensions := make([]DimensionScore, 0, len(signals))
	positive := make([]string, 0, 5)
	negative := make([]string, 0, 5)
	effectiveWeights := map[string]float64{}
	baseScore := 0.0
	positiveGroups := 0
	var structureScore, riskResilience float64
	for _, signal := range signals {
		if signal.Tone == "positive" {
			positive = append(positive, signal.Label+"："+signal.Detail)
		} else if signal.Tone == "negative" {
			negative = append(negative, signal.Label+"："+signal.Detail)
		}
	}
	for groupIndex, group := range active {
		groupWeight := group.definition.weight / activeWeight
		baseScore += group.score * groupWeight
		if group.score >= 65 {
			positiveGroups++
		}
		if groupIndex == 0 {
			structureScore = group.score
		}
		for key, memberWeight := range group.members {
			memberTotal := 0.0
			for _, weight := range group.members {
				memberTotal += weight
			}
			effectiveWeights[key] = groupWeight * memberWeight / memberTotal
		}
		if _, ok := group.members["risk"]; ok {
			riskResilience = group.score
		}
	}
	for _, signal := range signals {
		if weight := effectiveWeights[signal.Key]; weight > 0 {
			dimensions = append(dimensions, DimensionScore{Key: signal.Key, Label: signal.Label, Score: signal.Strength, Weight: weight, Status: scoreStatus(signal.Strength), Detail: signal.Detail})
		}
	}
	overall := calibratedStockScore(baseScore, positiveGroups)
	if structureScore < 30 && riskResilience <= 25 {
		overall = min(overall, 49)
	}
	direction := "中性"
	grade := "C"
	switch {
	case overall >= 85:
		direction, grade = "偏多", "A"
	case overall >= 75:
		direction, grade = "谨慎偏多", "B+"
	case overall >= 65:
		direction, grade = "略偏多", "B"
	case overall < 45:
		direction, grade = "偏空", "E"
	case overall < 55:
		direction, grade = "谨慎偏空", "D"
	}
	conviction := "中等"
	if profile.Confidence >= .76 && activeWeight >= .75 && positiveGroups >= 3 {
		conviction = "较高"
	} else if profile.Confidence < .58 || activeWeight < .60 || len(positive) == len(negative) {
		conviction = "较低"
	}
	if len(positive) == 0 {
		positive = []string{"暂未形成明确的多头共振，优先等待右侧确认"}
	}
	if len(negative) == 0 {
		negative = []string{"暂无突出负面共振，但仍必须执行结构失效条件"}
	}
	return Scorecard{AlgorithmVersion: "stock-score-v2", Overall: overall, Grade: grade, Direction: direction, Conviction: conviction, Dimensions: dimensions, PositiveSignals: uniqueStrings(positive, 5), NegativeSignals: uniqueStrings(negative, 5)}
}

func calibratedStockScore(base float64, positiveGroups int) int {
	bonus := 0.0
	if positiveGroups >= 3 {
		bonus += 2
	}
	if positiveGroups >= 5 {
		bonus += 2
	}
	return int(math.Round(clamp(58+.82*(base-50)+bonus, 20, 95)))
}

func closesOf(lines []foundation.KLine) []float64 {
	values := make([]float64, len(lines))
	for index, line := range lines {
		values[index] = line.Close
	}
	return values
}

func momentumScore(trend TrendAnalysis, short ShortTermAnalysis) int {
	score := 50 + short.RecentReturn5*2 + short.RecentReturn10 + trend.Return20*0.5
	if short.State == "修复" || short.State == "启动" || short.State == "发酵" {
		score += 8
	}
	if short.State == "退潮" {
		score -= 15
	}
	return int(math.Round(clamp(score, 0, 100)))
}

func volumeScore(trend TrendAnalysis) int {
	score := 50.0
	switch {
	case trend.VolumeRatio >= 1.1 && trend.VolumeRatio <= 1.8 && trend.Return20 > 0:
		score += 22
	case trend.VolumeRatio >= 1.8 && trend.Return20 < 4:
		score -= 15
	case trend.VolumeRatio < .65:
		score -= 8
	case trend.VolumeRatio >= .8:
		score += 8
	}
	if trend.ATR14Percent >= 6 {
		score -= 8
	}
	return int(math.Round(clamp(score, 0, 100)))
}

func timeframeConsistency(items []TimeframeAnalysis) int {
	if len(items) == 0 {
		return 50
	}
	total := 0
	for _, item := range items {
		total += item.Score
	}
	averageScore := float64(total) / float64(len(items))
	spread := 0.0
	for _, item := range items {
		spread += math.Abs(float64(item.Score) - averageScore)
	}
	spread /= float64(len(items))
	return int(math.Round(clamp(averageScore-spread*.45, 0, 100)))
}

func timeframeSummary(items []TimeframeAnalysis) string {
	strong, weak := make([]string, 0), make([]string, 0)
	for _, item := range items {
		if item.Score >= 58 {
			strong = append(strong, item.Label)
		} else if item.Score < 43 {
			weak = append(weak, item.Label)
		}
	}
	if len(strong) > 0 && len(weak) == 0 {
		return strings.Join(strong, "、") + "周期同步偏强"
	}
	if len(weak) > 0 && len(strong) == 0 {
		return strings.Join(weak, "、") + "周期同步偏弱"
	}
	return fmt.Sprintf("偏强周期%d个，偏弱周期%d个", len(strong), len(weak))
}

func scoreTone(score int) string {
	if score >= 60 {
		return "positive"
	}
	if score < 42 {
		return "negative"
	}
	return "neutral"
}

func scoreStatus(score int) string {
	if score >= 72 {
		return "强"
	}
	if score >= 58 {
		return "偏强"
	}
	if score < 30 {
		return "弱"
	}
	if score < 43 {
		return "偏弱"
	}
	return "中性"
}
