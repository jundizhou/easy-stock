package themeindex

import (
	"math"
	"reflect"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func testBar(date string, close float64) foundation.KLine {
	day, _ := time.ParseInLocation("2006-01-02", date, location)
	return foundation.KLine{Time: day, Open: close, High: close, Low: close, Close: close, Volume: 10, Amount: 100}
}

var testNow = time.Date(2026, 10, 6, 13, 0, 0, 0, location)

func TestComposeIsScaleInvariantAndChainsEqualWeightReturns(t *testing.T) {
	paths := map[string][]foundation.KLine{
		"A": {testBar("2026-09-28", 100), testBar("2026-09-29", 110), testBar("2026-09-30", 121)},
		"B": {testBar("2026-09-28", 10), testBar("2026-09-29", 9), testBar("2026-09-30", 9)},
	}
	lines, q := Compose("theme", paths, 2, testNow)
	if len(lines) != 2 || math.Abs(lines[0].Close-1000) > 1e-9 || math.Abs(lines[1].Close-1050) > 1e-9 || q.MinimumCoverage != 1 {
		t.Fatalf("equal weighting: %+v %+v", lines, q)
	}
	for i := range paths["A"] {
		bar := &paths["A"][i]
		bar.Open *= 100
		bar.High *= 100
		bar.Low *= 100
		bar.Close *= 100
	}
	scaled, _ := Compose("theme", paths, 2, testNow)
	for i := range lines {
		if math.Abs(lines[i].Close-scaled[i].Close) > 1e-9 {
			t.Fatal("nominal share prices changed index returns")
		}
	}
	for range 20 {
		repeated, _ := Compose("theme", paths, 2, testNow)
		if !reflect.DeepEqual(repeated, scaled) {
			t.Fatal("composition depends on map iteration order")
		}
	}
}
func TestComposeSuspensionCarriesFlatAndStaleTailIsNotInvented(t *testing.T) {
	paths := map[string][]foundation.KLine{
		"A": {testBar("2026-09-28", 100), testBar("2026-09-30", 110)},
		"B": {testBar("2026-09-28", 100), testBar("2026-09-29", 100), testBar("2026-09-30", 100)},
	}
	lines, q := Compose("theme", paths, 2, testNow)
	if len(lines) != 2 || lines[0].Close != 1000 || lines[1].Close != 1050 || q.SkippedDays != 0 {
		t.Fatalf("suspension: %+v %+v", lines, q)
	}
	paths["A"] = []foundation.KLine{testBar("2026-09-28", 100), testBar("2026-09-29", 100)}
	lines, q = Compose("theme", paths, 2, testNow)
	if len(lines) != 1 || q.SkippedDays != 1 || q.MinimumCoverage != .5 {
		t.Fatalf("stale tail was carried forward: %+v %+v", lines, q)
	}
}
func TestComposeForwardAdjustmentAndIPOAlignment(t *testing.T) {
	paths := map[string][]foundation.KLine{
		"adjusted": {testBar("2026-09-28", 50), testBar("2026-09-29", 50), testBar("2026-09-30", 55)},
		"IPO":      {testBar("2026-09-29", 200), testBar("2026-09-30", 220)},
	}
	lines, _ := Compose("theme", paths, 2, testNow)
	if len(lines) != 2 || lines[0].Close != 1000 || math.Abs(lines[1].Close-1100) > 1e-9 {
		t.Fatalf("adjustment/IPO: %+v", lines)
	}
}
func TestCleanBarsRejectsHolidayMalformedFutureAndDuplicateBars(t *testing.T) {
	invalid := testBar("2026-09-29", 10)
	invalid.Close = math.NaN()
	input := []foundation.KLine{testBar("2026-09-30", 10), testBar("2026-10-01", 10), testBar("2026-10-08", 10), invalid, testBar("2026-09-30", 11)}
	lines := CleanBars(input, testNow)
	if len(lines) != 1 || lines[0].Close != 11 {
		t.Fatalf("unexpected validated bars: %+v", lines)
	}
}
func TestComposeExtremaAreBoundsAndSamplingErrorUsesFinitePopulation(t *testing.T) {
	a := testBar("2026-09-29", 110)
	a.Open = 105
	a.High = 120
	a.Low = 95
	b := testBar("2026-09-29", 90)
	b.Open = 95
	b.High = 110
	b.Low = 80
	paths := map[string][]foundation.KLine{"A": {testBar("2026-09-28", 100), a}, "B": {testBar("2026-09-28", 100), b}}
	lines, q := Compose("theme", paths, 200, testNow)
	if len(lines) != 1 || lines[0].High != 1150 || lines[0].Low != 875 || lines[0].Open != 1000 || q.SamplingErrorPercent <= 0 || !q.EstimatedExtrema {
		t.Fatalf("bounds/precision: %+v %+v", lines, q)
	}
	_, full := Compose("theme", paths, 2, testNow)
	if full.SamplingErrorPercent != 0 {
		t.Fatal("full population has sampling error")
	}
}
func BenchmarkCompose128Stocks300Sessions(b *testing.B) {
	dates := make([]time.Time, 0, 301)
	for day := testNow; len(dates) < 301; day = day.AddDate(0, 0, -1) {
		if foundation.IsAStockTradingDay(day) {
			dates = append(dates, day)
		}
	}
	paths := map[string][]foundation.KLine{}
	for stock := 0; stock < 128; stock++ {
		bars := make([]foundation.KLine, 0, len(dates))
		for index := len(dates) - 1; index >= 0; index-- {
			bar := testBar(dates[index].Format("2006-01-02"), 100+float64(index%17))
			bars = append(bars, bar)
		}
		paths[string(rune(stock+100))] = bars
	}
	b.ResetTimer()
	for range b.N {
		Compose("theme", paths, 128, testNow)
	}
}
