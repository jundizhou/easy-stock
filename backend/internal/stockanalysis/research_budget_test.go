package stockanalysis

import (
	"testing"
	"time"
)

func TestResearchBudgetFitsConfiguredWaitAndReasoning(t *testing.T) {
	for _, level := range []ResearchLevel{ResearchLevelQuick, ResearchLevelStandard, ResearchLevelDeep} {
		for _, effort := range []string{"none", "low", "default", "medium", "enabled", "high", "xhigh", "max"} {
			for _, wait := range []time.Duration{30 * time.Second, 300 * time.Second, 900 * time.Second, time.Hour} {
				budget := ResearchBudgetFor(ResearchRequest{AnalysisLevel: level}, wait, effort)
				if budget.StageTimeout() < wait+time.Minute || budget.ResponseTimeout() != wait {
					t.Fatalf("stage cuts off the configured wait or answer: level=%s effort=%s budget=%+v", level, effort, budget)
				}
				if effort == "max" && budget.StageTimeout() < 15*time.Minute {
					t.Fatal("maximum reasoning still inherits a short stage deadline")
				}
			}
		}
	}
}

func TestResearchBudgetReservesRepairCollectionAndSupplement(t *testing.T) {
	for _, item := range []struct {
		level ResearchLevel
		calls int
		extra time.Duration
	}{{ResearchLevelQuick, 1, 0}, {ResearchLevelStandard, 3, 0}, {ResearchLevelDeep, 4, time.Minute}} {
		budget := ResearchBudgetFor(ResearchRequest{AnalysisLevel: item.level}, 300*time.Second, "max")
		want := ResearchCollectionTimeout + time.Duration(item.calls)*budget.StageTimeout() + item.extra
		if budget.TotalTimeout() != want {
			t.Fatalf("%s loses a possible model stage: total=%s want=%s", item.level, budget.TotalTimeout(), want)
		}
	}
	budget := ResearchBudgetFor(ResearchRequest{AnalysisLevel: ResearchLevelQuantitative}, time.Hour, "max")
	if budget.TotalTimeout() != ResearchCollectionTimeout || budget.StageTimeout() != 0 || budget.ResponseTimeout() != 0 {
		t.Fatal("quantitative-only work reserved model time")
	}
}
