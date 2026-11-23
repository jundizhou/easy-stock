package marketoverview

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

type failingPrimary struct{}

func (failingPrimary) IndexSnapshots(context.Context, []foundation.MarketIndexDefinition) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
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

type symbolFundFlowProvider struct {
	fundFlowProvider
	symbols []string
	fail    bool
}

func (p *symbolFundFlowProvider) StockFundFlowsBySymbols(_ context.Context, symbols []string) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	p.symbols = symbols
	if p.fail {
		return nil, foundation.SourceMeta{}, fmt.Errorf("symbol source unavailable")
	}
	meta := foundation.SourceMeta{Source: "test:symbol-flow"}
	return []foundation.MarketFundFlow{{Symbol: symbols[0], MainNetInflow: -100, Meta: meta}}, meta, nil
}

func TestProviderStockFlowsBySymbolsPreservesValuesAndErrors(t *testing.T) {
	upstream := &symbolFundFlowProvider{}
	provider := New(failingPrimary{}, nil, nil, upstream)
	items, meta, err := provider.StockFundFlowsBySymbols(context.Background(), []string{"600000.SH"})
	if err != nil || len(items) != 1 || items[0].MainNetInflow != -100 || meta.Source != "test:symbol-flow" || len(upstream.symbols) != 1 {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	upstream.fail = true
	if _, _, err := provider.StockFundFlowsBySymbols(context.Background(), []string{"600000.SH"}); err == nil || !strings.Contains(err.Error(), "symbol source unavailable") {
		t.Fatalf("source failure hidden by ranking fallback: %v", err)
	}
	unsupported := New(failingPrimary{}, nil, nil, fundFlowProvider{})
	if _, _, err := unsupported.StockFundFlowsBySymbols(context.Background(), []string{"600000.SH"}); err == nil {
		t.Fatal("ranking-only provider must not pretend to support symbol queries")
	}
}

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

func (indexFallback) IndexSnapshots(context.Context, []foundation.MarketIndexDefinition) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	meta := foundation.SourceMeta{Source: "tencent:index"}
	return []foundation.MarketIndexSnapshot{{ID: "sse", Name: "上证指数", Price: 3934.09, Meta: meta}}, meta, nil
}

func (indexFallback) MarketIndexSeries(context.Context, string, string, int) (foundation.MarketIndexSeries, error) {
	meta := foundation.SourceMeta{Source: "tencent:index-kline"}
	return foundation.MarketIndexSeries{Index: foundation.MarketIndexSnapshot{ID: "sse", Meta: meta}, Lines: []foundation.KLine{{Symbol: "sse", Close: 3934.09, Meta: meta}}, Meta: meta}, nil
}

func (indexFallback) USSectorMomentum(context.Context, int) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error) {
	meta := foundation.SourceMeta{Source: "tencent:us-sector-etf"}
	return []foundation.MarketUSSectorMomentum{{ProxySymbol: "XLK", Name: "信息技术", ChangePercent: 1.5, Meta: meta}}, meta, nil
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
	if err != nil || len(items) != 1 || meta.Source != "tencent:index" || !strings.Contains(meta.FallbackReason, "1/11") {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	series, err := provider.MarketIndexSeries(context.Background(), "sse", "day", 20)
	if err != nil || len(series.Lines) != 1 || series.Meta.Source != "tencent:index-kline" || !strings.Contains(series.Meta.FallbackReason, "腾讯") {
		t.Fatalf("series=%+v err=%v", series, err)
	}
}

func TestProviderFallsBackForUSSectorMomentum(t *testing.T) {
	provider := New(failingPrimary{}, indexFallback{}, nil, nil)
	items, meta, err := provider.USSectorMomentum(context.Background(), 11)
	if err != nil || len(items) != 1 || items[0].ProxySymbol != "XLK" || !strings.Contains(meta.FallbackReason, "腾讯") {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
}

type quoteFixture struct {
	failingPrimary
	load func([]foundation.MarketIndexDefinition) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error)
}

func (p quoteFixture) IndexSnapshots(_ context.Context, defs []foundation.MarketIndexDefinition) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	return p.load(defs)
}

func TestIndexesFillOnlyMissingIDsAndKeepRowSources(t *testing.T) {
	source := func(name string, predicate func(foundation.MarketIndexDefinition) bool) quoteFixture {
		return quoteFixture{load: func(defs []foundation.MarketIndexDefinition) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
			meta := foundation.SourceMeta{Source: name}
			var rows []foundation.MarketIndexSnapshot
			for _, d := range defs {
				if predicate(d) {
					rows = append(rows, foundation.MarketIndexSnapshot{ID: d.ID, Price: 101, Meta: meta})
				}
			}
			return rows, meta, nil
		}}
	}
	domestic := source("tdx:index", func(d foundation.MarketIndexDefinition) bool { return d.Market == "CN" && d.ID != "szse" })
	second := source("tencent:index", func(d foundation.MarketIndexDefinition) bool {
		if d.ID == "sse" {
			t.Fatal("redundant request for Shanghai index")
		}
		return true
	})
	unused := quoteFixture{load: func([]foundation.MarketIndexDefinition) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
		t.Fatal("complete batch reached East Money")
		return nil, foundation.SourceMeta{}, nil
	}}
	p := New(unused, second, nil, nil, WithDomesticIndexes(domestic))
	rows, meta, err := p.MarketIndexes(context.Background(), "core")
	if err != nil || len(rows) != 11 || rows[0].ID != "sse" || rows[0].Meta.Source != "tdx:index" || rows[1].ID != "szse" || rows[1].Meta.Source != "tencent:index" || meta.FallbackReason != "" {
		t.Fatalf("rows=%v meta=%v err=%v", rows, meta, err)
	}
}

func TestIndexSnapshotsAndSeriesExposeTotalOutage(t *testing.T) {
	p := New(failingPrimary{}, failingPrimary{}, nil, nil, WithDomesticIndexes(failingPrimary{}))
	if rows, _, err := p.MarketIndexes(context.Background(), "core"); len(rows) > 0 || err == nil {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if _, err := p.MarketIndexSeries(context.Background(), "sse", "day", 2); err == nil {
		t.Fatal("reported empty series as healthy")
	}
}
