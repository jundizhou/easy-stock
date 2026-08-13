package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxArticleBytes = 4 << 20

var (
	tagPattern           = regexp.MustCompile(`(?is)<[^>]+>`)
	unsafePattern        = regexp.MustCompile(`(?is)<(?:script|style|noscript|svg)[^>]*>.*?</(?:script|style|noscript|svg)>`)
	lineBreakPattern     = regexp.MustCompile(`(?is)</?(p|div|article|section|h[1-6]|li|br)[^>]*>`)
	titlePattern         = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	publishedPattern     = regexp.MustCompile(`(?is)(?:publish_time|create_time|\bct\b)\s*[:=]\s*["']?(\d{10,13})`)
	publishedDatePattern = regexp.MustCompile(`(?is)["']datePublished["']\s*:\s*["']([^"']+)["']`)
	spacePattern         = regexp.MustCompile(`[\t\r ]+`)
	blankLinePattern     = regexp.MustCompile(`\n{3,}`)
)

type Importer struct {
	httpClient   *http.Client
	wechatAPIURL string
}

func NewImporter(httpClient *http.Client, wechatAPIURL string) *Importer {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	return &Importer{httpClient: httpClient, wechatAPIURL: strings.TrimRight(strings.TrimSpace(wechatAPIURL), "/")}
}

func (i *Importer) ImportURL(ctx context.Context, rawURL string) (Post, error) {
	parsed, source, err := classifyURL(rawURL)
	if err != nil {
		return Post{}, err
	}
	if source == "wechat" && i.wechatAPIURL != "" {
		if post, sidecarErr := i.importWeChat(ctx, parsed.String()); sidecarErr == nil {
			return post, nil
		}
	}
	return i.importPublicPage(ctx, parsed.String(), source, nil)
}

func (i *Importer) ImportURLWithHeaders(ctx context.Context, rawURL string, headers http.Header) (Post, error) {
	parsed, source, err := classifyURL(rawURL)
	if err != nil {
		return Post{}, err
	}
	return i.importPublicPage(ctx, parsed.String(), source, headers)
}

func (i *Importer) importWeChat(ctx context.Context, articleURL string) (Post, error) {
	body, _ := json.Marshal(map[string]string{"url": articleURL})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.wechatAPIURL+"/api/article", bytes.NewReader(body))
	if err != nil {
		return Post{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := i.httpClient.Do(req)
	if err != nil {
		return Post{}, fmt.Errorf("微信公众号解析服务不可用: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Post{}, fmt.Errorf("微信公众号解析服务返回 HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Title          string   `json:"title"`
			PlainContent   string   `json:"plain_content"`
			Author         string   `json:"author"`
			PublishTime    int64    `json:"publish_time"`
			PublishTimeStr string   `json:"publish_time_str"`
			Images         []string `json:"images"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxArticleBytes)).Decode(&payload); err != nil {
		return Post{}, fmt.Errorf("解析微信公众号响应: %w", err)
	}
	if !payload.Success {
		return Post{}, errors.New(firstNonEmpty(payload.Error, "微信公众号文章解析失败"))
	}
	if payload.Data.PublishTime <= 0 {
		return Post{}, errors.New("微信公众号响应没有可靠的发布时间，为避免旧文章被误判为今日内容，本次不导入")
	}
	publishedAt := time.Unix(payload.Data.PublishTime, 0)
	return newPost("wechat", articleURL, payload.Data.Author, payload.Data.Title, payload.Data.PlainContent, "", publishedAt), nil
}

func (i *Importer) importPublicPage(ctx context.Context, articleURL, source string, headers http.Header) (Post, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, articleURL, nil)
	if err != nil {
		return Post{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/124 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := i.httpClient.Do(req)
	if err != nil {
		return Post{}, fmt.Errorf("读取原文失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Post{}, fmt.Errorf("原文返回 HTTP %d；该平台可能需要登录或触发了风控", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxArticleBytes+1))
	if err != nil {
		return Post{}, fmt.Errorf("读取原文: %w", err)
	}
	if len(body) > maxArticleBytes {
		return Post{}, errors.New("原文超过 4MB，暂不导入")
	}
	htmlText := string(body)
	title := firstNonEmpty(metaValue(htmlText, "property", "og:title"), metaValue(htmlText, "name", "twitter:title"), matchText(titlePattern, htmlText))
	title = cleanInline(title)
	if title == "" {
		return Post{}, errors.New("没有从页面识别到文章标题")
	}
	author := cleanInline(firstNonEmpty(metaValue(htmlText, "name", "author"), metaValue(htmlText, "property", "article:author"), sourceName(source)))
	description := cleanInline(firstNonEmpty(metaValue(htmlText, "property", "og:description"), metaValue(htmlText, "name", "description")))
	cover := strings.TrimSpace(metaValue(htmlText, "property", "og:image"))
	content := cleanDocument(htmlText)
	if len([]rune(content)) > 120000 {
		content = string([]rune(content)[:120000])
	}
	if description == "" {
		description = truncateRunes(content, 180)
	}
	finalURL := articleURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	publishedAt, ok := publicPagePublishedAt(htmlText)
	if !ok {
		return Post{}, errors.New("没有从原文识别到可靠的发布时间，为避免旧文章被误判为今日内容，本次不导入")
	}
	post := newPost(source, finalURL, author, title, content, cover, publishedAt)
	post.Digest = description
	return post, nil
}

func publicPagePublishedAt(document string) (time.Time, bool) {
	for _, attribute := range []string{"article:published_time", "og:published_time", "datePublished", "publishdate", "pubdate"} {
		if value := strings.TrimSpace(firstNonEmpty(metaValue(document, "property", attribute), metaValue(document, "name", attribute), metaValue(document, "itemprop", attribute))); value != "" {
			for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
				if parsed, err := time.ParseInLocation(layout, value, shanghaiLocation()); err == nil {
					return parsed, true
				}
			}
		}
	}
	if match := publishedDatePattern.FindStringSubmatch(document); len(match) > 1 {
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
			if parsed, err := time.ParseInLocation(layout, strings.TrimSpace(match[1]), shanghaiLocation()); err == nil {
				return parsed, true
			}
		}
	}
	match := publishedPattern.FindStringSubmatch(document)
	if len(match) < 2 {
		return time.Time{}, false
	}
	seconds := strings.TrimSpace(match[1])
	if len(seconds) == 13 {
		seconds = seconds[:10]
	}
	unixSeconds, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil || unixSeconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(unixSeconds, 0), true
}

func classifyURL(raw string) (*url.URL, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, "", errors.New("请输入有效的 http/https 文章链接")
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "mp.weixin.qq.com":
		return parsed, "wechat", nil
	case host == "xueqiu.com" || strings.HasSuffix(host, ".xueqiu.com"):
		return parsed, "xueqiu", nil
	case host == "taoguba.com.cn" || strings.HasSuffix(host, ".taoguba.com.cn") || host == "tgb.cn" || strings.HasSuffix(host, ".tgb.cn"):
		return parsed, "taoguba", nil
	default:
		return nil, "", errors.New("目前只支持微信公众号、雪球和淘股吧文章链接")
	}
}

func newPost(source, originalURL, author, title, content, cover string, publishedAt time.Time) Post {
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte(source + "\n" + originalURL))
	id := hex.EncodeToString(hash[:16])
	author = firstNonEmpty(strings.TrimSpace(author), sourceName(source))
	authorHash := sha256.Sum256([]byte(source + "\n" + author))
	return Post{
		ID:            id,
		Source:        source,
		ExternalID:    id,
		AuthorID:      hex.EncodeToString(authorHash[:12]),
		AuthorName:    author,
		Title:         strings.TrimSpace(title),
		Digest:        truncateRunes(content, 180),
		ContentText:   strings.TrimSpace(content),
		CoverURL:      cover,
		OriginalURL:   originalURL,
		PublishedAt:   publishedAt.UTC(),
		FetchedAt:     now,
		RelatedStocks: []string{},
		RelatedThemes: []string{},
	}
}

func metaValue(document, attribute, value string) string {
	quoted := regexp.QuoteMeta(value)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?is)<meta[^>]+` + attribute + `=["']` + quoted + `['"][^>]+content=["']([^"']*)["'][^>]*>`),
		regexp.MustCompile(`(?is)<meta[^>]+content=["']([^"']*)["'][^>]+` + attribute + `=["']` + quoted + `['"][^>]*>`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(document); len(match) > 1 {
			return html.UnescapeString(match[1])
		}
	}
	return ""
}

func matchText(pattern *regexp.Regexp, document string) string {
	match := pattern.FindStringSubmatch(document)
	if len(match) < 2 {
		return ""
	}
	return html.UnescapeString(tagPattern.ReplaceAllString(match[1], ""))
}

func cleanDocument(document string) string {
	document = unsafePattern.ReplaceAllString(document, " ")
	document = lineBreakPattern.ReplaceAllString(document, "\n")
	document = tagPattern.ReplaceAllString(document, " ")
	document = html.UnescapeString(document)
	document = strings.ReplaceAll(document, "\u00a0", " ")
	lines := strings.Split(document, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(spacePattern.ReplaceAllString(line, " "))
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return blankLinePattern.ReplaceAllString(strings.Join(cleaned, "\n"), "\n\n")
}

func cleanInline(value string) string {
	return strings.TrimSpace(spacePattern.ReplaceAllString(html.UnescapeString(tagPattern.ReplaceAllString(value, "")), " "))
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func sourceName(source string) string {
	switch source {
	case "wechat":
		return "微信公众号"
	case "xueqiu":
		return "雪球作者"
	case "taoguba":
		return "淘股吧作者"
	case "official":
		return "每日复盘"
	default:
		return source
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
