package portfoliooptimization

import (
	pi "easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDossierDoesNotGrowWithUnrelatedFullReportFields(t *testing.T) {
	j := fixtureJob()
	a, err := proposalPrompt(j)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range j.Results {
		r.Analysis.ResearchReport.Sources[0].Content = strings.Repeat("raw_provider_data", 100000)
		r.Analysis.ResearchReport.Sources[0].URL = "https://example.test/" + strings.Repeat("large", 10000)
		r.Analysis.ResearchReport.Scenarios = []stockanalysis.ResearchScenario{{Description: strings.Repeat("full scenario", 10000)}}
	}
	b, err := proposalPrompt(j)
	if err != nil {
		t.Fatal(err)
	}
	if a != b || len(b) > MaxModelPromptBytes {
		t.Fatal("full report leaked across model DTO boundary", len(a), len(b))
	}
}

func TestStockFactTablePreservesCanonicalFactsAndAvailability(t *testing.T) {
	j := fixtureJob()
	facts := map[string]pi.Fact{
		"600519.SH.financial.net_profit":  {Available: true, Value: 0.0},
		"600519.SH.financial.revenue_yoy": {Available: true, Value: -20.0},
		"000858.SZ.financial.net_profit":  {Available: true, Value: 80.0},
		"000858.SZ.financial.revenue_yoy": {Available: false},
		"correlation.600519.SH.000858.SZ": {Available: true, Value: .6},
		"600519.SH.condition.test":        {Available: true, Value: true},
		"600519.SH.weight_percent":        {Available: true, Value: 60},
	}
	table := stockFactTable(facts, j.Results)
	columns := table["columns"].([]string)
	known, unknown := map[string]any{}, map[string]bool{}
	for _, item := range table["rows"].([]any) {
		row := item.(map[string]any)
		unavailable := map[int]bool{}
		for _, i := range row["unavailable"].([]int) {
			unavailable[i] = true
		}
		for i, value := range row["values"].([]any) {
			key := row["symbol"].(string) + "." + columns[i]
			if unavailable[i] {
				unknown[key] = true
			} else {
				known[key] = value
			}
		}
	}
	want := map[string]any{"600519.SH.financial.net_profit": 0.0, "600519.SH.financial.revenue_yoy": -20.0, "000858.SZ.financial.net_profit": 80.0}
	if !reflect.DeepEqual(known, want) || !unknown["000858.SZ.financial.revenue_yoy"] || len(unknown) != 1 {
		t.Fatal("lost, fabricated or changed fact", known, unknown)
	}
	cross := table["cross"].(map[string]any)["available"].(map[string]any)
	if !reflect.DeepEqual(cross, map[string]any{"correlation.600519.SH.000858.SZ": .6}) {
		t.Fatal("cross-stock fact changed", cross)
	}
}

func TestSourceDictionaryReconstructsExactCitationIdentity(t *testing.T) {
	j := fixtureJob()
	for i, r := range j.Results {
		src := &r.Analysis.ResearchReport.Sources[0]
		src.Kind = []string{"financial", "quote"}[i]
		src.TimeStatus = []string{"dated", "quote_reference_only"}[i]
	}
	before, _ := json.Marshal(j)
	d := commonDossier(j, j.Results, 60)
	kinds, times := d["source_kinds"].([]string), d["source_times"].([]string)
	for i, raw := range d["stocks"].([]any) {
		row := raw.([]any)
		index := 0
		for n, name := range stockDossierColumns {
			if name == "sources" {
				index = n
			}
		}
		sources := row[index].([]any)
		if len(sources) != 1 {
			t.Fatal("citations lost", sources)
		}
		s := sources[0].([]any)
		original := j.Results[i].Analysis.ResearchReport.Sources[0]
		if s[0] != original.ID || kinds[s[1].(int)] != original.Kind || times[s[2].(int)] != original.TimeStatus {
			t.Fatal("source dictionary changed meaning", s)
		}
	}
	after, _ := json.Marshal(j)
	if string(before) != string(after) {
		t.Fatal("dictionary encoding changed persisted studies")
	}
}

func TestAllocationInputKeepsCounterfactsWithoutHistoricalQualityGates(t *testing.T) {
	j := fixtureJob()
	for _, r := range j.Results {
		rr := r.Analysis.ResearchReport
		rr.EvidenceLevel, rr.Decision.Status = "insufficient", "no_plan"
		rr.Counter = []stockanalysis.ResearchClaim{{Text: "经营亏损尚未解决", SourceIDs: []string{"s1"}}}
		rr.Decision.Reason = "量能买点仍需观察"
	}
	prompt, err := proposalPrompt(j)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.Split(prompt, "[资料JSON]\n")[1]), &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload["stock_columns"]), `"evidence_level"`) || strings.Contains(string(payload["stock_columns"]), `"decision"`) || !strings.Contains(string(payload["stocks"]), "经营亏损尚未解决") || !strings.Contains(string(payload["stocks"]), "量能买点仍需观察") || len(payload["initial_allocation_frontier"]) == 0 {
		t.Fatal("historical gates or missing counterfacts", string(payload["stocks"]))
	}
}

func TestProposalRangeMustCoverFixedStockPosition(t *testing.T) {
	j, p := fixtureJob(), fixtureProposal()
	p.Alternatives[0].Allocations[1].Minimum = 20
	p.Alternatives[0].Allocations[1].Maximum = 34
	p.Alternatives[0].Allocations[1].Preferred = 34
	if err := validateProposal(j, p); err == nil || !strings.Contains(err.Error(), "权重范围不可行") {
		t.Fatal("underfunded plan was not identified before solving", err)
	}
	p = fixtureProposal()
	p.Alternatives[0].Allocations[1].Minimum = 36
	p.Alternatives[0].Allocations[1].Maximum = 40
	p.Alternatives[0].Allocations[1].Preferred = 36
	if err := validateProposal(j, p); err == nil || !strings.Contains(err.Error(), "权重范围不可行") {
		t.Fatal("overfunded plan was not identified before solving", err)
	}
}

func TestComparisonSendsStockFactsOnceAndNoRejectedCandidates(t *testing.T) {
	j := fixtureJob()
	p := fixtureProposal()
	j.Proposal = &p
	before := j.Source
	req := before.Request
	req.Holdings = holds(45, 35)
	// The report factory is needed to recalculate actual B metrics.
	plan := Plan{Original: before, Proposed: pi.OptimizationReport(req, j.Results)}
	prompt, err := pairedPrompt(j, plan)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.Split(prompt, "[资料JSON]\n")[1]), &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Count(prompt, "可核验逻辑") != 2 || len(payload["stock_facts"]) == 0 || strings.Contains(string(payload["a"]), "condition.") || strings.Contains(prompt, "funding_reason") {
		t.Fatal("repeated facts or optimizer rationale leaked to reviewer")
	}
}

func TestCompactDossierPreservesCounterFactsAndCorrelationIdentity(t *testing.T) {
	j := fixtureJob()
	for _, r := range j.Results {
		rr := r.Analysis.ResearchReport
		rr.Counter = []stockanalysis.ResearchClaim{{Text: "经营仍亏损", SourceIDs: []string{"s1"}}, {Text: "增长存在反转风险", SourceIDs: []string{"s1"}}}
		rr.Decision.Blockers = []string{"待核验主营恢复"}
	}
	before, _ := json.Marshal(j)
	for _, r := range j.Results {
		d := compactStockDossier(r)
		cs := d["counter"].([]dossierClaim)
		if len(cs) != 2 || cs[0].Text != "经营仍亏损" || cs[1].Text != "增长存在反转风险" || len(cs[0].Sources) != 1 {
			t.Fatal("counter evidence lost", d)
		}
		if !reflect.DeepEqual(d["prior_blockers"], r.Analysis.ResearchReport.Decision.Blockers) {
			t.Fatal("blockers lost")
		}
		if d["anchors"] != nil {
			t.Fatal("capacity card must not imply numerical price targets")
		}
	}
	facts := map[string]pi.Fact{
		"600519.SH.financial.net_profit":  {Available: true, Value: -100.0},
		"000858.SZ.financial.net_profit":  {Available: false},
		"correlation.600519.SH.000858.SZ": {Available: true, Value: 0.0},
		"correlation.000858.SZ.600519.SH": {Available: false},
	}
	table := stockFactTable(facts, j.Results)
	rowsBefore, _ := json.Marshal(table["rows"])
	compactCorrelationTable(table, j.Results)
	rowsAfter, _ := json.Marshal(table["rows"])
	if string(rowsBefore) != string(rowsAfter) {
		t.Fatal("stock numeric facts or availability changed")
	}
	symbols := table["correlation_symbols"].([]string)
	seen := map[string]bool{}
	for _, raw := range table["correlations"].([]any) {
		row := raw.([]any)
		key := "correlation." + symbols[row[0].(int)] + "." + symbols[row[1].(int)]
		seen[key] = true
		f := facts[key]
		if f.Available && row[2] != f.Value || !f.Available && row[2] != nil {
			t.Fatal("zero confused with unknown", row)
		}
	}
	if len(seen) != 2 {
		t.Fatal("correlation missing")
	}
	after, _ := json.Marshal(j)
	if string(before) != string(after) {
		t.Fatal("mutated full research")
	}
}

func TestCompactStockFactsPreserveSharedUnknownAndZero(t *testing.T) {
	j := fixtureJob()
	seed := j.Results[0]
	j.Results = nil
	facts := map[string]pi.Fact{}
	for _, symbol := range []string{"600001.SH", "600002.SH", "600003.SH", "600004.SH"} {
		r := seed
		r.Holding.Symbol = symbol
		j.Results = append(j.Results, r)
		facts[symbol+".valuation.trade_date"] = pi.Fact{Value: "2026-09-30", Available: true}
		facts[symbol+".financial.revenue_yoy"] = pi.Fact{Value: 0.0, Available: true}
		facts[symbol+".financial.eps"] = pi.Fact{Available: false}
		facts[symbol+".price"] = pi.Fact{Value: symbol, Available: true}
	}
	table := stockFactTable(facts, j.Results)
	compactSharedStockFacts(table)
	shared := table["shared"].(map[string]any)
	if shared["financial.revenue_yoy"] != 0.0 || shared["financial.eps"] != nil || shared["valuation.trade_date"] != "2026-09-30" {
		t.Fatal(shared)
	}
	for _, item := range table["rows"].([]any) {
		row := item.(map[string]any)
		symbol := row["symbol"].(string)
		rebuilt := map[string]any{}
		for key, v := range shared {
			rebuilt[symbol+"."+key] = v
		}
		for i, key := range table["columns"].([]string) {
			rebuilt[symbol+"."+key] = row["values"].([]any)[i]
		}
		for key, v := range rebuilt {
			f := facts[key]
			if !reflect.DeepEqual(f.Value, v) {
				t.Fatal("fact value changed", key, f, v)
			}
		}
	}
}
