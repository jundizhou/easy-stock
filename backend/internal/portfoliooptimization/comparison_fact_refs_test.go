package portfoliooptimization

import (
	pi "easy-stock/backend/internal/portfolioinspection"
	"reflect"
	"strings"
	"testing"
)

func TestAssessmentScopedFactsRetainConfigurationAndExactValue(t *testing.T) {
	j := fixtureJob()
	a := pi.OptimizationReport(j.Source.Request, j.Results)
	req := j.Source.Request
	req.Holdings = holds(45, 35)
	b := pi.OptimizationReport(req, j.Results)
	a.Conclusion.RiskGroups = []pi.RiskGroup{{Name: "冻结驱动", Symbols: []string{req.Holdings[0].Symbol}}}
	b.Conclusion.RiskGroups = a.Conclusion.RiskGroups
	plan := Plan{Original: a, Proposed: b}
	paths := map[string]pi.Fact{
		"a.portfolio_facts.available.concentration_hhi": pi.OptimizationComparisonFacts(a)["concentration_hhi"],
		"b.portfolio_facts.available.concentration_hhi": pi.OptimizationComparisonFacts(b)["concentration_hhi"],
		"b.risk_exposures.冻结驱动":                         pi.OptimizationComparisonFacts(b)["risk_exposures.冻结驱动"],
	}
	refs := []pi.EvidenceRef{}
	for path := range paths {
		refs = append(refs, pi.EvidenceRef{Fact: path})
	}
	if err := checkComparisonRefs(j, &plan, a, b, refs); err != nil {
		t.Fatal(err)
	}
	for path, want := range paths {
		if !reflect.DeepEqual(plan.Original.Facts[path], want) || !reflect.DeepEqual(plan.Proposed.Facts[path], want) {
			t.Fatal("lost side or changed fact for report display", path, want)
		}
	}
	// Unscoped assessment columns describe both actual values. They must not
	// overwrite the canonical per-side facts used by individual score reasons.
	originalA, originalB := a.Facts["concentration_hhi"], b.Facts["concentration_hhi"]
	pairRefs := []pi.EvidenceRef{{Fact: "portfolio_facts.concentration_hhi"}, {Fact: "risk_exposures.冻结驱动"}}
	if err := checkComparisonRefs(j, &plan, a, b, pairRefs); err != nil {
		t.Fatal(err)
	}
	if pairRefs[0].Fact != "comparison.concentration_hhi" || !reflect.DeepEqual(plan.Proposed.Facts[pairRefs[0].Fact].Value, map[string]any{"a": originalA.Value, "b": originalB.Value}) || !reflect.DeepEqual(plan.Original.Facts["concentration_hhi"], originalA) || !reflect.DeepEqual(plan.Proposed.Facts["concentration_hhi"], originalB) {
		t.Fatal("unscoped reference guessed a side or corrupted original facts", pairRefs, plan.Proposed.Facts)
	}
	for _, ref := range []pi.EvidenceRef{{Fact: "a.portfolio_facts.available.invented"}, {Fact: "b.risk_exposures.未知驱动"}, {Fact: "b.portfolio_facts.available.known_stop_loss_risk_percent"}, {Fact: "a.concentration_hhi", ReportID: "mixed"}} {
		if err := checkComparisonRefs(j, &plan, a, b, []pi.EvidenceRef{ref}); err == nil {
			t.Fatal("invalid comparison path accepted", ref)
		}
	}
}

func TestReviewContractMatchesImprovementAndExcludesCash(t *testing.T) {
	j := fixtureJob()
	p := fixtureProposal()
	j.Proposal = &p
	a := pi.OptimizationReport(j.Source.Request, j.Results)
	req := j.Source.Request
	req.Holdings = holds(45, 35)
	b := pi.OptimizationReport(req, j.Results)
	for _, tc := range []struct{ kind, required string }{
		{"structure", "investment_comparisons必须[]"},
		{"investment", "本次只有投资改善"},
	} {
		plan := Plan{Original: a, Proposed: b, Improvements: []Improvement{{Kind: tc.kind}}}
		prompt, err := pairedPrompt(j, plan)
		if err != nil || !strings.Contains(prompt, tc.required) || !strings.Contains(prompt, "满仓、现金为0、总仓位高低均不加扣分") || !strings.Contains(prompt, "不输出adjustments") {
			t.Fatal("review output/cash contract missing", tc.kind, err)
		}
		if strings.Contains(prompt, "70分") || strings.Contains(prompt, "minimum_portfolio_score") {
			t.Fatal("independent reviewer saw desired score")
		}
	}
}
