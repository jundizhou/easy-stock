package nationalteam

import (
	"easy-stock/backend/internal/foundation"
	"testing"
	"time"
)

func TestSubscriptionYearStartClampsLeapDayAndUsesChinaDate(t *testing.T) {
	for _, tc := range []struct{ now, want string }{
		{"2026-11-19T10:00:00+08:00", "2025-11-19"},
		{"2024-02-29T10:00:00+08:00", "2023-02-28"},
		{"2026-01-01T00:00:00+08:00", "2025-01-01"},
		{"2026-11-18T20:00:00Z", "2025-11-19"},
	} {
		now, err := time.Parse(time.RFC3339, tc.now)
		if err != nil {
			t.Fatal(err)
		}
		if got := SubscriptionYearStart(now); got != tc.want {
			t.Errorf("SubscriptionYearStart(%s) = %s, want %s", tc.now, got, tc.want)
		}
	}
}

func TestSubscriptionAmountsUseSharesAndSameDayCloseNotTradingFlows(t *testing.T) {
	raw := map[string]SubscriptionRaw{}
	for _, item := range SubscriptionCatalog() {
		raw[item.Code] = SubscriptionRaw{Shares: map[string]float64{"2026-11-13": 1e8, "2026-11-16": 1.1e8}, Prices: map[string]float64{"2026-11-16": 4}}
	}
	result := BuildSubscriptions(raw, []string{"2026-11-13", "2026-11-16"}, "2026-10-17", "2026-11-16", nil)
	if len(result.Days) != 1 || result.Days[0].EstimatedNet == nil || *result.Days[0].EstimatedNet != 8.8e8 || result.Days[0].Subscribed != 22 || result.LatestCompleteDate != "2026-11-16" {
		t.Fatalf("bad totals %+v", result.Days)
	}
	point := result.Series[0].Points[0]
	if point.PriceDate != point.Date || point.PreviousDate != "2026-11-13" || *point.ShareChange != 1e7 {
		t.Fatalf("misaligned data %+v", point)
	}
}
func TestSubscriptionMissingBaselineAndPriceNeverUseFallback(t *testing.T) {
	raw := map[string]SubscriptionRaw{}
	for _, item := range SubscriptionCatalog() {
		raw[item.Code] = SubscriptionRaw{Shares: map[string]float64{"2026-11-12": 100, "2026-11-13": 110, "2026-11-16": 120}, Prices: map[string]float64{"2026-11-13": 3, "2026-11-16": 4}}
	}
	values := raw["510300"]
	delete(values.Shares, "2026-11-13")
	raw["510300"] = values
	values = raw["510310"]
	delete(values.Prices, "2026-11-16")
	values.Prices["2026-11-17"] = 999
	raw["510310"] = values
	result := BuildSubscriptions(raw, []string{"2026-11-12", "2026-11-13", "2026-11-16"}, "2026-11-13", "2026-11-16", nil)
	if result.Days[1].EstimatedNet != nil || result.Days[1].Coverage != 20 || result.Series[0].Points[1].ShareChange != nil || result.Series[1].Points[1].EstimatedNet != nil {
		t.Fatalf("missing values faked: %+v %+v", result.Days, result.Series[:2])
	}
}
func TestSubscriptionActualZeroAndDateConsistency(t *testing.T) {
	raw := map[string]SubscriptionRaw{}
	for _, item := range SubscriptionCatalog() {
		raw[item.Code] = SubscriptionRaw{Shares: map[string]float64{"2026-11-13": 100.00000001, "2026-11-16": 100}, Prices: map[string]float64{"2026-11-16": 2}}
	}
	result := BuildSubscriptions(raw, []string{"2026-11-16", "2026-11-13", "2026-11-16"}, "2026-11-16", "2026-11-16", nil)
	if len(result.Days) != 1 || result.Days[0].EstimatedNet == nil || *result.Days[0].EstimatedNet != 0 || *result.Series[0].Points[0].ShareChange != 0 {
		t.Fatalf("float artifact or zero lost %+v", result)
	}
}
func TestSubscriptionMismatchExcludedUntilResolved(t *testing.T) {
	raw := map[string]SubscriptionRaw{}
	for _, item := range SubscriptionCatalog() {
		raw[item.Code] = SubscriptionRaw{Shares: map[string]float64{"2026-11-13": 100, "2026-11-16": 110}, Prices: map[string]float64{"2026-11-16": 4}}
	}
	result := BuildSubscriptions(raw, []string{"2026-11-13", "2026-11-16"}, "2026-11-16", "2026-11-16", []foundation.ETFSubscriptionCheck{{Code: "510300", Date: "2026-11-16", Status: "不一致"}})
	if result.Days[0].Coverage != 21 || result.Days[0].EstimatedNet != nil {
		t.Fatalf("unresolved mismatch included %+v", result.Days)
	}
}
