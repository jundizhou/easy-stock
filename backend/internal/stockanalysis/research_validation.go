package stockanalysis

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// [ES_PARTIAL_CORE_SAVE_PATCH] §4 core 段独立校验。
//
// validateResearch 是「core + 交易条件」的完整校验。§4 需要在不依赖交易条件的前提下
// 单独判断 core 是否可信，因此这里把 **core 作用域** 的校验抽成 validateResearchCore：
// 它执行与完整校验完全相同的 claim 级检查（来源编号存在性、基线禁引、逐字引文匹配、
// 事实/观点来源类型约束、确定性收益承诺禁语、headline 必填、证据充分度归一化），
// 但**不**触碰 conditions / scenarios / decision —— 那些属于交易段义务。
//
// 安全性：core 校验与完整校验共用同一套 claim 检查实现，不会因为「分段」而放松标准。
// 若 core 的来源或引文不合法，这里同样硬失败 → 调用方不得做部分保存（§4.5）。
func validateResearchCore(result *ResearchSynthesis, snapshot ResearchSnapshot) ([]string, error) {
	sources := researchSourceIndex(snapshot)
	notes := []string{"已校验来源编号、引文和数字结构；语义解释仍属于AI判断，并非完整事实认证"}
	if repaired := repairResearchSourceIDs(result, sources); len(repaired) > 0 {
		notes = append(notes, repaired...)
	}
	if softenUnsupportedAttribution(result, sources) {
		notes = append(notes, "未提供直接资金流或板块因果证据，已将题材/资金归因降为待验证假设")
	}
	return validateResearchCoreClaims(result, snapshot, sources, notes)
}

// researchSourceIndex 建立来源编号索引（core 与完整校验共用）。
func researchSourceIndex(snapshot ResearchSnapshot) map[string]ResearchSource {
	sources := map[string]ResearchSource{}
	for _, source := range snapshot.Sources {
		sources[source.ID] = source
	}
	return sources
}

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
	if repaired := repairResearchSourceIDs(result, sources); len(repaired) > 0 {
		notes = append(notes, repaired...)
	}
	if softenUnsupportedAttribution(result, sources) {
		notes = append(notes, "未提供直接资金流或板块因果证据，已将题材/资金归因降为待验证假设")
	}
	var err error
	notes, err = validateResearchCoreClaims(result, snapshot, sources, notes)
	if err != nil {
		return notes, err
	}
	conditionIDs := map[string]bool{}
	if len(result.Conditions) > 6 {
		result.Conditions = result.Conditions[:6]
	}
	// [ES_VALIDATION_DEGRADE_PATCH_V2] condition 级违规改为「丢弃 + note」，
	// 不再让单条条件拖垮整份研究。与既有静默修正（window / threshold 回填）哲学一致。
	// 安全边界：claim 级来源/引文伪造、以及「失效条件编号不存在」仍然硬失败——
	// 只有可被无歧义移除的单条 condition（及其下游引用）才降级。
	keptConditions := make([]ResearchCondition, 0, len(result.Conditions))
	for i := range result.Conditions {
		condition := &result.Conditions[i]
		if condition.ID == "" || conditionIDs[condition.ID] {
			notes = append(notes, "已丢弃编号缺失或重复的交易条件")
			continue
		}
		conditionIDs[condition.ID] = true
		condition.Status = "pending"
		condition.Text = truncateExactText(condition.Text, 220)
		if normalized, ok := normalizeResearchConditionOperator(condition.Operator); ok && normalized != condition.Operator {
			condition.Operator = normalized
			notes = append(notes, "已将交易条件中的同义比较词规范为协议枚举")
		}
		unknownSource := false
		for _, id := range condition.SourceIDs {
			if _, ok := sources[id]; !ok {
				unknownSource = true
				break
			}
		}
		if unknownSource {
			notes = append(notes, "已丢弃引用了未知来源的交易条件"+condition.ID)
			continue
		}
		if condition.Text == "" {
			notes = append(notes, "已丢弃缺少观察描述的交易条件"+condition.ID)
			continue
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
				// [ES_VALIDATION_DEGRADE_PATCH_V2] 只接受已知锚点的同义别名归一，
				// 编造的锚点名称仍然硬失败（保持「伪造结构不被接受」的安全边界）。
				anchor, ok = fallbackResearchAnchor(anchors, condition.Metric, condition.AnchorID)
				if ok {
					notes = append(notes, "已将锚点别名"+condition.AnchorID+"归一为"+anchor.ID)
				}
			}
			if !ok {
				return notes, fmt.Errorf("引用了未知anchor_id：%s", condition.AnchorID)
			}
			value := anchor.Price
			condition.Threshold = &value
			condition.SourceIDs = uniqueStrings(append(condition.SourceIDs, anchor.SourceID), 6)
			if condition.Operator != "gte" && condition.Operator != "lte" {
				notes = append(notes, "已丢弃比较符非法的价格条件"+condition.ID)
				continue
			}
			comparison := "不低于"
			if condition.Operator == "lte" {
				comparison = "不高于"
			}
			// [ES_VALIDATION_DEGRADE_PATCH_V2] 用实际采用的锚点覆盖（可能是回退锚点），
			// 使下游 AnchoredPricePlan 的 AnchorID 引用与条件描述保持一致。
			condition.AnchorID = anchor.ID
			condition.Text = fmt.Sprintf("收盘价%s%.2f元（%s）", comparison, value, anchor.Label)
			if condition.Window == "next_disclosure" {
				notes = append(notes, "已丢弃窗口非法的价格条件"+condition.ID)
				continue
			}
		case "volume_ratio":
			if condition.Threshold == nil || !finite(*condition.Threshold) || *condition.Threshold < .2 || *condition.Threshold > 10 {
				notes = append(notes, "已丢弃阈值越界的量比条件"+condition.ID)
				continue
			}
			if condition.Operator != "gte" && condition.Operator != "lte" {
				notes = append(notes, "已丢弃比较符非法的量比条件"+condition.ID)
				continue
			}
			condition.SourceIDs = uniqueStrings(append(condition.SourceIDs, "m-price"), 6)
			if condition.Window == "next_disclosure" {
				notes = append(notes, "已丢弃窗口非法的量比条件"+condition.ID)
				continue
			}
			notes = append(notes, "量比阈值为研究假设，并非已回测交易规则")
		case "disclosure", "auction", "opening":
			condition.Threshold = nil
			condition.AnchorID = ""
			condition.Operator = "confirmed"
		default:
			notes = append(notes, "已丢弃指标非法的交易条件"+condition.ID)
			continue
		}
		keptConditions = append(keptConditions, *condition)
	}
	// [ES_VALIDATION_DEGRADE_PATCH_V2] 回写被保留的条件；被丢弃条件的编号从 conditionIDs 中撤销，
	// 因此下游失效编号/情景若引用了它，会走既有的硬失败分支（不会被静默放行）。
	result.Conditions = keptConditions
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

// Models occasionally drop or alter one character while copying a long source
// id into JSON. Repair only a unique one-edit match for generated sources;
// ambiguous or unrelated ids remain validation errors.
func repairResearchSourceIDs(result *ResearchSynthesis, sources map[string]ResearchSource) []string {
	repairs := map[string]string{}
	repairIDs := func(ids *[]string) {
		for index, id := range *ids {
			if _, ok := sources[id]; ok || !strings.HasPrefix(id, "s-") {
				continue
			}
			matches := []string{}
			for candidate := range sources {
				if !strings.HasPrefix(candidate, "s-") || !sourceIDWithinOneEdit(id, candidate) {
					continue
				}
				matches = append(matches, candidate)
			}
			if len(matches) == 1 {
				(*ids)[index] = matches[0]
				repairs[id] = matches[0]
			}
		}
	}
	repairClaim := func(claim *ResearchClaim) { repairIDs(&claim.SourceIDs) }
	repairClaim(&result.Thesis)
	for index := range result.Support {
		repairClaim(&result.Support[index])
	}
	for index := range result.Counter {
		repairClaim(&result.Counter[index])
	}
	for index := range result.Alternatives {
		repairClaim(&result.Alternatives[index])
	}
	for index := range result.Conditions {
		repairIDs(&result.Conditions[index].SourceIDs)
	}
	if result.Decision.PricePlan != nil {
		repairIDs(&result.Decision.PricePlan.SourceIDs)
	}
	if len(repairs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(repairs))
	for id := range repairs {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	notes := make([]string, 0, len(keys))
	for _, id := range keys {
		notes = append(notes, fmt.Sprintf("已将模型近似来源编号%s纠正为%s", id, repairs[id]))
	}
	return notes
}

func sourceIDWithinOneEdit(a, b string) bool {
	if a == b {
		return true
	}
	if !strings.HasPrefix(a, "s-") || !strings.HasPrefix(b, "s-") {
		return false
	}
	a, b = a[2:], b[2:]
	if len(a) > len(b)+1 || len(b) > len(a)+1 {
		return false
	}
	if len(a) == len(b) {
		differences := 0
		for index := range a {
			if a[index] != b[index] {
				differences++
				if differences > 1 {
					return false
				}
			}
		}
		return differences == 1
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	shortIndex, longIndex := 0, 0
	differences := 0
	for shortIndex < len(a) && longIndex < len(b) {
		if a[shortIndex] == b[longIndex] {
			shortIndex++
			longIndex++
			continue
		}
		differences++
		if differences > 1 {
			return false
		}
		longIndex++
	}
	return true
}

func normalizeResearchConditionOperator(operator string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(operator)) {
	case "gte", "ge", ">=", "≥", "at_least", "greater_than_or_equal", "不低于", "至少", "大于等于":
		return "gte", true
	case "lte", "le", "<=", "≤", "at_most", "less_than_or_equal", "不高于", "至多", "小于等于":
		return "lte", true
	default:
		return operator, false
	}
}

// Price and volume evidence can show movement, but cannot identify who caused
// it. Keep generated reports from turning a correlation into a causal claim.
func softenUnsupportedAttribution(result *ResearchSynthesis, sources map[string]ResearchSource) bool {
	directCapitalEvidence := false
	for _, source := range sources {
		if source.Kind == "fund_flow" || source.Kind == "capital_flow" || source.Kind == "lhb" {
			directCapitalEvidence = true
			break
		}
	}
	if directCapitalEvidence {
		return false
	}
	rewrite := func(value *string) bool {
		before := *value
		if containsAnyFold(before, "缺少资金", "缺乏资金", "没有资金", "缺少增量资金", "缺乏增量资金", "没有增量资金", "不能证明", "无法确认", "未经证实", "待验证假设") {
			return false
		}
		for _, pair := range [][2]string{
			{"更可能由题材预期与资金驱动", "题材预期与资金行为仅为待验证假设，驱动来源无法确认"},
			{"题材资金脉冲", "题材与资金行为的待验证假设"},
			{"资金驱动", "资金行为假设"},
			{"资金动能强", "价格动能较强"},
			{"增量资金", "增量资金的未证实假设"},
			{"资金性质", "成交与持仓结构性质"},
		} {
			*value = strings.ReplaceAll(*value, pair[0], pair[1])
		}
		return before != *value
	}
	changed := false
	for _, text := range []*string{&result.Headline, &result.MainConflict, &result.BaselineReason} {
		changed = rewrite(text) || changed
	}
	for _, claim := range []*ResearchClaim{&result.Thesis} {
		if rewrite(&claim.Text) {
			claim.Kind = "inference"
			changed = true
		}
	}
	for _, claims := range [][]ResearchClaim{result.Support, result.Counter, result.Alternatives} {
		for index := range claims {
			if rewrite(&claims[index].Text) {
				claims[index].Kind = "inference"
				changed = true
			}
		}
	}
	changed = rewrite(&result.Decision.NewPosition) || changed
	changed = rewrite(&result.Decision.ExistingPosition) || changed
	changed = rewrite(&result.Decision.Reason) || changed
	for index := range result.Conditions {
		changed = rewrite(&result.Conditions[index].Text) || changed
	}
	for index := range result.Scenarios {
		changed = rewrite(&result.Scenarios[index].Description) || changed
		changed = rewrite(&result.Scenarios[index].Response) || changed
	}
	return changed
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

// [ES_VALIDATION_DEGRADE_PATCH_V2] 模型常把快照锚点写成同义别名（如 "close"/"ma_20"/"MA20"）。
// 这里只做「已知锚点的别名归一」，不接受任意编造的锚点：unresolved 的名称一律硬失败。
func fallbackResearchAnchor(anchors map[string]PriceAnchor, metric, requested string) (PriceAnchor, bool) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(requested), "_", ""))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	aliases := map[string]string{
		"lastclose": "last_close", "close": "last_close", "currentprice": "last_close",
		"latestclose": "last_close", "price": "last_close",
		"ma5": "ma5", "ma10": "ma10", "ma20": "ma20", "ma60": "ma60",
		"rangehigh": "range_high", "rangelow": "range_low",
	}
	canonical, ok := aliases[normalized]
	if !ok {
		return PriceAnchor{}, false
	}
	anchor, ok := anchors[canonical]
	if !ok || anchor.Price <= 0 {
		return PriceAnchor{}, false
	}
	return anchor, true
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

// [ES_PARTIAL_CORE_SAVE_PATCH] validateResearchCoreClaims 是 core 作用域的共享校验实现。
//
// 同时被两条路径调用：
//   - validateResearch（完整校验）：core 段 + 交易段，后者在此之后继续；
//   - validateResearchCore（§4 core-only 校验）：只跑本函数。
//
// 因为两条路径共用同一实现，分段保存**不会**放宽任何 claim 级安全约束。
func validateResearchCoreClaims(result *ResearchSynthesis, snapshot ResearchSnapshot, sources map[string]ResearchSource, notes []string) ([]string, error) {
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
			// [ES_EVIDENCE_CONTRACT_PATCH]
			// 基线字段名绝不能被当作来源。给出可操作的反馈，
			// 要求整条重写/删除，禁止只改编号掩盖问题。
			if isBaselineIdentifier(id) {
				return fmt.Errorf(
					"来源编号%q是基线字段而非证据（可引用编号见输入 allowed_source_ids）：请删除该编号并整条重写或删除此判断，不得只改编号保留原陈述；仅可用 baseline_relation/baseline_reason 表达与基线的关系",
					id)
			}
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
	return notes, nil
}
