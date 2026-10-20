package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/themelibrary"
)

func newThemeLibraryTestStore(t *testing.T) *themelibrary.Store {
	t.Helper()
	store, err := themelibrary.OpenWithSeed(":memory:", themelibrary.Seed{
		SchemaVersion:   1,
		Source:          "bigamap",
		SourceName:      "开盘啦题材库",
		SourceURL:       "https://www.bigamap.cn/app",
		APIURL:          "https://www.bigamap.cn/api/v1/public/theme-library",
		SourceUpdatedAt: "2026-10-19T09:41:51Z",
		GeneratedAt:     "2026-10-19T10:00:00Z",
		Selection:       "test",
		ThemeCount:      1,
		Themes: []themelibrary.Theme{{
			SourceID:       "42",
			Name:           "5G-A",
			Description:    "5G-A 商用部署",
			Introduction:   "题材相关新闻\n\n中国移动发布 5G-A。",
			ChangePercent:  0.67,
			StockCount:     1,
			LimitUpCount:   1,
			Hot:            8977,
			ThemeUpdatedAt: "2026-07-24T06:33:04Z",
			AsOf:           "2026-10-19T09:41:51Z",
			SourceURL:      "https://www.bigamap.cn/app",
			Groups: []themelibrary.Group{{
				SourceID: "262",
				Name:     "天线",
				Path:     []string{"天线"},
				Stocks: []themelibrary.Stock{{
					Code: "002792", Name: "通宇通讯", Price: 26.69, ChangePercent: 1.56, IsLimitUp: true, Hot: 466,
					Tags: []string{"5G", "6G"},
				}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("open theme library store: %v", err)
	}
	return store
}

func TestThemeLibraryHandlers(t *testing.T) {
	server := NewServer(Config{ThemeLibraryStore: newThemeLibraryTestStore(t)})
	defer server.Close()

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/market/theme-library?q=5G&sort=hot", nil)
	listResponse := httptest.NewRecorder()
	server.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var listPayload struct {
		Data themelibrary.Library  `json:"data"`
		Meta foundation.SourceMeta `json:"meta"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&listPayload); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if !listPayload.Data.Available || listPayload.Data.Total != 1 || listPayload.Data.Items[0].Name != "5G-A" {
		t.Fatalf("unexpected list payload: %+v", listPayload.Data)
	}
	if !listPayload.Meta.Stale || listPayload.Meta.SourceURL != "https://www.bigamap.cn/app" {
		t.Fatalf("unexpected list meta: %+v", listPayload.Meta)
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/market/theme-library/42", nil)
	detailResponse := httptest.NewRecorder()
	server.ServeHTTP(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status = %d: %s", detailResponse.Code, detailResponse.Body.String())
	}
	var detailPayload struct {
		Data themelibrary.Theme `json:"data"`
	}
	if err := json.NewDecoder(detailResponse.Body).Decode(&detailPayload); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detailPayload.Data.Name != "5G-A" || len(detailPayload.Data.Groups) != 1 {
		t.Fatalf("unexpected detail payload: %+v", detailPayload.Data)
	}
	if detailPayload.Data.Groups[0].Stocks[0].Code != "002792" || !detailPayload.Data.Groups[0].Stocks[0].IsLimitUp {
		t.Fatalf("unexpected detail stock: %+v", detailPayload.Data.Groups[0].Stocks)
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/api/v1/market/theme-library/missing", nil)
	missingResponse := httptest.NewRecorder()
	server.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d", missingResponse.Code, http.StatusNotFound)
	}
}

func TestThemeLibraryHandlerUnavailable(t *testing.T) {
	server := NewServer(Config{})
	defer server.Close()

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/market/theme-library", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data themelibrary.Library `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Data.Available {
		t.Fatalf("expected unavailable library: %+v", payload.Data)
	}
}
