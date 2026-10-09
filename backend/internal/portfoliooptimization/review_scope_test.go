package portfoliooptimization

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"easy-stock/backend/internal/agent"
	pi "easy-stock/backend/internal/portfolioinspection"
)

func scopeFixture(t *testing.T) (Job, pi.Report, pi.Report) {
	j, _ := searchFixture(t)
	original := j.Source.Request
	original.Holdings = original.Holdings[:2]
	a := pi.OptimizationReport(original, j.Results)
	target := original
	target.Holdings = append(append([]pi.Holding(nil), original.Holdings...), j.Results[2].Holding)
	b := pi.OptimizationReport(target, j.Results)
	return j, a, b
}

func TestReviewScopesDiagnoseEveryWrongSideReferenceWithoutRewriting(t *testing.T) {
	j, old, target := scopeFixture(t)
	newStock := j.Results[2]
	refs := []pi.EvidenceRef{{Fact: newStock.Holding.Symbol + ".financial.net_profit"}, {Fact: newStock.Holding.Symbol + ".financial.deducted_net_profit"}, {ReportID: newStock.AnalysisID, SourceID: "s1"}}
	bad, _ := json.Marshal(map[string]any{"dimensions": []any{map[string]any{"key": "holding_logic", "score": 55, "evidence_refs": refs}}})
	before := string(bad)
	good, _ := json.Marshal(fixtureScore(nil))
	for _, reversed := range []bool{false, true} {
		a, b, ra, rb := bad, good, old, target
		side := "a"
		if reversed {
			a, b, ra, rb = good, bad, target, old
			side = "b"
		}
		err := checkReviewScopes(a, b, ra, rb)
		var scope *reviewScopeError
		if !errors.As(err, &scope) || len(scope.Issues) != 3 {
			t.Fatal("one-at-a-time error repair", err)
		}
		for _, issue := range scope.Issues {
			if issue.Side != side || !strings.Contains(issue.Path, "evidence_refs") {
				t.Fatal(issue)
			}
		}
		if !reflect.DeepEqual(scope.Holdings[side], []string{old.Request.Holdings[0].Symbol, old.Request.Holdings[1].Symbol}) {
			t.Fatal(scope)
		}
		repair := modelRepairPrompt("独立组合复评员\n[资料JSON]\n{}", string(bad), err)
		if !strings.Contains(repair, "不能替换成同一只股票的另一字段") || !strings.Contains(repair, "actual_holdings") || !strings.Contains(repair, "不得仅换同股字段") {
			t.Fatal(repair)
		}
	}
	if string(bad) != before {
		t.Fatal("checker silently changed scores/evidence")
	}
	if err := checkReviewScopes(good, bad, old, target); err != nil {
		t.Fatal("valid new-stock facts rejected in target", err)
	}
	// Unknown facts remain for the normal validator, never reclassified as valid.
	unknown := json.RawMessage(`{"evidence_refs":[{"fact":"invented.fact"}]}`)
	if err := checkReviewScopes(unknown, good, old, target); err != nil {
		t.Fatal(err)
	}
	if err := checkRefs(j, []pi.EvidenceRef{{Fact: "invented.fact"}}); err == nil {
		t.Fatal("invented fact admitted")
	}
}

func TestReviewPromptMarksOwnershipInBothBlindOrders(t *testing.T) {
	j, old, target := scopeFixture(t)
	j.Proposal = &Proposal{}
	for _, order := range []string{"original_first", "target_first"} {
		prompt, err := pairedPrompt(j, Plan{Original: old, Proposed: target, AssessmentOrder: order})
		if err != nil {
			t.Fatal(err)
		}
		_, raw, _ := strings.Cut(prompt, "[资料JSON]\n")
		var data struct {
			Columns []string `json:"stock_columns"`
			Stocks  [][]any  `json:"stocks"`
		}
		if json.Unmarshal([]byte(raw), &data) != nil {
			t.Fatal("bad payload")
		}
		column := -1
		for i, k := range data.Columns {
			if k == "score_scope" {
				column = i
			}
		}
		if column < 0 {
			t.Fatal("missing ownership")
		}
		for _, r := range data.Stocks {
			want := "ab"
			if r[0] == j.Results[2].Holding.Symbol {
				want = "b"
				if order == "target_first" {
					want = "a"
				}
			}
			if r[column] != want {
				t.Fatal(order, r, want)
			}
		}
	}
}

type scopeRepairGateway struct {
	*testGateway
	first, repaired string
	repeated        bool
	repairPrompt    string
}

func (g *scopeRepairGateway) Prompt(_ context.Context, prompt string) (agent.PromptResult, error) {
	n := g.calls.Add(1)
	if n == 1 || g.repeated {
		return agent.PromptResult{Content: g.first}, nil
	}
	g.repairPrompt = prompt
	return agent.PromptResult{Content: g.repaired}, nil
}
func TestScopeRepairStopsAfterRepeatedFailure(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		j, old, target := scopeFixture(t)
		j.ID = "scope-test"
		j.Stage = "assessing"
		good := fixtureScore(nil)
		bad := fixtureScore(nil)
		bad.Dimensions[0].EvidenceRefs = []pi.EvidenceRef{{Fact: j.Results[2].Holding.Symbol + ".financial.net_profit"}}
		first, _ := json.Marshal(map[string]any{"a": good, "b": bad})
		fixed, _ := json.Marshal(map[string]any{"a": good, "b": good})
		g := &scopeRepairGateway{testGateway: &testGateway{}, first: string(first), repaired: string(fixed), repeated: repeated}
		store, err := pi.OpenStore("")
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		s := NewService(store, g, Dependencies{})
		defer s.Close()
		err = s.model(context.Background(), &j, "独立组合复评员\n[资料JSON]\n{}", func(raw string) error {
			var v struct{ A, B json.RawMessage }
			if err := json.Unmarshal([]byte(raw), &v); err != nil {
				return err
			}
			return checkReviewScopes(v.A, v.B, target, old)
		})
		expected := int32(2)
		if repeated {
			expected = 3
		}
		if (err != nil) != repeated || g.calls.Load() != expected || !jobRepairUsed(&j) {
			t.Fatal("wrong retry budget", g.calls.Load(), err)
		}
		if !repeated && !strings.Contains(g.repairPrompt, "actual_holdings") {
			t.Fatal("repair lost ownership diagnosis")
		}
	}
}
