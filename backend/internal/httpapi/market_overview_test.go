package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type fakeMarketOverviewProvider struct {
	fail bool
}

func (p *fakeMarketOverviewProvider) source() (foundation.SourceMeta, error) {
	if p.fail {
		return foundation.SourceMeta{}, fmt.Errorf("upstream unavailable")
	}
	return foundation.SourceMeta{Source: "test:market", FetchedAt: time.Now()}, nil
}

func (p *fakeMarketOverviewProvider) MarketIndexes(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	meta, err := p.source()
	return []foundation.MarketIndexSnapshot{{ID: "sse", Name: "上证指数", Price: 3450, Meta: meta}}, meta, err
}

func (p *fakeMarketOverviewProvider) MarketIndexSeries(context.Context, string, string, int) (foundation.MarketIndexSeries, error) {
	meta, err := p.source()
	return foundation.MarketIndexSeries{Index: foundation.MarketIndexSnapshot{ID: "sse", Name: "上证指数", Meta: meta}, Lines: []foundation.KLine{{Symbol: "sse", Close: 3450, Meta: meta}}, Meta: meta}, err
}

func (p *fakeMarketOverviewProvider) IndustryMomentum(context.Context, int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	meta, err := p.source()
	return []foundation.MarketIndustryMomentum{{Code: "BK001", Name: "电子", Score: 88, Meta: meta}}, meta, err
}

func (p *fakeMarketOverviewProvider) MarketFundFlows(_ context.Context, dimension string, _ string, _ int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	meta, err := p.source()
	return []foundation.MarketFundFlow{{Dimension: dimension, Code: "BK001", Name: "电子", MainNetInflow: 100, Meta: meta}}, meta, err
}

func (p *fakeMarketOverviewProvider) MarketBillboard(context.Context, string, int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error) {
	meta, err := p.source()
	return []foundation.MarketBillboardItem{{TradeDate: "2026-08-11", Symbol: "600001.SH", Name: "样本股份", NetAmount: 100, Meta: meta}}, meta, err
}

func (p *fakeMarketOverviewProvider) MarketBillboardDetail(context.Context, string, string, string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
	meta, err := p.source()
	detail := foundation.MarketBillboardDetail{
		TradeDate: "2026-08-11",
		Symbol:    "600001.SH",
		Reason:    "日涨幅偏离值达7%",
		BuySeats:  []foundation.MarketBillboardSeat{{Direction: "buy", Rank: 1, Name: "机构专用", BuyAmount: 100, Institution: true}},
		SellSeats: []foundation.MarketBillboardSeat{{Direction: "sell", Rank: 1, Name: "样本营业部", SellAmount: 80}},
		Meta:      meta,
	}
	return detail, meta, err
}

func (p *fakeMarketOverviewProvider) MarketAnnouncements(context.Context, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	meta, err := p.source()
	return []foundation.MarketResearchItem{{Kind: "announcement", ID: "A1", Title: "公告", Meta: meta}}, meta, err
}

func (p *fakeMarketOverviewProvider) MarketReports(_ context.Context, kind string, _ string, _ string, _ string, _ int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	meta, err := p.source()
	return []foundation.MarketResearchItem{{Kind: kind, ID: "R1", Title: "研报", Meta: meta}}, meta, err
}

func TestMarketOverviewRoutesReturnStructuredData(t *testing.T) {
	server := NewServer(Config{MarketOverview: &fakeMarketOverviewProvider{}})
	tests := []struct {
		path string
		want string
	}{
		{"/api/v1/market/indexes?scope=core", "上证指数"},
		{"/api/v1/market/index-series?id=sse&period=day&limit=30", "lines"},
		{"/api/v1/market/industries?limit=20", "电子"},
		{"/api/v1/market/flows?dimension=theme&sort=ratio&limit=20", "theme"},
		{"/api/v1/market/billboard?trade_date=2026-08-11", "样本股份"},
		{"/api/v1/market/billboard/detail?symbol=600001.SH&trade_date=2026-08-11&reason=%E6%97%A5%E6%B6%A8%E5%B9%85%E5%81%8F%E7%A6%BB%E5%80%BC%E8%BE%BE7%25", "机构专用"},
		{"/api/v1/research/announcements?q=公告", "announcement"},
		{"/api/v1/research/institution-reports?q=研报", "stock"},
		{"/api/v1/research/industries?industry=电子", "industry"},
	}
	for _, test := range tests {
		req := httptest.NewRequest(http.MethodGet, test.path, nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), test.want) {
			t.Errorf("%s status=%d body=%s", test.path, rec.Code, rec.Body.String())
		}
	}
}

func TestMarketOverviewValidatesQueries(t *testing.T) {
	server := NewServer(Config{MarketOverview: &fakeMarketOverviewProvider{}})
	for _, path := range []string{
		"/api/v1/market/indexes?scope=invalid",
		"/api/v1/market/index-series",
		"/api/v1/market/flows?dimension=invalid",
		"/api/v1/market/billboard?trade_date=11-08-2026",
		"/api/v1/market/billboard/detail?symbol=600001.SH&trade_date=11-08-2026&reason=test",
		"/api/v1/market/billboard/detail?symbol=600001.SH&trade_date=2026-08-11",
		"/api/v1/market/industries?limit=1000",
	} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestMarketOverviewReturnsStaleSnapshotOnRefreshFailure(t *testing.T) {
	provider := &fakeMarketOverviewProvider{}
	server := NewServer(Config{MarketOverview: provider})
	server.marketSnapshots.ttl = -time.Second
	path := "/api/v1/market/indexes?scope=core"

	first := httptest.NewRecorder()
	server.ServeHTTP(first, httptest.NewRequest(http.MethodGet, path, nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	provider.fail = true
	second := httptest.NewRecorder()
	server.ServeHTTP(second, httptest.NewRequest(http.MethodGet, path, nil))
	if second.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	var payload struct {
		Meta foundation.SourceMeta `json:"meta"`
	}
	if err := json.NewDecoder(second.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Meta.Stale || !strings.Contains(payload.Meta.FallbackReason, "upstream unavailable") {
		t.Fatalf("meta=%+v", payload.Meta)
	}
}
