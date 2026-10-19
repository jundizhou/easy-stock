package httpapi

import (
	"testing"
	"time"
)

func TestThemeLibrarySlotsSkipWeekends(t *testing.T) {
	friEvening := time.Date(2026, 10, 23, 21, 0, 0, 0, time.Local)
	next := nextThemeLibrarySlot(friEvening, 20, 0)
	want := time.Date(2026, 10, 26, 20, 0, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("next slot after Friday 21:00 = %s, want %s", next, want)
	}

	monMorning := time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local)
	last := lastThemeLibrarySlot(monMorning, 20, 0)
	wantLast := time.Date(2026, 10, 16, 20, 0, 0, 0, time.Local)
	if !last.Equal(wantLast) {
		t.Fatalf("last slot before Monday 10:00 = %s, want %s", last, wantLast)
	}
}

func TestThemeLibraryRefreshCatchUp(t *testing.T) {
	slot := time.Date(2026, 10, 16, 20, 0, 0, 0, time.Local)
	afterSlot := slot.Add(30 * time.Minute).UTC().Format(time.RFC3339)
	if !themeLibraryRefreshedSince(afterSlot, slot) {
		t.Fatalf("refresh after the slot should satisfy it")
	}
	beforeSlot := slot.Add(-72 * time.Hour).UTC().Format(time.RFC3339)
	if themeLibraryRefreshedSince(beforeSlot, slot) {
		t.Fatalf("a snapshot older than the slot must trigger catch-up")
	}
}
