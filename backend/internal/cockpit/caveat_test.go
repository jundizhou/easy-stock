package cockpit

import (
	"strings"
	"testing"
)

func TestManualPostCloseBeforeCloseCarriesCaveat(t *testing.T) {
	narrator := &fakeNarrator{available: true, text: "快照结论"}
	service, store := newTestService(t, narrator, at(20, 9, 31))
	if _, err := service.RunManual(PhasePostClose); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return store.List("", 0)[0].Status == "ready" })
	brief := store.List("", 0)[0]
	if !strings.Contains(brief.Caveat, "09:31") || !strings.Contains(brief.Caveat, "快照") {
		t.Fatalf("caveat missing: %+v", brief)
	}
	if !strings.Contains(narrator.lastPrompt(), "数据时效说明") {
		t.Fatal("caveat must be passed to the model")
	}
}

func TestManualRunInMatchingPhaseHasNoCaveat(t *testing.T) {
	service, store := newTestService(t, &fakeNarrator{available: false}, at(20, 15, 40))
	if _, err := service.RunManual(PhasePostClose); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return store.List("", 0)[0].Status == "ready" })
	if store.List("", 0)[0].Caveat != "" {
		t.Fatal("no caveat expected after the close")
	}
}
