package portfoliooptimization

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	pi "easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
)

func proposalCorrelationFixture() Job {
	j := fixtureJob()
	for _, r := range j.Results {
		for i := 0; i < 25; i++ {
			r.Analysis.Chart = append(r.Analysis.Chart, stockanalysis.TrendPoint{Date: j.AsOf.AddDate(0, 0, i-30).Format("2006-01-02"), Close: 100 + float64(i*i+i%3)})
		}
	}
	return j
}

func TestProposalReferencesCanonicalizeOnlyAvailableFacts(t *testing.T) {
	j := proposalCorrelationFixture()
	canonical := "correlation.600519.SH.000858.SZ"
	for _, key := range []string{canonical, "cross." + canonical, "stock_facts.cross.available." + canonical, "600519.SH.cross." + canonical, "000858.SZ.stock_facts.cross.available." + canonical, "cross.correlation.000858.SZ.600519.SH"} {
		refs := []pi.EvidenceRef{{Fact: key}}
		if err := checkRefs(j, refs); err != nil || refs[0].Fact != canonical {
			t.Fatal(key, refs, err)
		}
		c := InvestmentComparison{FromSymbol: "600519.SH", ToSymbol: "000858.SZ", Dimension: "portfolio_fit", Reason: "比较已测联动", Tradeoff: "历史关系会改变", EvidenceRefs: []pi.EvidenceRef{{Fact: key}}}
		if err := validateInvestmentComparison(j, c); err != nil {
			t.Fatal("alias broke investment evidence coverage", key, err)
		}
		c.Dimension = "business"
		if err := validateInvestmentComparison(j, c); err == nil {
			t.Fatal("correlation cannot prove earnings quality")
		}
	}
	for _, bad := range []pi.EvidenceRef{
		{Fact: "cross.correlation.600519.SH.unknown"},
		{Fact: "000001.SZ.cross." + canonical},
		{Fact: "a.cross." + canonical},
		{Fact: "cross." + canonical, ReportID: j.Results[0].AnalysisID},
		{Fact: "cross." + canonical, SourceID: "s1"},
	} {
		refs := []pi.EvidenceRef{{Fact: "cross." + canonical}, bad}
		original := append([]pi.EvidenceRef(nil), refs...)
		if err := checkRefs(j, refs); err == nil || !reflect.DeepEqual(refs, original) {
			t.Fatal("invalid evidence accepted or partially rewritten", refs, err)
		}
	}
	for _, r := range j.Results {
		r.Analysis.Chart = r.Analysis.Chart[:10]
	}
	for _, key := range []string{"cross." + canonical, "600519.SH.cross." + canonical} {
		if err := checkRefs(j, []pi.EvidenceRef{{Fact: key}}); err == nil {
			t.Fatal("unmeasured correlation became available", key)
		}
	}
}

func TestProgramProposalKeepsInvestmentAndWeightsWhenFixingFactPaths(t *testing.T) {
	j := proposalCorrelationFixture()
	p := fixtureProposal()
	p.RiskGroups = []pi.RiskGroup{{Name: "共同驱动", Symbols: []string{"600519.SH", "000858.SZ"}, Reason: "历史联动", EvidenceRefs: []pi.EvidenceRef{{Fact: "correlation.600519.SH.000858.SZ"}}}}
	p.InvestmentComparisons = []InvestmentComparison{{FromSymbol: "600519.SH", ToSymbol: "000858.SZ", Dimension: "portfolio_fit", Reason: "降低共同驱动暴露", Tradeoff: "历史关系会改变", EvidenceRefs: p.RiskGroups[0].EvidenceRefs}}
	encode := func(proposal Proposal) string {
		data, _ := json.Marshal(proposal)
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		raw["weight_mode"] = "program"
		for _, alt := range raw["alternatives"].([]any) {
			for _, a := range alt.(map[string]any)["allocations"].([]any) {
				row := a.(map[string]any)
				for _, key := range []string{"preferred_weight", "suitable_for_increase", "suitability_reason"} {
					delete(row, key)
				}
			}
		}
		data, _ = json.Marshal(raw)
		return string(data)
	}
	base := encode(p)
	want, err := decodeInitialProposal(context.Background(), j, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"cross.correlation.600519.SH.000858.SZ", "600519.SH.cross.correlation.600519.SH.000858.SZ"} {
		got, err := decodeInitialProposal(context.Background(), j, strings.ReplaceAll(base, "correlation.600519.SH.000858.SZ", key))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("path-only correction changed investments/weights or failed", key, err)
		}
		if err := validateProposal(j, got); err != nil {
			t.Fatal(err)
		}
	}
}

// Read-only regression against the exact frozen facts from a failed local job.
// It validates the recorded bad paths without calling a model or user store.
func TestSavedProposalReferenceAliases(t *testing.T) {
	path := os.Getenv("EASY_STOCK_REFERENCE_AUDIT")
	if path == "" {
		t.Skip("private frozen task is opt-in")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	facts := pi.OptimizationUnionReport(j.Source.Request, j.Results).Facts
	key := "correlation.301536.SZ.688220.SH"
	f := facts[pi.OptimizationFactAlias(key, facts)]
	if !f.Available {
		t.Fatal("recorded correlation was actually unavailable")
	}
	t.Logf("frozen correlation exists: value=%v method=%s", f.Value, f.Method)
	checked := 0
	for _, a := range j.ModelAttempts {
		if !strings.HasPrefix(a.Error, "不可用事实引用") {
			continue
		}
		refs := []pi.EvidenceRef{{Fact: strings.TrimPrefix(a.Error, "不可用事实引用")}}
		if err := checkRefs(j, refs); err != nil {
			t.Fatal(err)
		}
		if refs[0].Fact != pi.OptimizationFactAlias(key, facts) {
			t.Fatal("reference did not resolve to the same measured pair", refs)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no recorded reference failures")
	}
	t.Logf("%d recorded alias failures checked without new model calls", checked)
}
