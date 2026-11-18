package nationalteam

import (
	"math"
	"time"
)

var China = time.FixedZone("Asia/Shanghai", 8*60*60)

func valid(value *float64) bool { return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) }
func date(t time.Time) string   { return t.In(China).Format(time.DateOnly) }
func minute(t time.Time) int    { t = t.In(China); return t.Hour()*60 + t.Minute() }
func tradingHours(t time.Time) bool {
	t = t.In(China)
	m := minute(t)
	return t.Weekday() != time.Saturday && t.Weekday() != time.Sunday && ((m >= 570 && m <= 690) || (m >= 780 && m <= 900))
}
