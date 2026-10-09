package stockanalysis

import (
	"encoding/json"
	"testing"
)

func TestOptionalEmptyPlanDoesNotInvalidateInvestmentJudgment(t *testing.T) {
	_, snapshot := researchFixture(t)
	r := validResearch()
	r.Decision.PricePlan = &AnchoredPricePlan{}
	if _, err := validateResearch(&r, snapshot); err != nil {
		t.Fatal(err)
	}
	if r.Decision.Status != "conditional" || r.Decision.PricePlan != nil {
		t.Fatal("empty optional plan blocked research", r.Decision)
	}
	r = validResearch()
	r.Decision.PricePlan = &AnchoredPricePlan{EntryAnchor: "invented"}
	if _, err := validateResearch(&r, snapshot); err != nil {
		t.Fatal(err)
	}
	if r.Decision.Status != "no_plan" || len(r.Decision.Blockers) == 0 {
		t.Fatal("nonempty invalid plan escaped validation")
	}
}

func TestV8ReuseRepairsEmptyOptionalPlanWithoutRenewingHistory(t *testing.T) {
	j := legacyHolidayJob(t)
	j.Analysis.ResearchReport.PromptVersion = "stock-research-v8"
	j.Analysis.ResearchReport.ValidationVersion = "stock-research-validation-v2"
	j.Checkpoint.PromptVersion = "stock-research-v8"
	r := validResearch()
	r.EvidenceLevel = "sufficient"
	r.Decision.PricePlan = &AnchoredPricePlan{}
	b, _ := json.Marshal(r)
	j.Checkpoint.Outputs["核心判断"] = ResearchStageOutput{Value: b}
	j.Checkpoint.Outputs["交易条件"] = ResearchStageOutput{Value: b}
	before, _ := json.Marshal(j)
	got := RevalidateReusableResearch(j)
	if got.Analysis.ResearchReport.ValidationVersion != ResearchValidationVersion || got.Analysis.ResearchReport.Decision.Status != "conditional" || got.Analysis.ResearchReport.Decision.PricePlan != nil {
		t.Fatal("empty optional plan not revalidated", got.Analysis.ResearchReport.Decision)
	}
	if !ResearchCompletedAt(got).Equal(ResearchCompletedAt(j)) {
		t.Fatal("24h reuse window renewed")
	}
	after, _ := json.Marshal(j)
	if string(before) != string(after) {
		t.Fatal("historical report overwritten")
	}
	j.Checkpoint.PromptVersion = "different"
	if RevalidateReusableResearch(j).Analysis.ResearchReport.ValidationVersion == ResearchValidationVersion {
		t.Fatal("mismatched outputs replayed")
	}
}
