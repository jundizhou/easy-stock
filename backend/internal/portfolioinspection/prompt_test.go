package portfolioinspection

import (
	"strings"
	"testing"
)

func TestBuildPromptPinsDeterministicV2Scores(t *testing.T) {
	rules, _ := RulesFor(ProfileBalanced)
	metrics := Metrics{
		TotalPositionPercent: 70, CashPercent: 30, CoveragePercent: 100,
		HealthScore: 78, HealthScoreAvailable: true, WeightedRisk: 35,
		RiskResilienceScore: 100, DiversificationScore: 72, StyleMatchScore: 100,
	}
	prompt, err := buildPrompt(Request{TraderProfile: ProfileBalanced}, nil, metrics, rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"prompt_version":"portfolio-inspection-v2"`,
		`"deterministic_summary":{"health_score":78,"risk_level":"低","style_match":"匹配"}`,
		"必须原样复制deterministic_summary",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q: %s", expected, prompt)
		}
	}
}
