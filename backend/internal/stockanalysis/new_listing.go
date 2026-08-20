package stockanalysis

import (
	"fmt"
	"math"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// analyzeNewListing handles the first nineteen valid trading days separately
// from the mature-stock model. Its output is intentionally complete so the
// existing decision workspace remains usable, while unavailable long windows
// stay explicit instead of being represented by fabricated moving averages.
func analyzeNewListing(input Input, lines []foundation.KLine) Analysis {
	quote := input.Quote
	last := lines[len(lines)-1]
	if strings.TrimSpace(quote.Symbol) == "" {
		quote.Symbol = input.Symbol
	}
	if quote.Price <= 0 {
		quote.Price = last.Close
	}
	if quote.ChangePercent == 0 {
		if last.ChangePercent != 0 {
			quote.ChangePercent = last.ChangePercent
		} else if len(lines) >= 2 && lines[len(lines)-2].Close > 0 {
			quote.ChangePercent = percentChange(lines[len(lines)-2].Close, last.Close)
		}
	}
	name := strings.TrimSpace(quote.Name)
	if name == "" {
		name = firstNonEmpty(input.Symbol, quote.Symbol)
		quote.Name = name
	}

	trend := analyzeNewListingTrend(lines, quote)
	short := analyzeShortTerm(input.Symbol, lines, input.LimitUps)
	short.State = "价格发现中"
	short.Reasons = uniqueStrings(append([]string{
		fmt.Sprintf("上市以来仅%d个交易日，短线涨跌只作价格发现参考", len(lines)),
	}, short.Reasons...), 5)
	theme := analyzeTheme(input.Symbol, short, input.CachedThemes, input.Concepts, input.Industry, input.Themes, input.LimitUps, input.Business, input.BusinessDetail, input.BusinessSource)
	theme = enrichTheme(input, short, theme)
	theme.Route = "new_listing"
	market := marketContext(input)
	profile := Profile{
		PrimaryType: "new_listing",
		TypeLabel:   "上市初期",
		PricePhase:  trend.Phase,
		MarketRole:  firstNonEmpty(nonDefaultRole(theme.Role), "价格发现阶段"),
		Tags:        uniqueStrings(append([]string{"上市初期", trend.Phase, "数据积累中"}, theme.Primary), 5),
		Confidence:  round2(clamp(.26+float64(len(lines))*.009, .26, .43)),
	}
	fundamentalValue := analyzeFundamentals(input.Fundamentals)
	researchValue := analyzeResearch(input.Reports)
	stockNewsValue, themeNewsValue := analyzeRecentNews(input, theme)
	fundamental := &fundamentalValue
	research := &researchValue
	stockNews := &stockNewsValue
	themeNews := &themeNewsValue
	relative := RelativeStrength{
		BenchmarkSymbol: input.BenchmarkSymbol,
		BenchmarkName:   input.BenchmarkName,
		State:           "数据积累中",
		Detail:          fmt.Sprintf("上市仅%d个交易日，暂无20日同周期相对强度；不参与本次评分", len(lines)),
	}
	risk := buildNewListingRiskControl(trend, market)
	plan := buildNewListingActionPlan(profile, trend, risk, quote.Price)
	nextDay := buildNewListingNextDayPlan(trend, risk, market)
	signals := buildNewListingSignals(trend, short, theme, market, risk, fundamental, research)
	scorecard := buildNewListingScorecard(profile, signals)
	risks := buildNewListingRisks(trend, theme, market)
	conclusion := Conclusion{
		Headline: fmt.Sprintf("上市初期 · %s · 观察优先", trend.Phase),
		Summary:  fmt.Sprintf("%s当前仅有%d个交易日样本，已切换到新股价格发现模型。上市期涨跌、区间位置、换手和流动性可作观察依据，但均线、ATR和成熟趋势尚未形成。", name, len(lines)),
		Action:   plan.CurrentAction,
		BestPath: "等待更多交易日形成价格区间，并以小仓、分批和上市区间失效位验证",
		MainRisk: firstString(risks),
		Source:   "local-new-listing-engine",
	}
	evidence := buildNewListingEvidence(input, trend, short, theme, market, risk, lines, fundamental, research, stockNews, themeNews)
	quality := buildNewListingDataQuality(input, profile, lines, short, theme, market, relative, fundamental, research, stockNews, themeNews)
	return Analysis{
		Symbol: input.Symbol, Name: name, GeneratedAt: time.Now(), Quote: quote,
		Profile: profile, Conclusion: conclusion, Trend: trend, ShortTerm: short,
		Theme: theme, Fundamental: fundamental, Research: research, StockNews: stockNews,
		ThemeNews: themeNews, Market: market, Scorecard: scorecard,
		Timeframes: analyzeNewListingTimeframes(trend), Relative: relative,
		Signals: signals, NextDay: nextDay, RiskControl: risk, ActionPlan: plan,
		Risks: risks, Evidence: evidence, DataQuality: quality, Chart: newListingChart(lines),
		AI:        AISynthesisStatus{Status: "rules", Message: "当前结论由上市初期受限样本模型生成"},
		dailyBars: compactDailyBars(lines, 120),
	}
}

func analyzeNewListingTrend(lines []foundation.KLine, quote foundation.Quote) TrendAnalysis {
	last := lines[len(lines)-1]
	high, low := rangeHighLow(lines, len(lines))
	position := 50.0
	if high > low {
		position = clamp((last.Close-low)/(high-low)*100, 0, 100)
	}
	closes := closesOf(lines)
	listingReturn := windowReturn(closes, len(lines))
	if len(lines) == 1 {
		listingReturn = firstNonZero(last.ChangePercent, quote.ChangePercent)
		if listingReturn == 0 && last.Open > 0 {
			listingReturn = percentChange(last.Open, last.Close)
		}
	}
	listingDrawdown := 0.0
	if high > 0 {
		listingDrawdown = percentChange(high, last.Close)
	}
	volatility := observedListingVolatility(lines)
	amount := averageKLineAmount(lines, len(lines))
	turnover := averageListingTurnover(lines)
	score := int(math.Round(clamp(50+listingReturn*1.1+(position-50)*.12, 25, 75)))
	phase := "首日价格发现"
	if len(lines) > 1 {
		switch {
		case listingReturn >= 12 && position >= 70:
			phase = "上市初期强势"
		case listingReturn <= -8 || position <= 25:
			phase = "上市初期回落"
		default:
			phase = "上市初期震荡"
		}
	}
	strength := "样本有限"
	if len(lines) >= 10 {
		strength = "初步可观察"
	}
	support := low
	resistance := high
	if support <= 0 {
		support = last.Close * .9
	}
	if resistance <= 0 {
		resistance = last.Close * 1.1
	}
	return TrendAnalysis{
		Score: score, Strength: strength, Phase: phase,
		Setup:       "上市区间观察，不使用成熟均线结构",
		LatestClose: round2(last.Close), Return20: 0, Return60: 0, Return120: 0,
		RangePosition60: 0, DrawdownFromHigh120: 0, VolumeRatio: 0, ATR14Percent: 0,
		Support: round2(support), Resistance: round2(resistance),
		Invalidation: fmt.Sprintf("有效跌破上市以来低点 %.2f，或单日波动超出可承受范围", support),
		Reasons: uniqueStrings([]string{
			fmt.Sprintf("上市%d个交易日，上市期收益%+.1f%%", len(lines), listingReturn),
			fmt.Sprintf("上市区间 %.2f—%.2f，当前位于区间%.0f%%", low, high, position),
			fmt.Sprintf("可观测日内波动约%.1f%%，尚不足以计算成熟ATR14", volatility),
		}, 5),
		HistoryDays: len(lines), ListingReturn: round2(listingReturn), ListingHigh: round2(high),
		ListingLow: round2(low), ListingRangePosition: round2(position), ListingDrawdown: round2(listingDrawdown),
		AverageTurnover: round2(turnover), AverageAmount: round2(amount), ObservedVolatility: round2(volatility),
	}
}

func observedListingVolatility(lines []foundation.KLine) float64 {
	values := make([]float64, 0, len(lines))
	for _, line := range lines {
		base := firstPositive(line.Open, line.Close)
		if base > 0 && line.High >= line.Low {
			values = append(values, (line.High-line.Low)/base*100)
		}
	}
	return average(values)
}

func averageListingTurnover(lines []foundation.KLine) float64 {
	values := make([]float64, 0, len(lines))
	for _, line := range lines {
		if line.TurnoverRate > 0 {
			values = append(values, line.TurnoverRate)
		}
	}
	return average(values)
}

func newListingChart(lines []foundation.KLine) []TrendPoint {
	points := make([]TrendPoint, 0, len(lines))
	for _, line := range lines {
		points = append(points, TrendPoint{Date: line.Time.Format("2006-01-02"), Close: round2(line.Close)})
	}
	return points
}

func analyzeNewListingTimeframes(trend TrendAnalysis) []TimeframeAnalysis {
	missing20 := max(20-trend.HistoryDays, 1)
	return []TimeframeAnalysis{
		{Key: "listing_period", Label: "上市期", Window: trend.HistoryDays, Score: trend.Score, State: "价格发现样本", Return: trend.ListingReturn, Support: trend.ListingLow, Resistance: trend.ListingHigh},
		{Key: "ma20_pending", Label: "20日", Window: 20, State: fmt.Sprintf("还需%d个交易日", missing20)},
		{Key: "ma60_pending", Label: "60日", Window: 60, State: "数据积累中"},
		{Key: "ma120_pending", Label: "120日", Window: 120, State: "数据积累中"},
	}
}

func buildNewListingRiskControl(trend TrendAnalysis, market *MarketContext) RiskControl {
	entry := firstPositive(trend.LatestClose, trend.ListingHigh)
	volatility := clamp(trend.ObservedVolatility, 4, 15)
	stop := entry * (1 - clamp(volatility*1.15, 6, 15)/100)
	if trend.ListingLow > 0 && trend.ListingLow < entry {
		stop = math.Max(stop, trend.ListingLow*.98)
	}
	if stop <= 0 || stop >= entry {
		stop = entry * .88
	}
	riskPerShare := math.Max(entry-stop, entry*.01)
	firstTarget := entry + riskPerShare
	secondTarget := entry + riskPerShare*2
	riskScore := clamp(78+volatility*.9, 78, 96)
	if market != nil && (market.Phase == "退潮" || market.Phase == "冰点") {
		riskScore = math.Min(riskScore+5, 100)
	}
	positionMax := 5
	if trend.HistoryDays >= 10 {
		positionMax = 10
	}
	return RiskControl{
		Level: "高", Score: int(math.Round(riskScore)), EntryReference: round2(entry), StopPrice: round2(stop),
		StopPercent: round2(math.Abs(percentChange(entry, stop))), TakeProfitFirst: round2(firstTarget), TakeProfitSecond: round2(secondTarget),
		RiskReward: round2(divide(secondTarget-entry, riskPerShare)), SuggestedPositionMin: 0, SuggestedPositionMax: positionMax,
		SingleTradeRisk: .3, PositionFormula: "新股仅建议观察仓：账户允许亏损 ÷ 上市期风控距离，再受5%—10%仓位上限约束，向下取整到100股",
		Rules: []string{"上市初期优先观察，不因首日或连续脉冲追涨", "跌破上市区间失效位停止新增仓位", "单笔账户风险建议不超过0.3%，确认样本积累后再重新评估", "禁止在失效位下方补仓摊薄成本"},
	}
}

func buildNewListingActionPlan(profile Profile, trend TrendAnalysis, risk RiskControl, current float64) ActionPlan {
	entryLow := round2(math.Max(risk.StopPrice*1.02, current*(1-clamp(trend.ObservedVolatility/2, 1.5, 4)/100)))
	entryHigh := round2(math.Max(current, entryLow+.01))
	holdLow := round2(math.Max(risk.StopPrice*1.03, current*(1-clamp(trend.ObservedVolatility/3, 1, 3)/100)))
	holdHigh := round2(math.Max(current*1.02, holdLow+.01))
	firstTarget := round2(math.Max(risk.TakeProfitFirst, current*1.01))
	secondTarget := round2(math.Max(risk.TakeProfitSecond, firstTarget+.01))
	return ActionPlan{
		DecisionMode: "non_short", DecisionLabel: "新股价格发现", DecisionConfidence: profile.Confidence, Horizon: "观察期 / 数据积累",
		Rationale:     "上市交易日不足20日，价格计划只用于小仓验证和风险边界，不代表成熟趋势买点。",
		PricingSource: "local-rules", CurrentAction: "观察优先，等待价格区间形成",
		Entry:           ActionPriceZone{Label: "观察仓参考价格", PriceLow: entryLow, PriceHigh: entryHigh, PriceText: formatActionPriceRange(entryLow, entryHigh), Reason: "只在上市区间内出现缩量承接、且非首日脉冲时考虑观察仓。", Action: "满足基本面/题材与承接条件后，最多使用观察仓，不追首日或高波动拉升。"},
		Hold:            ActionPriceZone{Label: "持有价格", PriceLow: holdLow, PriceHigh: holdHigh, PriceText: formatActionPriceRange(holdLow, holdHigh), Reason: "持有前提是价格没有有效跌破上市期低点，且波动逐步收敛。", Action: "数据未积累前不扩大仓位；跌破失效位或波动失控时退出。"},
		TakeProfit:      ActionPriceZone{Label: "止盈价格", PriceLow: firstTarget, PriceHigh: secondTarget, PriceText: formatActionPriceRange(firstTarget, secondTarget), Reason: "目标仅按受限样本的风险距离计算，不把上市初期上涨外推为趋势。", Action: "到达第一目标分批兑现，不因短期强势继续追高。"},
		StopLoss:        ActionPriceZone{Label: "止损价格", PriceLow: 0, PriceHigh: risk.StopPrice, PriceText: fmt.Sprintf("≤ %.2f 元", risk.StopPrice), Reason: "上市区间失效位下方的保护性参考，跌破说明原观察假设失效。", Action: "触发后停止新增仓位，已有仓位按纪律减仓或止损。"},
		EntryConditions: []string{fmt.Sprintf("价格在上市区间 %.2f—%.2f 内出现承接", trend.ListingLow, trend.ListingHigh), "成交和换手不过度失控", "不在首日脉冲或连续加速时追入"},
		HoldConditions:  []string{"收盘不有效跌破上市期低点", "价格波动逐步收敛并继续积累交易日"},
		AvoidConditions: []string{"首日或高开加速时追涨", "跌破上市期低点后补仓", "在基本面或公告资料缺失时重仓"},
		Invalidation:    trend.Invalidation, PositionHint: fmt.Sprintf("仅观察仓，建议仓位0%%—%d%%；至少积累20个交易日后再切换成熟模型", risk.SuggestedPositionMax),
	}
}

func buildNewListingNextDayPlan(trend TrendAnalysis, risk RiskControl, market *MarketContext) NextDayPlan {
	volatility := clamp(trend.ObservedVolatility*.7, 3, 12)
	low := round2(trend.LatestClose * (1 - volatility/100))
	high := round2(trend.LatestClose * (1 + volatility/100))
	confirmation := round2(math.Max(trend.LatestClose*1.02, trend.ListingHigh))
	if confirmation <= trend.LatestClose {
		confirmation = round2(trend.LatestClose * 1.02)
	}
	bias := "观察价格发现"
	if market != nil && (market.Phase == "退潮" || market.Phase == "冰点") {
		bias = "防守观察"
	}
	return NextDayPlan{
		Bias: bias, Score: 40, Expectation: fmt.Sprintf("上市初期只观察%.2f—%.2f的正常波动和承接，不以单日涨跌推断趋势；有效站上%.2f且波动收敛才提高关注。", low, high, confirmation), ExpectedLow: low, ExpectedHigh: high,
		Levels: []PriceLevel{{Label: "上市区间低点", Price: trend.ListingLow, Detail: "失守后观察假设失效"}, {Label: "承接观察", Price: risk.EntryReference, Detail: "只作观察锚点，不等同买点"}, {Label: "价格发现确认", Price: confirmation, Detail: "需要收盘和换手共同确认"}, {Label: "计划失效", Price: risk.StopPrice, Detail: "触发后执行风控"}},
		Scenarios: []NextDayScenario{
			{Key: "range_hold", Name: "区间承接", Priority: "观察", Trigger: fmt.Sprintf("价格守住%.2f附近", trend.ListingLow), Confirmation: "波动收敛且成交不过度放大", Action: "继续观察，不扩大仓位", Invalidation: fmt.Sprintf("有效跌破%.2f", risk.StopPrice)},
			{Key: "discovery_breakout", Name: "价格发现上行", Priority: "次优", Trigger: fmt.Sprintf("放量站上%.2f", confirmation), Confirmation: "收盘未回落且换手可控", Action: "最多以观察仓分批验证，不追第一波", Invalidation: "冲高回落并跌回上市区间"},
			{Key: "volatile", Name: "高波动分歧", Priority: "回避", Trigger: fmt.Sprintf("日内振幅超过%.1f%%", volatility*2), Confirmation: "高低点快速扩张", Action: "不新增仓位，等待波动收敛", Invalidation: "波动恢复到可承受范围"},
			{Key: "breakdown", Name: "上市区间失效", Priority: "回避", Trigger: fmt.Sprintf("跌破%.2f", risk.StopPrice), Confirmation: "反抽不能收回", Action: "取消新增计划并执行风控", Invalidation: "重新形成更高低点后再评估"},
		},
		PreOpenChecks:  []string{"核对上市公告、临时停牌和监管风险", "确认市场情绪是否恶化", "不把首日涨跌外推成趋势"},
		OpeningChecks:  []string{"观察相对上市区间的位置", "观察开盘后波动是否失控", "确认成交和换手没有异常放大"},
		IntradayChecks: []string{"高低点是否快速扩张", "承接是否逐步抬高", "价格是否跌破上市期低点"},
		CloseChecks:    []string{"收盘是否留在上市区间内", "记录新高、新低和振幅", "累计交易日是否接近20日模型门槛"},
	}
}

func buildNewListingSignals(trend TrendAnalysis, short ShortTermAnalysis, theme ThemeAnalysis, market *MarketContext, risk RiskControl, fundamental *FundamentalAnalysis, research *ResearchAnalysis) []Signal {
	listingScore := trend.Score
	liquidityScore := 45
	if trend.AverageAmount >= 500_000_000 {
		liquidityScore = 70
	} else if trend.AverageAmount >= 100_000_000 {
		liquidityScore = 58
	} else if trend.AverageAmount > 0 {
		liquidityScore = 35
	}
	turnoverScore := int(clamp(40+trend.AverageTurnover*3, 25, 80))
	marketScore := 50
	marketDetail := "市场情绪数据不足"
	if market != nil {
		marketScore = int(clamp(market.Score, 0, 100))
		marketDetail = market.Phase + " · " + market.Confidence
	}
	fundamentalScore := 45
	fundamentalDetail := fundamental.Summary
	if fundamental.Available {
		fundamentalScore = fundamental.Score
	}
	researchScore := 45
	if research.Available {
		researchScore = research.Score
	}
	themeScore := 45
	if theme.Resonance.Available {
		themeScore = theme.Resonance.Score
	}
	return []Signal{
		{Key: "listing_period", Label: "价格发现", Tone: scoreTone(listingScore), Strength: listingScore, Detail: fmt.Sprintf("上市%d日，上市期收益%+.1f%%，区间位置%.0f%%", trend.HistoryDays, trend.ListingReturn, trend.ListingRangePosition)},
		{Key: "liquidity", Label: "上市期流动性", Tone: scoreTone(liquidityScore), Strength: liquidityScore, Detail: fmt.Sprintf("平均成交额%.1f亿元", trend.AverageAmount/100_000_000)},
		{Key: "turnover", Label: "换手与波动", Tone: scoreTone(turnoverScore), Strength: turnoverScore, Detail: fmt.Sprintf("平均换手%.1f%%，观测波动%.1f%%", trend.AverageTurnover, trend.ObservedVolatility)},
		{Key: "theme", Label: "题材事实", Tone: scoreTone(themeScore), Strength: themeScore, Detail: firstNonEmpty(theme.Description, "暂无明确题材共振")},
		{Key: "market", Label: "市场环境", Tone: scoreTone(marketScore), Strength: marketScore, Detail: marketDetail},
		{Key: "fundamental", Label: "基本面", Tone: scoreTone(fundamentalScore), Strength: fundamentalScore, Detail: fundamentalDetail},
		{Key: "research", Label: "机构研报", Tone: scoreTone(researchScore), Strength: researchScore, Detail: research.Summary},
		{Key: "risk", Label: "新股风险", Tone: scoreTone(100 - risk.Score), Strength: 100 - risk.Score, Detail: fmt.Sprintf("高风险 · 观察仓上限%d%%", risk.SuggestedPositionMax)},
		{Key: "short_term", Label: "短线状态", Tone: "neutral", Strength: 45, Detail: short.State},
	}
}

func buildNewListingScorecard(profile Profile, signals []Signal) Scorecard {
	weights := map[string]float64{"listing_period": .22, "liquidity": .12, "turnover": .12, "theme": .12, "market": .10, "fundamental": .14, "research": .05, "risk": .13}
	dimensions := make([]DimensionScore, 0, len(signals))
	positive, negative := make([]string, 0, 5), make([]string, 0, 5)
	total, weightTotal := 0.0, 0.0
	for _, signal := range signals {
		weight := weights[signal.Key]
		if weight <= 0 {
			continue
		}
		total += float64(signal.Strength) * weight
		weightTotal += weight
		dimensions = append(dimensions, DimensionScore{Key: signal.Key, Label: signal.Label, Score: signal.Strength, Weight: weight, Status: scoreStatus(signal.Strength), Detail: signal.Detail})
		if signal.Tone == "positive" {
			positive = append(positive, signal.Label+"："+signal.Detail)
		} else if signal.Tone == "negative" {
			negative = append(negative, signal.Label+"："+signal.Detail)
		}
	}
	return Scorecard{Overall: int(math.Round(divide(total, weightTotal))), Grade: "观察", Direction: "观察", Conviction: "较低", Dimensions: dimensions, PositiveSignals: uniqueStrings(positive, 5), NegativeSignals: uniqueStrings(negative, 5)}
}

func buildNewListingRisks(trend TrendAnalysis, theme ThemeAnalysis, market *MarketContext) []string {
	risks := []string{
		fmt.Sprintf("上市仅%d个交易日，尚未形成20/60/120日均线和成熟支撑结构", trend.HistoryDays),
		fmt.Sprintf("价格发现期观测波动约%.1f%%，首日或早期脉冲可能放大回撤", trend.ObservedVolatility),
		"上市区间和换手仍在形成，不能用短样本外推长期趋势",
	}
	if theme.Primary == "" {
		risks = append(risks, "尚未确认稳定题材归属，持续性证据不足")
	}
	if market != nil && (market.Phase == "退潮" || market.Phase == "冰点") {
		risks = append(risks, "市场处于"+market.Phase+"阶段，新股流动性和估值波动风险上升")
	}
	return uniqueStrings(risks, 5)
}

func buildNewListingEvidence(input Input, trend TrendAnalysis, short ShortTermAnalysis, theme ThemeAnalysis, market *MarketContext, risk RiskControl, lines []foundation.KLine, fundamental *FundamentalAnalysis, research *ResearchAnalysis, stockNews *NewsAnalysis, themeNews *NewsAnalysis) []Evidence {
	latest := lines[len(lines)-1]
	evidence := []Evidence{
		{Category: "上市样本", Title: fmt.Sprintf("上市%d个交易日 · 受限模型", trend.HistoryDays), Detail: "本次不计算成熟均线、ATR和长周期收益", Source: latest.Meta.Source, AsOf: latest.Time.Format("2006-01-02")},
		{Category: "价格发现", Title: fmt.Sprintf("上市期%+.1f%% · 区间位置%.0f%%", trend.ListingReturn, trend.ListingRangePosition), Detail: strings.Join(trend.Reasons, "；"), Source: latest.Meta.Source, AsOf: latest.Time.Format("2006-01-02")},
		{Category: "流动性", Title: fmt.Sprintf("平均成交额%.1f亿元 · 换手%.1f%%", trend.AverageAmount/100_000_000, trend.AverageTurnover), Detail: fmt.Sprintf("可观测波动约%.1f%%", trend.ObservedVolatility), Source: latest.Meta.Source, AsOf: latest.Time.Format("2006-01-02")},
		{Category: "短线", Title: short.State, Detail: strings.Join(short.Reasons, "；"), Source: limitUpSource(input.LimitUps), AsOf: latestLimitUpDate(input.Symbol, input.LimitUps)},
	}
	if theme.Primary != "" {
		evidence = append(evidence, Evidence{Category: "题材", Title: theme.Primary, Detail: theme.Description, Source: firstNonEmpty(theme.Source, "题材归因"), AsOf: theme.AsOf})
	}
	if fundamental != nil && fundamental.Available {
		evidence = append(evidence, Evidence{Category: "基本面", Title: fundamental.ReportName + " · " + fundamental.Quality, Detail: fundamental.Summary, Source: fundamental.Source, AsOf: fundamental.ReportDate})
	}
	if research != nil && research.Available {
		evidence = append(evidence, Evidence{Category: "研报", Title: fmt.Sprintf("机构覆盖 · %d篇", research.ReportCount), Detail: research.Summary, Source: "eastmoney:report"})
	}
	if stockNews != nil && stockNews.Available {
		evidence = append(evidence, Evidence{Category: "个股新闻", Title: fmt.Sprintf("近%d日 · %d条", stockNews.WindowDays, stockNews.ArticleCount), Detail: stockNews.Summary, Source: firstNewsSource(stockNews.Articles), AsOf: newsAnalysisDate(stockNews)})
	}
	if themeNews != nil && themeNews.Available {
		evidence = append(evidence, Evidence{Category: "题材新闻", Title: fmt.Sprintf("近%d日 · %d条", themeNews.WindowDays, themeNews.ArticleCount), Detail: themeNews.Summary, Source: firstNewsSource(themeNews.Articles), AsOf: newsAnalysisDate(themeNews)})
	}
	if market != nil {
		evidence = append(evidence, Evidence{Category: "市场", Title: fmt.Sprintf("%s · %.0f分", market.Phase, market.Score), Detail: "市场情绪只作为新股风险约束", Source: market.Source, AsOf: market.TradeDate})
	}
	evidence = append(evidence, Evidence{Category: "风控", Title: fmt.Sprintf("高风险 · 观察仓0%%—%d%%", risk.SuggestedPositionMax), Detail: fmt.Sprintf("上市区间失效位%.2f", risk.StopPrice), Source: "新股受限样本风控模型"})
	return evidence[:min(len(evidence), 12)]
}

func buildNewListingDataQuality(input Input, profile Profile, lines []foundation.KLine, short ShortTermAnalysis, theme ThemeAnalysis, market *MarketContext, relative RelativeStrength, fundamental *FundamentalAnalysis, research *ResearchAnalysis, stockNews *NewsAnalysis, themeNews *NewsAnalysis) []DataQuality {
	quality := []DataQuality{{Key: "kline", Status: "limited", Message: fmt.Sprintf("上市初期仅有%d个交易日；采用新股价格发现模型", len(lines))}, {Key: "technical_window", Status: "limited", Message: fmt.Sprintf("还需%d个交易日形成20日窗口；MA20/60/120与ATR14未参与评分", max(20-len(lines), 1))}}
	if input.Quote.Price > 0 {
		quality = append(quality, DataQuality{Key: "quote", Status: "ready", Message: "实时行情已接入"})
	} else {
		quality = append(quality, DataQuality{Key: "quote", Status: "limited", Message: "实时行情不可用，使用最新日K收盘价"})
	}
	if short.ExactLimitUpData {
		quality = append(quality, DataQuality{Key: "limit_up", Status: "ready", Message: "已匹配精确涨停事件"})
	} else {
		quality = append(quality, DataQuality{Key: "limit_up", Status: "limited", Message: "上市初期涨停识别仅作辅助参考"})
	}
	if theme.Primary != "" {
		status := "limited"
		if theme.IsHot && theme.Resonance.Available {
			status = "ready"
		}
		quality = append(quality, DataQuality{Key: "theme", Status: status, Message: "上市初期题材仅作辅助，不替代价格发现判断"})
	} else {
		quality = append(quality, DataQuality{Key: "theme", Status: "missing", Message: "未匹配稳定题材"})
	}
	if market != nil {
		quality = append(quality, DataQuality{Key: "market_emotion", Status: "ready", Message: "已使用最近市场情绪缓存"})
	} else {
		quality = append(quality, DataQuality{Key: "market_emotion", Status: "limited", Message: "市场情绪缓存暂不可用"})
	}
	quality = append(quality, DataQuality{Key: "benchmark", Status: "limited", Message: relative.Detail})
	if stockNews != nil && stockNews.Available {
		quality = append(quality, DataQuality{Key: "stock_news", Status: "ready", Message: fmt.Sprintf("已匹配近%d日%d条个股新闻/公告", stockNews.WindowDays, stockNews.ArticleCount)})
	} else {
		quality = append(quality, DataQuality{Key: "stock_news", Status: "limited", Message: fmt.Sprintf("近%d日暂无匹配的个股新闻/公告", recentNewsWindowDays)})
	}
	if themeNews != nil && themeNews.Available {
		quality = append(quality, DataQuality{Key: "theme_news", Status: "ready", Message: fmt.Sprintf("已匹配近%d日%d条题材新闻", themeNews.WindowDays, themeNews.ArticleCount)})
	} else {
		quality = append(quality, DataQuality{Key: "theme_news", Status: "limited", Message: fmt.Sprintf("近%d日暂无匹配的题材新闻", recentNewsWindowDays)})
	}
	if profile.PrimaryType != "emotion_leader" {
		if fundamental != nil && fundamental.Available {
			quality = append(quality, DataQuality{Key: "fundamental", Status: "ready", Message: "已接入最新东方财富F10财务指标"})
		} else {
			quality = append(quality, DataQuality{Key: "fundamental", Status: "limited", Message: "最新F10财务指标暂不可用"})
		}
		if research != nil && research.Available {
			quality = append(quality, DataQuality{Key: "research", Status: "ready", Message: fmt.Sprintf("已读取近45日%d篇机构研报", research.ReportCount)})
		} else {
			quality = append(quality, DataQuality{Key: "research", Status: "limited", Message: "近45日暂无可用机构研报"})
		}
	}
	for _, gap := range input.CollectionGaps {
		quality = append(quality, DataQuality{Key: "collection", Status: "limited", Message: gap})
	}
	return quality
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
