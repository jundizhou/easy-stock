package eastmoney

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

const (
	// 东方财富板块资金流榜（data.eastmoney.com/bkzj）用 fs 过滤概念与行业板块。
	sectorFlowConceptFilter  = "m:90+t:3+f:!50"
	sectorFlowIndustryFilter = "m:90+t:2+f:!50"

	sectorFlowRankSource     = "eastmoney:bkzj"
	sectorFlowIntradaySource = "eastmoney:board-intraday-flow"
	sectorPriceIntradaySource = "eastmoney:board-intraday-trend"

	// 榜单最多返回 600 行，覆盖概念（约 508）与行业（约 496）两个维度。
	sectorFlowRankLimit = 600
	// 分时曲线一次最多拉取 12 个板块，避免前端叠加过多序列。
	sectorFlowIntradayLimit = 12
	// 榜单与分时都按并发上限抓取，降低被限流的概率。
	sectorFlowRankConcurrent = 3
	// 分时资金流串行抓取：东财对突发请求会直接断连（营销看板项目实测同样如此，
	// 见其 server/sources/eastmoney.cjs 的串行队列 + 节流），并发请求很容易触发临时封禁。
	sectorFlowIntradayConcurrent = 1
	// 相邻两次分时请求之间的最小间隔，进一步降低触发风控的概率。
	sectorFlowIntradayInterval = 400 * time.Millisecond
	// 相邻两次镜像尝试之间的间隔：镜像轮换只在主入口失败时发生，
	// 连着打多个镜像同样会被当成突发流量，这里留出和板块间一致的节奏。
	sectorFlowIntradayMirrorInterval = 400 * time.Millisecond
	// 分时序列按板块缓存，前端逐个勾选板块时只抓新增的代码。
	sectorFlowIntradayBoardTTL = 45 * time.Second

	// 分时资金流连续失败到阈值后熔断一段时间，避免持续打已不可用的上游路径。
	sectorFlowFailureThreshold = 2
	sectorFlowCooldownWindow   = 90 * time.Second

	// sectorFlowIntradayPath 是分时（当日逐分钟）累计资金流的接口路径。
	sectorFlowIntradayPath = "/api/qt/stock/fflow/kline/get"
	// sectorFlowIntradayToken 是东财页面调用该接口时携带的 ut 参数，
	// 这里保持一致，避免成为风控判定的差异项。
	sectorFlowIntradayToken = "b2884a393a59ad64002292a3e90d46a5"

	// sectorPriceIntradayPath 是板块分时行情（逐分钟价格）的接口路径。
	//
	// 与分时资金流不同：上游是按路径拦截的，fflow/kline 在缺少会话时会被直接断连，
	// 而 trends2 这条路径可以匿名取到（2026-10-18 复测：8/8 成功），
	// 因此价格分时不需要浏览器会话，作为资金曲线的走势对照。
	sectorPriceIntradayPath = "/api/qt/stock/trends2/get"
	// 价格分时按板块缓存，窗口比分时资金流更短。
	sectorPriceIntradayBoardTTL = 30 * time.Second
	// 分时行情接口比资金流宽松，允许小幅并发。
	sectorPriceIntradayConcurrent = 3
)

// sectorFlowRankMetricKeys 是需要从 bkzj 拉取的字段。
//
// f62 是主口径（今日主力净流入），失败即整体失败；其余字段失败只降级提示，
// 榜单仍然可用。f3 涨幅、f184 主力净占比、f164/f174 为 5 日/10 日累计净流入，
// f165/f175 为对应的 5 日/10 日主力净占比，f66/f72/f84 为超大单、大单、小单净流入
// （中单由四类资金净额之和为零反推）。
var sectorFlowRankMetricKeys = []string{"f62", "f3", "f184", "f66", "f72", "f84", "f164", "f165", "f174", "f175"}

type sectorFlowRankPayload struct {
	RC   int `json:"rc"`
	Data struct {
		Total int                 `json:"total"`
		Diff  []sectorFlowRankRow `json:"diff"`
	} `json:"data"`
}

// sectorFlowRankRow 覆盖 bkzj 返回的所有候选字段；每次请求只会带回 key 指定的那一个字段。
type sectorFlowRankRow struct {
	Code         string        `json:"f12"`
	Name         string        `json:"f14"`
	ChangePct    flexibleFloat `json:"f3"`
	Main         flexibleFloat `json:"f62"`
	MainRatio    flexibleFloat `json:"f184"`
	SuperLarge   flexibleFloat `json:"f66"`
	Large        flexibleFloat `json:"f72"`
	Small        flexibleFloat `json:"f84"`
	FiveDayMain  flexibleFloat `json:"f164"`
	FiveDayRatio flexibleFloat `json:"f165"`
	TenDayMain   flexibleFloat `json:"f174"`
	TenDayRatio  flexibleFloat `json:"f175"`
}

type sectorFlowMetricResult struct {
	key        string
	rows       map[string]sectorFlowRankRow
	requestURL string
	err        error
}

type sectorFlowKLinePayload struct {
	RC   int `json:"rc"`
	Data struct {
		Code   string   `json:"code"`
		Name   string   `json:"name"`
		Klines []string `json:"klines"`
	} `json:"data"`
}

// sectorPriceTrendPayload 是东财分时行情（trends2）的返回结构。
//
// trends 每行形如「2026-10-16 09:30,2468.24,2468.24,2468.24,2461.28,623298,2815619168.00,2468.238」，
// 字段依次为 时间、开、收、高、低、成交量、成交额、均价。
type sectorPriceTrendPayload struct {
	RC   int `json:"rc"`
	Data struct {
		Code     string        `json:"code"`
		Name     string        `json:"name"`
		PreClose flexibleFloat `json:"preClose"`
		Trends   []string      `json:"trends"`
	} `json:"data"`
}

// sectorFlowCooldown 记录分时资金流的连续失败次数。
//
// 分时资金流走的是 push2 的专用路径，可用镜像比其它报价路径窄，
// 这里做一层熔断：确认所有镜像都不可用时短时间内不再打上游，让接口快速回落到缓存。
type sectorFlowCooldown struct {
	mu           sync.Mutex
	failures     int
	blockedUntil time.Time
}

func (c *sectorFlowCooldown) blocked() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.blockedUntil.IsZero() || time.Now().After(c.blockedUntil) {
		return 0, false
	}
	return time.Until(c.blockedUntil), true
}

func (c *sectorFlowCooldown) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	if c.failures >= sectorFlowFailureThreshold {
		c.failures = 0
		c.blockedUntil = time.Now().Add(sectorFlowCooldownWindow)
	}
}

func (c *sectorFlowCooldown) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = 0
	c.blockedUntil = time.Time{}
}

// sectorFlowSessionRefresh 限制会话重取的频率。
//
// 会话重取会让桌面端在隐藏窗口里把东财页面重新加载一遍（秒级），
// 多板块连续失败时没有必要逐个触发，因此按时间窗口去重。
type sectorFlowSessionRefresh struct {
	mu   sync.Mutex
	last time.Time
}

// sectorFlowSessionRefreshWindow 是两次会话重取之间的最小间隔。
const sectorFlowSessionRefreshWindow = 60 * time.Second

func (r *sectorFlowSessionRefresh) allow(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.last.IsZero() && now.Sub(r.last) < sectorFlowSessionRefreshWindow {
		return false
	}
	r.last = now
	return true
}

// SectorFlowRanking 返回板块资金流榜单，dimension 支持 concept（题材/概念）与 industry。
func (c *Client) SectorFlowRanking(ctx context.Context, dimension string, limit int) ([]foundation.SectorFlowRank, foundation.SourceMeta, error) {
	filter, _, err := sectorFlowFilter(dimension)
	if err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > sectorFlowRankLimit {
		limit = sectorFlowRankLimit
	}

	start := time.Now()
	meta := foundation.SourceMeta{Source: sectorFlowRankSource, FetchedAt: time.Now()}

	results := make([]sectorFlowMetricResult, len(sectorFlowRankMetricKeys))
	var waitGroup sync.WaitGroup
	semaphore := make(chan struct{}, sectorFlowRankConcurrent)
	for index, key := range sectorFlowRankMetricKeys {
		waitGroup.Add(1)
		go func(index int, key string) {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			rows, requestURL, fetchErr := c.fetchSectorFlowRankMetric(ctx, filter, key)
			results[index] = sectorFlowMetricResult{key: key, rows: rows, requestURL: requestURL, err: fetchErr}
		}(index, key)
	}
	waitGroup.Wait()

	requestURLs := make([]string, 0, len(results))
	primaryURL := ""
	degraded := make([]string, 0, len(results))
	ranks := make(map[string]*foundation.SectorFlowRank)
	for _, result := range results {
		if result.requestURL != "" {
			requestURLs = append(requestURLs, result.requestURL)
		}
		if result.err != nil {
			if result.key == "f62" {
				return nil, foundation.SourceMeta{}, fmt.Errorf("eastmoney sector flow ranking failed: %w", result.err)
			}
			degraded = append(degraded, result.key)
			continue
		}
		if result.key == "f62" {
			primaryURL = result.requestURL
		}
		for code, row := range result.rows {
			rank, exists := ranks[code]
			if !exists {
				// 榜单主体由今日主力净流入（f62）决定，其余字段只做补充。
				if result.key != "f62" {
					continue
				}
				rank = &foundation.SectorFlowRank{Code: code, Name: strings.TrimSpace(row.Name)}
				ranks[code] = rank
			}
			applySectorFlowMetric(rank, result.key, row)
		}
	}

	items := make([]foundation.SectorFlowRank, 0, len(ranks))
	for _, rank := range ranks {
		if rank.Name == "" {
			rank.Name = rank.Code
		}
		// 超大单、大单、中单、小单净额之和恒为零，中单由其余三类反推；
		// 未取到明细时三者均为 0，反推结果同样是 0。
		rank.MediumNet = -(rank.SuperLargeNet + rank.LargeNet + rank.SmallNet)
		items = append(items, *rank)
	}
	if len(items) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("eastmoney sector flow ranking returned no boards")
	}
	sort.SliceStable(items, func(left int, right int) bool {
		if items[left].MainNetInflow == items[right].MainNetInflow {
			return items[left].Code < items[right].Code
		}
		return items[left].MainNetInflow > items[right].MainNetInflow
	})
	if len(items) > limit {
		items = items[:limit]
	}

	// SourceURL 只保留定义榜单口径的主力净流入请求，完整字段列表见 available_fields，
	// 避免把 8 个请求 URL 复制到每一行。
	if primaryURL != "" {
		meta.SourceURL = primaryURL
	} else {
		meta.SourceURL = strings.Join(requestURLs, ",")
	}
	meta.LatencyMS = time.Since(start).Milliseconds()
	meta.AvailableFields = sectorFlowRankFields(items)
	if len(degraded) > 0 {
		meta.FallbackReason = fmt.Sprintf("部分板块资金字段暂不可用（%s），已按可用字段返回", strings.Join(degraded, ","))
	}
	for index := range items {
		items[index].Meta = meta
	}
	return items, meta, nil
}

// SectorFlowIntraday 返回多个板块当日分时累计净流入序列，codes 为 BK 板块代码。
func (c *Client) SectorFlowIntraday(ctx context.Context, codes []string) ([]foundation.SectorFlowSeries, foundation.SourceMeta, error) {
	normalized, err := normalizeSectorFlowCodes(codes)
	if err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	if remaining, blocked := c.sectorFlowCooldown.blocked(); blocked {
		return nil, foundation.SourceMeta{}, fmt.Errorf("板块分时资金上游暂不可用，约 %.0f 秒后自动重试", remaining.Seconds())
	}

	start := time.Now()
	meta := foundation.SourceMeta{Source: sectorFlowIntradaySource, FetchedAt: time.Now()}

	// 先按板块命中缓存：前端逐个勾选板块会反复请求递增的代码组合，
	// 只抓缺失或过期的板块可以把上游请求量压到最低。
	cached, pending := c.sectorFlowIntradayCached(normalized, time.Now())

	series := make([]foundation.SectorFlowSeries, len(pending))
	usedURLs := make([]string, len(pending))
	failures := make([]error, len(pending))
	var waitGroup sync.WaitGroup
	semaphore := make(chan struct{}, sectorFlowIntradayConcurrent)
	for index, code := range pending {
		waitGroup.Add(1)
		go func(index int, code string) {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			value, requestURL, loadErr := c.sectorFlowIntradayOne(ctx, code)
			series[index], usedURLs[index], failures[index] = value, requestURL, loadErr
			// 串行抓取时给上游留出间隔，避免连续请求被判定为突发流量。
			time.Sleep(sectorFlowIntradayInterval)
		}(index, code)
	}
	waitGroup.Wait()

	fetched := make(map[string]foundation.SectorFlowSeries, len(pending))
	failed := make(map[string]error, len(pending))
	for index, code := range pending {
		if failures[index] != nil {
			failed[code] = failures[index]
			continue
		}
		fetched[code] = series[index]
		c.storeSectorFlowIntraday(code, series[index], usedURLs[index])
	}

	items := make([]foundation.SectorFlowSeries, 0, len(pending))
	skipped := make([]string, 0, len(pending))
	cachedURL := ""
	var lastErr error
	for _, code := range normalized {
		if entry, ok := cached[code]; ok {
			items = append(items, entry.series)
			if cachedURL == "" {
				cachedURL = entry.sourceURL
			}
			continue
		}
		if value, ok := fetched[code]; ok {
			items = append(items, value)
			continue
		}
		lastErr = failed[code]
		skipped = append(skipped, code)
	}
	if len(items) == 0 {
		c.sectorFlowCooldown.recordFailure()
		if lastErr == nil {
			lastErr = fmt.Errorf("eastmoney intraday flow returned no series")
		}
		return nil, foundation.SourceMeta{}, lastErr
	}
	c.sectorFlowCooldown.reset()

	tradeDate := items[0].TradeDate
	meta.TradeDate = tradeDate
	meta.SourceURL = c.quoteBaseURL + sectorFlowIntradayPath
	for _, usedURL := range usedURLs {
		if usedURL != "" {
			// 记录实际命中的镜像，便于排查某个镜像被上游摘掉的情况。
			meta.SourceURL = usedURL
			break
		}
	}
	// 全部命中缓存时没有新的请求 URL，沿用缓存条目记录的上游地址。
	if cachedURL != "" && (len(usedURLs) == 0 || meta.SourceURL == c.quoteBaseURL+sectorFlowIntradayPath) {
		meta.SourceURL = cachedURL
	}
	meta.LatencyMS = time.Since(start).Milliseconds()
	meta.AvailableFields = []string{"main", "super_large", "large", "medium", "small"}
	if len(skipped) > 0 {
		meta.FallbackReason = fmt.Sprintf("部分板块分时资金不可用（%s），已跳过", strings.Join(skipped, ","))
	}
	for index := range items {
		items[index].Meta = meta
		if items[index].TradeDate == "" {
			items[index].TradeDate = tradeDate
		}
	}
	return items, meta, nil
}

func (c *Client) fetchSectorFlowRankMetric(ctx context.Context, filter string, key string) (map[string]sectorFlowRankRow, string, error) {
	endpoint := c.dataBaseURL + "/dataapi/bkzj/getbkzj"
	params := url.Values{}
	params.Set("key", key)
	params.Set("code", filter)
	requestURL := endpoint + "?" + params.Encode()

	var payload sectorFlowRankPayload
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, requestURL, err
	}
	if payload.RC != 0 {
		return nil, requestURL, fmt.Errorf("eastmoney bkzj rc=%d", payload.RC)
	}
	if len(payload.Data.Diff) == 0 {
		return nil, requestURL, fmt.Errorf("eastmoney bkzj returned no boards for key %s", key)
	}

	rows := make(map[string]sectorFlowRankRow, len(payload.Data.Diff))
	for _, row := range payload.Data.Diff {
		code := strings.ToUpper(strings.TrimSpace(row.Code))
		if code == "" {
			continue
		}
		rows[code] = row
	}
	return rows, requestURL, nil
}

func (c *Client) sectorFlowIntradayOne(ctx context.Context, code string) (foundation.SectorFlowSeries, string, error) {
	params := url.Values{}
	params.Set("lmt", "0")
	params.Set("klt", "1")
	params.Set("secid", "90."+code)
	params.Set("fields1", "f1,f2,f3,f7")
	params.Set("fields2", "f51,f52,f53,f54,f55,f56")
	params.Set("ut", sectorFlowIntradayToken)
	query := params.Encode()

	// 该路径在上游有反爬门禁：缺少东财会话（nid18）时连接会被直接断开。
	// 会话由桌面端在真实浏览器里取得后转交，这里只负责带上。
	cookie := c.sessionCookie(ctx, false)
	payload, usedURL, lastErr := c.fetchSectorFlowIntraday(ctx, query, code, cookie)
	if lastErr != nil {
		// 全部镜像都失败，通常是当前这份会话被上游临时拒绝（连接被直接断开）。
		// 桌面端重新走一遍东财页面即可恢复，这里换一份会话、只打主入口再试一次：
		// nid18 本身不会因为刷新而变，起作用的是页面回访带来的现场状态。
		if host, refreshed, ok := c.refreshSectorFlowSession(ctx, cookie); ok {
			if candidate, retryErr := c.fetchSectorFlowIntradayHost(ctx, host, query, code, refreshed); retryErr == nil {
				payload, usedURL, lastErr = candidate, host+sectorFlowIntradayPath+"?"+query, nil
			}
		}
	}
	if lastErr != nil {
		return foundation.SectorFlowSeries{}, "", lastErr
	}

	points := make([]foundation.SectorFlowPoint, 0, len(payload.Data.Klines))
	tradeDate := ""
	for _, line := range payload.Data.Klines {
		parts := strings.Split(line, ",")
		if len(parts) < 6 {
			continue
		}
		stamp := strings.TrimSpace(parts[0])
		if len(stamp) >= 10 {
			tradeDate = stamp[:10]
		}
		points = append(points, foundation.SectorFlowPoint{
			Time:       sectorFlowClock(stamp),
			Main:       sectorFlowValue(parts[1]),
			Small:      sectorFlowValue(parts[2]),
			Medium:     sectorFlowValue(parts[3]),
			Large:      sectorFlowValue(parts[4]),
			SuperLarge: sectorFlowValue(parts[5]),
		})
	}
	if len(points) == 0 {
		return foundation.SectorFlowSeries{}, "", fmt.Errorf("eastmoney intraday flow returned no points for %s", code)
	}

	name := strings.TrimSpace(payload.Data.Name)
	if name == "" {
		name = code
	}
	return foundation.SectorFlowSeries{
		Code:       code,
		Name:       name,
		TradeDate:  tradeDate,
		Points:     points,
		LatestMain: points[len(points)-1].Main,
	}, usedURL, nil
}

// sectorFlowIntradayCached 把请求拆成“命中缓存”与“需要抓取”两组。
//
// 上游对这个接口有配额/风控限制（突发请求会被临时封禁），而前端每次勾选板块都会
// 重新请求整组代码，因此逐板块缓存能显著减少上游请求。
func (c *Client) sectorFlowIntradayCached(codes []string, now time.Time) (map[string]sectorFlowIntradayEntry, []string) {
	c.sectorFlowIntradayMu.Lock()
	defer c.sectorFlowIntradayMu.Unlock()
	cached := make(map[string]sectorFlowIntradayEntry, len(codes))
	pending := make([]string, 0, len(codes))
	for _, code := range codes {
		entry, exists := c.sectorFlowIntradayBoards[code]
		if exists && now.Before(entry.expiresAt) {
			cached[code] = entry
			continue
		}
		pending = append(pending, code)
	}
	return cached, pending
}

func (c *Client) storeSectorFlowIntraday(code string, series foundation.SectorFlowSeries, sourceURL string) {
	if series.Code == "" {
		return
	}
	c.sectorFlowIntradayMu.Lock()
	defer c.sectorFlowIntradayMu.Unlock()
	if c.sectorFlowIntradayBoards == nil {
		c.sectorFlowIntradayBoards = make(map[string]sectorFlowIntradayEntry)
	}
	c.sectorFlowIntradayBoards[code] = sectorFlowIntradayEntry{
		series:    series,
		sourceURL: sourceURL,
		expiresAt: time.Now().Add(sectorFlowIntradayBoardTTL),
	}
}

// sectorFlowIntradayEntry 是单个板块分时序列的缓存条目。
type sectorFlowIntradayEntry struct {
	series    foundation.SectorFlowSeries
	sourceURL string
	expiresAt time.Time
}

// fetchSectorFlowIntraday 按镜像顺序抓取单个板块的分时资金流，命中即止。
//
// 每个镜像只打一次：不可用的镜像会快速断连，重试留给下一个镜像；
// 镜像之间留出间隔，避免连续请求被上游判定为突发流量。
// 全部镜像都失败时返回最后一个错误。
func (c *Client) fetchSectorFlowIntraday(ctx context.Context, query string, code string, cookie string) (sectorFlowKLinePayload, string, error) {
	var lastErr error
	for index, host := range c.sectorFlowIntradayHosts() {
		if index > 0 {
			select {
			case <-ctx.Done():
				return sectorFlowKLinePayload{}, "", ctx.Err()
			case <-time.After(sectorFlowIntradayMirrorInterval):
			}
		}
		payload, err := c.fetchSectorFlowIntradayHost(ctx, host, query, code, cookie)
		if err != nil {
			lastErr = err
			continue
		}
		return payload, host + sectorFlowIntradayPath + "?" + query, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("eastmoney intraday flow has no available mirror for %s", code)
	}
	return sectorFlowKLinePayload{}, "", lastErr
}

// fetchSectorFlowIntradayHost 抓取单个镜像；空数据与上游错误都按失败返回，交由调用方决定是否换镜像。
func (c *Client) fetchSectorFlowIntradayHost(ctx context.Context, host string, query string, code string, cookie string) (sectorFlowKLinePayload, error) {
	var headers map[string]string
	if cookie != "" {
		headers = map[string]string{"Cookie": cookie}
	}
	requestURL := host + sectorFlowIntradayPath + "?" + query
	candidate := sectorFlowKLinePayload{}
	if err := c.getJSONWithHeaders(ctx, requestURL, headers, &candidate); err != nil {
		return sectorFlowKLinePayload{}, err
	}
	if candidate.RC != 0 {
		return sectorFlowKLinePayload{}, fmt.Errorf("eastmoney intraday flow rc=%d for %s", candidate.RC, code)
	}
	if len(candidate.Data.Klines) == 0 {
		// 部分镜像（如 push2his）对该路径返回 rc=0 但空数据。
		return sectorFlowKLinePayload{}, fmt.Errorf("eastmoney intraday flow returned no points for %s", code)
	}
	return candidate, nil
}

// refreshSectorFlowSession 让桌面端重新加载东财页面换一份会话，返回主入口与新的 Cookie。
//
// 会话由桌面端提供、且按时间窗口去重；没有会话来源或刚刷新过时返回 false，
// 调用方按原来的错误返回即可。
func (c *Client) refreshSectorFlowSession(ctx context.Context, fallback string) (string, string, bool) {
	if c.sessionProvider == nil || !c.sectorFlowSessionRefresh.allow(time.Now()) {
		return "", "", false
	}
	hosts := c.sectorFlowIntradayHosts()
	if len(hosts) == 0 {
		return "", "", false
	}
	refreshed := c.sessionCookie(ctx, true)
	if refreshed == "" {
		// 刷新拿不到新会话时沿用原来的那份，浏览器侧也可能已经把状态恢复了。
		refreshed = fallback
	}
	return hosts[0], refreshed, true
}

// sectorFlowIntradayHosts 给出分时资金流的候选主机顺序：常规入口在前，编号镜像兜底。
func (c *Client) sectorFlowIntradayHosts() []string {
	candidates := make([]string, 0, len(c.quoteFallbackURLs)+1)
	candidates = append(candidates, c.quoteBaseURL)
	candidates = append(candidates, c.quoteFallbackURLs...)

	hosts := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, host := range candidates {
		host = strings.TrimRight(strings.TrimSpace(host), "/")
		if host == "" {
			continue
		}
		if _, duplicate := seen[host]; duplicate {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts
}

// getJSONWithHeadersAndFamilyRetry 先按默认地址族请求，失败且属于瞬时错误时，
// 用强制 IPv4 的客户端对同一地址再试一次。
//
// 东财部分域名（例如 90.push2）会只对某一个地址族放行：默认拨号优先 IPv6 时 TCP 建连成功、
// 紧接着被对端断开（表现为 EOF），而 Go 不会自己回退到 IPv4。实测同一时刻、同一 URL，
// 默认客户端 EOF、强制 IPv4 返回 200，因此这里补一次地址族回退。
func (c *Client) getJSONWithHeadersAndFamilyRetry(ctx context.Context, requestURL string, headers map[string]string, target any) error {
	err := c.getJSONWithHeaders(ctx, requestURL, headers, target)
	if err == nil || c.fallbackHTTPClient == nil || !isTransient(err) {
		return err
	}
	return c.getJSONWithClientHeaders(ctx, c.fallbackHTTPClient, requestURL, headers, target)
}

// SectorPriceIntraday 返回多个板块的当日分时价格序列（东财分时行情 trends2）。
//
// 这条路径与分时资金流是两条独立的上游：fflow/kline 会被按路径拒绝，trends2 不会，
// 因此价格分时在没有浏览器会话时也能取到；涨跌幅按昨收换算，多板块叠加时口径一致。
func (c *Client) SectorPriceIntraday(ctx context.Context, codes []string) ([]foundation.SectorPriceSeries, foundation.SourceMeta, error) {
	normalized, err := normalizeSectorFlowCodes(codes)
	if err != nil {
		return nil, foundation.SourceMeta{}, err
	}

	start := time.Now()
	meta := foundation.SourceMeta{Source: sectorPriceIntradaySource, FetchedAt: time.Now()}

	// 与分时资金流一样先按板块命中缓存：前端逐个勾选板块时只抓缺失的代码。
	cached, pending := c.sectorPriceIntradayCached(normalized, time.Now())
	series := make([]foundation.SectorPriceSeries, len(pending))
	usedURLs := make([]string, len(pending))
	failures := make([]error, len(pending))
	var waitGroup sync.WaitGroup
	semaphore := make(chan struct{}, sectorPriceIntradayConcurrent)
	for index, code := range pending {
		waitGroup.Add(1)
		go func(index int, code string) {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			series[index], usedURLs[index], failures[index] = c.sectorPriceIntradayOne(ctx, code)
		}(index, code)
	}
	waitGroup.Wait()

	fetched := make(map[string]foundation.SectorPriceSeries, len(pending))
	failed := make(map[string]error, len(pending))
	for index, code := range pending {
		if failures[index] != nil {
			failed[code] = failures[index]
			continue
		}
		fetched[code] = series[index]
		c.storeSectorPriceIntraday(code, series[index], usedURLs[index])
	}

	items := make([]foundation.SectorPriceSeries, 0, len(pending))
	skipped := make([]string, 0, len(pending))
	cachedURL := ""
	var lastErr error
	for _, code := range normalized {
		if entry, ok := cached[code]; ok {
			items = append(items, entry.series)
			if cachedURL == "" {
				cachedURL = entry.sourceURL
			}
			continue
		}
		if value, ok := fetched[code]; ok {
			items = append(items, value)
			continue
		}
		lastErr = failed[code]
		skipped = append(skipped, code)
	}
	if len(items) == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("eastmoney intraday trend returned no series")
		}
		return nil, foundation.SourceMeta{}, lastErr
	}

	tradeDate := items[0].TradeDate
	meta.TradeDate = tradeDate
	meta.SourceURL = c.quoteBaseURL + sectorPriceIntradayPath
	for _, usedURL := range usedURLs {
		if usedURL != "" {
			meta.SourceURL = usedURL
			break
		}
	}
	// 全部命中缓存时没有新的请求 URL，沿用缓存条目记录的上游地址。
	if cachedURL != "" && (len(usedURLs) == 0 || meta.SourceURL == c.quoteBaseURL+sectorPriceIntradayPath) {
		meta.SourceURL = cachedURL
	}
	meta.LatencyMS = time.Since(start).Milliseconds()
	meta.AvailableFields = []string{"price", "average", "change_percent"}
	if len(skipped) > 0 {
		meta.FallbackReason = fmt.Sprintf("部分板块分时价格不可用（%s），已跳过", strings.Join(skipped, ","))
	}
	for index := range items {
		items[index].Meta = meta
		if items[index].TradeDate == "" {
			items[index].TradeDate = tradeDate
		}
	}
	return items, meta, nil
}

func (c *Client) sectorPriceIntradayOne(ctx context.Context, code string) (foundation.SectorPriceSeries, string, error) {
	params := url.Values{}
	params.Set("secid", "90."+code)
	params.Set("ndays", "1")
	params.Set("iscr", "0")
	params.Set("fields1", "f1,f2,f3,f4,f5,f6,f7,f8")
	params.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58")
	params.Set("ut", sectorFlowIntradayToken)
	query := params.Encode()

	var payload sectorPriceTrendPayload
	usedURL := ""
	var lastErr error
	// 与分时资金流一样按镜像顺序尝试，命中即止；东财的 fflow 路径可能被摘掉，
	// 分时行情路径不一定同时不可用，所以镜像列表各自独立生效。
	for _, host := range c.sectorFlowIntradayHosts() {
		requestURL := host + sectorPriceIntradayPath + "?" + query
		candidate := sectorPriceTrendPayload{}
		if err := c.getJSONWithHeadersAndFamilyRetry(ctx, requestURL, nil, &candidate); err != nil {
			lastErr = err
			continue
		}
		if candidate.RC != 0 {
			lastErr = fmt.Errorf("eastmoney intraday trend rc=%d for %s", candidate.RC, code)
			continue
		}
		if len(candidate.Data.Trends) == 0 {
			lastErr = fmt.Errorf("eastmoney intraday trend returned no points for %s", code)
			continue
		}
		payload, usedURL = candidate, requestURL
		break
	}
	if usedURL == "" {
		if lastErr == nil {
			lastErr = fmt.Errorf("eastmoney intraday trend has no available mirror for %s", code)
		}
		return foundation.SectorPriceSeries{}, "", lastErr
	}

	preClose := float64(payload.Data.PreClose)
	points := make([]foundation.SectorPricePoint, 0, len(payload.Data.Trends))
	tradeDate := ""
	for _, line := range payload.Data.Trends {
		parts := strings.Split(line, ",")
		// 时间、开、收三列齐全才是一条有效的分时点。
		if len(parts) < 3 {
			continue
		}
		stamp := strings.TrimSpace(parts[0])
		if len(stamp) >= 10 {
			tradeDate = stamp[:10]
		}
		price := sectorFlowValue(parts[2])
		point := foundation.SectorPricePoint{Time: sectorFlowClock(stamp), Price: price}
		if len(parts) >= 8 {
			point.Average = sectorFlowValue(parts[7])
		}
		// 昨收缺失时涨跌幅留 0，前端按点位显示而不是编造百分比。
		if preClose > 0 {
			point.ChangePercent = (price/preClose - 1) * 100
		}
		points = append(points, point)
	}
	if len(points) == 0 {
		return foundation.SectorPriceSeries{}, "", fmt.Errorf("eastmoney intraday trend returned no points for %s", code)
	}

	name := strings.TrimSpace(payload.Data.Name)
	if name == "" {
		name = code
	}
	return foundation.SectorPriceSeries{
		Code:         code,
		Name:         name,
		TradeDate:    tradeDate,
		PreClose:     preClose,
		Points:       points,
		LatestChange: points[len(points)-1].ChangePercent,
	}, usedURL, nil
}

func (c *Client) sectorPriceIntradayCached(codes []string, now time.Time) (map[string]sectorPriceIntradayEntry, []string) {
	c.sectorPriceIntradayMu.Lock()
	defer c.sectorPriceIntradayMu.Unlock()
	cached := make(map[string]sectorPriceIntradayEntry, len(codes))
	pending := make([]string, 0, len(codes))
	for _, code := range codes {
		entry, exists := c.sectorPriceIntradayBoards[code]
		if exists && now.Before(entry.expiresAt) {
			cached[code] = entry
			continue
		}
		pending = append(pending, code)
	}
	return cached, pending
}

func (c *Client) storeSectorPriceIntraday(code string, series foundation.SectorPriceSeries, sourceURL string) {
	if series.Code == "" {
		return
	}
	c.sectorPriceIntradayMu.Lock()
	defer c.sectorPriceIntradayMu.Unlock()
	if c.sectorPriceIntradayBoards == nil {
		c.sectorPriceIntradayBoards = make(map[string]sectorPriceIntradayEntry)
	}
	c.sectorPriceIntradayBoards[code] = sectorPriceIntradayEntry{
		series:    series,
		sourceURL: sourceURL,
		expiresAt: time.Now().Add(sectorPriceIntradayBoardTTL),
	}
}

// sectorPriceIntradayEntry 是单个板块分时价格序列的缓存条目。
type sectorPriceIntradayEntry struct {
	series    foundation.SectorPriceSeries
	sourceURL string
	expiresAt time.Time
}

func applySectorFlowMetric(rank *foundation.SectorFlowRank, key string, row sectorFlowRankRow) {
	switch key {
	case "f62":
		rank.MainNetInflow = float64(row.Main)
	case "f3":
		// 东财的涨跌幅与净占比都放大了 100 倍，这里还原成百分比数值。
		rank.ChangePercent = float64(row.ChangePct) / 100
	case "f184":
		rank.MainNetInflowRatio = float64(row.MainRatio) / 100
	case "f66":
		rank.SuperLargeNet = float64(row.SuperLarge)
	case "f72":
		rank.LargeNet = float64(row.Large)
	case "f84":
		rank.SmallNet = float64(row.Small)
	case "f164":
		rank.FiveDayMainNet = float64(row.FiveDayMain)
	case "f165":
		rank.FiveDayMainRatio = float64(row.FiveDayRatio) / 100
	case "f174":
		rank.TenDayMainNet = float64(row.TenDayMain)
	case "f175":
		rank.TenDayMainRatio = float64(row.TenDayRatio) / 100
	}
}

func sectorFlowFilter(dimension string) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(dimension)) {
	case "", "concept", "theme":
		return sectorFlowConceptFilter, "concept", nil
	case "industry":
		return sectorFlowIndustryFilter, "industry", nil
	default:
		return "", "", fmt.Errorf("unsupported sector flow dimension %q", dimension)
	}
}

func normalizeSectorFlowCodes(codes []string) ([]string, error) {
	normalized := make([]string, 0, len(codes))
	seen := make(map[string]struct{}, len(codes))
	for _, raw := range codes {
		code, ok := foundation.NormalizeSectorFlowCode(raw)
		if !ok {
			return nil, fmt.Errorf("invalid sector flow code %q", raw)
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		normalized = append(normalized, code)
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("at least one sector code is required")
	}
	if len(normalized) > sectorFlowIntradayLimit {
		normalized = normalized[:sectorFlowIntradayLimit]
	}
	return normalized, nil
}

func sectorFlowClock(stamp string) string {
	if index := strings.LastIndex(stamp, " "); index >= 0 {
		return strings.TrimSpace(stamp[index+1:])
	}
	return stamp
}

func sectorFlowValue(raw string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return value
}

func sectorFlowRankFields(items []foundation.SectorFlowRank) []string {
	fields := []string{"change_percent", "main_net_inflow", "main_net_inflow_ratio", "five_day_main_net_inflow", "five_day_main_net_inflow_ratio", "ten_day_main_net_inflow", "ten_day_main_net_inflow_ratio"}
	for _, item := range items {
		if item.SuperLargeNet != 0 || item.LargeNet != 0 || item.SmallNet != 0 {
			return append(fields, "super_large_net_inflow", "large_net_inflow", "medium_net_inflow", "small_net_inflow")
		}
	}
	return fields
}
