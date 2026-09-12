package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

type Client struct {
	baseURL             string
	quoteBaseURL        string
	quoteFallbackURLs   []string
	dataBaseURL         string
	topicBaseURL        string
	datacenterBaseURL   string
	f10BaseURL          string
	announcementBaseURL string
	reportBaseURL       string
	thsBaseURL          string
	httpClient          *http.Client
	catalogMu           sync.Mutex
	catalog             []foundation.StockCatalogEntry
	catalogUntil        time.Time
	thsBillboardMu      sync.Mutex
	thsBillboardPages   map[string]thsBillboardPage
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithQuoteBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.quoteBaseURL = strings.TrimRight(baseURL, "/")
		c.quoteFallbackURLs = nil
	}
}

func WithQuoteFallbackBaseURLs(baseURLs ...string) Option {
	return func(c *Client) {
		c.quoteFallbackURLs = c.quoteFallbackURLs[:0]
		for _, baseURL := range baseURLs {
			if normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/"); normalized != "" {
				c.quoteFallbackURLs = append(c.quoteFallbackURLs, normalized)
			}
		}
	}
}

func WithDataBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.dataBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithTopicBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.topicBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithDatacenterBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.datacenterBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithF10BaseURL(baseURL string) Option {
	return func(c *Client) {
		c.f10BaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithAnnouncementBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.announcementBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithReportBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.reportBaseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithTHSBaseURL is primarily used by tests and local mirrors. Production
// requests use the public 同花顺 data-center page for seat classifications.
func WithTHSBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.thsBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:             "https://push2his.eastmoney.com",
		quoteBaseURL:        "https://push2.eastmoney.com",
		// push2delay is the delayed-quote mirror of push2 and is reachable from
		// networks where push2 (and its 82/90 shards) reset the connection, so it
		// is tried before the numbered shards.
		quoteFallbackURLs:   []string{"https://push2delay.eastmoney.com", "https://82.push2.eastmoney.com", "https://90.push2.eastmoney.com"},
		dataBaseURL:         "https://data.eastmoney.com",
		topicBaseURL:        "https://push2ex.eastmoney.com",
		datacenterBaseURL:   "https://datacenter-web.eastmoney.com",
		f10BaseURL:          "https://datacenter.eastmoney.com/securities",
		announcementBaseURL: "https://np-anotice-stock.eastmoney.com",
		reportBaseURL:       "https://reportapi.eastmoney.com",
		thsBaseURL:          "https://data.10jqka.com.cn",
		httpClient:          &http.Client{Timeout: 15 * time.Second},
		thsBillboardPages:   make(map[string]thsBillboardPage),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 120
	}
	klt := eastMoneyPeriod(period)
	if klt == "" {
		return nil, fmt.Errorf("unsupported period %q", period)
	}

	endpoint := c.baseURL + "/api/qt/stock/kline/get"
	params := url.Values{}
	params.Set("secid", normalized.EastMoneySecID)
	params.Set("fields1", "f1,f2,f3,f4,f5,f6")
	params.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61")
	params.Set("klt", klt)
	params.Set("fqt", "1")
	params.Set("end", "20500101")
	params.Set("lmt", strconv.Itoa(limit))
	params.Set("_", strconv.FormatInt(time.Now().UnixMilli(), 10))
	requestURL := endpoint + "?" + params.Encode()

	start := time.Now()
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			KLines []string `json:"klines"`
		} `json:"data"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	if payload.RC != 0 {
		return nil, fmt.Errorf("eastmoney rc=%d", payload.RC)
	}

	meta := foundation.SourceMeta{
		Source:    "eastmoney",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	items := make([]foundation.KLine, 0, len(payload.Data.KLines))
	for _, raw := range payload.Data.KLines {
		item, err := parseKLine(raw, normalized.Canonical, meta)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// QuoteAvailability 是一次轻量批量快照里的可交易性字段。
// 新浪日线只返回 volume，拿不到成交额与换手率，因此 K 线兜底到新浪时需要用
// 东财快照把这些字段补回来，否则前端的可交易性置信度会被无谓地压低。
type QuoteAvailability struct {
	Symbol       string
	Amount       float64
	TurnoverRate float64
}

// QuoteAvailabilityBatch 批量拉取最近一个交易日的成交额（f6）与换手率（f8）。
// 走 ulist.np 接口，一次请求可覆盖多只标的，避免逐股请求。
func (c *Client) QuoteAvailabilityBatch(ctx context.Context, symbols []string) (map[string]QuoteAvailability, error) {
	if len(symbols) == 0 {
		return map[string]QuoteAvailability{}, nil
	}
	secIDs := make([]string, 0, len(symbols))
	canonical := make(map[string]string, len(symbols))
	seen := make(map[string]bool, len(symbols))
	for _, symbol := range symbols {
		normalized, err := foundation.NormalizeSymbol(symbol)
		if err != nil {
			continue
		}
		if seen[normalized.EastMoneySecID] {
			continue
		}
		seen[normalized.EastMoneySecID] = true
		secIDs = append(secIDs, normalized.EastMoneySecID)
		canonical[normalized.EastMoneySecID] = normalized.Canonical
	}
	if len(secIDs) == 0 {
		return map[string]QuoteAvailability{}, nil
	}

	endpoint := c.quoteBaseURL + "/api/qt/ulist.np/get"
	params := url.Values{}
	params.Set("fltt", "2")
	params.Set("invt", "2")
	params.Set("secids", strings.Join(secIDs, ","))
	params.Set("fields", "f12,f13,f14,f6,f8")
	params.Set("ut", "fa5fd1943c7b386f172d6893dbfba10b")
	params.Set("_", strconv.FormatInt(time.Now().UnixMilli(), 10))
	requestURL := endpoint + "?" + params.Encode()

	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			Diff []struct {
				Code         string        `json:"f12"`
				Market       flexibleFloat `json:"f13"`
				Amount       flexibleFloat `json:"f6"`
				TurnoverRate flexibleFloat `json:"f8"`
			} `json:"diff"`
		} `json:"data"`
	}
	// 与实时行情同族，复用主机轮换以绕开单节点限流。
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	if payload.RC != 0 {
		return nil, fmt.Errorf("eastmoney ulist rc=%d", payload.RC)
	}

	result := make(map[string]QuoteAvailability, len(payload.Data.Diff))
	for _, raw := range payload.Data.Diff {
		market := int(raw.Market)
		if market != 0 && market != 1 {
			continue
		}
		key := fmt.Sprintf("%d.%s", market, raw.Code)
		symbol := canonical[key]
		if symbol == "" {
			continue
		}
		result[symbol] = QuoteAvailability{
			Symbol:       symbol,
			Amount:       float64(raw.Amount),
			TurnoverRate: float64(raw.TurnoverRate),
		}
	}
	return result, nil
}

func (c *Client) getJSONWithRetry(ctx context.Context, requestURL string, target any) error {
	// push2 quote hosts are sharded and not equally reachable from every
	// network (some ISPs reset connections to push2/82.push2/90.push2 while
	// push2delay answers normally). Rotate across the configured hosts so a
	// single dead host does not take the whole quote family offline.
	variants := c.quoteRequestVariants(requestURL)
	if len(variants) > 1 {
		var lastErr error
		for _, variant := range variants {
			if err := c.getJSONWithRetrySingleHost(ctx, variant, target); err != nil {
				lastErr = err
				if ctx.Err() != nil {
					return ctx.Err()
				}
				continue
			}
			return nil
		}
		return lastErr
	}
	return c.getJSONWithRetrySingleHost(ctx, requestURL, target)
}

// quoteRequestVariants returns the same request pointed at every configured
// quote host, starting with the URL's own host. Non-quote requests (datacenter,
// reportapi, push2his, ...) are returned unchanged.
func (c *Client) quoteRequestVariants(requestURL string) []string {
	parsed, err := url.Parse(requestURL)
	if err != nil || parsed.Host == "" {
		return []string{requestURL}
	}
	hosts := append([]string{c.quoteBaseURL}, c.quoteFallbackURLs...)
	known := false
	for _, host := range hosts {
		if hostURL, hostErr := url.Parse(host); hostErr == nil && strings.EqualFold(hostURL.Host, parsed.Host) {
			known = true
			break
		}
	}
	if !known {
		return []string{requestURL}
	}
	variants := []string{requestURL}
	seen := map[string]bool{strings.ToLower(parsed.Host): true}
	for _, host := range hosts {
		hostURL, hostErr := url.Parse(host)
		if hostErr != nil || hostURL.Host == "" || seen[strings.ToLower(hostURL.Host)] {
			continue
		}
		seen[strings.ToLower(hostURL.Host)] = true
		clone := *parsed
		clone.Scheme = hostURL.Scheme
		clone.Host = hostURL.Host
		variants = append(variants, clone.String())
	}
	return variants
}

func (c *Client) getJSONWithRetrySingleHost(ctx context.Context, requestURL string, target any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(150 * time.Millisecond)
		}
		err := c.getJSON(ctx, requestURL, target)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isTransient(err) {
			return err
		}
	}
	return lastErr
}

func (c *Client) getJSON(ctx context.Context, requestURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	req.Header.Set("Referer", "https://quote.eastmoney.com/")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("eastmoney http status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func isTransient(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "eof") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "http status 5")
}

func eastMoneyPeriod(period string) string {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "", "day", "daily", "101":
		return "101"
	case "week", "weekly", "102":
		return "102"
	case "month", "monthly", "103":
		return "103"
	case "1", "5", "15", "30", "60", "120":
		return period
	default:
		return ""
	}
}

func parseKLine(raw string, symbol string, meta foundation.SourceMeta) (foundation.KLine, error) {
	fields := strings.Split(raw, ",")
	if len(fields) < 7 {
		return foundation.KLine{}, fmt.Errorf("invalid eastmoney kline %q", raw)
	}
	day, err := parseKLineTime(fields[0])
	if err != nil {
		return foundation.KLine{}, err
	}
	open, _ := strconv.ParseFloat(fields[1], 64)
	closePrice, _ := strconv.ParseFloat(fields[2], 64)
	high, _ := strconv.ParseFloat(fields[3], 64)
	low, _ := strconv.ParseFloat(fields[4], 64)
	volume, _ := strconv.ParseFloat(fields[5], 64)
	amount, _ := strconv.ParseFloat(fields[6], 64)
	changePercent := 0.0
	if len(fields) > 8 {
		changePercent, _ = strconv.ParseFloat(fields[8], 64)
	}
	turnover := 0.0
	if len(fields) > 10 {
		turnover, _ = strconv.ParseFloat(fields[10], 64)
	}
	return foundation.KLine{
		Symbol:        symbol,
		Time:          day,
		Open:          open,
		High:          high,
		Low:           low,
		Close:         closePrice,
		Volume:        volume,
		Amount:        amount,
		ChangePercent: changePercent,
		TurnoverRate:  turnover,
		Meta:          meta,
	}, nil
}

// parseKLineTime accepts both daily bars (YYYY-MM-DD) and intraday bars
// returned by Eastmoney (YYYY-MM-DD HH:MM[:SS]). A few upstream responses
// use slash-separated dates, so keep that format for resilient parsing too.
func parseKLineTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
		"2006/01/02",
	} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid kline time %q", value)
}
