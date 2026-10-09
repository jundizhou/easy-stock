package portfoliooptimization

import (
	pi "easy-stock/backend/internal/portfolioinspection"
	"fmt"
	"time"
)

func meetsScoreMinimum(p Plan) bool {
	score, valid := pi.VerifiedOptimizationScore(p.Proposed.Conclusion)
	if !valid || score < MinimumPortfolioScore {
		return false
	}
	if score >= TargetPortfolioScore {
		return true
	}
	original, ok := pi.VerifiedOptimizationScore(p.Original.Conclusion)
	if !ok || score-original < MinimumFallbackImprovement {
		return false
	}
	for _, d := range p.Proposed.Conclusion.Dimensions {
		if d.Score == nil || *d.Score < MinimumFallbackDimension {
			return false
		}
	}
	return true
}

func meetsScoreTarget(p Plan) bool {
	score, valid := pi.VerifiedOptimizationScore(p.Proposed.Conclusion)
	return valid && score >= TargetPortfolioScore
}

// Keep one genuinely reviewed alternative across the bounded refinement. A
// near-target score does not stop the search or get another score sample.
func rememberFallback(j *Job, p Plan) {
	if p.Status != "qualified_alternative" || !meetsScoreMinimum(p) || meetsScoreTarget(p) {
		return
	}
	p.Status = "conditional"
	if !currentComparison(*j, p) {
		return
	}
	if j.FallbackPlan != nil {
		previous, _ := pi.VerifiedOptimizationScore(j.FallbackPlan.Proposed.Conclusion)
		next, _ := pi.VerifiedOptimizationScore(p.Proposed.Conclusion)
		if next < previous || next == previous && p.Checks.Sold >= j.FallbackPlan.Checks.Sold {
			return
		}
	}
	j.FallbackPlan = &p
}

func fallbackOutcomeReason(p Plan) string {
	next, _ := pi.VerifiedOptimizationScore(p.Proposed.Conclusion)
	before, _ := pi.VerifiedOptimizationScore(p.Original.Conclusion)
	return fmt.Sprintf("本次合格备选为%d分，未达到70分优化目标；较原组合%d分提高%d分，四维均不低于50分，并通过独立复评和风险约束。%s", next, before, next-before, p.Assessment.Reason)
}

// A later provider/format failure cannot erase an already valid alternative.
// This publishes only the earlier independent review, never the failed output.
func finishFallbackAfterError(j *Job, cause error) bool {
	if j.FallbackPlan == nil {
		return false
	}
	p := *j.FallbackPlan
	if j.Proposal == nil {
		j.Proposal = j.InvestmentBaseline
	}
	if j.Proposal == nil || !currentComparison(*j, p) || !reviewConfirmsImprovement(*j, p, *p.Assessment) || !riskAcceptable(*j, p) {
		return false
	}
	selected := len(j.Plans)
	for i := range j.Plans {
		if equalWeights(j.Plans[i].Target, p.Target) {
			selected = i
		}
		if j.Plans[i].Status == "pending_review" {
			j.Plans[i].Status = "not_reviewed"
			j.Plans[i].Error = "后续搜索中断，保留此前通过独立复评的方案"
		}
	}
	if selected == len(j.Plans) {
		j.Plans = append(j.Plans, p)
	} else {
		j.Plans[selected] = p
	}
	j.SelectedPlan = &selected
	j.Limitations = append(j.Limitations, "继续优化阶段未完成，未采用失败输出："+cause.Error())
	j.Outcome, j.OutcomeReason = "conditional", fallbackOutcomeReason(p)+"后续搜索未完成，保留此前已验证方案，详见阶段记录。"
	j.Status, j.Stage, j.Message = "succeeded", "completed", "已保留通过独立复评的备选方案；后续搜索中断"
	j.Error = ""
	j.ResumeAvailable = false
	j.CompletedAt = time.Now().UTC()
	j.FallbackPlan = nil
	return true
}

func hasCurrentQualifiedScore(j Job) bool {
	for _, p := range j.Plans {
		if p.Assessment != nil && meetsScoreMinimum(p) {
			return true
		}
	}
	return false
}

func shouldRevise(j Job) bool {
	if j.RevisionCount >= MaxRevisionRounds {
		return false
	}
	for _, p := range j.Plans {
		score, valid := pi.VerifiedOptimizationScore(p.Proposed.Conclusion)
		if p.Assessment != nil && p.Checks.Valid && valid && (score < TargetPortfolioScore || p.Status == "rejected") {
			return true
		}
	}
	return false
}

func archiveRejectedRound(j *Job) {
	if j.InvestmentBaseline == nil {
		j.InvestmentBaseline = j.Proposal
	}
	for _, p := range j.Plans {
		if p.Assessment == nil && p.Status != "invalid_review" {
			continue
		}
		conclusion := p.Proposed.Conclusion
		conclusion.Holdings, conclusion.Scenarios = nil, nil
		j.RevisionHistory = append(j.RevisionHistory, RevisionRound{
			Round: j.RevisionCount + 1, Name: p.Name, Target: p.Target,
			Conclusion: conclusion, Assessment: p.Assessment, Checks: p.Checks,
			Funding: p.Funding, Improvements: p.Improvements,
			RiskGroups: j.Proposal.RiskGroups, Error: p.Error,
		})
	}
}

func repeatedRejectedTarget(j Job, target []pi.Holding) bool {
	for _, round := range j.RevisionHistory {
		if equalWeights(round.Target, target) {
			return true
		}
	}
	return false
}

func bestBelowTarget(j Job) (int, bool) {
	best, found := 0, false
	consider := func(c pi.AIReport) {
		score, valid := pi.VerifiedOptimizationScore(c)
		if valid && score < TargetPortfolioScore && (!found || score > best) {
			best, found = score, true
		}
	}
	for _, p := range j.Plans {
		if p.Assessment != nil {
			consider(p.Proposed.Conclusion)
		}
	}
	for _, round := range j.RevisionHistory {
		consider(round.Conclusion)
	}
	return best, found
}

func modelBudgetKey(j Job) string {
	if j.RevisionCount > 0 {
		return j.Stage + ".round_2"
	}
	return j.Stage
}

// Only the proposer sees these critiques. The independent scorer continues to
// see the common frozen facts and A/B weights without a desired score.
func revisionFeedback(j Job) []any {
	feedback := []any{}
	for _, round := range j.RevisionHistory {
		if round.Assessment == nil {
			continue
		}
		dimensions := []any{}
		for _, d := range round.Conclusion.Dimensions {
			dimensions = append(dimensions, []any{d.Key, d.Score, shortText(d.Reason, 80)})
		}
		w, _, _ := weights(round.Target, false)
		feedback = append(feedback, map[string]any{
			"rejected_weights": w, "score": round.Conclusion.TotalScore,
			"dimensions_key_score_reason": dimensions, "residual_risks": round.Assessment.ResidualRisks,
			"tradeoffs": round.Assessment.Tradeoffs, "rejection_reason": round.Error,
		})
	}
	if len(feedback) > MaxQualityPlans {
		feedback = feedback[len(feedback)-MaxQualityPlans:]
	}
	return feedback
}
