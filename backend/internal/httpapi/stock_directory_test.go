package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type countingStockDirectoryProvider struct {
	calls atomic.Int32
}

func (provider *countingStockDirectoryProvider) StockCatalog(context.Context) ([]foundation.StockCatalogEntry, error) {
	provider.calls.Add(1)
	meta := foundation.SourceMeta{Source: "test:stock-directory", FetchedAt: time.Date(2026, 8, 9, 9, 30, 0, 0, time.Local)}
	return []foundation.StockCatalogEntry{
		{BoardStock: foundation.BoardStock{Symbol: "600519.SH", Name: "贵州茅台", Meta: meta}},
		{BoardStock: foundation.BoardStock{Symbol: "000001.SZ", Name: "平安银行", Meta: meta}},
		{BoardStock: foundation.BoardStock{Symbol: "600519.SH", Name: "重复项", Meta: meta}},
		{BoardStock: foundation.BoardStock{Symbol: "", Name: "无效项", Meta: meta}},
	}, nil
}

func TestStockDirectoryCachesNamesAndCodes(t *testing.T) {
	provider := &countingStockDirectoryProvider{}
	server := NewServer(Config{StockDirectory: provider})

	for requestNumber := 0; requestNumber < 2; requestNumber++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/directory", nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, body = %s", requestNumber+1, rec.Code, rec.Body.String())
		}
		var payload struct {
			Data stockDirectoryData `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode stock directory: %v", err)
		}
		if payload.Data.Total != 2 || len(payload.Data.Stocks) != 2 {
			t.Fatalf("unexpected directory size: %+v", payload.Data)
		}
		if payload.Data.Stocks[0].Code != "000001" || payload.Data.Stocks[0].Name != "平安银行" || payload.Data.Stocks[1].Symbol != "600519.SH" {
			t.Fatalf("unexpected stock directory: %+v", payload.Data.Stocks)
		}
		if payload.Data.Source != "test:stock-directory" || payload.Data.UpdatedAt.IsZero() || payload.Data.ExpiresAt.IsZero() {
			t.Fatalf("missing directory cache metadata: %+v", payload.Data)
		}
	}

	if provider.calls.Load() != 1 {
		t.Fatalf("catalog calls = %d, want one cached load", provider.calls.Load())
	}
}
