package portfolioinspection

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

type compactHoldingAnalysis struct {
	Symbol          string   `json:"symbol"`
	Name            string   `json:"name"`
	Weight          int      `json:"weight_percent"`
	CostPrice       *float64 `json:"cost_price,omitempty"`
	GeneratedAt     string   `json:"generated_at"`
	CurrentPrice    float64  `json:"current_price"`
	StockType       string   `json:"stock_type"`
	PricePhase      string   `json:"price_phase"`
	MarketRole      string   `json:"market_role"`
	OverallScore    int      `json:"overall_score"`
	Direction       string   `json:"direction"`
	TrendScore      int      `json:"trend_score"`
	RiskScore       int      `json:"risk_score"`
	RiskLevel       string   `json:"risk_level"`
	Theme           string   `json:"theme"`
	ThemeScore      int      `json:"theme_score"`
	RelativeScore   int      `json:"relative_score"`
	ShortTermState  string   `json:"short_term_state"`
	DecisionMode    string   `json:"decision_mode"`
	CurrentAction   string   `json:"current_action"`
	Horizon         string   `json:"horizon"`
	StopPrice       float64  `json:"stop_price"`
	Conclusion      string   `json:"conclusion"`
	MainRisk        string   `json:"main_risk"`
	Confirmation    string   `json:"confirmation"`
	Invalidation    string   `json:"invalidation"`
	PositiveSignals []string `json:"positive_signals"`
	NegativeSignals []string `json:"negative_signals"`
	DataGaps        []string `json:"data_gaps"`
}

func buildPrompt(request Request, results []HoldingResult, metrics Metrics, rules ProfileRules) (string, error) {
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
	payload := map[string]any{
		"prompt_version": PromptVersion, "trader_profile": rules, "portfolio_metrics": metrics,
		"holdings": stocks, "failed_holdings": failed, "cash_percent": 100 - metrics.TotalPositionPercent,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	prompt := `你是 easy-stock 的 A 股持仓风险巡检器。输入包含用户交易风格、真实持仓权重、程序计算的组合指标，以及每只股票已经完成的结构化个股分析。

必须遵守：
1. 只使用输入事实，不补充模型记忆中的行情、公告、新闻或持仓信息。
2. 这是组合分析，不要简单重复每只股票的个股报告。
3. 同时检查单票集中、同题材集中、相关性、短线暴露、风险贡献和现金缓冲。
4. 仓位越大，对组合结论和调整优先级的影响必须越大。
5. 交易风格是风险约束，不得因为用户选择“激进”就忽略止损和组合风险。
6. 区分“个股本身较弱”和“个股尚可但组合中过度集中”。
7. 所有动作必须是条件化建议，不承诺收益，不给出确定性价格预测。
8. 个股数据缺失或过期时必须降低置信度，并列出缺口。
9. 若有效分析覆盖仓位不足70%，不得给出完整调仓方案。
10. risk_contribution必须使用输入中的确定性风险贡献比例，不得自行重算。
11. 严格输出单个JSON对象，不输出Markdown或额外说明。

输出格式：
{"health_score":0,"risk_level":"低|中|高|极高","style_match":"匹配|部分偏离|明显偏离","executive_summary":"80至180字","primary_risks":["最多8条"],"concentration_findings":["最多8条"],"holdings":[{"symbol":"","portfolio_role":"核心|进攻|防守|观察|风险拖累","risk_contribution":0,"conclusion":"","action_priority":"观察|保持|优先处理","action":"条件化动作","confirmation":"确认条件","invalidation":"失效条件"}],"adjustment_order":["最多10条"],"scenarios":[{"name":"市场增强|震荡分化|风险退潮","condition":"","portfolio_action":""}],"next_checklist":["最多10条"],"data_limitations":["最多10条"],"confidence":0.0}

[结构化持仓分析JSON]
` + string(encoded)
	return prompt, nil
}

func localReport(request Request, results []HoldingResult, metrics Metrics, rules ProfileRules) AIReport {
	health := int(math.Round(float64(metrics.StyleMatchScore)*.45 + metrics.WeightedScore*.35 + (100-metrics.WeightedRisk)*.2))
	health = max(0, min(100, health))
	riskLevel := "低"
	if metrics.WeightedRisk >= 75 || metrics.StopLossRiskPercent > rules.MaxStopLossRisk*1.5 {
		riskLevel = "极高"
	} else if metrics.WeightedRisk >= 60 || metrics.StopLossRiskPercent > rules.MaxStopLossRisk {
		riskLevel = "高"
	} else if metrics.WeightedRisk >= 40 {
		riskLevel = "中"
	}
	styleMatch := "匹配"
	if metrics.StyleMatchScore < 55 {
		styleMatch = "明显偏离"
	} else if metrics.StyleMatchScore < 80 {
		styleMatch = "部分偏离"
	}
	primaryRisks := append([]string(nil), metrics.StyleBreaches...)
	concentration := make([]string, 0)
	for _, theme := range metrics.ThemeExposures {
		if theme.Symbols > 1 && theme.Weight >= 30 {
			concentration = append(concentration, fmt.Sprintf("%s题材合计占仓%d%%，存在同向波动风险", theme.Theme, theme.Weight))
		}
	}
	for _, pair := range metrics.HighCorrelations {
		concentration = append(concentration, fmt.Sprintf("%s与%s近阶段相关性%.2f", pair.LeftSymbol, pair.RightSymbol, pair.Correlation))
	}
	byContribution := map[string]float64{}
	for _, item := range metrics.RiskContributions {
		byContribution[item.Symbol] = item.Percent
	}
	holdings := make([]HoldingConclusion, 0, len(results))
	limitations := make([]string, 0)
	for _, result := range results {
		if result.Status != "succeeded" || result.Analysis == nil {
			limitations = append(limitations, result.Holding.Symbol+"："+firstNonEmpty(result.Error, "个股分析未完成"))
			continue
		}
		analysis := result.Analysis
		priority := "保持"
		role := "核心"
		if analysis.RiskControl.Score >= 70 || byContribution[result.Holding.Symbol] >= 35 {
			priority, role = "优先处理", "风险拖累"
		} else if analysis.Scorecard.Overall < 50 {
			priority, role = "观察", "观察"
		} else if analysis.ActionPlan.DecisionMode == "short_term" {
			role = "进攻"
		}
		holdings = append(holdings, HoldingConclusion{
			Symbol: analysis.Symbol, PortfolioRole: role, RiskContribution: byContribution[result.Holding.Symbol],
			Conclusion: analysis.Conclusion.Summary, ActionPriority: priority, Action: analysis.ActionPlan.CurrentAction,
			Confirmation: analysis.Conclusion.BestPath, Invalidation: analysis.ActionPlan.Invalidation,
		})
	}
	sort.Slice(holdings, func(i, j int) bool { return holdings[i].RiskContribution > holdings[j].RiskContribution })
	adjustment := make([]string, 0, len(holdings))
	for _, item := range holdings {
		if item.ActionPriority == "优先处理" {
			adjustment = append(adjustment, item.Symbol+"："+firstNonEmpty(item.Action, item.Conclusion))
		}
	}
	if len(adjustment) == 0 {
		adjustment = append(adjustment, "暂无必须立即处理的单一持仓，按各股确认与失效条件继续观察")
	}
	summary := fmt.Sprintf("当前持仓%d只、总仓位%d%%、现金%d%%，个股分析覆盖%.1f%%。组合风险为%s，交易风格匹配度%d分（%s）。优先关注高风险贡献、同题材集中和失效条件，不把单一个股结论直接等同于组合动作。", len(request.Holdings), metrics.TotalPositionPercent, metrics.CashPercent, metrics.CoveragePercent, riskLevel, metrics.StyleMatchScore, styleMatch)
	return AIReport{
		HealthScore: health, RiskLevel: riskLevel, StyleMatch: styleMatch, ExecutiveSummary: summary,
		PrimaryRisks: limitStrings(primaryRisks, 8), ConcentrationFinding: limitStrings(concentration, 8), Holdings: holdings,
		AdjustmentOrder: limitStrings(adjustment, 10), Scenarios: []Scenario{
			{Name: "市场增强", Condition: "市场与主要持仓题材同步增强", PortfolioAction: "保留强势核心，新增仓位仍需满足个股确认条件"},
			{Name: "震荡分化", Condition: "指数震荡且持仓表现分化", PortfolioAction: "降低高相关和弱势持仓暴露，避免在同一题材内重复加仓"},
			{Name: "风险退潮", Condition: "市场情绪走弱或核心持仓触发失效", PortfolioAction: "优先执行既定止损与降仓纪律，提高现金缓冲"},
		},
		NextChecklist:   []string{"核对高风险贡献持仓是否触发失效条件", "检查同题材个股是否同时走弱", "复核现金仓位是否符合交易风格"},
		DataLimitations: limitStrings(limitations, 10), Confidence: round(metrics.CoveragePercent/100*.75, 2), Source: "local-rules",
	}
}
