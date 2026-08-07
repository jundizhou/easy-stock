package cls

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
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

func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:    "https://www.cls.cn",
		httpClient: &http.Client{Timeout: 12 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) LatestNews(ctx context.Context, limit int) ([]foundation.NewsItem, error) {
	if limit <= 0 {
		limit = 20
	}
	requestURL := c.baseURL + "/api/cache?app=CailianpressWeb&name=telegraph&os=web&sv=8.7.9"
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://www.cls.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cls http status %d", resp.StatusCode)
	}

	var payload struct {
		Errno int `json:"errno"`
		Data  struct {
			RollData []struct {
				ID       any    `json:"id"`
				Title    string `json:"title"`
				Content  string `json:"content"`
				CTime    int64  `json:"ctime"`
				Level    string `json:"level"`
				ShareURL string `json:"shareurl"`
				Subjects []struct {
					Name string `json:"subject_name"`
				} `json:"subjects"`
			} `json:"roll_data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Errno != 0 {
		return nil, fmt.Errorf("cls errno=%d", payload.Errno)
	}

	meta := foundation.SourceMeta{
		Source:    "cls",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	items := make([]foundation.NewsItem, 0, len(payload.Data.RollData))
	for _, raw := range payload.Data.RollData {
		tags := make([]string, 0, len(raw.Subjects))
		for _, subject := range raw.Subjects {
			if strings.TrimSpace(subject.Name) != "" {
				tags = append(tags, subject.Name)
			}
		}
		id := fmt.Sprint(raw.ID)
		if id == "" || id == "<nil>" {
			id = strconv.FormatInt(raw.CTime, 10)
		}
		item := foundation.NewsItem{
			ID:          id,
			Title:       firstNonEmpty(raw.Title, raw.Content),
			Content:     raw.Content,
			URL:         raw.ShareURL,
			PublishedAt: time.Unix(raw.CTime, 0),
			Tags:        tags,
			Meta:        meta,
		}
		if item.URL == "" && id != "" {
			item.URL = "https://www.cls.cn/telegraph/" + id
		}
		if item.Title == "" {
			continue
		}
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("cls returned no news")
	}
	return items, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
