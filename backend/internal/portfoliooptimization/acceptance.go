package portfoliooptimization

import (
	pi "easy-stock/backend/internal/portfolioinspection"
	"fmt"
	"strings"
)

const strongRiskGroupCorrelation = 0.6

// All pairwise values must be available, finite and above the same threshold.
// Available correlation facts already require at least 20 aligned sessions.
// A single citation about one member's growth cannot establish co-movement.
func stronglyLinkedGroup(group pi.RiskGroup, facts map[string]pi.Fact) bool {
	if len(group.Symbols) < 2 {
		return false
	}
	for i, a := range group.Symbols {
		for _, b := range group.Symbols[i+1:] {
			if a == b {
				return false
			}
			f, ok := facts["correlation."+a+"."+b]
			if !ok {
				f = facts["correlation."+b+"."+a]
			}
			v, number := f.Value.(float64)
			if !f.Available || !number || !finite(v) || v < strongRiskGroupCorrelation || v > 1 {
				return false
			}
		}
	}
	return true
}

func groupRiskChecks(job Job, target []pi.Holding) []RiskCheck {
	proposal := job.Proposal
	if proposal == nil {
		proposal = job.InvestmentBaseline
	}
	if proposal == nil {
		return nil
	}
	facts := pi.OptimizationUnionReport(job.Source.Request, job.Results).Facts
	rules, _ := pi.RulesFor(job.Source.Request.TraderProfile)
	before := frozenGroups(proposal.RiskGroups, job.Source.Request.Holdings)
	after := frozenGroups(proposal.RiskGroups, target)
	checks := []RiskCheck{}
	seenPairs := map[string]bool{}
	for i, g := range after {
		check := RiskCheck{Name: g.Name, Symbols: g.Symbols, Before: before[i].Weight, After: g.Weight, Passed: true,
			Basis: "共同标签未由组内全部股票的强正相关确认，仅供独立复评判断，不作为集中度硬限制"}
		if stronglyLinkedGroup(g, facts) {
			check.Hard = true
			check.Limit = max(before[i].Weight, rules.MaxHighRiskPercent)
			check.Passed = check.After <= check.Limit
			check.Basis = "组内两两历史日收益相关性均≥0.60，样本至少20个交易日；限制新增集中，不代表未来联动保证"
		}
		checks = append(checks, check)
		// A broad mixed group must not hide a genuinely correlated subgroup.
		if !check.Hard {
			for a, left := range g.Symbols {
				for _, right := range g.Symbols[a+1:] {
					pair := pi.RiskGroup{Symbols: []string{left, right}}
					if !stronglyLinkedGroup(pair, facts) {
						continue
					}
					key := left + "/" + right
					if seenPairs[key] || seenPairs[right+"/"+left] {
						continue
					}
					seenPairs[key] = true
					old := frozenGroups([]pi.RiskGroup{pair}, job.Source.Request.Holdings)[0].Weight
					next := frozenGroups([]pi.RiskGroup{pair}, target)[0].Weight
					limit := max(old, rules.MaxHighRiskPercent)
					checks = append(checks, RiskCheck{Name: "已确认联动：" + key, Symbols: pair.Symbols, Before: old, After: next, Limit: limit, Hard: true, Passed: next <= limit, Basis: "宽泛分组内这两只股票的历史相关性≥0.60，仍单独约束其集中度"})
				}
			}
		}
	}
	return checks
}

func planRiskChecks(job Job, plan Plan) []RiskCheck {
	limit := max(plan.Original.Metrics.HighRiskPercent, plan.Original.Profile.MaxHighRiskPercent)
	checks := []RiskCheck{{Name: "量化高风险股票仓位", Before: plan.Original.Metrics.HighRiskPercent, After: plan.Proposed.Metrics.HighRiskPercent, Limit: limit, Hard: true, Passed: plan.Proposed.Metrics.HighRiskPercent <= limit, Basis: "原量化高风险股票仓位不得新增超限"}}
	return append(checks, groupRiskChecks(job, plan.Target)...)
}

// Scores, reviewer preference, factual improvement and executable constraints
// are independent checks. Return every actual failure instead of a generic OR.
func acceptanceReasons(job Job, plan Plan) []string {
	reasons := []string{}
	if !plan.Checks.Valid || !Check(job.Baseline, plan.Target).Valid {
		reasons = append(reasons, "总仓位、现金或累计替换约束未通过")
	}
	if plan.Assessment == nil {
		return append(reasons, "独立复评尚未完成")
	}
	preferred, err := comparisonTargetsProposal(plan.Assessment.Preferred, plan.AssessmentOrder)
	if err != nil || !preferred || !plan.Assessment.Accepted {
		reasons = append(reasons, "独立复评未选择目标组合："+plan.Assessment.Reason)
	} else if !reviewConfirmsImprovement(job, plan, *plan.Assessment) {
		reasons = append(reasons, "复评未给出对应实际减持、增持方向且引用有效的改善依据")
	}
	for _, r := range planRiskChecks(job, plan) {
		if r.Hard && !r.Passed {
			reasons = append(reasons, fmt.Sprintf("%s：%d%%→%d%%，超过本次上限%d%%", r.Name, r.Before, r.After, r.Limit))
		}
	}
	if !meetsScoreMinimum(plan) {
		score, valid := pi.VerifiedOptimizationScore(plan.Proposed.Conclusion)
		if !valid {
			reasons = append(reasons, "独立复评评分不完整或总分核验失败")
		} else {
			reasons = append(reasons, fmt.Sprintf("独立复评综合评分%d分；70分为优化目标，65–69分须较原组合提高至少5分且四维均不低于50分", score))
		}
	}
	return reasons
}

func classifyReviewedPlan(job Job, plan *Plan) {
	plan.RiskChecks = planRiskChecks(job, *plan)
	plan.RejectionReasons = acceptanceReasons(job, *plan)
	plan.Error = strings.Join(plan.RejectionReasons, "；")
	plan.Status = "rejected"
	if len(plan.RejectionReasons) == 0 {
		plan.Status = "conditional"
		if !meetsScoreTarget(*plan) {
			plan.Status = "qualified_alternative"
		}
	}
}
