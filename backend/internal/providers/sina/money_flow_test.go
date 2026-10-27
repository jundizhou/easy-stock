package sina

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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

func TestStockFundFlowsBySymbolsIncludesUnrankedAndSTStocks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("num") != "10000" || r.URL.Query().Get("sort") != "symbol" {
			t.Errorf("must request complete snapshot: %s", r.URL.RawQuery)
		}
		rows := make([]map[string]any, 250)
		for index := range rows {
			rows[index] = map[string]any{"symbol": "sz300308", "name": "unrelated", "netamount": "999"}
		}
		rows = append(rows,
			map[string]any{"symbol": "sh600000", "name": "ST watched", "trade": "10", "changeratio": "-0.02", "netamount": "-10000", "r0_net": "-20000", "r0_ratio": "-0.1", "r3_net": "0", "r3_ratio": "0"},
			map[string]any{"symbol": "sh600000", "netamount": "555"})
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer upstream.Close()
	client := NewClient(WithMoneyFlowBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	items, _, err := client.StockFundFlowsBySymbols(context.Background(), []string{"600000.SH", "sh600000", "000001.SZ"})
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	item := items[0]
	if item.Symbol != "600000.SH" || item.NetInflow != -10000 || item.MainNetInflowRatio != -10 || item.ChangePercent != -2 || item.RetailNetInflow != 0 {
		t.Fatalf("wrong source values: %+v", item)
	}
	if !slices.Contains(item.Meta.AvailableFields, "retail_net_inflow") || slices.Contains(item.Meta.AvailableFields, "net_inflow_ratio") {
		t.Fatalf("real zero and missing field were conflated: %+v", item.Meta)
	}
}

func TestStockFundFlowsBySymbolsMarksInvalidNumbersUnavailable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"symbol":"sz000001","trade":"0","changeratio":"0","netamount":null,"ratioamount":"","r0_net":"--","r0_ratio":"NaN","r3_net":false,"r3_ratio":"Inf"}]`))
	}))
	defer upstream.Close()
	client := NewClient(WithMoneyFlowBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	items, _, err := client.StockFundFlowsBySymbols(context.Background(), []string{"000001.SZ"})
	if err != nil || len(items) != 1 || len(items[0].Meta.AvailableFields) != 1 || items[0].Meta.AvailableFields[0] != "change_percent" {
		t.Fatalf("invalid numbers advertised as data: items=%+v err=%v", items, err)
	}
}

func TestStockFundFlowsBySymbolsEmptySnapshotIsAnError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) }))
	defer upstream.Close()
	client := NewClient(WithMoneyFlowBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	if _, _, err := client.StockFundFlowsBySymbols(context.Background(), []string{"000001.SZ"}); err == nil {
		t.Fatal("empty upstream must not replace a successful cached snapshot")
	}
	for _, symbols := range [][]string{nil, {"bad"}, {"12345.SH"}} {
		if _, _, err := client.StockFundFlowsBySymbols(context.Background(), symbols); err == nil {
			t.Fatalf("invalid symbols accepted: %v", symbols)
		}
	}
}
