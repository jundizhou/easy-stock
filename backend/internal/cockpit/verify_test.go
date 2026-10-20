package cockpit

import (
	"reflect"
	"testing"
)

func TestVerifyNarrativeAcceptsOnlyNumbersPresentInFacts(t *testing.T) {
	facts := Facts{
		CNIndexes: []IndexQuote{{Name: "上证指数", ChangePercent: -1.3, Price: 3730.65}},
		Emotion:   &Emotion{Score: 44.5, LimitUp: 46},
	}
	narrative := "上证指数下跌 1.3%（A股指数），情绪分 44.5（情绪周期）。另有 9.9% 的虚构涨幅。"
	got := VerifyNarrative(narrative, facts)
	if !reflect.DeepEqual(got, []string{"9.9%"}) {
		t.Fatalf("unverified = %v, want only 9.9%%", got)
	}
}

func TestVerifyNarrativeIgnoresIntegersAndEmptyText(t *testing.T) {
	facts := Facts{}
	if got := VerifyNarrative("共 3 条预案，第 2 步", facts); len(got) != 0 {
		t.Fatalf("integers should not be checked, got %v", got)
	}
	if got := VerifyNarrative("  ", facts); got != nil {
		t.Fatalf("empty narrative returned %v", got)
	}
}

func TestBriefWithUnverifiedNumberIsFlaggedNotHidden(t *testing.T) {
	narrator := &fakeNarrator{available: true, text: "情绪分 88.8 为本次结论依据（情绪周期）"}
	service, store := newTestService(t, narrator, at(20, 15, 40))
	if _, err := service.RunManual(PhasePostClose); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return store.List("", 0)[0].Status == "ready" })
	brief := store.List("", 0)[0]
	if brief.NarrativeStatus != "ready" || !reflect.DeepEqual(brief.UnverifiedNumbers, []string{"88.8"}) {
		t.Fatalf("flagging failed: %+v", brief)
	}
}
