package portfoliooptimization

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"encoding/json"
	"testing"
	"time"

	pi "easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
)

func searchFixture(t *testing.T) (Job, Alternative) {
	t.Helper()
	j := fixtureJob()
	seed := j.Results[0]
	j.Results = nil
	j.Eligibility = nil
	hs := []pi.Holding{{Symbol: "600519.SH", Weight: 40}, {Symbol: "000858.SZ", Weight: 20}, {Symbol: "000001.SZ", Weight: 10}, {Symbol: "600036.SH", Weight: 10}}
	alt := Alternative{Name: "开放投资区间"}
	for i, h := range hs {
		data, _ := json.Marshal(seed)
		var r pi.HoldingResult
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
		r.Holding = h
		r.Analysis.Symbol = h.Symbol
		r.AnalysisID = "report-" + h.Symbol
		profit, cash, vol := 100.0, 1.0, 2.0
		if i == 0 {
			profit, cash, vol = -100, -1, 7
		}
		r.Analysis.Trend.ATR14Percent = vol
		payload, _ := json.Marshal(map[string]any{"data": map[string]any{"report_date": time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"), "net_profit": profit, "deducted_net_profit": profit, "deducted_net_profit_available": true, "revenue_yoy": 20, "operating_cash_flow_per_share": cash}})
		r.Analysis.ResearchReport.Sources = append(r.Analysis.ResearchReport.Sources, stockanalysis.ResearchSource{ID: "f-financial", Content: string(payload)})
		j.Results = append(j.Results, r)
		j.Eligibility = append(j.Eligibility, Eligibility{Symbol: h.Symbol, CanIncrease: true})
		a := fixtureProposal().Alternatives[0].Allocations[1]
		a.Symbol = h.Symbol
		a.Minimum = 0
		a.Maximum = max(h.Weight, 35)
		a.Preferred = h.Weight
		a.EvidenceRefs = []pi.EvidenceRef{{ReportID: r.AnalysisID, SourceID: "s1"}}
		alt.Allocations = append(alt.Allocations, a)
	}
	j.Source.Request.Holdings = hs
	j.Baseline = hs
	j.Proposal = &Proposal{Alternatives: []Alternative{alt}}
	return j, alt
}

func TestAllocationSearchImprovesActualWeightsRatherThanPreference(t *testing.T) {
	j, a := searchFixture(t)
	before, _ := json.Marshal(j)
	start := time.Now()
	solutions, err := SearchAllocations(context.Background(), j, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(solutions) > MaxQualityPlans {
		t.Fatal("unbounded frontier")
	}
	for _, s := range solutions {
		if equalWeights(s.Target, j.Source.Request.Holdings) || s.Search.ProfitablePercent < 70 || s.Search.LossPercent >= 20 || s.Search.Evaluated < 100 {
			t.Fatal("nearest preference was preserved", s)
		}
		if !Check(j.Baseline, s.Target).Valid || topWeight(s.Target) > 75 {
			t.Fatal("search violated hard constraints", s)
		}
		if s.Search.VolatilityProxy <= 0 {
			t.Fatal("unknown volatility made zero")
		}
	}
	for i := range solutions {
		for k := 0; k < i; k++ {
			if !materiallyDifferent(solutions[i].Target, solutions[k].Target) {
				t.Fatal("nearly identical portfolios received independent slots")
			}
		}
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("small allocation search is too slow")
	}
	after, _ := json.Marshal(j)
	if string(before) != string(after) {
		t.Fatal("frozen inputs changed")
	}
	// The low-quality stock is locked. The search cannot improve by pretending
	// this position may be sold, even though that would lift the objective.
	j.Eligibility[0].Locked = true
	solutions, err = SearchAllocations(context.Background(), j, a)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range solutions {
		w, _, _ := weights(s.Target, false)
		if w[a.Allocations[0].Symbol] != 40 {
			t.Fatal("lock ignored", s)
		}
	}
}

func TestMaterialDifferenceIsBasedOnReplacedFundsNotScoreSampling(t *testing.T) {
	if materiallyDifferent(holds(50, 30), holds(49, 31)) || !materiallyDifferent(holds(50, 30), holds(46, 34)) {
		t.Fatal("material-change boundary")
	}
}

func TestSearchBoundsCancellationAndNoRejectedTargetResampling(t *testing.T) {
	j, a := searchFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SearchAllocations(ctx, j, a); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
	for i := range a.Allocations {
		a.Allocations[i].Minimum = a.Allocations[i].Preferred
		a.Allocations[i].Maximum = a.Allocations[i].Preferred
	}
	s, err := SearchAllocations(context.Background(), j, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 1 || !equalWeights(s[0].Target, j.Baseline) {
		t.Fatal("rigid investment bounds were widened", s)
	}
	j.RevisionHistory = []RevisionRound{{Target: s[0].Target}}
	if _, err := SearchAllocations(context.Background(), j, a); err == nil {
		t.Fatal("rejected weights regraded")
	}
}

func TestReviewSeparatesPlannedExitsFromStaticProtection(t *testing.T) {
	j := fixtureJob()
	p := fixtureProposal()
	j.Proposal = &p
	a := &p.Alternatives[0].Allocations[0]
	a.Conditions = []AllocationCondition{{Kind: "exit", Text: "盈利恶化时减仓", Verification: "下次财报核验", Status: "pending", EvidenceRefs: a.EvidenceRefs}}
	plan := Plan{Allocations: p.Alternatives[0].Allocations}
	for _, hs := range [][]pi.Holding{j.Source.Request.Holdings, holds(45, 35)} {
		req := j.Source.Request
		req.Holdings = hs
		r := pi.OptimizationReport(req, j.Results)
		cfg := reviewConfiguration(j, plan, r)
		if len(cfg["exit_plans"].(map[string][]any)) != 2 || r.Metrics.StopLossCoveragePercent != 0 {
			t.Fatal("planned semantic exits were lost or became static stops", cfg)
		}
	}
}

func TestAllocationReferencesAreQuestionsNotInvestmentPermissions(t *testing.T) {
	j, alt := searchFixture(t)
	for _, r := range j.Results {
		r.Analysis.ResearchReport.EvidenceLevel = "insufficient"
		r.Analysis.ResearchReport.Decision.Status = "observe"
	}
	before, _ := json.Marshal(j)
	ref, err := initialAllocationReferences(j)
	if err != nil || len(ref["rows"].([]any)) == 0 {
		t.Fatal("missing preliminary research direction", ref, err)
	}
	for _, row := range ref["rows"].([]any) {
		weights := row.([]any)[0].([]int)
		target := []pi.Holding{}
		for i, w := range weights {
			if w > 0 {
				target = append(target, pi.Holding{Symbol: j.Results[i].Holding.Symbol, Weight: w})
			}
		}
		if !Check(j.Baseline, target).Valid {
			t.Fatal("reference ignores portfolio constraints", target)
		}
	}
	after, _ := json.Marshal(j)
	if string(before) != string(after) {
		t.Fatal("reference granted investment permission or changed input")
	}
	for i := range alt.Allocations {
		alt.Allocations[i].Investment = nil
	}
	if _, err := SearchAllocations(context.Background(), j, alt); err == nil {
		// Without validated investment permissions, increases cannot be funded.
		// A reference must never be accepted as a permission by production search.
		solutions, _ := SearchAllocations(context.Background(), j, alt)
		old, _, _ := weights(j.Source.Request.Holdings, false)
		for _, s := range solutions {
			for _, h := range s.Target {
				if h.Weight > old[h.Symbol] {
					t.Fatal("unvalidated reference became funded")
				}
			}
		}
	}
}

func TestSearchFallsBackToNonWorseningBoundaryWhenStyleReferenceIsInfeasible(t *testing.T) {
	j, alt := searchFixture(t)
	for i := range alt.Allocations {
		alt.Allocations[i].Minimum = alt.Allocations[i].Preferred
		alt.Allocations[i].Maximum = alt.Allocations[i].Preferred
	}
	// Three fixed holdings account for80% assets; balanced75% is infeasible.
	j.Source.Request.Holdings[0].Weight = 50
	j.Source.Request.Holdings = j.Source.Request.Holdings[:3]
	j.Baseline = append([]pi.Holding(nil), j.Source.Request.Holdings[:3]...)
	alt.Allocations[0].Minimum, alt.Allocations[0].Maximum, alt.Allocations[0].Preferred = 50, 50, 50
	alt.Allocations[3].Minimum, alt.Allocations[3].Maximum, alt.Allocations[3].Preferred = 0, 0, 0
	solutions, err := SearchAllocations(context.Background(), j, alt)
	if err != nil || len(solutions) != 1 || topWeight(solutions[0].Target) != 80 {
		t.Fatal("feasible non-worsening solution discarded", solutions, err)
	}
}

func TestStableValueCanCompeteWithHighGrowthInAllocation(t *testing.T) {
	j, alt := searchFixture(t)
	for i := range j.Results {
		r := &j.Results[i]
		growth := 50.0
		if i == 1 {
			growth = 0
		}
		payload, _ := json.Marshal(map[string]any{"data": map[string]any{"report_date": time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"), "net_profit": 100, "deducted_net_profit": 80, "deducted_net_profit_available": true, "revenue_yoy": growth, "deducted_net_profit_yoy": growth, "operating_cash_flow_per_share": 1}})
		for k := range r.Analysis.ResearchReport.Sources {
			if r.Analysis.ResearchReport.Sources[k].ID == "f-financial" {
				r.Analysis.ResearchReport.Sources[k].Content = string(payload)
			}
		}
		r.Analysis.Theme.Primary = "制造业"
		if i == 1 {
			pe, pb := 20.0, 2.0
			at := foundation.LatestCompletedAStockSession(time.Now()).Add(15 * time.Hour)
			r.CurrentQuote = &foundation.Quote{Symbol: r.Holding.Symbol, TradeTime: at, Valuation: &foundation.StockValuation{Symbol: r.Holding.Symbol, PETTM: &pe, PB: &pb, TradeTime: at, Meta: foundation.SourceMeta{Source: "test"}}}
		}
	}
	m, err := qualityAllocationModel(j, alt)
	if err != nil {
		t.Fatal(err)
	}
	if m.merit[1]+1e-9 < m.merit[2] {
		t.Fatal("zero-growth healthy value structurally loses to growth", m.merit)
	}
	key := j.Results[1].Holding.Symbol + ".valuation.pe_ttm"
	if err := checkRefs(j, []pi.EvidenceRef{{Fact: key}}); err != nil {
		t.Fatal("AI cannot cite admitted valuation", err)
	}
	before := m.merit[1]
	*j.Results[1].CurrentQuote.Valuation.PETTM = 1
	m, err = qualityAllocationModel(j, alt)
	if err != nil || m.merit[1] != before {
		t.Fatal("lower PE directly earns more merit", err, m.merit)
	}
	j.Results[1].CurrentQuote.Valuation.Meta.Stale = true
	m, err = qualityAllocationModel(j, alt)
	if err != nil || m.merit[1] >= before {
		t.Fatal("stale PE still credited", err, m.merit)
	}
	if err := checkRefs(j, []pi.EvidenceRef{{Fact: key}}); err == nil {
		t.Fatal("AI cited stale valuation")
	}
}
