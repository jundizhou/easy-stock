package sina

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

func WithFinancialBaseURL(value string) Option {
	return func(c *Client) { c.financialBaseURL = value }
}

// The public key-indicator feed uses CNY amounts and fractional YoY changes.
// Only explicitly returned fields enter evidence; null/blank never means zero.
func (c *Client) StockFinancialEvidence(ctx context.Context, symbol string, limit int) ([]foundation.StockFinancialEvidence, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	limit = max(1, min(limit, 8))
	params := url.Values{"paperCode": {normalized.Sina}, "source": {"gjzb"}, "type": {"0"}, "page": {"1"}, "num": {strconv.Itoa(limit)}}
	requestURL := c.financialBaseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sina financials: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sina financials HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Result struct {
			Status struct {
				Code    int    `json:"code"`
				Message string `json:"msg"`
			} `json:"status"`
			Data struct {
				Reports map[string]struct {
					Type      string `json:"rType"`
					Currency  string `json:"rCurrency"`
					Published string `json:"publish_date"`
					Items     []struct {
						Field string          `json:"item_field"`
						Value json.RawMessage `json:"item_value"`
						YoY   json.RawMessage `json:"item_tongbi"`
					} `json:"data"`
				} `json:"report_list"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("sina financials decode: %w", err)
	}
	if payload.Result.Status.Code != 0 {
		return nil, fmt.Errorf("sina financials: %s", payload.Result.Status.Message)
	}
	fields := map[string]string{"BIZTOTINCO": "revenue", "PARENETP": "net_profit", "NPCUT": "deducted_net_profit", "MANANETR": "operating_cash_flow", "EPSBASIC": "eps", "ROEWEIGHTED": "roe", "SGPMARGIN": "gross_margin", "ASSLIABRT": "debt_ratio"}
	items := []foundation.StockFinancialEvidence{}
	for date, raw := range payload.Result.Data.Reports {
		day, err := time.ParseInLocation("20060102", date, foundation.AStockLocation)
		if err != nil || raw.Currency != "CNY" || !strings.Contains(raw.Type, "合并") {
			continue
		}
		published, _ := time.ParseInLocation("20060102", raw.Published, foundation.AStockLocation)
		values := map[string]float64{}
		for _, item := range raw.Items {
			field := fields[item.Field]
			if field == "" {
				continue
			}
			if value, ok := financialNumber(item.Value); ok {
				values[field] = value
			}
			if field == "revenue" || field == "net_profit" || field == "deducted_net_profit" {
				if value, ok := financialNumber(item.YoY); ok {
					values[field+"_yoy"] = value * 100
				}
			}
		}
		if len(values) == 0 {
			continue
		}
		items = append(items, foundation.StockFinancialEvidence{Symbol: normalized.Canonical, ReportDate: day.Format("2006-01-02"), PublishedAt: published, Fields: values, Meta: foundation.SourceMeta{Source: "sina:financial-indicators", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ReportDate > items[j].ReportDate })
	if len(items) == 0 {
		return nil, fmt.Errorf("sina financials returned no usable CNY consolidated reports")
	}
	return items[:min(limit, len(items))], nil
}

func financialNumber(raw json.RawMessage) (float64, bool) {
	value := strings.Trim(strings.TrimSpace(string(raw)), "\"")
	n, err := strconv.ParseFloat(value, 64)
	return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}
