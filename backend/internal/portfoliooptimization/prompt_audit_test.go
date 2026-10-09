package portfoliooptimization

import (
	"easy-stock/backend/internal/agent"
	pi "easy-stock/backend/internal/portfolioinspection"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSavedProposalAgainstFrozenFacts(t *testing.T) {
	path, response := os.Getenv("EASY_STOCK_OPTIMIZATION_AUDIT"), os.Getenv("EASY_STOCK_OPTIMIZATION_SAVED_PROPOSAL")
	if path == "" || response == "" {
		t.Skip("saved proposal audit is opt-in")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(response)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Result agent.PromptResult `json:"result"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	var p Proposal
	if err := jsonContent(raw.Result.Content, &p); err != nil {
		t.Fatal(err)
	}
	for _, c := range p.InvestmentComparisons {
		if err := checkRefs(j, c.EvidenceRefs); err != nil {
			t.Logf("comparison %s->%s %s refs=%v error=%v", c.FromSymbol, c.ToSymbol, c.Dimension, c.EvidenceRefs, err)
		}
	}
	if err := validateProposal(j, p); err != nil {
		t.Fatal(err)
	}
}

// Inspect a saved local job without starting models or modifying its store.
func TestSavedOptimizationPromptAudit(t *testing.T) {
	path := os.Getenv("EASY_STOCK_OPTIMIZATION_AUDIT")
	if path == "" {
		t.Skip("set EASY_STOCK_OPTIMIZATION_AUDIT to inspect a saved job")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	for _, r := range j.Results {
		d := stockDossier(r, 12)
		fieldBytes := map[string]int{}
		for k, v := range d {
			data, _ := json.Marshal(v)
			fieldBytes[k] = len(data)
		}
		t.Logf("symbol=%s minimal_dossier_field_bytes=%v", r.Holding.Symbol, fieldBytes)
	}
	proposal, err := proposalPrompt(j)
	if err != nil {
		for k, v := range commonDossier(j, j.Results, 12) {
			data, _ := json.Marshal(v)
			t.Logf("minimal_common_field=%s bytes=%d", k, len(data))
		}
		t.Fatal(err)
	}
	t.Logf("stocks=%d proposal_prompt_bytes=%d", len(j.Results), len(proposal))
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.Split(proposal, "[资料JSON]\n")[1]), &payload); err != nil {
		t.Fatal(err)
	}
	for k, v := range payload {
		t.Logf("proposal field=%s bytes=%d", k, len(v))

	}
	for i, p := range j.Plans {
		review, err := pairedPrompt(j, p)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("plan=%d review_prompt_bytes=%d", i, len(review))
		payload = map[string]json.RawMessage{}
		_ = json.Unmarshal([]byte(strings.Split(review, "[资料JSON]\n")[1]), &payload)
		for k, v := range payload {
			t.Logf("review field=%s bytes=%d", k, len(v))
		}
		// Include the new policy's actual output fields instead of testing only
		// a legacy plan with no portfolio-level conditions.
		for n := range p.Allocations {
			a := &p.Allocations[n]
			if r, ok := researchFor(j, a.Symbol); ok && len(r.Analysis.ResearchReport.Thesis.SourceIDs) > 0 {
				ref := pi.EvidenceRef{ReportID: r.AnalysisID, SourceID: r.Analysis.ResearchReport.Thesis.SourceIDs[0]}
				a.Conditions = []AllocationCondition{{Kind: "exit", Text: "主营盈利持续恶化时退出", Verification: "核对下一期财务披露与原经营逻辑", Status: "pending", EvidenceRefs: []pi.EvidenceRef{ref}}}
				if n == 0 {
					a.Conditions = append(a.Conditions, AllocationCondition{Kind: "entry", Text: "估值与价格回到可接受区间", Verification: "核对当前行情与增长兑现", Status: "pending", EvidenceRefs: []pi.EvidenceRef{ref}})
				}
			}
		}
		newReview, err := pairedPrompt(j, p)
		if err != nil {
			t.Fatal("new conditions do not fit historical task", err)
		}
		t.Logf("plan=%d new_conditions_review_prompt_bytes=%d", i, len(newReview))
	}
}
