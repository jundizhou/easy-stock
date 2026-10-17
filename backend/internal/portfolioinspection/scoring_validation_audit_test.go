package portfolioinspection

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"easy-stock/backend/internal/agent"
)

type scoringReplayGateway struct {
	scoreGateway
	responses []json.RawMessage
	prompts   []string
}

// Migration audit keeps the original outputs unchanged and deliberately
// rejects invalid references. Only the test copy selects the new action IDs.
func TestSavedScoringContractAudit(t *testing.T) {
	path := os.Getenv("EASY_STOCK_SCORING_VALIDATION_AUDIT")
	if path == "" {
		t.Skip("set private audit bundle")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Job       Job               `json:"job"`
		Responses []json.RawMessage `json:"responses"`
	}
	if err := json.Unmarshal(data, &audit); err != nil {
		t.Fatal(err)
	}
	j := audit.Job
	before, _ := json.Marshal(j)
	rules, _ := RulesFor(j.Request.TraderProfile)
	metrics := metricsForReport(j.Request, j.Results, rules)
	catalog := buildScoringCatalog(j.Request, j.Results, metrics, j.AggregationStartedAt)
	accepted := 0
	for i, raw := range audit.Responses {
		r, err := decodeScoringReport(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		originalErr := validateScoringReportFactsAt(&r, j.Request, j.Results, metrics, true, nil, j.AggregationStartedAt)
		if originalErr != nil {
			t.Logf("original %d remains rejected: %s", i+1, originalErr)
		}
		// Decode again: the successful legacy validator computes derived scores.
		r, err = decodeScoringReport(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		r = fixtureContractReport(r, catalog)
		for _, a := range catalog.Actions {
			if a.Kind != "conditional_reduce" {
				continue
			}
			for k := range r.Holdings {
				if r.Holdings[k].Symbol == a.Symbol {
					r.Holdings[k].ActionID = a.ID
				}
			}
			r.AdjustmentActionIDs = []string{a.ID}
			if len(r.Scenarios) > 0 {
				r.Scenarios[len(r.Scenarios)-1].ActionIDs = []string{a.ID}
			}
		}
		err = validateScoringReportFactsAt(&r, j.Request, j.Results, metrics, true, nil, j.AggregationStartedAt)
		if err != nil {
			if originalErr == nil || !strings.Contains(err.Error(), "[evidence]") {
				t.Fatal("unexpected contract failure", err)
			}
			t.Logf("contract copy %d still rejects wrong references", i+1)
			continue
		}
		accepted++
		t.Logf("contract copy %d: score=%d risk=%s actions=%d", i+1, *r.TotalScore, r.RiskLevel, len(r.Actions))
	}
	if accepted == 0 {
		t.Fatal("no evidence-valid response passed new contract")
	}
	after, _ := json.Marshal(j)
	if string(before) != string(after) {
		t.Fatal("audit modified frozen inputs")
	}
}

func (g *scoringReplayGateway) Prompt(_ context.Context, prompt string) (agent.PromptResult, error) {
	i := int(g.calls.Add(1)) - 1
	g.prompts = append(g.prompts, prompt)
	if i >= len(g.responses) {
		return agent.PromptResult{}, fmt.Errorf("unexpected replay call %d", i+1)
	}
	return agent.PromptResult{Content: fixtureRepairResponse(string(g.responses[i]), prompt)}, nil
}

// Optional offline replay of private frozen inputs and saved model JSON. This
// never opens the live database or calls a model/data provider.
func TestSavedScoringValidationAudit(t *testing.T) {
	path := os.Getenv("EASY_STOCK_SCORING_VALIDATION_AUDIT")
	if path == "" {
		t.Skip("set private frozen job/responses bundle for offline validation audit")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Job             Job                 `json:"job"`
		Responses       []json.RawMessage   `json:"responses"`
		RepairSequences [][]json.RawMessage `json:"repair_sequences"`
	}
	if err := json.Unmarshal(data, &audit); err != nil {
		t.Fatal(err)
	}
	j := audit.Job
	if len(j.Results) == 0 || j.AggregationStartedAt.IsZero() || len(audit.Responses)+len(audit.RepairSequences) == 0 {
		t.Fatal("missing frozen research/evaluation time or saved responses")
	}
	before, _ := json.Marshal(j)
	rules, _ := RulesFor(j.Request.TraderProfile)
	metrics := metricsForReport(j.Request, j.Results, rules)
	run := func(name string, responses []json.RawMessage) {
		t.Run(name, func(t *testing.T) {
			if len(responses) == 0 || len(responses) > aggregationModelCalls {
				t.Fatal("invalid replay sequence length")
			}
			var report AIReport
			var err error
			calls := 0
			for _, raw := range responses {
				calls++
				report, err = decodeScoringReport(string(raw))
				if err == nil {
					err = validateScoringReportFactsAt(&report, j.Request, j.Results, metrics, true, nil, j.AggregationStartedAt)
				}
				if err == nil {
					break
				}
			}
			if err != nil || !report.ScoreAvailable || calls != len(responses) {
				t.Fatal("saved output failed or unexpected repair count", err, calls)
			}
			t.Logf("saved output validated in %d offline calls: score=%d risk=%s", calls, *report.TotalScore, report.RiskLevel)
		})
	}
	for i, raw := range audit.Responses {
		run("response/"+strconv.Itoa(i+1), []json.RawMessage{raw})
	}
	for i, responses := range audit.RepairSequences {
		run("repair/"+strconv.Itoa(i+1), responses)
	}
	after, _ := json.Marshal(j)
	if string(before) != string(after) {
		t.Fatal("validation mutated frozen inspection/research inputs")
	}
}
