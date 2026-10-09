package portfoliooptimization

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	pi "easy-stock/backend/internal/portfolioinspection"
)

// Uses private frozen research only. There is no gateway or network call and no
// reuse of historical scores under the new scoring policy.
func TestSavedScoringPromptFitsBudget(t *testing.T) {
	path := os.Getenv("EASY_STOCK_SCORING_PROMPT_AUDIT")
	if path == "" {
		t.Skip("private frozen research is opt-in")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	if j.Proposal == nil || len(j.Plans) == 0 || len(j.Results) == 0 {
		t.Fatal("frozen investment proposal and targets required")
	}
	for i, p := range j.Plans {
		if len(p.Target) == 0 {
			continue
		}
		p.Original = pi.OptimizationReport(j.Source.Request, j.Results)
		req := j.Source.Request
		req.Holdings = p.Target
		p.Proposed = pi.OptimizationReport(req, j.Results)
		prompt, err := pairedPrompt(j, p)
		if err != nil {
			t.Fatal(err)
		}
		if len(prompt) > MaxReviewModelPromptBytes {
			t.Fatal("scoring policy exceeded existing input budget")
		}
		t.Logf("frozen plan %d: %d bytes / %d; no model call or score reused", i+1, len(prompt), MaxReviewModelPromptBytes)
	}
}

func TestPairedScoringPayloadIgnoresTotalExposureAndPreservesTradeFacts(t *testing.T) {
	j := fixtureJob()
	p := fixtureProposal()
	p.RiskGroups = []pi.RiskGroup{{Name: "测试驱动", Symbols: []string{"600519.SH"}, Reason: "相同驱动"}}
	j.Proposal = &p
	var prior string
	for _, scale := range []int{1, 2, 5} {
		req := j.Source.Request
		req.Holdings = holds(12*scale, 8*scale)
		before := pi.OptimizationReport(req, j.Results)
		req.Holdings = holds(10*scale, 10*scale)
		after := pi.OptimizationReport(req, j.Results)
		prompt, err := pairedPrompt(j, Plan{Original: before, Proposed: after})
		if err != nil {
			t.Fatal(err)
		}
		if prior != "" && prior != prompt {
			t.Fatal("total account exposure changed independent review input")
		}
		prior = prompt
		if !strings.Contains(prompt, pi.ScoringPolicy) {
			t.Fatal("inspection and optimization scoring policies diverged")
		}
		_, payload, _ := strings.Cut(prompt, "[资料JSON]\n")
		for _, key := range []string{`"cash_percent"`, `"total_position_percent"`, `"minimum_cash_percent"`, `"weight_percent"`} {
			if strings.Contains(payload, key) {
				t.Fatal("account exposure leaked", key)
			}
		}
		var data struct {
			A struct {
				Weights   map[string]float64
				Exposures map[string]float64 `json:"equity_risk_exposures"`
			}
		}
		if err := json.Unmarshal([]byte(payload), &data); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(data.A.Weights, map[string]float64{"600519.SH": 60, "000858.SZ": 40}) || data.A.Exposures["测试驱动"] != 60 {
			t.Fatal("wrong stock-relative weights", data)
		}
		before.Conclusion.RiskGroups = p.RiskGroups
		facts := pi.OptimizationComparisonFacts(before)
		if facts["equity_risk_exposures.测试驱动"].Value != float64(60) || before.Facts["cash_percent"].Value != 100-20*scale {
			t.Fatal("scoring/trading fact identities confused", facts)
		}
	}
}
