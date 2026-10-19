package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/sectorflowsample"
)

// sampleFallbackProvider 用固定的上游结果模拟「分时接口失败 / 只返回部分板块」两种情况。
type sampleFallbackProvider struct {
	*fakeMarketOverviewProvider
	flowSeries  []foundation.SectorFlowSeries
	priceSeries []foundation.SectorPriceSeries
	flowErr     error
	priceErr    error
}

func (p *sampleFallbackProvider) SectorFlowRanking(context.Context, string, int) ([]foundation.SectorFlowRank, foundation.SourceMeta, error) {
	return nil, foundation.SourceMeta{}, errors.New("ranking is not used in these tests")
}

func (p *sampleFallbackProvider) SectorFlowIntraday(_ context.Context, codes []string) ([]foundation.SectorFlowSeries, foundation.SourceMeta, error) {
	if p.flowErr != nil {
		return nil, foundation.SourceMeta{}, p.flowErr
	}
	items := make([]foundation.SectorFlowSeries, 0, len(p.flowSeries))
	for _, item := range p.flowSeries {
		for _, code := range codes {
			if item.Code == code {
				items = append(items, item)
			}
		}
	}
	meta := foundation.SourceMeta{Source: "eastmoney:board-intraday-flow"}
	return items, meta, nil
}

func (p *sampleFallbackProvider) SectorPriceIntraday(_ context.Context, codes []string) ([]foundation.SectorPriceSeries, foundation.SourceMeta, error) {
	if p.priceErr != nil {
		return nil, foundation.SourceMeta{}, p.priceErr
	}
	items := make([]foundation.SectorPriceSeries, 0, len(p.priceSeries))
	for _, item := range p.priceSeries {
		for _, code := range codes {
			if item.Code == code {
				items = append(items, item)
			}
		}
	}
	meta := foundation.SourceMeta{Source: "eastmoney:board-intraday-trend"}
	return items, meta, nil
}

func openSectorFlowSampleStore(t *testing.T) *sectorflowsample.Store {
	t.Helper()
	store, err := sectorflowsample.OpenStore(":memory:")
	if err != nil {
		t.Fatalf("open sample store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func todaySamples() string {
	return time.Now().In(foundation.AStockLocation).Format("2006-01-02")
}

func TestSectorFlowIntradayFallsBackToLocalSamples(t *testing.T) {
	store := openSectorFlowSampleStore(t)
	if _, err := store.Save(context.Background(), []sectorflowsample.Sample{
		{TradeDate: todaySamples(), Minute: "09:31", Code: "BK0917", Name: "半导体概念", Dimension: "concept", Main: 100, ChangePercent: 0.5},
		{TradeDate: todaySamples(), Minute: "09:33", Code: "BK0917", Name: "半导体概念", Dimension: "concept", Main: 240, ChangePercent: 0.9},
	}); err != nil {
		t.Fatalf("seed samples: %v", err)
	}
	provider := &sampleFallbackProvider{
		fakeMarketOverviewProvider: &fakeMarketOverviewProvider{},
		flowErr:                    errors.New("EOF"),
	}
	server := NewServer(Config{MarketOverview: provider, SectorFlowSampleStore: store})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/market/sector-flow-intraday?codes=BK0917", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []foundation.SectorFlowSeries `json:"data"`
		Meta foundation.SourceMeta         `json:"meta"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || len(payload.Data[0].Points) != 2 {
		t.Fatalf("unexpected data: %+v", payload.Data)
	}
	if payload.Data[0].LatestMain != 240 || payload.Data[0].Name != "半导体概念" {
		t.Fatalf("unexpected series: %+v", payload.Data[0])
	}
	if payload.Meta.Source != "local:sector-flow-samples" || !payload.Meta.Stale {
		t.Fatalf("unexpected meta: %+v", payload.Meta)
	}
	if payload.Meta.FallbackReason == "" {
		t.Fatal("fallback reason should explain the degraded source")
	}
}

func TestSectorPriceIntradayFallsBackToLocalSamples(t *testing.T) {
	store := openSectorFlowSampleStore(t)
	if _, err := store.Save(context.Background(), []sectorflowsample.Sample{
		{TradeDate: todaySamples(), Minute: "09:31", Code: "BK0917", Name: "半导体概念", Dimension: "concept", ChangePercent: -0.6},
		{TradeDate: todaySamples(), Minute: "13:01", Code: "BK0917", Name: "半导体概念", Dimension: "concept", ChangePercent: 1.2},
	}); err != nil {
		t.Fatalf("seed samples: %v", err)
	}
	provider := &sampleFallbackProvider{
		fakeMarketOverviewProvider: &fakeMarketOverviewProvider{},
		priceErr:                   errors.New("502 Bad Gateway"),
	}
	server := NewServer(Config{MarketOverview: provider, SectorFlowSampleStore: store})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/market/sector-price-intraday?codes=BK0917", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []foundation.SectorPriceSeries `json:"data"`
		Meta foundation.SourceMeta          `json:"meta"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].LatestChange != 1.2 {
		t.Fatalf("unexpected data: %+v", payload.Data)
	}
	if payload.Meta.Source != "local:sector-flow-samples" {
		t.Fatalf("unexpected meta: %+v", payload.Meta)
	}
}

func TestSectorFlowIntradayMergesSamplesForMissingBoards(t *testing.T) {
	store := openSectorFlowSampleStore(t)
	if _, err := store.Save(context.Background(), []sectorflowsample.Sample{
		{TradeDate: todaySamples(), Minute: "10:00", Code: "BK0490", Name: "算力概念", Dimension: "concept", Main: -320},
	}); err != nil {
		t.Fatalf("seed samples: %v", err)
	}
	provider := &sampleFallbackProvider{
		fakeMarketOverviewProvider: &fakeMarketOverviewProvider{},
		flowSeries: []foundation.SectorFlowSeries{
			{Code: "BK0917", Name: "半导体概念", TradeDate: todaySamples(), Points: []foundation.SectorFlowPoint{{Time: "10:00", Main: 88}}, LatestMain: 88},
		},
	}
	server := NewServer(Config{MarketOverview: provider, SectorFlowSampleStore: store})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/market/sector-flow-intraday?codes=BK0490,BK0917", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []foundation.SectorFlowSeries `json:"data"`
		Meta foundation.SourceMeta         `json:"meta"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 2 {
		t.Fatalf("data = %d series, want 2: %+v", len(payload.Data), payload.Data)
	}
	if payload.Meta.FallbackReason == "" || !payload.Meta.Stale {
		t.Fatalf("merged payload should be marked degraded: %+v", payload.Meta)
	}
}

func TestSectorFlowIntradayWithoutSamplesStillFails(t *testing.T) {
	provider := &sampleFallbackProvider{
		fakeMarketOverviewProvider: &fakeMarketOverviewProvider{},
		flowErr:                    errors.New("EOF"),
	}
	server := NewServer(Config{MarketOverview: provider, SectorFlowSampleStore: openSectorFlowSampleStore(t)})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/market/sector-flow-intraday?codes=BK0917", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s, want 502", rec.Code, rec.Body.String())
	}
}
