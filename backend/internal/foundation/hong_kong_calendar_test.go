package foundation

import (
	"testing"
	"time"
)

func TestHongKongCashMarketSessions(t *testing.T) {
	for _, tc := range []struct {
		clock, last string
		open        bool
	}{
		{"2026-10-02 11:45", "2026-10-02", true},
		{"2026-10-02 12:00", "2026-10-02", false},
		{"2026-10-02 15:30", "2026-10-02", true},
		{"2026-10-02 16:00", "2026-10-02", false},
		{"2026-10-01 10:00", "2026-09-30", false},
		{"2026-12-24 11:59", "2026-12-24", true},
		{"2026-12-24 13:00", "2026-12-24", false},
		{"2026-12-26 10:00", "2026-12-24", false},
	} {
		now, err := time.ParseInLocation("2006-01-02 15:04", tc.clock, AStockLocation)
		if err != nil {
			t.Fatal(err)
		}
		if got := HongKongMarketOpen(now); got != tc.open {
			t.Errorf("%s open=%v", tc.clock, got)
		}
		if got := LatestSymbolSession("00700.HK", now); got != tc.last {
			t.Errorf("%s latest=%s", tc.clock, got)
		}
	}
}

func TestHongKongCompletedSession(t *testing.T) {
	for _, tc := range []struct {
		day, latest string
		hour        int
	}{
		{"2026-10-02", "2026-09-30", 15}, {"2026-10-02", "2026-10-02", 16}, {"2026-12-24", "2026-12-24", 12},
	} {
		day, _ := time.ParseInLocation("2006-01-02", tc.day, AStockLocation)
		now := day.Add(time.Duration(tc.hour) * time.Hour)
		if got := LatestCompletedSymbolSession("00700.HK", now).Format("2006-01-02"); got != tc.latest {
			t.Errorf("%v: got %s want %s", now, got, tc.latest)
		}
		if lag, valid := SymbolSessionLag("00700.HK", tc.latest, now); !valid || lag != 0 {
			t.Errorf("unexpected lag %d", lag)
		}
	}
}
