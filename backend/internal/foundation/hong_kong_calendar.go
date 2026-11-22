package foundation

import "time"

// Hong Kong cash-market holidays and half days, per the HKEX trading calendars.
var hongKongHolidays = map[string]bool{
	"2025-01-01": true,
	"2025-01-29": true,
	"2025-01-30": true,
	"2025-01-31": true,
	"2025-04-04": true,
	"2025-04-18": true,
	"2025-04-21": true,
	"2025-05-01": true,
	"2025-05-05": true,
	"2025-07-01": true,
	"2025-10-01": true,
	"2025-10-07": true,
	"2025-10-29": true,
	"2025-12-25": true,
	"2025-12-26": true,
	"2026-01-01": true,
	"2026-02-17": true,
	"2026-02-18": true,
	"2026-02-19": true,
	"2026-04-03": true,
	"2026-04-06": true,
	"2026-04-07": true,
	"2026-05-01": true,
	"2026-05-25": true,
	"2026-06-19": true,
	"2026-07-01": true,
	"2026-10-01": true,
	"2026-10-19": true,
	"2026-12-25": true,
	"2027-01-01": true,
	"2027-02-08": true,
	"2027-02-09": true,
	"2027-03-26": true,
	"2027-03-29": true,
	"2027-04-05": true,
	"2027-05-13": true,
	"2027-06-09": true,
	"2027-07-01": true,
	"2027-09-16": true,
	"2027-10-01": true,
	"2027-10-08": true,
	"2027-12-27": true,
}

func IsHongKongTradingDay(now time.Time) bool {
	day := now.In(AStockLocation)
	return day.Weekday() != time.Saturday && day.Weekday() != time.Sunday && !hongKongHolidays[day.Format("2006-01-02")]
}

func HongKongMarketOpen(now time.Time) bool {
	local := now.In(AStockLocation)
	if !IsHongKongTradingDay(local) {
		return false
	}
	minutes := local.Hour()*60 + local.Minute()
	if minutes >= 9*60+30 && minutes < 12*60 {
		return true
	}
	return minutes >= 13*60 && minutes < hongKongCloseHour(local)*60
}

func hongKongCloseHour(local time.Time) int {
	switch local.Format("2006-01-02") {
	case "2025-01-28", "2026-02-16", "2027-02-05":
		return 12
	}
	if local.Month() == time.December && (local.Day() == 24 || local.Day() == 31) {
		return 12
	}
	return 16
}

func LatestSymbolSession(symbol string, now time.Time) string {
	if !IsHongKongSymbol(symbol) {
		return LatestAStockSession(now)
	}
	return LatestHongKongSession(now)
}

func LatestHongKongSession(now time.Time) string { return latestSession(now, IsHongKongTradingDay) }
