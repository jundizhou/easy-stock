package stockanalysis

import (
	"strings"
	"testing"
	"time"
)

func TestHolidayDoesNotRevokeResearchOrAnchoredPlan(t *testing.T) {
	_, snapshot := researchFixture(t)
	snapshot.CutoffAt, _ = time.Parse(time.RFC3339, "2026-10-05T19:47:25+08:00")
	snapshot.DailyBars[len(snapshot.DailyBars)-1].Date = "2026-09-30"
	result := validResearch()
	result.EvidenceLevel = "sufficient"
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if result.EvidenceLevel != "sufficient" || result.Decision.Status != "conditional" || result.Decision.PricePlan == nil || len(result.Decision.Blockers) != 0 {
		t.Fatalf("holiday rejected usable research: %+v", result)
	}
}

func TestStalePricesRevokeExecutionButPreserveScopedEvidence(t *testing.T) {
	_, snapshot := researchFixture(t)
	snapshot.CutoffAt, _ = time.Parse(time.RFC3339, "2026-10-20T18:00:00+08:00")
	snapshot.DailyBars[len(snapshot.DailyBars)-1].Date = "2026-09-30"
	result := validResearch()
	result.EvidenceLevel = "sufficient"
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if result.EvidenceLevel != "sufficient" || result.Decision.Status != "no_plan" || result.Decision.PricePlan != nil || !strings.Contains(result.Decision.Reason, "超过5个交易日") {
		t.Fatalf("evidence and execution coupled: %+v", result)
	}
}

func TestShortPriceSampleDoesNotInvalidateCompanyFacts(t *testing.T) {
	_, snapshot := researchFixture(t)
	snapshot.DailyBars = snapshot.DailyBars[len(snapshot.DailyBars)-10:]
	result := validResearch()
	result.Thesis.SourceIDs = []string{"f-business"}
	result.EvidenceLevel = "sufficient"
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if result.EvidenceLevel != "sufficient" || result.Decision.Status != "no_plan" || !strings.Contains(result.Decision.Reason, "不足20") {
		t.Fatalf("company evidence lost: %+v", result)
	}
}

func TestTradingLogicMatchingMarketEvidenceSurvivesNationalHoliday(t *testing.T) {
	cutoff, _ := time.Parse(time.RFC3339, "2026-10-07T18:00:00+08:00")
	source := ResearchSource{ID: "m-themes", Content: `[{"name":"端侧AI","trade_date":"2026-09-30","usable_for_current_move":true}]`}
	if !tradingLogicMarketUsable(source, "端侧AI", cutoff) {
		t.Fatal("holiday invalidated correctly dated topic")
	}
}
