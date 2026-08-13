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

func (c *Client) getJSONWithRetry(ctx context.Context, requestURL string, target any) error {
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
