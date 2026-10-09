package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

var stockNewsHTML = regexp.MustCompile(`<[^>]*>`)

// SearchStockNews returns dated search excerpts, never claims to have read the
// complete article. The caller verifies company identity and the analysis cutoff.
func (c *Client) SearchStockNews(ctx context.Context, symbol, query string, limit int) ([]foundation.NewsItem, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	keyword := strings.TrimSpace(query)
	if keyword == "" {
		keyword = strings.Split(normalized.Canonical, ".")[0]
	}
	limit = min(max(limit, 1), 30)
	param, err := json.Marshal(map[string]any{
		"uid": "", "keyword": keyword, "type": []string{"cmsArticleWebOld"},
		"client": "web", "clientType": "web", "clientVersion": "curr",
		"param": map[string]any{"cmsArticleWebOld": map[string]any{
			"searchScope": "default", "sort": "default", "pageIndex": 1, "pageSize": limit, "preTag": "", "postTag": "",
		}},
	})
	if err != nil {
		return nil, err
	}
	params := url.Values{"cb": {"easyStockNews"}, "param": {string(param)}}
	requestURL := c.newsSearchBaseURL + "/search/jsonp?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock")
	req.Header.Set("Referer", "https://so.eastmoney.com/")
	started := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("eastmoney stock news http status %d", resp.StatusCode)
	}
	const maxBytes = 2 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBytes {
		return nil, fmt.Errorf("eastmoney stock news response too large")
	}
	text := strings.TrimSpace(string(body))
	if strings.HasPrefix(text, "easyStockNews(") && strings.HasSuffix(strings.TrimSuffix(text, ";"), ")") {
		text = strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(text, "easyStockNews("), ";"), ")")
	}
	var payload struct {
		Code   int `json:"code"`
		Result struct {
			Items []struct {
				ID      string `json:"code"`
				Title   string `json:"title"`
				Content string `json:"content"`
				Date    string `json:"date"`
				Media   string `json:"mediaName"`
				URL     string `json:"url"`
			} `json:"cmsArticleWebOld"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, fmt.Errorf("decode stock news: %w", err)
	}
	if payload.Code != 0 {
		return nil, fmt.Errorf("eastmoney stock news code=%d", payload.Code)
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	items := []foundation.NewsItem{}
	seen := map[string]bool{}
	clean := func(value string) string {
		return strings.TrimSpace(html.UnescapeString(stockNewsHTML.ReplaceAllString(value, "")))
	}
	for _, row := range payload.Result.Items {
		if len(items) >= limit {
			break
		}
		title, content := clean(row.Title), clean(row.Content)
		if title == "" || seen[title] {
			continue
		}
		seen[title] = true
		published, _ := time.ParseInLocation("2006-01-02 15:04:05", row.Date, zone)
		if published.IsZero() {
			published, _ = time.ParseInLocation("2006-01-02", row.Date, zone)
		}
		if runes := []rune(content); len(runes) > 2400 {
			content = string(runes[:2400])
		}
		items = append(items, foundation.NewsItem{ID: row.ID, Title: title,
			Content: "新闻检索摘要（非全文，第三方报道需核实）：\n" + content, URL: row.URL, PublishedAt: published,
			Meta: foundation.SourceMeta{Source: "eastmoney:stock-news-search:" + clean(row.Media), SourceURL: requestURL, FetchedAt: time.Now().UTC(), LatencyMS: time.Since(started).Milliseconds()},
		})
	}
	return items, nil
}
