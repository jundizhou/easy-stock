package portfolioinspection

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestScoringActionPriorityReportsExactFieldAndAllowedValues(t *testing.T) {
	for _, priority := range []string{"持有观察", "优先复核", "", "  "} {
		t.Run(priority, func(t *testing.T) {
			req, results, metrics, base, at := etfScoringFixture(.2, 50)
			base = fixtureContractReport(base, buildScoringCatalog(req, results, metrics, at))
			for i := range base.Holdings {
				base.Holdings[i].ActionPriority = priority
			}
			raw, _ := json.Marshal(base)
			report, issues := validateScoringOutput(string(raw), req, results, metrics, at)
			if report.ScoreAvailable || report.TotalScore != nil || len(issues) != len(base.Holdings) {
				t.Fatalf("invalid priority published or obscured by unrelated errors: %+v", issues)
			}
			for i, issue := range issues {
				if issue.Code != "action" || issue.Path != fmt.Sprintf("holdings[%d].action_priority", i) || !reflect.DeepEqual(issue.AllowedValues, []string{"观察", "保持", "优先处理"}) {
					t.Fatalf("priority failure lacks actionable diagnostics: %+v", issue)
				}
				for _, text := range []string{"动作优先级", fmt.Sprintf("%q", priority), "观察", "保持", "优先处理"} {
					if !strings.Contains(issue.Message, text) {
						t.Fatalf("user-facing diagnostic omitted %q: %s", text, issue.Message)
					}
				}
			}
		})
	}
}

// Replay the observed failure vocabulary through automatic repair and resume. The
// model must receive exact enum diagnostics and explicitly choose a new value.
func TestScoringActionPriorityRepairAndResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprintf("resume_%v", resume), func(t *testing.T) {
			req, results, metrics, base, at := etfScoringFixture(.2, 50)
			rules, _ := RulesFor(req.TraderProfile)
			base = fixtureContractReport(base, buildScoringCatalog(req, results, metrics, at))
			base.Holdings[0].ActionPriority = "持有观察"
			base.Holdings[1].ActionPriority = "优先复核"
			failed, _ := json.Marshal(base)
			// These are explicit choices by the replayed model, not aliases
			// applied by production code.
			base.Holdings[0].ActionPriority = "保持"
			base.Holdings[1].ActionPriority = "优先处理"
			fixed, _ := json.Marshal(base)
			g := &scoringReplayGateway{responses: []json.RawMessage{failed, fixed}}
			var seed *scoringAttempt
			if resume {
				seed = &scoringAttempt{PromptVersion: PromptVersion, ContractVersion: scoringContractVersion, Response: string(failed)}
				g.responses = []json.RawMessage{fixed}
			}
			s := NewService(nil, g, nil, nil)
			defer s.Close()
			report, err := s.scorePortfolioWithAudit(context.Background(), req, results, metrics, rules, at, seed, nil)
			if err != nil || !report.ScoreAvailable || int(g.calls.Load()) != len(g.responses) {
				t.Fatal("priority repair failed", err, g.calls.Load())
			}
			prompt := g.prompts[len(g.prompts)-1]
			for _, text := range []string{"holdings[0].action_priority", "holdings[1].action_priority", `"allowed_values":["观察","保持","优先处理"]`} {
				if !strings.Contains(prompt, text) {
					t.Fatal("repair did not receive precise priority diagnostics", text)
				}
			}
			for i, h := range report.Holdings {
				if h.ActionPriority != base.Holdings[i].ActionPriority || h.ActionID != base.Holdings[i].ActionID || h.Action == "" || h.Confirmation == "" || h.Invalidation == "" {
					t.Fatal("repair changed the selected action or lost generated conditions", h)
				}
			}
		})
	}
}

func TestScoringActionPriorityPromptContract(t *testing.T) {
	req, results, metrics, _ := scoreFixture()
	rules, _ := RulesFor(req.TraderProfile)
	prompt, err := buildPrompt(req, results, metrics, rules)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "action_priority只能为观察/保持/优先处理") {
		t.Fatal("initial prompt does not enumerate the supported priorities")
	}
}
