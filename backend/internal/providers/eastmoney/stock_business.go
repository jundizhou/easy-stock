package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/foundation"
)

// StockBusinessProfile reads the company's F10 profile. Main business is
// deliberately independent from CONCEPT because concept membership often
// contains broad regional or policy labels that are not the current trade.
func (c *Client) StockBusinessProfile(ctx context.Context, symbol string) (foundation.StockBusinessProfile, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return foundation.StockBusinessProfile{}, err
	}
	endpoint := c.f10BaseURL + "/api/data/v1/get"
	params := url.Values{}
	params.Set("reportName", "RPT_F10_ORG_BASICINFO")
	params.Set("columns", "SECUCODE,SECURITY_NAME_ABBR,EM2016,ORG_PROFIE,BUSINESS_SCOPE")
	params.Set("filter", fmt.Sprintf("(SECUCODE=\"%s\")", escapeEastMoneyFilter(normalized.Canonical)))
	params.Set("pageNumber", "1")
	params.Set("pageSize", "1")
	params.Set("source", "HSF10")
	params.Set("client", "PC")
	requestURL := endpoint + "?" + params.Encode()
	start := time.Now()
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Result  *struct {
			Data []struct {
				Symbol        string `json:"SECUCODE"`
				Name          string `json:"SECURITY_NAME_ABBR"`
				IndustryPath  string `json:"EM2016"`
				Profile       string `json:"ORG_PROFIE"`
				BusinessScope string `json:"BUSINESS_SCOPE"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return foundation.StockBusinessProfile{}, fmt.Errorf("eastmoney stock business: %w", err)
	}
	if !payload.Success {
		return foundation.StockBusinessProfile{}, fmt.Errorf("eastmoney stock business: %s", payload.Message)
	}
	if payload.Result == nil || len(payload.Result.Data) == 0 {
		return foundation.StockBusinessProfile{}, fmt.Errorf("eastmoney stock business returned no profile for %s", normalized.Canonical)
	}
	raw := payload.Result.Data[0]
	industryPath := strings.TrimSpace(raw.IndustryPath)
	industry := lastBusinessSegment(industryPath)
	description := normalizeBusinessText(raw.Profile)
	if description == "" {
		description = normalizeBusinessText(raw.BusinessScope)
	}
	mainBusiness := extractMainBusiness(description)
	if mainBusiness == "" {
		mainBusiness = industry
	}
	return foundation.StockBusinessProfile{
		Symbol: normalized.Canonical, Name: strings.TrimSpace(raw.Name), MainBusiness: mainBusiness,
		Industry: industry, IndustryPath: industryPath, Description: description,
		Meta: foundation.SourceMeta{Source: "eastmoney:f10-business", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()},
	}, nil
}

// StockFundamentals returns the most recent published main financial metrics.
func (c *Client) StockFundamentals(ctx context.Context, symbol string) (foundation.StockFundamentals, error) {
	items, err := c.StockFinancialHistory(ctx, symbol, 1)
	if err != nil {
		return foundation.StockFundamentals{}, err
	}
	return items[0], nil
}

// StockFinancialHistory returns bounded, newest-first cumulative disclosures.
func (c *Client) StockFinancialHistory(ctx context.Context, symbol string, limit int) ([]foundation.StockFundamentals, error) {
	limit = max(1, min(limit, 8))
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	endpoint := c.f10BaseURL + "/api/data/v1/get"
	params := url.Values{}
	params.Set("reportName", "RPT_F10_FINANCE_MAINFINADATA")
	params.Set("columns", "SECUCODE,REPORT_DATE,NOTICE_DATE,REPORT_DATE_NAME,TOTALOPERATEREVE,TOTALOPERATEREVETZ,PARENTNETPROFIT,PARENTNETPROFITTZ,KCFJCXSYJLR,KCFJCXSYJLRTZ,EPSJB,ROEJQ,XSMLL,ZCFZL,MGJYXJJE")
	params.Set("filter", fmt.Sprintf("(SECUCODE=\"%s\")", escapeEastMoneyFilter(normalized.Canonical)))
	params.Set("pageNumber", "1")
	params.Set("pageSize", strconv.Itoa(limit))
	params.Set("sortTypes", "-1")
	params.Set("sortColumns", "REPORT_DATE")
	params.Set("source", "HSF10")
	params.Set("client", "PC")
	requestURL := endpoint + "?" + params.Encode()
	start := time.Now()
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Result  *struct {
			Data []struct {
				Symbol                    string          `json:"SECUCODE"`
				ReportDate                string          `json:"REPORT_DATE"`
				NoticeDate                string          `json:"NOTICE_DATE"`
				ReportName                string          `json:"REPORT_DATE_NAME"`
				Revenue                   financialNumber `json:"TOTALOPERATEREVE"`
				RevenueYearOverYear       financialNumber `json:"TOTALOPERATEREVETZ"`
				NetProfit                 financialNumber `json:"PARENTNETPROFIT"`
				NetProfitYearOverYear     financialNumber `json:"PARENTNETPROFITTZ"`
				DeductedNetProfit         financialNumber `json:"KCFJCXSYJLR"`
				DeductedNetProfitYoY      financialNumber `json:"KCFJCXSYJLRTZ"`
				EPS                       financialNumber `json:"EPSJB"`
				ROE                       financialNumber `json:"ROEJQ"`
				GrossMargin               financialNumber `json:"XSMLL"`
				DebtRatio                 financialNumber `json:"ZCFZL"`
				OperatingCashFlowPerShare financialNumber `json:"MGJYXJJE"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, fmt.Errorf("eastmoney stock fundamentals: %w", err)
	}
	if !payload.Success {
		return nil, fmt.Errorf("eastmoney stock fundamentals: %s", payload.Message)
	}
	if payload.Result == nil || len(payload.Result.Data) == 0 {
		return nil, fmt.Errorf("eastmoney stock fundamentals returned no data for %s", normalized.Canonical)
	}
	items := make([]foundation.StockFundamentals, 0, min(limit, len(payload.Result.Data)))
	for _, raw := range payload.Result.Data {
		if len(items) >= limit {
			break
		}
		deductedAvailable := raw.DeductedNetProfit.Available
		deductedNetProfit := 0.0
		deductedNetProfitYearOverYear := 0.0
		deductedReportDate := ""
		if raw.DeductedNetProfit.Available {
			deductedNetProfit = raw.DeductedNetProfit.Value
			deductedReportDate = strings.TrimSpace(raw.ReportDate)
		}
		if raw.DeductedNetProfitYoY.Available {
			deductedNetProfitYearOverYear = raw.DeductedNetProfitYoY.Value
		}
		fields := []string{}
		for name, number := range map[string]financialNumber{
			"revenue": raw.Revenue, "revenue_yoy": raw.RevenueYearOverYear, "net_profit": raw.NetProfit, "net_profit_yoy": raw.NetProfitYearOverYear,
			"deducted_net_profit": raw.DeductedNetProfit, "deducted_net_profit_yoy": raw.DeductedNetProfitYoY, "eps": raw.EPS, "roe": raw.ROE,
			"gross_margin": raw.GrossMargin, "debt_ratio": raw.DebtRatio, "operating_cash_flow_per_share": raw.OperatingCashFlowPerShare,
		} {
			if number.Available {
				fields = append(fields, name)
			}
		}
		sort.Strings(fields)
		items = append(items, foundation.StockFundamentals{
			PublishedAt: parseEastMoneyTime(raw.NoticeDate),
			Symbol:      normalized.Canonical, ReportDate: strings.TrimSpace(raw.ReportDate), ReportName: strings.TrimSpace(raw.ReportName),
			Revenue: raw.Revenue.Value, RevenueYearOverYear: raw.RevenueYearOverYear.Value,
			NetProfit: raw.NetProfit.Value, NetProfitYearOverYear: raw.NetProfitYearOverYear.Value,
			DeductedNetProfit: deductedNetProfit, DeductedNetProfitYearOverYear: deductedNetProfitYearOverYear,
			DeductedNetProfitAvailable: deductedAvailable, DeductedNetProfitReportDate: deductedReportDate, EPS: raw.EPS.Value,
			ROE: raw.ROE.Value, GrossMargin: raw.GrossMargin.Value, DebtRatio: raw.DebtRatio.Value,
			OperatingCashFlowPerShare: raw.OperatingCashFlowPerShare.Value,
			Meta:                      foundation.SourceMeta{Source: "eastmoney:f10-financials", AvailableFields: fields, SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()},
		})
	}
	return items, nil
}

func normalizeBusinessText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}

func extractMainBusiness(profile string) string {
	profile = normalizeBusinessText(profile)
	for _, prefix := range []string{"核心业务主要是", "核心业务为", "主要从事于", "主要从事", "主营业务为", "主营业务是", "主营"} {
		start := strings.Index(profile, prefix)
		if start < 0 {
			continue
		}
		value := profile[start+len(prefix):]
		if end := strings.IndexAny(value, "，,。；;"); end >= 0 {
			value = value[:end]
		}
		for _, marker := range []string{"的设计研发", "的设计", "的研发", "的研究", "的生产", "的制造", "的开发", "的运营", "的销售", "的服务"} {
			if end := strings.Index(value, marker); end > 0 {
				value = value[:end]
				break
			}
		}
		value = strings.Trim(value, "：:、 ")
		if utf8.RuneCountInString(value) >= 2 && utf8.RuneCountInString(value) <= 30 {
			return value
		}
	}
	return ""
}

func lastBusinessSegment(value string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool { return r == '-' || r == '—' || r == '>' || r == '/' })
	for index := len(parts) - 1; index >= 0; index-- {
		if part := strings.TrimSpace(parts[index]); part != "" {
			return part
		}
	}
	return ""
}

// Preserve the difference between explicitly reported zero growth and a
// missing/placeholder field. Value's zero default alone must not prove stability.
type financialNumber struct {
	Value     float64
	Available bool
}

func (n *financialNumber) UnmarshalJSON(data []byte) error {
	*n = financialNumber{}
	raw := strings.TrimSpace(string(data))
	if strings.HasPrefix(raw, "\"") {
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		raw = strings.TrimSpace(raw)
	}
	if raw == "" || raw == "null" || raw == "-" || raw == "--" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return err
	}
	if !math.IsNaN(value) && !math.IsInf(value, 0) {
		n.Value = value
		n.Available = true
	}
	return nil
}
