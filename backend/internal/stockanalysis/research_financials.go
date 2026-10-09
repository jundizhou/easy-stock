package stockanalysis

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

func financialConflictReason(source ResearchSource) string {
	var data struct {
		Data struct {
			ReportDate string `json:"report_date"`
		} `json:"data"`
		Checks []struct {
			ReportDate string   `json:"report_date"`
			Fields     []string `json:"conflicting_fields"`
		} `json:"cross_checks"`
	}
	if json.Unmarshal([]byte(source.Content), &data) != nil {
		return ""
	}
	for _, check := range data.Checks {
		if check.ReportDate == strings.Split(data.Data.ReportDate, " ")[0] && len(check.Fields) > 0 {
			return "最新报告期财务数据跨来源存在冲突，需核对公司原始披露：" + strings.Join(check.Fields, "、")
		}
	}
	return ""
}

// Model evidence must not expose fallback struct defaults as reported zeroes.
func financialModelRows(items []foundation.StockFundamentals, supplement []foundation.StockFinancialEvidence) []any {
	result := []any{}
	for _, item := range items {
		if item.Meta.Source != "sina:financial-indicators" {
			result = append(result, item)
			continue
		}
		found := false
		for _, raw := range supplement {
			if raw.ReportDate != item.ReportDate {
				continue
			}
			row := map[string]any{"symbol": item.Symbol, "report_date": item.ReportDate, "published_at": item.PublishedAt, "financial_provider": item.Meta.Source, "deducted_net_profit_available": item.DeductedNetProfitAvailable}
			for key, value := range raw.Fields {
				row[key] = value
			}
			result = append(result, row)
			found = true
			break
		}
		if !found {
			result = append(result, map[string]any{"symbol": item.Symbol, "report_date": item.ReportDate, "revenue": item.Revenue, "net_profit": item.NetProfit, "financial_provider": item.Meta.Source, "limitation": "补充来源字段明细缺失，只保留已确认的收入和归母净利润"})
		}
	}
	return result
}

func selectComparableFinancialPeriods(items []any, latest string, limit int) []any {
	latest = strings.Split(latest, " ")[0]
	day, err := time.Parse("2006-01-02", latest)
	if err != nil {
		return items[:min(limit, len(items))]
	}
	prior := day.AddDate(-1, 0, 0).Format("2006-01-02")
	quarter := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -3, 0)
	quarter = time.Date(quarter.Year(), quarter.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	wanted := []string{latest, prior, quarter.Format("2006-01-02"), quarter.AddDate(-1, 0, 0).Format("2006-01-02")}
	result := []any{}
	seen := map[int]bool{}
	for _, date := range wanted {
		for i, item := range items {
			value, ok := item.(map[string]any)
			if !ok {
				continue
			}
			raw, _ := value["report_date"].(string)
			if strings.Split(raw, " ")[0] == date && !seen[i] {
				result = append(result, item)
				seen[i] = true
				break
			}
		}
		if len(result) >= limit {
			return result
		}
	}
	for i, item := range items {
		if !seen[i] {
			result = append(result, item)
		}
		if len(result) >= limit {
			break
		}
	}
	return result
}

func financialEvidenceAt(items []foundation.StockFinancialEvidence, symbol string, cutoff time.Time) []foundation.StockFinancialEvidence {
	result := []foundation.StockFinancialEvidence{}
	for _, item := range items {
		day, err := time.ParseInLocation("2006-01-02", item.ReportDate, foundation.AStockLocation)
		if err != nil || day.After(cutoff) || item.Symbol != symbol || item.PublishedAt.After(cutoff) {
			continue
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ReportDate > result[j].ReportDate })
	return result[:min(8, len(result))]
}

// Fallback uses explicit core values, preserving missing deducted-profit status.
func FinancialFallback(items []foundation.StockFinancialEvidence, symbol string, cutoff time.Time) []foundation.StockFundamentals {
	result := []foundation.StockFundamentals{}
	for _, item := range financialEvidenceAt(items, symbol, cutoff) {
		_, revenue := item.Fields["revenue"]
		_, profit := item.Fields["net_profit"]
		if !revenue || !profit {
			continue
		}
		_, deducted := item.Fields["deducted_net_profit"]
		result = append(result, foundation.StockFundamentals{Symbol: symbol, ReportDate: item.ReportDate, PublishedAt: item.PublishedAt, Revenue: item.Fields["revenue"], RevenueYearOverYear: item.Fields["revenue_yoy"], NetProfit: item.Fields["net_profit"], NetProfitYearOverYear: item.Fields["net_profit_yoy"], DeductedNetProfit: item.Fields["deducted_net_profit"], DeductedNetProfitYearOverYear: item.Fields["deducted_net_profit_yoy"], DeductedNetProfitAvailable: deducted, EPS: item.Fields["eps"], ROE: item.Fields["roe"], GrossMargin: item.Fields["gross_margin"], DebtRatio: item.Fields["debt_ratio"], Meta: item.Meta})
	}
	return result
}

func financialCrossChecks(primary []foundation.StockFundamentals, secondary []foundation.StockFinancialEvidence) []map[string]any {
	checks := []map[string]any{}
	for _, a := range primary {
		date := strings.Split(a.ReportDate, " ")[0]
		for _, b := range secondary {
			if date != b.ReportDate || a.Meta.Source == b.Meta.Source {
				continue
			}
			fields := map[string]float64{"revenue": a.Revenue, "net_profit": a.NetProfit}
			if a.DeductedNetProfitAvailable {
				fields["deducted_net_profit"] = a.DeductedNetProfit
			}
			same, conflicts := []string{}, []string{}
			for _, key := range []string{"revenue", "net_profit", "deducted_net_profit"} {
				av, available := fields[key]
				bv, other := b.Fields[key]
				if !available || !other {
					continue
				}
				if math.Abs(av-bv) <= math.Max(1, math.Max(math.Abs(av), math.Abs(bv))*.001) {
					same = append(same, key)
				} else {
					conflicts = append(conflicts, key)
				}
			}
			checks = append(checks, map[string]any{"report_date": date, "primary_source": a.Meta.Source, "secondary_source": b.Meta.Source, "matched_fields": same, "conflicting_fields": conflicts, "tolerance": "同报告期累计值，金额为元；容许0.1%舍入差异，冲突保留两源值，不平均或覆盖"})
		}
	}
	return checks
}
