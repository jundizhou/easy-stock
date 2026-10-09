package tencent

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"easy-stock/backend/internal/foundation"
)

// AdjustedKLine deliberately refuses the unadjusted day array. Mixing raw and
// forward-adjusted histories would manufacture theme returns on ex-dividend days.
func (c *Client) AdjustedKLine(ctx context.Context, symbol string, limit int) ([]foundation.KLine, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	params := url.Values{"param": {normalized.Sina + ",day,,," + strconv.Itoa(limit) + ",qfq"}}
	requestURL := c.klineBaseURL + "?" + params.Encode()
	var payload struct {
		Code int `json:"code"`
		Data map[string]struct {
			QFQDay [][]any `json:"qfqday"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	rows := payload.Data[normalized.Sina].QFQDay
	if payload.Code != 0 || len(rows) == 0 {
		return nil, fmt.Errorf("tencent adjusted history unavailable")
	}
	meta := foundation.SourceMeta{Source: "tencent:qfq-kline", SourceURL: requestURL, FetchedAt: time.Now()}
	lines := make([]foundation.KLine, 0, len(rows))
	for _, row := range rows {
		if len(row) < 6 {
			return nil, fmt.Errorf("invalid tencent adjusted bar")
		}
		day, err := time.ParseInLocation("2006-01-02", anyString(row[0]), time.FixedZone("Asia/Shanghai", 8*60*60))
		if err != nil {
			return nil, err
		}
		lines = append(lines, foundation.KLine{Symbol: normalized.Canonical, Time: day, Open: parseFloat(anyString(row[1])), Close: parseFloat(anyString(row[2])), High: parseFloat(anyString(row[3])), Low: parseFloat(anyString(row[4])), Volume: parseFloat(anyString(row[5])), Meta: meta})
	}
	return lines, nil
}
