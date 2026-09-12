// Package xueqiu 提供雪球社区数据的只读访问：A 股热榜、个股热议 KOL 与 7×24 快讯。
// 访问策略移植自开源项目 dsh-xueqiu 的实测经验：
//   - 匿名 cookie 播种（优先 https://xueqiu.com/hq，无 WAF 挑战且直接下发全套 token）；
//   - error_code 400016 表示 cookie 失效，需重播种后重试；400017/限频走指数退避；
//   - 请求节流：并发 2 + 最小间隔 100ms，对齐雪球网页端行为；
//   - TTL 缓存 + in-flight 去重，避免重复打上游。
package xueqiu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	userAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	stockBase       = "https://stock.xueqiu.com"
	siteBase        = "https://www.xueqiu.com"
	referer         = "https://www.xueqiu.com/"
	minGap          = 100 * time.Millisecond
	maxConcurrency  = 2
	requestTimeout  = 15 * time.Second
	maxCacheEntries = 200
)

// HotStock 雪球热榜条目（按社区讨论热度排名，而非涨幅）。
type HotStock struct {
	Symbol     string  `json:"symbol"`
	Name       string  `json:"name"`
	Current    float64 `json:"current"`
	Percent    float64 `json:"percent"`
	Rank       int     `json:"rank"`
	RankChange int     `json:"rank_change"`
	Increase   float64 `json:"increment"`
	Exchange   string  `json:"exchange,omitempty"`
}

// HotUser 个股热议 KOL：雪球社区里讨论该股最热的用户。
type HotUser struct {
	ID          json.Number `json:"id"`
	ScreenName  string      `json:"screen_name"`
	Description string      `json:"description,omitempty"`
	Followers   int64       `json:"followers_count"`
	Statuses    int64       `json:"status_count,omitempty"`
	Verified    bool        `json:"verified"`
}

// News 雪球 7×24 快讯条目；Mark 为 1 表示重要。
type News struct {
	ID        json.Number `json:"id"`
	Text      string      `json:"text"`
	Mark      int         `json:"mark,omitempty"`
	CreatedAt int64       `json:"created_at"`
	Target    string      `json:"target,omitempty"`
}

type cacheEntry struct {
	at    time.Time
	value any
}

// Client 是雪球只读客户端，零配置可用（匿名 cookie 自动播种）。
type Client struct {
	http *http.Client

	mu        sync.Mutex
	cookie    string
	lastStart time.Time
	sem       chan struct{}
	cache     map[string]cacheEntry
	inflight  map[string]*inflightCall
	now       func() time.Time
}

type inflightCall struct {
	done  chan struct{}
	value any
	err   error
}

// NewClient 构造客户端；nil 参数使用默认 http.Client。
func NewClient() *Client {
	return &Client{
		http:     &http.Client{Timeout: requestTimeout},
		sem:      make(chan struct{}, maxConcurrency),
		cache:    make(map[string]cacheEntry),
		inflight: make(map[string]*inflightCall),
		now:      time.Now,
	}
}

// ---- cookie 播种：/hq 优先（无 WAF 挑战、直接发匿名 token），首页兜底 ----

var seedURLs = []string{"https://xueqiu.com/hq", "https://www.xueqiu.com/"}

func (c *Client) ensureCookie(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	if c.cookie != "" && !force {
		cookie := c.cookie
		c.mu.Unlock()
		return cookie, nil
	}
	c.mu.Unlock()

	var pairs []string
	for _, seed := range seedURLs {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, seed, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := c.http.Do(req)
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		pairs = pairs[:0]
		for _, cookie := range resp.Cookies() {
			if cookie.Value == "" {
				continue
			}
			if !seen[cookie.Name] {
				seen[cookie.Name] = true
				pairs = append(pairs, cookie.Name+"="+cookie.Value)
			}
		}
		_ = resp.Body.Close()
		if seen["xq_a_token"] {
			break
		}
	}
	// kline 等端点要求 cookie 里存在 u=<id>；缺失时补随机值
	hasU := false
	for _, pair := range pairs {
		if strings.HasPrefix(pair, "u=") {
			hasU = true
			break
		}
	}
	if !hasU {
		pairs = append(pairs, fmt.Sprintf("u=%d%d", time.Now().UnixMilli(), rand.Int63n(1_000_000)))
	}
	cookie := strings.Join(pairs, "; ")
	c.mu.Lock()
	c.cookie = cookie
	c.mu.Unlock()
	return cookie, nil
}

// ---- 请求节流：并发 2 + 最小间隔 100ms ----

func (c *Client) throttle(ctx context.Context) func() {
	c.sem <- struct{}{}
	c.mu.Lock()
	wait := minGap - c.now().Sub(c.lastStart)
	if wait > 0 {
		c.lastStart = c.lastStart.Add(minGap)
	} else {
		c.lastStart = c.now()
		wait = 0
	}
	c.mu.Unlock()
	timer := time.NewTimer(wait)
	select {
	case <-ctx.Done():
		timer.Stop()
		<-c.sem
		return func() {}
	case <-timer.C:
	}
	return func() { <-c.sem }
}

// ---- 错误分类（对齐 dsh-xueqiu 实测口径）----

type apiError struct {
	Kind      string // cookie_expired / rate_limited / api
	Code      string
	Retryable bool
}

func (e *apiError) Error() string {
	if e == nil {
		return ""
	}
	return e.Kind + ": " + e.Code
}

func classifyAPIError(payload map[string]any) *apiError {
	rawCode, _ := payload["error_code"].(float64)
	code := strconv.Itoa(int(rawCode))
	if code == "0" || rawCode == 0 {
		return nil
	}
	desc, _ := payload["error_description"].(string)
	switch {
	case code == "400016":
		return &apiError{Kind: "cookie_expired", Code: code, Retryable: true}
	case code == "400017" || strings.Contains(desc, "频繁") || strings.Contains(strings.ToLower(desc), "too many"):
		return &apiError{Kind: "rate_limited", Code: code, Retryable: true}
	default:
		return &apiError{Kind: "api", Code: code, Retryable: false}
	}
}

// ---- 缓存 + in-flight 去重 ----

func (c *Client) cacheGet(key string, ttl time.Duration) (any, bool) {
	if ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok || c.now().Sub(entry.at) >= ttl {
		return nil, false
	}
	return entry.value, true
}

func (c *Client) cachePut(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = cacheEntry{at: c.now(), value: value}
	if len(c.cache) > maxCacheEntries {
		for key, entry := range c.cache {
			if c.now().Sub(entry.at) > 24*time.Hour {
				delete(c.cache, key)
			}
		}
	}
	for len(c.cache) > maxCacheEntries {
		for key := range c.cache {
			delete(c.cache, key)
			break
		}
	}
}

// getJSON 请求雪球 JSON 接口：自动播种 cookie、400016 自愈、限频退避、缓存去重。
// 返回值可能是对象或顶层数组（雪球部分接口如 stock_hot_user 返回裸数组），由调用方断言。
func (c *Client) getJSON(ctx context.Context, base, path string, params map[string]string, ttl time.Duration) (any, error) {
	query := make([]string, 0, len(params))
	for key, value := range params {
		if value == "" {
			continue
		}
		query = append(query, key+"="+value)
	}
	// map 遍历顺序随机：不排序的话同一逻辑请求会拼出参数顺序不同的 URL，
	// 缓存与 in-flight 去重的 key 都会分裂（键唯一，按整体排序即按键排序）。
	sort.Strings(query)
	url := base + path
	if len(query) > 0 {
		url += "?" + strings.Join(query, "&")
	}
	if hit, ok := c.cacheGet(url, ttl); ok {
		return hit, nil
	}

	// in-flight 去重：同一 URL 的并发调用共享同一次上游请求
	c.mu.Lock()
	if call, ok := c.inflight[url]; ok {
		c.mu.Unlock()
		<-call.done
		return call.value, call.err
	}
	call := &inflightCall{done: make(chan struct{})}
	c.inflight[url] = call
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.inflight, url)
		c.mu.Unlock()
		close(call.done)
	}()

	value, err := c.fetchWithRetry(ctx, url)
	if err == nil && ttl > 0 {
		c.cachePut(url, value)
	}
	call.value, call.err = value, err
	return value, err
}

func (c *Client) fetchWithRetry(ctx context.Context, url string) (any, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		forceReseed := attempt > 0 && lastErr != nil &&
			(strings.Contains(lastErr.Error(), "cookie_expired") || strings.Contains(lastErr.Error(), "empty"))
		cookie, err := c.ensureCookie(ctx, forceReseed)
		if err != nil {
			return nil, err
		}
		value, err := c.fetchOnce(ctx, url, cookie)
		if err == nil {
			return value, nil
		}
		lastErr = err
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			if !apiErr.Retryable {
				return nil, err
			}
			continue // fetchOnce 内部已处理重播种/退避前的分类
		}
	}
	return nil, lastErr
}

func (c *Client) fetchOnce(ctx context.Context, url, cookie string) (any, error) {
	release := c.throttle(ctx)
	defer release()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	// 空响应是雪球风控的常见形态（dsh-xueqiu 同款处理）：归类为可重试并触发重播种。
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, &apiError{Kind: "empty", Retryable: true}
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("雪球响应解析失败: %w", err)
	}
	if payloadMap, ok := payload.(map[string]any); ok {
		if apiErr := classifyAPIError(payloadMap); apiErr != nil {
			if apiErr.Kind == "cookie_expired" {
				// 失效 cookie 立即作废，下一次尝试会重新播种
				c.mu.Lock()
				c.cookie = ""
				c.mu.Unlock()
			}
			return nil, fmt.Errorf("[%s] 雪球错误 %s: %w", apiErr.Kind, apiErr.Code, apiErr)
		}
	}
	return payload, nil
}
