package themelibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://www.bigamap.cn"
	// LibraryPageURL is the human-facing BigAMap application. Individual themes
	// are rendered inside the SPA, so the deep link is the app rather than a
	// theme path that would 404.
	LibraryPageURL = "https://www.bigamap.cn/app"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func NewClient(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL: baseURL,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          32,
				MaxIdleConnsPerHost:   16,
				IdleConnTimeout:       60 * time.Second,
				TLSHandshakeTimeout:   15 * time.Second,
				ResponseHeaderTimeout: 20 * time.Second,
			},
		},
	}
}

type RawList struct {
	Items     []RawListItem `json:"items"`
	UpdatedAt string        `json:"updatedAt"`
}

type RawListItem struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	ChangePercent float64 `json:"changePercent"`
	StockCount    int     `json:"stockCount"`
	LimitUpCount  int     `json:"limitUpCount"`
	UpdatedAt     string  `json:"updatedAt"`
	CreateTime    int64   `json:"createTime"`
	UpdateTime    int64   `json:"updateTime"`
	Description   string  `json:"description"`
	RisingCount   int     `json:"risingCount"`
	FallingCount  int     `json:"fallingCount"`
	FlatCount     int     `json:"flatCount"`
	Hot           float64 `json:"hot"`
}

type RawTheme struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	ChangePercent float64       `json:"changePercent"`
	StockCount    int           `json:"stockCount"`
	LimitUpCount  int           `json:"limitUpCount"`
	UpdatedAt     string        `json:"updatedAt"`
	CreateTime    int64         `json:"createTime"`
	UpdateTime    int64         `json:"updateTime"`
	Description   string        `json:"description"`
	Categories    []RawCategory `json:"categories"`
	Stocks        RawStocks     `json:"stocks"`
	Introduction  string        `json:"introduction"`
}

type RawCategory struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	ChangePercent float64       `json:"changePercent"`
	LimitUpCount  int           `json:"limitUpCount"`
	Stocks        []RawStock    `json:"stocks"`
	Children      []RawCategory `json:"children"`
}

type RawStock struct {
	StockID       int64    `json:"stockId"`
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	Price         float64  `json:"price"`
	ChangePercent float64  `json:"changePercent"`
	IsLimitUp     bool     `json:"isLimitUp"`
	LimitUpStatus *string  `json:"limitUpStatus"`
	Hot           float64  `json:"hot"`
	Tags          []string `json:"tags"`
	Description   string   `json:"description"`
}

// RawStocks accepts both the null and the array forms used by the upstream
// detail endpoint. Most themes put their members under categories, while a
// few expose a flat top-level stocks array instead.
type RawStocks []RawStock

func (s *RawStocks) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == "null" {
		*s = nil
		return nil
	}
	var stocks []RawStock
	if err := json.Unmarshal(data, &stocks); err != nil {
		return err
	}
	*s = stocks
	return nil
}

func (c *Client) List(ctx context.Context) (RawList, error) {
	var result RawList
	if err := c.getJSON(ctx, "/api/v1/public/theme-library", &result); err != nil {
		return RawList{}, err
	}
	return result, nil
}

func (c *Client) Detail(ctx context.Context, id string) (RawTheme, error) {
	var result RawTheme
	path := "/api/v1/public/theme-library/" + strings.TrimSpace(id)
	if err := c.getJSON(ctx, path, &result); err != nil {
		return RawTheme{}, err
	}
	return result, nil
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	request.Header.Set("Referer", LibraryPageURL)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("bigamap %s: HTTP %d: %s", path, response.StatusCode, strings.TrimSpace(string(body)))
	}
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("bigamap %s: decode response: %w", path, err)
	}
	return nil
}

func DetailURL(id string) string {
	return DefaultBaseURL + "/api/v1/public/theme-library/" + strings.TrimSpace(id)
}
