package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMarketOverviewProvidersParsePrimarySources(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/qt/ulist.np/get":
			_, _ = w.Write([]byte(`{"rc":0,"data":{"diff":[{"f12":"000001","f13":1,"f14":"上证指数","f2":3456.7,"f3":1.25,"f4":42.6,"f124":1786410000},{"f12":"HSI","f13":100,"f14":"恒生指数","f2":25000,"f3":-0.5,"f4":-120,"f124":1786410000}]}}`))
		case "/api/qt/stock/kline/get":
			_, _ = w.Write([]byte(`{"rc":0,"data":{"klines":["2026-08-10,3400,3420,3430,3390,100,200,1.1,0.58,10,0.8","2026-08-11,3420,3450,3460,3410,120,240,1.4,0.88,12,0.9"]}}`))
		case "/api/qt/clist/get":
			_, _ = w.Write([]byte(`{"rc":0,"data":{"diff":[{"f12":"BK001","f14":"半导体","f2":1234.5,"f3":2.5,"f8":3.2,"f24":8.1,"f62":320000000,"f66":120000000,"f69":3.1,"f72":200000000,"f75":5.2,"f78":-50000000,"f81":-1.2,"f84":-270000000,"f87":-7.1,"f104":51,"f105":10,"f109":5.6,"f128":"样本股份","f136":9.9,"f184":8.3}]}}`))
		case "/api/data/v1/get":
			_, _ = w.Write([]byte(`{"success":true,"message":"ok","result":{"data":[{"SECUCODE":"600001.SH","SECURITY_NAME_ABBR":"样本股份","CLOSE_PRICE":12.3,"CHANGE_RATE":9.9,"TURNOVERRATE":16.2,"EXPLANATION":"日涨幅偏离值达7%","EXPLAIN":"买一为2家机构","BILLBOARD_BUY_AMT":50000000,"BILLBOARD_SELL_AMT":20000000,"BILLBOARD_NET_AMT":30000000,"BUY_SEAT":5,"SELL_SEAT":5}]}}`))
		case "/securities/api/data/v1/get":
			if r.URL.Query().Get("filter") == "" {
				t.Fatal("billboard seat request should include filters")
			}
			if strings.Contains(r.URL.Query().Get("filter"), `TRADE_DIRECTION="0"`) {
				_, _ = w.Write([]byte(`{"success":true,"message":"ok","result":{"data":[{"TRADE_DATE":"2026-08-11 00:00:00","EXPLANATION":"日涨幅偏离值达7%","OPERATEDEPT_NAME":"机构专用","BUY_AMT_REAL":12000000,"BUY_RATIO":4.5,"SELL_AMT_REAL":2000000,"SELL_RATIO":0.8,"TRADE_DIRECTION":"0","RANK":1},{"OPERATEDEPT_NAME":"样本买方营业部","BUY_AMT_REAL":8000000,"SELL_AMT_REAL":1000000,"TRADE_DIRECTION":"0","RANK":2}]}}`))
			} else {
				_, _ = w.Write([]byte(`{"success":true,"message":"ok","result":{"data":[{"TRADE_DATE":"2026-08-11 00:00:00","EXPLANATION":"日涨幅偏离值达7%","OPERATEDEPT_NAME":"样本卖方营业部","BUY_AMT_REAL":500000,"SELL_AMT_REAL":9000000,"SELL_RATIO":3.4,"TRADE_DIRECTION":"1","RANK":1}]}}`))
			}
		case "/api/security/ann":
			_, _ = w.Write([]byte(`{"success":1,"data":{"list":[{"art_code":"AN001","title":"重大合同公告","notice_date":"2026-08-11 09:30:00","codes":[{"stock_code":"600001","short_name":"样本股份"}],"columns":[{"column_name":"重大事项"}]}]}}`))
		case "/report/list":
			_, _ = w.Write([]byte(`{"data":[{"title":"景气度持续改善","stockName":"样本股份","stockCode":"600001","orgSName":"样本证券","publishDate":"2026-08-11 08:00:00","infoCode":"RP001","industryCode":"I01","industryName":"电子","emRatingName":"买入","lastEmRatingName":"增持","ratingChange":"调高","researcher":"研究员甲","indvAimPriceT":20,"indvAimPriceL":18,"predictThisYearEps":1.2,"predictThisYearPe":15}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	client := NewClient(
		WithBaseURL(upstream.URL),
		WithQuoteBaseURL(upstream.URL),
		WithDataBaseURL(upstream.URL),
		WithDatacenterBaseURL(upstream.URL),
		WithF10BaseURL(upstream.URL+"/securities"),
		WithAnnouncementBaseURL(upstream.URL),
		WithReportBaseURL(upstream.URL),
		WithHTTPClient(upstream.Client()),
	)
	ctx := context.Background()

	indexes, meta, err := client.MarketIndexes(ctx, "global")
	if err != nil || len(indexes) != 2 || indexes[0].ID != "sse" || indexes[1].ID != "hsi" || meta.Source != "eastmoney:index" {
		t.Fatalf("indexes=%+v meta=%+v err=%v", indexes, meta, err)
	}
	series, err := client.MarketIndexSeries(ctx, "sse", "day", 2)
	if err != nil || len(series.Lines) != 2 || series.Index.Price != 3450 || series.Lines[1].ChangePercent != 0.88 {
		t.Fatalf("series=%+v err=%v", series, err)
	}
	industries, _, err := client.IndustryMomentum(ctx, 10)
	if err != nil || len(industries) != 1 || industries[0].Name != "半导体" || industries[0].Score <= 50 {
		t.Fatalf("industries=%+v err=%v", industries, err)
	}
	flows, _, err := client.MarketFundFlows(ctx, "stock", "ratio", 10)
	if err != nil || len(flows) != 1 || flows[0].MainNetInflowRatio != 8.3 || flows[0].MainNetInflow != 320000000 {
		t.Fatalf("flows=%+v err=%v", flows, err)
	}
	billboard, _, err := client.MarketBillboard(ctx, "2026-08-11", 10)
	if err != nil || len(billboard) != 1 || billboard[0].InstitutionBuyers != 2 || billboard[0].NetAmount != 30000000 {
		t.Fatalf("billboard=%+v err=%v", billboard, err)
	}
	billboardDetail, detailMeta, err := client.MarketBillboardDetail(ctx, "600001.SH", "2026-08-11", "日涨幅偏离值达7%")
	if err != nil || detailMeta.Source != "eastmoney:billboard-seats" || len(billboardDetail.BuySeats) != 2 || len(billboardDetail.SellSeats) != 1 || !billboardDetail.BuySeats[0].Institution || billboardDetail.BuySeats[0].NetAmount != 10000000 || billboardDetail.SellSeats[0].Rank != 1 {
		t.Fatalf("billboard detail=%+v meta=%+v err=%v", billboardDetail, detailMeta, err)
	}
	announcements, _, err := client.MarketAnnouncements(ctx, "合同", "600001.SH", "重大", 10)
	if err != nil || len(announcements) != 1 || announcements[0].ID != "AN001" || !strings.Contains(announcements[0].URL, "600001/AN001") {
		t.Fatalf("announcements=%+v err=%v", announcements, err)
	}
	reports, _, err := client.MarketReports(ctx, "stock", "景气", "600001.SH", "电子", 10)
	if err != nil || len(reports) != 1 || reports[0].Rating != "买入" || reports[0].TargetHigh != 20 {
		t.Fatalf("reports=%+v err=%v", reports, err)
	}
	industryReports, _, err := client.MarketReports(ctx, "industry", "电子", "", "电子", 10)
	if err != nil || len(industryReports) != 1 || !strings.Contains(industryReports[0].URL, "zw_industry") {
		t.Fatalf("industry reports=%+v err=%v", industryReports, err)
	}
}

func TestMarketFundFlowsFallsBackToBoardRanking(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/qt/clist/get" {
			_, _ = w.Write([]byte(`{"rc":0,"data":{"diff":[]}}`))
			return
		}
		if r.URL.Path == "/dataapi/bkzj/getbkzj" {
			_, _ = w.Write([]byte(`{"rc":0,"data":{"diff":[{"f12":"BK099","f14":"回退行业","f62":88000000}]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	client := NewClient(WithQuoteBaseURL(upstream.URL), WithDataBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))

	items, meta, err := client.MarketFundFlows(context.Background(), "industry", "net", 10)
	if err != nil || len(items) != 1 || items[0].Name != "回退行业" || !strings.Contains(meta.FallbackReason, "详细分单") {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	momentum, _, err := client.IndustryMomentum(context.Background(), 1)
	if err != nil || len(momentum) != 1 {
		t.Fatalf("momentum fallback should honor limit: items=%+v err=%v", momentum, err)
	}
}

func TestMarketBillboardFallsBackWhenLatestDateHasNoPublishedData(t *testing.T) {
	var requestedDates []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		filter := r.URL.Query().Get("filter")
		requestedDates = append(requestedDates, filter)
		if len(requestedDates) == 1 {
			_, _ = w.Write([]byte(`{"version":null,"result":null,"success":false,"message":"返回数据为空","code":9201}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"ok","result":{"data":[{"SECUCODE":"600001.SH","SECURITY_NAME_ABBR":"上一交易日样本","EXPLANATION":"日涨幅偏离值达7%","BILLBOARD_NET_AMT":30000000}]}}`))
	}))
	defer upstream.Close()

	client := NewClient(WithDatacenterBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	items, meta, err := client.MarketBillboard(context.Background(), "", 10)
	if err != nil || len(items) != 1 || items[0].Name != "上一交易日样本" || len(requestedDates) < 2 {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	if strings.TrimSpace(meta.TradeDate) == "" || !strings.Contains(requestedDates[1], meta.TradeDate) {
		t.Fatalf("requested dates=%v", requestedDates)
	}
}

func TestMarketBillboardExplicitEmptyDateReturnsEmptyList(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":null,"result":null,"success":false,"message":"返回数据为空","code":9201}`))
	}))
	defer upstream.Close()

	client := NewClient(WithDatacenterBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	items, meta, err := client.MarketBillboard(context.Background(), "2026-08-12", 10)
	if err != nil || len(items) != 0 || meta.TradeDate != "2026-08-12" {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
}
