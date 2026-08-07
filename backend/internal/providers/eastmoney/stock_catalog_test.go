package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestClientStockCatalogParsesPagesAndCachesSnapshot(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/dataapi/xuangu/list" {
			t.Fatalf("path = %s, want /dataapi/xuangu/list", r.URL.Path)
		}
		if r.URL.Query().Get("source") != "SELECT_SECURITIES" || r.URL.Query().Get("filter") != aShareMarketFilter {
			t.Fatalf("unexpected stock catalog query: %s", r.URL.RawQuery)
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("p"))
		switch page {
		case 1:
			_, _ = w.Write([]byte(`{
				"code":0,"success":true,"message":"ok",
				"result":{"nextpage":true,"currentpage":1,"count":2,"data":[
					{"SECUCODE":"600664.SH","SECURITY_CODE":"600664","SECURITY_NAME_ABBR":"哈药股份","NEW_PRICE":5.33,"CHANGE_RATE":7.89,"CHANGERATE_5DAYS":12.34,"VOLUME":8740456,"DEAL_AMOUNT":4522777492,"INDUSTRY":"化学制药","CONCEPT":["创新药","中药概念"]},
					{"SECUCODE":"600000.SH","SECURITY_CODE":"600000","SECURITY_NAME_ABBR":"停牌样本","NEW_PRICE":"-","CHANGE_RATE":"-","VOLUME":"-","DEAL_AMOUNT":"-","INDUSTRY":"银行","CONCEPT":null}
				]}
			}`))
		case 2:
			_, _ = w.Write([]byte(`{
				"code":0,"success":true,"message":"ok",
				"result":{"nextpage":false,"currentpage":2,"count":2,"data":[
					{"SECUCODE":"301520.SZ","SECURITY_CODE":"301520","SECURITY_NAME_ABBR":"万邦医药","NEW_PRICE":66.62,"CHANGE_RATE":-0.05,"VOLUME":169810,"DEAL_AMOUNT":1119678337.95,"INDUSTRY":"生物医药","CONCEPT":"CRO"}
				]}
			}`))
		default:
			t.Fatalf("unexpected page %d", page)
		}
	}))
	defer server.Close()

	client := NewClient(WithDataBaseURL(server.URL))
	stocks, err := client.StockCatalog(context.Background())
	if err != nil {
		t.Fatalf("StockCatalog failed: %v", err)
	}
	if len(stocks) != 3 {
		t.Fatalf("len = %d, want 3", len(stocks))
	}
	if stocks[0].Symbol != "600664.SH" || stocks[0].FiveDayChangePercent != 12.34 || stocks[0].Industry != "化学制药" || len(stocks[0].Concepts) != 2 {
		t.Fatalf("unexpected 哈药股份 catalog row: %+v", stocks[0])
	}
	if stocks[1].Price != 0 || stocks[1].ChangePercent != 0 || stocks[1].Amount != 0 {
		t.Fatalf("dash-valued quote fields should parse as zero: %+v", stocks[1])
	}
	if stocks[2].Symbol != "301520.SZ" || len(stocks[2].Concepts) != 1 || stocks[2].Concepts[0] != "CRO" {
		t.Fatalf("unexpected 万邦医药 catalog row: %+v", stocks[2])
	}
	if stocks[0].Meta.Source != "eastmoney:stock-selection" || stocks[0].Meta.SourceURL == "" {
		t.Fatalf("missing source evidence: %+v", stocks[0].Meta)
	}

	stocks[0].Concepts[0] = "mutated"
	cached, err := client.StockCatalog(context.Background())
	if err != nil {
		t.Fatalf("cached StockCatalog failed: %v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("request count = %d, want 2 after cache hit", requests.Load())
	}
	if cached[0].Concepts[0] != "创新药" {
		t.Fatalf("cache should return a defensive copy: %+v", cached[0].Concepts)
	}
}
