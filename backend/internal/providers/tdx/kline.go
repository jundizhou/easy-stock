package tdx

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"github.com/bensema/gotdx"
	"github.com/bensema/gotdx/proto"
	"github.com/bensema/gotdx/types"
)

var periods = map[string]uint16{
	"": types.KLINE_TYPE_DAILY, "day": types.KLINE_TYPE_DAILY, "daily": types.KLINE_TYPE_DAILY, "101": types.KLINE_TYPE_DAILY,
	"week": types.KLINE_TYPE_WEEKLY, "weekly": types.KLINE_TYPE_WEEKLY, "102": types.KLINE_TYPE_WEEKLY,
	"month": types.KLINE_TYPE_MONTHLY, "monthly": types.KLINE_TYPE_MONTHLY, "103": types.KLINE_TYPE_MONTHLY,
	"1": types.KLINE_TYPE_1MIN, "5": types.KLINE_TYPE_5MIN, "15": types.KLINE_TYPE_15MIN, "30": types.KLINE_TYPE_30MIN, "60": types.KLINE_TYPE_1HOUR,
}

func (c *Client) SupportsKLine(symbol, period string) bool {
	n, err := foundation.NormalizeSymbol(symbol)
	_, ok := periods[strings.ToLower(strings.TrimSpace(period))]
	return err == nil && (n.Market == "SH" || n.Market == "SZ") && ok
}

func (c *Client) KLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	if !c.SupportsKLine(symbol, period) {
		return nil, fmt.Errorf("通达信不支持该市场或周期")
	}
	n, _ := foundation.NormalizeSymbol(symbol)
	market := uint16(0)
	if n.Market == "SH" {
		market = 1
	}
	return c.bars(ctx, market, n.RawCode, n.Canonical, period, limit, types.AdjustQFQ)
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func (c *Client) bars(ctx context.Context, market uint16, code, symbol, period string, limit int, adjust uint16) ([]foundation.KLine, error) {
	kind, ok := periods[strings.ToLower(strings.TrimSpace(period))]
	if !ok {
		return nil, fmt.Errorf("通达信不支持周期 %q", period)
	}
	if limit <= 0 {
		limit = 120
	}
	started := time.Now()
	var lines []foundation.KLine
	for offset := 0; offset < limit; {
		count := min(limit-offset, int(gotdx.DefaultMACSymbolBarsPage))
		req := proto.MACSymbolBarsRequest{Market: market, Period: kind, Count: uint16(count), Start: uint32(offset), Adjust: adjust}
		copy(req.Code[:], code)
		messageMu.Lock()
		msg := proto.NewMACSymbolBars(&req)
		messageMu.Unlock()
		if err := c.request(ctx, proto.KMSG_MACSYMBOLBARS, msg); err != nil {
			return nil, err
		}
		reply := msg.Response()
		if len(reply.List) == 0 {
			break
		}
		if reply.Market != market || reply.Code != code || uint16(reply.Period) != kind || len(reply.List) > count || reply.VolUnit <= 0 || !finite(reply.VolUnit) {
			return nil, errors.New("通达信 K 线字段无效")
		}
		for _, b := range reply.List {
			if b.DateTime.Year() < 1990 || !(b.Open > 0 && b.Close > 0 && b.High >= b.Low && b.Low > 0) || !finite(b.High+b.Low+b.Open+b.Close+b.Vol+b.Amount+b.PreClose) || b.Vol < 0 || b.Amount < 0 {
				return nil, errors.New("通达信 K 线数据无效")
			}
			// The SDK decodes exchange wall time in time.Local, including for users outside China.
			t := b.DateTime
			line := foundation.KLine{Symbol: symbol, Time: time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, foundation.AStockLocation),
				Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, PreviousClose: b.PreClose, Volume: b.Vol / reply.VolUnit, Amount: b.Amount}
			if b.PreClose > 0 {
				line.ChangePercent = (b.Close - b.PreClose) / b.PreClose * 100
			}
			// Index bars use the float-shares field for other data.
			if reply.Category == 0 && b.FloatShares > 0 && finite(b.FloatShares) {
				line.TurnoverRate = b.Vol / (b.FloatShares * 10000) * 100
			}
			lines = append(lines, line)
		}
		if len(reply.List) < count {
			break
		}
		offset += count
	}
	if len(lines) == 0 {
		return nil, errors.New("通达信未返回 K 线")
	}
	slices.SortFunc(lines, func(a, b foundation.KLine) int { return a.Time.Compare(b.Time) })
	meta := foundation.SourceMeta{Source: "tdx:kline", FetchedAt: time.Now(), LatencyMS: time.Since(started).Milliseconds()}
	for i := range lines {
		lines[i].Meta = meta
	}
	return lines, nil
}
