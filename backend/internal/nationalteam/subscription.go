package nationalteam

import (
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// The same disclosed-holder sample of 22 broad-market ETFs as national-team-etf-monitor.
// Inclusion in this sample does not identify the owners of daily subscriptions.
func SubscriptionCatalog() []foundation.ETFSubscriptionSeries {
	return []foundation.ETFSubscriptionSeries{
		{Code: "510300", Name: "华泰柏瑞沪深300ETF", Index: "沪深300", Exchange: "SSE"},
		{Code: "510310", Name: "易方达沪深300ETF", Index: "沪深300", Exchange: "SSE"},
		{Code: "510330", Name: "华夏沪深300ETF", Index: "沪深300", Exchange: "SSE"},
		{Code: "159919", Name: "嘉实沪深300ETF", Index: "沪深300", Exchange: "SZSE"},
		{Code: "510050", Name: "华夏上证50ETF", Index: "上证50", Exchange: "SSE"},
		{Code: "510500", Name: "南方中证500ETF", Index: "中证500", Exchange: "SSE"},
		{Code: "512100", Name: "南方中证1000ETF", Index: "中证1000", Exchange: "SSE"},
		{Code: "159915", Name: "易方达创业板ETF", Index: "创业板", Exchange: "SZSE"},
		{Code: "159845", Name: "华夏中证1000ETF", Index: "中证1000", Exchange: "SZSE"},
		{Code: "560010", Name: "广发中证1000ETF", Index: "中证1000", Exchange: "SSE"},
		{Code: "588080", Name: "易方达科创50ETF", Index: "科创50", Exchange: "SSE"},
		{Code: "510180", Name: "华安上证180ETF", Index: "上证180", Exchange: "SSE"},
		{Code: "512500", Name: "华夏中证500ETF", Index: "中证500", Exchange: "SSE"},
		{Code: "159629", Name: "富国中证1000ETF", Index: "中证1000", Exchange: "SZSE"},
		{Code: "159922", Name: "嘉实中证500ETF", Index: "中证500", Exchange: "SZSE"},
		{Code: "159901", Name: "易方达深证100ETF", Index: "深证100", Exchange: "SZSE"},
		{Code: "159952", Name: "广发创业板ETF", Index: "创业板", Exchange: "SZSE"},
		{Code: "515800", Name: "汇添富中证800ETF", Index: "中证800", Exchange: "SSE"},
		{Code: "588050", Name: "工银瑞信科创50ETF", Index: "科创50", Exchange: "SSE"},
		{Code: "159977", Name: "天弘创业板ETF", Index: "创业板", Exchange: "SZSE"},
		{Code: "560050", Name: "汇添富MSCI中国A50ETF", Index: "MSCI中国A50", Exchange: "SSE"},
		{Code: "510100", Name: "易方达上证50ETF", Index: "上证50", Exchange: "SSE"},
	}
}

type SubscriptionRaw struct {
	Shares map[string]float64
	Prices map[string]float64
}

func SubscriptionYearStart(now time.Time) string {
	now = now.In(China)
	last := time.Date(now.Year()-1, now.Month()+1, 1, 0, 0, 0, 0, China).AddDate(0, 0, -1)
	return time.Date(last.Year(), last.Month(), min(now.Day(), last.Day()), 0, 0, 0, 0, China).Format(time.DateOnly)
}

func goodValue(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// BuildSubscriptions uses a common calendar and a strictly matching price date.
// Missing intervening dates never become multi-day deltas labelled as daily flows.
func BuildSubscriptions(raw map[string]SubscriptionRaw, calendar []string, start, end string, checks []foundation.ETFSubscriptionCheck) foundation.ETFSubscriptionDashboard {
	calendar = append([]string{}, calendar...)
	sort.Strings(calendar)
	unique := calendar[:0]
	for _, day := range calendar {
		if len(unique) == 0 || unique[len(unique)-1] != day {
			unique = append(unique, day)
		}
	}
	calendar = unique
	result := foundation.ETFSubscriptionDashboard{StartDate: start, SourceDate: end, Expected: len(SubscriptionCatalog()), Series: SubscriptionCatalog(), Days: []foundation.ETFSubscriptionDay{}, Checks: checks, Warnings: []string{}}
	if result.Checks == nil {
		result.Checks = []foundation.ETFSubscriptionCheck{}
	}
	verification := map[string]string{}
	for _, check := range checks {
		verification[check.Code+"|"+check.Date] = check.Status
	}
	type aggregate struct {
		net, in, out                   float64
		coverage, subscribed, redeemed int
	}
	totals := map[string]aggregate{}
	previousDates := map[string]string{}
	for index, day := range calendar {
		if day < start || day > end || index == 0 {
			continue
		}
		previousDates[day] = calendar[index-1]
		totals[day] = aggregate{}
	}
	for index, item := range result.Series {
		values := raw[item.Code]
		item.Points = []foundation.ETFSubscriptionPoint{}
		for _, day := range calendar {
			prev, ok := previousDates[day]
			if !ok {
				continue
			}
			point := foundation.ETFSubscriptionPoint{Date: day, PreviousDate: prev, Verification: verification[item.Code+"|"+day]}
			if point.Verification == "" {
				point.Verification = "公开快照，未独立核验"
			}
			if shares, exists := values.Shares[day]; exists && goodValue(shares) && shares >= 0 {
				value := math.Round(shares)
				point.Shares = &value
			}
			if shares, exists := values.Shares[prev]; exists && goodValue(shares) && shares >= 0 {
				value := math.Round(shares)
				point.PreviousShares = &value
			}
			if price, exists := values.Prices[day]; exists && goodValue(price) && price > 0 {
				point.Close = &price
				point.PriceDate = day
			}
			currentMismatch := strings.Contains(verification[item.Code+"|"+day], "不一致")
			previousMismatch := strings.Contains(verification[item.Code+"|"+prev], "不一致")
			if point.Shares != nil && point.PreviousShares != nil && !currentMismatch && !previousMismatch {
				delta := *point.Shares - *point.PreviousShares
				point.ShareChange = &delta
				if point.Close != nil {
					net := delta * *point.Close
					point.EstimatedNet = &net
				}
			}
			if currentMismatch || previousMismatch {
				point.Verification = "交易所核验不一致，未计入估算"
			}
			aggregate := totals[day]
			if point.EstimatedNet != nil {
				net := *point.EstimatedNet
				aggregate.net += net
				aggregate.coverage++
				if net > 0 {
					aggregate.in += net
					aggregate.subscribed++
				}
				if net < 0 {
					aggregate.out += net
					aggregate.redeemed++
				}
			}
			totals[day] = aggregate
			item.Points = append(item.Points, point)
		}
		result.Series[index] = item
	}
	for day, total := range totals {
		point := foundation.ETFSubscriptionDay{Date: day, PreviousDate: previousDates[day], Coverage: total.coverage, Subscribed: total.subscribed, Redeemed: total.redeemed}
		if total.coverage == result.Expected {
			point.EstimatedNet = &total.net
			point.Inflow = &total.in
			point.Outflow = &total.out
			if day > result.LatestCompleteDate {
				result.LatestCompleteDate = day
			}
		}
		if day > result.LatestDate {
			result.LatestDate = day
		}
		result.Days = append(result.Days, point)
	}
	sort.Slice(result.Days, func(i, j int) bool { return result.Days[i].Date < result.Days[j].Date })
	return result
}
