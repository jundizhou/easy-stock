package marketoverview

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

type Primary interface {
	IndexProvider
	IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error)
	MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error)
	MarketMarginSeries(ctx context.Context, limit int) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error)
	MarketBillboard(ctx context.Context, tradeDate string, limit int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error)
	MarketBillboardDetail(ctx context.Context, symbol string, tradeDate string, reason string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error)
	MarketAnnouncements(ctx context.Context, query string, symbol string, category string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error)
	MarketReports(ctx context.Context, kind string, query string, symbol string, industry string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error)
}

type IndustryMomentumProvider interface {
	IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error)
}

type FundFlowProvider interface {
	MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error)
}

type USSectorMomentumProvider interface {
	USSectorMomentum(ctx context.Context, limit int) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error)
}

type IndexProvider interface {
	IndexSnapshots(ctx context.Context, requested []foundation.MarketIndexDefinition) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error)
	MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error)
}

// IndexSeriesProvider 只提供指数走势：新浪没有批量指数快照，
// 但它的日线通道能补上「东财 + 腾讯都挂了」的那一段。
type IndexSeriesProvider interface {
	MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error)
}

type Provider struct {
	domestic         IndexProvider
	primary          Primary
	indexFallback    IndexProvider
	indexFallback2   IndexSeriesProvider
	industryProvider IndustryMomentumProvider
	fundFlowProvider FundFlowProvider
}

// Option 让调用方按需追加备用通道，避免把构造签名撑成四个位置参数。
type Option func(*Provider)

// WithSecondaryIndexFallback 追加第三路指数走势通道（东财 → 腾讯 → 新浪）：
// 偏离值计算强依赖基准指数，单一路径抖动就会让整块异动页失去数据。
func WithSecondaryIndexFallback(fallback IndexSeriesProvider) Option {
	return func(p *Provider) { p.indexFallback2 = fallback }
}

func WithDomesticIndexes(provider IndexProvider) Option {
	return func(p *Provider) { p.domestic = provider }
}

func New(primary Primary, indexFallback IndexProvider, industryProvider IndustryMomentumProvider, fundFlowProvider FundFlowProvider, opts ...Option) *Provider {
	provider := &Provider{primary: primary, indexFallback: indexFallback, industryProvider: industryProvider, fundFlowProvider: fundFlowProvider}
	for _, opt := range opts {
		opt(provider)
	}
	return provider
}

func (p *Provider) MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	requested := foundation.MarketIndexDefinitions(scope)
	missing := slices.Clone(requested)
	byID := make(map[string]foundation.MarketIndexSnapshot, len(requested))
	var sources []string
	var errs []error
	started := time.Now()
	var meta foundation.SourceMeta
	providers := []IndexProvider{p.primary, p.indexFallback}
	if p.domestic != nil {
		providers = []IndexProvider{p.domestic, p.indexFallback, p.primary}
	}
	for _, provider := range providers {
		if provider == nil || len(missing) == 0 {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		// One slow market must not consume the entire overview request budget.
		attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
		rows, m, err := provider.IndexSnapshots(attempt, missing)
		cancel()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		added := false
		for _, row := range rows {
			if _, exists := byID[row.ID]; !exists && row.Price > 0 {
				byID[row.ID] = row
				added = true
			}
		}
		if added {
			sources = append(sources, m.Source)
			meta = m
		}
		missing = slices.DeleteFunc(missing, func(d foundation.MarketIndexDefinition) bool {
			_, ok := byID[d.ID]
			return ok
		})
	}
	if err := ctx.Err(); err != nil && (len(byID) == 0 || errors.Is(err, context.Canceled)) {
		return nil, foundation.SourceMeta{}, err
	}
	var result []foundation.MarketIndexSnapshot
	for _, d := range requested {
		if row, ok := byID[d.ID]; ok {
			result = append(result, row)
		}
	}
	if len(result) == 0 {
		return nil, foundation.SourceMeta{}, errors.Join(append(errs, errors.New("未取得指数行情"))...)
	}
	if len(sources) > 1 {
		meta = foundation.SourceMeta{Source: strings.Join(sources, " + "), FetchedAt: time.Now(), LatencyMS: time.Since(started).Milliseconds()}
	}
	if len(missing) > 0 {
		meta.FallbackReason = fmt.Sprintf("已取得 %d/%d 个指数行情", len(result), len(requested))
	}
	return result, meta, nil
}

func (p *Provider) MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error) {
	// Keep the existing overseas index periods and benchmarks. Domestic
	// benchmarks can use the MAC index protocol before falling back.
	var errs []error
	for i, source := range []struct {
		provider IndexSeriesProvider
		label    string
	}{
		{p.domestic, "通达信"}, {p.primary, "东方财富"}, {p.indexFallback, "腾讯 K 线"}, {p.indexFallback2, "新浪指数通道"},
	} {
		if source.provider == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return foundation.MarketIndexSeries{}, err
		}
		attempt, cancel := context.WithTimeout(ctx, 4*time.Second)
		series, err := source.provider.MarketIndexSeries(attempt, id, period, limit)
		cancel()
		if err == nil && len(series.Lines) > 0 {
			if i >= 2 {
				series.Meta.FallbackReason = "指数走势已切换" + source.label
				series.Index.Meta = series.Meta
				for j := range series.Lines {
					series.Lines[j].Meta = series.Meta
				}
			}
			return series, nil
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return foundation.MarketIndexSeries{}, errors.Join(append(errs, errors.New("未取得指数走势"))...)
}

func (p *Provider) IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	if p.industryProvider == nil {
		return p.primary.IndustryMomentum(ctx, limit)
	}
	items, meta, err := p.industryProvider.IndustryMomentum(ctx, limit)
	if err == nil {
		return items, meta, nil
	}
	fallbackItems, fallbackMeta, fallbackErr := p.primary.IndustryMomentum(ctx, limit)
	if fallbackErr != nil {
		return nil, foundation.SourceMeta{}, fmt.Errorf("tencent industry momentum failed: %v; eastmoney fallback failed: %w", err, fallbackErr)
	}
	fallbackMeta.FallbackReason = joinFallbackReason("腾讯行业强度不可用，已回退东方财富可用字段", fallbackMeta.FallbackReason)
	for index := range fallbackItems {
		fallbackItems[index].Meta = fallbackMeta
	}
	return fallbackItems, fallbackMeta, nil
}

func (p *Provider) MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	if p.fundFlowProvider == nil {
		return p.primary.MarketFundFlows(ctx, dimension, sortKey, limit)
	}
	items, meta, err := p.fundFlowProvider.MarketFundFlows(ctx, dimension, sortKey, limit)
	if err == nil {
		return items, meta, nil
	}
	fallbackItems, fallbackMeta, fallbackErr := p.primary.MarketFundFlows(ctx, dimension, sortKey, limit)
	if fallbackErr != nil {
		return nil, foundation.SourceMeta{}, fmt.Errorf("sina %s fund flow failed: %v; eastmoney fallback failed: %w", dimension, err, fallbackErr)
	}
	fallbackMeta.FallbackReason = joinFallbackReason("新浪资金榜不可用，已回退东方财富可用字段", fallbackMeta.FallbackReason)
	for index := range fallbackItems {
		fallbackItems[index].Meta = fallbackMeta
	}
	return fallbackItems, fallbackMeta, nil
}

// Symbol lookups must not silently fall back to a limited ranking.
func (p *Provider) StockFundFlowsBySymbols(ctx context.Context, symbols []string) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	provider, ok := p.fundFlowProvider.(interface {
		StockFundFlowsBySymbols(context.Context, []string) ([]foundation.MarketFundFlow, foundation.SourceMeta, error)
	})
	if !ok {
		return nil, foundation.SourceMeta{}, fmt.Errorf("stock fund-flow symbol lookup is unavailable")
	}
	return provider.StockFundFlowsBySymbols(ctx, symbols)
}

func (p *Provider) USSectorMomentum(ctx context.Context, limit int) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error) {
	if provider, ok := p.primary.(USSectorMomentumProvider); ok {
		return provider.USSectorMomentum(ctx, limit)
	}
	if provider, ok := p.indexFallback.(USSectorMomentumProvider); ok {
		items, meta, err := provider.USSectorMomentum(ctx, limit)
		if err != nil {
			return nil, foundation.SourceMeta{}, err
		}
		meta.FallbackReason = joinFallbackReason("主行情源不提供美股板块ETF，已切换腾讯行情", meta.FallbackReason)
		for index := range items {
			items[index].Meta = meta
		}
		return items, meta, nil
	}
	return nil, foundation.SourceMeta{}, fmt.Errorf("US sector momentum provider unavailable")
}

func (p *Provider) MarketMarginSeries(ctx context.Context, limit int) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error) {
	return p.primary.MarketMarginSeries(ctx, limit)
}

func joinFallbackReason(primary string, secondary string) string {
	if secondary == "" {
		return primary
	}
	return primary + "；" + secondary
}

func (p *Provider) MarketBillboard(ctx context.Context, tradeDate string, limit int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error) {
	return p.primary.MarketBillboard(ctx, tradeDate, limit)
}

func (p *Provider) MarketBillboardDetail(ctx context.Context, symbol string, tradeDate string, reason string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
	return p.primary.MarketBillboardDetail(ctx, symbol, tradeDate, reason)
}

func (p *Provider) MarketAnnouncements(ctx context.Context, query string, symbol string, category string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return p.primary.MarketAnnouncements(ctx, query, symbol, category, limit)
}

func (p *Provider) MarketAnnouncementContent(ctx context.Context, id string) (string, error) {
	if provider, ok := p.primary.(interface {
		MarketAnnouncementContent(context.Context, string) (string, error)
	}); ok {
		return provider.MarketAnnouncementContent(ctx, id)
	}
	return "", fmt.Errorf("announcement body provider unavailable")
}

func (p *Provider) MarketReports(ctx context.Context, kind string, query string, symbol string, industry string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return p.primary.MarketReports(ctx, kind, query, symbol, industry, limit)
}

func (p *Provider) MarketReportContent(ctx context.Context, kind string, infoCode string) (foundation.MarketResearchDetail, error) {
	if provider, ok := p.primary.(interface {
		MarketReportContent(context.Context, string, string) (foundation.MarketResearchDetail, error)
	}); ok {
		return provider.MarketReportContent(ctx, kind, infoCode)
	}
	return foundation.MarketResearchDetail{}, fmt.Errorf("report body provider unavailable")
}

// SectorFlowSource 是能提供板块资金流榜单、分时资金序列与分时价格序列的行情源（目前只有东方财富）。
type SectorFlowSource interface {
	SectorFlowRanking(ctx context.Context, dimension string, limit int) ([]foundation.SectorFlowRank, foundation.SourceMeta, error)
	SectorFlowIntraday(ctx context.Context, codes []string) ([]foundation.SectorFlowSeries, foundation.SourceMeta, error)
	SectorPriceIntraday(ctx context.Context, codes []string) ([]foundation.SectorPriceSeries, foundation.SourceMeta, error)
}

func (p *Provider) SectorFlowRanking(ctx context.Context, dimension string, limit int) ([]foundation.SectorFlowRank, foundation.SourceMeta, error) {
	if provider, ok := p.primary.(SectorFlowSource); ok {
		return provider.SectorFlowRanking(ctx, dimension, limit)
	}
	return nil, foundation.SourceMeta{}, fmt.Errorf("sector flow ranking provider unavailable")
}

func (p *Provider) SectorFlowIntraday(ctx context.Context, codes []string) ([]foundation.SectorFlowSeries, foundation.SourceMeta, error) {
	if provider, ok := p.primary.(SectorFlowSource); ok {
		return provider.SectorFlowIntraday(ctx, codes)
	}
	return nil, foundation.SourceMeta{}, fmt.Errorf("sector flow intraday provider unavailable")
}

func (p *Provider) SectorPriceIntraday(ctx context.Context, codes []string) ([]foundation.SectorPriceSeries, foundation.SourceMeta, error) {
	if provider, ok := p.primary.(SectorFlowSource); ok {
		return provider.SectorPriceIntraday(ctx, codes)
	}
	return nil, foundation.SourceMeta{}, fmt.Errorf("sector price intraday provider unavailable")
}
