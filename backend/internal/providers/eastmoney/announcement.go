package eastmoney

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
)

// LatestNews 拉取东方财富全市场最新公告（np-anotice-stock.eastmoney.com），
// 作为 easy-stock 实时资讯的第二源（与财联社快讯互补：公告 vs 快讯）。
// 满足 httpapi.NewsProvider 接口。
func (c *Client) LatestNews(ctx context.Context, limit int) ([]foundation.NewsItem, error) {
	if limit <= 0 {
		limit = 20
	}
	values := url.Values{}
	values.Set("sr", "-1")
	values.Set("page_size", strconv.Itoa(limit))
	values.Set("page_index", "1")
	values.Set("ann_type", "0")
	values.Set("client_source", "web")
	requestURL := c.announcementBaseURL + "/api/security/announcement/getAnnouncements?" + values.Encode()

	// 抗封：同主机最小间隔限速。
	if !foundation.Throttle(ctx, foundation.HostOf(requestURL), 120*time.Millisecond) {
		return nil, ctx.Err()
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	foundation.ApplyBrowserHeaders(req)
	req.Header.Set("Referer", "https://data.eastmoney.com/")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("eastmoney announcement http status %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			List []struct {
				ArtCode     string `json:"art_code"`
				Codes       string `json:"codes"`
				Title       string `json:"title"`
				NoticeDate  string `json:"notice_date"`
				AnnType     int    `json:"ann_type"`
				Attachments []struct {
					FileName string `json:"file_name"`
				} `json:"attachments"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, err
	}
	if !payload.Success {
		return nil, fmt.Errorf("eastmoney announcement success=false")
	}

	meta := foundation.SourceMeta{
		Source:    "eastmoney",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	items := make([]foundation.NewsItem, 0, len(payload.Data.List))
	for _, raw := range payload.Data.List {
		title := strings.TrimSpace(raw.Title)
		if title == "" || strings.TrimSpace(raw.ArtCode) == "" {
			continue
		}
		stockCode := trimMarketPrefix(raw.Codes)
		publishedAt, _ := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(raw.NoticeDate), time.Local)
		detailURL := fmt.Sprintf("https://data.eastmoney.com/notices/detail/%s/%s.html", stockCode, strings.TrimSpace(raw.ArtCode))
		tags := make([]string, 0, 1)
		if stockCode != "" {
			tags = append(tags, stockCode)
		}
		items = append(items, foundation.NewsItem{
			ID:          "em-" + strings.TrimSpace(raw.ArtCode),
			Title:       title,
			URL:         detailURL,
			PublishedAt: publishedAt,
			Tags:        tags,
			Meta:        meta,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("eastmoney returned no announcements")
	}
	return items, nil
}

func trimMarketPrefix(codes string) string {
	codes = strings.TrimSpace(codes)
	codes = strings.TrimPrefix(codes, "SH")
	codes = strings.TrimPrefix(codes, "SZ")
	codes = strings.TrimPrefix(codes, "BJ")
	return strings.TrimSpace(codes)
}
