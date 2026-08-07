package duanxianxia

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

const maxResponseBytes = 2 << 20

var poolStreakLabel = regexp.MustCompile(`^(\d+)天(\d+)板$`)

var (
	poolCipherKey = []byte("secretkey322yes!!aaaaaaaaaaaaaaa")
	poolCipherIV  = []byte("fixediv_16valued")
)

type Client struct {
	baseURL string
	http    *http.Client
	now     func() time.Time
}

type ClientConfig struct {
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}

func NewClient(config ClientConfig) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 12 * time.Second}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Client{baseURL: baseURL, http: httpClient, now: now}
}

func (c *Client) Fetch(ctx context.Context, leaderThemeLimit int) (Snapshot, error) {
	if leaderThemeLimit <= 0 {
		leaderThemeLimit = 3
	}
	var rotation struct {
		First string `json:"first"`
		HTML  string `json:"html"`
	}
	if err := c.postForm(ctx, "/api/getPlateRotatData", url.Values{
		"from":  {"kaipan"},
		"days":  {"20"},
		"dates": {""},
	}, &rotation); err != nil {
		return Snapshot{}, err
	}
	tradeDate, themes, err := ParseRotationHTML(rotation.HTML)
	if err != nil {
		return Snapshot{}, err
	}
	if rotation.First != "" && len(themes) > 0 && themes[0].Code != rotation.First {
		return Snapshot{}, fmt.Errorf("rotation first code %s does not match rank one %s", rotation.First, themes[0].Code)
	}

	for index := 0; index < len(themes) && index < leaderThemeLimit; index++ {
		var response struct {
			HTML string `json:"html"`
		}
		if err := c.postForm(ctx, "/api/getLongByPlate", url.Values{
			"platecode": {themes[index].Code},
			"days":      {"20"},
			"dates":     {""},
		}, &response); err != nil {
			return Snapshot{}, fmt.Errorf("fetch leaders for %s: %w", themes[index].Name, err)
		}
		leaders, noLeaders, err := ParseLeadersHTML(response.HTML)
		if err != nil {
			return Snapshot{}, fmt.Errorf("parse leaders for %s: %w", themes[index].Name, err)
		}
		themes[index].Leaders = leaders
		themes[index].LeadersLoaded = true
		themes[index].NoLeaders = noLeaders
	}

	fetchedAt := c.now()
	return Snapshot{
		ID:        fmt.Sprintf("kpl-%s-%d", tradeDate, fetchedAt.UnixMilli()),
		TradeDate: tradeDate,
		FetchedAt: fetchedAt,
		Themes:    themes,
	}, nil
}

func (c *Client) FetchLimitUpPool(ctx context.Context) (LimitUpPoolSnapshot, error) {
	dataBaseURL := c.baseURL
	var source struct {
		DataURL string `json:"data_url"`
	}
	if _, _, err := c.getBytes(ctx, c.baseURL+"/vendor/stockdata/datasource.json", &source); err == nil {
		if value := strings.TrimRight(strings.TrimSpace(source.DataURL), "/"); value != "" {
			dataBaseURL = value
		}
	}

	sourceURL := dataBaseURL + "/vendor/stockdata/ztpool.json"
	payload, headers, err := c.getBytes(ctx, sourceURL, nil)
	if err != nil {
		return LimitUpPoolSnapshot{}, err
	}
	plain, err := decryptPoolPayload(payload)
	if err != nil {
		return LimitUpPoolSnapshot{}, fmt.Errorf("decrypt limit-up pool: %w", err)
	}
	tradeTime := c.now()
	if raw := strings.TrimSpace(headers.Get("Last-Modified")); raw != "" {
		if parsed, parseErr := http.ParseTime(raw); parseErr == nil {
			tradeTime = parsed
		}
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	tradeDate := tradeTime.In(location).Format("2006-01-02")
	fetchedAt := c.now()
	events, err := parseLimitUpPool(plain, tradeDate, fetchedAt, sourceURL)
	if err != nil {
		return LimitUpPoolSnapshot{}, err
	}
	return LimitUpPoolSnapshot{
		ID:         fmt.Sprintf("kpl-pool-%s-%d", tradeDate, fetchedAt.UnixMilli()),
		TradeDate:  tradeDate,
		FetchedAt:  fetchedAt,
		ModifiedAt: tradeTime,
		SourceURL:  sourceURL,
		ETag:       strings.TrimSpace(headers.Get("ETag")),
		Events:     events,
	}, nil
}

func (c *Client) getBytes(ctx context.Context, requestURL string, target any) ([]byte, http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Accept", "application/json,text/plain,*/*")
	request.Header.Set("Referer", c.baseURL+"/web/pool")
	request.Header.Set("User-Agent", "easy-stock/1.0 short-term-radar")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("get %s: %w", requestURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, response.Header, fmt.Errorf("get %s: unexpected status %d", requestURL, response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, response.Header, fmt.Errorf("read %s response: %w", requestURL, err)
	}
	if len(payload) > maxResponseBytes {
		return nil, response.Header, fmt.Errorf("get %s: response exceeds %d bytes", requestURL, maxResponseBytes)
	}
	if target != nil {
		if err := json.Unmarshal(payload, target); err != nil {
			return nil, response.Header, fmt.Errorf("decode %s response: %w", requestURL, err)
		}
	}
	return payload, response.Header, nil
}

func decryptPoolPayload(payload []byte) ([]byte, error) {
	trimmed := strings.TrimSpace(string(payload))
	if strings.HasPrefix(trimmed, "{") {
		return []byte(trimmed), nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	block, err := aes.NewCipher(poolCipherKey)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) == 0 || len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("invalid encrypted payload length %d", len(ciphertext))
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, poolCipherIV).CryptBlocks(plain, ciphertext)
	padding := int(plain[len(plain)-1])
	if padding <= 0 || padding > block.BlockSize() || padding > len(plain) {
		return nil, fmt.Errorf("invalid PKCS7 padding")
	}
	for _, value := range plain[len(plain)-padding:] {
		if int(value) != padding {
			return nil, fmt.Errorf("invalid PKCS7 padding bytes")
		}
	}
	return plain[:len(plain)-padding], nil
}

func parseLimitUpPool(payload []byte, tradeDate string, fetchedAt time.Time, sourceURL string) ([]foundation.LimitUpEvent, error) {
	var document struct {
		List [][]json.RawMessage `json:"list"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		return nil, fmt.Errorf("decode limit-up pool JSON: %w", err)
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	date, err := time.ParseInLocation("2006-01-02", tradeDate, location)
	if err != nil {
		return nil, fmt.Errorf("parse limit-up pool trade date %q: %w", tradeDate, err)
	}
	meta := foundation.SourceMeta{
		Source:    "duanxianxia:kaipanla-limit-up",
		SourceURL: sourceURL,
		FetchedAt: fetchedAt,
		TradeDate: tradeDate,
	}
	events := make([]foundation.LimitUpEvent, 0, len(document.List))
	for _, item := range document.List {
		if len(item) < 12 {
			continue
		}
		normalized, err := foundation.NormalizeSymbol(rawPoolString(item[0]))
		if err != nil {
			continue
		}
		streakLabel := rawPoolString(item[7])
		days, count := parsePoolStreakLabel(streakLabel)
		streak := rawPoolInt(item[11])
		if streak <= 0 {
			streak = max(count, 1)
		}
		lastLimitTime := ""
		if len(item) > 12 {
			lastLimitTime = rawPoolString(item[12])
		}
		events = append(events, foundation.LimitUpEvent{
			Symbol:         normalized.Canonical,
			Name:           rawPoolString(item[1]),
			Date:           date,
			ChangePercent:  rawPoolFloat(item[2]),
			Amount:         rawPoolFloat(item[8]),
			FloatMarketCap: rawPoolFloat(item[9]),
			Streak:         streak,
			FirstLimitTime: rawPoolString(item[5]),
			LastLimitTime:  lastLimitTime,
			OpenCount:      rawPoolInt(item[4]),
			Days:           days,
			Count:          count,
			Concepts:       splitPoolConcepts(rawPoolString(item[6])),
			StreakLabel:    streakLabel,
			BoardType:      rawPoolString(item[10]),
			Meta:           meta,
		})
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("limit-up pool returned no stocks")
	}
	return events, nil
}

func parsePoolStreakLabel(value string) (int, int) {
	value = strings.TrimSpace(value)
	if value == "首板" {
		return 1, 1
	}
	match := poolStreakLabel.FindStringSubmatch(value)
	if len(match) != 3 {
		return 0, 0
	}
	days, _ := strconv.Atoi(match[1])
	count, _ := strconv.Atoi(match[2])
	return days, count
}

func splitPoolConcepts(value string) []string {
	items := []string{}
	seen := map[string]struct{}{}
	for _, candidate := range strings.Split(value, "+") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		items = append(items, candidate)
	}
	return items
}

func rawPoolString(value json.RawMessage) string {
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if err := json.Unmarshal(value, &number); err == nil {
		return number.String()
	}
	return ""
}

func rawPoolFloat(value json.RawMessage) float64 {
	raw := rawPoolString(value)
	if raw == "" {
		raw = strings.TrimSpace(string(value))
	}
	parsed, _ := strconv.ParseFloat(raw, 64)
	return parsed
}

func rawPoolInt(value json.RawMessage) int {
	return int(rawPoolFloat(value))
}

func (c *Client) postForm(ctx context.Context, path string, form url.Values, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json,text/plain,*/*")
	request.Header.Set("Referer", c.baseURL+"/web/platerotat")
	request.Header.Set("User-Agent", "easy-stock/1.0 theme-radar")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("post %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("post %s: unexpected status %d", path, response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read %s response: %w", path, err)
	}
	if len(payload) > maxResponseBytes {
		return fmt.Errorf("post %s: response exceeds %d bytes", path, maxResponseBytes)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	return nil
}
