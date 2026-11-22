package stockanalysis

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"easy-stock/backend/internal/foundation"
)

// The provider's field list distinguishes a disclosed zero from a missing value.
type financialFields map[string]float64

func reportedFinancialFields(item *foundation.StockFundamentals, hk bool) financialFields {
	values := financialFields{
		"revenue": item.Revenue, "revenue_yoy": item.RevenueYearOverYear,
		"net_profit": item.NetProfit, "net_profit_yoy": item.NetProfitYearOverYear,
		"deducted_net_profit": item.DeductedNetProfit, "deducted_net_profit_yoy": item.DeductedNetProfitYearOverYear,
		"eps": item.EPS, "roe": item.ROE, "gross_margin": item.GrossMargin, "debt_ratio": item.DebtRatio,
		"operating_cash_flow_per_share": item.OperatingCashFlowPerShare,
	}
	for key, value := range values {
		if item.Meta.AvailableFields != nil {
			if !slices.Contains(item.Meta.AvailableFields, key) {
				delete(values, key)
			}
		} else if hk && value == 0 {
			// Older HK rows lack presence information for zero-valued fields.
			delete(values, key)
		}
	}
	if hk || (item.Meta.AvailableFields == nil && !item.DeductedNetProfitAvailable && item.DeductedNetProfit == 0 && item.DeductedNetProfitYearOverYear == 0) {
		delete(values, "deducted_net_profit")
		delete(values, "deducted_net_profit_yoy")
	}
	return values
}

func (f financialFields) number(key string) *float64 {
	if value, ok := f[key]; ok {
		return &value
	}
	return nil
}

func analyzeFundamentals(symbol string, item *foundation.StockFundamentals) FundamentalAnalysis {
	if item == nil || strings.TrimSpace(item.ReportDate) == "" {
		return FundamentalAnalysis{Quality: "数据不足", Sustainability: "数据不足", Summary: "尚未取得最新F10财务数据"}
	}
	hk := foundation.IsHongKongSymbol(symbol)
	fields := reportedFinancialFields(item, hk)
	recurringProfit, hasDeductedProfit := fields["deducted_net_profit"]
	result := FundamentalAnalysis{
		Available: len(fields) > 0, ReportDate: item.ReportDate, ReportName: item.ReportName,
		Revenue: fields.number("revenue"), RevenueYearOverYear: fields.number("revenue_yoy"),
		NetProfit: fields.number("net_profit"), NetProfitYearOverYear: fields.number("net_profit_yoy"),
		RecurringNetProfitAvailable: hasDeductedProfit, RecurringNetProfit: fields.number("deducted_net_profit"),
		RecurringNetProfitYearOverYear: fields.number("deducted_net_profit_yoy"),
		EPS:                            fields.number("eps"), ROE: fields.number("roe"), GrossMargin: fields.number("gross_margin"),
		DebtRatio: fields.number("debt_ratio"), OperatingCashFlowPerShare: fields.number("operating_cash_flow_per_share"),
		Sustainability: "待确认", Source: item.Meta.Source, Currency: item.Currency,
	}
	growth := fields["net_profit_yoy"]
	if hasDeductedProfit {
		growth = fields["deducted_net_profit_yoy"]
	}
	score := 50 + clamp(fields["revenue_yoy"]/8, -15, 15) + clamp(growth/6, -20, 20)
	if hasDeductedProfit && result.NetProfit != nil {
		profit := *result.NetProfit
		oneOff := profit - recurringProfit
		result.NonRecurringProfit = &oneOff
		if math.Abs(profit) > 1e-9 {
			ratio := math.Abs(oneOff) / math.Abs(profit) * 100
			result.NonRecurringProfitRatio = new(round2(ratio))
			result.Sustainability = "较好"
			switch {
			case recurringProfit <= 0 && profit > 0:
				result.Sustainability = "较差"
				result.SustainabilityFlags = append(result.SustainabilityFlags, "扣非后利润非正")
				score -= 20
			case oneOff > 0 && ratio >= 50:
				result.Sustainability = "较差"
				result.SustainabilityFlags = append(result.SustainabilityFlags, "净利润主要依赖非经常性损益")
				score -= 18
			case oneOff > 0 && ratio >= 30:
				result.Sustainability = "一般"
				result.SustainabilityFlags = append(result.SustainabilityFlags, "非经常性损益占比较高")
				score -= 12
			case oneOff > 0 && ratio >= 15:
				result.Sustainability = "一般"
				result.SustainabilityFlags = append(result.SustainabilityFlags, "存在一定一次性收益影响")
				score -= 6
			}
		}
	} else if !hk && !hasDeductedProfit {
		result.SustainabilityFlags = []string{"缺少扣非净利润，持续性无法完全核验"}
	}
	roe := fields["roe"]
	switch {
	case roe >= 15:
		score += 12
	case roe >= 8:
		score += 6
	case roe > 0 && roe < 3:
		score -= 6
	case roe < 0:
		score -= 15
	}
	if margin := fields["gross_margin"]; margin >= 35 {
		score += 8
	} else if margin > 0 && margin < 12 {
		score -= 8
	}
	if debt := fields["debt_ratio"]; debt >= 75 {
		score -= 12
	} else if debt > 0 && debt <= 45 {
		score += 5
	}
	if cash := fields["operating_cash_flow_per_share"]; cash > 0 {
		score += 5
	} else if cash < 0 {
		score -= 5
	}
	result.Score = int(math.Round(clamp(score, 0, 100)))
	switch {
	case result.Score >= 72:
		result.Quality = "较好"
	case result.Score >= 58:
		result.Quality = "稳健"
	case result.Score < 35:
		result.Quality = "承压"
	case result.Score < 48:
		result.Quality = "偏弱"
	default:
		result.Quality = "中性"
	}
	var metrics []string
	addMetric := func(format string, value *float64) {
		if value != nil {
			metrics = append(metrics, fmt.Sprintf(format, *value))
		}
	}
	addMetric("营收同比%+.1f%%", result.RevenueYearOverYear)
	addMetric("归母净利同比%+.1f%%", result.NetProfitYearOverYear)
	if hasDeductedProfit {
		addMetric("扣非净利同比%+.1f%%", result.RecurringNetProfitYearOverYear)
	}
	addMetric("ROE %.1f%%", result.ROE)
	addMetric("毛利率%.1f%%", result.GrossMargin)
	addMetric("负债率%.1f%%", result.DebtRatio)
	addMetric("非经常性损益占归母净利%.1f%%", result.NonRecurringProfitRatio)
	addMetric("扣非净利润%.2f", result.RecurringNetProfit)
	result.Summary = firstNonEmpty(item.ReportName, item.ReportDate)
	if len(metrics) > 0 {
		result.Summary += "：" + strings.Join(metrics, "，")
	}
	return result
}
