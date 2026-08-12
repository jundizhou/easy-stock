package sina

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
	"unicode/utf8"

	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type Client struct {
	baseURL                string
	kLineBaseURL           string
	moneyFlowBaseURL       string
	sectorMoneyFlowBaseURL string
	httpClient             *http.Client
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

func WithKLineBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.kLineBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithMoneyFlowBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.moneyFlowBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithSectorMoneyFlowBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.sectorMoneyFlowBaseURL = strings.TrimRight(baseURL, "/")
	}
}

func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:                "https://hq.sinajs.cn",
		kLineBaseURL:           "https://quotes.sina.cn/cn/api/jsonp_v2.php/callback/CN_MarketDataService.getKLineData",
		moneyFlowBaseURL:       "https://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/MoneyFlow.ssl_bkzj_ssggzj",
		sectorMoneyFlowBaseURL: "https://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/MoneyFlow.ssl_bkzj_bk",
		httpClient:             &http.Client{Timeout: 10 * time.Second},
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
	scale := sinaKLineScale(period)
	if scale == "" {
		return nil, fmt.Errorf("unsupported period %q", period)
	}
	values := url.Values{}
	values.Set("symbol", normalized.Sina)
	values.Set("scale", scale)
	values.Set("ma", "no")
	values.Set("datalen", strconv.Itoa(limit))
	requestURL := c.kLineBaseURL + "?" + values.Encode()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sina kline http status %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	meta := foundation.SourceMeta{
		Source:    "sina",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	return parseKLineJSONP(decodeSinaBody(bodyBytes), normalized.Canonical, meta)
}

func (c *Client) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("symbols is required")
	}
	normalized := make([]foundation.Symbol, 0, len(symbols))
	sinaCodes := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		n, err := foundation.NormalizeSymbol(symbol)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, n)
		sinaCodes = append(sinaCodes, n.Sina)
	}

	values := url.Values{}
	values.Set("rn", strconv.FormatInt(time.Now().UnixMilli(), 10))
	values.Set("list", strings.Join(sinaCodes, ","))
	requestURL := c.requestURL(values)

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Host", "hq.sinajs.cn")
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sina http status %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	body := decodeSinaBody(bodyBytes)
	meta := foundation.SourceMeta{
		Source:    "sina",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	return parseRealtime(body, normalized, meta)
}

func (c *Client) requestURL(values url.Values) string {
	if strings.Contains(c.baseURL, "hq.sinajs.cn") {
		return c.baseURL + "/rn=" + values.Get("rn") + "&list=" + values.Get("list")
	}
	return c.baseURL + "?" + values.Encode()
}

func sinaKLineScale(period string) string {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "", "day", "daily", "101", "240":
		return "240"
	case "week", "weekly", "102", "1200":
		return "1200"
	case "1", "5", "15", "30", "60":
		return period
	default:
		return ""
	}
}

type sinaKLineItem struct {
	Day    string `json:"day"`
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
}

func parseKLineJSONP(body string, symbol string, meta foundation.SourceMeta) ([]foundation.KLine, error) {
	start := strings.Index(body, "[")
	end := strings.LastIndex(body, "]")
	if start < 0 || end < start {
		return nil, fmt.Errorf("sina kline response has no JSON array")
	}
	var rawItems []sinaKLineItem
	if err := json.Unmarshal([]byte(body[start:end+1]), &rawItems); err != nil {
		return nil, err
	}
	items := make([]foundation.KLine, 0, len(rawItems))
	for _, raw := range rawItems {
		day, err := time.ParseInLocation("2006-01-02", raw.Day, time.Local)
		if err != nil {
			return nil, err
		}
		open, _ := strconv.ParseFloat(raw.Open, 64)
		high, _ := strconv.ParseFloat(raw.High, 64)
		low, _ := strconv.ParseFloat(raw.Low, 64)
		closePrice, _ := strconv.ParseFloat(raw.Close, 64)
		volume, _ := strconv.ParseFloat(raw.Volume, 64)
		items = append(items, foundation.KLine{
			Symbol: symbol,
			Time:   day,
			Open:   open,
			High:   high,
			Low:    low,
			Close:  closePrice,
			Volume: volume,
			Meta:   meta,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("sina returned no kline bars")
	}
	return items, nil
}

func decodeSinaBody(body []byte) string {
	if utf8.Valid(body) {
		return string(body)
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(body)
	if err != nil {
		return string(body)
	}
	return string(decoded)
}

func parseRealtime(body string, symbols []foundation.Symbol, meta foundation.SourceMeta) ([]foundation.Quote, error) {
	lines := strings.Split(body, ";")
	byCode := map[string]foundation.Symbol{}
	for _, symbol := range symbols {
		byCode[symbol.Sina] = symbol
	}
	quotes := make([]foundation.Quote, 0, len(symbols))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		left, payload, ok := strings.Cut(line, "\"")
		if !ok {
			continue
		}
		payload, _, _ = strings.Cut(payload, "\"")
		code := strings.TrimPrefix(strings.TrimSpace(left), "var hq_str_")
		code = strings.TrimSuffix(code, "=")
		symbol, ok := byCode[code]
		if !ok {
			continue
		}
		fields := strings.Split(payload, ",")
		if len(fields) < 32 || fields[0] == "" {
			continue
		}
		open, _ := strconv.ParseFloat(fields[1], 64)
		prevClose, _ := strconv.ParseFloat(fields[2], 64)
		price, _ := strconv.ParseFloat(fields[3], 64)
		high, _ := strconv.ParseFloat(fields[4], 64)
		low, _ := strconv.ParseFloat(fields[5], 64)
		change := price - prevClose
		changePercent := 0.0
		if prevClose != 0 {
			changePercent = change / prevClose * 100
		}
		tradeTime, _ := time.ParseInLocation("2006-01-02 15:04:05", fields[30]+" "+fields[31], time.Local)
		quotes = append(quotes, foundation.Quote{
			Symbol:        symbol.Canonical,
			Name:          fields[0],
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
		return nil, fmt.Errorf("sina returned no quotes")
	}
	return quotes, nil
}
