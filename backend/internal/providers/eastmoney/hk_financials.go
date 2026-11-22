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

// The indicator table supplies ratios. Monetary values come from the original-currency income statement.
const hkFinancialHistoryColumns = "SECUCODE,REPORT_DATE,REPORT_TYPE,ROE_AVG,GROSS_PROFIT_RATIO,DEBT_ASSET_RATIO,PER_NETCASH_OPERATE,CURRENCY"

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
				ReportDate    string          `json:"REPORT_DATE"`
				ReportName    string          `json:"REPORT_TYPE"`
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

	oldest := strings.Fields(payload.Result.Data[len(payload.Result.Data)-1].ReportDate)
	if len(oldest) == 0 {
		return nil, fmt.Errorf("港股报告缺少报告期")
	}
	statements, statementURL, err := c.hkIncomeStatements(ctx, normalized.Canonical, oldest[0])
	if err != nil {
		return nil, err
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
		if raw.Symbol != normalized.Canonical || reportDate == "" || seen[reportDate] {
			continue
		}
		statement := statements[reportDate]
		if statement == nil || statement.conflict || statement.currency == "" || !statement.revenue.Available || !statement.profit.Available {
			continue
		}
		if raw.Currency != statement.currency {
			raw.CashFlowPerSH = financialNumber{}
		}
		seen[reportDate] = true
		reportName := strings.TrimSpace(raw.ReportName)
		if reportName == "" {
			reportName = reportDate
		}
		fields := []string{}
		for name, number := range map[string]financialNumber{
			"revenue": statement.revenue, "revenue_yoy": statement.revenueYoY, "net_profit": statement.profit, "net_profit_yoy": statement.profitYoY,
			"eps": statement.eps, "roe": raw.ROE, "gross_margin": raw.GrossMargin, "debt_ratio": raw.DebtRatio,
			"operating_cash_flow_per_share": raw.CashFlowPerSH,
		} {
			if number.Available {
				fields = append(fields, name)
			}
		}
		sort.Strings(fields)
		items = append(items, foundation.StockFundamentals{
			Symbol: normalized.Canonical, ReportDate: reportDate, ReportName: reportName,
			Revenue: statement.revenue.Value, RevenueYearOverYear: statement.revenueYoY.Value,
			NetProfit: statement.profit.Value, NetProfitYearOverYear: statement.profitYoY.Value,
			// 港股没有扣非净利润口径，显式标记不可用，避免下游把它当成 0。
			DeductedNetProfitAvailable: false,
			EPS:                        statement.eps.Value, ROE: raw.ROE.Value,
			GrossMargin: raw.GrossMargin.Value, DebtRatio: raw.DebtRatio.Value,
			OperatingCashFlowPerShare: raw.CashFlowPerSH.Value,
			Currency:                  statement.currency,
			Meta: foundation.SourceMeta{
				Source: "eastmoney:hkf10-financials", AvailableFields: fields,
				SourceURL: statementURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds(),
			},
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("eastmoney hk financials returned no usable report for %s", normalized.Canonical)
	}
	return items, nil
}

// Income statement rows bind each amount to a reporting period and its original currency.
type hkIncomePeriod struct {
	currency                                    string
	conflict                                    bool
	revenue, revenueYoY, profit, profitYoY, eps financialNumber
}

func (c *Client) hkIncomeStatements(ctx context.Context, symbol, oldest string) (map[string]*hkIncomePeriod, string, error) {
	params := url.Values{
		"reportName": {"RPT_HKF10_FN_INCOME"},
		"columns":    {"SECUCODE,REPORT_DATE,ITEM_NAME,CURRENCY_CODE,AMOUNT,YOY_RATIO"},
		"filter":     {fmt.Sprintf(`(SECUCODE="%s")(REPORT_DATE>='%s')(ITEM_NAME in ("营运收入","经营收入总额","股东应占溢利","每股基本盈利"))`, symbol, escapeEastMoneyFilter(oldest))},
		"pageSize":   {"100"}, "sortColumns": {"REPORT_DATE"}, "sortTypes": {"-1"}, "source": {"HSF10"}, "client": {"PC"},
	}
	address := c.f10BaseURL + "/api/data/v1/get?" + params.Encode()
	var response struct {
		Success bool
		Result  struct {
			Data []struct {
				Symbol   string `json:"SECUCODE"`
				Date     string `json:"REPORT_DATE"`
				Name     string `json:"ITEM_NAME"`
				Currency string `json:"CURRENCY_CODE"`
				Amount   financialNumber
				YoY      financialNumber `json:"YOY_RATIO"`
			}
		}
	}
	if err := c.getJSONWithRetry(ctx, address, &response); err != nil {
		return nil, address, err
	}
	if !response.Success {
		return nil, address, fmt.Errorf("港股利润表不可用")
	}
	periods := map[string]*hkIncomePeriod{}
	for _, row := range response.Result.Data {
		date, _, _ := strings.Cut(row.Date, " ")
		if row.Symbol != symbol || date == "" || row.Currency == "" {
			continue
		}
		p := periods[date]
		if p == nil {
			p = &hkIncomePeriod{currency: row.Currency}
			periods[date] = p
		}
		if p.currency != row.Currency {
			p.conflict = true
			continue
		}
		switch row.Name {
		case "营运收入", "经营收入总额":
			p.revenue, p.revenueYoY = row.Amount, row.YoY
		case "股东应占溢利":
			p.profit, p.profitYoY = row.Amount, row.YoY
		case "每股基本盈利":
			p.eps = row.Amount
		}
	}
	return periods, address, nil
}
