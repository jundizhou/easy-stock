package foundation

import "time"

var AStockLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

// LatestCompletedAStockSession excludes today's incomplete daily bar.
func LatestCompletedAStockSession(value time.Time) time.Time {
	day := value.In(AStockLocation)
	if day.Hour() < 15 {
		day = day.AddDate(0, 0, -1)
	}
	for !IsAStockTradingDay(day) {
		day = day.AddDate(0, 0, -1)
	}
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, AStockLocation)
}

// AStockSessionLag counts completed exchange sessions missing after a data date.
// Weekends and exchange holidays do not age market data. A bar dated today may
// be provisional; freshness alone never confirms that its close has completed.
// Counts are capped at six: callers only distinguish lags above five sessions.
func AStockSessionLag(date string, asOf time.Time) (int, bool) {
	day, err := time.ParseInLocation("2006-01-02", date, AStockLocation)
	if err != nil || asOf.IsZero() || day.After(asOf) {
		return 0, false
	}
	latest := LatestCompletedAStockSession(asOf)
	lag := 0
	for next := day.AddDate(0, 0, 1); !next.After(latest); next = next.AddDate(0, 0, 1) {
		if IsAStockTradingDay(next) {
			lag++
		}
		if lag > 5 {
			break
		}
	}
	return lag, true
}

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
	day := value.In(AStockLocation)
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
