package portfoliooptimization

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	pi "easy-stock/backend/internal/portfolioinspection"
)

func underfundedProgramFixture(t *testing.T) (Job, Proposal, string) {
	t.Helper()
	j, alt := searchFixture(t)
	w := []int{26, 24, 16, 34}
	mins, maxs := []int{20, 20, 10, 10}, []int{30, 24, 16, 18}
	patch := []rangeAdjustment{}
	for i := range j.Results {
		j.Results[i].Holding.Weight = w[i]
		j.Source.Request.Holdings[i].Weight = w[i]
		alt.Allocations[i].Minimum, alt.Allocations[i].Maximum = mins[i], maxs[i]
		alt.Allocations[i].Preferred = mins[i]
		patch = append(patch, rangeAdjustment{Symbol: alt.Allocations[i].Symbol, Minimum: 0, Maximum: 35, Reason: "保持投资方向，放开合理范围以满足固定总仓位"})
	}
	j.Baseline = j.Source.Request.Holdings
	j.Source = pi.OptimizationReport(j.Source.Request, j.Results)
	j.ID, j.Version, j.ModelPromptVersion = "underfunded-ranges", Version, ModelPromptVersion
	j.SnapshotAt = time.Now().UTC()
	j.Proposal = nil
	p := Proposal{Alternatives: []Alternative{alt}}
	for _, a := range alt.Allocations[1:] {
		p.InvestmentComparisons = append(p.InvestmentComparisons, InvestmentComparison{FromSymbol: alt.Allocations[0].Symbol, ToSymbol: a.Symbol, Dimension: "business", Reason: "盈利质量改善", Tradeoff: "可能减少价格弹性", EvidenceRefs: []pi.EvidenceRef{{ReportID: "report-" + alt.Allocations[0].Symbol, SourceID: "s1"}, {ReportID: "report-" + a.Symbol, SourceID: "s1"}}})
	}
	raw, _ := json.Marshal(map[string]any{"allocation_ranges": patch, "keep_reason": ""})
	return j, p, string(raw)
}

func TestRangeRepairFreezesInvestmentAndSolvesFullPosition(t *testing.T) {
	j, p, patch := underfundedProgramFixture(t)
	frozen, err := decodeInitialProposal(context.Background(), j, programProposalJSON(t, p))
	var numeric *programRangeRepairError
	if !errors.As(err, &numeric) || !strings.Contains(err.Error(), "最小合计60%、最大合计88%") {
		t.Fatal("missing typed numeric failure", err)
	}
	before, _ := json.Marshal(frozen)
	updated, err := repairInitialRanges(context.Background(), j, frozen, patch)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for i, a := range updated.Alternatives[0].Allocations {
		total += a.Preferred
		old := frozen.Alternatives[0].Allocations[i]
		a.Minimum, a.Maximum, a.Preferred = old.Minimum, old.Maximum, old.Preferred
		if !reflect.DeepEqual(a, old) {
			t.Fatal("numeric repair rewrote investment", a.Symbol)
		}
	}
	after, _ := json.Marshal(frozen)
	if total != 100 || string(before) != string(after) || len(updated.RangeAdjustments) != 4 {
		t.Fatal("wrong total or mutated baseline", total)
	}
	j.Proposal = &updated
	solutions, err := SearchAllocations(context.Background(), j, updated.Alternatives[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range solutions {
		if !Check(j.Baseline, s.Target).Valid {
			t.Fatal("constraints bypassed")
		}
	}
}

func TestRangeRepairDoesNotGrantPermissionsOrHideInvalidContent(t *testing.T) {
	j, p, patch := underfundedProgramFixture(t)
	for _, kind := range []string{"unknown_stock", "unknown_evidence", "missing_judgment", "invalid_range"} {
		t.Run(kind, func(t *testing.T) {
			raw, _ := json.Marshal(p)
			var bad Proposal
			_ = json.Unmarshal(raw, &bad)
			a := &bad.Alternatives[0].Allocations[0]
			switch kind {
			case "unknown_stock":
				a.Symbol = "999999.SH"
			case "unknown_evidence":
				a.EvidenceRefs = []pi.EvidenceRef{{Fact: "invented"}}
			case "missing_judgment":
				a.Investment = nil
			case "invalid_range":
				a.Minimum = 101
			}
			_, err := decodeInitialProposal(context.Background(), j, programProposalJSON(t, bad))
			var numeric *programRangeRepairError
			if err == nil || errors.As(err, &numeric) {
				t.Fatal("invalid investment became a numeric repair", err)
			}
		})
	}
	frozen, _ := decodeInitialProposal(context.Background(), j, programProposalJSON(t, p))
	for _, kind := range []string{"hold", "locked", "excluded", "extra_field", "changed_action", "unknown_stock"} {
		t.Run(kind, func(t *testing.T) {
			raw, _ := json.Marshal(frozen)
			var f Proposal
			_ = json.Unmarshal(raw, &f)
			job := j
			job.Eligibility = append([]Eligibility(nil), j.Eligibility...)
			input := patch
			switch kind {
			case "hold":
				f.Alternatives[0].Allocations[0].Suitable = false
				f.Alternatives[0].Allocations[0].Investment.Action = "hold"
			case "locked":
				job.Eligibility[0].Locked = true
			case "excluded":
				f.Alternatives[0].Allocations[0].Maximum = 0
			case "extra_field":
				input = strings.TrimSuffix(input, "}") + `,"alternatives":[]}`
			case "changed_action":
				input = strings.Replace(input, `"min_weight":0`, `"min_weight":0,"action":"allocate"`, 1)
			case "unknown_stock":
				input = strings.ReplaceAll(input, "600519.SH", "999999.SH")
			}
			if _, err := repairInitialRanges(context.Background(), job, f, input); err == nil {
				t.Fatal("range patch granted new permission", kind)
			}
		})
	}
}

type feasibilityGateway struct {
	*testGateway
	proposal, patch      string
	badSyntax, failPatch bool
	prompts              []string
}

func (g *feasibilityGateway) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	g.prompts = append(g.prompts, prompt)
	if strings.Contains(prompt, "独立组合复评员") {
		result, err := g.testGateway.Prompt(ctx, prompt)
		if err != nil {
			return result, err
		}
		var decoded struct {
			A, B       json.RawMessage
			Assessment Assessment
		}
		if err = json.Unmarshal([]byte(result.Content), &decoded); err != nil {
			return result, err
		}
		var input struct {
			A, B struct{ Weights map[string]float64 }
		}
		_, payload, _ := strings.Cut(prompt, "[资料JSON]\n")
		_ = json.Unmarshal([]byte(payload), &input)
		pw, ow := input.B.Weights, input.A.Weights
		if decoded.Assessment.Preferred == "a" {
			pw, ow = ow, pw
		}
		for symbol, w := range pw {
			if symbol != "600519.SH" && w > ow[symbol] {
				decoded.Assessment.InvestmentComparisons = []ReviewedInvestmentComparison{{PreferredSymbol: symbol, OtherSymbol: "600519.SH", Dimension: "business", Reason: "盈利质量改善", Tradeoff: "价格弹性降低", EvidenceRefs: []pi.EvidenceRef{{ReportID: "report-600519.SH", SourceID: "s1"}, {ReportID: "report-" + symbol, SourceID: "s1"}}}}
				break
			}
		}
		raw, _ := json.Marshal(decoded)
		result.Content = string(raw)
		return result, nil
	}
	if strings.Contains(prompt, "仓位范围修复员") {
		if g.failPatch {
			return agent.PromptResult{Content: `{"allocation_ranges":[]}`}, nil
		}
		return agent.PromptResult{Content: g.patch}, nil
	}
	if g.badSyntax && len(g.prompts) == 1 {
		return agent.PromptResult{Content: `{"issues":`}, nil
	}
	return agent.PromptResult{Content: g.proposal + "\n```"}, nil
}

func TestFeasibilityRepairUsesBoundedOperationLoop(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		badSyntax, failPatch, alreadyUsed bool
	}{{"dangling_fence", false, false, false}, {"syntax_then_range", true, false, false}, {"invalid_patch_stops", true, true, false}, {"no_second_range_repair", false, false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			j, p, patch := underfundedProgramFixture(t)
			base := &testGateway{}
			s, _, _ := setupService(t, base)
			g := &feasibilityGateway{testGateway: base, proposal: programProposalJSON(t, p), patch: patch, badSyntax: tc.badSyntax, failPatch: tc.failPatch}
			s.gateway = g
			j.Stage = "proposing"
			j.RangeRepairUsed = tc.alreadyUsed
			j.ModelStageDurationMS = map[string]int64{"proposing": 3 * time.Minute.Milliseconds()}
			ctx, cancel := context.WithTimeout(context.Background(), TotalTimeout)
			defer cancel()
			err := s.executeFrozen(ctx, &j)
			count := 0
			for _, prompt := range g.prompts {
				if strings.Contains(prompt, "仓位范围修复员") {
					count++
					if len(prompt) > MaxRevisionModelPromptBytes {
						t.Fatal("range repair prompt oversized")
					}
				}
			}
			expected := 1
			if tc.failPatch {
				expected = 3
			}
			if count != expected || !j.RangeRepairUsed || !jobRepairUsed(&j) {
				t.Fatal("unexpected bounded repairs", count, j.Limitations)
			}
			for _, a := range j.ModelAttempts {
				if a.Stage == "proposing" && a.BudgetMS > 5*time.Minute.Milliseconds() {
					t.Fatal("range repair reset stage budget", a.BudgetMS)
				}
			}
			if tc.failPatch {
				if err == nil || j.SelectedPlan != nil {
					t.Fatal("invalid patch adopted")
				}
				return
			}
			if err != nil || j.SelectedPlan == nil || j.RevisionCount != 0 {
				t.Fatal("production flow failed", err, j.OutcomeReason)
			}
			if _, err := ApplyRequest(j); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDanglingJSONFenceNeverDiscardsSubstantiveContent(t *testing.T) {
	for _, input := range []string{"{\"a\":1}\n```", "```json\n{\"a\":1}\n```", "{\"a\":1}"} {
		var v map[string]int
		if jsonContent(input, &v) != nil || v["a"] != 1 {
			t.Fatal(input)
		}
	}
	for _, input := range []string{"{\"a\":1}\n```\n改为其他方案", "{\"a\":1}{\"b\":2}\n```", "{\"a\":\n```"} {
		var v any
		if jsonContent(input, &v) == nil {
			t.Fatal("discarded content", input)
		}
	}
}

func TestZeroWatchNeedsNoEntryAndRowReferencesStayStockSpecific(t *testing.T) {
	j, p, _ := underfundedProgramFixture(t)
	a := p.Alternatives[0].Allocations[0]
	a.Investment.Action = "wait"
	a.Suitable = false
	a.Minimum, a.Maximum, a.Preferred = 0, 0, 0
	a.Conditions, a.ConfirmationIDs, a.InvalidationIDs = nil, nil, nil
	j.Results[0].Holding.Weight = 0
	if err := validateAllocationConditions(j, a); err != nil {
		t.Fatal("zero-watch requires an unused plan", err)
	}
	a.Suitable = true
	a.Maximum = 10
	if validateAllocationConditions(j, a) == nil {
		t.Fatal("real waiting allocation lacks conditions")
	}
	a = p.Alternatives[0].Allocations[1]
	a.EvidenceRefs = []pi.EvidenceRef{{ReportID: j.Results[0].AnalysisID, SourceID: "s1"}}
	a.Conditions = []AllocationCondition{{Kind: "exit", Text: "盈利恶化退出", Verification: "下期财报", Status: "pending", EvidenceRefs: []pi.EvidenceRef{{ReportID: j.Results[1].AnalysisID, SourceID: "s1"}}}}
	if err := validateInvestment(j, a); err != nil {
		t.Fatal("own supplied condition reference ignored", err)
	}
	a.Conditions[0].EvidenceRefs = []pi.EvidenceRef{{ReportID: j.Results[0].AnalysisID, SourceID: "s1"}}
	if validateInvestment(j, a) == nil {
		t.Fatal("other stock reference was used")
	}
	a.Conditions[0].EvidenceRefs = []pi.EvidenceRef{{Fact: "missing"}}
	if validateInvestment(j, a) == nil {
		t.Fatal("unknown condition reference was used")
	}
}

func TestSavedProgramProposalUsesOriginalZeroWatchAndReferences(t *testing.T) {
	path := os.Getenv("EASY_STOCK_RANGE_PROPOSAL_AUDIT")
	if path == "" {
		t.Skip("explicit private model response and frozen job required")
	}
	data, err := os.ReadFile(os.Getenv("EASY_STOCK_OPTIMIZATION_AUDIT"))
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err = json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Result agent.PromptResult `json:"result"`
	}
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	var original Proposal
	if err = jsonContent(saved.Result.Content, &original); err != nil {
		t.Fatal(err)
	}
	p, err := decodeInitialProposal(context.Background(), j, saved.Result.Content)
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range p.Alternatives[0].Allocations {
		old := original.Alternatives[0].Allocations[i]
		if !reflect.DeepEqual(a.Investment, old.Investment) || !reflect.DeepEqual(a.Conditions, old.Conditions) || !reflect.DeepEqual(a.EvidenceRefs, old.EvidenceRefs) || a.Minimum != old.Minimum || a.Maximum != old.Maximum {
			t.Fatal("real response content changed", a.Symbol)
		}
	}
	t.Log("exact saved first response passes with original investment, conditions, references and ranges; code only supplies program-mode weights")
}
