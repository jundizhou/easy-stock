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

func partsFixture(t *testing.T) (Job, Proposal, Proposal) {
	t.Helper()
	j, good := fixtureJob(), fixtureProposal()
	// A genuinely researched candidate, deliberately omitted from the response.
	data, _ := json.Marshal(j.Results[0])
	data = []byte(strings.ReplaceAll(string(data), j.Results[0].Holding.Symbol, "300185.SZ"))
	var r pi.HoldingResult
	_ = json.Unmarshal(data, &r)
	r.Holding.Weight = 0
	j.Results = append(j.Results, r)
	j.Eligibility = append(j.Eligibility, Eligibility{Symbol: r.Holding.Symbol, CanIncrease: true})
	j.Candidates = append(j.Candidates, Candidate{Symbol: r.Holding.Symbol, Selected: true})
	a := fixtureProposal().Alternatives[0].Allocations[0]
	a.Symbol, a.Minimum, a.Maximum, a.Preferred, a.Suitable = r.Holding.Symbol, 0, 0, 0, false
	a.Investment.Action, a.Reason = "wait", "与旧股用途重复，本轮保留观察"
	a.EvidenceRefs = []pi.EvidenceRef{{ReportID: r.AnalysisID, SourceID: "s1"}}
	good.Alternatives[0].Allocations = append(good.Alternatives[0].Allocations, a)
	data, _ = json.Marshal(good)
	var bad Proposal
	_ = json.Unmarshal(data, &bad)
	bad.Alternatives[0].Allocations = bad.Alternatives[0].Allocations[:2]
	bad.Alternatives[0].Allocations[0].EvidenceRefs = []pi.EvidenceRef{{Fact: "600519.SH.valuation.pe_ttm"}}
	return j, good, bad
}

func partsPatch(t *testing.T, rows map[string]any) string {
	t.Helper()
	patch := []any{}
	for key, value := range rows {
		patch = append(patch, map[string]any{"key": key, "value": value})
	}
	data, err := json.Marshal(map[string]any{"repairs": patch})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestProposalPartsCollectsMissingRowsAndBadReferencesTogether(t *testing.T) {
	j, good, bad := partsFixture(t)
	frozen, err := decodeInitialProposal(context.Background(), j, programProposalJSON(t, bad))
	var parts *proposalPartsError
	if !errors.As(err, &parts) || len(parts.parts) != 2 || !strings.Contains(err.Error(), "300185.SZ") || !strings.Contains(err.Error(), "pe_ttm") {
		t.Fatal("first invalid reference hid another missing row", err)
	}
	before, _ := json.Marshal(frozen)
	prompt, err := proposalPrompt(j)
	if err != nil {
		t.Fatal(err)
	}
	repairPrompt := modelRepairPrompt(prompt, strings.Repeat("full output must not be resent", 10000), parts)
	_, payload, _ := strings.Cut(prompt, "[资料JSON]\n")
	if len(repairPrompt) > MaxModelPromptBytes || !strings.Contains(repairPrompt, payload) || strings.Contains(repairPrompt, "full output") || strings.Contains(strings.Split(repairPrompt, "[待补项]\n")[1], `"index":`) || !strings.Contains(repairPrompt, "不将PB/利润转正推算为PE") {
		t.Fatal("repair changed frozen data or resent full output", len(repairPrompt))
	}
	updated, err := repairProposalParts(context.Background(), j, parts, partsPatch(t, map[string]any{
		"allocation:600519.SH": programAllocation(good.Alternatives[0].Allocations[0]),
		"allocation:300185.SZ": programAllocation(good.Alternatives[0].Allocations[2]),
	}))
	if err != nil || validateProposal(j, updated) != nil {
		t.Fatal("bounded repair did not complete proposal", err)
	}
	untouched := updated.Alternatives[0].Allocations[1]
	untouched.Preferred = frozen.Alternatives[0].Allocations[1].Preferred
	after, _ := json.Marshal(frozen)
	if !reflect.DeepEqual(untouched, frozen.Alternatives[0].Allocations[1]) || string(before) != string(after) || len(updated.Alternatives[0].Allocations) != 3 {
		t.Fatal("valid row or frozen baseline changed")
	}
	if checkRefs(j, []pi.EvidenceRef{{Fact: "600519.SH.valuation.pe_ttm"}}) == nil {
		t.Fatal("repair invented missing PE")
	}
}

func TestProposalPartsCannotDropRowsOrRewriteValidContent(t *testing.T) {
	for _, kind := range []string{"omitted", "extra_valid_row", "unknown_key", "duplicate", "full_rewrite", "changed_stock", "changed_range", "changed_action", "derived_permission", "unknown_field", "unknown_reference", "null"} {
		t.Run(kind, func(t *testing.T) {
			j, good, bad := partsFixture(t)
			_, err := decodeInitialProposal(context.Background(), j, programProposalJSON(t, bad))
			var parts *proposalPartsError
			if !errors.As(err, &parts) {
				t.Fatal(err)
			}
			a := programAllocation(good.Alternatives[0].Allocations[0])
			rows := map[string]any{"allocation:600519.SH": a, "allocation:300185.SZ": programAllocation(good.Alternatives[0].Allocations[2])}
			switch kind {
			case "omitted":
				delete(rows, "allocation:300185.SZ")
			case "extra_valid_row":
				rows["allocation:000858.SZ"] = programAllocation(good.Alternatives[0].Allocations[1])
			case "unknown_key":
				rows["allocation:999999.SH"] = rows["allocation:300185.SZ"]
				delete(rows, "allocation:300185.SZ")
			case "changed_stock":
				a["symbol"] = "000858.SZ"
			case "changed_range":
				a["max_weight"] = 80
			case "changed_action":
				a["investment"].(map[string]any)["action"] = "hold"
			case "derived_permission":
				a["suitable_for_increase"] = true
			case "unknown_field":
				a["new_authorization"] = true
			case "unknown_reference":
				a["evidence_refs"] = []pi.EvidenceRef{{Fact: "600519.SH.valuation.pe_ttm"}}
			case "null":
				rows["allocation:600519.SH"] = nil
			}
			input := partsPatch(t, rows)
			if kind == "full_rewrite" {
				input = programProposalJSON(t, good)
			}
			if kind == "duplicate" {
				input = strings.Replace(input, "allocation:300185.SZ", "allocation:600519.SH", 1)
			}
			if _, err := repairProposalParts(context.Background(), j, parts, input); err == nil {
				t.Fatal("unsafe or incomplete repair accepted")
			}
		})
	}
}

func TestProposalPartsRepairsAllAffectedSections(t *testing.T) {
	j, good, bad := partsFixture(t)
	refs := good.Alternatives[0].Allocations[0].EvidenceRefs
	badRefs := []pi.EvidenceRef{{Fact: "600519.SH.valuation.pe_ttm"}}
	bad.RiskGroups = []pi.RiskGroup{{Name: "共同波动", Symbols: []string{"600519.SH", "000858.SZ"}, Reason: "共享行业风险", EvidenceRefs: badRefs}}
	bad.IssueDetails = []IssueDetail{{Text: "估值待核验", EvidenceRefs: badRefs}}
	bad.Issues = []string{"估值待核验", "600519市盈率90倍", "与错误无关的摘要"}
	c := InvestmentComparison{FromSymbol: "600519.SH", ToSymbol: "000858.SZ", Dimension: "business", Reason: "经营用途改善", Tradeoff: "减少原股弹性", EvidenceRefs: badRefs}
	bad.InvestmentComparisons = []InvestmentComparison{c}
	_, err := decodeInitialProposal(context.Background(), j, programProposalJSON(t, bad))
	var parts *proposalPartsError
	if !errors.As(err, &parts) || len(parts.parts) != 6 {
		t.Fatal("did not collect all affected components", err)
	}
	g := bad.RiskGroups[0]
	g.EvidenceRefs = refs
	c.EvidenceRefs = append(append([]pi.EvidenceRef(nil), refs...), good.Alternatives[0].Allocations[1].EvidenceRefs...)
	patch := map[string]any{
		"allocation:600519.SH": programAllocation(good.Alternatives[0].Allocations[0]),
		"allocation:300185.SZ": programAllocation(good.Alternatives[0].Allocations[2]),
		"risk_group:0":         g, "issue_detail:0": IssueDetail{Text: "估值未知", EvidenceRefs: refs}, "investment_comparison:0": c,
		"issue_text:1": "600519市盈率未知",
	}
	updated, err := repairProposalParts(context.Background(), j, parts, partsPatch(t, patch))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.Issues, []string{"估值未知", "600519市盈率未知", "与错误无关的摘要"}) {
		t.Fatal("affected summaries not reconciled", updated.Issues)
	}
	g.Symbols = []string{"000858.SZ"}
	patch["risk_group:0"] = g
	if _, err := repairProposalParts(context.Background(), j, parts, partsPatch(t, patch)); err == nil {
		t.Fatal("changed risk membership accepted")
	}
}

type partsGateway struct {
	*feasibilityGateway
	partPatch string
}

func (g *partsGateway) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	if strings.Contains(prompt, "持仓方案局部修复员") {
		g.prompts = append(g.prompts, prompt)
		return agent.PromptResult{Content: g.partPatch}, nil
	}
	return g.feasibilityGateway.Prompt(ctx, prompt)
}

func TestProposalPartsUsesExistingRepairAndStageBudget(t *testing.T) {
	for _, fail := range []bool{false, true} {
		j, good, rangePatch := underfundedProgramFixture(t)
		var raw map[string]any
		_ = json.Unmarshal([]byte(programProposalJSON(t, good)), &raw)
		row := raw["alternatives"].([]any)[0].(map[string]any)["allocations"].([]any)[0].(map[string]any)
		row["evidence_refs"] = []pi.EvidenceRef{{Fact: "600519.SH.valuation.pe_ttm"}}
		data, _ := json.Marshal(raw)
		bad := string(data)
		base := &testGateway{}
		s, _, _ := setupService(t, base)
		patch := partsPatch(t, map[string]any{"allocation:600519.SH": programAllocation(good.Alternatives[0].Allocations[0])})
		if fail {
			patch = `{"repairs":[]}`
		}
		g := &partsGateway{feasibilityGateway: &feasibilityGateway{testGateway: base, proposal: bad, patch: rangePatch}, partPatch: patch}
		s.gateway = g
		j.ModelStageDurationMS = map[string]int64{"proposing": 3 * time.Minute.Milliseconds()}
		ctx, cancel := context.WithTimeout(context.Background(), TotalTimeout)
		err := s.executeFrozen(ctx, &j)
		cancel()
		count := 0
		for _, prompt := range g.prompts {
			if strings.Contains(prompt, "持仓方案局部修复员") {
				count++
			}
		}
		expected := 1
		if fail {
			expected = 2
		}
		if count != expected || !jobRepairUsed(&j) {
			t.Fatal("repair allowance changed", count)
		}
		for _, a := range j.ModelAttempts {
			if a.Stage == "proposing" && a.BudgetMS > 5*time.Minute.Milliseconds() {
				t.Fatal("repair reset stage budget")
			}
		}
		if fail {
			if err == nil || j.Proposal != nil || j.SelectedPlan != nil || j.RangeRepairUsed {
				t.Fatal("invalid patch published or retried", err)
			}
		} else if err != nil || j.Proposal == nil || !j.RangeRepairUsed || j.SelectedPlan == nil {
			t.Fatal("content repair did not continue through range repair and review", err, j.Outcome)
		}
	}
}

// Opt-in real patch of a saved, unmodified bad response. No new stock research,
// score sampling or production-store writes; numeric infeasibility is reported.
func testLiveProposalParts(t *testing.T, s *Service, j Job, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct{ Result agent.PromptResult }
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	j.Version, j.ModelPromptVersion = Version, ModelPromptVersion
	j.Stage, j.RevisionCount = "proposing", 0
	j.ModelStageDurationMS, j.ModelAttempts, j.Limitations = nil, nil, nil
	j.ModelStartedAt, j.ModelDurationMS = time.Time{}, 0
	ctx, cancel := context.WithTimeout(context.Background(), ModelTimeout)
	defer cancel()
	ctx, release, err := agent.BindTask(ctx, s.gateway)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	frozen, err := decodeInitialProposal(ctx, j, saved.Result.Content)
	var parts *proposalPartsError
	if !errors.As(err, &parts) {
		t.Fatal("saved response does not reproduce a partial content failure", err)
	}
	prompt, err := proposalPrompt(j)
	if err != nil {
		t.Fatal(err)
	}
	var updated Proposal
	var numeric *programRangeRepairError
	err = s.model(ctx, &j, parts.prompt(prompt), func(content string) error {
		var err error
		updated, err = repairProposalParts(ctx, j, parts, content)
		if errors.As(err, &numeric) {
			return nil // content valid; this test does not ask for new ranges/scores
		}
		return err
	})
	if err != nil || len(j.ModelAttempts) != 1 {
		t.Fatal("single real local repair did not complete", err)
	}
	changed := map[string]bool{}
	for _, part := range parts.parts {
		if part.kind == "allocation" {
			changed[part.symbol] = true
		}
	}
	for i, old := range frozen.Alternatives[0].Allocations {
		if changed[old.Symbol] {
			continue
		}
		a := updated.Alternatives[0].Allocations[i]
		a.Preferred = old.Preferred
		// An omitted empty conditions list may decode as nil. Compare the saved
		// wire content, including every judgment/ref, not Go slice allocation.
		before, _ := json.Marshal(old)
		after, _ := json.Marshal(a)
		if string(before) != string(after) {
			t.Fatal("patch rewrote an unrequested stock", old.Symbol, string(before), string(after))
		}
	}
	result := map[string]any{"version": Version, "prompt_version": ModelPromptVersion, "content_valid": true, "ranges_pending": numeric != nil, "proposal": updated, "model_attempts": j.ModelAttempts}
	if path := os.Getenv("EASY_STOCK_REPLAY_OUTPUT"); path != "" {
		raw, _ := json.MarshalIndent(result, "", "  ")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("proposal patch: %d requested parts, %d complete stock rows; ranges_pending=%t; input=%d bytes, duration=%dms", len(parts.parts), len(updated.Alternatives[0].Allocations), numeric != nil, j.ModelAttempts[0].PromptBytes, j.ModelAttempts[0].DurationMS)
}
