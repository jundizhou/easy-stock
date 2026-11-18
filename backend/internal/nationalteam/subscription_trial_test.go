package nationalteam

import (
	"easy-stock/backend/internal/foundation"
	"testing"
	"time"
)

func trialInputs(now time.Time) (map[string]SubscriptionRaw, map[string]foundation.ETFSubscriptionSeries) {
	raw := map[string]SubscriptionRaw{}
	quotes := map[string]foundation.ETFSubscriptionSeries{}
	for _, item := range SubscriptionCatalog() {
		raw[item.Code] = SubscriptionRaw{Shares: map[string]float64{"2026-11-16": 100, "2026-11-17": 120}, Prices: map[string]float64{}}
		price := 4.0
		stamp := now
		quotes[item.Code] = foundation.ETFSubscriptionSeries{LatestPrice: &price, QuoteTime: &stamp}
	}
	return raw, quotes
}
func TestTrialUsesTodaySharesAndTodayPriceWithoutChangingHistory(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 30, 0, 0, China)
	raw, quotes := trialInputs(now)
	result := BuildSubscriptionTrial(raw, []string{"2026-11-13", "2026-11-16"}, quotes, now)
	if result.Coverage != 22 || result.EstimatedNet == nil || *result.EstimatedNet != 1760 || result.PreviousDate != "2026-11-16" {
		t.Fatalf("trial %+v", result)
	}
	quote := quotes["510300"]
	price := 5.0
	quote.LatestPrice = &price
	quotes["510300"] = quote
	updated := BuildSubscriptionTrial(raw, []string{"2026-11-16"}, quotes, now)
	if *updated.EstimatedNet != 1780 || len(raw["510300"].Prices) != 0 {
		t.Fatalf("did not recalculate or wrote live price into daily history %+v", updated)
	}
}
func TestTrialNeverRelabelsYesterdaySharesAsToday(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 30, 0, 0, China)
	raw, quotes := trialInputs(now)
	for code, values := range raw {
		delete(values.Shares, "2026-11-17")
		raw[code] = values
	}
	result := BuildSubscriptionTrial(raw, []string{"2026-11-13", "2026-11-16"}, quotes, now)
	if result.ShareCoverage != 0 || result.PriceCoverage != 22 || result.Coverage != 0 || result.EstimatedNet != nil || result.CoveredNet != nil || result.Status != "等待今日份额披露" {
		t.Fatalf("yesterday's units fabricated today's total: %+v", result)
	}
}
func TestTrialPartialCoverageMissingBaselineAndTrueZero(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 30, 0, 0, China)
	raw, quotes := trialInputs(now)
	values := raw["510300"]
	delete(values.Shares, "2026-11-17")
	raw["510300"] = values
	values = raw["510310"]
	delete(values.Shares, "2026-11-16")
	raw["510310"] = values
	result := BuildSubscriptionTrial(raw, []string{"2026-11-13", "2026-11-16"}, quotes, now)
	if result.Coverage != 20 || result.EstimatedNet != nil || result.CoveredNet == nil || *result.CoveredNet != 1600 {
		t.Fatalf("partial total passed as complete: %+v", result)
	}
	raw, quotes = trialInputs(now)
	for code, values := range raw {
		values.Shares["2026-11-17"] = 100
		raw[code] = values
	}
	result = BuildSubscriptionTrial(raw, []string{"2026-11-16"}, quotes, now)
	if result.EstimatedNet == nil || *result.EstimatedNet != 0 {
		t.Fatal("real zero treated as missing")
	}
}
func TestTrialRejectsStaleFutureAndCrossDayQuotes(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 30, 0, 0, China)
	for _, stamp := range []time.Time{now.Add(-4 * time.Minute), now.Add(2 * time.Minute), now.AddDate(0, 0, -1)} {
		raw, quotes := trialInputs(now)
		q := quotes["510300"]
		q.QuoteTime = &stamp
		quotes["510300"] = q
		result := BuildSubscriptionTrial(raw, []string{"2026-11-16"}, quotes, now)
		if result.PriceCoverage != 21 || result.EstimatedNet != nil {
			t.Fatalf("bad quote included %+v", result)
		}
	}
	closeNow := time.Date(2026, 11, 17, 18, 0, 0, 0, China)
	morning := closeNow.Add(-8 * time.Hour)
	closed := closeNow.Add(-3 * time.Hour)
	if TrialQuoteUsable(&morning, closeNow) || !TrialQuoteUsable(&closed, closeNow) {
		t.Fatal("morning quote mislabeled as a closing quote")
	}
}
