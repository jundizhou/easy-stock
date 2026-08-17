package marketoverview

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

type failingPrimary struct{}

func (failingPrimary) MarketIndexes(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, fmt.Errorf("index unavailable")
}
func (failingPrimary) MarketIndexSeries(context.Context, string, string, int) (foundation.MarketIndexSeries, error) {
	return foundation.MarketIndexSeries{}, fmt.Errorf("series unavailable")
}
func (failingPrimary) IndustryMomentum(context.Context, int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}
func (failingPrimary) MarketFundFlows(context.Context, string, string, int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, fmt.Errorf("eastmoney unavailable")
}
func (failingPrimary) MarketMarginSeries(context.Context, int) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, fmt.Errorf("margin balance unavailable")
}
func (failingPrimary) MarketBillboard(context.Context, string, int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}
func (failingPrimary) MarketBillboardDetail(context.Context, string, string, string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
	return foundation.MarketBillboardDetail{}, foundation.SourceMeta{}, nil
}
func (failingPrimary) MarketAnnouncements(context.Context, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}
func (failingPrimary) MarketReports(context.Context, string, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}

type fundFlowProvider struct{}

func (fundFlowProvider) MarketFundFlows(_ context.Context, dimension string, _ string, _ int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	meta := foundation.SourceMeta{Source: "sina:" + dimension + "-money-flow"}
	return []foundation.MarketFundFlow{{Dimension: dimension, Symbol: "300308.SZ", Name: "样本资金", Meta: meta}}, meta, nil
}

type industryProvider struct{}

func (industryProvider) IndustryMomentum(context.Context, int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	meta := foundation.SourceMeta{Source: "tencent:industry-rank"}
	return []foundation.MarketIndustryMomentum{{Code: "pt01", Name: "光伏设备", ChangePercent: 1.76, Meta: meta}}, meta, nil
}

type indexFallback struct{}

func (indexFallback) MarketIndexes(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	meta := foundation.SourceMeta{Source: "tencent:index"}
	return []foundation.MarketIndexSnapshot{{ID: "sse", Name: "上证指数", Meta: meta}}, meta, nil
}

func (indexFallback) MarketIndexSeries(context.Context, string, string, int) (foundation.MarketIndexSeries, error) {
	meta := foundation.SourceMeta{Source: "tencent:index-kline"}
	return foundation.MarketIndexSeries{Index: foundation.MarketIndexSnapshot{ID: "sse", Meta: meta}, Lines: []foundation.KLine{{Symbol: "sse", Close: 3934.09, Meta: meta}}, Meta: meta}, nil
}

func TestProviderUsesSinaFundFlowsForEveryDimension(t *testing.T) {
	provider := New(failingPrimary{}, nil, nil, fundFlowProvider{})
	items, meta, err := provider.MarketFundFlows(context.Background(), "stock", "net", 20)
	if err != nil || len(items) != 1 || meta.Source != "sina:stock-money-flow" {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	items, meta, err = provider.MarketFundFlows(context.Background(), "industry", "net", 20)
	if err != nil || len(items) != 1 || meta.Source != "sina:industry-money-flow" {
		t.Fatalf("industry items=%+v meta=%+v err=%v", items, meta, err)
	}
}

func TestProviderUsesTencentIndustryMomentum(t *testing.T) {
	provider := New(failingPrimary{}, nil, industryProvider{}, nil)
	items, meta, err := provider.IndustryMomentum(context.Background(), 20)
	if err != nil || len(items) != 1 || meta.Source != "tencent:industry-rank" || items[0].ChangePercent != 1.76 {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
}

func TestProviderFallsBackForIndexes(t *testing.T) {
	provider := New(failingPrimary{}, indexFallback{}, nil, nil)
	items, meta, err := provider.MarketIndexes(context.Background(), "core")
	if err != nil || len(items) != 1 || meta.Source != "tencent:index" || !strings.Contains(meta.FallbackReason, "腾讯") {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	series, err := provider.MarketIndexSeries(context.Background(), "sse", "day", 20)
	if err != nil || len(series.Lines) != 1 || !strings.Contains(series.Meta.FallbackReason, "腾讯") {
		t.Fatalf("series=%+v err=%v", series, err)
	}
}
