package portfolioinspection

import (
	"strings"
	"testing"
)

func TestBuildPromptUsesAIResearchRubric(t *testing.T) {
	req, results, metrics, _ := scoreFixture()
	rules, _ := RulesFor(req.TraderProfile)
	prompt, err := buildPrompt(req, results, metrics, rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"prompt_version":"portfolio-inspection-v4"`, `"scoring_version":"portfolio-ai-score-v4"`, "holding_logic", "不得调用工具", "没有静态止损价不阻断评分"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("missing %s", expected)
		}
	}
	if strings.Contains(prompt, "deterministic_summary") || strings.Contains(prompt, "必须原样复制") {
		t.Fatal("AI scoring still overridden")
	}
}
