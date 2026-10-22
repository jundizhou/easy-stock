package httpapi

import (
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestThemeLibrarySlotsSkipWeekends(t *testing.T) {
	friEvening := time.Date(2026, 10, 23, 21, 0, 0, 0, foundation.AStockLocation)
	want := time.Date(2026, 10, 26, 20, 0, 0, 0, foundation.AStockLocation)
	monMorning := time.Date(2026, 10, 19, 10, 0, 0, 0, foundation.AStockLocation)
	wantLast := time.Date(2026, 10, 16, 20, 0, 0, 0, foundation.AStockLocation)
	for _, location := range []*time.Location{foundation.AStockLocation, time.UTC, time.FixedZone("UTC-7", -7*60*60)} {
		t.Run(location.String(), func(t *testing.T) {
			if next := nextThemeLibrarySlot(friEvening.In(location), 20, 0); !next.Equal(want) {
				t.Fatalf("next slot after Friday 21:00 = %s, want %s", next, want)
			}
			if last := lastThemeLibrarySlot(monMorning.In(location), 20, 0); !last.Equal(wantLast) {
				t.Fatalf("last slot before Monday 10:00 = %s, want %s", last, wantLast)
			}
		})
	}
}

func TestThemeLibraryRefreshCatchUp(t *testing.T) {
	slot := time.Date(2026, 10, 16, 20, 0, 0, 0, foundation.AStockLocation)
	afterSlot := slot.Add(30 * time.Minute).UTC().Format(time.RFC3339)
	if !themeLibraryRefreshedSince(afterSlot, slot) {
		t.Fatalf("refresh after the slot should satisfy it")
	}
	beforeSlot := slot.Add(-72 * time.Hour).UTC().Format(time.RFC3339)
	if themeLibraryRefreshedSince(beforeSlot, slot) {
		t.Fatalf("a snapshot older than the slot must trigger catch-up")
	}
}
