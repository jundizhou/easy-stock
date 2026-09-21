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
