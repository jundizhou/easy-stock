package nationalteam

import (
	"math"
	"time"

	"easy-stock/backend/internal/foundation"
)

// TrialQuoteUsable treats prices as dated inputs, not evidence of a share date.
// During trading hours quotes older than 3 minutes are excluded; lunch/closing
// quotes must have reached the respective session boundary.
func TrialQuoteUsable(stamp *time.Time, now time.Time) bool {
	if stamp == nil || date(*stamp) != date(now) || stamp.After(now.Add(time.Minute)) {
		return false
	}
	if tradingHours(now) {
		return now.Sub(*stamp) <= 3*time.Minute
	}
	if minute(now) > 900 {
		return minute(*stamp) >= 900
	}
	if minute(now) > 690 && minute(now) < 780 {
		return minute(*stamp) >= 685
	}
	return false
}

func BuildSubscriptionTrial(raw map[string]SubscriptionRaw, calendar []string, quotes map[string]foundation.ETFSubscriptionSeries, now time.Time) foundation.ETFSubscriptionTrial {
	today := date(now)
	previous := ""
	// Previous means the preceding available market-calendar day, not a selected
	// convenient per-instrument observation spanning an unknown missing period.
	for _, day := range calendar {
		if day < today && day > previous {
			previous = day
		}
	}
	result := foundation.ETFSubscriptionTrial{Date: today, PreviousDate: previous, Expected: len(SubscriptionCatalog()), Items: []foundation.ETFSubscriptionTrialItem{}}
	total := 0.0
	for _, definition := range SubscriptionCatalog() {
		values := raw[definition.Code]
		quote := quotes[definition.Code]
		item := foundation.ETFSubscriptionTrialItem{Code: definition.Code, Name: definition.Name, Price: quote.LatestPrice, QuoteTime: quote.QuoteTime}
		if shares, ok := values.Shares[today]; ok && goodValue(shares) && shares >= 0 {
			v := math.Round(shares)
			item.Shares = &v
			item.ShareDate = today
			result.ShareCoverage++
		}
		if shares, ok := values.Shares[previous]; ok && previous != "" && goodValue(shares) && shares >= 0 {
			v := math.Round(shares)
			item.PreviousShares = &v
			result.BaselineCoverage++
		}
		validPrice := valid(item.Price) && *item.Price > 0 && TrialQuoteUsable(item.QuoteTime, now)
		if validPrice {
			result.PriceCoverage++
		}
		switch {
		case item.Shares == nil:
			item.Status = "等待今日份额披露"
		case item.PreviousShares == nil:
			item.Status = "前一交易日份额基准缺失"
		case !validPrice:
			item.Status = "今日价格缺失或已过期"
		default:
			delta := *item.Shares - *item.PreviousShares
			net := delta * *item.Price
			item.ShareChange = &delta
			item.EstimatedNet = &net
			result.Coverage++
			total += net
			item.Status = "当日份额 × 最新价试算"
			if minute(now) > 900 {
				item.Status = "当日份额 × 收盘报价试算"
			}
		}
		result.Items = append(result.Items, item)
	}
	if result.Coverage > 0 {
		result.CoveredNet = &total
	}
	switch {
	case result.ShareCoverage == 0:
		result.Status = "等待今日份额披露"
	case result.Coverage == result.Expected:
		result.Status = "当日试算已齐备"
		result.EstimatedNet = &total
	case result.Coverage > 0:
		result.Status = "部分 ETF 可试算，全池未齐备"
	default:
		result.Status = "今日试算条件尚未齐备"
	}
	return result
}
