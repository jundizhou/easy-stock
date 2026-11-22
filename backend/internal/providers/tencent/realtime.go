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

// 腾讯报价时间列的两种布局：A 股紧凑 14 位，港股带斜杠分隔。
const (
	aStockQuoteTimeLayout   = "20060102150405"
	hongKongQuoteTimeLayout = "2006/01/02 15:04:05"
)

// hongKongQuoteLocation 港股交易时钟（UTC+8，与 A 股同一时区但独立命名）。
var hongKongQuoteLocation = time.FixedZone("Asia/Hong_Kong", 8*3600)

// Realtime supplies a second quote source when the primary feed is unavailable.
func (c *Client) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("symbols is required")
	}
	keys := make([]string, 0, len(symbols))
	canonical := make(map[string]string, len(symbols))
	markets := make(map[string]string, len(symbols))
	for _, symbol := range symbols {
		normalized, err := foundation.NormalizeSymbol(symbol)
		if err != nil {
			return nil, err
		}
		if _, exists := canonical[normalized.Sina]; !exists {
			keys = append(keys, normalized.Sina)
			canonical[normalized.Sina] = normalized.Canonical
			markets[normalized.Sina] = normalized.Market
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
			// 港股与 A 股共用同一套报价接口，但时间列格式不同：
			// A 股是 20261120161447，港股是 2026/11/20 16:08:37。
			// 用 A 股布局解析港股会让整只标的被当成无效行丢弃。
			layout := aStockQuoteTimeLayout
			location := foundation.AStockLocation
			if markets[key] == "HK" {
				layout = hongKongQuoteTimeLayout
				location = hongKongQuoteLocation
			}
			tradeTime, err := time.ParseInLocation(layout, fieldAt(fields, 30), location)
			if err != nil || parseFloat(fieldAt(fields, 3)) <= 0 {
				continue
			}
			quote := foundation.Quote{
				Symbol: canonical[key], Name: fieldAt(fields, 1), Price: parseFloat(fieldAt(fields, 3)),
				PreviousClose: parseFloat(fieldAt(fields, 4)), Open: parseFloat(fieldAt(fields, 5)),
				High: parseFloat(fieldAt(fields, 33)), Low: parseFloat(fieldAt(fields, 34)),
				Change: parseFloat(fieldAt(fields, 31)), ChangePercent: parseFloat(fieldAt(fields, 32)),
				TradeTime: tradeTime, Meta: meta,
			}
			// 港股第 36/37 列是成交量（股）与成交额（港元），A 股这两列口径不同，
			// 只在港股补，避免把 A 股的手数当成股数写进同一个字段。
			if markets[key] == "HK" && len(fields) > 37 {
				quote.Currency = fieldAt(fields, 75)
				if lot, err := strconv.Atoi(fieldAt(fields, 60)); err == nil && lot > 0 {
					quote.BoardLot = lot
				}
				switch fieldAt(fields, 63) {
				case "GP":
					quote.InstrumentType = "stock"
				case "GP-FUND":
					quote.InstrumentType = "etf"
				}
				quote.Volume = parseFloat(fieldAt(fields, 36))
				quote.Amount = parseFloat(fieldAt(fields, 37))
			}
			quotes = append(quotes, quote)
		}
	}
	if len(quotes) == 0 {
		return nil, fmt.Errorf("tencent returned no quotes")
	}
	return quotes, nil
}
