package eastmoney

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

const (
	stockCatalogPageSize = 3000
	stockCatalogTTL      = 3 * time.Minute
)

const aShareMarketFilter = `(MARKET in ("上交所主板","深交所主板","深交所创业板","上交所科创板","上交所风险警示板","深交所风险警示板","北京证券交易所"))`

type conceptNames []string

type flexibleFloat float64

func (value *flexibleFloat) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*value = 0
		return nil
	}
	if data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" || raw == "-" || raw == "--" {
			*value = 0
			return nil
		}
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		*value = flexibleFloat(parsed)
		return nil
	}
	parsed, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return err
	}
	*value = flexibleFloat(parsed)
	return nil
}

func (names *conceptNames) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*names = nil
		return nil
	}
	if data[0] == '[' {
		var values []string
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		*names = values
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	if value == "" || value == "-" || value == "--" {
		*names = nil
		return nil
	}
	*names = []string{value}
	return nil
}

type stockCatalogPayload struct {
	Code    int    `json:"code"`
	Success bool   `json:"success"`
	Message string `json:"message"`
	Result  struct {
		NextPage    bool `json:"nextpage"`
		CurrentPage int  `json:"currentpage"`
		Count       int  `json:"count"`
		Data        []struct {
			Symbol        string        `json:"SECUCODE"`
			Code          string        `json:"SECURITY_CODE"`
			Name          string        `json:"SECURITY_NAME_ABBR"`
			Price         flexibleFloat `json:"NEW_PRICE"`
			ChangePercent flexibleFloat `json:"CHANGE_RATE"`
			FiveDay       flexibleFloat `json:"CHANGERATE_5DAYS"`
			Volume        flexibleFloat `json:"VOLUME"`
			Amount        flexibleFloat `json:"DEAL_AMOUNT"`
			Industry      string        `json:"INDUSTRY"`
			Concepts      conceptNames  `json:"CONCEPT"`
		} `json:"data"`
	} `json:"result"`
}

// StockCatalog returns the current A-share universe with industry, concepts,
// and quote fields. It follows go-stock's GetAllStocks data path and caches the
// two-page market snapshot so switching themes does not refetch 5,000+ rows.
func (c *Client) StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error) {
	c.catalogMu.Lock()
	defer c.catalogMu.Unlock()

	if len(c.catalog) > 0 && time.Now().Before(c.catalogUntil) {
		return cloneStockCatalog(c.catalog), nil
	}

	start := time.Now()
	entries := make([]foundation.StockCatalogEntry, 0, 6000)
	requestURLs := make([]string, 0, 2)
	for page := 1; page <= 5; page++ {
		requestURL := c.stockCatalogURL(page)
		requestURLs = append(requestURLs, requestURL)
		var payload stockCatalogPayload
		if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
			return nil, fmt.Errorf("eastmoney stock catalog page %d: %w", page, err)
		}
		if payload.Code != 0 || !payload.Success {
			return nil, fmt.Errorf("eastmoney stock catalog code=%d: %s", payload.Code, payload.Message)
		}

		for _, raw := range payload.Result.Data {
			symbol, err := normalizeEastMoneyStockCode(firstNonEmpty(raw.Symbol, raw.Code))
			if err != nil {
				continue
			}
			entries = append(entries, foundation.StockCatalogEntry{
				BoardStock: foundation.BoardStock{
					Symbol:               symbol,
					Name:                 raw.Name,
					Price:                float64(raw.Price),
					ChangePercent:        float64(raw.ChangePercent),
					FiveDayChangePercent: float64(raw.FiveDay),
					Volume:               float64(raw.Volume),
					Amount:               float64(raw.Amount),
				},
				Industry: strings.TrimSpace(raw.Industry),
				Concepts: append([]string(nil), raw.Concepts...),
			})
		}
		if !payload.Result.NextPage || len(payload.Result.Data) == 0 {
			break
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("eastmoney stock catalog returned no stocks")
	}

	meta := foundation.SourceMeta{
		Source:    "eastmoney:stock-selection",
		SourceURL: strings.Join(requestURLs, ","),
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	for i := range entries {
		entries[i].Meta = meta
	}
	c.catalog = cloneStockCatalog(entries)
	c.catalogUntil = time.Now().Add(stockCatalogTTL)
	return entries, nil
}

func (c *Client) stockCatalogURL(page int) string {
	params := url.Values{}
	params.Set("st", "CHANGE_RATE")
	params.Set("sr", "-1")
	params.Set("ps", strconv.Itoa(stockCatalogPageSize))
	params.Set("p", strconv.Itoa(page))
	params.Set("sty", "SECUCODE,SECURITY_CODE,SECURITY_NAME_ABBR,NEW_PRICE,CHANGE_RATE,CHANGERATE_5DAYS,VOLUME,DEAL_AMOUNT,MARKET,CONCEPT,INDUSTRY")
	params.Set("filter", aShareMarketFilter)
	params.Set("source", "SELECT_SECURITIES")
	params.Set("client", "WEB")
	params.Set("hyversion", "v2")
	return c.dataBaseURL + "/dataapi/xuangu/list?" + params.Encode()
}

func cloneStockCatalog(entries []foundation.StockCatalogEntry) []foundation.StockCatalogEntry {
	cloned := make([]foundation.StockCatalogEntry, len(entries))
	for i, entry := range entries {
		cloned[i] = entry
		cloned[i].Concepts = append([]string(nil), entry.Concepts...)
	}
	return cloned
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
