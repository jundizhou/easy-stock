package portfoliooptimization

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	pi "easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
)

func TestIndependentImprovementUsesNetDirectionsNotFundingPairs(t *testing.T) {
	j, alt := searchFixture(t)
	j.Source = pi.OptimizationReport(j.Source.Request, j.Results)
	target := []pi.Holding{{Symbol: "600519.SH", Weight: 25}, {Symbol: "000858.SZ", Weight: 15}, {Symbol: "000001.SZ", Weight: 20}, {Symbol: "600036.SH", Weight: 20}}
	req := j.Source.Request
	req.Holdings = target
	refs := []pi.EvidenceRef{{ReportID: "report-600519.SH", SourceID: "s1"}, {ReportID: "report-600036.SH", SourceID: "s1"}}
	p := Plan{Target: target, Original: j.Source, Proposed: pi.OptimizationReport(req, j.Results), Checks: Check(j.Baseline, target), Allocations: alt.Allocations, AssessmentOrder: "original_first",
		Improvements: []Improvement{{Kind: "investment", FromSymbol: "600519.SH", ToSymbol: "000001.SZ", Metric: "investment.business"}, {Kind: "investment", FromSymbol: "000858.SZ", ToSymbol: "600036.SH", Metric: "investment.risk"}},
		Assessment:   &Assessment{Preferred: "b", Accepted: true, Reason: "盈利角色改善", InvestmentComparisons: []ReviewedInvestmentComparison{{OtherSymbol: "600519.SH", PreferredSymbol: "600036.SH", Dimension: "growth", Reason: "增长更可持续", Tradeoff: "弹性降低", EvidenceRefs: refs}}}}
	p.Original.Conclusion = qualityScore(54)
	p.Proposed.Conclusion = qualityScore(72)
	classifyReviewedPlan(j, &p)
	if p.Status != "conditional" || len(p.RejectionReasons) != 0 {
		t.Fatal("valid independent cross-pair comparison rejected", p.Error)
	}
	// Blind A/B order changes labels, never actual funding direction.
	p.AssessmentOrder = "target_first"
	p.Assessment.Preferred = "a"
	classifyReviewedPlan(j, &p)
	if p.Status != "conditional" {
		t.Fatal(p.Error)
	}
	j.Version, j.ModelPromptVersion, j.Status = Version, ModelPromptVersion, "succeeded"
	selected := 0
	j.Plans, j.SelectedPlan = []Plan{p}, &selected
	if _, err := ApplyRequest(j); err != nil {
		t.Fatal("valid independent comparison cannot be applied", err)
	}
	for _, bad := range []string{"reversed", "unchanged", "unavailable", "score_only", "neither"} {
		t.Run(bad, func(t *testing.T) {
			raw, _ := json.Marshal(p)
			var copyPlan Plan
			_ = json.Unmarshal(raw, &copyPlan)
			c := &copyPlan.Assessment.InvestmentComparisons[0]
			switch bad {
			case "reversed":
				c.PreferredSymbol, c.OtherSymbol = c.OtherSymbol, c.PreferredSymbol
			case "unchanged":
				copyPlan.Target = append([]pi.Holding(nil), j.Source.Request.Holdings...)
			case "unavailable":
				c.EvidenceRefs = []pi.EvidenceRef{{Fact: "unknown"}}
			case "score_only":
				copyPlan.Assessment.InvestmentComparisons = nil
			case "neither":
				copyPlan.Assessment.Preferred = "neither"
				copyPlan.Assessment.Accepted = false
			}
			classifyReviewedPlan(j, &copyPlan)
			if copyPlan.Status != "rejected" || len(copyPlan.RejectionReasons) == 0 {
				t.Fatal("invalid comparison accepted", bad)
			}
			// A stored selected index/status cannot bypass the same acceptance rules.
			copyPlan.Status = "conditional"
			j.Plans = []Plan{copyPlan}
			if _, err := ApplyRequest(j); err == nil {
				t.Fatal("invalid saved comparison applied", bad)
			}
		})
	}
}

func TestRiskGroupRequiresMeasuredLinkageNotSharedLabel(t *testing.T) {
	g := pi.RiskGroup{Name: "中报盈利兑现", Symbols: []string{"A", "B", "C"}}
	facts := map[string]pi.Fact{}
	for _, tc := range []struct {
		v               float64
		available, want bool
	}{{.8, true, true}, {.6, true, true}, {.59, true, false}, {-.8, true, false}, {.8, false, false}, {math.NaN(), true, false}, {1.01, true, false}} {
		for _, key := range []string{"correlation.A.B", "correlation.A.C", "correlation.B.C"} {
			facts[key] = pi.Fact{Available: tc.available, Value: tc.v}
		}
		if stronglyLinkedGroup(g, facts) != tc.want {
			t.Fatal(tc)
		}
	}
	delete(facts, "correlation.A.C")
	if stronglyLinkedGroup(g, facts) {
		t.Fatal("one pair missing")
	}
	// Labels alone have no authority to bypass or impose a hard constraint.
	facts = map[string]pi.Fact{"A.financial.revenue_yoy": {Available: true, Value: 30.0}}
	if stronglyLinkedGroup(g, facts) {
		t.Fatal("financial growth became co-movement")
	}
}

func linkedSearchFixture(t *testing.T) (Job, Alternative) {
	j, alt := searchFixture(t)
	for n, r := range j.Results {
		if n != 1 && n != 2 {
			continue
		}
		for i := 0; i < 25; i++ {
			r.Analysis.Chart = append(r.Analysis.Chart, stockanalysis.TrendPoint{Date: j.AsOf.AddDate(0, 0, i-30).Format("2006-01-02"), Close: 100 + float64(i*i+i%3)})
		}
	}
	j.Source = pi.OptimizationReport(j.Source.Request, j.Results)
	j.Proposal.RiskGroups = []pi.RiskGroup{{Name: "中报盈利兑现依赖", Symbols: []string{"000858.SZ", "000001.SZ", "600036.SH"}}}
	return j, alt
}
func TestMixedRiskGroupPreservesHardCorrelatedSubsetAndSearchLimit(t *testing.T) {
	j, alt := linkedSearchFixture(t)
	target := []pi.Holding{{Symbol: "600519.SH", Weight: 20}, {Symbol: "000858.SZ", Weight: 30}, {Symbol: "000001.SZ", Weight: 30}}
	req := j.Source.Request
	req.Holdings = target
	plan := Plan{Original: j.Source, Proposed: pi.OptimizationReport(req, j.Results), Target: target}
	checks := planRiskChecks(j, plan)
	if len(checks) != 3 || checks[1].Hard || !checks[2].Hard || checks[2].Passed || checks[2].After != 60 || checks[2].Limit != 55 || riskAcceptable(j, plan) {
		t.Fatal(checks)
	}
	// Removing the unsupported grouping must not change the solver objective.
	m, err := buildQualityAllocationModel(j, alt, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.groups) != 1 || m.riskWithinLimits([]int{20, 30, 30, 0}) {
		t.Fatal("real bound not in search", m.groups)
	}
	solutions, err := SearchAllocations(context.Background(), j, alt)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range solutions {
		req.Holdings = s.Target
		if !riskAcceptable(j, Plan{Original: j.Source, Proposed: pi.OptimizationReport(req, j.Results), Target: s.Target}) {
			t.Fatal("search returned a known risk rejection")
		}
	}
	alt.Allocations[1].Minimum = 30
	alt.Allocations[1].Preferred = 30
	alt.Allocations[2].Minimum = 30
	alt.Allocations[2].Preferred = 30
	if _, err := SearchAllocations(context.Background(), j, alt); err == nil {
		t.Fatal("impossible joint risk bound ignored")
	}
	j.Proposal.RiskGroups = []pi.RiskGroup{{Name: "同一报告期", Symbols: []string{"600519.SH", "600036.SH"}}}
	m, err = buildQualityAllocationModel(j, j.Proposal.Alternatives[0], true)
	if err != nil || len(m.groups) != 0 {
		t.Fatal("unmeasured label still penalized allocation", err)
	}
}

func TestRejectedHighScoreGetsOneRevisionWithConcreteReason(t *testing.T) {
	j := fixtureJob()
	p := Plan{Status: "rejected", Checks: Checks{Valid: true}, Assessment: &Assessment{Reason: "需要控制联动"}, Proposed: pi.Report{Conclusion: qualityScore(72)}, Error: "已确认联动：60%超过55%"}
	j.Proposal = &Proposal{}
	j.Plans = []Plan{p}
	if !shouldRevise(j) {
		t.Fatal("72-point rejection stopped refinement")
	}
	archiveRejectedRound(&j)
	feedback, _ := json.Marshal(revisionFeedback(j))
	if !strings.Contains(string(feedback), p.Error) {
		t.Fatal("specific failed check absent from refinement", string(feedback))
	}
	j.RevisionCount = MaxRevisionRounds
	if shouldRevise(j) {
		t.Fatal("revision cap reset")
	}
	j.RevisionCount = 0
	j.Plans[0].Status = "conditional"
	if shouldRevise(j) {
		t.Fatal("qualified score resampled")
	}
	before, _ := json.Marshal(p.Proposed.Conclusion)
	classifyReviewedPlan(j, &p)
	after, _ := json.Marshal(p.Proposed.Conclusion)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("admission rewrote score")
	}
}

func TestHighRiskExposureUsesSameBoundInSearchAndAcceptance(t *testing.T) {
	for _, existingExcess := range []bool{false, true} {
		j, alt := searchFixture(t)
		second := 2
		limit := 55
		if existingExcess {
			second, limit = 0, 60
		}
		j.Results[1].Analysis.RiskControl.Score = 75
		j.Results[second].Analysis.RiskControl.Score = 75
		j.Source = pi.OptimizationReport(j.Source.Request, j.Results)
		m, err := buildQualityAllocationModel(j, alt, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(m.groupLimits) != 1 || m.groupLimits[0] != limit {
			t.Fatal("search risk limit differs", m.groupLimits)
		}
		values := []int{0, 30, 0, 0}
		values[second] = limit - 30
		if !m.riskWithinLimits(values) {
			t.Fatal("existing/style boundary rejected")
		}
		values[second]++
		if m.riskWithinLimits(values) {
			t.Fatal("risk boundary exceeded")
		}
		solutions, err := SearchAllocations(context.Background(), j, alt)
		if err != nil {
			t.Fatal(err)
		}
		for _, solution := range solutions {
			req := j.Source.Request
			req.Holdings = solution.Target
			p := Plan{Original: j.Source, Proposed: pi.OptimizationReport(req, j.Results), Target: solution.Target}
			if p.Proposed.Metrics.HighRiskPercent > limit || !riskAcceptable(j, p) {
				t.Fatal("solver returned unreviewable risk", p.Proposed.Metrics)
			}
		}
	}
}
