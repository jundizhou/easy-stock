package portfolioinspection

import "strings"

// Shared by inspection and the independent optimization review. Trading limits
// remain separate: these judgments concern the stock sleeve, regardless of cash.
const ScoringPolicy = `评分对象是股票组合本身：满仓、现金为0、总仓位高低均不加扣分，也不据此降低风格匹配或风险评价。同一股票配比仅改变总仓位，四维分不变。股票与风险组按股票合计=100%归一化，总资产仓位仅作执行约束。
四维0至100整数：holding_logic持仓逻辑35%仅评盈利质量、增长兑现、估值/价格透支、催化与经营反证；portfolio_structure组合结构25%独占单票/前三集中、共同驱动、相关性与互补；risk_capacity风险管理25%仅评股票自身波动回撤、流动性、跳空及退出可执行性；strategy_fit策略匹配15%仅评研究周期、持仓角色、交易方式与所选风格。集中只扣结构，经营反证只扣逻辑；同一问题只归一维，其余仅说明。
统一标尺：90-100突出且依据充分；80-89较好；70-79整体合理、风险可管理；60-69基本可用但有明确缺陷；40-59存在重大缺陷；0-39严重不足。70不要求无缺点也非保底；按事实选档定位，不为目标倒推、不按引用数量扣分。
锚点：逻辑70=主要持仓经营理由成立、无主导性反证，80=增长/盈利兑现且价格合理，60=部分重要持仓逻辑薄弱；结构70=集中可解释且有互补，80=集中受控且共振较低，60=明显集中或共同驱动；风险70=波动可承受且退出明确，80=波动/流动性较好且退出可执行，60=明显波动或退出缺陷；策略70=周期/角色/交易方式基本一致，80=一致且分工清晰，60=有明确错配。
缺正文、未知估值、覆盖/时效不足降低置信度并列核验，不自动扣投资分；已证实亏损、债务、价格透支、交易障碍按所属维度评价。未知不假定优质或经营差。无静态止损≠无语义退出；待入场限制执行≠逻辑失败。分数非胜率或收益预测。
`

func EquityPercent(weight, total int) float64 {
	if total <= 0 {
		return 0
	}
	return round(float64(weight)*100/float64(total), 4)
}

func ScoringProfile(r ProfileRules) map[string]any {
	description := map[TraderProfile]string{
		ProfileAggressive: "关注趋势弹性与主动交易，接受较高波动",
		ProfileBalanced:   "兼顾增长与稳定，持仓角色互补",
		ProfileSteady:     "本金保护与趋势确认优先，关注经营稳定和退出可执行性",
	}[r.ID]
	return map[string]any{"id": r.ID, "label": r.Label, "scoring_description": description,
		"equity_max_single_percent": r.MaxSinglePercent, "equity_max_top_three_percent": r.MaxTopThreePercent,
		"preferred_short_term_max_percent": r.PreferredShortTermMax}
}

// Preserve raw account facts for reporting/trade validation; add explicitly
// named stock-relative facts so citations never silently change their meaning.
func addEquityScoringFacts(facts map[string]Fact, request Request, results []HoldingResult, metrics Metrics) {
	add := func(key string, value any, available bool, method string) {
		if !available {
			value = nil
		}
		facts[key] = Fact{Value: value, Available: available, Method: method, Limitation: "仅评价股票组合，不按总资产仓位或现金比例加扣分"}
	}
	total := metrics.TotalPositionPercent
	add("equity_max_single_percent", EquityPercent(metrics.MaxSinglePercent, total), total > 0, "最大单票占股票持仓比例%")
	add("equity_top_three_percent", EquityPercent(metrics.TopThreePercent, total), total > 0, "前三大占股票持仓比例%")
	stop, stopAvailable := 0.0, false
	if total > 0 {
		for _, r := range results {
			if r.Status != "succeeded" || r.Analysis == nil {
				continue
			}
			price, _ := holdingPrice(r)
			limit := r.Analysis.RiskControl.StopPrice
			if price > 0 && limit > 0 && limit < price {
				stop += float64(r.Holding.Weight) / float64(total) * 100 * (price - limit) / price
				stopAvailable = true
			}
		}
	}
	add("equity_known_stop_loss_risk_percent", round(stop, 4), stopAvailable, "已知静态止损损失占股票持仓比例%，不含未知部分、跳空和滑点")
	for _, h := range request.Holdings {
		add(h.Symbol+".equity_weight_percent", EquityPercent(h.Weight, total), total > 0, "个股占股票持仓比例%")
	}
	rules, known := RulesFor(request.TraderProfile)
	for key, value := range ScoringProfile(rules) {
		if key == "id" || key == "label" || key == "preferred_short_term_max_percent" {
			continue
		}
		method := "评分风格参考；集中度按股票内部比例比较，仅用于结构维度"
		if key == "scoring_description" {
			method = "评分风格定义；策略维度评价周期、角色和交易方式"
		}
		add("profile."+key, value, known, method)
	}
}

func ExcludedScoringFact(key string) bool {
	switch key {
	case "cash_percent", "total_position_percent", "max_single_percent", "top_three_percent", "known_stop_loss_risk_percent", "max_high_risk_percent",
		"profile.minimum_cash_percent", "profile.description", "profile.max_single_percent", "profile.max_top_three_percent", "profile.max_high_risk_percent", "profile.max_stop_loss_risk_percent":
		return true
	}
	return strings.HasSuffix(key, ".weight_percent") || strings.HasPrefix(key, "risk_exposures.")
}

func ScoringFacts(facts map[string]Fact) map[string]Fact {
	filtered := make(map[string]Fact, len(facts))
	for key, f := range facts {
		if !ExcludedScoringFact(key) {
			filtered[key] = f
		}
	}
	return filtered
}
