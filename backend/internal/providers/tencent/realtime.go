package tencent

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// Realtime supplies a second quote source when the primary feed is unavailable.
func (c *Client) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("symbols is required")
	}
	keys := make([]string, 0, len(symbols))
	canonical := make(map[string]string, len(symbols))
	for _, symbol := range symbols {
		normalized, err := foundation.NormalizeSymbol(symbol)
		if err != nil {
			return nil, err
		}
		if _, exists := canonical[normalized.Sina]; !exists {
			keys = append(keys, normalized.Sina)
			canonical[normalized.Sina] = normalized.Canonical
		}
	}
	quotes := make([]foundation.Quote, 0, len(keys))
	for start := 0; start < len(keys); start += 80 {
		end := min(start+80, len(keys))
		batch := keys[start:end]
		requestURL := c.quoteBaseURL + "?" + url.Values{"q": {strings.Join(batch, ",")}}.Encode()
		started := time.Now()
		body, err := c.get(ctx, requestURL)
		if err != nil {
			return nil, err
		}
		meta := foundation.SourceMeta{Source: "tencent:stock-quote", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(started).Milliseconds()}
		byKey := parseTencentQuoteLines(body)
		for _, key := range batch {
			fields := byKey[key]
			if len(fields) < 35 || fieldAt(fields, 2) != strings.Split(canonical[key], ".")[0] {
				continue
			}
			tradeTime, err := time.ParseInLocation("20060102150405", fieldAt(fields, 30), foundation.AStockLocation)
			if err != nil || parseFloat(fieldAt(fields, 3)) <= 0 {
				continue
			}
			quotes = append(quotes, foundation.Quote{
				Symbol: canonical[key], Name: fieldAt(fields, 1), Price: parseFloat(fieldAt(fields, 3)),
				PreviousClose: parseFloat(fieldAt(fields, 4)), Open: parseFloat(fieldAt(fields, 5)),
				High: parseFloat(fieldAt(fields, 33)), Low: parseFloat(fieldAt(fields, 34)),
				Change: parseFloat(fieldAt(fields, 31)), ChangePercent: parseFloat(fieldAt(fields, 32)),
				TradeTime: tradeTime, Meta: meta,
			})
		}
	}
	if len(quotes) == 0 {
		return nil, fmt.Errorf("tencent returned no quotes")
	}
	return quotes, nil
}
