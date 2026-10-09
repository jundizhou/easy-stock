package portfoliooptimization

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	pi "easy-stock/backend/internal/portfolioinspection"
)

func in(value string, values ...string) bool {
	for _, item := range values {
		if value == item {
			return true
		}
	}
	return false
}
func researchFor(job Job, symbol string) (pi.HoldingResult, bool) {
	for _, r := range job.Results {
		if r.Holding.Symbol == symbol && pi.ValidOptimizationResearch(r) {
			return r, true
		}
	}
	return pi.HoldingResult{}, false
}
func refsCoverStock(job Job, refs []pi.EvidenceRef, symbol string) bool {
	r, ok := researchFor(job, symbol)
	if !ok || checkRefs(job, refs) != nil {
		return false
	}
	for _, ref := range refs {
		if (ref.ReportID == r.AnalysisID && ref.SourceID != "") || strings.HasPrefix(ref.Fact, symbol+".") {
			return true
		}
	}
	return false
}

func validateInvestment(job Job, a Allocation) error {
	i := a.Investment
	if i == nil || !in(i.Action, "allocate", "hold", "wait", "reduce") || !in(i.Role, "核心成长", "盈利兑现", "进攻", "防守", "周期机会") || i.Horizon != job.Source.Request.Horizon {
		return fmt.Errorf("%s缺少本周期投资角色及持有/增持判断", a.Symbol)
	}
	values := []string{i.Business, i.Growth, i.Valuation, i.Timing, i.PortfolioFit, i.Risk, i.Exit, i.OpportunityCost}
	if err := validateProfitInterpretation(strings.Join(append(values, a.Reason, a.Funding, a.SuitabilityReason), "；")); err != nil {
		return fmt.Errorf("%s：%w", a.Symbol, err)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len([]rune(value)) > 180 {
			return fmt.Errorf("%s六项投资判断、退出与机会成本须完整且简洁", a.Symbol)
		}
	}
	// A stock's own supplied condition references belong to the same investment
	// row. Reuse those exact citations rather than demanding a duplicate in the
	// top-level list. Correlation alone still cannot stand in for company facts.
	refs := append([]pi.EvidenceRef(nil), a.EvidenceRefs...)
	for _, condition := range a.Conditions {
		refs = append(refs, condition.EvidenceRefs...)
	}
	if !refsCoverStock(job, refs, a.Symbol) {
		return fmt.Errorf("%s投资判断缺少该股真实事实引用", a.Symbol)
	}
	r, ok := researchFor(job, a.Symbol)
	if !ok {
		return fmt.Errorf("%s个股研究未完成", a.Symbol)
	}
	rr := r.Analysis.ResearchReport
	if err := validateCurrentDeductedProfit(a.Symbol, i.Business+"；"+i.Growth, pi.InvestmentFinancialFacts(r)); err != nil {
		return err
	}
	if i.Role == "防守" && r.Analysis.Trend.ATR14Percent > 5 {
		facts := pi.InvestmentFinancialFacts(r)
		net, deducted := facts[a.Symbol+".financial.net_profit"], facts[a.Symbol+".financial.deducted_net_profit"]
		n, nok := net.Value.(float64)
		d, dok := deducted.Value.(float64)
		if net.Available && deducted.Available && nok && dok && n < 0 && d < 0 {
			return fmt.Errorf("%s双亏且ATR超过5%%，减仓操作不代表该股是防守资产；保留投资方向并修正角色", a.Symbol)
		}
	}
	if (rr.Decision.Status != "conditional" || rr.Decision.Horizon != i.Horizon) && strings.TrimSpace(i.PriorOpinion) == "" {
		return fmt.Errorf("%s须解释原观察/无计划意见与本次组合判断的关系", a.Symbol)
	}
	if rr.Decision.Horizon != i.Horizon && strings.TrimSpace(i.PeriodSuitability) == "" {
		return fmt.Errorf("%s须说明原研究哪些事实适用本次周期", a.Symbol)
	}
	if len([]rune(i.PriorOpinion)) > 180 || len([]rune(i.PeriodSuitability)) > 180 {
		return fmt.Errorf("%s原意见及周期说明过长", a.Symbol)
	}
	if err := pi.ValidateOptimizationInvestmentAction(pi.HoldingConclusion{Action: strings.Join(append(values, i.PriorOpinion, i.PeriodSuitability), "；")}, r); err != nil {
		return err
	}
	if a.Suitable != in(i.Action, "allocate", "wait") {
		// An unselected zero-weight candidate may await a future setup without
		// accepting any funds in this plan. Nonzero ranges remain conditional buys.
		zeroCandidate := i.Action == "wait" && !a.Suitable && r.Holding.Weight == 0 && a.Minimum == 0 && a.Maximum == 0 && a.Preferred == 0
		if !zeroCandidate {
			return fmt.Errorf("%s新资金权限与投资行动不一致", a.Symbol)
		}
	}
	return nil
}

var directDeductedAmount = regexp.MustCompile(`扣非(?:净利润|利润)?(?:为|约)?(亏损|盈利)?\s*([-+]?\d+(?:\.\d+)?)\s*(亿|万|元)`)

// Catch the observed confusion between 644% growth and 6.44亿元 of profit.
// This narrow check is for a stock's own business/growth description, when a
// current immutable deducted-profit fact exists. It adds no evidence gate.
func validateCurrentDeductedProfit(symbol, text string, facts map[string]pi.Fact) error {
	fact := facts[symbol+".financial.deducted_net_profit"]
	actual, numeric := fact.Value.(float64)
	if !fact.Available || !numeric {
		return nil
	}
	for _, clause := range strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune("；;。", r) }) {
		if strings.Contains(clause, "去年") || strings.Contains(clause, "上年") || strings.Contains(clause, "上期") {
			continue
		}
		for _, m := range directDeductedAmount.FindAllStringSubmatch(clause, -1) {
			value, _ := strconv.ParseFloat(m[2], 64)
			unit := map[string]float64{"亿": 1e8, "万": 1e4, "元": 1}[m[3]]
			if m[1] == "亏损" {
				value = -math.Abs(value)
			}
			decimals := 0
			if _, fractional, ok := strings.Cut(m[2], "."); ok {
				decimals = len(fractional)
			}
			tolerance := .5 * math.Pow10(-decimals) * unit
			if math.Abs(value*unit-actual) > tolerance+1e-5 {
				return fmt.Errorf("%s正文扣非金额%s与原财务事实%.4g元不一致；同比百分比不能改写为利润金额", symbol, m[0], actual)
			}
		}
	}
	return nil
}

func validateAllocationConditions(job Job, a Allocation) error {
	r, ok := researchFor(job, a.Symbol)
	if !ok {
		return fmt.Errorf("%s个股研究未完成", a.Symbol)
	}
	rr := r.Analysis.ResearchReport
	invalid := map[string]bool{}
	for _, id := range rr.InvalidationIDs {
		invalid[id] = true
	}
	old := map[string]bool{}
	sources := map[string]bool{}
	for _, s := range rr.Sources {
		sources[s.ID] = true
	}
	for _, c := range rr.Conditions {
		valid := len(c.SourceIDs) > 0 && strings.TrimSpace(c.Text) != ""
		for _, id := range c.SourceIDs {
			valid = valid && sources[id]
		}
		old[c.ID] = valid
	}
	for _, id := range a.ConfirmationIDs {
		if !old[id] || invalid[id] {
			return fmt.Errorf("%s原确认条件不存在或类型错误", a.Symbol)
		}
	}
	for _, id := range a.InvalidationIDs {
		if !old[id] || !invalid[id] {
			return fmt.Errorf("%s原失效条件不存在或类型错误", a.Symbol)
		}
	}
	if len(a.Conditions) > 4 {
		return fmt.Errorf("%s组合行动条件最多4项", a.Symbol)
	}
	hasEntry, hasExit := len(a.ConfirmationIDs) > 0, len(a.InvalidationIDs) > 0
	for _, c := range a.Conditions {
		if !in(c.Kind, "entry", "exit") || c.Status != "pending" || strings.TrimSpace(c.Text) == "" || strings.TrimSpace(c.Verification) == "" || !refsCoverStock(job, c.EvidenceRefs, a.Symbol) {
			return fmt.Errorf("%s行动条件须有观察方法、真实引用且保持待验证", a.Symbol)
		}
		if len([]rune(c.Text)) > 180 || len([]rune(c.Verification)) > 180 {
			return fmt.Errorf("%s行动条件过长", a.Symbol)
		}
		if err := pi.ValidateOptimizationInvestmentAction(pi.HoldingConclusion{Action: c.Text + "；" + c.Verification}, r); err != nil {
			return err
		}
		if c.Threshold != nil || c.AnchorID != "" || c.Operator != "" {
			if c.Threshold == nil || !in(c.Operator, "gte", "lte") || !finitePositive(*c.Threshold) {
				return fmt.Errorf("%s价格条件格式无效", a.Symbol)
			}
			valid := false
			for _, anchor := range rr.Anchors {
				if anchor.ID == c.AnchorID && finitePositive(anchor.Price) && math.Abs(anchor.Price-*c.Threshold) < .005 && sources[anchor.SourceID] {
					valid = true
				}
			}
			if !valid {
				return fmt.Errorf("%s价格条件没有真实原锚点", a.Symbol)
			}
		}
		if c.Kind == "entry" {
			hasEntry = true
		} else {
			hasExit = true
		}
	}
	if a.Suitable && !hasExit {
		return fmt.Errorf("%s新增资金须说明失效/退出条件", a.Symbol)
	}
	if a.Suitable && a.Investment != nil && a.Investment.Action == "wait" && !hasEntry {
		return fmt.Errorf("%s等待买点须明确入场条件", a.Symbol)
	}
	return nil
}

func validateInvestmentComparison(job Job, c InvestmentComparison) error {
	if err := validateProfitInterpretation(c.Reason + "；" + c.Tradeoff); err != nil {
		return err
	}
	if c.FromSymbol == c.ToSymbol || !in(c.Dimension, "business", "growth", "valuation", "timing", "portfolio_fit", "risk") || strings.TrimSpace(c.Reason) == "" || strings.TrimSpace(c.Tradeoff) == "" {
		return fmt.Errorf("投资比较须明确两股、改善维度、原因与代价")
	}
	from, to := refsCoverStock(job, c.EvidenceRefs, c.FromSymbol), refsCoverStock(job, c.EvidenceRefs, c.ToSymbol)
	// A measured pairwise correlation genuinely references both stocks for
	// risk/portfolio fit. It cannot substantiate either company's financials.
	if in(c.Dimension, "risk", "portfolio_fit") && checkRefs(job, c.EvidenceRefs) == nil {
		for _, ref := range c.EvidenceRefs {
			for _, left := range job.Results {
				for _, right := range job.Results {
					if left.Holding.Symbol == right.Holding.Symbol || ref.Fact != "correlation."+left.Holding.Symbol+"."+right.Holding.Symbol {
						continue
					}
					// Separate correlations to the same existing holding are also
					// real evidence of differing common-driver exposure. They do
					// not substantiate either company's business or financials.
					from = from || c.FromSymbol == left.Holding.Symbol || c.FromSymbol == right.Holding.Symbol
					to = to || c.ToSymbol == left.Holding.Symbol || c.ToSymbol == right.Holding.Symbol
				}
			}
		}
	}
	if !from || !to {
		return &comparisonEvidenceError{comparison: c, from: from, to: to}
	}
	if len([]rune(c.Reason)) > 180 || len([]rune(c.Tradeoff)) > 180 {
		return fmt.Errorf("投资比较%s→%s原因或代价超过180字", c.FromSymbol, c.ToSymbol)
	}
	return nil
}

type comparisonEvidenceError struct {
	comparison InvestmentComparison
	from, to   bool
	proposal   *Proposal
	needed     []InvestmentComparison
}

func (e *comparisonEvidenceError) Error() string {
	return fmt.Sprintf("投资比较%s→%s须分别引用双方真实资料；来源股引用有效=%t，接收股引用有效=%t", e.comparison.FromSymbol, e.comparison.ToSymbol, e.from, e.to)
}

func comparisonKey(c InvestmentComparison) string {
	return c.FromSymbol + "/" + c.ToSymbol + "/" + c.Dimension
}

func repairComparisonReferences(job Job, frozen Proposal, content string) (Proposal, error) {
	if !json.Valid([]byte(trimJSONFence(content))) {
		return frozen, fmt.Errorf("引用补丁须为单个完整JSON")
	}
	var patch struct {
		Rows []struct {
			FromSymbol   string           `json:"from_symbol"`
			ToSymbol     string           `json:"to_symbol"`
			Dimension    string           `json:"dimension"`
			EvidenceRefs []pi.EvidenceRef `json:"evidence_refs"`
		} `json:"comparison_refs"`
	}
	decoder := json.NewDecoder(strings.NewReader(trimJSONFence(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		return frozen, err
	}
	expected := map[string]int{}
	for i, c := range frozen.InvestmentComparisons {
		var invalid *comparisonEvidenceError
		if errors.As(validateInvestmentComparison(job, c), &invalid) {
			expected[comparisonKey(c)] = i
		}
	}
	if len(patch.Rows) != len(expected) {
		return frozen, fmt.Errorf("引用补丁须只包含全部缺引用比较")
	}
	data, _ := json.Marshal(frozen)
	var updated Proposal
	if err := json.Unmarshal(data, &updated); err != nil {
		return frozen, err
	}
	for _, row := range patch.Rows {
		key := comparisonKey(InvestmentComparison{FromSymbol: row.FromSymbol, ToSymbol: row.ToSymbol, Dimension: row.Dimension})
		i, ok := expected[key]
		if !ok {
			return frozen, fmt.Errorf("引用补丁不得新增比较或改写投资内容")
		}
		updated.InvestmentComparisons[i].EvidenceRefs = row.EvidenceRefs
		delete(expected, key)
	}
	if err := validateProposal(job, updated); err != nil {
		return frozen, err
	}
	return updated, nil
}

func validateProfitInterpretation(text string) error {
	for _, clause := range strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune("，,；;。", r) }) {
		for _, claim := range []string{"全为主营", "全为经营"} {
			if i := strings.Index(clause, claim); i >= 0 {
				prefix := clause[:i]
				if strings.HasSuffix(prefix, "并非") || strings.HasSuffix(prefix, "不是") || strings.HasSuffix(prefix, "不代表") || strings.HasSuffix(prefix, "不能认定") || strings.HasSuffix(prefix, "不能说") || strings.HasSuffix(prefix, "不能称") {
					continue
				}
				return fmt.Errorf("归母、扣非与主营/经营利润口径不同，不能凭当前数字宣称全为主营或经营；请分别描述真实利润口径")
			}
		}
	}
	return nil
}

func investmentImprovements(job Job, target []pi.Holding) []Improvement {
	if job.Proposal == nil {
		return nil
	}
	flows, _, _ := FundingFlowsCompared(job.Source.Request.Holdings, target, job.Proposal.InvestmentComparisons)
	out := []Improvement{}
	for _, c := range job.Proposal.InvestmentComparisons {
		if validateInvestmentComparison(job, c) != nil {
			continue
		}
		for _, f := range flows {
			if f.FromSymbol == c.FromSymbol && f.ToSymbol == c.ToSymbol {
				out = append(out, Improvement{Issue: "投资比较", Metric: "investment." + c.Dimension, Kind: "investment", FromSymbol: c.FromSymbol, ToSymbol: c.ToSymbol, Weight: f.Weight, Reason: c.Reason, Tradeoff: c.Tradeoff, EvidenceRefs: c.EvidenceRefs})
			}
		}
	}
	return out
}

func validateReviewedComparisons(job Job, plan Plan, review Assessment) error {
	if len(review.InvestmentComparisons) > 8 {
		return fmt.Errorf("复评投资比较最多8项")
	}
	a, b := plan.Original.Request.Holdings, plan.Target
	if plan.AssessmentOrder == "target_first" {
		a, b = b, a
	}
	preferred, other := b, a
	if review.Preferred == "a" {
		preferred, other = a, b
	}
	pw, _, _ := weights(preferred, false)
	ow, _, _ := weights(other, false)
	for _, c := range review.InvestmentComparisons {
		if review.Preferred == "neither" || pw[c.PreferredSymbol] <= ow[c.PreferredSymbol] || pw[c.OtherSymbol] >= ow[c.OtherSymbol] {
			return fmt.Errorf("复评投资比较股票与实际偏好资金方向不一致")
		}
		if err := validateInvestmentComparison(job, InvestmentComparison{FromSymbol: c.OtherSymbol, ToSymbol: c.PreferredSymbol, Dimension: c.Dimension, Reason: c.Reason, Tradeoff: c.Tradeoff, EvidenceRefs: c.EvidenceRefs}); err != nil {
			return err
		}
	}
	return nil
}

func reviewConfirmsImprovement(job Job, plan Plan, review Assessment) bool {
	for _, m := range plan.Improvements {
		if m.Kind != "investment" {
			return true
		}
	}
	// Funding is fungible. The blind reviewer may identify a different pairing
	// or dimension; validate actual net changes and both stocks' evidence instead
	// of requiring it to reproduce the proposer's arbitrary funding decomposition.
	preferred, err := comparisonTargetsProposal(review.Preferred, plan.AssessmentOrder)
	return err == nil && preferred && len(plan.Improvements) > 0 && len(review.InvestmentComparisons) > 0 && validateReviewedComparisons(job, plan, review) == nil
}

// Existing findings drive screening without another model call. Directory
// industries are diversification hints; they do not prove return correlation.
func PortfolioNeeds(report pi.Report) []string {
	out := []string{}
	if report.Metrics.MaxSinglePercent > report.Profile.MaxSinglePercent && report.Profile.MaxSinglePercent > 0 {
		out = append(out, "降低单票集中，给腾出的资金寻找更合适用途")
	}
	for _, g := range report.Conclusion.RiskGroups {
		if g.Weight > report.Profile.MaxHighRiskPercent && report.Profile.MaxHighRiskPercent > 0 {
			out = append(out, "减少共同驱动重复："+g.Name)
			break
		}
	}
	for _, r := range report.Holdings {
		facts := pi.InvestmentFinancialFacts(r)
		f := facts[r.Holding.Symbol+".financial.net_profit"]
		if v, ok := f.Value.(float64); f.Available && ok && v < 0 {
			out = append(out, "增加主营盈利兑现，降低亏损业务暴露")
			break
		}
	}
	if len(out) < 3 {
		for _, issue := range report.Conclusion.PrimaryRisks {
			if strings.TrimSpace(issue) != "" {
				out = append(out, shortText(issue, 80))
				if len(out) >= 3 {
					break
				}
			}
		}
	}
	if len(out) == 0 {
		out = append(out, "比较旧股增持与新股配置的性价比，优先补齐不同业务角色")
	}
	return out[:min(3, len(out))]
}

func CandidateFit(report pi.Report, catalog map[string]string, industry string) (int, string) {
	group := IndustryGroup(industry)
	if group == "" {
		return 0, "目录行业未知，不能推断组合互补"
	}
	exposure, total := 0, 0
	for _, h := range report.Request.Holdings {
		if IndustryGroup(catalog[h.Symbol]) == "" {
			return 0, "原持仓目录行业未完整确认，暂不计算行业互补奖励"
		}
		total += h.Weight
		if IndustryGroup(catalog[h.Symbol]) == group {
			exposure += h.Weight
		}
	}
	if total == 0 {
		return 0, "没有可比较的原股票仓位"
	}
	if exposure == 0 {
		return 10, "原组合尚无该目录行业，优先核验业务互补；不代表低相关"
	}
	if exposure*100 >= 35*total {
		return -10, "该目录行业已较集中，新增同业排序降低；最终比较实际驱动"
	}
	return 0, "原组合已有该行业，需比较增量资金是否优于旧股"
}

// A whole-portfolio semiconductor claim must match the frozen member exposure.
// Non-chip holdings cannot be silently relabelled from a shared tech narrative.
func validateWholePortfolioIndustryClaim(job Job, report pi.Report, score pi.AIReport) error {
	_, total, _ := weights(report.Request.Holdings, false)
	chip := map[string]bool{}
	for _, g := range job.Proposal.RiskGroups {
		if strings.Contains(g.Name, "半导体") || strings.Contains(g.Name, "芯片") {
			for _, s := range g.Symbols {
				chip[s] = true
			}
		}
	}
	if len(chip) == 0 {
		return nil
	}
	exposure := 0
	for _, h := range report.Request.Holdings {
		if chip[h.Symbol] {
			exposure += h.Weight
		}
	}
	if exposure == total {
		return nil
	}
	texts := []string{score.ExecutiveSummary, score.RiskReason, score.ConfidenceReason}
	for _, d := range score.Dimensions {
		texts = append(texts, d.Reason)
		texts = append(texts, d.Limitations...)
	}
	for _, text := range texts {
		for _, claim := range []string{"全为芯片", "全部为芯片", "全是芯片", "全部是芯片", "全为半导体", "全部为半导体", "全是半导体", "全部是半导体"} {
			if strings.Contains(text, claim) && !strings.Contains(text, "并非"+claim) && !strings.Contains(text, "不是"+claim) && !strings.Contains(text, "不"+claim) {
				return fmt.Errorf("组合含非芯片资产，半导体/芯片冻结暴露为%d%%/%d%%，不能称全为芯片；仅修正行业事实，不改变偏好或评分", exposure, total)
			}
		}
	}
	return nil
}
