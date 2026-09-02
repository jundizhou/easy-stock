package httpapi

import (
	"context"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/review"
)

type fakeUSMarketOverview struct{}

func (fakeUSMarketOverview) MarketIndexes(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	meta := foundation.SourceMeta{Source: "test:index"}
	return []foundation.MarketIndexSnapshot{
		{ID: "dow", Market: "US", Name: "道琼斯", Price: 100, ChangePercent: 0.5, TradeTime: time.Date(2026, 8, 28, 5, 0, 0, 0, time.UTC), Meta: meta},
		{ID: "sse", Market: "CN", Name: "上证指数", Price: 3000, ChangePercent: 1, Meta: meta},
	}, meta, nil
}

func (fakeUSMarketOverview) MarketIndexSeries(context.Context, string, string, int) (foundation.MarketIndexSeries, error) {
	return foundation.MarketIndexSeries{}, nil
}
func (fakeUSMarketOverview) IndustryMomentum(context.Context, int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}
func (fakeUSMarketOverview) MarketFundFlows(context.Context, string, string, int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}
func (fakeUSMarketOverview) MarketMarginSeries(context.Context, int) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}
func (fakeUSMarketOverview) MarketBillboard(context.Context, string, int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}
func (fakeUSMarketOverview) MarketBillboardDetail(context.Context, string, string, string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
	return foundation.MarketBillboardDetail{}, foundation.SourceMeta{}, nil
}
func (fakeUSMarketOverview) MarketAnnouncements(context.Context, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}
func (fakeUSMarketOverview) MarketReports(context.Context, string, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, nil
}
func (fakeUSMarketOverview) USSectorMomentum(context.Context, int) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error) {
	meta := foundation.SourceMeta{Source: "test:sector"}
	return []foundation.MarketUSSectorMomentum{
		{ProxySymbol: "XLF", Name: "金融", ChangePercent: 1.2, TradeTime: time.Date(2026, 8, 28, 5, 0, 0, 0, time.UTC), Meta: meta},
		{ProxySymbol: "XLK", Name: "信息技术", ChangePercent: -1.3, TradeTime: time.Date(2026, 8, 28, 5, 0, 0, 0, time.UTC), Meta: meta},
	}, meta, nil
}

func TestReviewDailyMarketProviderFiltersUSIndexesAndSplitsSectors(t *testing.T) {
	provider := newReviewDailyMarketProvider(fakeUSMarketOverview{})
	market, err := provider.Snapshot(context.Background(), time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(market.Indexes) != 1 || market.Indexes[0].ID != "dow" || len(market.LeadingSectors) != 1 || market.LeadingSectors[0].ProxySymbol != "XLF" || len(market.LaggingSectors) != 1 || market.LaggingSectors[0].ProxySymbol != "XLK" {
		t.Fatalf("market=%+v", market)
	}
	if market.AsOf == "" || len(market.DataQuality) != 0 {
		t.Fatalf("market metadata=%+v", market)
	}
}

var _ review.DailyMarketProvider = reviewDailyMarketProvider{}
