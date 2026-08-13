package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStockBusinessProfileExtractsMainBusiness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("reportName") != "RPT_F10_ORG_BASICINFO" || !strings.Contains(r.URL.Query().Get("filter"), "688297.SH") {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"result":{"data":[{"SECUCODE":"688297.SH","SECURITY_NAME_ABBR":"中无人机","EM2016":"国防与装备-航空航天装备-航天装备","ORG_PROFIE":"公司主要从事于无人机系统的设计研发、生产制造、销售和服务。","BUSINESS_SCOPE":"无人机系统设计生产"}]},"success":true}`))
	}))
	defer server.Close()

	client := NewClient(WithF10BaseURL(server.URL))
	profile, err := client.StockBusinessProfile(context.Background(), "688297.SH")
	if err != nil {
		t.Fatal(err)
	}
	if profile.MainBusiness != "无人机系统" || profile.Industry != "航天装备" || profile.Meta.Source != "eastmoney:f10-business" {
		t.Fatalf("unexpected business profile: %+v", profile)
	}
}

func TestStockFundamentalsReturnsLatestReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("reportName") != "RPT_F10_FINANCE_MAINFINADATA" || r.URL.Query().Get("sortColumns") != "REPORT_DATE" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"result":{"data":[{"SECUCODE":"688297.SH","REPORT_DATE":"2026-03-31 00:00:00","REPORT_DATE_NAME":"2026一季报","TOTALOPERATEREVE":574284022.39,"TOTALOPERATEREVETZ":143.59,"PARENTNETPROFIT":16864497.49,"PARENTNETPROFITTZ":0.11,"EPSJB":0.025,"ROEJQ":0.29,"XSMLL":13.82,"ZCFZL":42.5,"MGJYXJJE":0.32}]},"success":true}`))
	}))
	defer server.Close()

	client := NewClient(WithF10BaseURL(server.URL))
	item, err := client.StockFundamentals(context.Background(), "688297.SH")
	if err != nil {
		t.Fatal(err)
	}
	if item.ReportName != "2026一季报" || item.RevenueYearOverYear != 143.59 || item.GrossMargin != 13.82 || item.Meta.Source != "eastmoney:f10-financials" {
		t.Fatalf("unexpected fundamentals: %+v", item)
	}
}
