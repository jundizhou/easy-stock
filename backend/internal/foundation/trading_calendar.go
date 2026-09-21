package foundation

import "time"

// A-share exchanges are closed on weekends and the statutory holiday ranges
// below. The list is kept locally so summary generation remains deterministic
// and does not depend on a third-party calendar endpoint. Add each year's
// exchange holiday announcement here when it is published.
var aStockHolidayRanges = [][2]string{
	{"2025-01-01", "2025-01-01"},
	{"2025-01-28", "2025-02-04"},
	{"2025-04-04", "2025-04-06"},
	{"2025-05-01", "2025-05-05"},
	{"2025-05-31", "2025-06-02"},
	{"2025-10-01", "2025-10-08"},
	{"2026-01-01", "2026-01-03"},
	{"2026-02-15", "2026-02-23"},
	{"2026-04-04", "2026-04-06"},
	{"2026-05-01", "2026-05-05"},
	{"2026-06-19", "2026-06-21"},
	{"2026-09-25", "2026-09-27"},
	{"2026-10-01", "2026-10-07"},
}

func IsAStockTradingDay(value time.Time) bool {
	day := value.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	date := day.Format("2006-01-02")
	for _, holiday := range aStockHolidayRanges {
		if date >= holiday[0] && date <= holiday[1] {
			return false
		}
	}
	// Mainland exchanges stay closed on weekends even when an adjusted public
	// holiday designates that weekend as a working day.
	if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		return false
	}
	return true
}
