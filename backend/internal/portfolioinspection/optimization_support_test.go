package portfolioinspection

import (
	"easy-stock/backend/internal/stockanalysis"
	"encoding/json"
	"strings"
	"testing"
)

func TestOptimizationDossierKeepsRestrictionsAndAllConditions(t *testing.T) {
	text := strings.Repeat("风险依据", 1000)
	s := stockanalysis.ResearchSynthesis{EvidenceLevel: "insufficient", Thesis: stockanalysis.ResearchClaim{Text: text, SourceIDs: []string{"actual-source"}}, Counter: []stockanalysis.ResearchClaim{{Text: "有反证", SourceIDs: []string{"actual-source"}}}, Decision: stockanalysis.ResearchDecision{Status: "no_plan", Horizon: "swing", Reason: text}, InvalidationIDs: []string{"invalid-last"}}
	for i := 0; i < 8; i++ {
		s.Conditions = append(s.Conditions, stockanalysis.ResearchCondition{ID: strings.Repeat("c", i+1), Text: text, SourceIDs: []string{"actual-source"}})
	}
	s.Conditions[7].ID = "invalid-last"
	rr := &stockanalysis.ResearchReport{ResearchSynthesis: s, Request: stockanalysis.ResearchRequest{Purpose: "observe", Horizon: "swing"}, Sources: []stockanalysis.ResearchSource{{ID: "actual-source", Title: text, Content: text, URL: "https://example.test/" + strings.Repeat("raw-data", 10000)}}}
	r := HoldingResult{Holding: Holding{Weight: 20}, Analysis: &stockanalysis.Analysis{ResearchReport: rr}, AnalysisID: "actual-report"}
	before, _ := json.Marshal(rr)
	d := OptimizationEvidence(r)
	data, _ := json.Marshal(d)
	after, _ := json.Marshal(rr)
	if string(before) != string(after) {
		t.Fatal("mutated original research")
	}
	if d.Research.EvidenceLevel != "insufficient" || d.Research.Decision.Status != "no_plan" || d.OriginalRequest.Purpose != "observe" || len(d.Research.Conditions) != 8 || d.Research.Conditions[7].ID != d.Research.InvalidationIDs[0] || len(d.Research.Counter) != 1 {
		t.Fatal("lost decision restrictions, counter evidence or invalidation")
	}
	if len(data) > 10000 || strings.Contains(string(data), "raw-data") || len(d.Sources) != 1 || d.Sources[0].ID != "actual-source" {
		t.Fatal("unbounded dossier or lost citations", len(data))
	}
}

func TestOptimizationFactsPreserveAvailabilityAndValues(t *testing.T) {
	facts := map[string]Fact{"equity_top_three_percent": {Value: 20, Available: true, Method: "仓位计算"}, "stock.condition.c1": {Available: false, Method: strings.Repeat("原条件", 100), Limitation: "未核验"}}
	data, _ := json.Marshal(OptimizationFacts(facts))
	var compact map[string]Fact
	if err := json.Unmarshal(data, &compact); err != nil {
		t.Fatal(err)
	}
	if compact["equity_top_three_percent"].Value != float64(20) || !compact["equity_top_three_percent"].Available || compact["stock.condition.c1"].Available || compact["stock.condition.c1"].Value != nil {
		t.Fatal("changed factual meaning", compact)
	}
}

func TestCompactComparisonUsesOriginalConditionsAndKeepsEvidenceChecks(t *testing.T) {
	req, results, metrics, score := scoreFixture()
	rr := results[0].Analysis.ResearchReport
	rr.Conditions = []stockanalysis.ResearchCondition{{ID: "confirm", Text: "原研究趋势确认"}, {ID: "invalid", Text: "原研究逻辑失效"}}
	rr.InvalidationIDs = []string{"invalid"}
	score.Holdings = nil
	score.Scenarios = nil
	r := Report{Request: req, Holdings: results, Metrics: metrics}
	data, _ := json.Marshal(score)
	decoded, err := DecodeOptimizationComparisonScore(data, r)
	if err != nil || !decoded.ScoreAvailable || len(decoded.Scenarios) != 0 || len(decoded.Holdings) != 1 {
		t.Fatal("compact comparison did not validate", decoded, err)
	}
	h := decoded.Holdings[0]
	if h.Confirmation != rr.Conditions[0].Text || h.Invalidation != rr.Conditions[1].Text || !strings.Contains(h.Action, "尚未成交") {
		t.Fatal("original research was not preserved", h)
	}
	if _, err := DecodeOptimizationScore(data, r); err == nil {
		t.Fatal("ordinary scoring validation was weakened")
	}
	full := decoded
	if err := validateScoringReport(&full, req, results, metrics); err == nil || !strings.Contains(err.Error(), "组合情景") {
		t.Fatal("ordinary inspection must still require scenarios", err)
	}
	score.Dimensions[0].EvidenceRefs = []EvidenceRef{{ReportID: "report1", SourceID: "invented"}}
	data, _ = json.Marshal(score)
	if _, err := DecodeOptimizationComparisonScore(data, r); err == nil {
		t.Fatal("invented evidence accepted by compact comparison")
	}
	score.Dimensions[0].EvidenceRefs = []EvidenceRef{{Fact: "equity_top_three_percent"}}
	score.Holdings = []HoldingConclusion{{Symbol: req.Holdings[0].Symbol, Conclusion: "虚构", ActionPriority: "观察", Action: "止损价99", Confirmation: "趋势", Invalidation: "失效"}}
	data, _ = json.Marshal(score)
	if _, err := DecodeOptimizationComparisonScore(data, r); err == nil {
		t.Fatal("invented prices accepted by compact comparison")
	}
}

func TestNamedComparisonResolvesOnlyOwnAvailableFacts(t *testing.T) {
	req, results, _, score := scoreFixture()
	r := OptimizationReport(req, results)
	score.Holdings, score.Scenarios = nil, nil
	for _, label := range []string{"a", "b"} {
		for _, suffix := range []string{"equity_top_three_percent", "known_stop_loss_risk_percent", "invented"} {
			for _, prefix := range []string{label, map[string]string{"a": "b", "b": "a"}[label]} {
				score.Dimensions[0].EvidenceRefs = []EvidenceRef{{Fact: prefix + "." + suffix}}
				data, _ := json.Marshal(score)
				decoded, err := DecodeNamedOptimizationComparisonScore(data, r, label)
				if prefix == label && suffix == "equity_top_three_percent" {
					if err != nil {
						t.Fatal(err)
					}
					if decoded.Dimensions[0].EvidenceRefs[0].Fact != suffix || *decoded.Dimensions[0].Score != 70 || *decoded.TotalScore != 71 {
						t.Fatal("facts or scores changed", decoded)
					}
				} else if err == nil {
					t.Fatal("opposite-side or unavailable fact accepted", prefix, suffix)
				}
			}
		}
	}
}
