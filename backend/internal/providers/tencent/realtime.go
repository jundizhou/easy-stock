package tencent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// Realtime 从腾讯 qt.gtimg.cn 拉取实时报价（GBK，`~` 分隔）。
// 字段映射 1:1 对齐 quant_terminal.get_quotes：
//
//	val[1]=名称 val[3]=现价 val[4]=昨收 val[5]=今开
//	val[31]=涨跌 val[32]=涨跌幅% val[33]=最高 val[34]=最低 val[30]=时间
//
// 与 sina 一起作为 easy-stock 实时报价双源，消除「单接口被封被限制」的单点。
func (c *Client) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("symbols is required")
	}
	normalized := make([]foundation.Symbol, 0, len(symbols))
	tencentCodes := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		n, err := foundation.NormalizeSymbol(symbol)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, n)
		tencentCodes = append(tencentCodes, n.Sina) // sz000001 / sh600519 / bjXXXXXX
	}

	values := url.Values{}
	values.Set("q", strings.Join(tencentCodes, ","))
	requestURL := c.quoteBaseURL + "/q?" + values.Encode()

	// 抗封：同主机最小间隔限速，避免突发高频触发风控。
	if !foundation.Throttle(ctx, foundation.HostOf(requestURL), 120*time.Millisecond) {
		return nil, ctx.Err()
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	foundation.ApplyBrowserHeaders(req)
	req.Header.Set("Referer", "https://gu.qq.com/")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tencent quote http status %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	body := decodeTencentBody(bodyBytes, resp.Header.Get("Content-Type"))
	meta := foundation.SourceMeta{
		Source:      "tencent",
		SourceURL:   requestURL,
		FetchedAt:   time.Now(),
		LatencyMS:   time.Since(start).Milliseconds(),
		TradeDate:   time.Now().Format("2006-01-02"),
	}

	byCode := make(map[string]foundation.Symbol, len(normalized))
	for _, n := range normalized {
		byCode[n.Sina] = n
	}

	quotes := make([]foundation.Quote, 0, len(normalized))
	for _, line := range strings.Split(body, ";") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=") || !strings.Contains(line, "\"") {
			continue
		}
		left, payload, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		code := strings.TrimPrefix(strings.TrimSpace(left), "v_")
		symbol, ok := byCode[code]
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		payload = strings.Trim(payload, "\"")
		val := strings.Split(payload, "~")
		if len(val) < 40 || strings.TrimSpace(val[1]) == "" {
			continue
		}
		price := atof(val[3])
		prevClose := atof(val[4])
		open := atof(val[5])
		high := atof(val[33])
		low := atof(val[34])
		change := atof(val[31])
		changePercent := atof(val[32])
		tradeTime := parseTencentTime(val[30])

		quotes = append(quotes, foundation.Quote{
			Symbol:        symbol.Canonical,
			Name:          strings.TrimSpace(val[1]),
			Price:         price,
			Open:          open,
			PreviousClose: prevClose,
			High:          high,
			Low:           low,
			Change:        change,
			ChangePercent: changePercent,
			TradeTime:     tradeTime,
			Meta:          meta,
		})
	}
	if len(quotes) == 0 {
		return nil, fmt.Errorf("tencent returned no quotes")
	}
	return quotes, nil
}

func atof(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func parseTencentTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	now := time.Now()
	if t, err := time.ParseInLocation("15:04:05", s, time.Local); err == nil {
		return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return t
	}
	return time.Time{}
}

func decodeTencentBody(body []byte, contentType string) string {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "gb18030") || strings.Contains(ct, "gbk") || strings.Contains(ct, "gb2312") {
		if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(body); err == nil {
			return string(decoded)
		}
	}
	if utf8.Valid(body) {
		return string(body)
	}
	if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(body); err == nil {
		return string(decoded)
	}
	return string(body)
}
