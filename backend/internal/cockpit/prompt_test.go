package cockpit

import (
	"strings"
	"testing"
)

func TestBuildPromptRoundsLongFloatsButKeepsStoredFacts(t *testing.T) {
	facts := Facts{
		Emotion: &Emotion{Score: 43.812345678, FinalBreakRate: 0.31343283582089554},
		Sources: []SourceResult{{Name: "情绪周期", OK: true}},
	}
	prompt := BuildPrompt(PhasePostClose, "2026-10-20", "盘后复盘", facts, nil)
	if strings.Contains(prompt, "0.31343283582089554") || strings.Contains(prompt, "43.812345678") {
		t.Fatalf("long floats leaked into prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "0.3134") || !strings.Contains(prompt, "43.8123") {
		t.Fatalf("rounded values missing from prompt: %s", prompt)
	}
	if facts.Emotion.FinalBreakRate != 0.31343283582089554 {
		t.Fatal("rounding must not mutate the stored facts")
	}
}
