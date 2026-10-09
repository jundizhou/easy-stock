package foundation

import (
	"testing"
	"time"
)

func TestAStockTradingCalendar(t *testing.T) {
	for _, item := range []struct {
		date string
		open bool
	}{
		{"2026-09-18", true}, {"2026-09-19", false}, {"2026-09-20", false},
		{"2026-09-25", false}, {"2026-10-01", false}, {"2026-10-08", true},
	} {
		date, _ := time.ParseInLocation("2006-01-02", item.date, time.FixedZone("Asia/Shanghai", 8*60*60))
		if got := IsAStockTradingDay(date); got != item.open {
			t.Errorf("%s: got %v", item.date, got)
		}
	}
}

func TestMarketFreshnessCountsCompletedSessionsInsteadOfHolidayDays(t *testing.T) {
	for _, tc := range []struct {
		asOf, date string
		lag        int
		valid      bool
	}{
		{"2026-10-05T19:47:25+08:00", "2026-09-30", 0, true},
		{"2026-10-08T14:30:00+08:00", "2026-09-30", 0, true},
		{"2026-10-08T15:01:00+08:00", "2026-09-30", 1, true},
		{"2026-10-11T12:00:00+08:00", "2026-09-30", 2, true},
		{"2026-10-19T16:00:00+08:00", "2026-09-30", 6, true},
		{"2026-10-05T12:00:00+08:00", "2026-10-06", 0, false},
		{"2026-10-05T12:00:00+08:00", "invalid", 0, false},
	} {
		asOf, _ := time.Parse(time.RFC3339, tc.asOf)
		lag, valid := AStockSessionLag(tc.date, asOf)
		if lag != tc.lag || valid != tc.valid {
			t.Fatalf("%+v got %d %v", tc, lag, valid)
		}
	}
}
