package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
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
