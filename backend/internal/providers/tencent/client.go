package tencent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type Client struct {
	quoteBaseURL          string
	klineBaseURL          string
	industryBaseURL       string
	industryStocksBaseURL string
	httpClient            *http.Client
}

type Option func(*Client)

func WithQuoteBaseURL(value string) Option {
	return func(c *Client) { c.quoteBaseURL = strings.TrimRight(value, "/") }
}

func WithKLineBaseURL(value string) Option {
	return func(c *Client) { c.klineBaseURL = strings.TrimRight(value, "/") }
}

func WithIndustryBaseURL(value string) Option {
	return func(c *Client) { c.industryBaseURL = strings.TrimRight(value, "/") }
}

func WithIndustryStocksBaseURL(value string) Option {
	return func(c *Client) { c.industryStocksBaseURL = strings.TrimRight(value, "/") }
}

func WithHTTPClient(value *http.Client) Option {
	return func(c *Client) {
		if value != nil {
			c.httpClient = value
		}
	}
}

func NewClient(options ...Option) *Client {
	client := &Client{
		quoteBaseURL:          "https://qt.gtimg.cn",
		klineBaseURL:          "https://web.ifzq.gtimg.cn/appstock/app/fqkline/get",
		industryBaseURL:       "https://proxy.finance.qq.com/ifzqgtimg/appstock/app/mktHs/rank",
		industryStocksBaseURL: "https://proxy.finance.qq.com/cgi/cgi-bin/rank/hs/getBoardRankList",
		httpClient:            &http.Client{Timeout: 12 * time.Second},
	}
	for _, option := range options {
		option(client)
	}
	return client
}

type indexDefinition struct {
	ID       string
	QuoteKey string
	KLineKey string
	Code     string
	Name     string
	Region   string
	Market   string
	Currency string
	Core     bool
}

var indexCatalog = []indexDefinition{
	{ID: "sse", QuoteKey: "s_sh000001", KLineKey: "sh000001", Code: "000001", Name: "上证指数", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "szse", QuoteKey: "s_sz399001", KLineKey: "sz399001", Code: "399001", Name: "深证成指", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "chinext", QuoteKey: "s_sz399006", KLineKey: "sz399006", Code: "399006", Name: "创业板指", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "csi300", QuoteKey: "s_sh000300", KLineKey: "sh000300", Code: "000300", Name: "沪深300", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "sse50", QuoteKey: "s_sh000016", KLineKey: "sh000016", Code: "000016", Name: "上证50", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "csi1000", QuoteKey: "s_sh000852", KLineKey: "sh000852", Code: "000852", Name: "中证1000", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "star50", QuoteKey: "s_sh000688", KLineKey: "sh000688", Code: "000688", Name: "科创50", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "hsi", QuoteKey: "r_hkHSI", KLineKey: "hkHSI", Code: "HSI", Name: "恒生指数", Region: "中国香港", Market: "HK", Currency: "HKD", Core: true},
	{ID: "dow", QuoteKey: "usDJI", KLineKey: "usDJI", Code: ".DJI", Name: "道琼斯", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "sp500", QuoteKey: "usINX", KLineKey: "usINX", Code: ".INX", Name: "标普500", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "nasdaq", QuoteKey: "usIXIC", KLineKey: "usIXIC", Code: ".IXIC", Name: "纳斯达克", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "ftse", QuoteKey: "ukUKX", KLineKey: "ukUKX", Code: "UKX", Name: "英国富时100", Region: "欧洲", Market: "UK", Currency: "GBP"},
}

func (c *Client) MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	definitions := make([]indexDefinition, 0, len(indexCatalog))
	keys := make([]string, 0, len(indexCatalog))
	for _, definition := range indexCatalog {
		if scope == "core" && !definition.Core {
			continue
		}
		definitions = append(definitions, definition)
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
	meta := foundation.SourceMeta{Source: "tencent:index", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	byKey := parseTencentQuoteLines(body)
	items := make([]foundation.MarketIndexSnapshot, 0, len(definitions))
	for _, definition := range definitions {
		fields := byKey[definition.QuoteKey]
		if len(fields) < 6 {
			continue
		}
		tradeTime := parseTencentTradeTime(fieldAt(fields, 30))
		change, changePercent := tencentChange(fields, strings.HasPrefix(definition.QuoteKey, "s_"))
		items = append(items, foundation.MarketIndexSnapshot{
			ID: definition.ID, SecID: definition.QuoteKey, Code: firstString(fieldAt(fields, 2), definition.Code), Name: firstString(fieldAt(fields, 1), definition.Name),
			Region: definition.Region, Market: definition.Market, Currency: definition.Currency, Price: parseFloat(fieldAt(fields, 3)), Change: change, ChangePercent: changePercent,
			TradeTime: tradeTime, Status: tencentMarketStatus(definition.Market, tradeTime, time.Now()), Meta: meta,
		})
	}
	if len(items) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("tencent returned no index snapshots")
	}
	return items, meta, nil
}

func (c *Client) MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error) {
	definition, ok := findIndex(id)
	if !ok {
		return foundation.MarketIndexSeries{}, fmt.Errorf("unsupported index %q", id)
	}
	period = strings.ToLower(strings.TrimSpace(period))
	if period == "" || period == "daily" {
		period = "day"
	}
	if period != "day" && period != "week" && period != "month" {
		return foundation.MarketIndexSeries{}, fmt.Errorf("unsupported period %q", period)
	}
	if limit <= 0 {
		limit = 120
	}
	values := url.Values{}
	values.Set("param", fmt.Sprintf("%s,%s,,,%d,qfq", definition.KLineKey, period, limit))
	requestURL := c.klineBaseURL + "?" + values.Encode()
	start := time.Now()
	var payload struct {
		Code int `json:"code"`
		Data map[string]struct {
			Day    [][]any `json:"day"`
			Week   [][]any `json:"week"`
			Month  [][]any `json:"month"`
			QFQDay [][]any `json:"qfqday"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, requestURL, &payload); err != nil {
		return foundation.MarketIndexSeries{}, err
	}
	if payload.Code != 0 {
		return foundation.MarketIndexSeries{}, fmt.Errorf("tencent index kline code=%d", payload.Code)
	}
	raw := payload.Data[definition.KLineKey]
	rawLines := raw.Day
	if period == "week" {
		rawLines = raw.Week
	} else if period == "month" {
		rawLines = raw.Month
	} else if len(rawLines) == 0 {
		rawLines = raw.QFQDay
	}
	meta := foundation.SourceMeta{Source: "tencent:index-kline", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	lines := make([]foundation.KLine, 0, len(rawLines))
	previousClose := 0.0
	for _, values := range rawLines {
		if len(values) < 6 {
			continue
		}
		day, parseErr := time.ParseInLocation("2006-01-02", anyString(values[0]), time.Local)
		if parseErr != nil {
			continue
		}
		closePrice := parseFloat(anyString(values[2]))
		changePercent := 0.0
		if previousClose > 0 {
			changePercent = (closePrice/previousClose - 1) * 100
		}
		lines = append(lines, foundation.KLine{Symbol: definition.ID, Time: day, Open: parseFloat(anyString(values[1])), Close: closePrice, High: parseFloat(anyString(values[3])), Low: parseFloat(anyString(values[4])), Volume: parseFloat(anyString(values[5])), ChangePercent: changePercent, Meta: meta})
		previousClose = closePrice
	}
	if len(lines) == 0 {
		return foundation.MarketIndexSeries{}, fmt.Errorf("tencent returned no index bars")
	}
	latest := lines[len(lines)-1]
	index := foundation.MarketIndexSnapshot{ID: definition.ID, SecID: definition.QuoteKey, Code: definition.Code, Name: definition.Name, Region: definition.Region, Market: definition.Market, Currency: definition.Currency, Price: latest.Close, ChangePercent: latest.ChangePercent, TradeTime: latest.Time, Status: "closed", Meta: meta}
	return foundation.MarketIndexSeries{Index: index, Lines: lines, Meta: meta}, nil
}

func (c *Client) get(ctx context.Context, requestURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Referer", "https://stockapp.finance.qq.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tencent http status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	decoded, decodeErr := simplifiedchinese.GB18030.NewDecoder().Bytes(body)
	if decodeErr == nil {
		body = decoded
	}
	return string(body), nil
}

func (c *Client) getJSON(ctx context.Context, requestURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", "https://gu.qq.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tencent http status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func parseTencentQuoteLines(body string) map[string][]string {
	result := map[string][]string{}
	for _, line := range strings.Split(body, ";") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		left, value, ok := strings.Cut(line, "=\"")
		if !ok {
			continue
		}
		key := strings.TrimPrefix(left, "v_")
		result[key] = strings.Split(strings.TrimSuffix(value, "\""), "~")
	}
	return result
}

func tencentChange(fields []string, simple bool) (float64, float64) {
	if simple {
		return parseFloat(fieldAt(fields, 4)), parseFloat(fieldAt(fields, 5))
	}
	return parseFloat(fieldAt(fields, 31)), parseFloat(fieldAt(fields, 32))
}

func tencentMarketStatus(_ string, tradeTime time.Time, now time.Time) string {
	if !tradeTime.IsZero() && now.Sub(tradeTime) >= 0 && now.Sub(tradeTime) <= 20*time.Minute {
		return "open"
	}
	return "closed"
}

func findIndex(id string) (indexDefinition, bool) {
	for _, item := range indexCatalog {
		if item.ID == strings.ToLower(strings.TrimSpace(id)) {
			return item, true
		}
	}
	return indexDefinition{}, false
}

func fieldAt(fields []string, index int) string {
	if index < 0 || index >= len(fields) {
		return ""
	}
	return strings.TrimSpace(fields[index])
}

func parseFloat(value string) float64 {
	parsed, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return parsed
}

func parseTencentTradeTime(value string) time.Time {
	for _, layout := range []string{"2006/01/02 15:04:05", "2006-01-02 15:04:05", "20060102150405"} {
		if parsed, err := time.ParseInLocation(layout, strings.TrimSpace(value), time.Local); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func anyString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
