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

	// 榜单最多返回 600 行，覆盖概念（约 508）与行业（约 496）两个维度。
	sectorFlowRankLimit = 600
	// 分时曲线一次最多拉取 12 个板块，避免前端叠加过多序列。
	sectorFlowIntradayLimit = 12
	// 榜单与分时都按并发上限抓取，降低被限流的概率。
	sectorFlowRankConcurrent     = 3
	sectorFlowIntradayConcurrent = 4

	// 分时资金流连续失败到阈值后熔断一段时间，避免持续打已不可用的上游路径。
	sectorFlowFailureThreshold = 2
	sectorFlowCooldownWindow   = 90 * time.Second
)

// sectorFlowRankMetricKeys 是需要从 bkzj 拉取的字段。
//
// f62 是主口径（今日主力净流入），失败即整体失败；其余字段失败只降级提示，
// 榜单仍然可用。f3 涨幅、f184 主力净占比、f164/f174 为 5 日/10 日累计净流入，
// f66/f72/f84 为超大单、大单、小单净流入（中单由四类资金净额之和为零反推）。
var sectorFlowRankMetricKeys = []string{"f62", "f3", "f184", "f66", "f72", "f84", "f164", "f174"}

type sectorFlowRankPayload struct {
	RC   int `json:"rc"`
	Data struct {
		Total int                 `json:"total"`
		Diff  []sectorFlowRankRow `json:"diff"`
	} `json:"data"`
}

// sectorFlowRankRow 覆盖 bkzj 返回的所有候选字段；每次请求只会带回 key 指定的那一个字段。
type sectorFlowRankRow struct {
	Code        string        `json:"f12"`
	Name        string        `json:"f14"`
	ChangePct   flexibleFloat `json:"f3"`
	Main        flexibleFloat `json:"f62"`
	MainRatio   flexibleFloat `json:"f184"`
	SuperLarge  flexibleFloat `json:"f66"`
	Large       flexibleFloat `json:"f72"`
	Small       flexibleFloat `json:"f84"`
	FiveDayMain flexibleFloat `json:"f164"`
	TenDayMain  flexibleFloat `json:"f174"`
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

// sectorFlowCooldown 记录分时资金流的连续失败次数。
//
// push2 的分时资金流路径在被密集请求后会整段重置连接（同机其他 push2 路径仍然可用），
// 这里做一层熔断：确认不可用时短时间内不再打上游，让接口快速回落到缓存。
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

	series := make([]foundation.SectorFlowSeries, len(normalized))
	failures := make([]error, len(normalized))
	var waitGroup sync.WaitGroup
	semaphore := make(chan struct{}, sectorFlowIntradayConcurrent)
	for index, code := range normalized {
		waitGroup.Add(1)
		go func(index int, code string) {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			value, loadErr := c.sectorFlowIntradayOne(ctx, code)
			series[index], failures[index] = value, loadErr
		}(index, code)
	}
	waitGroup.Wait()

	items := make([]foundation.SectorFlowSeries, 0, len(series))
	skipped := make([]string, 0, len(series))
	var lastErr error
	for index, value := range series {
		if failures[index] != nil {
			lastErr = failures[index]
			skipped = append(skipped, normalized[index])
			continue
		}
		items = append(items, value)
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
	meta.SourceURL = c.quoteBaseURL + "/api/qt/stock/fflow/kline/get"
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

func (c *Client) sectorFlowIntradayOne(ctx context.Context, code string) (foundation.SectorFlowSeries, error) {
	params := url.Values{}
	params.Set("lmt", "0")
	params.Set("klt", "1")
	params.Set("secid", "90."+code)
	params.Set("fields1", "f1,f2,f3,f7")
	params.Set("fields2", "f51,f52,f53,f54,f55,f56")
	query := params.Encode()

	// push2 的分时资金流偶发整站被重置，逐个镜像重试；全部失败才向上报错，
	// 单个板块失败由上层跳过，不影响其它曲线。
	var payload sectorFlowKLinePayload
	var lastErr error
	for _, host := range append([]string{c.quoteBaseURL}, c.quoteFallbackURLs...) {
		requestURL := host + "/api/qt/stock/fflow/kline/get?" + query
		lastErr = c.getJSONWithRetry(ctx, requestURL, &payload)
		if lastErr == nil && payload.RC == 0 {
			break
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("eastmoney intraday flow rc=%d for %s", payload.RC, code)
		}
	}
	if lastErr != nil {
		return foundation.SectorFlowSeries{}, lastErr
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
		return foundation.SectorFlowSeries{}, fmt.Errorf("eastmoney intraday flow returned no points for %s", code)
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
	}, nil
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
	case "f174":
		rank.TenDayMainNet = float64(row.TenDayMain)
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
	fields := []string{"change_percent", "main_net_inflow", "main_net_inflow_ratio", "five_day_main_net_inflow", "ten_day_main_net_inflow"}
	for _, item := range items {
		if item.SuperLargeNet != 0 || item.LargeNet != 0 || item.SmallNet != 0 {
			return append(fields, "super_large_net_inflow", "large_net_inflow", "medium_net_inflow", "small_net_inflow")
		}
	}
	return fields
}
