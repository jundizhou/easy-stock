package sina

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// 基准指数（交易所异常波动口径）在新浪的代码映射。
// 主板用「A 股指数」而不是上证指数/深证成指：交易所算偏离值的分母
// 是 A 股指数，成分不同会让深市标的的偏离值与公告对不上。
var sinaIndexSymbols = map[string]struct {
	symbol string
	name   string
}{
	"sse":          {"sh000001", "上证指数"},
	"sse_a":        {"sh000003", "上证A股指数"},
	"szse":         {"sz399001", "深证成指"},
	"szse_a":       {"sz399107", "深证A股指数"},
	"chinext":      {"sz399006", "创业板指"},
	"chinext_comp": {"sz399102", "创业板综指"},
	"star50":       {"sh000688", "科创50"},
	"bse50":        {"bj899050", "北证50"},
}

// MarketIndexSeries 是指数日线的新浪通道：东方财富与腾讯都不可用时兜底，
// 免得偏离值计算因为单一上游抖动整块失效。
func (c *Client) MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error) {
	definition, ok := sinaIndexSymbols[strings.ToLower(strings.TrimSpace(id))]
	if !ok {
		return foundation.MarketIndexSeries{}, fmt.Errorf("unsupported index %q", id)
	}
	scale := sinaKLineScale(period)
	if scale == "" {
		return foundation.MarketIndexSeries{}, fmt.Errorf("unsupported period %q", period)
	}
	if limit <= 0 {
		limit = 120
	}
	values := url.Values{}
	values.Set("symbol", definition.symbol)
	values.Set("scale", scale)
	values.Set("ma", "no")
	values.Set("datalen", strconv.Itoa(limit))
	requestURL := c.kLineBaseURL + "?" + values.Encode()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return foundation.MarketIndexSeries{}, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return foundation.MarketIndexSeries{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return foundation.MarketIndexSeries{}, fmt.Errorf("sina index kline http status %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return foundation.MarketIndexSeries{}, err
	}
	meta := foundation.SourceMeta{
		Source:    "sina:index-kline",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	bars, err := parseKLineJSONP(decodeSinaBody(bodyBytes, resp.Header.Get("Content-Type")), definition.symbol, meta)
	if err != nil {
		return foundation.MarketIndexSeries{}, err
	}
	if len(bars) == 0 {
		return foundation.MarketIndexSeries{}, fmt.Errorf("sina index kline empty for %q", id)
	}
	last := bars[len(bars)-1]
	return foundation.MarketIndexSeries{
		Index: foundation.MarketIndexSnapshot{
			ID: id, Code: strings.TrimPrefix(definition.symbol, "sh"),
			Name: definition.name, Region: "中国", Market: "CN", Currency: "CNY",
			Price: last.Close, TradeTime: last.Time, Meta: meta,
		},
		Lines: bars,
		Meta:  meta,
	}, nil
}
