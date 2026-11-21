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

// HongKongKLine 拉取港股前复权 K 线。
//
// 港股不能用 A 股的 fqkline/newfqkline 接口：同一条 hk00700 在 fqkline 下只有
// 未复权的 day/week/month 数组，复权序列要走 hkfqkline（qfqday/qfqweek/qfqmonth）。
// 东财 push2his 与新浪 CN_MarketDataService 都不覆盖港股，因此单独成一路。
func (c *Client) HongKongKLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if normalized.Market != "HK" {
		return nil, fmt.Errorf("tencent hk kline: %s is not a hong kong symbol", symbol)
	}
	period = strings.ToLower(strings.TrimSpace(period))
	if period == "" || period == "daily" {
		period = "day"
	}
	if period != "day" && period != "week" && period != "month" {
		return nil, fmt.Errorf("unsupported period %q", period)
	}
	if limit <= 0 {
		limit = 300
	}

	params := url.Values{"param": {normalized.Sina + "," + period + ",,," + strconv.Itoa(limit) + ",qfq"}}
	requestURL := hongKongKLineBaseURL(c.klineBaseURL) + "?" + params.Encode()
	var payload struct {
		Code int `json:"code"`
		Data map[string]struct {
			QFQDay   [][]any `json:"qfqday"`
			QFQWeek  [][]any `json:"qfqweek"`
			QFQMonth [][]any `json:"qfqmonth"`
			Day      [][]any `json:"day"`
			Week     [][]any `json:"week"`
			Month    [][]any `json:"month"`
		} `json:"data"`
	}
	start := time.Now()
	if err := c.getJSON(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	if payload.Code != 0 {
		return nil, fmt.Errorf("tencent hk kline code=%d", payload.Code)
	}
	node := payload.Data[normalized.Sina]
	rows := node.QFQDay
	switch period {
	case "week":
		rows = node.QFQWeek
	case "month":
		rows = node.QFQMonth
	}
	if len(rows) == 0 {
		// 极端情况下上游不给复权序列，退回未复权，宁可少精度也不要整段缺失。
		switch period {
		case "week":
			rows = node.Week
		case "month":
			rows = node.Month
		default:
			rows = node.Day
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("tencent hk kline unavailable")
	}

	meta := foundation.SourceMeta{Source: "tencent:hk-qfq-kline", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	chinaTime := time.FixedZone("Asia/Shanghai", 8*60*60)
	lines := make([]foundation.KLine, 0, len(rows))
	for _, row := range rows {
		if len(row) < 6 {
			continue
		}
		day, parseErr := time.ParseInLocation("2006-01-02", anyString(row[0]), chinaTime)
		if parseErr != nil {
			continue
		}
		lines = append(lines, foundation.KLine{
			Symbol: normalized.Canonical, Time: day,
			Open: parseFloat(anyString(row[1])), Close: parseFloat(anyString(row[2])),
			High: parseFloat(anyString(row[3])), Low: parseFloat(anyString(row[4])),
			Volume: parseFloat(anyString(row[5])), Meta: meta,
		})
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("tencent hk kline returned no valid bars")
	}
	return lines, nil
}

// hongKongKLineBaseURL 把 A 股 K 线端点换成港股端点：
// .../app/fqkline/get、.../app/newfqkline/get 都对应 .../app/hkfqkline/get。
func hongKongKLineBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "https://web.ifzq.gtimg.cn/appstock/app/hkfqkline/get"
	}
	if strings.HasSuffix(base, "/hkfqkline/get") {
		return base
	}
	for _, suffix := range []string{"/newfqkline/get", "/fqkline/get"} {
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix) + "/hkfqkline/get"
		}
	}
	return base
}
