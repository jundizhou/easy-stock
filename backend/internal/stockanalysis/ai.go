package stockanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"easy-stock/backend/internal/hermes"
)

type aiConclusion struct {
	Headline  string           `json:"headline"`
	Summary   string           `json:"summary"`
	Action    string           `json:"action"`
	BestPath  string           `json:"best_path"`
	MainRisk  string           `json:"main_risk"`
	StockNews aiNewsConclusion `json:"stock_news"`
	ThemeNews aiNewsConclusion `json:"theme_news"`
	Decision  aiDecisionPlan   `json:"decision"`
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

type aiThemeEvidenceResponse struct {
	Items []ThemeEvidence `json:"items"`
}

// ExtractThemeEvidence asks Hermes to normalize facts that are difficult to
// recover from short announcement titles (for example an investment in a
// compound-semiconductor subsidiary). The response is only a candidate-evidence
// layer; enrichTheme still requires deterministic market confirmation before it
// promotes a theme to the primary hot label.
func ExtractThemeEvidence(ctx context.Context, prompter hermes.Prompter, input Input) ([]ThemeEvidence, error) {
	if prompter == nil {
		return nil, errors.New("AI分析底座不可用")
	}
	if len(input.Announcements) == 0 && len(input.News) == 0 {
		return nil, nil
	}
	var source strings.Builder
	source.WriteString("股票：" + input.Quote.Name + "（" + input.Symbol + "）\n")
	source.WriteString("主营：" + firstNonEmpty(input.Business, input.Industry) + "\n")
	source.WriteString("概念目录：" + strings.Join(input.Concepts, "、") + "\n")
	source.WriteString("公告：\n")
	for _, item := range input.Announcements {
		source.WriteString("- " + item.PublishedAt.Format("2006-01-02") + " | " + item.Title + " | " + item.Category + " | " + truncateText(item.Content, 900) + "\n")
	}
	source.WriteString("资讯：\n")
	for _, item := range input.News {
		if containsAnyFold(item.Title+" "+item.Content, input.Quote.Name, strings.Split(input.Symbol, ".")[0]) {
			source.WriteString("- " + item.PublishedAt.Format("2006-01-02 15:04") + " | " + item.Title + " | " + truncateText(item.Content, 240) + "\n")
		}
	}
	prompt := `你是A股题材归因器。请仅基于输入事实，提取可能影响当前炒作的具体题材。
规则：
1. 区分 fact（公告或公司明确事实）、market_mapping（产业链/市场映射）、inference（弱推断）。
2. 不要把主营行业、宽泛概念目录直接当成热点；不要编造公告未出现的公司事实。
3. 公司出版、引用或获奖的图书、丛书、教材、论文标题即使包含题材词，也不代表公司经营该产业，不得据此输出题材。
4. 题材名称使用短而具体的中文，例如“化合物半导体”“砷化镓”“光电子器件”“CPO”。
5. 每项必须给出原文标题或摘要作为snippet，strength为0到1。
6. 只输出JSON：{"items":[{"theme":"...","type":"fact|market_mapping|inference","source":"...","title":"...","snippet":"...","strength":0.0}]}

[输入]
` + source.String()
	result, err := prompter.Prompt(ctx, prompt)
	if err != nil {
		return nil, err
	}
	var decoded aiThemeEvidenceResponse
	if err := decodeJSONObject(result.Content, &decoded); err != nil {
		return nil, err
	}
	out := make([]ThemeEvidence, 0, min(len(decoded.Items), 12))
	for _, item := range decoded.Items {
		item.Theme = strings.TrimSpace(item.Theme)
		if item.Theme == "" {
			continue
		}
		item.Strength = clamp(item.Strength, .15, .98)
		item.Freshness = freshnessForTime(item.PublishedAt)
		out = append(out, item)
		if len(out) >= 12 {
			break
		}
	}
	return out, nil
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
			"current_quote":      analysis.Quote,
			"current_price":      analysis.Quote.Price,
			"current_trade_time": analysis.Quote.TradeTime,
			"latest_daily_bar":   latestDailyBar(analysis.dailyBars),
			"daily_bars":         analysis.dailyBars,
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
5. price_context.current_price是当前行情价，优先级高于任何由日K推导出的旧收盘价；latest_daily_bar是最近一根日K，必须结合日期判断是否为当日收盘。daily_bars是最近120个交易日的精简日K序列，价格计划必须同时参考现价、日K结构、成交量和阶段压力。
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
	response, err := prompter.Prompt(ctx, prompt)
	if err != nil {
		return fmt.Errorf("Hermes个股分析失败: %w", err)
	}
	var result aiConclusion
	if err := decodeJSONObject(response.Content, &result); err != nil {
		return fmt.Errorf("Hermes未返回有效个股分析JSON: %w", err)
	}
	if strings.TrimSpace(result.Headline) == "" || strings.TrimSpace(result.Summary) == "" || strings.TrimSpace(result.Action) == "" {
		return errors.New("Hermes个股分析缺少必要字段")
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
		return "Hermes已综合市场、题材、资金、基本面、研报与风险生成价格计划"
	}
	plan.PricingSource = "local-rules"
	analysis.RiskControl = alignRiskControlWithActionPlan(analysis.RiskControl, *plan)
	return "Hermes已完成全局研判；AI价格未通过一致性校验，当前保留本地候选价格"
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

func decodeJSONObject(content string, target any) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("empty response")
	}
	if strings.HasPrefix(content, "```") {
		content = strings.TrimSpace(strings.TrimPrefix(content, "```json"))
		content = strings.TrimSpace(strings.TrimPrefix(content, "```"))
		content = strings.TrimSpace(strings.TrimSuffix(content, "```"))
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return errors.New("JSON object not found")
	}
	decoder := json.NewDecoder(strings.NewReader(content[start : end+1]))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}
