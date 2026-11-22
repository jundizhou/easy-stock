package eastmoney

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStockSuggestIncludesHongKong(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/suggest/get" {
			t.Error("unexpected path", r.URL.Path)
		}
		// 模拟东财对 "腾讯" 的真实返回：港股、美股、A 股近似代码混排。
		fmt.Fprint(w, `{"QuotationCodeTable":{"Status":0,"Data":[
			{"Code":"00700","Name":"腾讯控股","QuoteID":"116.00700","SecurityType":"19"},
			{"Code":"TCTZF","Name":"腾讯控股","QuoteID":"153.TCTZF","SecurityType":"26"},
			{"Code":"00700","Name":"腾讯控股-R","QuoteID":"116.80700","SecurityType":"6"},
			{"Code":"000700","Name":"模塑科技","QuoteID":"0.000700","SecurityType":"2"},
			{"Code":"600519","Name":"贵州茅台","QuoteID":"1.600519","SecurityType":"1"}
		]}}`)
	}))
	defer api.Close()
	client := NewClient(WithBaseURL(api.URL))
	client.indexSearchBaseURL = api.URL

	hits, err := client.StockSuggest(context.Background(), "腾讯", 10)
	if err != nil {
		t.Fatalf("StockSuggest error = %v", err)
	}
	want := []SymbolHit{
		{Symbol: "00700.HK", Code: "00700", Name: "腾讯控股"},
		{Symbol: "80700.HK", Code: "80700", Name: "腾讯控股-R"},
		{Symbol: "000700.SZ", Code: "000700", Name: "模塑科技"},
		{Symbol: "600519.SH", Code: "600519", Name: "贵州茅台"},
	}
	if len(hits) != len(want) {
		t.Fatalf("got %d hits %+v, want %d", len(hits), hits, len(want))
	}
	for i := range want {
		if hits[i] != want[i] {
			t.Errorf("hit[%d] = %+v, want %+v", i, hits[i], want[i])
		}
	}
}

func TestStockSuggestRespectsCountWithHongKong(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"QuotationCodeTable":{"Status":0,"Data":[
			{"Code":"00700","Name":"腾讯控股","QuoteID":"116.00700"},
			{"Code":"09988","Name":"阿里巴巴","QuoteID":"116.09988"},
			{"Code":"00001","Name":"长和","QuoteID":"116.00001"}
		]}}`)
	}))
	defer api.Close()
	client := NewClient(WithBaseURL(api.URL))
	client.indexSearchBaseURL = api.URL

	hits, err := client.StockSuggest(context.Background(), "hk", 2)
	if err != nil {
		t.Fatalf("StockSuggest error = %v", err)
	}
	if len(hits) != 2 || hits[1].Symbol != "09988.HK" {
		t.Fatalf("expected first 2 HK hits, got %+v", hits)
	}
}
