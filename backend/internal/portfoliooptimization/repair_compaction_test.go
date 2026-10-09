package portfoliooptimization

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRepairInvestmentCompactionIsLossless(t *testing.T) {
	p := fixtureProposal()
	data, _ := json.Marshal(p)
	compact, changed := compactRepairProposal(string(data))
	if !changed {
		t.Fatal("duplicated keys not compressed")
	}
	var root map[string]json.RawMessage
	_ = json.Unmarshal([]byte(compact), &root)
	var columns []string
	_ = json.Unmarshal(root["investment_columns"], &columns)
	var alternatives []map[string]json.RawMessage
	_ = json.Unmarshal(root["alternatives"], &alternatives)
	for _, alt := range alternatives {
		var rows []json.RawMessage
		_ = json.Unmarshal(alt["allocations"], &rows)
		allocations := []map[string]json.RawMessage{}
		for _, raw := range rows {
			a := restoreRepairColumns(t, raw, repairAllocationColumns)
			value := restoreRepairColumns(t, a["investment"], columns)
			a["investment"], _ = json.Marshal(value)
			allocations = append(allocations, a)
		}
		alt["allocations"], _ = json.Marshal(allocations)
	}
	root["alternatives"], _ = json.Marshal(alternatives)
	delete(root, "investment_columns")
	delete(root, "allocation_columns")
	restored, _ := json.Marshal(root)
	var got Proposal
	_ = json.Unmarshal(restored, &got)
	if !reflect.DeepEqual(p, got) {
		t.Fatal("compaction altered investment facts/weights/references")
	}
	for _, invalid := range []string{"not JSON", `{"alternatives":[{"allocations":[{"investment":{"role":"进攻","unknown":"keep"}}]}]}`, `{"investment_columns":[],"alternatives":[]}`} {
		got, changed := compactRepairProposal(invalid)
		if changed || got != invalid {
			t.Fatal("partial/unknown data changed")
		}
	}
}

// A read-only private regression for representative six-stock frozen inputs.
// No model call or credentials are needed to audit size and preservation.
func TestSavedQualityPromptBudgets(t *testing.T) {
	path := os.Getenv("EASY_STOCK_OPTIMIZATION_AUDIT")
	if path == "" {
		t.Skip("private saved-job audit is opt-in")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if json.Unmarshal(data, &j) != nil {
		t.Fatal("invalid saved job")
	}
	prompt, err := proposalPrompt(j)
	if err != nil {
		t.Fatal("initial", err)
	}
	t.Logf("initial prompt=%d", len(prompt))
	archiveRejectedRound(&j)
	j.RevisionCount = 1
	j.Proposal = nil
	j.Plans = nil
	revision, err := proposalPrompt(j)
	if err != nil {
		t.Fatal("revision", err)
	}
	t.Logf("revision prompt=%d", len(revision))
	if len(prompt) > MaxInitialModelPromptBytes || len(revision) > MaxRevisionModelPromptBytes {
		t.Fatal("stage input cap exceeded")
	}
	if path := os.Getenv("EASY_STOCK_OPTIMIZATION_RESPONSE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var r struct{ Result struct{ Content string } }
		if json.Unmarshal(data, &r) != nil {
			t.Fatal("invalid response")
		}
		repaired := modelRepairPrompt(prompt, r.Result.Content, json.Unmarshal([]byte("not json"), new(any)))
		if len(repaired) > MaxModelPromptBytes {
			t.Fatalf("repair too large: %d", len(repaired))
		}
		t.Logf("repair prompt=%d", len(repaired))
		_, originalFacts, _ := strings.Cut(prompt, "[资料JSON]\n")
		if !strings.Contains(repaired, "[资料JSON]\n"+originalFacts) {
			t.Fatal("repair changed frozen facts")
		}
	}
}

func restoreRepairColumns(t *testing.T, raw json.RawMessage, columns []string) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil {
		return object
	}
	var tuple []json.RawMessage
	if json.Unmarshal(raw, &tuple) != nil || len(tuple) != 2 {
		t.Fatal("invalid tuple")
	}
	var values []json.RawMessage
	var absent []int
	_ = json.Unmarshal(tuple[0], &values)
	_ = json.Unmarshal(tuple[1], &absent)
	missing := map[int]bool{}
	for _, i := range absent {
		missing[i] = true
	}
	object = map[string]json.RawMessage{}
	for i, key := range columns {
		if !missing[i] {
			object[key] = values[i]
		}
	}
	return object
}

func TestSavedProposalSyntaxRecovery(t *testing.T) {
	jobPath, outputPath := os.Getenv("EASY_STOCK_OPTIMIZATION_AUDIT"), os.Getenv("EASY_STOCK_OPTIMIZATION_RESPONSE")
	if jobPath == "" || outputPath == "" {
		t.Skip("private saved proposal recovery is opt-in")
	}
	data, err := os.ReadFile(jobPath)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	_ = json.Unmarshal(data, &j)
	data, err = os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var r struct{ Result struct{ Content string } }
	_ = json.Unmarshal(data, &r)
	corrected, changed := normalizeExtraAllocationBraces(r.Result.Content)
	if !changed {
		t.Skip("saved output does not contain this confirmed syntax defect")
	}
	var p Proposal
	if err := jsonContent(corrected, &p); err != nil {
		t.Fatal(err)
	}
	if err := validateProposal(j, p); err != nil {
		t.Logf("Syntax preserved; remaining content validation still rejects: %v", err)
		return
	}
	target, err := Solve(j, p.Alternatives[0])
	if err != nil {
		t.Fatal(err)
	}
	if !Check(j.Baseline, target).Valid {
		t.Fatal("constraint violation after syntax recovery")
	}
	t.Logf("recovered target=%v", target)
}

func TestCompactRepairResponseExpandsOnlyDeclaredLosslessColumns(t *testing.T) {
	p := fixtureProposal()
	data, _ := json.Marshal(p)
	compact, ok := compactRepairProposal(string(data))
	if !ok {
		t.Fatal("expected compact fixture")
	}
	named, ok := normalizeCompactProposal(compact)
	if !ok {
		t.Fatal("declared compact response not decoded")
	}
	var got Proposal
	_ = json.Unmarshal([]byte(named), &got)
	if !reflect.DeepEqual(p, got) {
		t.Fatal("response normalization changed content")
	}
	invalid := strings.Replace(compact, `"preferred_weight"`, `"invented_weight"`, 1)
	if unchanged, ok := normalizeCompactProposal(invalid); ok || unchanged != invalid {
		t.Fatal("unknown column mapping guessed")
	}
	// Presence/absence and explicit null survive a round trip, without inventing
	// an investment field that was missing from the original response.
	var root map[string]json.RawMessage
	_ = json.Unmarshal(data, &root)
	var alternatives []map[string]json.RawMessage
	_ = json.Unmarshal(root["alternatives"], &alternatives)
	var rows []map[string]json.RawMessage
	_ = json.Unmarshal(alternatives[0]["allocations"], &rows)
	var investment map[string]json.RawMessage
	_ = json.Unmarshal(rows[0]["investment"], &investment)
	delete(investment, "period_suitability")
	investment["prior_opinion"] = json.RawMessage("null")
	rows[0]["investment"], _ = json.Marshal(investment)
	alternatives[0]["allocations"], _ = json.Marshal(rows)
	root["alternatives"], _ = json.Marshal(alternatives)
	partial, _ := json.Marshal(root)
	compact, ok = compactRepairProposal(string(partial))
	if !ok {
		t.Fatal("partial known columns not compacted")
	}
	named, ok = normalizeCompactProposal(compact)
	if !ok {
		t.Fatal("partial columns not restored")
	}
	var decoded any
	var original any
	_ = json.Unmarshal([]byte(named), &decoded)
	_ = json.Unmarshal(partial, &original)
	if !reflect.DeepEqual(decoded, original) {
		t.Fatal("missing/null distinction lost")
	}
}
