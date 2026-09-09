package stockanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"easy-stock/backend/internal/hermes"
)

type aiConclusion struct {
	Headline       string             `json:"headline"`
	Title          string             `json:"title"`
	Summary        string             `json:"summary"`
	Overview       string             `json:"overview"`
	Action         string             `json:"action"`
	Recommendation string             `json:"recommendation"`
	BestPath       string             `json:"best_path"`
	MainRisk       string             `json:"main_risk"`
	StockNews      aiNewsConclusion   `json:"stock_news"`
	ThemeNews      aiNewsConclusion   `json:"theme_news"`
	Decision       aiDecisionPlan     `json:"decision"`
	Conclusion     *aiConclusionBlock `json:"conclusion"`
}

// Some providers wrap the requested fields in a conclusion object or use a
// nearby synonym despite the explicit output contract. Keep that response
// usable while still rejecting an actually empty model response.
type aiConclusionBlock struct {
	Headline       string `json:"headline"`
	Title          string `json:"title"`
	Summary        string `json:"summary"`
	Overview       string `json:"overview"`
	Action         string `json:"action"`
	Recommendation string `json:"recommendation"`
	BestPath       string `json:"best_path"`
	MainRisk       string `json:"main_risk"`
}

type aiNewsConclusion struct {
	Tone      string   `json:"tone"`
	Summary   string   `json:"summary"`
	Catalysts []string `json:"catalysts"`
	Risks     []string `json:"risks"`
}

type aiDecisionPlan struct {
	DecisionMode       string              `json:"decision_mode"`
	DecisionLabel      string              `json:"decision_label"`
	DecisionConfidence float64             `json:"decision_confidence"`
	Horizon            string              `json:"horizon"`
	Rationale          string              `json:"rationale"`
	CurrentAction      string              `json:"current_action"`
	PositionHint       string              `json:"position_hint"`
	NonShortPricePlan  aiNonShortPricePlan `json:"non_short_price_plan"`
	ShortTermPlaybook  ShortTermPlaybook   `json:"short_term_playbook"`
}

type aiNonShortPricePlan struct {
	Entry      aiPriceZone `json:"entry"`
	Hold       aiPriceZone `json:"hold"`
	TakeProfit aiPriceZone `json:"take_profit"`
	StopLoss   aiPriceZone `json:"stop_loss"`
}

type aiPriceZone struct {
	PriceLow  float64 `json:"price_low"`
	PriceHigh float64 `json:"price_high"`
	Reason    string  `json:"reason"`
	Action    string  `json:"action"`
}

type aiDailyKLineSummary struct {
	SampleDays           int                   `json:"sample_days"`
	LimitedSample        bool                  `json:"limited_sample"`
	StartDate            string                `json:"start_date,omitempty"`
	EndDate              string                `json:"end_date,omitempty"`
	StartClose           float64               `json:"start_close,omitempty"`
	LatestClose          float64               `json:"latest_close,omitempty"`
	PeriodHigh           float64               `json:"period_high,omitempty"`
	PeriodLow            float64               `json:"period_low,omitempty"`
	RangePositionPercent float64               `json:"range_position_percent,omitempty"`
	DrawdownFromPeak     float64               `json:"drawdown_from_peak_close_percent,omitempty"`
	MaxDrawdown          float64               `json:"max_drawdown_percent,omitempty"`
	UpDays               int                   `json:"up_days"`
	DownDays             int                   `json:"down_days"`
	FlatDays             int                   `json:"flat_days"`
	MaxDailyGain         float64               `json:"max_daily_gain_percent,omitempty"`
	MaxDailyLoss         float64               `json:"max_daily_loss_percent,omitempty"`
	CurrentStreak        aiDailyKLineStreak    `json:"current_streak"`
	WindowReturns        map[string]float64    `json:"window_returns_percent,omitempty"`
	DailyVolatility      map[string]float64    `json:"daily_volatility_percent,omitempty"`
	AverageVolume        map[string]float64    `json:"average_volume,omitempty"`
	AverageAmount        map[string]float64    `json:"average_amount,omitempty"`
	AverageTurnover      map[string]float64    `json:"average_turnover_percent,omitempty"`
	VolumeRatio5D20D     float64               `json:"volume_ratio_5d_20d,omitempty"`
	TwentyDaySegments    []aiDailyKLineSegment `json:"twenty_day_segments,omitempty"`
}

type aiDailyKLineStreak struct {
	Direction string `json:"direction"`
	Days      int    `json:"days"`
}

type aiDailyKLineSegment struct {
	StartDate       string  `json:"start_date"`
	EndDate         string  `json:"end_date"`
	TradingDays     int     `json:"trading_days"`
	ReturnPercent   float64 `json:"return_percent"`
	High            float64 `json:"high"`
	Low             float64 `json:"low"`
	AverageVolume   float64 `json:"average_volume"`
	AverageTurnover float64 `json:"average_turnover_percent,omitempty"`
}

func EnrichWithAI(ctx context.Context, prompter hermes.Prompter, analysis *Analysis, methodologyContext string) error {
	if prompter == nil || analysis == nil {
		return errors.New("AI分析底座不可用")
	}
	payload := map[string]any{
		"symbol":       analysis.Symbol,
		"name":         analysis.Name,
		"quote":        analysis.Quote,
		"profile":      analysis.Profile,
		"trend":        analysis.Trend,
		"short_term":   analysis.ShortTerm,
		"theme":        analysis.Theme,
		"fundamental":  analysis.Fundamental,
		"research":     analysis.Research,
		"stock_news":   analysis.StockNews,
		"theme_news":   analysis.ThemeNews,
		"market":       analysis.Market,
		"scorecard":    analysis.Scorecard,
		"timeframes":   analysis.Timeframes,
		"relative":     analysis.Relative,
		"signals":      analysis.Signals,
		"next_day":     analysis.NextDay,
		"risk_control": analysis.RiskControl,
		"action_plan":  analysis.ActionPlan,
		"risks":        analysis.Risks,
		"data_quality": analysis.DataQuality,
		"price_context": map[string]any{
			"current_quote":       analysis.Quote,
			"current_price":       analysis.Quote.Price,
			"current_trade_time":  analysis.Quote.TradeTime,
			"latest_daily_bar":    latestDailyBar(analysis.dailyBars),
			"daily_kline_summary": summarizeDailyKLines(analysis.dailyBars),
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	methodologyContext = truncateText(methodologyContext, 6_000)
	prompt := `你是 easy-stock 的A股全局交易决策器。请基于输入的全部结构化事实，先判断收益来源和执行方式，再生成与交易类型匹配的决策。

必须遵守：
1. 不编造输入JSON之外的实时数据、机构持仓、游资席位或公司事实。
2. profile.primary_type只是本地初筛，不是最终答案。你必须综合市场情绪、题材共振、个股地位、涨停历史、量价趋势、相对强度、基本面、研报、公告和新闻，自主判断decision_mode。
3. 不模拟任何投资名人的口吻，不使用“大佬投票”或人格化结论。
4. action 必须是条件化建议，不能承诺收益；隔日预期只能描述情景，不得表述为确定性预测。
5. price_context.current_price是当前行情价，优先级高于任何由日K推导出的旧收盘价；latest_daily_bar是最近一根日K，必须结合日期判断是否为当日收盘。daily_kline_summary是本地代码对最近最多120个交易日K线计算出的确定性统计摘要，包含区间收益、波动、回撤、量能、连涨跌和20日分段结构；不要反推或编造摘要之外的逐日K线。
6. scorecard、relative、theme、fundamental等事实和分数不得篡改；action_plan与risk_control里的价格只是本地候选，你可以依据全局分析重新定价。
6.1 当profile.primary_type=new_listing时，这是不足20个交易日的上市初期受限样本分析。不得推断MA20/60/120、ATR14、20/60/120日收益或成熟趋势结构，不得把价格发现评分表述为成熟趋势评分；维持观察优先、小仓验证和上市区间失效约束。
7. decision_mode=non_short时必须计算完整价格计划。价格不能只由均线或ATR决定，原因需要同时结合至少两个不同维度，例如基本面/研报预期、题材持续性、资金与趋势结构、新闻风险。必须满足：止损价 < 允许介入区间 < 第一止盈价 <= 第二止盈价，且第一止盈价必须高于当前现价；持有区间必须高于止损价。若现价已经超过原先按介入成本计算的目标，重新以现价上方的趋势延伸或压力位重算止盈区，并明确已有仓位如何移动保护位、新仓不追高。弱势非短线票可以把允许介入价设为右侧修复确认区，但仍要给出价格。
7. decision_mode=short_term时不要输出静态介入、止盈价格。重点输出盘后预案、9:25竞价确认、9:30—9:35开盘确认、参与/持有/退出条件和一票否决。输入没有次日实时竞价时，auction.status必须明确为“待9:25竞价确认”，不得假装已经看到竞价。
8. 短线决策必须使用action_plan.short_term_playbook.quantitative中的确定性阈值，逐条引用具体指数名称与代码、竞价涨幅区间、竞价成交额、9:35回撤/成交额，以及peers中的同题材个股名称；不得把这些条件改写成“板块同步”“资金较强”“承接良好”等笼统话术，也不得自行修改量化阈值。
9. 非情绪型必须结合fundamental与research；机构评级仅代表第三方观点，不得当作确定性结论。
10. 分别分析stock_news与theme_news：区分事实、潜在催化、风险、已兑现事件与未兑现预期，并判断新闻是否可能已被价格反映；不得把新闻标题直接等同于买卖建议。
11. 新闻结论只能引用输入中的文章；新闻为空时明确写“暂无匹配新闻”，不得补充模型记忆中的新闻。
12. 严格输出单个JSON对象，不要Markdown，不要额外解释。

输出格式：
{"headline":"不超过35字","summary":"80至180字","action":"当前动作","best_path":"最优验证路径","main_risk":"最主要风险","stock_news":{"tone":"偏多|中性|偏空|信息不足","summary":"60至140字","catalysts":["最多3条"],"risks":["最多3条"]},"theme_news":{"tone":"偏多|中性|偏空|信息不足","summary":"60至140字","catalysts":["最多3条"],"risks":["最多3条"]},"decision":{"decision_mode":"short_term|non_short","decision_label":"超短次日作战|趋势与价值定价|其他准确标签","decision_confidence":0.0,"horizon":"持有周期","rationale":"为何采用该决策模型","current_action":"当前动作","position_hint":"仓位约束","non_short_price_plan":{"entry":{"price_low":0,"price_high":0,"reason":"允许介入原因","action":"执行条件"},"hold":{"price_low":0,"price_high":0,"reason":"持有原因","action":"执行条件"},"take_profit":{"price_low":0,"price_high":0,"reason":"止盈原因","action":"执行条件"},"stop_loss":{"price_low":0,"price_high":0,"reason":"止损原因","action":"执行条件"}},"short_term_playbook":{"positioning":"个股定位","sentiment_cycle":"情绪周期","expected_pattern":"明日预期模式","overnight_conclusion":"盘后结论","data_status":"数据状态","auction":{"label":"9:25竞价确认","status":"待9:25竞价确认","summary":"竞价判断重点","required":["必要条件，最多4条"],"avoid":["竞价否决，最多4条"]},"opening":{"label":"9:30—9:35开盘确认","status":"待开盘确认","summary":"开盘判断重点","required":["必要条件，最多4条"],"avoid":["开盘否决，最多4条"]},"participation_conditions":["全部满足才允许参与，最多4条"],"hold_conditions":["持有条件，最多4条"],"exit_conditions":["退出条件，最多4条"],"veto_conditions":["任一触发即禁止，最多5条"],"scenarios":[{"name":"超预期|符合预期|低于预期","tone":"positive|neutral|negative","condition":"条件","action":"动作"}]}}}

只填写与decision_mode对应的计划：non_short时short_term_playbook可为空对象；short_term时non_short_price_plan可为空对象。

[结构化分析JSON]
` + string(encoded)
	if strings.TrimSpace(methodologyContext) != "" {
		prompt += "\n\n[本地游资心法的相关历史经验，仅作为短线风险与执行约束，不代表真人观点]\n" + methodologyContext
	}
	result, err := promptJSONObject[aiConclusion](ctx, prompter, prompt, "个股分析")
	if err != nil {
		return err
	}
	if !normalizeAIConclusion(&result, analysis.Conclusion) {
		return errors.New("Hermes个股分析未返回可用结论")
	}
	analysis.Conclusion = Conclusion{
		Headline: truncateText(result.Headline, 60),
		Summary:  truncateText(result.Summary, 360),
		Action:   truncateText(result.Action, 120),
		BestPath: truncateText(firstNonEmpty(result.BestPath, analysis.Conclusion.BestPath), 180),
		MainRisk: truncateText(firstNonEmpty(result.MainRisk, analysis.Conclusion.MainRisk), 180),
		Source:   "hermes-ai",
	}
	applyAINewsConclusion(analysis.StockNews, result.StockNews)
	applyAINewsConclusion(analysis.ThemeNews, result.ThemeNews)
	decisionMessage := applyAIDecision(analysis, result.Decision)
	analysis.AI = AISynthesisStatus{Status: "ready", Message: decisionMessage}
	return nil
}

func normalizeAIConclusion(result *aiConclusion, fallback Conclusion) bool {
	if result == nil {
		return false
	}
	if block := result.Conclusion; block != nil {
		result.Headline = firstNonEmpty(result.Headline, block.Headline, block.Title)
		result.Summary = firstNonEmpty(result.Summary, block.Summary, block.Overview)
		result.Action = firstNonEmpty(result.Action, block.Action, block.Recommendation)
		result.BestPath = firstNonEmpty(result.BestPath, block.BestPath)
		result.MainRisk = firstNonEmpty(result.MainRisk, block.MainRisk)
	}
	result.Headline = firstNonEmpty(result.Headline, result.Title)
	result.Summary = firstNonEmpty(result.Summary, result.Overview)
	result.Action = firstNonEmpty(result.Action, result.Recommendation)
	usable := strings.TrimSpace(result.Headline) != "" ||
		strings.TrimSpace(result.Summary) != "" ||
		strings.TrimSpace(result.Action) != "" ||
		strings.TrimSpace(result.BestPath) != "" ||
		strings.TrimSpace(result.MainRisk) != "" ||
		strings.TrimSpace(result.Decision.DecisionMode) != "" ||
		strings.TrimSpace(result.StockNews.Summary) != "" ||
		strings.TrimSpace(result.ThemeNews.Summary) != ""
	if !usable {
		return false
	}
	result.Headline = firstNonEmpty(result.Headline, result.Title, fallback.Headline)
	result.Summary = firstNonEmpty(result.Summary, result.Overview, fallback.Summary)
	result.Action = firstNonEmpty(result.Action, result.Recommendation, fallback.Action)
	result.BestPath = firstNonEmpty(result.BestPath, fallback.BestPath)
	result.MainRisk = firstNonEmpty(result.MainRisk, fallback.MainRisk)

	// A decision or a news synthesis is still a meaningful AI result even when
	// a provider omitted one of the three narrative fields. The local narrative
	// is retained for only the omitted fields above.
	return true
}

func applyAIDecision(analysis *Analysis, decision aiDecisionPlan) string {
	// New listings deliberately keep the local constrained-sample plan. A model
	// must not turn one to nineteen bars into a mature moving-average trade.
	if analysis.Profile.PrimaryType == "new_listing" {
		return "Hermes已结合上市初期证据完成综合研判；价格发现与风控计划沿用本地受限样本模型"
	}
	mode := normalizeDecisionMode(decision.DecisionMode)
	if mode == "" {
		return "Hermes已基于结构化证据完成综合研判；交易计划沿用本地校验结果"
	}
	plan := &analysis.ActionPlan
	plan.DecisionMode = mode
	plan.DecisionLabel = truncateText(firstNonEmpty(strings.TrimSpace(decision.DecisionLabel), map[string]string{"short_term": "超短次日作战", "non_short": "趋势与价值定价"}[mode]), 32)
	confidence := decision.DecisionConfidence
	if confidence <= 0 {
		confidence = firstPositive(plan.DecisionConfidence, analysis.Profile.Confidence)
	}
	plan.DecisionConfidence = round2(clamp(confidence, .3, .98))
	plan.Horizon = truncateText(firstNonEmpty(strings.TrimSpace(decision.Horizon), plan.Horizon), 48)
	plan.Rationale = truncateText(firstNonEmpty(strings.TrimSpace(decision.Rationale), plan.Rationale), 280)
	plan.CurrentAction = truncateText(firstNonEmpty(strings.TrimSpace(decision.CurrentAction), plan.CurrentAction), 120)
	plan.PositionHint = truncateText(firstNonEmpty(strings.TrimSpace(decision.PositionHint), plan.PositionHint), 160)

	if mode == "short_term" {
		fallback := plan.ShortTerm
		if fallback == nil {
			quantitative := ShortTermQuantitativePlan{}
			if analysis.shortTermQuantitative != nil {
				quantitative = *analysis.shortTermQuantitative
			}
			fallback = buildShortTermPlaybook(analysis.Profile, analysis.Trend, analysis.ShortTerm, analysis.Theme, analysis.Market, analysis.Relative, quantitative)
		}
		plan.ShortTerm = mergeShortTermPlaybook(decision.ShortTermPlaybook, fallback)
		plan.ShortTerm.Auction.Status = "待9:25竞价确认"
		plan.ShortTerm.Opening.Status = "待9:30—9:35开盘确认"
		plan.PricingSource = "not-applicable"
		plan.Entry = ActionPriceZone{}
		plan.Hold = ActionPriceZone{}
		plan.TakeProfit = ActionPriceZone{}
		plan.StopLoss = ActionPriceZone{}
		plan.EntryConditions = plan.ShortTerm.ParticipationConditions
		plan.HoldConditions = plan.ShortTerm.HoldConditions
		plan.AvoidConditions = plan.ShortTerm.VetoConditions
		return "Hermes已完成全局研判，并生成短线盘后、竞价与开盘作战计划"
	}

	plan.ShortTerm = nil
	if plan.Entry.PriceLow <= 0 {
		plan.Entry, plan.Hold, plan.TakeProfit, plan.StopLoss = buildActionPriceZones(analysis.Profile, analysis.Trend, analysis.ShortTerm, analysis.Market, analysis.RiskControl, currentAnalysisPrice(analysis))
	}
	if entry, hold, takeProfit, stopLoss, ok := validatedAIPricePlan(decision.NonShortPricePlan, currentAnalysisPrice(analysis)); ok {
		plan.Entry = entry
		plan.Hold = hold
		plan.TakeProfit = takeProfit
		plan.StopLoss = stopLoss
		plan.PricingSource = "hermes-ai"
		analysis.RiskControl = alignRiskControlWithActionPlan(analysis.RiskControl, *plan)
		refreshAnalysisRisk(analysis)
		return "Hermes已综合市场、题材、资金、基本面、研报与风险生成价格计划"
	}
	plan.PricingSource = "local-rules"
	analysis.RiskControl = alignRiskControlWithActionPlan(analysis.RiskControl, *plan)
	refreshAnalysisRisk(analysis)
	return "Hermes已完成全局研判；AI价格未通过一致性校验，当前保留本地候选价格"
}

func refreshAnalysisRisk(analysis *Analysis) {
	if analysis == nil {
		return
	}
	analysis.RiskControl = finalizeRiskControl(analysis.RiskControl, analysis.Profile, analysis.Trend, analysis.ShortTerm, analysis.Market)
	analysis.Signals = buildSignals(analysis.Trend, analysis.ShortTerm, analysis.Theme, analysis.Market, analysis.Relative, analysis.RiskControl, analysis.Timeframes, analysis.Fundamental, analysis.Research)
	analysis.Scorecard = buildScorecard(analysis.Profile, analysis.Signals)
	for index := range analysis.Evidence {
		if analysis.Evidence[index].Category != "风控" {
			continue
		}
		analysis.Evidence[index].Title = fmt.Sprintf("%s风险 · %d分", analysis.RiskControl.Level, analysis.RiskControl.Score)
		analysis.Evidence[index].Detail = fmt.Sprintf("计划失效位%.2f，建议仓位%d%%—%d%%", analysis.RiskControl.StopPrice, analysis.RiskControl.SuggestedPositionMin, analysis.RiskControl.SuggestedPositionMax)
	}
}

func normalizeDecisionMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "short_term", "short", "ultra_short", "超短", "短线":
		return "short_term"
	case "non_short", "trend", "swing", "long_term", "非短线", "趋势":
		return "non_short"
	default:
		return ""
	}
}

func validatedAIPricePlan(input aiNonShortPricePlan, latest float64) (ActionPriceZone, ActionPriceZone, ActionPriceZone, ActionPriceZone, bool) {
	entryLow, entryHigh := round2(input.Entry.PriceLow), round2(input.Entry.PriceHigh)
	holdLow, holdHigh := round2(input.Hold.PriceLow), round2(input.Hold.PriceHigh)
	takeLow, takeHigh := round2(input.TakeProfit.PriceLow), round2(input.TakeProfit.PriceHigh)
	stop := round2(firstPositive(input.StopLoss.PriceHigh, input.StopLoss.PriceLow))
	if entryLow <= 0 || entryHigh < entryLow || holdLow <= 0 || holdHigh < holdLow || stop <= 0 || stop >= entryLow || holdLow <= stop || takeLow <= entryHigh || takeHigh < takeLow || (latest > 0 && takeLow <= latest) {
		return ActionPriceZone{}, ActionPriceZone{}, ActionPriceZone{}, ActionPriceZone{}, false
	}
	entryMid := (entryLow + entryHigh) / 2
	if latest <= 0 {
		latest = entryMid
	}
	if stop < latest*.25 || entryMid > latest*2.5 || entryMid < latest*.3 || takeHigh > entryMid*4 || entryMid-stop > entryMid*.5 {
		return ActionPriceZone{}, ActionPriceZone{}, ActionPriceZone{}, ActionPriceZone{}, false
	}
	if strings.TrimSpace(input.Entry.Reason) == "" || strings.TrimSpace(input.Hold.Reason) == "" || strings.TrimSpace(input.TakeProfit.Reason) == "" || strings.TrimSpace(input.StopLoss.Reason) == "" {
		return ActionPriceZone{}, ActionPriceZone{}, ActionPriceZone{}, ActionPriceZone{}, false
	}
	entry := makeAIPriceZone("允许介入价格", entryLow, entryHigh, input.Entry)
	hold := makeAIPriceZone("持有价格", holdLow, holdHigh, input.Hold)
	takeProfit := makeAIPriceZone("止盈价格", takeLow, takeHigh, input.TakeProfit)
	stopLoss := ActionPriceZone{
		Label: "止损价格", PriceLow: 0, PriceHigh: stop, PriceText: fmt.Sprintf("≤ %.2f 元", stop),
		Reason: truncateText(input.StopLoss.Reason, 280), Action: truncateText(firstNonEmpty(strings.TrimSpace(input.StopLoss.Action), "触发后执行减仓或止损，不在逻辑失效后补仓。"), 220),
	}
	return entry, hold, takeProfit, stopLoss, true
}

func currentAnalysisPrice(analysis *Analysis) float64 {
	if analysis == nil {
		return 0
	}
	return firstPositive(analysis.Quote.Price, analysis.Trend.LatestClose)
}

func latestDailyBar(bars []AIDailyBar) AIDailyBar {
	if len(bars) == 0 {
		return AIDailyBar{}
	}
	return bars[len(bars)-1]
}

func summarizeDailyKLines(bars []AIDailyBar) aiDailyKLineSummary {
	valid := make([]AIDailyBar, 0, min(len(bars), 120))
	start := max(len(bars)-120, 0)
	for _, bar := range bars[start:] {
		if bar.Close > 0 && bar.High > 0 && bar.Low > 0 {
			valid = append(valid, bar)
		}
	}
	summary := aiDailyKLineSummary{
		SampleDays:      len(valid),
		LimitedSample:   len(valid) < 20,
		WindowReturns:   map[string]float64{},
		DailyVolatility: map[string]float64{},
		AverageVolume:   map[string]float64{},
		AverageAmount:   map[string]float64{},
		AverageTurnover: map[string]float64{},
		CurrentStreak:   aiDailyKLineStreak{Direction: "flat"},
	}
	if len(valid) == 0 {
		return summary
	}

	summary.StartDate = valid[0].Date
	summary.EndDate = valid[len(valid)-1].Date
	summary.StartClose = round2(valid[0].Close)
	summary.LatestClose = round2(valid[len(valid)-1].Close)
	summary.PeriodHigh = valid[0].High
	summary.PeriodLow = valid[0].Low
	closes := make([]float64, len(valid))
	volumes := make([]float64, len(valid))
	amounts := make([]float64, len(valid))
	turnovers := make([]float64, len(valid))
	returns := make([]float64, 0, len(valid))
	peakClose := valid[0].Close
	maxDrawdown := 0.0
	for index, bar := range valid {
		closes[index] = bar.Close
		volumes[index] = bar.Volume
		amounts[index] = bar.Amount
		turnovers[index] = bar.TurnoverRate
		if bar.High > summary.PeriodHigh {
			summary.PeriodHigh = bar.High
		}
		if bar.Low < summary.PeriodLow {
			summary.PeriodLow = bar.Low
		}
		if bar.Close > peakClose {
			peakClose = bar.Close
		}
		if peakClose > 0 {
			maxDrawdown = math.Min(maxDrawdown, percentChange(peakClose, bar.Close))
		}

		change := bar.ChangePercent
		if change == 0 && index > 0 && valid[index-1].Close > 0 {
			change = percentChange(valid[index-1].Close, bar.Close)
		}
		if index > 0 || bar.ChangePercent != 0 {
			returns = append(returns, change)
		}
		switch {
		case change > .005:
			summary.UpDays++
		case change < -.005:
			summary.DownDays++
		default:
			summary.FlatDays++
		}
		if len(returns) == 1 || change > summary.MaxDailyGain {
			summary.MaxDailyGain = change
		}
		if len(returns) == 1 || change < summary.MaxDailyLoss {
			summary.MaxDailyLoss = change
		}
	}

	summary.PeriodHigh = round2(summary.PeriodHigh)
	summary.PeriodLow = round2(summary.PeriodLow)
	if summary.PeriodHigh > summary.PeriodLow {
		summary.RangePositionPercent = round2(clamp((summary.LatestClose-summary.PeriodLow)/(summary.PeriodHigh-summary.PeriodLow)*100, 0, 100))
	}
	summary.DrawdownFromPeak = round2(percentChange(peakClose, summary.LatestClose))
	summary.MaxDrawdown = round2(maxDrawdown)
	summary.MaxDailyGain = round2(summary.MaxDailyGain)
	summary.MaxDailyLoss = round2(summary.MaxDailyLoss)
	summary.CurrentStreak = dailyKLineStreak(returns)

	for _, window := range []int{5, 10, 20, 60, 120} {
		if len(valid) < window {
			continue
		}
		key := fmt.Sprintf("%dd", window)
		summary.WindowReturns[key] = round2(windowReturn(closes, window))
		summary.AverageVolume[key] = round2(averageTail(volumes, window))
		summary.AverageAmount[key] = round2(averageTail(amounts, window))
		summary.AverageTurnover[key] = round2(averageTail(turnovers, window))
		if len(returns) > 0 {
			observations := min(window, len(returns))
			summary.DailyVolatility[key] = round2(standardDeviation(returns[len(returns)-observations:]))
		}
	}
	if len(valid) >= 20 {
		summary.VolumeRatio5D20D = round2(divide(averageTail(volumes, 5), averageTail(volumes, 20)))
	}
	summary.TwentyDaySegments = summarizeDailyKLineSegments(valid, 20)
	return summary
}

func dailyKLineStreak(returns []float64) aiDailyKLineStreak {
	if len(returns) == 0 {
		return aiDailyKLineStreak{Direction: "flat"}
	}
	direction := "flat"
	switch {
	case returns[len(returns)-1] > .005:
		direction = "up"
	case returns[len(returns)-1] < -.005:
		direction = "down"
	}
	days := 0
	for index := len(returns) - 1; index >= 0; index-- {
		current := "flat"
		if returns[index] > .005 {
			current = "up"
		} else if returns[index] < -.005 {
			current = "down"
		}
		if current != direction {
			break
		}
		days++
	}
	return aiDailyKLineStreak{Direction: direction, Days: days}
}

func summarizeDailyKLineSegments(bars []AIDailyBar, window int) []aiDailyKLineSegment {
	if len(bars) == 0 || window <= 0 {
		return nil
	}
	segments := make([]aiDailyKLineSegment, 0, (len(bars)+window-1)/window)
	for start := 0; start < len(bars); start += window {
		end := min(start+window, len(bars))
		segmentBars := bars[start:end]
		high := segmentBars[0].High
		low := segmentBars[0].Low
		volumeTotal := 0.0
		turnoverTotal := 0.0
		for _, bar := range segmentBars {
			high = math.Max(high, bar.High)
			low = math.Min(low, bar.Low)
			volumeTotal += bar.Volume
			turnoverTotal += bar.TurnoverRate
		}
		segments = append(segments, aiDailyKLineSegment{
			StartDate:       segmentBars[0].Date,
			EndDate:         segmentBars[len(segmentBars)-1].Date,
			TradingDays:     len(segmentBars),
			ReturnPercent:   round2(percentChange(segmentBars[0].Close, segmentBars[len(segmentBars)-1].Close)),
			High:            round2(high),
			Low:             round2(low),
			AverageVolume:   round2(volumeTotal / float64(len(segmentBars))),
			AverageTurnover: round2(turnoverTotal / float64(len(segmentBars))),
		})
	}
	return segments
}

func standardDeviation(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	mean := 0.0
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	variance := 0.0
	for _, value := range values {
		delta := value - mean
		variance += delta * delta
	}
	return math.Sqrt(variance / float64(len(values)))
}

func makeAIPriceZone(label string, low, high float64, input aiPriceZone) ActionPriceZone {
	return ActionPriceZone{
		Label: label, PriceLow: low, PriceHigh: high, PriceText: formatActionPriceRange(low, high),
		Reason: truncateText(input.Reason, 280), Action: truncateText(firstNonEmpty(strings.TrimSpace(input.Action), "满足全局分析条件后分批执行。"), 220),
	}
}

func mergeShortTermPlaybook(input ShortTermPlaybook, fallback *ShortTermPlaybook) *ShortTermPlaybook {
	if fallback == nil {
		fallback = &ShortTermPlaybook{}
	}
	result := input
	result.Positioning = truncateText(firstNonEmpty(strings.TrimSpace(result.Positioning), fallback.Positioning), 100)
	result.SentimentCycle = truncateText(firstNonEmpty(strings.TrimSpace(result.SentimentCycle), fallback.SentimentCycle), 100)
	result.ExpectedPattern = truncateText(firstNonEmpty(strings.TrimSpace(result.ExpectedPattern), fallback.ExpectedPattern), 180)
	result.OvernightConclusion = truncateText(firstNonEmpty(strings.TrimSpace(result.OvernightConclusion), fallback.OvernightConclusion), 300)
	result.DataStatus = truncateText(firstNonEmpty(strings.TrimSpace(result.DataStatus), fallback.DataStatus), 180)
	result.Quantitative = fallback.Quantitative
	result.Auction = mergeShortTermStage(result.Auction, fallback.Auction)
	result.Opening = mergeShortTermStage(result.Opening, fallback.Opening)
	result.ParticipationConditions = uniqueStrings(append(fallback.ParticipationConditions, result.ParticipationConditions...), 4)
	result.HoldConditions = uniqueStrings(append(fallback.HoldConditions, result.HoldConditions...), 4)
	result.ExitConditions = uniqueStrings(append(fallback.ExitConditions, result.ExitConditions...), 4)
	result.VetoConditions = uniqueStrings(append(fallback.VetoConditions, result.VetoConditions...), 5)
	if len(result.Scenarios) == 0 {
		result.Scenarios = fallback.Scenarios
	}
	if len(result.Scenarios) > 3 {
		result.Scenarios = result.Scenarios[:3]
	}
	for index := range result.Scenarios {
		result.Scenarios[index].Name = truncateText(result.Scenarios[index].Name, 24)
		result.Scenarios[index].Condition = truncateText(result.Scenarios[index].Condition, 180)
		result.Scenarios[index].Action = truncateText(result.Scenarios[index].Action, 180)
		switch result.Scenarios[index].Tone {
		case "positive", "negative":
		default:
			result.Scenarios[index].Tone = "neutral"
		}
	}
	return &result
}

func mergeShortTermStage(input, fallback ShortTermDecisionStage) ShortTermDecisionStage {
	return ShortTermDecisionStage{
		Label:    truncateText(firstNonEmpty(strings.TrimSpace(input.Label), fallback.Label), 32),
		Status:   truncateText(firstNonEmpty(strings.TrimSpace(input.Status), fallback.Status), 48),
		Summary:  truncateText(firstNonEmpty(strings.TrimSpace(input.Summary), fallback.Summary), 240),
		Required: uniqueStrings(append(fallback.Required, input.Required...), 4),
		Avoid:    uniqueStrings(append(fallback.Avoid, input.Avoid...), 4),
	}
}

func applyAINewsConclusion(target *NewsAnalysis, result aiNewsConclusion) {
	if target == nil || strings.TrimSpace(result.Summary) == "" {
		return
	}
	target.Tone = truncateText(firstNonEmpty(strings.TrimSpace(result.Tone), target.Tone), 12)
	target.Summary = truncateText(result.Summary, 280)
	target.Catalysts = uniqueStrings(result.Catalysts, 3)
	target.Risks = uniqueStrings(result.Risks, 3)
	target.AnalysisSource = "hermes-ai"
}

func normalizeThemeEvidenceType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "fact", "announcement":
		return "fact"
	case "market_mapping", "mapping":
		return "market_mapping"
	default:
		return "inference"
	}
}

func normalizeThemeEvidenceRelation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "own_business", "subsidiary", "equity_investment", "customer_supplier", "disposed_asset", "market_mapping":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func normalizeThemeEvidenceDirection(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "positive", "negative":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "neutral"
	}
}

type promptJSONObjectOptions struct {
	maxAttempts  int
	disableTools bool
	onAttempt    func(promptJSONAttempt)
}

type invalidJSONResponseError struct {
	label string
	cause error
}

func (e *invalidJSONResponseError) Error() string {
	return fmt.Sprintf("Hermes未返回有效%sJSON: %v", e.label, e.cause)
}
func (e *invalidJSONResponseError) Unwrap() error { return e.cause }

func isInvalidModelJSON(err error) bool {
	var invalid *invalidJSONResponseError
	return errors.As(err, &invalid)
}

type promptJSONAttempt struct {
	number        int
	durationMS    int64
	promptBytes   int
	responseBytes int
	err           string
}

func promptJSONObject[T any](ctx context.Context, prompter hermes.Prompter, prompt, label string) (T, error) {
	return promptJSONObjectWithOptions[T](ctx, prompter, prompt, label, promptJSONObjectOptions{maxAttempts: 2, disableTools: true})
}

func promptJSONObjectWithOptions[T any](ctx context.Context, prompter hermes.Prompter, prompt, label string, options promptJSONObjectOptions) (T, error) {
	var decoded T
	if options.maxAttempts <= 0 {
		options.maxAttempts = 1
	}
	attemptPrompt := prompt
	var firstDecodeErr error
	for attempt := 1; attempt <= options.maxAttempts; attempt++ {
		startedAt := time.Now()
		result, callErr := hermes.PromptUsingOptions(ctx, prompter, attemptPrompt, hermes.PromptOptions{
			Sandbox: true, AutoApprove: true, DisableTools: options.disableTools,
		})
		diagnostic := promptJSONAttempt{
			number: attempt, durationMS: time.Since(startedAt).Milliseconds(), promptBytes: len([]byte(attemptPrompt)),
			responseBytes: len([]byte(result.Content)),
		}
		if callErr != nil {
			diagnostic.err = callErr.Error()
			if options.onAttempt != nil {
				options.onAttempt(diagnostic)
			}
			if attempt == 1 {
				return decoded, fmt.Errorf("Hermes%s失败: %w", label, callErr)
			}
			return decoded, fmt.Errorf("Hermes未返回有效%sJSON: %v；自动纠错失败: %w", label, firstDecodeErr, callErr)
		}

		decoded = *new(T)
		decodeErr := decodeJSONObject(result.Content, &decoded)
		if decodeErr == nil {
			if options.onAttempt != nil {
				options.onAttempt(diagnostic)
			}
			return decoded, nil
		}
		diagnostic.err = decodeErr.Error()
		if options.onAttempt != nil {
			options.onAttempt(diagnostic)
		}
		if attempt == 1 {
			firstDecodeErr = decodeErr
		}
		if attempt >= options.maxAttempts {
			if options.maxAttempts == 1 {
				return decoded, &invalidJSONResponseError{label: label, cause: decodeErr}
			}
			return decoded, fmt.Errorf("Hermes未返回有效%sJSON: 首次%v；自动纠错后%w", label, firstDecodeErr, decodeErr)
		}
		attemptPrompt = `上一次输出无法解析为任务要求的JSON。请重新完成下面的原始任务，并只返回一个合法JSON对象：
- 使用英文半角双引号；
- 不要Markdown代码块、解释、思考过程或前后缀；
- 所有字段严格遵循原始任务给出的结构；
- 无法确定的数组返回[]，不要用自然语言拒答。

[原始任务]
` + prompt + "\n\n[上一次无效输出，仅用于纠错]\n" + truncateText(result.Content, 4_000)
	}
	return decoded, fmt.Errorf("Hermes未返回有效%sJSON", label)
}

func decodeJSONObject(content string, target any) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("empty response")
	}
	var encoded string
	if strings.HasPrefix(content, `"`) && json.Unmarshal([]byte(content), &encoded) == nil {
		content = strings.TrimSpace(encoded)
	}
	var lastErr error
	for start := strings.IndexByte(content, '{'); start >= 0; {
		end := balancedJSONObjectEnd(content, start)
		if end > start {
			candidate := content[start:end]
			if !hasKnownJSONField(candidate, target) {
				lastErr = errors.New("JSON object does not contain expected fields")
			} else if err := decodeFreshJSONObject(candidate, target); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
		next := strings.IndexByte(content[start+1:], '{')
		if next < 0 {
			break
		}
		start += next + 1
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("JSON object not found")
}

func hasKnownJSONField(candidate string, target any) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(candidate), &fields); err != nil {
		return false
	}
	targetType := reflect.TypeOf(target)
	if targetType == nil {
		return false
	}
	for targetType.Kind() == reflect.Pointer {
		targetType = targetType.Elem()
	}
	if targetType.Kind() != reflect.Struct {
		return len(fields) > 0
	}
	for index := 0; index < targetType.NumField(); index++ {
		field := targetType.Field(index)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" {
			name = field.Name
		}
		if name == "-" {
			continue
		}
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}

func decodeFreshJSONObject(candidate string, target any) error {
	targetValue := reflect.ValueOf(target)
	if !targetValue.IsValid() || targetValue.Kind() != reflect.Pointer || targetValue.IsNil() {
		return errors.New("JSON decode target must be a non-nil pointer")
	}
	fresh := reflect.New(targetValue.Elem().Type())
	decoder := json.NewDecoder(strings.NewReader(candidate))
	if err := decoder.Decode(fresh.Interface()); err != nil {
		return err
	}
	targetValue.Elem().Set(fresh.Elem())
	return nil
}

func balancedJSONObjectEnd(content string, start int) int {
	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(content); index++ {
		switch current := content[index]; {
		case inString && escaped:
			escaped = false
		case inString && current == '\\':
			escaped = true
		case current == '"':
			inString = !inString
		case !inString && current == '{':
			depth++
		case !inString && current == '}':
			depth--
			if depth == 0 {
				return index + 1
			}
		}
	}
	return -1
}
