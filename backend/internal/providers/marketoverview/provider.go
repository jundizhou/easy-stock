package marketoverview

import (
	"context"
	"fmt"

	"easy-stock/backend/internal/foundation"
)

type Primary interface {
	MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error)
	MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error)
	IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error)
	MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error)
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

type IndexFallback interface {
	MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error)
	MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error)
}

type Provider struct {
	primary          Primary
	indexFallback    IndexFallback
	industryProvider IndustryMomentumProvider
	fundFlowProvider FundFlowProvider
}

func New(primary Primary, indexFallback IndexFallback, industryProvider IndustryMomentumProvider, fundFlowProvider FundFlowProvider) *Provider {
	return &Provider{primary: primary, indexFallback: indexFallback, industryProvider: industryProvider, fundFlowProvider: fundFlowProvider}
}

func (p *Provider) MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	items, meta, err := p.primary.MarketIndexes(ctx, scope)
	if err == nil || p.indexFallback == nil {
		return items, meta, err
	}
	fallbackItems, fallbackMeta, fallbackErr := p.indexFallback.MarketIndexes(ctx, scope)
	if fallbackErr != nil {
		return nil, foundation.SourceMeta{}, fmt.Errorf("primary indexes failed: %v; fallback failed: %w", err, fallbackErr)
	}
	fallbackMeta.FallbackReason = "东方财富指数快照不可用，已切换腾讯行情；备用目录暂不包含部分亚太与欧洲指数"
	for index := range fallbackItems {
		fallbackItems[index].Meta = fallbackMeta
	}
	return fallbackItems, fallbackMeta, nil
}

func (p *Provider) MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error) {
	series, err := p.primary.MarketIndexSeries(ctx, id, period, limit)
	if err == nil || p.indexFallback == nil {
		return series, err
	}
	fallback, fallbackErr := p.indexFallback.MarketIndexSeries(ctx, id, period, limit)
	if fallbackErr != nil {
		return foundation.MarketIndexSeries{}, fmt.Errorf("primary index series failed: %v; fallback failed: %w", err, fallbackErr)
	}
	fallback.Meta.FallbackReason = "东方财富指数走势不可用，已切换腾讯 K 线"
	fallback.Index.Meta = fallback.Meta
	for index := range fallback.Lines {
		fallback.Lines[index].Meta = fallback.Meta
	}
	return fallback, nil
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

func (p *Provider) MarketReports(ctx context.Context, kind string, query string, symbol string, industry string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return p.primary.MarketReports(ctx, kind, query, symbol, industry, limit)
}
