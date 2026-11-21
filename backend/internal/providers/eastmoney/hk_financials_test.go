package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const hkFundamentalsPayload = `{"success":true,"result":{"data":[
{"SECUCODE":"00700.HK","SECURITY_NAME_ABBR":"腾讯控股","REPORT_DATE":"2026-09-30 00:00:00","REPORT_TYPE":"2026年三季报","OPERATE_INCOME":608579000000,"OPERATE_INCOME_YOY":8.6,"HOLDER_PROFIT":177727000000,"HOLDER_PROFIT_YOY":12.3,"BASIC_EPS":19.697,"ROE_AVG":15.025,"GROSS_PROFIT_RATIO":56.79,"DEBT_ASSET_RATIO":40.1,"PER_NETCASH_OPERATE":17.235,"CURRENCY":"HKD"},
{"SECUCODE":"00700.HK","SECURITY_NAME_ABBR":"腾讯控股","REPORT_DATE":"2026-06-30 00:00:00","REPORT_TYPE":"2026年中报","OPERATE_INCOME":401243000000,"HOLDER_PROFIT":114115000000,"BASIC_EPS":12.639,"ROE_AVG":9.966,"GROSS_PROFIT_RATIO":57.24,"PER_NETCASH_OPERATE":16.944,"CURRENCY":"HKD"}
]}}`

func TestHongKongFinancialHistoryMapsHKF10Fields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("reportName") != "RPT_HKF10_FN_MAININDICATOR" {
			t.Errorf("unexpected reportName: %s", query.Get("reportName"))
		}
		if !strings.Contains(query.Get("filter"), "00700.HK") {
			t.Errorf("unexpected filter: %s", query.Get("filter"))
		}
		if query.Get("sortColumns") != "REPORT_DATE" || query.Get("sortTypes") != "-1" {
			t.Errorf("not newest-first: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(hkFundamentalsPayload))
	}))
	defer server.Close()

	client := NewClient(WithF10BaseURL(server.URL))
	items, err := client.HongKongFinancialHistory(context.Background(), "00700.HK", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 reports, got %d", len(items))
	}
	latest := items[0]
	if latest.ReportDate != "2026-09-30" || latest.ReportName != "2026年三季报" {
		t.Fatalf("unexpected report identity: %+v", latest)
	}
	if latest.Revenue != 608579000000 || latest.RevenueYearOverYear != 8.6 || latest.NetProfit != 177727000000 || latest.NetProfitYearOverYear != 12.3 {
		t.Fatalf("unexpected revenue/profit: %+v", latest)
	}
	if latest.EPS != 19.697 || latest.ROE != 15.025 || latest.GrossMargin != 56.79 || latest.DebtRatio != 40.1 {
		t.Fatalf("unexpected ratios: %+v", latest)
	}
	if latest.OperatingCashFlowPerShare != 17.235 || latest.Currency != "HKD" {
		t.Fatalf("unexpected cash flow/currency: %+v", latest)
	}
	if latest.DeductedNetProfitAvailable {
		t.Fatal("hk reports must not claim deducted net profit availability")
	}
	if latest.Meta.Source != "eastmoney:hkf10-financials" || len(latest.Meta.AvailableFields) == 0 {
		t.Fatalf("unexpected meta: %+v", latest.Meta)
	}
}

func TestHongKongFinancialHistorySkipsDuplicatePeriods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"result":{"data":[
{"REPORT_DATE":"2026-06-30 00:00:00","OPERATE_INCOME":1},
{"REPORT_DATE":"2026-06-30 00:00:00","OPERATE_INCOME":2}
]}}`))
	}))
	defer server.Close()

	client := NewClient(WithF10BaseURL(server.URL))
	items, err := client.HongKongFinancialHistory(context.Background(), "00700.HK", 8)
	if err != nil || len(items) != 1 || items[0].Revenue != 1 {
		t.Fatalf("dedupe failed: %+v %v", items, err)
	}
	if items[0].ReportName != "2026年中报" {
		t.Fatalf("report name should be derived from date, got %q", items[0].ReportName)
	}
}

func TestHongKongFinancialHistoryRejectsAShareSymbol(t *testing.T) {
	client := NewClient()
	if _, err := client.HongKongFinancialHistory(context.Background(), "600519.SH", 8); err == nil {
		t.Fatal("a-share symbol accepted by hk financial history")
	}
}
