package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type fixedHotStockProvider struct{}

func (fixedHotStockProvider) HotStockRanks(context.Context, int) []foundation.HotStockRankList {
	fetchedAt := time.Date(2026, 8, 19, 10, 0, 0, 0, time.Local)
	return []foundation.HotStockRankList{
		{Source: "ths", SourceName: "同花顺", FetchedAt: fetchedAt, Items: []foundation.HotStockRankItem{
			{Symbol: "600519.SH", Name: "贵州茅台", Rank: 1},
			{Symbol: "000001.SZ", Name: "平安银行", Rank: 2},
		}},
		{Source: "eastmoney", SourceName: "东方财富", FetchedAt: fetchedAt, Items: []foundation.HotStockRankItem{
			{Symbol: "300750.SZ", Rank: 1},
			{Symbol: "600519.SH", Rank: 20},
		}},
	}
}

func TestHotStockRanksHandlerBuildsDeduplicatedConsensusUnion(t *testing.T) {
	server := NewServer(Config{HotStocks: fixedHotStockProvider{}, StockDirectory: &countingStockDirectoryProvider{}})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/stocks/hot-ranks", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data hotStockRankData `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.Total != 3 || len(payload.Data.Stocks) != 3 {
		t.Fatalf("unexpected union: %+v", payload.Data)
	}
	first := payload.Data.Stocks[0]
	if first.Symbol != "600519.SH" || first.SourceCount != 2 || first.Ranks["ths"] != 1 || first.Ranks["eastmoney"] != 20 {
		t.Fatalf("unexpected consensus leader: %+v", first)
	}
	if len(payload.Data.Sources) != 2 || !payload.Data.Sources[0].Available || payload.Data.Sources[0].Count != 2 {
		t.Fatalf("source status missing: %+v", payload.Data.Sources)
	}
}
