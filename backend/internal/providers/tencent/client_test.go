package tencent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientParsesIndexSnapshotsAndSeries(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("param") != "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"sh000001":{"day":[["2026-08-10","3943.82","3966.59","3967.59","3938.63","542118110"],["2026-08-11","3950.71","3934.09","3966.39","3930.64","529490944"]]}}}`))
			return
		}
		_, _ = w.Write([]byte("v_s_sh000001=\"1~SSE~000001~3934.09~-32.50~-0.82~529490944~106673709~~689731.22~ZS~\";\n" +
			"v_r_hkHSI=\"100~HSI~HSI~25652.820~25937.490~25998.590~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~2026/08/11 18:31:13~-284.670~-1.10~\";"))
	}))
	defer upstream.Close()
	client := NewClient(WithQuoteBaseURL(upstream.URL), WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))

	indexes, meta, err := client.MarketIndexes(context.Background(), "global")
	if err != nil || len(indexes) != 2 || indexes[0].ID != "sse" || indexes[0].Price != 3934.09 || indexes[1].ID != "hsi" || indexes[1].ChangePercent != -1.10 || meta.Source != "tencent:index" {
		t.Fatalf("indexes=%+v meta=%+v err=%v", indexes, meta, err)
	}
	series, err := client.MarketIndexSeries(context.Background(), "sse", "day", 2)
	if err != nil || len(series.Lines) != 2 || series.Lines[1].Close != 3934.09 || series.Lines[1].ChangePercent >= 0 || series.Meta.Source != "tencent:index-kline" {
		t.Fatalf("series=%+v err=%v", series, err)
	}
}

func TestClientParsesIndustryMomentum(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":[{
			"bd_name":"光伏设备","bd_code":"pt01801735","bd_zxj":"5606.59","bd_zdf":"1.76","bd_zdf5":"5.48","bd_zdf20":"2.35",
			"nzg_code":"sz300051","nzg_name":"琏升科技","nzg_zxj":"8.93","nzg_zdf":"7.59"
		}]}`))
	}))
	defer upstream.Close()
	client := NewClient(WithIndustryBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))

	items, meta, err := client.IndustryMomentum(context.Background(), 20)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	item := items[0]
	if item.Name != "光伏设备" || item.ChangePercent != 1.76 || item.FiveDayChangePercent != 5.48 || item.TwentyDayChange != 2.35 || item.LeaderSymbol != "300051.SZ" || item.LeaderName != "琏升科技" || item.Score <= 50 || meta.Source != "tencent:industry-rank" {
		t.Fatalf("item=%+v meta=%+v", item, meta)
	}
}

func TestClientParsesIndustryStocks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("board_code") != "pt01801039" || r.URL.Query().Get("count") != "200" || r.URL.Query().Get("offset") != "0" {
			http.Error(w, "unexpected query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"total":2,"rank_list":[
			{"code":"sh688300","name":"联瑞新材","zxj":"186.24","zd":"31.04","zdf":"20.00","turnover":"376979","volume":"218402.46","zsz":"449.71","ltsz":"449.71"},
			{"code":"sz301071","name":"力量钻石","zxj":"61.77","zd":"4.77","zdf":"8.37","turnover":"97585","volume":"162237.00","zsz":"157.17","ltsz":"115.54"}
		]}}`))
	}))
	defer upstream.Close()
	client := NewClient(WithIndustryStocksBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))

	stocks, meta, err := client.IndustryStocks(context.Background(), "pt01801039", 500)
	if err != nil || len(stocks) != 2 {
		t.Fatalf("stocks=%+v meta=%+v err=%v", stocks, meta, err)
	}
	if stocks[0].Symbol != "688300.SH" || stocks[0].Name != "联瑞新材" || stocks[0].ChangePercent != 20 || stocks[0].Amount != 2_184_024_600 || stocks[1].Symbol != "301071.SZ" {
		t.Fatalf("unexpected industry stocks: %+v", stocks)
	}
	if meta.Source != "tencent:industry-constituents" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}
