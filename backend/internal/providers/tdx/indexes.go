package tdx

import (
	"context"
	"fmt"
	"time"

	"easy-stock/backend/internal/foundation"
	"github.com/bensema/gotdx"
	"github.com/bensema/gotdx/proto"
	"github.com/bensema/gotdx/types"
)

func indexSymbol(d foundation.MarketIndexDefinition) (proto.MACSymbolQuoteStock, bool) {
	var stock proto.MACSymbolQuoteStock
	switch d.Symbol.Market {
	case "SH":
		stock.Market = 1
	case "SZ":
	default:
		return stock, false
	}
	copy(stock.Code[:], d.Symbol.RawCode)
	return stock, true
}

func indexSnapshot(d foundation.MarketIndexDefinition, code string, price, prev float64, tradeTime time.Time, meta foundation.SourceMeta) foundation.MarketIndexSnapshot {
	item := foundation.MarketIndexSnapshot{ID: d.ID, Code: code, SecID: d.SecID, Name: d.Name, Region: d.Region, Market: d.Market, Currency: d.Currency, Price: price, TradeTime: tradeTime, Meta: meta, Status: "closed"}
	if age := meta.FetchedAt.Sub(tradeTime); age >= 0 && age <= 20*time.Minute {
		item.Status = "open"
	}
	if prev > 0 {
		item.Change = price - prev
		item.ChangePercent = item.Change / prev * 100
	}
	return item
}

func (c *Client) IndexSnapshots(ctx context.Context, definitions []foundation.MarketIndexDefinition) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	start := time.Now()
	var stocks []proto.MACSymbolQuoteStock
	bySymbol := make(map[proto.MACSymbolQuoteStock]foundation.MarketIndexDefinition)
	for _, d := range definitions {
		if stock, ok := indexSymbol(d); ok {
			stocks = append(stocks, stock)
			bySymbol[stock] = d
		}
	}
	if len(stocks) == 0 {
		return nil, foundation.SourceMeta{}, nil
	}
	bitmap := gotdx.MACFieldBitmap(gotdx.MACFieldPreClose, gotdx.MACFieldClose, gotdx.MACFieldServerUpdateDate, gotdx.MACFieldServerUpdateTime)
	messageMu.Lock()
	msg := proto.NewMACSymbolQuotes(&proto.MACSymbolQuotesRequest{Stocks: stocks, FieldBitmap: bitmap})
	messageMu.Unlock()
	if err := c.request(ctx, proto.KMSG_MACSYMBOLQUOTES, msg); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	meta := foundation.SourceMeta{Source: "tdx:index", FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	var items []foundation.MarketIndexSnapshot
	for _, q := range msg.Response().Stocks {
		key := proto.MACSymbolQuoteStock{Market: q.Market}
		copy(key.Code[:], q.Symbol)
		d, ok := bySymbol[key]
		price, _ := q.Values["close"].(float64)
		prev, _ := q.Values["pre_close"].(float64)
		date, _ := q.Values["server_update_date"].(uint32)
		clock, _ := q.Values["server_update_time"].(uint32)
		stamp, err := time.ParseInLocation("20060102150405", fmt.Sprintf("%08d%06d", date, clock), foundation.AStockLocation)
		if !ok || err != nil || !(price > 0 && prev > 0) || !finite(price+prev) {
			continue
		}
		items = append(items, indexSnapshot(d, q.Symbol, price, prev, stamp, meta))
	}
	return items, meta, nil
}

func (c *Client) MarketIndexSeries(ctx context.Context, id, period string, limit int) (foundation.MarketIndexSeries, error) {
	d, ok := foundation.FindMarketIndex(id)
	stock, supported := indexSymbol(d)
	if !ok || !supported {
		return foundation.MarketIndexSeries{}, fmt.Errorf("通达信不支持指数 %q", id)
	}
	code := d.Symbol.RawCode
	lines, err := c.bars(ctx, stock.Market, code, d.ID, period, limit, types.AdjustNone)
	if err != nil {
		return foundation.MarketIndexSeries{}, err
	}
	last := lines[len(lines)-1]
	return foundation.MarketIndexSeries{Index: indexSnapshot(d, code, last.Close, last.PreviousClose, last.Time, last.Meta), Lines: lines, Meta: last.Meta}, nil
}
