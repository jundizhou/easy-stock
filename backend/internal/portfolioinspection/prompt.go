package portfolioinspection

import (
	"fmt"
	"sort"

	"easy-stock/backend/internal/stockanalysis"
)

type compactHoldingAnalysis struct {
	Symbol          string                           `json:"symbol"`
	Name            string                           `json:"name"`
	Weight          int                              `json:"weight_percent"`
	CostPrice       *float64                         `json:"cost_price,omitempty"`
	GeneratedAt     string                           `json:"generated_at"`
	CurrentPrice    float64                          `json:"current_price"`
	StockType       string                           `json:"stock_type"`
	PricePhase      string                           `json:"price_phase"`
	MarketRole      string                           `json:"market_role"`
	OverallScore    int                              `json:"overall_score"`
	Direction       string                           `json:"direction"`
	TrendScore      int                              `json:"trend_score"`
	RiskScore       int                              `json:"risk_score"`
	RiskLevel       string                           `json:"risk_level"`
	Theme           string                           `json:"theme"`
	ThemeScore      int                              `json:"theme_score"`
	RelativeScore   int                              `json:"relative_score"`
	ShortTermState  string                           `json:"short_term_state"`
	DecisionMode    string                           `json:"decision_mode"`
	CurrentAction   string                           `json:"current_action"`
	Horizon         string                           `json:"horizon"`
	StopPrice       float64                          `json:"stop_price"`
	Conclusion      string                           `json:"conclusion"`
	MainRisk        string                           `json:"main_risk"`
	Confirmation    string                           `json:"confirmation"`
	Invalidation    string                           `json:"invalidation"`
	PositiveSignals []string                         `json:"positive_signals"`
	NegativeSignals []string                         `json:"negative_signals"`
	DataGaps        []string                         `json:"data_gaps"`
	Research        *stockanalysis.ResearchSynthesis `json:"ai_research,omitempty"`
	AIStatus        string                           `json:"ai_status"`
	AnalysisID      string                           `json:"analysis_id,omitempty"`
}

func compactResearch(analysis *stockanalysis.Analysis) *stockanalysis.ResearchSynthesis {
	if analysis.ResearchReport == nil {
		return nil
	}
	return &analysis.ResearchReport.ResearchSynthesis
}

func buildPrompt(request Request, results []HoldingResult, metrics Metrics, rules ProfileRules) (string, error) {
	return buildScoringPrompt(request, results, metrics, rules)
}

func localReport(request Request, results []HoldingResult, metrics Metrics, rules ProfileRules) AIReport {
	health := metrics.HealthScore
	riskLevel := riskLevelForMetrics(metrics, rules)
	styleMatch := styleMatchLabel(metrics.StyleMatchScore)
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
		if analysis.RiskControl.Score >= 80 || byContribution[result.Holding.Symbol] >= 40 {
			priority, role = "优先处理", "风险拖累"
		} else if analysis.Scorecard.Overall < 45 {
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

func riskLevelForMetrics(metrics Metrics, rules ProfileRules) string {
	if extremePortfolioRisk(metrics, rules) {
		return "极高"
	}
	if metrics.WeightedRisk >= 70 || metrics.StopLossRiskPercent > rules.MaxStopLossRisk*1.5 {
		return "高"
	}
	if metrics.WeightedRisk >= 50 || metrics.StopLossRiskPercent > rules.MaxStopLossRisk+1 {
		return "中"
	}
	return "低"
}

func styleMatchLabel(score int) string {
	if score < 50 {
		return "明显偏离"
	}
	if score < 75 {
		return "部分偏离"
	}
	return "匹配"
}
