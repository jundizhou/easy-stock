package sina

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStockFundFlowsParsesAndFiltersAStocks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"symbol":"sh511360","name":"ETF样本","trade":"100","changeratio":"0.01","r0_net":"999","r0_ratio":"0.1","r3_net":"0","r3_ratio":"0"},
			{"symbol":"sz300308","name":"中际旭创","trade":"886.96","changeratio":"0.0258854","inamount":"14973590253.36","outamount":"11412068826.98","netamount":"3561521426.38","ratioamount":"0.118881","r0_in":"14827154503.36","r0_out":"11255228027.66","r0_net":"3571926475.7","r0_ratio":"0.119229","r3_in":"1000","r3_out":"2000","r3_net":"-1000","r3_ratio":"-0.0001"}
		]`))
	}))
	defer upstream.Close()
	client := NewClient(WithMoneyFlowBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))

	items, meta, err := client.StockFundFlows(context.Background(), "net", 20)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	item := items[0]
	if item.Symbol != "300308.SZ" || item.ChangePercent < 2.58 || item.NetInflow < 3_500_000_000 || item.MainNetInflowRatio < 11.92 || item.RetailNetInflow != -1000 || meta.Source != "sina:stock-money-flow" {
		t.Fatalf("item=%+v meta=%+v", item, meta)
	}
	if len(meta.AvailableFields) == 0 {
		t.Fatalf("available fields missing: %+v", meta)
	}
}

func TestSectorFundFlowsParsesIndustryAndThemeFields(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"category":"gn_gfgn","name":"光伏概念","avg_price":"15.1765","avg_changeratio":"0.0289572",
			"inamount":"61322958051.38","outamount":"57674302676.16","netamount":"3648655375.22","ratioamount":"0.0287783",
			"ts_symbol":"sz002248","ts_name":"华东数控","ts_trade":"11.06","ts_changeratio":"0.100498","ts_ratioamount":"0.0722581"
		}]`))
	}))
	defer upstream.Close()
	client := NewClient(WithSectorMoneyFlowBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))

	items, meta, err := client.MarketFundFlows(context.Background(), "theme", "ratio", 20)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	item := items[0]
	if item.Code != "gn_gfgn" || item.ChangePercent < 2.89 || item.NetInflow < 3_600_000_000 || item.LeaderSymbol != "002248.SZ" || item.LeaderChange < 10 || meta.Source != "sina:theme-money-flow" {
		t.Fatalf("item=%+v meta=%+v", item, meta)
	}
}
