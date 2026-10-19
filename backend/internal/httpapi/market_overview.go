package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/sectorflowsample"
)

type marketOverviewSnapshot struct {
	value     any
	meta      foundation.SourceMeta
	expiresAt time.Time
}

type marketOverviewCache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[string]marketOverviewSnapshot
}

func newMarketOverviewCache(ttl time.Duration) *marketOverviewCache {
	return &marketOverviewCache{ttl: ttl, items: map[string]marketOverviewSnapshot{}}
}

func (c *marketOverviewCache) fresh(key string) (marketOverviewSnapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, ok := c.items[key]
	return item, ok && time.Now().Before(item.expiresAt)
}

func (c *marketOverviewCache) any(key string) (marketOverviewSnapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, ok := c.items[key]
	return item, ok
}

func (c *marketOverviewCache) store(key string, value any, meta foundation.SourceMeta) {
	c.mu.Lock()
	c.items[key] = marketOverviewSnapshot{value: value, meta: meta, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

func loadMarketOverview[T any](ctx context.Context, cache *marketOverviewCache, key string, loader func(context.Context) (T, foundation.SourceMeta, error)) (T, foundation.SourceMeta, error) {
	var zero T
	if cached, ok := cache.fresh(key); ok {
		if value, typeOK := cached.value.(T); typeOK {
			return value, cached.meta, nil
		}
	}
	value, meta, err := loader(ctx)
	if err == nil {
		cache.store(key, value, meta)
		return value, meta, nil
	}
	if cached, ok := cache.any(key); ok {
		if stale, typeOK := cached.value.(T); typeOK {
			cached.meta.Stale = true
			cached.meta.FallbackReason = "实时数据刷新失败，已返回最近一次成功快照：" + err.Error()
			return stale, cached.meta, nil
		}
	}
	return zero, foundation.SourceMeta{}, err
}

func (s *Server) marketIndexesHandler(w http.ResponseWriter, r *http.Request) {
	scope := firstNonEmpty(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope"))), "global")
	if scope != "global" && scope != "core" {
		writeError(w, http.StatusBadRequest, "scope must be global or core")
		return
	}
	marketOverviewList(s, w, r, "indexes:"+scope, func(ctx context.Context) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
		return s.marketOverview.MarketIndexes(ctx, scope)
	})
}

func (s *Server) marketIndexSeriesHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("id")))
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	period := firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("period")), "day")
	limit, err := marketLimitQuery(r, 120, 500)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	key := fmt.Sprintf("index-series:%s:%s:%d", id, period, limit)
	series, meta, err := loadMarketOverview(ctx, s.marketSnapshots, key, func(loadCtx context.Context) (foundation.MarketIndexSeries, foundation.SourceMeta, error) {
		value, loadErr := s.marketOverview.MarketIndexSeries(loadCtx, id, period, limit)
		return value, value.Meta, loadErr
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	series.Meta = meta
	series.Index.Meta = meta
	writeJSON(w, http.StatusOK, map[string]any{"data": series, "meta": meta})
}

func (s *Server) marketIndustriesHandler(w http.ResponseWriter, r *http.Request) {
	limit, err := marketLimitQuery(r, 50, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("industries:%d", limit), func(ctx context.Context) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
		return s.marketOverview.IndustryMomentum(ctx, limit)
	})
}

func (s *Server) marketFlowsHandler(w http.ResponseWriter, r *http.Request) {
	dimension := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("dimension")))
	if dimension != "industry" && dimension != "theme" && dimension != "stock" {
		writeError(w, http.StatusBadRequest, "dimension must be industry, theme, or stock")
		return
	}
	sortKey := firstNonEmpty(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort"))), "net")
	if sortKey != "net" && sortKey != "change" && sortKey != "ratio" {
		writeError(w, http.StatusBadRequest, "sort must be net, change, or ratio")
		return
	}
	limit, err := marketLimitQuery(r, 50, 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("flows:%s:%s:%d", dimension, sortKey, limit), func(ctx context.Context) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
		return s.marketOverview.MarketFundFlows(ctx, dimension, sortKey, limit)
	})
}

// 资金流向工作台：概念/行业板块资金流榜单 + 选中板块的当日分时累计净流入。
func (s *Server) marketSectorFlowsHandler(w http.ResponseWriter, r *http.Request) {
	dimension := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("dimension")))
	if dimension == "" {
		dimension = "concept"
	}
	if dimension != "concept" && dimension != "theme" && dimension != "industry" {
		writeError(w, http.StatusBadRequest, "dimension must be concept or industry")
		return
	}
	limit, err := marketLimitQuery(r, 200, 600)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	provider, ok := s.marketOverview.(SectorFlowProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "sector flow provider is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	data, meta, err := loadMarketOverview(ctx, s.marketSnapshots, fmt.Sprintf("sector-flows:%s:%d", dimension, limit), func(ctx context.Context) ([]foundation.SectorFlowRank, foundation.SourceMeta, error) {
		return provider.SectorFlowRanking(ctx, dimension, limit)
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

func (s *Server) marketSectorFlowIntradayHandler(w http.ResponseWriter, r *http.Request) {
	codes, err := sectorFlowIntradayCodes(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	provider, ok := s.marketOverview.(SectorFlowProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "sector flow provider is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	data, meta, err := loadMarketOverview(ctx, s.marketSnapshots, "sector-flow-intraday:"+strings.Join(codes, ","), func(ctx context.Context) ([]foundation.SectorFlowSeries, foundation.SourceMeta, error) {
		return provider.SectorFlowIntraday(ctx, codes)
	})
	if err != nil {
		// 上游分钟序列不可用时不再直接失败：改由本地采样（每 3 分钟一次）拼出当天曲线。
		if fallback, fallbackMeta, ok := s.sectorFlowSeriesFromSamples(ctx, codes); ok {
			writeJSON(w, http.StatusOK, map[string]any{"data": fallback, "meta": fallbackMeta})
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if merged, mergedMeta := s.mergeSectorFlowSamples(ctx, codes, data, meta); len(merged) > 0 {
		data, meta = merged, mergedMeta
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

// 板块分时价格（东财分时行情 trends2）：与分时资金流是两条独立的上游路径，
// 这里单独出一个接口，前端把资金曲线与价格走势并排对照。
func (s *Server) marketSectorPriceIntradayHandler(w http.ResponseWriter, r *http.Request) {
	codes, err := sectorFlowIntradayCodes(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	provider, ok := s.marketOverview.(SectorFlowProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "sector flow provider is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	data, meta, err := loadMarketOverview(ctx, s.marketSnapshots, "sector-price-intraday:"+strings.Join(codes, ","), func(ctx context.Context) ([]foundation.SectorPriceSeries, foundation.SourceMeta, error) {
		return provider.SectorPriceIntraday(ctx, codes)
	})
	if err != nil {
		// 与资金曲线一致：价格分时同样回退到本地采样的涨跌幅序列。
		if fallback, fallbackMeta, ok := s.sectorPriceSeriesFromSamples(ctx, codes); ok {
			writeJSON(w, http.StatusOK, map[string]any{"data": fallback, "meta": fallbackMeta})
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if merged, mergedMeta := s.mergeSectorPriceSamples(ctx, codes, data, meta); len(merged) > 0 {
		data, meta = merged, mergedMeta
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

// sectorFlowIntradayCodes 解析 codes 查询参数：归一化去重后按从小到大排序，
// 保证同一组板块命中同一个缓存键；非法代码与超限都作为请求错误返回。
func sectorFlowIntradayCodes(r *http.Request) ([]string, error) {
	codes, err := normalizeSectorFlowQueryCodes(r.URL.Query().Get("codes"))
	if err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return nil, fmt.Errorf("codes is required, e.g. codes=BK0917,BK0490")
	}
	if len(codes) > 12 {
		return nil, fmt.Errorf("codes must contain at most 12 boards")
	}
	return codes, nil
}

// normalizeSectorFlowQueryCodes 归一化并去重板块代码，保证同样的一组板块命中同一个缓存键。
// 非法代码直接返回错误，由调用方判定为 400 而不是上游故障。
func normalizeSectorFlowQueryCodes(raw string) ([]string, error) {
	seen := make(map[string]struct{})
	codes := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		code, ok := foundation.NormalizeSectorFlowCode(part)
		if !ok {
			return nil, fmt.Errorf("invalid sector code %q", strings.TrimSpace(part))
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes, nil
}

// sectorFlowSampleSource 是本地采样拼接曲线的数据源标识，便于前端与日志区分
// 「上游直出」和「本地采样」两种口径。
const sectorFlowSampleSource = "local:sector-flow-samples"

// sectorFlowSamplesFor 读取指定板块的本地采样。
//
// 优先取当天；当天还没有任何采样时（后端刚启动、或收盘后才打开页面）退到最近
// 一个有采样的交易日，由调用方在 meta 里写清楚是哪一天的数据。
func (s *Server) sectorFlowSamplesFor(ctx context.Context, codes []string) ([]sectorflowsample.Sample, string, bool) {
	if s == nil || s.sectorFlowSamples == nil {
		return nil, "", false
	}
	today := time.Now().In(foundation.AStockLocation).Format("2006-01-02")
	samples, err := s.sectorFlowSamples.DaySamples(ctx, today, codes)
	if err == nil && len(samples) > 0 {
		return samples, today, true
	}
	latest, latestErr := s.sectorFlowSamples.LatestTradeDate(ctx)
	if latestErr != nil || latest == "" || latest == today {
		return nil, "", false
	}
	samples, err = s.sectorFlowSamples.DaySamples(ctx, latest, codes)
	if err != nil || len(samples) == 0 {
		return nil, "", false
	}
	return samples, latest, true
}

// sectorFlowSeriesFromSamples 把本地采样拼成与上游同构的分时资金序列。
func (s *Server) sectorFlowSeriesFromSamples(ctx context.Context, codes []string) ([]foundation.SectorFlowSeries, foundation.SourceMeta, bool) {
	samples, tradeDate, ok := s.sectorFlowSamplesFor(ctx, codes)
	if !ok {
		return nil, foundation.SourceMeta{}, false
	}
	grouped := make(map[string][]sectorflowsample.Sample, len(codes))
	for _, sample := range samples {
		grouped[sample.Code] = append(grouped[sample.Code], sample)
	}
	items := make([]foundation.SectorFlowSeries, 0, len(grouped))
	firstMinute := ""
	for _, code := range codes {
		rows := grouped[code]
		if len(rows) == 0 {
			continue
		}
		points := make([]foundation.SectorFlowPoint, 0, len(rows))
		for _, row := range rows {
			points = append(points, foundation.SectorFlowPoint{
				Time:       row.Minute,
				Main:       row.Main,
				SuperLarge: row.SuperLarge,
				Large:      row.Large,
				Medium:     row.Medium,
				Small:      row.Small,
			})
		}
		name := strings.TrimSpace(rows[0].Name)
		if name == "" {
			name = code
		}
		if firstMinute == "" || rows[0].Minute < firstMinute {
			firstMinute = rows[0].Minute
		}
		items = append(items, foundation.SectorFlowSeries{
			Code:       code,
			Name:       name,
			TradeDate:  tradeDate,
			Points:     points,
			LatestMain: points[len(points)-1].Main,
		})
	}
	if len(items) == 0 {
		return nil, foundation.SourceMeta{}, false
	}
	meta := s.sectorFlowSampleMeta(tradeDate, firstMinute, "东财分时资金接口暂不可用")
	for index := range items {
		items[index].Meta = meta
	}
	return items, meta, true
}

// sectorPriceSeriesFromSamples 用采样里的涨跌幅拼出价格分时曲线。
//
// 本地采样只有「累计主力净流入」与「当日涨跌幅」两个标量，没有绝对点位；
// 曲线组件本来就按涨跌幅画价格分时，因此这里 0 点位的 price 字段不影响显示。
func (s *Server) sectorPriceSeriesFromSamples(ctx context.Context, codes []string) ([]foundation.SectorPriceSeries, foundation.SourceMeta, bool) {
	samples, tradeDate, ok := s.sectorFlowSamplesFor(ctx, codes)
	if !ok {
		return nil, foundation.SourceMeta{}, false
	}
	grouped := make(map[string][]sectorflowsample.Sample, len(codes))
	for _, sample := range samples {
		grouped[sample.Code] = append(grouped[sample.Code], sample)
	}
	items := make([]foundation.SectorPriceSeries, 0, len(grouped))
	firstMinute := ""
	for _, code := range codes {
		rows := grouped[code]
		if len(rows) == 0 {
			continue
		}
		points := make([]foundation.SectorPricePoint, 0, len(rows))
		for _, row := range rows {
			points = append(points, foundation.SectorPricePoint{Time: row.Minute, ChangePercent: row.ChangePercent})
		}
		name := strings.TrimSpace(rows[0].Name)
		if name == "" {
			name = code
		}
		if firstMinute == "" || rows[0].Minute < firstMinute {
			firstMinute = rows[0].Minute
		}
		items = append(items, foundation.SectorPriceSeries{
			Code:         code,
			Name:         name,
			TradeDate:    tradeDate,
			Points:       points,
			LatestChange: points[len(points)-1].ChangePercent,
		})
	}
	if len(items) == 0 {
		return nil, foundation.SourceMeta{}, false
	}
	meta := s.sectorFlowSampleMeta(tradeDate, firstMinute, "东财分时行情接口暂不可用")
	for index := range items {
		items[index].Meta = meta
	}
	return items, meta, true
}

// sectorFlowSampleMeta 统一描述本地采样曲线的来源与覆盖范围。
func (s *Server) sectorFlowSampleMeta(tradeDate string, firstMinute string, cause string) foundation.SourceMeta {
	interval := s.sectorFlowSampleIntervalMinutes()
	today := time.Now().In(foundation.AStockLocation).Format("2006-01-02")
	reason := fmt.Sprintf("%s，曲线改由本地每 %d 分钟采样拼接（自 %s 起）", cause, interval, firstMinute)
	if tradeDate != today {
		reason = fmt.Sprintf("当天还没有采样，以下为 %s 的本地采样曲线（每 %d 分钟一次）", tradeDate, interval)
	}
	nextRefresh := time.Now().Add(time.Duration(interval) * time.Minute)
	return foundation.SourceMeta{
		Source:          sectorFlowSampleSource,
		FetchedAt:       time.Now(),
		TradeDate:       tradeDate,
		Stale:           true,
		AvailableFields: []string{"main", "super_large", "large", "medium", "small", "change_percent"},
		FallbackReason:  reason,
		NextRefreshAt:   &nextRefresh,
	}
}

func (s *Server) sectorFlowSampleIntervalMinutes() int {
	if s.sectorFlowSampleInterval <= 0 {
		return int(sectorflowsample.DefaultInterval / time.Minute)
	}
	minutes := int(s.sectorFlowSampleInterval.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	return minutes
}

// mergeSectorFlowSamples 在上游只返回部分板块时，用本地采样补齐缺的那几个。
func (s *Server) mergeSectorFlowSamples(ctx context.Context, codes []string, data []foundation.SectorFlowSeries, meta foundation.SourceMeta) ([]foundation.SectorFlowSeries, foundation.SourceMeta) {
	if s.sectorFlowSamples == nil {
		return nil, meta
	}
	missing := missingSectorFlowCodes(codes, data)
	if len(missing) == 0 {
		return nil, meta
	}
	fallback, _, ok := s.sectorFlowSeriesFromSamples(ctx, missing)
	if !ok {
		return nil, meta
	}
	merged := make([]foundation.SectorFlowSeries, 0, len(data)+len(fallback))
	merged = append(merged, data...)
	merged = append(merged, fallback...)
	meta.FallbackReason = appendFallbackReason(meta.FallbackReason, fmt.Sprintf("其中 %d 个板块的分时资金已改用本地采样拼接", len(missing)))
	meta.Source += "+local-samples"
	meta.Stale = true
	for index := range merged {
		merged[index].Meta = meta
	}
	return merged, meta
}

// mergeSectorPriceSamples 是价格分时版的部分补齐。
func (s *Server) mergeSectorPriceSamples(ctx context.Context, codes []string, data []foundation.SectorPriceSeries, meta foundation.SourceMeta) ([]foundation.SectorPriceSeries, foundation.SourceMeta) {
	if s.sectorFlowSamples == nil {
		return nil, meta
	}
	missing := missingSectorPriceCodes(codes, data)
	if len(missing) == 0 {
		return nil, meta
	}
	fallback, _, ok := s.sectorPriceSeriesFromSamples(ctx, missing)
	if !ok {
		return nil, meta
	}
	merged := make([]foundation.SectorPriceSeries, 0, len(data)+len(fallback))
	merged = append(merged, data...)
	merged = append(merged, fallback...)
	meta.FallbackReason = appendFallbackReason(meta.FallbackReason, fmt.Sprintf("其中 %d 个板块的分时价格已改用本地采样拼接", len(missing)))
	meta.Source += "+local-samples"
	meta.Stale = true
	for index := range merged {
		merged[index].Meta = meta
	}
	return merged, meta
}

func missingSectorFlowCodes(codes []string, series []foundation.SectorFlowSeries) []string {
	present := make(map[string]struct{}, len(series))
	for _, item := range series {
		present[item.Code] = struct{}{}
	}
	missing := make([]string, 0, len(codes))
	for _, code := range codes {
		if _, ok := present[code]; !ok {
			missing = append(missing, code)
		}
	}
	return missing
}

func missingSectorPriceCodes(codes []string, series []foundation.SectorPriceSeries) []string {
	present := make(map[string]struct{}, len(series))
	for _, item := range series {
		present[item.Code] = struct{}{}
	}
	missing := make([]string, 0, len(codes))
	for _, code := range codes {
		if _, ok := present[code]; !ok {
			missing = append(missing, code)
		}
	}
	return missing
}

func appendFallbackReason(current string, addition string) string {
	current = strings.TrimSpace(current)
	addition = strings.TrimSpace(addition)
	if current == "" {
		return addition
	}
	if addition == "" {
		return current
	}
	return current + "；" + addition
}

func (s *Server) marketMarginBalanceHandler(w http.ResponseWriter, r *http.Request) {
	limit, err := marketLimitQuery(r, 120, 500)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("margin-balance:%d", limit), func(ctx context.Context) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error) {
		return s.marketOverview.MarketMarginSeries(ctx, limit)
	})
}

func (s *Server) marketBillboardHandler(w http.ResponseWriter, r *http.Request) {
	tradeDate := strings.TrimSpace(r.URL.Query().Get("trade_date"))
	if tradeDate != "" {
		if _, err := time.Parse("2006-01-02", tradeDate); err != nil {
			writeError(w, http.StatusBadRequest, "trade_date must use YYYY-MM-DD")
			return
		}
	}
	limit, err := marketLimitQuery(r, 50, 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("billboard:%s:%d", tradeDate, limit), func(ctx context.Context) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error) {
		return s.marketOverview.MarketBillboard(ctx, tradeDate, limit)
	})
}

func (s *Server) marketBillboardDetailHandler(w http.ResponseWriter, r *http.Request) {
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	if symbol == "" {
		writeError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	tradeDate := strings.TrimSpace(r.URL.Query().Get("trade_date"))
	if _, err := time.Parse("2006-01-02", tradeDate); err != nil {
		writeError(w, http.StatusBadRequest, "trade_date must use YYYY-MM-DD")
		return
	}
	reason := strings.TrimSpace(r.URL.Query().Get("reason"))
	if reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	key := fmt.Sprintf("billboard-detail:%s:%s:%s", symbol, tradeDate, reason)
	detail, meta, err := loadMarketOverview(ctx, s.marketSnapshots, key, func(loadCtx context.Context) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
		return s.marketOverview.MarketBillboardDetail(loadCtx, symbol, tradeDate, reason)
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	detail.Meta = meta
	writeJSON(w, http.StatusOK, map[string]any{"data": detail, "meta": meta})
}

func (s *Server) marketFuturesPositionHandler(w http.ResponseWriter, r *http.Request) {
	if s.futuresPosition == nil {
		writeError(w, http.StatusServiceUnavailable, "futures position provider is unavailable")
		return
	}
	variety := strings.ToUpper(firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("variety")), "IF"))
	limit, err := marketLimitQuery(r, 60, 250)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	key := fmt.Sprintf("futures-position:%s:%d", variety, limit)
	series, meta, err := loadMarketOverview(ctx, s.marketSnapshots, key, func(loadCtx context.Context) (foundation.MarketFuturesPositionSeries, foundation.SourceMeta, error) {
		value, loadErr := s.futuresPosition.Trend(loadCtx, variety, limit)
		return value, value.Meta, loadErr
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	series.Meta = meta
	writeJSON(w, http.StatusOK, map[string]any{"data": series, "meta": meta})
}

func (s *Server) marketFuturesMembersHandler(w http.ResponseWriter, r *http.Request) {
	if s.futuresPosition == nil {
		writeError(w, http.StatusServiceUnavailable, "futures position provider is unavailable")
		return
	}
	contract := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("contract")))
	tradeDate := strings.TrimSpace(r.URL.Query().Get("trade_date"))
	if contract == "" || tradeDate == "" {
		writeError(w, http.StatusBadRequest, "contract and trade_date are required")
		return
	}
	if _, err := time.Parse("2006-01-02", tradeDate); err != nil {
		writeError(w, http.StatusBadRequest, "trade_date must use YYYY-MM-DD")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	key := fmt.Sprintf("futures-members:%s:%s", contract, tradeDate)
	data, meta, err := loadMarketOverview(ctx, s.marketSnapshots, key, func(loadCtx context.Context) (foundation.MarketFuturesMembers, foundation.SourceMeta, error) {
		value, loadErr := s.futuresPosition.Members(loadCtx, contract, tradeDate)
		return value, value.Meta, loadErr
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	data.Meta = meta
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

func (s *Server) marketFuturesConsensusHandler(w http.ResponseWriter, r *http.Request) {
	if s.futuresPosition == nil {
		writeError(w, http.StatusServiceUnavailable, "futures position provider is unavailable")
		return
	}
	tradeDate := strings.TrimSpace(r.URL.Query().Get("trade_date"))
	if tradeDate != "" {
		if _, err := time.Parse("2006-01-02", tradeDate); err != nil {
			writeError(w, http.StatusBadRequest, "trade_date must use YYYY-MM-DD")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	key := "futures-consensus:" + tradeDate
	data, meta, err := loadMarketOverview(ctx, s.marketSnapshots, key, func(loadCtx context.Context) (foundation.MarketFuturesConsensus, foundation.SourceMeta, error) {
		value, loadErr := s.futuresPosition.Consensus(loadCtx, tradeDate)
		return value, value.Meta, loadErr
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	data.Meta = meta
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

func (s *Server) marketAnnouncementsHandler(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	category := firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("category")), "all")
	limit, err := marketLimitQuery(r, 50, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("announcements:%s:%s:%s:%d", query, symbol, category, limit), func(ctx context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
		return s.marketOverview.MarketAnnouncements(ctx, query, symbol, category, limit)
	})
}

func (s *Server) marketInstitutionReportsHandler(w http.ResponseWriter, r *http.Request) {
	s.marketReportsHandler(w, r, "stock")
}

func (s *Server) marketIndustryResearchHandler(w http.ResponseWriter, r *http.Request) {
	s.marketReportsHandler(w, r, "industry")
}

func (s *Server) marketReportsHandler(w http.ResponseWriter, r *http.Request, kind string) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	industry := strings.TrimSpace(r.URL.Query().Get("industry"))
	limit, err := marketLimitQuery(r, 50, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("reports:%s:%s:%s:%s:%d", kind, query, symbol, industry, limit), func(ctx context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
		return s.marketOverview.MarketReports(ctx, kind, query, symbol, industry, limit)
	})
}

// marketAnnouncementContentHandler 按需返回某条公告的正文：列表里个别公告正文抓取失败时，展开那条再补齐。
func (s *Server) marketAnnouncementContentHandler(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.marketOverview.(MarketAnnouncementContentProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "announcement body provider is unavailable")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || len(id) > 48 {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	content, _, err := loadMarketOverview(ctx, s.marketSnapshots, "announcement-content:"+id, func(loadCtx context.Context) (string, foundation.SourceMeta, error) {
		body, loadErr := provider.MarketAnnouncementContent(loadCtx, id)
		if loadErr != nil {
			return "", foundation.SourceMeta{}, loadErr
		}
		trimmed := strings.TrimSpace(body)
		if trimmed == "" {
			return "", foundation.SourceMeta{}, fmt.Errorf("公告正文暂不可用")
		}
		if runes := []rune(trimmed); len(runes) > 8_000 {
			trimmed = string(runes[:8_000]) + "…"
		}
		return trimmed, foundation.SourceMeta{Source: "eastmoney:announcement", SourceURL: "https://data.eastmoney.com/notices/detail/" + id + ".html", FetchedAt: time.Now()}, nil
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	detail := foundation.MarketResearchDetail{Kind: "announcement", ID: id, Content: content}
	writeJSON(w, http.StatusOK, map[string]any{"data": detail, "meta": detail.Meta})
}

// marketReportContentHandler 按需返回某篇研报的正文，用于列表内展开阅读，避免跳转外部页面。
func (s *Server) marketReportContentHandler(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.marketOverview.(MarketReportContentProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "report body provider is unavailable")
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind == "" {
		kind = "stock"
	}
	if kind != "stock" && kind != "industry" {
		writeError(w, http.StatusBadRequest, "kind must be stock or industry")
		return
	}
	infoCode := strings.TrimSpace(r.URL.Query().Get("id"))
	if infoCode == "" || len(infoCode) > 48 {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	detail, _, err := loadMarketOverview(ctx, s.marketSnapshots, fmt.Sprintf("report-content:%s:%s", kind, infoCode), func(loadCtx context.Context) (foundation.MarketResearchDetail, foundation.SourceMeta, error) {
		value, loadErr := provider.MarketReportContent(loadCtx, kind, infoCode)
		return value, value.Meta, loadErr
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": detail, "meta": detail.Meta})
}

func marketOverviewList[T any](s *Server, w http.ResponseWriter, r *http.Request, key string, loader func(context.Context) (T, foundation.SourceMeta, error)) {
	if s.marketOverview == nil {
		writeError(w, http.StatusServiceUnavailable, "market overview provider is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	data, meta, err := loadMarketOverview(ctx, s.marketSnapshots, key, loader)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

func marketLimitQuery(r *http.Request, fallback int, maximum int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > maximum {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximum)
	}
	return value, nil
}
