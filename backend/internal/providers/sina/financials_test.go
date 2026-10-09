package sina

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestFinancialEvidencePreservesReportUnitsMissingFieldsAndMarketCode(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("paperCode") != "sz301536" || r.URL.Query().Get("num") != "8" {
			t.Errorf("wrong symbol or unbounded request: %s", r.URL)
		}
		w.Write([]byte(`{"result":{"status":{"code":0},"data":{"report_list":{"20260630":{"rType":"合并期末","rCurrency":"CNY","publish_date":"20260827","data":[{"item_field":"BIZTOTINCO","item_value":"260000000","item_tongbi":0.276},{"item_field":"PARENETP","item_value":"0","item_tongbi":""},{"item_field":"NPCUT","item_value":null},{"item_field":"MANANETR","item_value":"123"}]},"20260331":{"rType":"母公司期末","rCurrency":"CNY","data":[{"item_field":"BIZTOTINCO","item_value":"999"}]},"20251231":{"rType":"合并期末","rCurrency":"USD","data":[{"item_field":"BIZTOTINCO","item_value":"999"}]}}}}}`))
	}))
	defer s.Close()
	items, err := NewClient(WithFinancialBaseURL(s.URL)).StockFinancialEvidence(context.Background(), "301536.SZ", 100)
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %+v", err, items)
	}
	x := items[0]
	if x.ReportDate != "2026-06-30" || x.PublishedAt.IsZero() || x.Fields["revenue"] != 260000000 || x.Fields["revenue_yoy"] != 27.6 || x.Fields["operating_cash_flow"] != 123 {
		t.Fatalf("units/dates lost: %+v", x)
	}
	if _, ok := x.Fields["deducted_net_profit"]; ok {
		t.Fatal("missing deducted profit fabricated")
	}
	if _, ok := x.Fields["net_profit"]; !ok {
		t.Fatal("explicit zero discarded")
	}
}

func TestLiveFinancialEvidencePublicFeed(t *testing.T) {
	if os.Getenv("EASY_STOCK_LIVE_FINANCIALS") != "1" {
		t.Skip("public financial smoke check not requested")
	}
	for _, symbol := range []string{"301536.SZ", "600519.SH"} {
		items, err := NewClient().StockFinancialEvidence(context.Background(), symbol, 8)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 0 || items[0].Symbol != symbol || items[0].Fields["revenue"] <= 0 {
			t.Fatalf("invalid live financial result: %+v", items)
		}
		t.Logf("%s: %d reports, latest=%s, fields=%d, published=%s", symbol, len(items), items[0].ReportDate, len(items[0].Fields), items[0].PublishedAt.Format("2006-01-02"))
	}
}

func TestFinancialEvidenceRejectsProviderFailureAndMalformedJSON(t *testing.T) {
	for _, body := range []string{`{"result":{"status":{"code":11,"msg":"Input error"},"data":[]}}`, `{"result":{"status":{"code":0},"data":{"report_list":{}}}}`, `not json`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		_, err := NewClient(WithFinancialBaseURL(s.URL)).StockFinancialEvidence(context.Background(), "600519.SH", 8)
		s.Close()
		if err == nil {
			t.Fatal("bad source accepted")
		}
	}
}
