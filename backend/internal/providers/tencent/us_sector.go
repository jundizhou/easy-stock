package tencent

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

type usSectorDefinition struct {
	QuoteKey string
	Symbol   string
	Name     string
}

// SPDR Select Sector ETFs provide a stable, observable proxy for the eleven
// major US equity sectors. Keeping the mapping local also makes the result
// understandable when a provider returns only the ticker and quote fields.
var usSectorCatalog = []usSectorDefinition{
	{QuoteKey: "usXLB", Symbol: "XLB", Name: "材料"},
	{QuoteKey: "usXLC", Symbol: "XLC", Name: "通信服务"},
	{QuoteKey: "usXLE", Symbol: "XLE", Name: "能源"},
	{QuoteKey: "usXLF", Symbol: "XLF", Name: "金融"},
	{QuoteKey: "usXLI", Symbol: "XLI", Name: "工业"},
	{QuoteKey: "usXLK", Symbol: "XLK", Name: "信息技术"},
	{QuoteKey: "usXLP", Symbol: "XLP", Name: "必需消费"},
	{QuoteKey: "usXLRE", Symbol: "XLRE", Name: "房地产"},
	{QuoteKey: "usXLU", Symbol: "XLU", Name: "公用事业"},
	{QuoteKey: "usXLV", Symbol: "XLV", Name: "医疗保健"},
	{QuoteKey: "usXLY", Symbol: "XLY", Name: "可选消费"},
}

// USSectorMomentum returns all available sector proxies sorted by daily
// performance. The caller can then retain both leaders and laggards without
// making a second request.
func (c *Client) USSectorMomentum(ctx context.Context, limit int) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error) {
	if limit <= 0 || limit > len(usSectorCatalog) {
		limit = len(usSectorCatalog)
	}
	keys := make([]string, 0, len(usSectorCatalog))
	for _, definition := range usSectorCatalog {
		keys = append(keys, definition.QuoteKey)
	}
	values := url.Values{}
	values.Set("q", strings.Join(keys, ","))
	requestURL := c.quoteBaseURL + "?" + values.Encode()
	start := time.Now()
	body, err := c.get(ctx, requestURL)
	if err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	meta := foundation.SourceMeta{
		Source:          "tencent:us-sector-etf",
		SourceURL:       requestURL,
		AvailableFields: []string{"proxy_symbol", "name", "price", "change_percent", "trade_time"},
		FetchedAt:       time.Now(),
		LatencyMS:       time.Since(start).Milliseconds(),
	}
	quotes := parseTencentQuoteLines(body)
	items := make([]foundation.MarketUSSectorMomentum, 0, len(usSectorCatalog))
	for _, definition := range usSectorCatalog {
		fields := quotes[definition.QuoteKey]
		if len(fields) < 33 {
			continue
		}
		items = append(items, foundation.MarketUSSectorMomentum{
			ProxySymbol:   definition.Symbol,
			Name:          definition.Name,
			Price:         parseFloat(fieldAt(fields, 3)),
			ChangePercent: parseFloat(fieldAt(fields, 32)),
			TradeTime:     parseTencentTradeTime(fieldAt(fields, 30)),
			Meta:          meta,
		})
	}
	if len(items) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("tencent returned no US sector ETF quotes")
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].ChangePercent > items[j].ChangePercent
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, meta, nil
}
