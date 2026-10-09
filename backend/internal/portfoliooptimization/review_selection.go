package portfoliooptimization

import "math"

// Rank all feasible alternatives under one common objective before scoring any.
// This is a deterministic search preference, never an AI score or adoption gate.
func selectReviewPlan(job *Job) {
	best := -1
	bestUtility := math.Inf(-1)
	for i := range job.Plans {
		p := &job.Plans[i]
		if p.Status != "pending_review" {
			continue
		}
		p.Status = "not_reviewed"
		p.Error = "程序备选，未独立评分；本轮仅复评程序选定的配置"
		if !p.Checks.Valid || len(p.Improvements) == 0 {
			continue
		}
		safe := true
		p.RiskChecks = planRiskChecks(*job, *p)
		for _, check := range p.RiskChecks {
			if check.Hard && !check.Passed {
				safe = false
			}
		}
		if !safe {
			p.Error = "程序风险约束未通过，未进入复评"
			continue
		}
		similar := false
		for _, old := range job.RevisionHistory {
			if !materiallyDifferent(old.Target, p.Target) {
				similar = true
			}
		}
		if similar {
			p.Error = "与已复评组合仅微小调权，未重复复评"
			continue
		}
		m, err := qualityAllocationModel(*job, Alternative{Name: p.Name, Allocations: p.Allocations})
		if err != nil {
			p.Error = err.Error()
			continue
		}
		w, _, err := weights(p.Target, false)
		if err != nil {
			continue
		}
		values := make([]int, len(p.Allocations))
		for k, a := range p.Allocations {
			values[k] = w[a.Symbol]
		}
		utility, _ := m.utility(values, m.riskAversion)
		if best < 0 || utility > bestUtility+1e-9 || math.Abs(utility-bestUtility) <= 1e-9 && (p.Checks.Sold < job.Plans[best].Checks.Sold || p.Checks.Sold == job.Plans[best].Checks.Sold && baselineHash(p.Target) < baselineHash(job.Plans[best].Target)) {
			best, bestUtility = i, utility
		}
	}
	if best >= 0 {
		job.Plans[best].Status = "pending_review"
		job.Plans[best].Error = ""
	}
}
