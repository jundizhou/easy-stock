package portfolioinspection

import (
	"encoding/json"
	"fmt"
	"strings"

	"easy-stock/backend/internal/review"
)

func buildExpectationPrompt(summary review.DailySummary, request Request, results []HoldingResult, metrics Metrics, rules ProfileRules) (string, error) {
	stocks := make([]compactHoldingAnalysis, 0, len(results))
	failed := make([]string, 0)
	for _, result := range results {
		if result.Status != "succeeded" || result.Analysis == nil {
			failed = append(failed, result.Holding.Symbol+": "+firstNonEmpty(result.Error, "分析未完成"))
			continue
		}
		analysis := result.Analysis
		price := analysis.Quote.Price
		if price <= 0 {
			price = analysis.Trend.LatestClose
		}
		dataGaps := make([]string, 0)
		for _, item := range analysis.DataQuality {
			if item.Status != "ready" {
				dataGaps = append(dataGaps, item.Message)
			}
		}
		stocks = append(stocks, compactHoldingAnalysis{
			Symbol: analysis.Symbol, Name: analysis.Name, Weight: result.Holding.Weight, CostPrice: result.Holding.CostPrice,
			GeneratedAt: analysis.GeneratedAt.Format("2006-01-02 15:04:05"), CurrentPrice: price,
			StockType: analysis.Profile.TypeLabel, PricePhase: analysis.Profile.PricePhase, MarketRole: analysis.Profile.MarketRole,
			OverallScore: analysis.Scorecard.Overall, Direction: analysis.Scorecard.Direction, TrendScore: analysis.Trend.Score,
			RiskScore: analysis.RiskControl.Score, RiskLevel: analysis.RiskControl.Level, Theme: analysis.Theme.Primary,
			ThemeScore: analysis.Theme.HotScore, RelativeScore: analysis.Relative.Score, ShortTermState: analysis.ShortTerm.State,
			DecisionMode: analysis.ActionPlan.DecisionMode, CurrentAction: analysis.ActionPlan.CurrentAction, Horizon: analysis.ActionPlan.Horizon,
			StopPrice: analysis.RiskControl.StopPrice, Conclusion: analysis.Conclusion.Summary, MainRisk: analysis.Conclusion.MainRisk,
			Confirmation: analysis.Conclusion.BestPath, Invalidation: analysis.ActionPlan.Invalidation,
			PositiveSignals: limitStrings(analysis.Scorecard.PositiveSignals, 4), NegativeSignals: limitStrings(analysis.Scorecard.NegativeSignals, 4), DataGaps: limitStrings(dataGaps, 5),
		})
	}
	reviewPayload := map[string]any{
		"trade_date": summary.TradeDate, "generated_at": summary.GeneratedAt, "market_regime": summary.MarketRegime,
		"executive_summary": summary.ExecutiveSummary, "market_analysis": summary.MarketAnalysis, "market_framework": summary.MarketFramework,
		"consensus": summary.Consensus, "disagreements": summary.Disagreements, "scenarios": summary.Scenarios,
		"directions": summary.Directions, "tomorrow_focus": summary.TomorrowFocus, "tomorrow_outlook": summary.TomorrowOutlook,
		"tomorrow_playbook": summary.TomorrowPlaybook, "catalysts": summary.Catalysts, "risks": summary.Risks,
		"verification_checklist": summary.VerificationChecklist, "limitations": summary.Limitations,
	}
	payload := map[string]any{
		"prompt_version": ExpectationPromptVersion, "daily_review": reviewPayload, "trader_profile": rules,
		"portfolio_metrics": metrics, "holdings": stocks, "failed_holdings": failed, "cash_percent": 100 - metrics.TotalPositionPercent,
		"deterministic_summary": map[string]any{"health_score": metrics.HealthScore, "risk_level": riskLevelForMetrics(metrics, rules), "style_match": styleMatchLabel(metrics.StyleMatchScore)},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	prompt := `你是 easy-stock 商业版的 A 股持仓次日情景分析师和组合风险控制器。

你的任务不是预测明日涨跌，而是把“今日大 V 综合复盘”与“用户真实持仓”映射为可验证的次日情景、持仓影响和条件化行动计划。

证据优先级：
1. 程序计算的行情、仓位、成本、风险指标和确定性组合指标；
2. 每只股票刚刚生成的结构化个股分析；
3. 今日复盘中的作者共识、分歧、题材方向和次日剧本；
4. 模型推理只能连接上述证据，不得补充记忆中的行情、新闻、公告或价格。

必须遵守：
1. 明确区分程序事实、作者观点和你的条件化推断；作者观点只能作为情景证据。
2. 大 V 共识不能覆盖个股自身趋势、风险、数据质量和失效条件。
3. 输入中的每只持仓都必须出现在review_exposure、holdings以及每个scenario的holding_responses中。
4. 仓位越大，对组合结论、风险优先级和处理顺序影响越大。
5. 成本价仅用于描述浮盈浮亏处境和风险承受，不得产生“必须回本”的沉没成本偏差。
6. 交易风格是风险边界；激进不代表忽略止损、集中度和现金缓冲。
7. 禁止使用“必涨、必跌、确定反包”等确定性语言；禁止承诺收益。
8. 不凭空制造价格目标、支撑位、概率、新闻或公告。只能引用输入中已有的价格和条件。
9. 所有动作必须是条件化动作，并同时给出可观察的确认条件和失效条件。
10. 若复盘观点与个股结构冲突，alignment必须标为“背离”并解释两侧证据。
11. 数据缺失、分析过期、复盘覆盖不足或有效个股分析仓位低于70%时降低confidence并写入data_limitations。
12. strong、base、weak三个情景必须齐全，使用可观察指标区分，不输出伪精确概率。
13. opening_expectation描述竞价至开盘30分钟，intraday_expectation描述盘中确认，close_expectation描述收盘验证。
14. before_trigger必须说明触发前保持什么状态，避免用户把预案当成即时交易指令。
15. confidence范围0至1。严格输出单个JSON对象，不输出Markdown或额外说明。

输出格式：
{"headline":"80至180字","portfolio_bias":"有利|中性|承压","core_conflict":"组合最重要的机会与风险冲突","review_exposure":[{"symbol":"","alignment":"共振|部分共振|背离|无直接关联","review_evidence":["最多3条"],"structure_evidence":["最多3条"]}],"scenarios":[{"key":"strong|base|weak","name":"","market_triggers":["最多5条"],"portfolio_impact":"","total_position_response":"条件化组合响应","holding_responses":[{"symbol":"","expected_behavior":"","action":"条件化动作","confirmation":"确认条件","invalidation":"失效条件"}]}],"holdings":[{"symbol":"","priority":"优先观察|保持跟踪|优先控制风险","opening_expectation":"","intraday_expectation":"","close_expectation":"","cost_context":"","before_trigger":"","positive_trigger":"","negative_trigger":"","invalidation":""}],"timeline":{"pre_open":["最多8条"],"opening_30m":["最多8条"],"intraday":["最多8条"],"close":["最多8条"]},"risk_alerts":["最多10条"],"data_limitations":["最多10条"],"confidence":0.0}

[结构化输入JSON]
` + string(encoded)
	return prompt, nil
}

func localExpectationReport(summary review.DailySummary, request Request, results []HoldingResult, metrics Metrics) ExpectationConclusion {
	holdings := make([]ExpectationHolding, 0, len(results))
	exposures := make([]ReviewExposure, 0, len(results))
	for _, result := range results {
		item := ExpectationHolding{Symbol: result.Holding.Symbol, Priority: "保持跟踪", BeforeTrigger: "条件未确认前保持观察，不根据单一盘后观点行动"}
		exposure := ReviewExposure{Symbol: result.Holding.Symbol, Alignment: "无直接关联"}
		if result.Analysis == nil {
			item.Priority = "优先观察"
			item.OpeningExpectation = "个股分析未完成，开盘阶段不做方向推断"
			item.IntradayExpectation = "等待行情与结构数据恢复"
			item.CloseExpectation = "收盘后补充完整分析"
			exposure.StructureEvidence = []string{"个股结构化分析缺失"}
		} else {
			analysis := result.Analysis
			item.OpeningExpectation = firstNonEmpty(strings.Join(analysis.NextDay.OpeningChecks, "；"), analysis.NextDay.Expectation, analysis.Conclusion.Summary)
			item.IntradayExpectation = firstNonEmpty(strings.Join(analysis.NextDay.IntradayChecks, "；"), analysis.ActionPlan.CurrentAction)
			item.CloseExpectation = firstNonEmpty(strings.Join(analysis.NextDay.CloseChecks, "；"), analysis.Conclusion.BestPath)
			item.PositiveTrigger = analysis.Conclusion.BestPath
			item.NegativeTrigger = analysis.Conclusion.MainRisk
			item.Invalidation = analysis.ActionPlan.Invalidation
			exposure.StructureEvidence = limitStrings(append(analysis.Scorecard.PositiveSignals, analysis.Scorecard.NegativeSignals...), 3)
		}
		holdings = append(holdings, item)
		exposures = append(exposures, exposure)
	}
	limitations := []string{"AI综合研判暂不可用，当前结果由复盘基线与逐股规则降级生成"}
	if len(summary.Limitations) > 0 {
		limitations = append(limitations, summary.Limitations...)
	}
	return ExpectationConclusion{
		Headline:      fmt.Sprintf("今日复盘基线为“%s”。当前持仓%d只、总仓位%d%%；请以开盘确认、盘中结构和个股失效条件逐步核验，不把盘后观点直接转化为交易动作。", firstNonEmpty(summary.MarketRegime, "样本不足"), len(request.Holdings), metrics.TotalPositionPercent),
		PortfolioBias: "中性", CoreConflict: firstNonEmpty(summary.TomorrowOutlook, summary.ExecutiveSummary), ReviewExposure: exposures,
		Scenarios: localExpectationScenarios(summary, results),
		Holdings:  holdings, Timeline: ExpectationTimeline{PreOpen: limitStrings(summary.TomorrowPlaybook.PreOpen, 8), Opening30M: limitStrings(summary.TomorrowPlaybook.Opening, 8), Intraday: limitStrings(summary.TomorrowPlaybook.Intraday, 8), Close: limitStrings(summary.TomorrowPlaybook.Close, 8)},
		RiskAlerts: limitStrings(summary.Risks, 10), DataLimitations: limitStrings(limitations, 10), Confidence: 0.35, Source: "local-rules",
	}
}

func localExpectationScenarios(summary review.DailySummary, results []HoldingResult) []ExpectationScenario {
	reviewScenarios := make(map[string]review.DailyScenario, len(summary.Scenarios))
	for _, scenario := range summary.Scenarios {
		reviewScenarios[scenario.Key] = scenario
	}
	defaults := []ExpectationScenario{
		{Key: "strong", Name: "市场增强", PortfolioImpact: "风险偏好改善，但持仓仍需用自身结构确认", TotalPositionResponse: "仅在市场与持仓同步确认后评估提高风险暴露", MarketTriggers: []string{"成交与主线出现增强信号"}},
		{Key: "base", Name: "震荡分化", PortfolioImpact: "组合按原有定位运行，重点核对个股相对强弱", TotalPositionResponse: "触发条件出现前维持既定仓位与现金缓冲", MarketTriggers: []string{"市场延续当前复盘基线"}},
		{Key: "weak", Name: "风险退潮", PortfolioImpact: "系统性风险可能放大个股结构弱点", TotalPositionResponse: "市场和个股风险条件同时触发时优先控制组合风险", MarketTriggers: []string{"风险偏好与承接同步走弱"}},
	}
	for index := range defaults {
		scenario := &defaults[index]
		if source, ok := reviewScenarios[scenario.Key]; ok {
			scenario.Name = firstNonEmpty(source.Name, scenario.Name)
			scenario.PortfolioImpact = firstNonEmpty(source.Summary, scenario.PortfolioImpact)
			scenario.MarketTriggers = limitStrings([]string{source.Trigger, source.Confirmation}, 5)
			if len(scenario.MarketTriggers) == 0 {
				scenario.MarketTriggers = []string{"等待市场条件确认"}
			}
		}
		for _, result := range results {
			expectation := "个股分析缺失，等待行情与结构数据恢复"
			confirmation := "个股结构与市场情景同步确认"
			invalidation := "市场情景或个股结构不再成立"
			if result.Analysis != nil {
				expectation = firstNonEmpty(result.Analysis.NextDay.Expectation, result.Analysis.Conclusion.Summary, expectation)
				confirmation = firstNonEmpty(result.Analysis.Conclusion.BestPath, confirmation)
				invalidation = firstNonEmpty(result.Analysis.ActionPlan.Invalidation, result.Analysis.Conclusion.MainRisk, invalidation)
			}
			action := scenario.TotalPositionResponse
			scenario.HoldingResponses = append(scenario.HoldingResponses, ExpectationHoldingAction{
				Symbol: result.Holding.Symbol, ExpectedBehavior: expectation, Action: action,
				Confirmation: confirmation, Invalidation: invalidation,
			})
		}
	}
	return defaults
}

func normalizeExpectationConclusion(report *ExpectationConclusion, request Request) error {
	if strings.TrimSpace(report.Headline) == "" || strings.TrimSpace(report.PortfolioBias) == "" {
		return fmt.Errorf("持仓明日预期缺少必要结论")
	}
	report.Confidence = max(0, min(1, report.Confidence))
	report.RiskAlerts = limitStrings(report.RiskAlerts, 10)
	report.DataLimitations = limitStrings(report.DataLimitations, 10)
	report.Timeline.PreOpen = limitStrings(report.Timeline.PreOpen, 8)
	report.Timeline.Opening30M = limitStrings(report.Timeline.Opening30M, 8)
	report.Timeline.Intraday = limitStrings(report.Timeline.Intraday, 8)
	report.Timeline.Close = limitStrings(report.Timeline.Close, 8)
	if len(report.Holdings) > len(request.Holdings) {
		report.Holdings = report.Holdings[:len(request.Holdings)]
	}
	if len(report.ReviewExposure) > len(request.Holdings) {
		report.ReviewExposure = report.ReviewExposure[:len(request.Holdings)]
	}
	if len(report.Scenarios) > 3 {
		report.Scenarios = report.Scenarios[:3]
	}
	expected := make(map[string]struct{}, len(request.Holdings))
	for _, holding := range request.Holdings {
		expected[holding.Symbol] = struct{}{}
	}
	if err := requireExpectationSymbols("holdings", expectationHoldingSymbols(report.Holdings), expected); err != nil {
		return err
	}
	if err := requireExpectationSymbols("review_exposure", exposureSymbols(report.ReviewExposure), expected); err != nil {
		return err
	}
	scenarioKeys := map[string]bool{}
	for _, scenario := range report.Scenarios {
		scenarioKeys[scenario.Key] = true
		if err := requireExpectationSymbols("scenario "+scenario.Key, responseSymbols(scenario.HoldingResponses), expected); err != nil {
			return err
		}
	}
	for _, key := range []string{"strong", "base", "weak"} {
		if !scenarioKeys[key] {
			return fmt.Errorf("持仓明日预期缺少%s情景", key)
		}
	}
	return nil
}

func requireExpectationSymbols(section string, actual []string, expected map[string]struct{}) error {
	seen := map[string]struct{}{}
	for _, symbol := range actual {
		seen[strings.TrimSpace(symbol)] = struct{}{}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("持仓明日预期%s未覆盖全部持仓", section)
	}
	for symbol := range expected {
		if _, ok := seen[symbol]; !ok {
			return fmt.Errorf("持仓明日预期%s缺少%s", section, symbol)
		}
	}
	return nil
}

func expectationHoldingSymbols(items []ExpectationHolding) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Symbol)
	}
	return result
}
func exposureSymbols(items []ReviewExposure) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Symbol)
	}
	return result
}
func responseSymbols(items []ExpectationHoldingAction) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Symbol)
	}
	return result
}
