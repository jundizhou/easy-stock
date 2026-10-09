package portfoliooptimization

import (
	pi "easy-stock/backend/internal/portfolioinspection"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type preferredWeightTotalError struct {
	total, required int
	limits          map[string][2]int
}

type allocationRangeError struct{ minimum, maximum, required int }

func (e *allocationRangeError) Error() string {
	return fmt.Sprintf("权重范围不可行：最小合计%d%%、最大合计%d%%，须覆盖固定股票总仓位%d%%；不得新增现金", e.minimum, e.maximum, e.required)
}

func (e *preferredWeightTotalError) Error() string {
	return fmt.Sprintf("preferred_weight合计%d%%，须在原区间内修正为固定股票总仓位%d%%；股票、区间、投资方向和理由保持不变", e.total, e.required)
}

func patchPreferredWeights(job Job, frozen Proposal, patch map[string]int) (Proposal, error) {
	data, _ := json.Marshal(frozen)
	var updated Proposal
	if err := json.Unmarshal(data, &updated); err != nil {
		return Proposal{}, err
	}
	if len(updated.Alternatives) != 1 || len(patch) != len(updated.Alternatives[0].Allocations) {
		return Proposal{}, errors.New("偏好修复须只含全部原方案股票")
	}
	total, sum := 0, 0
	_, total, _ = weights(job.Source.Request.Holdings, false)
	for i, a := range updated.Alternatives[0].Allocations {
		w, exists := patch[a.Symbol]
		if !exists || w < a.Minimum || w > a.Maximum {
			return Proposal{}, errors.New("偏好修复超出原股票或原范围")
		}
		sum += w
		updated.Alternatives[0].Allocations[i].Preferred = w
	}
	if sum != total {
		return Proposal{}, &preferredWeightTotalError{total: sum, required: total}
	}
	// Only a complete numeric patch can be projected, and only one percentage
	// point may move. Investment judgments/ranges remain frozen; the same solver
	// enforces locks, increase permissions, style and cumulative change budget.
	target, err := Solve(job, updated.Alternatives[0])
	if err != nil {
		return Proposal{}, err
	}
	projected := map[string]int{}
	for _, h := range target {
		projected[h.Symbol] = h.Weight
	}
	deviation := 0
	for _, a := range updated.Alternatives[0].Allocations {
		deviation += abs(a.Preferred - projected[a.Symbol])
	}
	if deviation > 2 {
		return Proposal{}, errors.New("偏好修复违反交易/新增资金权限或预算；校正超过1个百分点，不自动改写方案")
	}
	for i, a := range updated.Alternatives[0].Allocations {
		updated.Alternatives[0].Allocations[i].Preferred = projected[a.Symbol]
	}
	if err := validateProposal(job, updated); err != nil {
		return Proposal{}, err
	}
	return updated, nil
}

func checkRefs(job Job, refs []pi.EvidenceRef) error {
	if len(refs) == 0 {
		return errors.New("缺少证据引用")
	}
	report := pi.OptimizationUnionReport(job.Source.Request, job.Results)
	normalized := append([]pi.EvidenceRef(nil), refs...)
	for i, ref := range refs {
		if ref.Fact != "" {
			key := pi.OptimizationFactAlias(ref.Fact, report.Facts)
			f, ok := report.Facts[key]
			if !ok || !f.Available || ref.ReportID != "" || ref.SourceID != "" {
				return fmt.Errorf("不可用事实引用%s", ref.Fact)
			}
			normalized[i].Fact = key
			continue
		}
		found := false
		for _, r := range job.Results {
			if r.AnalysisID == ref.ReportID && pi.ValidOptimizationResearch(r) {
				for _, s := range r.Analysis.ResearchReport.Sources {
					if s.ID == ref.SourceID {
						found = true
					}
				}
			}
		}
		if !found || ref.ReportID == "" || ref.SourceID == "" {
			return errors.New("个股来源引用不存在")
		}
	}
	// Persist exact canonical identities for per-stock checks and report links.
	// Do not partially rewrite a list that contains any invalid evidence.
	copy(refs, normalized)
	return nil
}

func checkComparisonRefs(job Job, plan *Plan, a, b pi.Report, refs []pi.EvidenceRef) error {
	if len(refs) == 0 {
		return errors.New("缺少证据引用")
	}
	af, bf := pi.OptimizationComparisonFacts(a), pi.OptimizationComparisonFacts(b)
	store := func(key string, f pi.Fact) {
		if plan.Original.Facts == nil {
			plan.Original.Facts = map[string]pi.Fact{}
		}
		if plan.Proposed.Facts == nil {
			plan.Proposed.Facts = map[string]pi.Fact{}
		}
		plan.Original.Facts[key], plan.Proposed.Facts[key] = f, f
	}
	for i, ref := range refs {
		resolved := false
		for label, r := range map[string]pi.Report{"a": a, "b": b} {
			if !strings.HasPrefix(ref.Fact, label+".") {
				continue
			}
			facts := pi.OptimizationComparisonFacts(r)
			key := pi.OptimizationFactAlias(strings.TrimPrefix(ref.Fact, label+"."), facts)
			f, ok := facts[key]
			if !ok || !f.Available || ref.ReportID != "" || ref.SourceID != "" {
				return fmt.Errorf("不可用比较事实引用%s", ref.Fact)
			}
			store(ref.Fact, f)
			resolved = true
		}
		// An assessment compares both sides. A column reference without a side
		// is represented as the pair of actual values, never guessed as A or B.
		if !resolved && ref.Fact != "" && ref.ReportID == "" && ref.SourceID == "" {
			key := pi.OptimizationFactAlias(ref.Fact, af)
			fa, oka := af[key]
			fb, okb := bf[pi.OptimizationFactAlias(ref.Fact, bf)]
			if oka && fa.Available && okb && fb.Available {
				refs[i].Fact = "comparison." + key
				store(refs[i].Fact, pi.Fact{Value: map[string]any{"a": fa.Value, "b": fb.Value}, Available: true, Method: "两组配置同名事实的实际值，A/B对应见复评说明", Limitation: "比较引用不替代任何一组的原始事实；" + fa.Limitation + "；" + fb.Limitation})
				resolved = true
			}
		}
		if !resolved {
			shared := []pi.EvidenceRef{ref}
			if err := checkRefs(job, shared); err != nil {
				return err
			}
			refs[i] = shared[0]
		}
	}
	return nil
}
func canIncrease(job Job, a Allocation) bool {
	return a.Suitable && strings.TrimSpace(a.SuitabilityReason) != "" && strings.TrimSpace(a.Funding) != "" && validateInvestment(job, a) == nil && validateAllocationConditions(job, a) == nil
}

// Shared by full validation and bounded row repair; neither path relaxes investment checks.
func validateAllocation(job Job, a Allocation) error {
	if strings.TrimSpace(a.Reason) == "" {
		return fmt.Errorf("%s缺少投资理由", a.Symbol)
	}
	if a.Minimum < 0 || a.Maximum > 100 || a.Minimum > a.Maximum || a.Preferred < a.Minimum || a.Preferred > a.Maximum {
		return fmt.Errorf("%s权重范围或参考权重无效", a.Symbol)
	}
	if err := checkRefs(job, a.EvidenceRefs); err != nil {
		return err
	}
	if err := validateInvestment(job, a); err != nil {
		return err
	}
	if err := validateAllocationConditions(job, a); err != nil {
		return err
	}
	if a.Suitable && !canIncrease(job, a) {
		return fmt.Errorf("%s 新资金适用性核验未通过；请补齐投资比较、退出条件或限于保持/减持", a.Symbol)
	}
	// All price directives must already be anchored in the source research.
	for _, r := range job.Results {
		if r.Holding.Symbol == a.Symbol {
			if err := pi.ValidateOptimizationInvestmentAction(pi.HoldingConclusion{Action: a.Reason + "；" + a.Funding + "；" + a.SuitabilityReason + "；" + a.Investment.Timing + "；" + a.Investment.Exit}, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateProposal(job Job, p Proposal) error {
	if len(p.IssueDetails) > 8 {
		return errors.New("问题描述数量超限")
	}
	for _, issue := range p.IssueDetails {
		if len(issue.EvidenceRefs) > 0 {
			if err := checkRefs(job, issue.EvidenceRefs); err != nil {
				return err
			}
		}
	}
	if job.RevisionCount > 0 && (len(job.RevisionHistory) == 0 || (len(p.RiskGroups) != 0 || len(job.RevisionHistory[0].RiskGroups) != 0) && fingerprint(p.RiskGroups) != fingerprint(job.RevisionHistory[0].RiskGroups)) {
		return errors.New("改进方案须原样沿用已冻结risk_groups，不通过改分组提高评分")
	}
	if len(p.Alternatives) > 1 || len(p.RiskGroups) > 6 || len(p.Issues) > 8 || len(p.InvestmentComparisons) > 8 {
		return errors.New("方案、问题或风险组数量超限")
	}
	if len(p.Alternatives) == 0 && strings.TrimSpace(p.KeepReason) == "" {
		return errors.New("无方案时须解释保持原因")
	}
	known := map[string]bool{}
	for _, r := range job.Results {
		known[r.Holding.Symbol] = true
	}
	names := map[string]bool{}
	for _, g := range p.RiskGroups {
		if g.Name == "" || g.Reason == "" || len(g.Symbols) == 0 || names[g.Name] {
			return errors.New("风险组为空或重复")
		}
		names[g.Name] = true
		if err := checkRefs(job, g.EvidenceRefs); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, s := range g.Symbols {
			if !known[s] || seen[s] {
				return errors.New("风险组包含未知或重复股票")
			}
			seen[s] = true
		}
	}
	comparisons := map[string]bool{}
	for _, c := range p.InvestmentComparisons {
		if err := validateInvestmentComparison(job, c); err != nil {
			return err
		}
		key := c.FromSymbol + "/" + c.ToSymbol + "/" + c.Dimension
		if comparisons[key] {
			return errors.New("投资比较重复")
		}
		comparisons[key] = true
	}
	for _, alt := range p.Alternatives {
		if alt.Name == "" || len(alt.Allocations) == 0 {
			return errors.New("方案缺少权重范围")
		}
		_, total, err := weights(job.Source.Request.Holdings, false)
		if err != nil {
			return err
		}
		minimum, maximum := 0, 0
		for _, a := range alt.Allocations {
			minimum += a.Minimum
			maximum += a.Maximum
		}
		seen := map[string]bool{}
		for _, a := range alt.Allocations {
			if !known[a.Symbol] || seen[a.Symbol] || a.Reason == "" {
				return errors.New("方案股票或理由无效")
			}
			seen[a.Symbol] = true
			if err := validateAllocation(job, a); err != nil {
				return err
			}
		}
		for _, r := range job.Results {
			if pi.ValidOptimizationResearch(r) && !seen[r.Holding.Symbol] {
				return fmt.Errorf("%s缺少投资比较；未选候选也须列零仓位并说明原因", r.Holding.Symbol)
			}
		}
		// First validate all investment content, identities and permissions. Only
		// then may a numeric-only repair freeze this proposal as its baseline.
		if minimum > total || maximum < total {
			return &allocationRangeError{minimum: minimum, maximum: maximum, required: total}
		}
		preferred := 0
		for _, a := range alt.Allocations {
			preferred += a.Preferred
		}
		if preferred != total {
			limits := map[string][2]int{}
			original, _, _ := weights(job.Source.Request.Holdings, false)
			eligibility := map[string]Eligibility{}
			for _, e := range job.Eligibility {
				eligibility[e.Symbol] = e
			}
			rules, _ := pi.RulesFor(job.Source.Request.TraderProfile)
			for _, a := range alt.Allocations {
				lo, hi := allocationBounds(job, a, original[a.Symbol], rules, eligibility[a.Symbol])
				limits[a.Symbol] = [2]int{lo, hi}
			}
			return &preferredWeightTotalError{total: preferred, required: total, limits: limits}
		}
	}
	return nil
}
func frozenGroups(groups []pi.RiskGroup, holdings []pi.Holding) []pi.RiskGroup {
	weights := map[string]int{}
	for _, h := range holdings {
		weights[h.Symbol] = h.Weight
	}
	out := []pi.RiskGroup{}
	for _, g := range groups {
		g.Weight = 0
		g.Symbols = append([]string{}, g.Symbols...)
		for _, s := range g.Symbols {
			g.Weight += weights[s]
		}
		out = append(out, g)
	}
	return out
}
func measureImprovements(job Job, target []pi.Holding) []Improvement {
	before := pi.OptimizationReport(job.Source.Request, job.Results)
	req := job.Source.Request
	req.Holdings = target
	after := pi.OptimizationReport(req, job.Results)
	out := []Improvement{}
	add := func(issue, key string, b, a float64) {
		if a < b {
			out = append(out, Improvement{Issue: issue, Metric: key, Before: b, After: a, Kind: "quantitative"})
		}
	}
	// An observed source problem is established by data, not by higher AI scores.
	if before.Metrics.MaxSinglePercent > before.Profile.MaxSinglePercent {
		add("原组合单票集中超参考上限", "max_single_percent", float64(before.Metrics.MaxSinglePercent), float64(after.Metrics.MaxSinglePercent))
	}
	if before.Metrics.TopThreePercent > before.Profile.MaxTopThreePercent {
		add("原组合前三大仓位超参考上限", "top_three_percent", float64(before.Metrics.TopThreePercent), float64(after.Metrics.TopThreePercent))
	}
	for _, g := range groupRiskChecks(job, target) {
		if g.Hard && g.Before > before.Profile.MaxHighRiskPercent {
			add("原组合共同驱动集中："+g.Name, "risk_group."+g.Name, float64(g.Before), float64(g.After))
		}
	}
	out = append(out, investmentImprovements(job, target)...)
	return out
}
func riskAcceptable(job Job, plan Plan) bool {
	for _, check := range planRiskChecks(job, plan) {
		if check.Hard && !check.Passed {
			return false
		}
	}
	return true
}
