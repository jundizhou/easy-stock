package eastmoney

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// suggestQuoteID 匹配 A 股的 QuoteID：0.000001（深）与 1.600000（沪）。
var suggestQuoteID = regexp.MustCompile(`^([01])\.(\d{6})$`)

// suggestHKQuoteID 匹配港股 QuoteID：116.00700（5 位代码，市场号固定 116）。
// 东财建议接口原生返回港股，SecurityType=19 或 6（港股通/港股），无需目录接口。
var suggestHKQuoteID = regexp.MustCompile(`^116\.(\d{5})$`)

// SymbolHit 是一次股票搜索的命中项：建议接口天然支持名称、代码、
// 拼音全拼与拼音首字母（如 hjsj → 华胜天成），无需本地拼音表。
type SymbolHit struct {
	InstrumentType string `json:"instrument_type,omitempty"`
	Symbol         string `json:"symbol"`
	Code           string `json:"code"`
	Name           string `json:"name"`
}

// StockSuggest 用东财建议接口查找 A 股与港股。query 可以是名称、代码或拼音；
// 返回带后缀的规范代码（600519.SH / 000858.SZ），命中为空不算错误。
func (c *Client) StockSuggest(ctx context.Context, query string, count int) ([]SymbolHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if count <= 0 || count > 20 {
		count = 10
	}
	base := c.indexSearchBaseURL
	if base == "" {
		base = "https://searchapi.eastmoney.com"
	}
	address := base + "/api/suggest/get?" + url.Values{
		"input": {query}, "type": {"14"}, "count": {strconv.Itoa(count)},
	}.Encode()
	var payload struct {
		Table struct {
			Status int `json:"Status"`
			Data   []struct {
				Code             string `json:"Code"`
				Name             string `json:"Name"`
				QuoteID          string `json:"QuoteID"`
				SecurityTypeName string `json:"SecurityTypeName"`
				SecurityType     string `json:"SecurityType"`
			} `json:"Data"`
		} `json:"QuotationCodeTable"`
	}
	if err := c.getJSONWithRetry(ctx, address, &payload); err != nil {
		return nil, fmt.Errorf("股票搜索不可用: %w", err)
	}
	hits := make([]SymbolHit, 0, count)
	seen := map[string]bool{}
	for _, item := range payload.Table.Data {
		var symbol, code string
		if hk := suggestHKQuoteID.FindStringSubmatch(item.QuoteID); hk != nil {
			// 港股：116.00700 → 00700.HK。接口同时会返回同名美股（153.）
			// 和 A 股近似代码，靠 QuoteID 市场号精确区分，只认 116.。
			code = hk[1]
			symbol = code + ".HK"
		} else if match := suggestQuoteID.FindStringSubmatch(item.QuoteID); match != nil {
			code = match[2]
			symbol = code
			if match[1] == "1" {
				symbol += ".SH"
			} else {
				symbol += ".SZ"
			}
		} else {
			continue
		}
		if seen[symbol] {
			continue
		}
		seen[symbol] = true
		hits = append(hits, SymbolHit{Symbol: symbol, Code: code, Name: strings.TrimSpace(item.Name)})
		if len(hits) >= count {
			break
		}
	}
	return hits, nil
}
