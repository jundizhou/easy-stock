package eastmoney

import (
	"context"
	"fmt"
	"net/url"
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
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return foundation.StockFundamentals{}, err
	}
	endpoint := c.f10BaseURL + "/api/data/v1/get"
	params := url.Values{}
	params.Set("reportName", "RPT_F10_FINANCE_MAINFINADATA")
	params.Set("columns", "SECUCODE,REPORT_DATE,REPORT_DATE_NAME,TOTALOPERATEREVE,TOTALOPERATEREVETZ,PARENTNETPROFIT,PARENTNETPROFITTZ,EPSJB,ROEJQ,XSMLL,ZCFZL,MGJYXJJE")
	params.Set("filter", fmt.Sprintf("(SECUCODE=\"%s\")", escapeEastMoneyFilter(normalized.Canonical)))
	params.Set("pageNumber", "1")
	params.Set("pageSize", "1")
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
				Symbol                    string        `json:"SECUCODE"`
				ReportDate                string        `json:"REPORT_DATE"`
				ReportName                string        `json:"REPORT_DATE_NAME"`
				Revenue                   flexibleFloat `json:"TOTALOPERATEREVE"`
				RevenueYearOverYear       flexibleFloat `json:"TOTALOPERATEREVETZ"`
				NetProfit                 flexibleFloat `json:"PARENTNETPROFIT"`
				NetProfitYearOverYear     flexibleFloat `json:"PARENTNETPROFITTZ"`
				EPS                       flexibleFloat `json:"EPSJB"`
				ROE                       flexibleFloat `json:"ROEJQ"`
				GrossMargin               flexibleFloat `json:"XSMLL"`
				DebtRatio                 flexibleFloat `json:"ZCFZL"`
				OperatingCashFlowPerShare flexibleFloat `json:"MGJYXJJE"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return foundation.StockFundamentals{}, fmt.Errorf("eastmoney stock fundamentals: %w", err)
	}
	if !payload.Success {
		return foundation.StockFundamentals{}, fmt.Errorf("eastmoney stock fundamentals: %s", payload.Message)
	}
	if payload.Result == nil || len(payload.Result.Data) == 0 {
		return foundation.StockFundamentals{}, fmt.Errorf("eastmoney stock fundamentals returned no data for %s", normalized.Canonical)
	}
	raw := payload.Result.Data[0]
	return foundation.StockFundamentals{
		Symbol: normalized.Canonical, ReportDate: strings.TrimSpace(raw.ReportDate), ReportName: strings.TrimSpace(raw.ReportName),
		Revenue: float64(raw.Revenue), RevenueYearOverYear: float64(raw.RevenueYearOverYear),
		NetProfit: float64(raw.NetProfit), NetProfitYearOverYear: float64(raw.NetProfitYearOverYear), EPS: float64(raw.EPS),
		ROE: float64(raw.ROE), GrossMargin: float64(raw.GrossMargin), DebtRatio: float64(raw.DebtRatio),
		OperatingCashFlowPerShare: float64(raw.OperatingCashFlowPerShare),
		Meta:                      foundation.SourceMeta{Source: "eastmoney:f10-financials", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()},
	}, nil
}

func normalizeBusinessText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}

func extractMainBusiness(profile string) string {
	profile = normalizeBusinessText(profile)
	for _, prefix := range []string{"主要从事于", "主要从事", "主营业务为", "主营业务是", "主营"} {
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
