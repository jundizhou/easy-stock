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

// SymbolHit 是一次股票搜索的命中项：建议接口天然支持名称、代码、
// 拼音全拼与拼音首字母（如 hjsj → 华胜天成），无需本地拼音表。
type SymbolHit struct {
	Symbol string `json:"symbol"`
	Code   string `json:"code"`
	Name   string `json:"name"`
}

// StockSuggest 用东财建议接口搜 A 股。query 可以是名称、6 位代码或拼音；
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
		match := suggestQuoteID.FindStringSubmatch(item.QuoteID)
		if match == nil {
			continue
		}
		symbol := match[2]
		if match[1] == "1" {
			symbol += ".SH"
		} else {
			symbol += ".SZ"
		}
		if seen[symbol] {
			continue
		}
		seen[symbol] = true
		hits = append(hits, SymbolHit{Symbol: symbol, Code: match[2], Name: strings.TrimSpace(item.Name)})
		if len(hits) >= count {
			break
		}
	}
	return hits, nil
}
