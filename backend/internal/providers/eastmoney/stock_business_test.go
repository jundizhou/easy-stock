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
