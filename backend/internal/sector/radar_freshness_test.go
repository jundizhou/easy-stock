package sector

import (
	"testing"
	"time"
)

func TestRadarSnapshotAgeCountsExchangeSessions(t *testing.T) {
	for _, tc := range []struct {
		name, date, asOf string
		age              int
	}{
		{"national day closure", "2026-09-30", "2026-10-06T13:00:00+08:00", 0},
		{"first session after holiday", "2026-09-30", "2026-10-08T10:00:00+08:00", 1},
		{"weekend after two sessions", "2026-09-30", "2026-10-11T12:00:00+08:00", 2},
		{"third session expires snapshot", "2026-09-30", "2026-10-12T10:00:00+08:00", 3},
		{"mid autumn closure", "2026-09-24", "2026-09-27T12:00:00+08:00", 0},
		{"Shanghai date from UTC clock", "2026-09-30", "2026-10-07T17:00:00Z", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.asOf)
			if err != nil {
				t.Fatal(err)
			}
			if age := tradingDayAge(tc.date, now); age != tc.age {
				t.Fatalf("snapshot %s at %s aged %d sessions, want %d", tc.date, tc.asOf, age, tc.age)
			}
		})
	}
}
