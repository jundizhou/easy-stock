package tencent

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// The full A-share quote uses field 39 for trailing PE, 46 for PB.
// Fields 52/53 are dynamic/static PE and must not replace trailing earnings.
func (c *Client) StockValuations(ctx context.Context, symbols []string) ([]foundation.StockValuation, error) {
	keys, canonical := []string{}, map[string]string{}
	for _, symbol := range symbols {
		n, err := foundation.NormalizeSymbol(symbol)
		if err != nil {
			return nil, err
		}
		if _, ok := canonical[n.Sina]; !ok {
			keys = append(keys, n.Sina)
			canonical[n.Sina] = n.Canonical
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}
	if len(keys) > 80 {
		return nil, fmt.Errorf("valuation request exceeds 80 symbols")
	}
	requestURL := c.quoteBaseURL + "?" + url.Values{"q": {strings.Join(keys, ",")}}.Encode()
	start := time.Now()
	body, err := c.get(ctx, requestURL)
	if err != nil {
		return nil, err
	}
	meta := foundation.SourceMeta{Source: "tencent:stock-valuation", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	byKey := parseTencentQuoteLines(body)
	out := []foundation.StockValuation{}
	for _, key := range keys {
		f := byKey[key]
		if len(f) < 47 || fieldAt(f, 2) != strings.Split(canonical[key], ".")[0] {
			continue
		}
		at, err := time.ParseInLocation("20060102150405", fieldAt(f, 30), foundation.AStockLocation)
		if err != nil {
			continue
		}
		v := foundation.StockValuation{Symbol: canonical[key], TradeTime: at, Meta: meta}
		for field, target := range map[int]**float64{39: &v.PETTM, 46: &v.PB} {
			n, err := strconv.ParseFloat(strings.TrimSpace(fieldAt(f, field)), 64)
			if err == nil && foundation.PositiveMultiple(&n) {
				*target = &n
			}
		}
		if v.PETTM != nil {
			v.Meta.AvailableFields = append(v.Meta.AvailableFields, "pe_ttm")
		}
		if v.PB != nil {
			v.Meta.AvailableFields = append(v.Meta.AvailableFields, "pb")
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tencent returned no stock valuations")
	}
	return out, nil
}
