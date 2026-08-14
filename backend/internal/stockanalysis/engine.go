package stockanalysis

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

func Analyze(input Input) (Analysis, error) {
	lines := normalizeKLines(input.KLines)
	if len(lines) < 20 {
		return Analysis{}, fmt.Errorf("个股AI分析至少需要20个交易日K线，当前只有%d个", len(lines))
	}

	quote := input.Quote
	last := lines[len(lines)-1]
	if strings.TrimSpace(quote.Symbol) == "" {
		quote.Symbol = input.Symbol
	}
	if quote.Price <= 0 {
		quote.Price = last.Close
	}
	if quote.ChangePercent == 0 && len(lines) >= 2 && lines[len(lines)-2].Close > 0 {
		quote.ChangePercent = percentChange(lines[len(lines)-2].Close, last.Close)
	}
	name := strings.TrimSpace(quote.Name)
	if name == "" {
		name = input.Symbol
		quote.Name = name
	}

	trend, chart := analyzeTrend(lines)
	shortTerm := analyzeShortTerm(input.Symbol, lines, input.LimitUps)
	theme := analyzeTheme(input.Symbol, shortTerm, input.CachedThemes, input.Concepts, input.Industry, input.Themes, input.LimitUps, input.Business, input.BusinessDetail, input.BusinessSource)
	theme = enrichTheme(input, shortTerm, theme)
	market := marketContext(input)
	profile := classifyProfile(trend, shortTerm, theme, market)
	var fundamental *FundamentalAnalysis
	var research *ResearchAnalysis
	if profile.PrimaryType != "emotion_leader" {
		value := analyzeFundamentals(input.Fundamentals)
		fundamental = &value
		researchValue := analyzeResearch(input.Reports)
		research = &researchValue
	}
	action := buildActionPlan(profile, trend, shortTerm, market)
	risks := buildRisks(profile, trend, shortTerm, theme, market)
	timeframes := analyzeTimeframes(lines)
	relative := analyzeRelativeStrength(input, lines)
	riskControl := buildRiskControl(profile, trend, shortTerm, market)
	nextDay := buildNextDayPlan(lines, profile, trend, shortTerm, theme, market, relative, riskControl)
	signals := buildSignals(trend, shortTerm, theme, market, relative, riskControl, timeframes, fundamental, research)
	scorecard := buildScorecard(profile, signals)
	conclusion := buildConclusion(name, profile, trend, shortTerm, action, risks)
	evidence := buildEvidence(input, profile, trend, shortTerm, theme, market, relative, riskControl, lines, fundamental, research)
	quality := buildDataQuality(input, profile, lines, shortTerm, theme, market, relative, fundamental, research)

	return Analysis{
		Symbol:      input.Symbol,
		Name:        name,
		GeneratedAt: time.Now(),
		Quote:       quote,
		Profile:     profile,
		Conclusion:  conclusion,
		Trend:       trend,
		ShortTerm:   shortTerm,
		Theme:       theme,
		Fundamental: fundamental,
		Research:    research,
		Market:      market,
		Scorecard:   scorecard,
		Timeframes:  timeframes,
		Relative:    relative,
		Signals:     signals,
		NextDay:     nextDay,
		RiskControl: riskControl,
		ActionPlan:  action,
		Risks:       risks,
		Evidence:    evidence,
		DataQuality: quality,
		Chart:       chart,
		AI:          AISynthesisStatus{Status: "rules", Message: "当前结论由本地结构化分析引擎生成"},
	}, nil
}

func analyzeFundamentals(item *foundation.StockFundamentals) FundamentalAnalysis {
	if item == nil || strings.TrimSpace(item.ReportDate) == "" {
		return FundamentalAnalysis{Quality: "数据不足", Summary: "尚未取得最新F10财务数据"}
	}
	score := 50.0
	score += clamp(item.RevenueYearOverYear/8, -15, 15)
	score += clamp(item.NetProfitYearOverYear/6, -20, 20)
	if item.ROE >= 15 {
		score += 12
	} else if item.ROE >= 8 {
		score += 6
	} else if item.ROE > 0 && item.ROE < 3 {
		score -= 6
	} else if item.ROE < 0 {
		score -= 15
	}
	if item.GrossMargin >= 35 {
		score += 8
	} else if item.GrossMargin > 0 && item.GrossMargin < 12 {
		score -= 8
	}
	if item.DebtRatio >= 75 {
		score -= 12
	} else if item.DebtRatio > 0 && item.DebtRatio <= 45 {
		score += 5
	}
	if item.OperatingCashFlowPerShare > 0 {
		score += 5
	} else if item.OperatingCashFlowPerShare < 0 {
		score -= 5
	}
	finalScore := int(math.Round(clamp(score, 0, 100)))
	quality := "中性"
	if finalScore >= 72 {
		quality = "较好"
	} else if finalScore >= 58 {
		quality = "稳健"
	} else if finalScore < 35 {
		quality = "承压"
	} else if finalScore < 48 {
		quality = "偏弱"
	}
	summary := fmt.Sprintf("%s：营收同比%+.1f%%，归母净利同比%+.1f%%，ROE %.1f%%，毛利率%.1f%%，负债率%.1f%%", firstNonEmpty(item.ReportName, item.ReportDate), item.RevenueYearOverYear, item.NetProfitYearOverYear, item.ROE, item.GrossMargin, item.DebtRatio)
	return FundamentalAnalysis{
		Available: true, Score: finalScore, Quality: quality, ReportDate: item.ReportDate, ReportName: item.ReportName,
		Revenue: item.Revenue, RevenueYearOverYear: item.RevenueYearOverYear, NetProfit: item.NetProfit,
		NetProfitYearOverYear: item.NetProfitYearOverYear, EPS: item.EPS, ROE: item.ROE, GrossMargin: item.GrossMargin,
		DebtRatio: item.DebtRatio, OperatingCashFlowPerShare: item.OperatingCashFlowPerShare,
		Summary: summary, Source: item.Meta.Source,
	}
}

func analyzeResearch(items []foundation.MarketResearchItem) ResearchAnalysis {
	if len(items) == 0 {
		return ResearchAnalysis{Coverage: "暂无覆盖", Summary: "近45日未取得该股机构研报", Reports: []foundation.MarketResearchItem{}}
	}
	organizations := map[string]bool{}
	ratingChanges := []string{}
	positiveRatings := 0
	latestRating := ""
	for index, item := range items {
		if value := strings.TrimSpace(item.Organization); value != "" {
			organizations[value] = true
		}
		if index == 0 {
			latestRating = strings.TrimSpace(item.Rating)
		}
		if ratingIsPositive(item.Rating) {
			positiveRatings++
		}
		if value := strings.TrimSpace(item.RatingChange); value != "" {
			ratingChanges = append(ratingChanges, value)
		}
	}
	score := 45 + min(len(items), 8)*3 + min(len(organizations), 5)*2
	if len(items) > 0 {
		score += int(math.Round(float64(positiveRatings) / float64(len(items)) * 20))
	}
	score = int(clamp(float64(score), 0, 100))
	coverage := "有限"
	if len(items) >= 5 || len(organizations) >= 3 {
		coverage = "较充分"
	} else if len(items) >= 2 {
		coverage = "一般"
	}
	summary := fmt.Sprintf("近45日收录%d篇个股研报，覆盖%d家机构", len(items), len(organizations))
	if latestRating != "" {
		summary += "，最新评级" + latestRating
	}
	return ResearchAnalysis{Available: true, Score: score, Coverage: coverage, ReportCount: len(items), OrganizationCount: len(organizations), LatestRating: latestRating, RatingChanges: uniqueStrings(ratingChanges, 5), Summary: summary, Reports: items[:min(len(items), 6)]}
}

func ratingIsPositive(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, keyword := range []string{"买入", "增持", "强烈推荐", "推荐", "outperform", "buy"} {
		if strings.Contains(value, keyword) {
			return true
		}
	}
	return false
}

func analyzeTrend(lines []foundation.KLine) (TrendAnalysis, []TrendPoint) {
	closes := make([]float64, len(lines))
	highs := make([]float64, len(lines))
	lows := make([]float64, len(lines))
	volumes := make([]float64, len(lines))
	ma20Series := movingAverageSeries(lines, 20)
	ma60Series := movingAverageSeries(lines, 60)
	ma120Series := movingAverageSeries(lines, 120)
	for index, line := range lines {
		closes[index] = line.Close
		highs[index] = line.High
		lows[index] = line.Low
		volumes[index] = line.Volume
	}

	last := closes[len(closes)-1]
	ma20 := lastValue(ma20Series)
	ma60 := lastValue(ma60Series)
	ma120 := lastValue(ma120Series)
	slope20 := seriesSlopePercent(ma20Series, 10)
	slope60 := seriesSlopePercent(ma60Series, 20)
	return20 := windowReturn(closes, 20)
	return60 := windowReturn(closes, 60)
	return120 := windowReturn(closes, 120)
	high60, low60 := rangeHighLow(lines, 60)
	high120, _ := rangeHighLow(lines, 120)
	position60 := 50.0
	if high60 > low60 {
		position60 = clamp((last-low60)/(high60-low60)*100, 0, 100)
	}
	drawdown120 := 0.0
	if high120 > 0 {
		drawdown120 = percentChange(high120, last)
	}
	volumeRatio := divide(averageTail(volumes, 5), averageTail(volumes, 20))
	atrPercent := averageTrueRangePercent(lines, 14)

	score := 20
	reasons := make([]string, 0, 6)
	if ma20 > 0 && last > ma20 {
		score += 12
		reasons = append(reasons, "收盘价站在20日均线上方")
	}
	if ma60 > 0 && last > ma60 {
		score += 14
		reasons = append(reasons, "中期价格结构保持在60日均线上方")
	}
	if ma120 > 0 && last > ma120 {
		score += 8
	}
	if ma20 > 0 && ma60 > 0 && ma20 > ma60 {
		score += 10
		reasons = append(reasons, "20日均线高于60日均线")
	}
	if ma60 > 0 && ma120 > 0 && ma60 > ma120 {
		score += 8
	}
	if slope20 > 0.5 {
		score += 8
		reasons = append(reasons, "20日趋势斜率向上")
	} else if slope20 < -1 {
		score -= 10
	}
	if slope60 > 0.5 {
		score += 8
	} else if slope60 < -1 {
		score -= 10
	}
	if return60 > 8 {
		score += 7
	} else if return60 < -10 {
		score -= 8
	}
	if drawdown120 >= -15 {
		score += 5
	} else if drawdown120 < -30 {
		score -= 8
	}
	if position60 >= 82 && volumeRatio >= 1.15 {
		score += 5
		reasons = append(reasons, "价格接近阶段高位且量能得到确认")
	}
	score = int(math.Round(clamp(float64(score), 0, 100)))

	phase := classifyTrendPhase(last, ma20, ma60, ma120, slope20, slope60, position60, drawdown120, volumeRatio)
	strength := "偏弱"
	switch {
	case score >= 78:
		strength = "强趋势"
	case score >= 62:
		strength = "趋势成立"
	case score >= 45:
		strength = "结构中性"
	case score >= 28:
		strength = "趋势转弱"
	default:
		strength = "弱势结构"
	}
	setup := trendSetup(phase, position60, volumeRatio)
	support := firstPositive(ma20, ma60, low60)
	if phase == "趋势回踩" || phase == "高位整理" {
		support = firstPositive(ma60, ma20, low60)
	}
	resistance := high60
	invalidation := ""
	if ma60 > 0 {
		invalidation = fmt.Sprintf("放量跌破60日趋势线 %.2f，且两个交易日不能收回", ma60)
	} else {
		invalidation = fmt.Sprintf("跌破近20日结构低点 %.2f", rangeLow(lines, 20))
	}

	chartStart := max(len(lines)-120, 0)
	chart := make([]TrendPoint, 0, len(lines)-chartStart)
	for index := chartStart; index < len(lines); index++ {
		point := TrendPoint{Date: lines[index].Time.Format("2006-01-02"), Close: round2(lines[index].Close)}
		if ma20Series[index] > 0 {
			value := round2(ma20Series[index])
			point.MA20 = &value
		}
		if ma60Series[index] > 0 {
			value := round2(ma60Series[index])
			point.MA60 = &value
		}
		if ma120Series[index] > 0 {
			value := round2(ma120Series[index])
			point.MA120 = &value
		}
		chart = append(chart, point)
	}

	return TrendAnalysis{
		Score:               score,
		Strength:            strength,
		Phase:               phase,
		Setup:               setup,
		LatestClose:         round2(last),
		MA20:                round2(ma20),
		MA60:                round2(ma60),
		MA120:               round2(ma120),
		Return20:            round2(return20),
		Return60:            round2(return60),
		Return120:           round2(return120),
		RangePosition60:     round2(position60),
		DrawdownFromHigh120: round2(drawdown120),
		VolumeRatio:         round2(volumeRatio),
		ATR14Percent:        round2(atrPercent),
		Support:             round2(support),
		Resistance:          round2(resistance),
		Invalidation:        invalidation,
		Reasons:             uniqueStrings(reasons, 6),
	}, chart
}

func analyzeShortTerm(symbol string, lines []foundation.KLine, events []foundation.LimitUpEvent) ShortTermAnalysis {
	threshold := limitUpThreshold(symbol)
	returns := dailyReturns(lines)
	recent20 := tailFloat(returns, 20)
	limitFlags := make([]bool, len(recent20))
	for index, value := range recent20 {
		limitFlags[index] = value >= threshold
	}
	limitCount := 0
	for _, value := range limitFlags {
		if value {
			limitCount++
		}
	}
	maxStreak := maxBoolStreak(limitFlags)
	matching := make([]foundation.LimitUpEvent, 0)
	for _, event := range events {
		if event.Symbol == symbol {
			matching = append(matching, event)
		}
	}
	sort.SliceStable(matching, func(i, j int) bool { return matching[i].Date.Before(matching[j].Date) })
	latestStreak := 0
	latestOpenCount := 0
	latestTurnover := lines[len(lines)-1].TurnoverRate
	if len(matching) > 0 {
		latest := matching[len(matching)-1]
		latestStreak = latest.Streak
		latestOpenCount = latest.OpenCount
		if latest.TurnoverRate > 0 {
			latestTurnover = latest.TurnoverRate
		}
		limitCount = max(limitCount, min(len(matching), 20))
		maxStreak = max(maxStreak, latest.Streak)
	}
	return5 := windowReturnFromReturns(returns, 5)
	return10 := windowReturnFromReturns(returns, 10)
	state := classifyShortTermState(returns, limitCount, maxStreak)
	avgAmount := averageKLineAmount(lines, 20)
	tradability := "一般"
	switch {
	case avgAmount >= 2_000_000_000 && latestTurnover >= 3:
		tradability = "容量充足"
	case avgAmount >= 500_000_000:
		tradability = "流动性较好"
	case avgAmount > 0 && avgAmount < 100_000_000:
		tradability = "流动性偏低"
	}
	reasons := []string{fmt.Sprintf("近20日识别到%d个涨停日，最高%d连板", limitCount, maxStreak)}
	if return5 >= 5 {
		reasons = append(reasons, fmt.Sprintf("近5日累计上涨%.1f%%", return5))
	}
	if latestOpenCount > 0 {
		reasons = append(reasons, fmt.Sprintf("最近一次涨停开板%d次，需观察回封承接", latestOpenCount))
	}
	if avgAmount >= 1_000_000_000 {
		reasons = append(reasons, fmt.Sprintf("近20日平均成交额%.1f亿元", avgAmount/100_000_000))
	}

	return ShortTermAnalysis{
		State:            state,
		LimitUpCount20:   limitCount,
		MaxLimitStreak20: maxStreak,
		ExactLimitUpData: len(matching) > 0,
		LatestStreak:     latestStreak,
		LatestOpenCount:  latestOpenCount,
		LatestTurnover:   round2(latestTurnover),
		AverageAmount20:  round2(avgAmount),
		RecentReturn5:    round2(return5),
		RecentReturn10:   round2(return10),
		Tradability:      tradability,
		Reasons:          uniqueStrings(reasons, 5),
	}
}

type themeCandidate struct {
	theme    string
	concepts []string
	source   string
	asOf     string
	role     string
	evidence string
}

func analyzeTheme(symbol string, short ShortTermAnalysis, cached []foundation.StockThemeAttribution, concepts []string, industry string, overviews []foundation.ThemeOverview, events []foundation.LimitUpEvent, businessValues ...string) ThemeAnalysis {
	concepts = uniqueStrings(concepts, 8)
	industry = strings.TrimSpace(industry)
	business := ""
	businessDetail := ""
	businessSource := ""
	if len(businessValues) > 0 {
		business = strings.TrimSpace(businessValues[0])
	}
	if len(businessValues) > 1 {
		businessDetail = strings.TrimSpace(businessValues[1])
	}
	if len(businessValues) > 2 {
		businessSource = strings.TrimSpace(businessValues[2])
	}
	if business == "" {
		business = industry
	}
	route := "trend"
	if short.ExactLimitUpData || short.MaxLimitStreak20 >= 2 || short.LimitUpCount20 >= 1 {
		route = "short_term"
	}

	poolCached, hasPoolCached := newestCachedTheme(cached, "kaipanla-limit-up")
	leaderCached, hasLeaderCached := newestCachedTheme(cached, "kaipanla-theme-leader")
	poolEvent, hasPoolEvent := newestPoolEventTheme(symbol, events)
	leaderEvent, hasLeaderEvent := newestLeaderEventTheme(symbol, events)
	limitEvent, hasLimitEvent := newestLimitEventTheme(symbol, events)
	poolCandidate, hasPool := newerThemeCandidate(
		candidateFromAttribution(poolCached, "短线连板缓存"), hasPoolCached,
		poolEvent, hasPoolEvent,
	)
	leaderCandidate, hasLeader := newerThemeCandidate(
		candidateFromAttribution(leaderCached, "趋势题材领涨归因"), hasLeaderCached,
		leaderEvent, hasLeaderEvent,
	)

	selected := themeCandidate{}
	selectedOK := false
	selectCandidate := func(candidate themeCandidate, ok bool) {
		if !selectedOK && ok && strings.TrimSpace(candidate.theme) != "" {
			selected = candidate
			selectedOK = true
		}
	}
	if route == "short_term" {
		selectCandidate(poolCandidate, hasPool)
		selectCandidate(leaderCandidate, hasLeader)
		selectCandidate(limitEvent, hasLimitEvent)
	} else {
		selectCandidate(leaderCandidate, hasLeader)
		selectCandidate(poolCandidate, hasPool)
	}

	// A concept listed in a stock catalog is only a possible membership. It is
	// not enough to call the stock a hot-theme play. Only explicit Kaipanla
	// attribution/event evidence can promote a hot theme to the primary label.
	hotTheme := strings.TrimSpace(selected.theme)
	best := foundation.ThemeOverview{}
	if hotTheme != "" {
		best = bestThemeOverview(overviews, append([]string{hotTheme}, selected.concepts...))
	}
	primary := hotTheme
	primarySource := selected.source
	primaryEvidence := selected.evidence
	if primary == "" {
		primary = strings.TrimSpace(business)
		primarySource = firstNonEmpty(businessSource, "eastmoney-f10-business")
		if primary == "" {
			primary = industry
			primarySource = "eastmoney-stock-industry"
		}
		if primary != "" {
			primaryEvidence = "公司主业/行业：" + primary
		}
	}
	displayConcepts := uniqueStrings(append(append([]string{hotTheme}, selected.concepts...), concepts...), 8)
	evidence := uniqueStrings([]string{primaryEvidence}, 4)
	description := "尚未匹配到明确热点，按公司主业分析"
	if hotTheme != "" {
		description = themeDescription(hotTheme, route, primarySource)
	} else if primary != "" {
		description = fmt.Sprintf("未发现明确热点炒作证据，按公司主业%s分析", primary)
		if businessDetail != "" {
			evidence = uniqueStrings(append(evidence, "F10主营资料："+truncateText(businessDetail, 120)), 4)
		}
	}
	if hotTheme != "" && best.Name != "" {
		description += fmt.Sprintf("；题材趋势分%d，阶段%s，活跃%d日", best.TrendScore, firstNonEmpty(best.TrendStage, "待确认"), best.ActiveDays)
		evidence = uniqueStrings(append(evidence, fmt.Sprintf("趋势题材雷达：%s %d分", best.Name, best.TrendScore)), 4)
	}
	return ThemeAnalysis{
		Primary: primary, Business: business, HotTheme: hotTheme, IsHot: hotTheme != "",
		Concepts:    displayConcepts,
		Source:      primarySource,
		AsOf:        selected.asOf,
		Evidence:    evidence,
		Route:       route,
		TrendScore:  max(best.TrendScore, 0),
		TrendStage:  best.TrendStage,
		ActiveDays:  best.ActiveDays,
		MaxStreak:   best.MaxStreak,
		Role:        firstNonEmpty(selected.role, "待确认"),
		Description: description,
	}
}

func marketContext(input Input) *MarketContext {
	if input.MarketEmotion == nil {
		return nil
	}
	item := input.MarketEmotion
	return &MarketContext{
		TradeDate:  item.TradeDate,
		Phase:      item.Phase,
		Score:      item.EmotionScore,
		Confidence: item.Confidence,
		Source:     item.Source,
	}
}

func classifyProfile(trend TrendAnalysis, short ShortTermAnalysis, theme ThemeAnalysis, market *MarketContext) Profile {
	primaryType := "range_watch"
	typeLabel := "震荡观察型"
	role := firstNonEmpty(theme.Role, "独立走势")
	confidence := 0.58

	switch {
	case short.MaxLimitStreak20 >= 2 || (short.LimitUpCount20 >= 2 && short.RecentReturn10 >= 12):
		primaryType = "emotion_leader"
		typeLabel = "情绪连板型"
		confidence = 0.78
	case trend.Score >= 72 && short.AverageAmount20 >= 1_000_000_000:
		primaryType = "trend_capacity"
		typeLabel = "趋势容量型"
		role = firstNonEmpty(nonDefaultRole(theme.Role), "容量趋势候选")
		confidence = 0.82
	case trend.Score >= 68:
		primaryType = "trend_growth"
		typeLabel = "成长趋势型"
		role = firstNonEmpty(nonDefaultRole(theme.Role), "趋势强势股")
		confidence = 0.76
	case trend.Score < 32 || trend.Phase == "弱势下跌":
		primaryType = "weak_risk"
		typeLabel = "走弱风险型"
		role = "风险观察"
		confidence = 0.8
	}
	if market == nil {
		confidence -= 0.08
	}
	if theme.Primary == "" {
		confidence -= 0.05
	}
	tags := []string{trend.Phase, short.State}
	if theme.Primary != "" {
		tags = append(tags, theme.Primary)
	}
	if short.Tradability != "一般" {
		tags = append(tags, short.Tradability)
	}
	return Profile{
		PrimaryType: primaryType,
		TypeLabel:   typeLabel,
		PricePhase:  trend.Phase,
		MarketRole:  role,
		Tags:        uniqueStrings(tags, 5),
		Confidence:  round2(clamp(confidence, 0.3, 0.92)),
	}
}

func buildActionPlan(profile Profile, trend TrendAnalysis, short ShortTermAnalysis, market *MarketContext) ActionPlan {
	plan := ActionPlan{
		CurrentAction: "等待结构确认",
		EntryConditions: []string{
			fmt.Sprintf("价格重新站稳%.2f附近的关键趋势位", firstPositive(trend.MA20, trend.Support)),
			"成交量放大但日内波动不过度扩张",
		},
		HoldConditions: []string{
			"中期趋势线保持向上",
			"个股相对题材或指数没有持续走弱",
		},
		AvoidConditions: []string{
			"高开加速且量价明显失衡时不追",
			"市场风险阶段与个股破位同时出现时回避",
		},
		Invalidation: trend.Invalidation,
		PositionHint: "首次验证以观察仓为主，确认后再递增",
	}
	switch profile.PrimaryType {
	case "emotion_leader":
		plan.CurrentAction = "等待分歧后的承接确认"
		plan.EntryConditions = []string{"题材高度没有坍塌", "分歧后回封或次日弱转强", "换手充分且未出现一致性缩量加速"}
		plan.HoldConditions = []string{"仍保持板块辨识度", "连板梯队和赚钱效应没有明显恶化"}
		plan.AvoidConditions = []string{"一致高潮后继续追高", "高位股批量负反馈", "开板次数增加且回封质量下降"}
		plan.PositionHint = "高波动路径，单次仓位应低于趋势容量股"
	case "trend_capacity", "trend_growth":
		if trend.Phase == "主升" {
			plan.CurrentAction = "持有者跟踪趋势，新开仓等待回踩或有效突破"
		} else if trend.Phase == "趋势回踩" {
			plan.CurrentAction = "观察回踩确认，避免提前猜底"
		}
		plan.EntryConditions = []string{
			fmt.Sprintf("回踩%.2f附近缩量企稳，或放量突破%.2f", trend.Support, trend.Resistance),
			"20日与60日趋势没有同步转弱",
			"题材强度或相对强度重新改善",
		}
		plan.HoldConditions = []string{"收盘维持在20日或60日趋势线上方", "上涨放量、回调缩量的量价关系未破坏"}
		plan.AvoidConditions = []string{"距离阶段高点过近但量能不能确认", "放量滞涨并连续跌破短期均线"}
		plan.PositionHint = "趋势未确认前分批验证，避免在单日加速时一次性重仓"
	case "weak_risk":
		plan.CurrentAction = "回避，等待重新筑底"
		plan.EntryConditions = []string{"60日趋势由下行转为走平", "价格重新站回中期均线", "形成更高低点并出现持续量能"}
		plan.HoldConditions = []string{"仅适用于已有仓位的纪律性减仓观察"}
		plan.AvoidConditions = []string{"下跌途中因单日反弹追入", "跌破新低后继续补仓摊薄成本"}
		plan.PositionHint = "不建议新增交易仓位"
	}
	if market != nil && (market.Phase == "退潮" || market.Phase == "冰点") {
		plan.AvoidConditions = append([]string{"当前市场处于" + market.Phase + "阶段，所有触发条件需要更严格"}, plan.AvoidConditions...)
	}
	plan.EntryConditions = uniqueStrings(plan.EntryConditions, 4)
	plan.HoldConditions = uniqueStrings(plan.HoldConditions, 4)
	plan.AvoidConditions = uniqueStrings(plan.AvoidConditions, 4)
	_ = short
	return plan
}

func buildRisks(profile Profile, trend TrendAnalysis, short ShortTermAnalysis, theme ThemeAnalysis, market *MarketContext) []string {
	risks := make([]string, 0, 6)
	if trend.RangePosition60 >= 90 {
		risks = append(risks, "价格位于60日区间高位，追涨的盈亏比下降")
	}
	if trend.ATR14Percent >= 5 {
		risks = append(risks, fmt.Sprintf("近14日平均真实波幅约%.1f%%，日内波动较大", trend.ATR14Percent))
	}
	if trend.VolumeRatio >= 1.5 && trend.Return20 < 3 {
		risks = append(risks, "近期明显放量但价格推进有限，需防范高位换手或分歧扩大")
	}
	if short.LatestOpenCount >= 2 {
		risks = append(risks, "最近涨停反复开板，封板质量和次日溢价存在不确定性")
	}
	if theme.Primary == "" {
		risks = append(risks, "暂未确认稳定题材归属，个股持续性缺少板块共振证据")
	}
	if market == nil {
		risks = append(risks, "市场情绪缓存尚未提供，本次未纳入整体赚钱效应")
	} else if market.Phase == "退潮" {
		risks = append(risks, "市场处于退潮阶段，强势股补跌风险高于常态")
	}
	if profile.PrimaryType == "weak_risk" {
		risks = append(risks, "中期结构尚未修复，单日反弹不能视为趋势反转")
	}
	if len(risks) == 0 {
		risks = append(risks, "当前结构未出现突出风险，但仍需以失效条件约束判断")
	}
	return uniqueStrings(risks, 5)
}

func buildConclusion(name string, profile Profile, trend TrendAnalysis, short ShortTermAnalysis, action ActionPlan, risks []string) Conclusion {
	headline := fmt.Sprintf("%s · %s · %s", profile.TypeLabel, trend.Phase, action.CurrentAction)
	summary := fmt.Sprintf("%s当前趋势得分%d，短线状态为%s。分析优先级由%s路径主导，判断以可验证的趋势、量价和题材证据为准。", name, trend.Score, short.State, profile.TypeLabel)
	bestPath := firstNonEmpty(firstString(action.EntryConditions), "等待价格和量能共同确认")
	mainRisk := firstNonEmpty(firstString(risks), "关键趋势位失守")
	return Conclusion{
		Headline: headline,
		Summary:  summary,
		Action:   action.CurrentAction,
		BestPath: bestPath,
		MainRisk: mainRisk,
		Source:   "local-analysis-engine",
	}
}

func buildEvidence(input Input, profile Profile, trend TrendAnalysis, short ShortTermAnalysis, theme ThemeAnalysis, market *MarketContext, relative RelativeStrength, risk RiskControl, lines []foundation.KLine, fundamental *FundamentalAnalysis, research *ResearchAnalysis) []Evidence {
	evidence := []Evidence{
		{Category: "趋势", Title: fmt.Sprintf("%s · %d分", trend.Strength, trend.Score), Detail: strings.Join(trend.Reasons, "；"), Source: lines[len(lines)-1].Meta.Source, AsOf: lines[len(lines)-1].Time.Format("2006-01-02")},
		{Category: "量价", Title: fmt.Sprintf("近20日%+.1f%% · 量比%.2f", trend.Return20, trend.VolumeRatio), Detail: fmt.Sprintf("60日区间位置%.0f%%，距120日高点%+.1f%%", trend.RangePosition60, trend.DrawdownFromHigh120), Source: lines[len(lines)-1].Meta.Source, AsOf: lines[len(lines)-1].Time.Format("2006-01-02")},
		{Category: "短线", Title: short.State, Detail: strings.Join(short.Reasons, "；"), Source: limitUpSource(input.LimitUps), AsOf: latestLimitUpDate(input.Symbol, input.LimitUps)},
	}
	if theme.Primary != "" {
		detail := theme.Description
		if len(theme.Evidence) > 0 {
			detail = strings.Join(theme.Evidence, "；") + "。" + detail
		}
		evidence = append(evidence, Evidence{Category: "题材", Title: theme.Primary, Detail: detail, Source: firstNonEmpty(theme.Source, "题材归因"), AsOf: theme.AsOf})
	}
	if profile.PrimaryType != "emotion_leader" && fundamental != nil && fundamental.Available {
		evidence = append(evidence, Evidence{Category: "基本面", Title: fundamental.ReportName + " · " + fundamental.Quality, Detail: fundamental.Summary, Source: fundamental.Source, AsOf: fundamental.ReportDate})
	}
	if profile.PrimaryType != "emotion_leader" && research != nil && research.Available {
		detail := research.Summary + "；评级为机构观点，仅作预期参考"
		evidence = append(evidence, Evidence{Category: "研报", Title: fmt.Sprintf("机构覆盖 · %d篇", research.ReportCount), Detail: detail, Source: "eastmoney:report"})
	}
	if market != nil {
		evidence = append(evidence, Evidence{Category: "市场", Title: fmt.Sprintf("%s · %.0f分", market.Phase, market.Score), Detail: "市场情绪仅作为交易环境约束，不替代个股结构", Source: market.Source, AsOf: market.TradeDate})
	}
	if relative.Available {
		evidence = append(evidence, Evidence{Category: "相对强度", Title: relative.State, Detail: relative.Detail, Source: firstNonEmpty(relative.BenchmarkName, relative.BenchmarkSymbol) + "对照"})
	}
	evidence = append(evidence, Evidence{Category: "风控", Title: fmt.Sprintf("%s风险 · %d分", risk.Level, risk.Score), Detail: fmt.Sprintf("计划失效位%.2f，建议仓位%d%%—%d%%", risk.StopPrice, risk.SuggestedPositionMin, risk.SuggestedPositionMax), Source: "结构化风险模型"})
	for _, news := range relatedNews(input, theme) {
		evidence = append(evidence, Evidence{Category: "催化", Title: news.Title, Detail: truncateText(news.Content, 120), Source: news.Meta.Source, AsOf: news.PublishedAt.Format("2006-01-02 15:04")})
	}
	return evidence[:min(len(evidence), 10)]
}

func buildDataQuality(input Input, profile Profile, lines []foundation.KLine, short ShortTermAnalysis, theme ThemeAnalysis, market *MarketContext, relative RelativeStrength, fundamental *FundamentalAnalysis, research *ResearchAnalysis) []DataQuality {
	quality := []DataQuality{{Key: "kline", Status: "ready", Message: fmt.Sprintf("已读取%d个交易日K线", len(lines))}}
	if input.Quote.Price > 0 {
		quality = append(quality, DataQuality{Key: "quote", Status: "ready", Message: "实时行情已接入"})
	} else {
		quality = append(quality, DataQuality{Key: "quote", Status: "limited", Message: "实时行情不可用，使用最新日K收盘价"})
	}
	if short.ExactLimitUpData {
		quality = append(quality, DataQuality{Key: "limit_up", Status: "ready", Message: "已匹配精确涨停事件"})
	} else {
		quality = append(quality, DataQuality{Key: "limit_up", Status: "limited", Message: "涨停次数主要由日K涨幅近似识别"})
	}
	switch {
	case theme.IsHot && theme.Resonance.Available:
		quality = append(quality, DataQuality{Key: "theme", Status: "ready", Message: fmt.Sprintf("已完成事件驱动题材归因：%s；共振%d分", theme.Primary, theme.Resonance.Score)})
	case theme.IsHot:
		quality = append(quality, DataQuality{Key: "theme", Status: "limited", Message: fmt.Sprintf("已识别事实题材%s，等待题材成分股盘面验证", theme.Primary)})
	case strings.Contains(theme.Source, "kaipanla-limit-up"):
		quality = append(quality, DataQuality{Key: "theme", Status: "ready", Message: "已命中开盘啦短线连板缓存"})
	case strings.Contains(theme.Source, "kaipanla-theme-leader"):
		quality = append(quality, DataQuality{Key: "theme", Status: "ready", Message: "已命中开盘啦趋势题材缓存"})
	case theme.Source == "eastmoney:f10-business" || theme.Source == "eastmoney-f10-business":
		quality = append(quality, DataQuality{Key: "theme", Status: "ready", Message: "未发现明确热点炒作证据，当前按东方财富F10主营业务定位"})
	case theme.Source == "eastmoney-stock-industry":
		quality = append(quality, DataQuality{Key: "theme", Status: "limited", Message: "F10主营业务不可用，当前按东方财富行业定位"})
	case theme.Primary != "":
		quality = append(quality, DataQuality{Key: "theme", Status: "ready", Message: "已完成主业定位"})
	default:
		quality = append(quality, DataQuality{Key: "theme", Status: "missing", Message: "未匹配稳定题材"})
	}
	if market != nil {
		quality = append(quality, DataQuality{Key: "market_emotion", Status: "ready", Message: "已使用最近市场情绪缓存"})
	} else {
		quality = append(quality, DataQuality{Key: "market_emotion", Status: "limited", Message: "市场情绪缓存暂不可用"})
	}
	if relative.Available {
		quality = append(quality, DataQuality{Key: "benchmark", Status: "ready", Message: fmt.Sprintf("已接入%s作为相对强度基准", firstNonEmpty(relative.BenchmarkName, relative.BenchmarkSymbol))})
	} else {
		quality = append(quality, DataQuality{Key: "benchmark", Status: "limited", Message: "基准指数数据不可用，相对强度未参与结论"})
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

func classifyTrendPhase(last, ma20, ma60, ma120, slope20, slope60, position60, drawdown120, volumeRatio float64) string {
	if ma60 > 0 && last < ma60 && slope60 < -0.5 {
		return "弱势下跌"
	}
	if ma20 > 0 && ma60 > 0 && last > ma20 && ma20 > ma60 && slope20 > 0.5 && slope60 >= 0 {
		if position60 >= 88 && volumeRatio >= 1.15 {
			return "主升"
		}
		if drawdown120 >= -12 {
			return "趋势推进"
		}
	}
	if ma60 > 0 && last > ma60 && ma20 > 0 && last <= ma20*1.03 && slope60 > 0 {
		return "趋势回踩"
	}
	if ma60 > 0 && last > ma60 && slope60 > 0 && position60 >= 65 {
		return "高位整理"
	}
	if ma60 > 0 && math.Abs(last-ma60)/ma60 <= 0.06 && math.Abs(slope60) < 1.5 {
		return "筑底整理"
	}
	if ma120 > 0 && last > ma120 && slope20 > 0 {
		return "启动尝试"
	}
	return "区间震荡"
}

func trendSetup(phase string, position60, volumeRatio float64) string {
	switch phase {
	case "主升":
		if position60 >= 92 && volumeRatio < 1.1 {
			return "等待有效突破"
		}
		return "趋势持有"
	case "趋势推进":
		return "回踩或突破跟随"
	case "趋势回踩":
		return "等待缩量企稳"
	case "高位整理":
		return "等待平台方向选择"
	case "筑底整理", "启动尝试":
		return "观察右侧确认"
	case "弱势下跌":
		return "回避并等待筑底"
	default:
		return "区间边界观察"
	}
}

func classifyShortTermState(returns []float64, limitCount, maxStreak int) string {
	latest := lastFloat(returns)
	previous := 0.0
	if len(returns) >= 2 {
		previous = returns[len(returns)-2]
	}
	return5 := windowReturnFromReturns(returns, 5)
	switch {
	case latest <= -4 && return5 < 0:
		return "退潮"
	case previous <= -2 && latest >= 3:
		return "修复"
	case latest < 0 && return5 >= 6:
		return "分歧"
	case maxStreak >= 2 && latest >= 5:
		return "加速"
	case limitCount >= 1 && return5 < 12:
		return "启动"
	case return5 >= 5:
		return "发酵"
	default:
		return "待确认"
	}
}

func normalizeKLines(lines []foundation.KLine) []foundation.KLine {
	out := append([]foundation.KLine(nil), lines...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	filtered := out[:0]
	for _, line := range out {
		if line.Close > 0 && line.High > 0 && line.Low > 0 {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func movingAverageSeries(lines []foundation.KLine, window int) []float64 {
	out := make([]float64, len(lines))
	if window <= 0 {
		return out
	}
	sum := 0.0
	for index, line := range lines {
		sum += line.Close
		if index >= window {
			sum -= lines[index-window].Close
		}
		if index >= window-1 {
			out[index] = sum / float64(window)
		}
	}
	return out
}

func seriesSlopePercent(values []float64, lookback int) float64 {
	end := lastValue(values)
	if end <= 0 {
		return 0
	}
	endIndex := len(values) - 1
	startIndex := max(endIndex-lookback, 0)
	for startIndex < endIndex && values[startIndex] <= 0 {
		startIndex++
	}
	start := values[startIndex]
	if start <= 0 {
		return 0
	}
	return percentChange(start, end)
}

func windowReturn(values []float64, window int) float64 {
	if len(values) < 2 {
		return 0
	}
	start := max(len(values)-window, 0)
	if start >= len(values)-1 {
		start = len(values) - 2
	}
	return percentChange(values[start], values[len(values)-1])
}

func dailyReturns(lines []foundation.KLine) []float64 {
	returns := make([]float64, len(lines))
	for index, line := range lines {
		if line.ChangePercent != 0 {
			returns[index] = line.ChangePercent
			continue
		}
		if index > 0 && lines[index-1].Close > 0 {
			returns[index] = percentChange(lines[index-1].Close, line.Close)
		}
	}
	return returns
}

func windowReturnFromReturns(returns []float64, window int) float64 {
	start := max(len(returns)-window, 0)
	value := 1.0
	for _, item := range returns[start:] {
		value *= 1 + item/100
	}
	return (value - 1) * 100
}

func rangeHighLow(lines []foundation.KLine, window int) (float64, float64) {
	start := max(len(lines)-window, 0)
	high := 0.0
	low := math.MaxFloat64
	for _, line := range lines[start:] {
		if line.High > high {
			high = line.High
		}
		if line.Low < low {
			low = line.Low
		}
	}
	if low == math.MaxFloat64 {
		low = 0
	}
	return high, low
}

func rangeLow(lines []foundation.KLine, window int) float64 {
	_, low := rangeHighLow(lines, window)
	return low
}

func averageTrueRangePercent(lines []foundation.KLine, window int) float64 {
	if len(lines) < 2 {
		return 0
	}
	start := max(len(lines)-window, 1)
	total := 0.0
	count := 0
	for index := start; index < len(lines); index++ {
		previous := lines[index-1].Close
		tr := math.Max(lines[index].High-lines[index].Low, math.Max(math.Abs(lines[index].High-previous), math.Abs(lines[index].Low-previous)))
		if previous > 0 {
			total += tr / previous * 100
			count++
		}
	}
	return divide(total, float64(count))
}

func averageKLineAmount(lines []foundation.KLine, window int) float64 {
	start := max(len(lines)-window, 0)
	values := make([]float64, 0, len(lines)-start)
	for _, line := range lines[start:] {
		if line.Amount > 0 {
			values = append(values, line.Amount)
		}
	}
	return average(values)
}

func averageTail(values []float64, window int) float64 {
	start := max(len(values)-window, 0)
	return average(values[start:])
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

func limitUpThreshold(symbol string) float64 {
	code := strings.Split(symbol, ".")[0]
	if strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301") || strings.HasPrefix(code, "688") {
		return 19.0
	}
	if strings.HasSuffix(symbol, ".BJ") {
		return 28.5
	}
	return 9.5
}

func maxBoolStreak(values []bool) int {
	best := 0
	current := 0
	for _, value := range values {
		if value {
			current++
			best = max(best, current)
		} else {
			current = 0
		}
	}
	return best
}

func themeMatches(value string, overview foundation.ThemeOverview) bool {
	left := compactTheme(value)
	if left == "" {
		return false
	}
	for _, candidate := range []string{overview.Name, overview.Theme} {
		right := compactTheme(candidate)
		if right != "" && (left == right || strings.Contains(left, right) || strings.Contains(right, left)) {
			return true
		}
	}
	return false
}

func newestCachedTheme(items []foundation.StockThemeAttribution, sourcePart string) (foundation.StockThemeAttribution, bool) {
	best := foundation.StockThemeAttribution{}
	found := false
	for _, item := range items {
		if !strings.Contains(item.Source, sourcePart) || strings.TrimSpace(item.Theme) == "" {
			continue
		}
		if !found || item.TradeDate > best.TradeDate || (item.TradeDate == best.TradeDate && themeRank(item.Rank) < themeRank(best.Rank)) {
			best = item
			found = true
		}
	}
	return best, found
}

func candidateFromAttribution(item foundation.StockThemeAttribution, label string) themeCandidate {
	concepts := uniqueStrings(append([]string{item.Theme}, item.Concepts...), 8)
	evidence := ""
	if strings.TrimSpace(item.Theme) != "" {
		evidence = fmt.Sprintf("%s：%s", label, item.Theme)
		if item.Role != "" {
			evidence += " · " + item.Role
		}
	}
	return themeCandidate{
		theme: item.Theme, concepts: concepts, source: item.Source, asOf: item.TradeDate,
		role: item.Role, evidence: evidence,
	}
}

func newerThemeCandidate(left themeCandidate, leftOK bool, right themeCandidate, rightOK bool) (themeCandidate, bool) {
	if !leftOK {
		return right, rightOK
	}
	if !rightOK {
		return left, true
	}
	if right.asOf > left.asOf {
		return right, true
	}
	return left, true
}

func newestPoolEventTheme(symbol string, events []foundation.LimitUpEvent) (themeCandidate, bool) {
	return newestEventTheme(symbol, events, func(event foundation.LimitUpEvent) (themeCandidate, bool) {
		if !strings.Contains(event.Meta.Source, "kaipanla-limit-up") {
			return themeCandidate{}, false
		}
		theme := preferredPoolConcept(event)
		if theme == "" {
			return themeCandidate{}, false
		}
		return themeCandidate{
			theme: theme, concepts: event.Concepts, source: event.Meta.Source,
			role: event.ThemeLeaderRole, evidence: "短线连板涨停池：" + strings.Join(uniqueStrings(event.Concepts, 5), "、"),
		}, true
	})
}

func newestLeaderEventTheme(symbol string, events []foundation.LimitUpEvent) (themeCandidate, bool) {
	return newestEventTheme(symbol, events, func(event foundation.LimitUpEvent) (themeCandidate, bool) {
		if !strings.Contains(event.ThemeSource, "kaipanla-theme-leader") || strings.TrimSpace(event.PrimaryTheme) == "" {
			return themeCandidate{}, false
		}
		return themeCandidate{
			theme: event.PrimaryTheme, concepts: event.Concepts, source: event.ThemeSource,
			role: event.ThemeLeaderRole, evidence: "趋势题材领涨归因：" + event.PrimaryTheme + roleEvidenceSuffix(event.ThemeLeaderRole),
		}, true
	})
}

func newestLimitEventTheme(symbol string, events []foundation.LimitUpEvent) (themeCandidate, bool) {
	return newestEventTheme(symbol, events, func(event foundation.LimitUpEvent) (themeCandidate, bool) {
		theme := strings.TrimSpace(event.PrimaryTheme)
		if theme == "" {
			theme = firstString(uniqueStrings(event.Concepts, 1))
		}
		if theme == "" {
			return themeCandidate{}, false
		}
		source := firstNonEmpty(event.ThemeSource, event.Meta.Source, "limit-up-event")
		return themeCandidate{
			theme: theme, concepts: event.Concepts, source: source,
			role: event.ThemeLeaderRole, evidence: "涨停事件题材：" + theme,
		}, true
	})
}

func newestEventTheme(symbol string, events []foundation.LimitUpEvent, build func(foundation.LimitUpEvent) (themeCandidate, bool)) (themeCandidate, bool) {
	best := themeCandidate{}
	found := false
	for _, event := range events {
		if event.Symbol != symbol {
			continue
		}
		candidate, ok := build(event)
		if !ok {
			continue
		}
		if !event.Date.IsZero() {
			candidate.asOf = event.Date.Format("2006-01-02")
		}
		if !found || candidate.asOf > best.asOf {
			best = candidate
			found = true
		}
	}
	return best, found
}

func preferredPoolConcept(event foundation.LimitUpEvent) string {
	primary := compactTheme(event.PrimaryTheme)
	if strings.Contains(event.ThemeSource, "kaipanla-theme-leader") && primary != "" {
		for _, concept := range event.Concepts {
			if compactTheme(concept) != primary && strings.TrimSpace(concept) != "" {
				return strings.TrimSpace(concept)
			}
		}
	}
	return firstString(uniqueStrings(event.Concepts, 1))
}

func bestThemeOverview(overviews []foundation.ThemeOverview, terms []string) foundation.ThemeOverview {
	best := foundation.ThemeOverview{}
	bestScore := -1
	for _, overview := range overviews {
		matched := false
		for _, term := range terms {
			if themeMatches(term, overview) {
				matched = true
				break
			}
		}
		if matched && overview.TrendScore > bestScore {
			best = overview
			bestScore = overview.TrendScore
		}
	}
	return best
}

func candidateFromOverview(overview foundation.ThemeOverview) (themeCandidate, bool) {
	theme := strings.TrimSpace(overview.Name)
	if theme == "" {
		theme = strings.TrimSpace(overview.Theme)
	}
	if theme == "" {
		return themeCandidate{}, false
	}
	return themeCandidate{
		theme: theme, concepts: []string{theme}, source: firstNonEmpty(overview.Source, "theme-radar"),
		asOf: overview.TradeDate, evidence: fmt.Sprintf("趋势题材雷达：%s %d分", theme, overview.TrendScore),
	}, true
}

func themeDescription(primary, route, source string) string {
	switch {
	case strings.Contains(source, "kaipanla-limit-up"):
		return fmt.Sprintf("短线连板路径优先采用开盘啦涨停池归因：%s", primary)
	case strings.Contains(source, "kaipanla-theme-leader"):
		return fmt.Sprintf("%s路径采用开盘啦题材领涨归因：%s", routeLabel(route), primary)
	case source == "eastmoney-stock-concepts":
		return fmt.Sprintf("开盘啦缓存未命中，降级按东方财富个股概念归入%s", primary)
	case source == "eastmoney-stock-industry":
		return fmt.Sprintf("仅匹配到宽泛行业%s，题材归属仍需确认", primary)
	default:
		return fmt.Sprintf("%s路径按趋势题材雷达归入%s", routeLabel(route), primary)
	}
}

func routeLabel(route string) string {
	if route == "short_term" {
		return "短线连板"
	}
	return "趋势"
}

func roleEvidenceSuffix(role string) string {
	if strings.TrimSpace(role) == "" {
		return ""
	}
	return " · " + strings.TrimSpace(role)
}

func themeRank(rank int) int {
	if rank <= 0 {
		return 1 << 30
	}
	return rank
}

func compactTheme(value string) string {
	replacer := strings.NewReplacer("概念", "", "板块", "", "产业链", "", " ", "", "-", "", "_", "")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(value)))
}

func relatedNews(input Input, theme ThemeAnalysis) []foundation.NewsItem {
	terms := []string{input.Quote.Name, strings.Split(input.Symbol, ".")[0], theme.Primary}
	terms = append(terms, theme.Concepts...)
	items := make([]foundation.NewsItem, 0, 2)
	for _, item := range input.News {
		haystack := strings.ToLower(item.Title + " " + item.Content)
		matched := false
		for _, term := range terms {
			term = strings.TrimSpace(term)
			if len([]rune(term)) >= 2 && strings.Contains(haystack, strings.ToLower(term)) {
				matched = true
				break
			}
		}
		if matched {
			items = append(items, item)
			if len(items) >= 2 {
				break
			}
		}
	}
	return items
}

func latestLimitUpDate(symbol string, events []foundation.LimitUpEvent) string {
	latest := time.Time{}
	for _, event := range events {
		if event.Symbol == symbol && event.Date.After(latest) {
			latest = event.Date
		}
	}
	if latest.IsZero() {
		return ""
	}
	return latest.Format("2006-01-02")
}

func limitUpSource(events []foundation.LimitUpEvent) string {
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Meta.Source != "" {
			return events[index].Meta.Source
		}
	}
	return "日K近似识别"
}

func lastValue(values []float64) float64 {
	for index := len(values) - 1; index >= 0; index-- {
		if values[index] > 0 {
			return values[index]
		}
	}
	return 0
}

func tailFloat(values []float64, window int) []float64 {
	return values[max(len(values)-window, 0):]
}

func lastFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}

func percentChange(start, end float64) float64 {
	if start == 0 {
		return 0
	}
	return (end/start - 1) * 100
}

func divide(value, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return value / denominator
}

func clamp(value, low, high float64) float64 {
	return math.Min(math.Max(value, low), high)
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonDefaultRole(value string) string {
	if value == "待确认" {
		return ""
	}
	return value
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func uniqueStrings(values []string, limit int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func truncateText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}
