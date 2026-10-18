package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"easy-stock/backend/internal/foundation"
)

type fakeSectorFlowProvider struct {
	*fakeMarketOverviewProvider
	mu              sync.Mutex
	rankingCalls    int
	intradayCalls   int
	lastDimension   string
	lastLimit       int
	lastCodes       []string
	rankingFailure  error
	intradayFailure error
}

func (p *fakeSectorFlowProvider) SectorFlowRanking(_ context.Context, dimension string, limit int) ([]foundation.SectorFlowRank, foundation.SourceMeta, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rankingCalls++
	p.lastDimension = dimension
	p.lastLimit = limit
	if p.rankingFailure != nil {
		return nil, foundation.SourceMeta{}, p.rankingFailure
	}
	meta := foundation.SourceMeta{Source: "eastmoney:bkzj"}
	return []foundation.SectorFlowRank{
		{Code: "BK0917", Name: "半导体概念", ChangePercent: -0.18, MainNetInflow: 3981310464, MainNetInflowRatio: 11.72, FiveDayMainNet: -26905739776, TenDayMainNet: -99373526272, Meta: meta},
	}, meta, nil
}

func (p *fakeSectorFlowProvider) SectorFlowIntraday(_ context.Context, codes []string) ([]foundation.SectorFlowSeries, foundation.SourceMeta, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.intradayCalls++
	p.lastCodes = append([]string(nil), codes...)
	if p.intradayFailure != nil {
		return nil, foundation.SourceMeta{}, p.intradayFailure
	}
	meta := foundation.SourceMeta{Source: "eastmoney:board-intraday-flow", TradeDate: "2026-10-16"}
	items := make([]foundation.SectorFlowSeries, 0, len(codes))
	for _, code := range codes {
		items = append(items, foundation.SectorFlowSeries{
			Code:       code,
			Name:       "半导体概念",
			TradeDate:  "2026-10-16",
			Points:     []foundation.SectorFlowPoint{{Time: "09:31", Main: 100}, {Time: "15:00", Main: 200}},
			LatestMain: 200,
			Meta:       meta,
		})
	}
	return items, meta, nil
}

func (p *fakeSectorFlowProvider) counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rankingCalls, p.intradayCalls
}

func (p *fakeSectorFlowProvider) codes() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.lastCodes...)
}

func (p *fakeSectorFlowProvider) rankingQuery() (string, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastDimension, p.lastLimit
}

func TestSectorFlowRankingRouteReturnsEvidenceAndCaches(t *testing.T) {
	provider := &fakeSectorFlowProvider{fakeMarketOverviewProvider: &fakeMarketOverviewProvider{}}
	server := NewServer(Config{MarketOverview: provider})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/market/sector-flows", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []foundation.SectorFlowRank `json:"data"`
		Meta foundation.SourceMeta       `json:"meta"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].Name != "半导体概念" || payload.Data[0].FiveDayMainNet != -26905739776 {
		t.Fatalf("unexpected data: %+v", payload.Data)
	}
	if payload.Meta.Source != "eastmoney:bkzj" {
		t.Fatalf("unexpected meta: %+v", payload.Meta)
	}
	if dimension, limit := provider.rankingQuery(); dimension != "concept" || limit != 200 {
		t.Fatalf("default query = %s/%d, want concept/200", dimension, limit)
	}

	// 缓存命中后不再打上游，避免盘中重复请求。
	cached := httptest.NewRecorder()
	server.ServeHTTP(cached, httptest.NewRequest(http.MethodGet, "/api/v1/market/sector-flows", nil))
	if calls, _ := provider.counts(); calls != 1 {
		t.Fatalf("ranking calls = %d, want 1 (cache hit expected)", calls)
	}

	industry := httptest.NewRecorder()
	server.ServeHTTP(industry, httptest.NewRequest(http.MethodGet, "/api/v1/market/sector-flows?dimension=industry&limit=40", nil))
	if industry.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", industry.Code, industry.Body.String())
	}
	if dimension, limit := provider.rankingQuery(); dimension != "industry" || limit != 40 {
		t.Fatalf("industry query = %s/%d, want industry/40", dimension, limit)
	}
}

func TestSectorFlowRankingRouteValidatesQueries(t *testing.T) {
	server := NewServer(Config{MarketOverview: &fakeSectorFlowProvider{fakeMarketOverviewProvider: &fakeMarketOverviewProvider{}}})
	for _, path := range []string{
		"/api/v1/market/sector-flows?dimension=stock",
		"/api/v1/market/sector-flows?limit=0",
		"/api/v1/market/sector-flows?limit=1000",
	} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestSectorFlowIntradayRouteNormalizesAndCachesCodes(t *testing.T) {
	provider := &fakeSectorFlowProvider{fakeMarketOverviewProvider: &fakeMarketOverviewProvider{}}
	server := NewServer(Config{MarketOverview: provider})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/market/sector-flow-intraday?codes=BK0490,bk0917,BK0490", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := provider.codes(); len(got) != 2 || got[0] != "BK0490" || got[1] != "BK0917" {
		t.Fatalf("codes = %+v, want [BK0490 BK0917]", got)
	}
	var payload struct {
		Data []foundation.SectorFlowSeries `json:"data"`
		Meta foundation.SourceMeta         `json:"meta"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 2 || payload.Data[0].LatestMain != 200 || payload.Meta.TradeDate != "2026-10-16" {
		t.Fatalf("unexpected payload: %+v / %+v", payload.Data, payload.Meta)
	}

	// 同一组板块用不同顺序请求时命中同一个缓存键。
	reordered := httptest.NewRecorder()
	server.ServeHTTP(reordered, httptest.NewRequest(http.MethodGet, "/api/v1/market/sector-flow-intraday?codes=BK0917,BK0490", nil))
	if reordered.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", reordered.Code, reordered.Body.String())
	}
	if _, calls := provider.counts(); calls != 1 {
		t.Fatalf("intraday calls = %d, want 1 (cache hit expected)", calls)
	}
}

func TestSectorFlowIntradayRouteValidatesCodes(t *testing.T) {
	server := NewServer(Config{MarketOverview: &fakeSectorFlowProvider{fakeMarketOverviewProvider: &fakeMarketOverviewProvider{}}})
	tooMany := make([]string, 0, 13)
	for index := 0; index < 13; index++ {
		tooMany = append(tooMany, fmt.Sprintf("BK%04d", index))
	}
	for _, path := range []string{
		"/api/v1/market/sector-flow-intraday",
		"/api/v1/market/sector-flow-intraday?codes=,,,",
		"/api/v1/market/sector-flow-intraday?codes=hello",
		"/api/v1/market/sector-flow-intraday?codes=BK0917,hello",
		"/api/v1/market/sector-flow-intraday?codes=" + strings.Join(tooMany, ","),
	} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestSectorFlowRoutesReportUnavailableProvider(t *testing.T) {
	// 旧行情源不提供板块资金流时，接口应明确返回 503，而不是空榜单。
	server := NewServer(Config{MarketOverview: &fakeMarketOverviewProvider{}})
	for _, path := range []string{
		"/api/v1/market/sector-flows",
		"/api/v1/market/sector-flow-intraday?codes=BK0917",
	} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}
