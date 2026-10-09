package portfoliooptimization

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCompactInvestmentResponsePreservesNamedReportAndValidation(t *testing.T) {
	j, p := fixtureJob(), fixtureProposal()
	i := p.Alternatives[0].Allocations[1].Investment
	named, _ := json.Marshal(i)
	row, _ := json.Marshal([]string{i.Role, i.Action, i.Horizon, i.Business, i.Growth, i.Valuation, i.Timing, i.PortfolioFit, i.Risk, i.Exit, i.OpportunityCost, i.PriorOpinion, i.PeriodSuitability})
	var compact InvestmentJudgment
	if err := json.Unmarshal(row, &compact); err != nil {
		t.Fatal(err)
	}
	back, _ := json.Marshal(compact)
	if string(back) != string(named) {
		t.Fatal("public report shape or investment meaning changed")
	}
	p.Alternatives[0].Allocations[1].Investment = &compact
	if err := validateProposal(j, p); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`["盈利兑现","allocate"]`, `[1,2,3]`} {
		if json.Unmarshal([]byte(bad), &compact) == nil {
			t.Fatal("bad compact structure accepted")
		}
	}
	prompt, err := proposalPrompt(j)
	if err != nil || !strings.Contains(prompt, "investment为13项命名对象") || len(prompt) > MaxInitialModelPromptBytes {
		t.Fatal(err)
	}
}

func TestEarlyClosedAllocationNormalizationPreservesEveryValue(t *testing.T) {
	p := fixtureProposal()
	root := map[string]any{"issues": p.Issues, "risk_groups": p.RiskGroups, "investment_comparisons": p.InvestmentComparisons,
		"alternatives": []any{map[string]any{"name": p.Alternatives[0].Name, "allocations": p.Alternatives[0].Allocations[:1]}, p.Alternatives[0].Allocations[1]}}
	data, _ := json.Marshal(root)
	broken := string(data) + `],"keep_reason":"保持原判断"}`
	corrected, ok := normalizeEarlyClosedAllocations(broken)
	var decoded Proposal
	if !ok || json.Unmarshal([]byte(corrected), &decoded) != nil || len(decoded.Alternatives) != 1 || !reflect.DeepEqual(decoded.Alternatives[0].Allocations, p.Alternatives[0].Allocations) || decoded.KeepReason != "保持原判断" {
		t.Fatal("normalization changed data instead of nesting", corrected)
	}
	if err := validateProposal(fixtureJob(), decoded); err != nil {
		t.Fatal(err)
	}
	for _, tail := range []string{`],"extra":"hidden"}`, `],"keep_reason":3}`, ` instructions`} {
		if _, ok := normalizeEarlyClosedAllocations(string(data) + tail); ok {
			t.Fatal("unknown trailer was guessed", tail)
		}
	}
	root["alternatives"] = []any{p.Alternatives[0], p.Alternatives[0]}
	data, _ = json.Marshal(root)
	if _, ok := normalizeEarlyClosedAllocations(string(data)); ok {
		t.Fatal("real alternatives silently merged")
	}
	root["alternatives"] = []any{map[string]any{"name": p.Alternatives[0].Name, "allocations": p.Alternatives[0].Allocations[:1]}, map[string]any{"symbol": "000858.SZ"}}
	data, _ = json.Marshal(root)
	if _, ok := normalizeEarlyClosedAllocations(string(data)); ok {
		t.Fatal("incomplete row guessed")
	}
}

func TestSavedMalformedProposalNormalization(t *testing.T) {
	path, response := os.Getenv("EASY_STOCK_OPTIMIZATION_AUDIT"), os.Getenv("EASY_STOCK_OPTIMIZATION_RESPONSE")
	if path == "" || response == "" {
		t.Skip("saved evaluation input required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var job Job
	if err = json.Unmarshal(data, &job); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(response)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Result struct{ Content string } `json:"result"`
	}
	if err = json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	content, normalized := normalizeEarlyClosedAllocations(raw.Result.Content)
	var proposal Proposal
	if !normalized {
		t.Fatal("exact recorded structure was not recognized")
	}
	if err = jsonContent(content, &proposal); err != nil {
		t.Fatal(err)
	}
	if err = validateProposal(job, proposal); err != nil {
		var weightError *preferredWeightTotalError
		if !errors.As(err, &weightError) {
			t.Fatal(err)
		}
		prompt, promptErr := proposalPrompt(job)
		if promptErr != nil {
			t.Fatal(promptErr)
		}
		repair := modelRepairPrompt(prompt, raw.Result.Content, err)
		if len(repair) > MaxModelPromptBytes || !strings.Contains(repair, "preferred_weights") {
			t.Fatal("numeric patch did not fit", len(repair))
		}
		t.Logf("nesting normalized; mathematical correction still required: %v; patch input=%d", err, len(repair))
		return
	}
	job.Proposal = &proposal
	for _, alternative := range proposal.Alternatives {
		target, err := Solve(job, alternative)
		if err != nil {
			t.Fatal(err)
		}
		if c := Check(job.Baseline, target); !c.Valid {
			t.Fatal(c.Errors)
		}
		t.Logf("recorded proposal normalized without another model call: target=%v", target)
	}
}

func TestPreferredWeightPatchPreservesOriginalJudgmentAndRanges(t *testing.T) {
	j, frozen := fixtureJob(), fixtureProposal()
	frozen.Alternatives[0].Allocations[0].Maximum = 55
	frozen.Alternatives[0].Allocations[1].Minimum = 30
	frozen.Alternatives[0].Allocations[1].Preferred = 30
	before, _ := json.Marshal(frozen)
	var weightError *preferredWeightTotalError
	if !errors.As(validateProposal(j, frozen), &weightError) {
		t.Fatal("incomplete preferences not detected")
	}
	updated, err := patchPreferredWeights(j, frozen, map[string]int{"600519.SH": 45, "000858.SZ": 35})
	if err != nil {
		t.Fatal(err)
	}
	updated.Alternatives[0].Allocations[1].Preferred = 30
	back, _ := json.Marshal(updated)
	untouched, _ := json.Marshal(frozen)
	if string(back) != string(before) || string(untouched) != string(before) {
		t.Fatal("numeric patch changed judgments, ranges, sources or original proposal")
	}
	for _, patch := range []map[string]int{{"600519.SH": 45, "unknown": 35}, {"600519.SH": 44, "000858.SZ": 36}, {"600519.SH": 45, "000858.SZ": 34}} {
		if _, err := patchPreferredWeights(j, frozen, patch); err == nil {
			t.Fatal("invalid patch accepted", patch)
		}
	}
	j.Eligibility[1].Locked = true
	if _, err := patchPreferredWeights(j, frozen, map[string]int{"600519.SH": 45, "000858.SZ": 35}); err == nil {
		t.Fatal("patch bypassed trading lock")
	}
}

func TestPreferredWeightPatchOnePointProjectionPreservesPermissions(t *testing.T) {
	j, frozen := fixtureJob(), fixtureProposal()
	a := &frozen.Alternatives[0].Allocations[0]
	a.Minimum, a.Maximum, a.Preferred = 45, 65, 59
	a.Suitable, a.Investment.Action = false, "hold"
	b := &frozen.Alternatives[0].Allocations[1]
	b.Minimum, b.Maximum, b.Preferred = 15, 35, 20
	before, _ := json.Marshal(frozen)
	patch := map[string]int{"600519.SH": 61, "000858.SZ": 19}
	updated, err := patchPreferredWeights(j, frozen, patch)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Alternatives[0].Allocations[0].Preferred != 60 || updated.Alternatives[0].Allocations[1].Preferred != 20 {
		t.Fatal("unauthorized increase survived projection")
	}
	for i := range updated.Alternatives[0].Allocations {
		updated.Alternatives[0].Allocations[i].Preferred = frozen.Alternatives[0].Allocations[i].Preferred
	}
	after, _ := json.Marshal(updated)
	original, _ := json.Marshal(frozen)
	if string(after) != string(before) || string(original) != string(before) || patch["600519.SH"] != 61 {
		t.Fatal("projection changed investment content or input")
	}
	if _, err := patchPreferredWeights(j, frozen, map[string]int{"600519.SH": 62, "000858.SZ": 18}); err == nil {
		t.Fatal("larger change silently projected")
	}
	j.Eligibility[0].Locked = true
	updated, err = patchPreferredWeights(j, frozen, patch)
	if err != nil || updated.Alternatives[0].Allocations[0].Preferred != 60 {
		t.Fatal("locked position changed", err)
	}
}

func TestSavedPreferredWeightPatchProjection(t *testing.T) {
	audit, response, patchPath := os.Getenv("EASY_STOCK_OPTIMIZATION_AUDIT"), os.Getenv("EASY_STOCK_OPTIMIZATION_RESPONSE"), os.Getenv("EASY_STOCK_OPTIMIZATION_PATCH")
	if audit == "" || response == "" || patchPath == "" {
		t.Skip("exact saved records required")
	}
	data, err := os.ReadFile(audit)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err = json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var raw struct {
			Result struct{ Content string } `json:"result"`
		}
		if err = json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		return raw.Result.Content
	}
	content, ok := normalizeEarlyClosedAllocations(read(response))
	if !ok {
		t.Fatal("recorded shape changed")
	}
	var frozen Proposal
	if err = jsonContent(content, &frozen); err != nil {
		t.Fatal(err)
	}
	var patch struct {
		Weights map[string]int `json:"preferred_weights"`
	}
	if err = jsonContent(read(patchPath), &patch); err != nil {
		t.Fatal(err)
	}
	updated, err := patchPreferredWeights(j, frozen, patch.Weights)
	if err != nil {
		t.Fatal(err)
	}
	j.Proposal = &updated
	target, err := Solve(j, updated.Alternatives[0])
	if err != nil {
		t.Fatal(err)
	}
	if c := Check(j.Baseline, target); !c.Valid {
		t.Fatal(c.Errors)
	}
	if w, _, _ := weights(target, false); w["688220.SH"] != 24 || w["688512.SH"] != 11 {
		t.Fatal("actual no-increase bounds bypassed", w)
	}
	for i := range updated.Alternatives[0].Allocations {
		updated.Alternatives[0].Allocations[i].Preferred = frozen.Alternatives[0].Allocations[i].Preferred
	}
	before, _ := json.Marshal(frozen)
	after, _ := json.Marshal(updated)
	if string(after) != string(before) {
		t.Fatal("investment content changed")
	}
	t.Logf("exact recorded numeric patch projected within one percentage point: %v", target)
}

// Recompute only program arithmetic on an isolated materialized plan. The saved
// facts, investment body, target and review conditions remain frozen.
func TestSavedPlanFundingAndConstraintConsistency(t *testing.T) {
	path := os.Getenv("EASY_STOCK_OPTIMIZATION_MATERIALIZED")
	if path == "" {
		t.Skip("isolated saved plan required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err = json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	if j.Proposal == nil || len(j.Plans) == 0 {
		t.Fatal("materialized proposal required")
	}
	if err = validateProposal(j, *j.Proposal); err != nil {
		t.Fatal(err)
	}
	for i := range j.Plans {
		p := &j.Plans[i]
		target, err := Solve(j, Alternative{Name: p.Name, Allocations: p.Allocations})
		if err != nil {
			t.Fatal(err)
		}
		if !equalWeights(target, p.Target) {
			t.Fatal("saved target changed under current constraints")
		}
		if c := Check(j.Baseline, p.Target); !c.Valid {
			t.Fatal(c.Errors)
		}
		flows, sold, bought := FundingFlowsCompared(j.Source.Request.Holdings, p.Target, j.Proposal.InvestmentComparisons)
		if sold != bought {
			t.Fatal("cash not conserved")
		}
		p.Funding, p.TradeSold, p.TradeBought = flows, sold, bought
		p.Improvements = measureImprovements(j, p.Target)
		for _, m := range p.Improvements {
			if m.Kind != "investment" {
				continue
			}
			found := false
			for _, f := range flows {
				if f.FromSymbol == m.FromSymbol && f.ToSymbol == m.ToSymbol && f.Weight == m.Weight {
					found = true
				}
			}
			if !found {
				t.Fatal("investment improvement disagrees with actual funding")
			}
		}
		t.Logf("current arithmetic confirmed: sold=%d target=%v", sold, p.Target)
	}
	if output := os.Getenv("EASY_STOCK_OPTIMIZATION_MATERIALIZED_OUTPUT"); output != "" {
		data, err = json.MarshalIndent(j, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExtraAllocationBraceCorrectionPreservesEveryValue(t *testing.T) {
	input := `{"alternatives":[{"name":"原样","allocations":[{"symbol":"600519.SH","investment":{"business":"不改正文","risk":"保留风险"},"preferred_weight":30,"evidence_refs":[{"fact":"cash_percent"}]},{"symbol":"000858.SZ","investment":{"business":"完整保留"},"preferred_weight":50,"evidence_refs":[{"fact":"cash_percent"}]}]}],"keep_reason":"原原因"}`
	// The exact confirmed defect occurs after the first row's final refs.
	defect := strings.Replace(input, `}]},{"symbol"`, `}]}},{"symbol"`, 1)
	if defect == input {
		t.Fatal("fixture missing row transition")
	}
	fixed, changed := normalizeExtraAllocationBraces(defect)
	if !changed || fixed != input {
		t.Fatal("non-lossless syntax correction", changed)
	}
	for _, invalid := range []string{input, defect + "trailer", strings.Replace(defect, `"investment":`, `"unknown":`, 1), `{"text":"}]}} , {\"symbol\":","alternatives":[]}`} {
		unchanged, changed := normalizeExtraAllocationBraces(invalid)
		if changed || unchanged != invalid {
			t.Fatal("guessed unknown shape")
		}
	}
}
