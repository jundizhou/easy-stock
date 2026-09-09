package stockanalysis

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func validateResearch(result *ResearchSynthesis, snapshot ResearchSnapshot) ([]string, error) {
	sources := map[string]ResearchSource{}
	anchors := map[string]PriceAnchor{}
	for _, source := range snapshot.Sources {
		sources[source.ID] = source
	}
	for _, anchor := range snapshot.Anchors {
		anchors[anchor.ID] = anchor
	}
	notes := []string{"已校验来源编号、引文和数字结构；语义解释仍属于AI判断，并非完整事实认证"}
	validateClaim := func(claim *ResearchClaim) error {
		claim.Text = truncateExactText(claim.Text, 350)
		if strings.TrimSpace(claim.Text) == "" {
			return fmt.Errorf("claim缺少text")
		}
		claim.SourceIDs = uniqueStrings(claim.SourceIDs, 6)
		if len(claim.SourceIDs) == 0 {
			return fmt.Errorf("claim缺少来源：%s", claim.Text)
		}
		switch claim.Kind {
		case "fact", "opinion", "inference":
		default:
			claim.Kind = "inference"
		}
		quoteMatches := claim.Quote == ""
		for _, id := range claim.SourceIDs {
			source, ok := sources[id]
			if !ok {
				return fmt.Errorf("不存在的来源编号：%s", id)
			}
			if claim.Quote != "" && strings.Contains(source.Content, claim.Quote) {
				quoteMatches = true
			}
			if claim.Kind == "fact" && (source.Kind == "opinion" || source.Kind == "methodology" || source.Kind == "news") {
				claim.Kind = "inference"
				notes = append(notes, "第三方观点或新闻解释已降为研究推断")
			}
		}
		if !quoteMatches {
			return fmt.Errorf("引文未匹配原文：%s", claim.Quote)
		}
		if containsAnyFold(claim.Text, "保证收益", "稳赚", "必然涨停", "必涨", "无风险套利") {
			return fmt.Errorf("不允许确定性收益承诺")
		}
		return nil
	}
	if strings.TrimSpace(result.Headline) == "" {
		return notes, fmt.Errorf("缺少headline")
	}
	if err := validateClaim(&result.Thesis); err != nil {
		return notes, err
	}
	for _, claims := range []*[]ResearchClaim{&result.Support, &result.Counter, &result.Alternatives} {
		if len(*claims) > 5 {
			*claims = (*claims)[:5]
		}
		for i := range *claims {
			if err := validateClaim(&(*claims)[i]); err != nil {
				return notes, err
			}
		}
		if *claims == nil {
			*claims = []ResearchClaim{}
		}
	}
	result.Headline = truncateExactText(result.Headline, 80)
	result.MainConflict = truncateExactText(result.MainConflict, 260)
	result.BaselineReason = truncateExactText(result.BaselineReason, 260)
	switch result.BaselineRelation {
	case "agree", "disagree", "insufficient":
	default:
		result.BaselineRelation = "insufficient"
	}
	switch result.EvidenceLevel {
	case "sufficient", "limited", "insufficient":
	default:
		result.EvidenceLevel = "limited"
	}
	if len(result.Support) < 2 || len(snapshot.DailyBars) < 20 {
		result.EvidenceLevel = "insufficient"
	}
	if result.EvidenceLevel == "sufficient" {
		coverage := map[string]bool{}
		for _, claim := range append(append([]ResearchClaim{result.Thesis}, result.Support...), result.Counter...) {
			for _, id := range claim.SourceIDs {
				coverage[sources[id].Kind] = true
			}
		}
		if !coverage["announcement"] || (!coverage["disclosure"] && !coverage["company_profile"]) {
			result.EvidenceLevel = "limited"
			notes = append(notes, "公司披露与业务证据覆盖不完整，证据充分度不标为充分")
		}
	}
	if len(result.Counter) == 0 {
		result.Limitations = append(result.Limitations, "尚未取得直接反证，不表示不存在反对理由")
	}
	result.Limitations = uniqueStrings(append(append([]string{}, snapshot.Limitations...), result.Limitations...), 24)
	conditionIDs := map[string]bool{}
	if len(result.Conditions) > 6 {
		result.Conditions = result.Conditions[:6]
	}
	for i := range result.Conditions {
		condition := &result.Conditions[i]
		if condition.ID == "" || conditionIDs[condition.ID] {
			return notes, fmt.Errorf("condition编号缺失或重复")
		}
		conditionIDs[condition.ID] = true
		condition.Status = "pending"
		condition.Text = truncateExactText(condition.Text, 220)
		for _, id := range condition.SourceIDs {
			if _, ok := sources[id]; !ok {
				return notes, fmt.Errorf("条件引用了未知来源%s", id)
			}
		}
		if condition.Text == "" {
			return notes, fmt.Errorf("条件缺少观察描述")
		}
		switch condition.Window {
		case "next_close", "next_5_sessions", "next_disclosure":
		default:
			condition.Window = "next_5_sessions"
		}
		switch condition.Metric {
		case "close":
			anchor, ok := anchors[condition.AnchorID]
			if !ok {
				return notes, fmt.Errorf("价格条件引用了未知anchor_id %s", condition.AnchorID)
			}
			value := anchor.Price
			condition.Threshold = &value
			condition.SourceIDs = uniqueStrings(append(condition.SourceIDs, anchor.SourceID), 6)
			if condition.Operator != "gte" && condition.Operator != "lte" {
				return notes, fmt.Errorf("价格条件只允许gte/lte")
			}
			comparison := "不低于"
			if condition.Operator == "lte" {
				comparison = "不高于"
			}
			condition.Text = fmt.Sprintf("收盘价%s%.2f元（%s）", comparison, value, anchor.Label)
			if condition.Window == "next_disclosure" {
				return notes, fmt.Errorf("价格条件必须指定交易日窗口")
			}
		case "volume_ratio":
			if condition.Threshold == nil || !finite(*condition.Threshold) || *condition.Threshold < .2 || *condition.Threshold > 10 {
				return notes, fmt.Errorf("量比条件阈值不合法")
			}
			if condition.Operator != "gte" && condition.Operator != "lte" {
				return notes, fmt.Errorf("量比条件只允许gte/lte")
			}
			condition.SourceIDs = uniqueStrings(append(condition.SourceIDs, "m-price"), 6)
			if condition.Window == "next_disclosure" {
				return notes, fmt.Errorf("成交量条件必须指定交易日窗口")
			}
			notes = append(notes, "量比阈值为研究假设，并非已回测交易规则")
		case "disclosure", "auction", "opening":
			condition.Threshold = nil
			condition.AnchorID = ""
			condition.Operator = "confirmed"
		default:
			return notes, fmt.Errorf("不支持的观察指标%s", condition.Metric)
		}
	}
	if result.Conditions == nil {
		result.Conditions = []ResearchCondition{}
	}
	result.InvalidationIDs = uniqueStrings(result.InvalidationIDs, 6)
	for _, id := range result.InvalidationIDs {
		if !conditionIDs[id] {
			return notes, fmt.Errorf("失效条件编号不存在：%s", id)
		}
	}
	keys := map[string]bool{}
	for i := range result.Scenarios {
		scenario := &result.Scenarios[i]
		if (scenario.Key != "strong" && scenario.Key != "base" && scenario.Key != "weak") || keys[scenario.Key] {
			return notes, fmt.Errorf("情景key重复或无效")
		}
		keys[scenario.Key] = true
		for _, id := range scenario.ConditionIDs {
			if !conditionIDs[id] {
				return notes, fmt.Errorf("情景引用未知条件%s", id)
			}
		}
		scenario.Description = truncateExactText(scenario.Description, 260)
		scenario.Response = truncateExactText(scenario.Response, 220)
	}
	if result.Scenarios == nil {
		result.Scenarios = []ResearchScenario{}
	}
	decision := &result.Decision
	originalStatus := decision.Status
	switch decision.Status {
	case "observe", "conditional", "no_plan":
	default:
		decision.Status = "no_plan"
	}
	if decision.Mode != "short_term" {
		decision.Mode = "non_short"
	}
	if result.EvidenceLevel == "insufficient" || len(result.InvalidationIDs) == 0 {
		decision.Status = "no_plan"
		notes = append(notes, "证据不足或缺少失效条件，未形成交易计划")
	}
	if researchPricesStale(snapshot) {
		decision.Status = "no_plan"
		result.EvidenceLevel = "insufficient"
		notes = append(notes, "行情时效不足，不能据此形成当前交易计划")
	}
	if decision.NewPosition == "" || decision.ExistingPosition == "" || decision.Reason == "" {
		return notes, fmt.Errorf("必须分别说明新仓、已有仓位和计划依据")
	}
	decision.NewPosition = truncateExactText(decision.NewPosition, 260)
	decision.ExistingPosition = truncateExactText(decision.ExistingPosition, 260)
	decision.Reason = truncateExactText(decision.Reason, 300)
	if decision.Status != "conditional" || decision.Mode == "short_term" {
		decision.PricePlan = nil
	}
	if decision.PricePlan != nil {
		problem := validateAnchoredPlan(*decision.PricePlan, snapshot)
		if problem != "" {
			notes = append(notes, problem)
			decision.PricePlan = nil
			decision.Status = "no_plan"
			decision.Reason = "价格依据校验未通过：" + problem
			decision.NewPosition = "暂不形成新仓计划，等待补充依据"
			decision.ExistingPosition = "价格方案未通过校验，不沿用其持有或止损结论；需重新核实已有仓位风险"
		}
	}
	if decision.Status == "no_plan" {
		decision.PricePlan = nil
		if originalStatus != "no_plan" {
			decision.Reason = "证据、行情时效或失效条件不足，原条件化计划未通过校验"
			decision.ExistingPosition = "暂不沿用原计划的加仓、持有或止损判断；需要补齐资料后重新评估已有仓位风险"
		}
		decision.NewPosition = "暂不形成新仓计划；" + decision.Reason
	}
	return uniqueStrings(notes, 12), nil
}

func researchPricesStale(snapshot ResearchSnapshot) bool {
	if len(snapshot.DailyBars) == 0 {
		return true
	}
	day, err := time.Parse("2006-01-02", snapshot.DailyBars[len(snapshot.DailyBars)-1].Date)
	return err != nil || snapshot.CutoffAt.Sub(day) > 5*24*time.Hour || day.After(snapshot.CutoffAt)
}

// A quantitative snapshot is useful while AI runs, but is not a completed plan.
func QuantitativeOnly(analysis Analysis) Analysis {
	analysis.ResearchReport = nil
	analysis.Conclusion = Conclusion{Headline: "量化快照 · 尚无AI研究结论", Summary: "已采集历史量价与公司资料；规则评分仅作为研究基线，不构成已完成的AI判断", Action: "等待研究完成或补齐缺失资料", Source: "quantitative-baseline"}
	analysis.ActionPlan = ActionPlan{DecisionMode: "non_short", DecisionLabel: "仅量化快照", PricingSource: "none", CurrentAction: "尚未形成经过证据校验的交易计划"}
	risk := &analysis.RiskControl
	risk.EntryReference, risk.StopPrice, risk.StopPercent = 0, 0, 0
	risk.TakeProfitFirst, risk.TakeProfitSecond, risk.RiskReward = 0, 0, 0
	risk.ExistingPositionStopPrice, risk.ExistingPositionStopPercent = 0, 0
	risk.SuggestedPositionMin, risk.SuggestedPositionMax, risk.SingleTradeRisk = 0, 0, 0
	risk.Rules = []string{"AI研究未完成，不生成静态交易价格或股数"}
	analysis.NextDay = NextDayPlan{Bias: "尚未研究", Expectation: "尚无经过证据核对的未来情景", Scenarios: []NextDayScenario{}, Levels: []PriceLevel{}}
	return analysis
}

func validateAnchoredPlan(plan AnchoredPricePlan, snapshot ResearchSnapshot) string {
	if len(snapshot.DailyBars) < 20 {
		return "历史样本不足20日"
	}
	last := snapshot.DailyBars[len(snapshot.DailyBars)-1]
	day, err := time.Parse("2006-01-02", last.Date)
	if err != nil || snapshot.CutoffAt.Sub(day) > 5*24*time.Hour {
		return "日线时效不足"
	}
	anchors := map[string]PriceAnchor{}
	for _, anchor := range snapshot.Anchors {
		anchors[anchor.ID] = anchor
	}
	entry, ok := anchors[plan.EntryAnchor]
	if !ok {
		return "介入参考位不存在"
	}
	stop, ok := anchors[plan.StopAnchor]
	if !ok {
		return "失效参考位不存在"
	}
	if entry.Price <= stop.Price || (entry.Price-stop.Price)/entry.Price > .2 {
		return "参考位顺序错误或计划止损距离超过20%"
	}
	current := firstPositive(snapshot.Quote.Price, last.Close)
	if current <= 0 || math.Abs(entry.Price/current-1) > .3 {
		return "参考价偏离当前价格超过30%"
	}
	if plan.TargetAnchor != "" {
		target, ok := anchors[plan.TargetAnchor]
		if !ok || target.Price <= entry.Price || target.Price <= current {
			return "目标缺少有效上方空间，不自动抬高目标"
		}
	}
	if strings.TrimSpace(plan.Reason) == "" || len(plan.SourceIDs) == 0 {
		return "价格计划缺少依据与来源"
	}
	sources := map[string]bool{}
	for _, source := range snapshot.Sources {
		sources[source.ID] = true
	}
	for _, id := range plan.SourceIDs {
		if !sources[id] {
			return "价格计划引用未知来源"
		}
	}
	return ""
}

func ApplyResearch(analysis *Analysis, report *ResearchReport, snapshot ResearchSnapshot) {
	analysis.ResearchReport = report
	analysis.SnapshotID = snapshot.ID
	analysis.Scorecard = snapshot.Baseline
	decision := report.Decision
	mainRisk := "仍有未取得或未验证的风险资料"
	if len(report.Counter) > 0 {
		mainRisk = report.Counter[0].Text
	}
	conditions := map[string]ResearchCondition{}
	checks := []string{}
	for _, c := range report.Conditions {
		conditions[c.ID] = c
		checks = append(checks, c.Text)
	}
	invalidations := []string{}
	for _, id := range report.InvalidationIDs {
		invalidations = append(invalidations, conditions[id].Text)
	}
	if len(invalidations) > 0 {
		mainRisk = invalidations[0]
	}
	currentAction := decision.NewPosition
	if report.Request.Purpose == "holding" {
		currentAction = decision.ExistingPosition
	}
	analysis.Conclusion = Conclusion{Headline: report.Headline, Summary: report.Thesis.Text, Action: currentAction, BestPath: strings.Join(checks, "；"), MainRisk: mainRisk, Source: "hermes-research"}
	analysis.Risks = uniqueStrings(append(invalidations, claimTexts(report.Counter)...), 8)
	if len(analysis.Risks) == 0 {
		analysis.Risks = []string{"证据未排除潜在风险，不能把缺少反证当作安全"}
	}
	label := "条件化研究"
	if decision.Status == "no_plan" {
		label = "暂不形成交易计划"
	} else if decision.Status == "observe" {
		label = "观察与核实"
	}
	analysis.ActionPlan = ActionPlan{DecisionMode: decision.Mode, DecisionLabel: label, Horizon: decision.Horizon, Rationale: decision.Reason, PricingSource: "none", CurrentAction: currentAction,
		EntryConditions: checks, HoldConditions: []string{decision.ExistingPosition}, AvoidConditions: invalidations, Invalidation: strings.Join(invalidations, "；"), PositionHint: "仅为研究条件，未指定账户仓位"}
	risk := analysis.RiskControl
	maxPosition, maxTradeRisk := risk.SuggestedPositionMax, risk.SingleTradeRisk
	risk.EntryReference = 0
	risk.StopPrice = 0
	risk.StopPercent = 0
	risk.TakeProfitFirst = 0
	risk.TakeProfitSecond = 0
	risk.RiskReward = 0
	risk.ExistingPositionStopPrice = 0
	risk.ExistingPositionStopPercent = 0
	risk.SuggestedPositionMin = 0
	risk.SuggestedPositionMax = 0
	risk.SingleTradeRisk = 0
	risk.Rules = []string{"没有经过校验的静态价格计划时，不计算买入股数或隐含目标"}
	if plan := decision.PricePlan; plan != nil {
		anchors := map[string]PriceAnchor{}
		for _, a := range snapshot.Anchors {
			anchors[a.ID] = a
		}
		entry, stop := anchors[plan.EntryAnchor], anchors[plan.StopAnchor]
		analysis.ActionPlan.PricingSource = "validated-anchors"
		analysis.ActionPlan.Entry = ActionPriceZone{Label: "条件介入参考", PriceLow: entry.Price, PriceHigh: entry.Price, PriceText: formatActionPriceRange(entry.Price, entry.Price), Reason: plan.Reason, Action: decision.NewPosition}
		analysis.ActionPlan.StopLoss = ActionPriceZone{Label: "结构失效参考", PriceHigh: stop.Price, PriceText: fmt.Sprintf("≤ %.2f 元", stop.Price), Reason: stop.Label, Action: strings.Join(invalidations, "；")}
		analysis.ActionPlan.Hold = ActionPriceZone{Label: "已有仓位观察", PriceLow: stop.Price, PriceHigh: entry.Price, PriceText: fmt.Sprintf("高于 %.2f 元仍需核实其他条件", stop.Price), Reason: decision.ExistingPosition, Action: decision.ExistingPosition}
		risk.EntryReference = entry.Price
		risk.StopPrice = stop.Price
		risk.StopPercent = round2((entry.Price - stop.Price) / entry.Price * 100)
		if plan.TargetAnchor != "" {
			target := anchors[plan.TargetAnchor]
			analysis.ActionPlan.TakeProfit = ActionPriceZone{Label: "结构压力参考", PriceLow: target.Price, PriceHigh: target.Price, PriceText: formatActionPriceRange(target.Price, target.Price), Reason: target.Label, Action: "到达后重新评估，不等同于预测必然达到"}
			risk.TakeProfitFirst = target.Price
			risk.TakeProfitSecond = target.Price
			risk.RiskReward = round2((target.Price - entry.Price) / (entry.Price - stop.Price))
		}
		risk = finalizeRiskControl(risk, analysis.Profile, analysis.Trend, analysis.ShortTerm, analysis.Market)
		risk.SuggestedPositionMax = min(risk.SuggestedPositionMax, maxPosition)
		risk.SuggestedPositionMin = min(risk.SuggestedPositionMin, risk.SuggestedPositionMax)
		risk.SingleTradeRisk = math.Min(risk.SingleTradeRisk, maxTradeRisk)
	}
	analysis.RiskControl = risk
	analysis.NextDay = NextDayPlan{Bias: "条件情景", Expectation: report.Thesis.Text, Levels: []PriceLevel{}, Scenarios: []NextDayScenario{}, PreOpenChecks: checks, OpeningChecks: []string{}, IntradayChecks: []string{}, CloseChecks: invalidations}
	for _, scenario := range report.Scenarios {
		triggers := []string{}
		for _, id := range scenario.ConditionIDs {
			triggers = append(triggers, conditions[id].Text)
		}
		analysis.NextDay.Scenarios = append(analysis.NextDay.Scenarios, NextDayScenario{Key: scenario.Key, Name: scenario.Name, Priority: "条件情景，非概率预测", Trigger: scenario.Description, Confirmation: strings.Join(triggers, "；"), Action: scenario.Response, Invalidation: strings.Join(invalidations, "；")})
	}
	analysis.AI = AISynthesisStatus{Status: "ready", Model: report.Model, Message: "AI研究完成；来源结构已检查，语义与未来情景仍需验证"}
	analysis.GeneratedAt = report.GeneratedAt
}

func claimTexts(claims []ResearchClaim) []string {
	result := []string{}
	for _, claim := range claims {
		result = append(result, claim.Text)
	}
	return result
}
