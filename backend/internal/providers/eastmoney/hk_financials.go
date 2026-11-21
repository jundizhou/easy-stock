package eastmoney

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// hkFinancialHistoryColumns 是港股 F10 主指标表返回的列快照：
// OPERATE_INCOME 营业额、HOLDER_PROFIT 股东应占溢利、GROSS_PROFIT_RATIO 毛利率、
// DEBT_ASSET_RATIO 资产负债率、ROE_AVG 平均净资产收益率、PER_NETCASH_OPERATE 每股经营现金流。
// 港股披露口径与 A 股一致（年初至今累计），但没有「扣非净利润」，也不带公告日期。
const hkFinancialHistoryColumns = "SECUCODE,SECURITY_NAME_ABBR,REPORT_DATE,REPORT_TYPE,DATE_TYPE_CODE,OPERATE_INCOME,OPERATE_INCOME_YOY,HOLDER_PROFIT,HOLDER_PROFIT_YOY,BASIC_EPS,ROE_AVG,GROSS_PROFIT_RATIO,DEBT_ASSET_RATIO,PER_NETCASH_OPERATE,CURRENCY,IS_CNY_CODE"

// HongKongFinancialHistory 返回港股最近若干期主要财务指标（新到旧）。
//
// A 股的 RPT_F10_FINANCE_MAINFINADATA 不覆盖港股（SECUCODE=00700.HK 查不到），
// 港股要走 RPT_HKF10_FN_MAININDICATOR。两者报告期口径相同，字段名不同，
// 这里统一映射到 foundation.StockFundamentals，并把计价货币一并带出。
func (c *Client) HongKongFinancialHistory(ctx context.Context, symbol string, limit int) ([]foundation.StockFundamentals, error) {
	limit = max(1, min(limit, 8))
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if normalized.Market != "HK" {
		return nil, fmt.Errorf("eastmoney hk financials: %s is not a hong kong symbol", symbol)
	}
	params := url.Values{}
	params.Set("reportName", "RPT_HKF10_FN_MAININDICATOR")
	params.Set("columns", hkFinancialHistoryColumns)
	params.Set("filter", fmt.Sprintf("(SECUCODE=\"%s\")", escapeEastMoneyFilter(normalized.Canonical)))
	params.Set("pageNumber", "1")
	params.Set("pageSize", strconv.Itoa(limit))
	params.Set("sortTypes", "-1")
	params.Set("sortColumns", "REPORT_DATE")
	params.Set("source", "HSF10")
	params.Set("client", "PC")
	requestURL := c.f10BaseURL + "/api/data/v1/get?" + params.Encode()

	start := time.Now()
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Result  *struct {
			Data []struct {
				Symbol        string          `json:"SECUCODE"`
				Name          string          `json:"SECURITY_NAME_ABBR"`
				ReportDate    string          `json:"REPORT_DATE"`
				ReportName    string          `json:"REPORT_TYPE"`
				Revenue       financialNumber `json:"OPERATE_INCOME"`
				RevenueYoY    financialNumber `json:"OPERATE_INCOME_YOY"`
				NetProfit     financialNumber `json:"HOLDER_PROFIT"`
				NetProfitYoY  financialNumber `json:"HOLDER_PROFIT_YOY"`
				EPS           financialNumber `json:"BASIC_EPS"`
				ROE           financialNumber `json:"ROE_AVG"`
				GrossMargin   financialNumber `json:"GROSS_PROFIT_RATIO"`
				DebtRatio     financialNumber `json:"DEBT_ASSET_RATIO"`
				CashFlowPerSH financialNumber `json:"PER_NETCASH_OPERATE"`
				Currency      string          `json:"CURRENCY"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, fmt.Errorf("eastmoney hk financials: %w", err)
	}
	if !payload.Success {
		return nil, fmt.Errorf("eastmoney hk financials: %s", payload.Message)
	}
	if payload.Result == nil || len(payload.Result.Data) == 0 {
		return nil, fmt.Errorf("eastmoney hk financials returned no data for %s", normalized.Canonical)
	}

	items := make([]foundation.StockFundamentals, 0, min(limit, len(payload.Result.Data)))
	seen := map[string]bool{}
	for _, raw := range payload.Result.Data {
		if len(items) >= limit {
			break
		}
		reportDate := strings.TrimSpace(raw.ReportDate)
		if cut, _, ok := strings.Cut(reportDate, " "); ok {
			reportDate = cut
		}
		if reportDate == "" || seen[reportDate] {
			continue
		}
		seen[reportDate] = true
		reportName := strings.TrimSpace(raw.ReportName)
		if reportName == "" {
			reportName = hkReportName(reportDate)
		}
		fields := []string{}
		for name, number := range map[string]financialNumber{
			"revenue": raw.Revenue, "revenue_yoy": raw.RevenueYoY, "net_profit": raw.NetProfit, "net_profit_yoy": raw.NetProfitYoY,
			"eps": raw.EPS, "roe": raw.ROE, "gross_margin": raw.GrossMargin, "debt_ratio": raw.DebtRatio,
			"operating_cash_flow_per_share": raw.CashFlowPerSH,
		} {
			if number.Available {
				fields = append(fields, name)
			}
		}
		sort.Strings(fields)
		items = append(items, foundation.StockFundamentals{
			Symbol: normalized.Canonical, ReportDate: reportDate, ReportName: reportName,
			Revenue: raw.Revenue.Value, RevenueYearOverYear: raw.RevenueYoY.Value,
			NetProfit: raw.NetProfit.Value, NetProfitYearOverYear: raw.NetProfitYoY.Value,
			// 港股没有扣非净利润口径，显式标记不可用，避免下游把它当成 0。
			DeductedNetProfitAvailable: false,
			EPS:                        raw.EPS.Value, ROE: raw.ROE.Value,
			GrossMargin: raw.GrossMargin.Value, DebtRatio: raw.DebtRatio.Value,
			OperatingCashFlowPerShare: raw.CashFlowPerSH.Value,
			Currency:                  strings.ToUpper(strings.TrimSpace(raw.Currency)),
			Meta: foundation.SourceMeta{
				Source: "eastmoney:hkf10-financials", AvailableFields: fields,
				SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds(),
			},
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("eastmoney hk financials returned no usable report for %s", normalized.Canonical)
	}
	return items, nil
}

// hkReportName 在上游没给中文报表名时按月份推断，如 2026-09-30 → 2026年三季报。
func hkReportName(reportDate string) string {
	if len(reportDate) < 10 {
		return reportDate
	}
	year := reportDate[:4]
	switch reportDate[5:10] {
	case "03-31":
		return year + "年一季报"
	case "06-30":
		return year + "年中报"
	case "09-30":
		return year + "年三季报"
	case "12-31":
		return year + "年年报"
	default:
		return reportDate
	}
}
