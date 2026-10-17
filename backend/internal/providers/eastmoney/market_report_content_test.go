package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMarketReportContentExtractsParagraphsFromStockPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/report/info/AP202610161830626430.html" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`<html><body><div class="ctx-body"><p class="ctx-img-box"><a href="#"><img src="k.png"></a></p>` +
			`<div id="ctx-content" class="ctx-content"><p> 博瑞医药(688166)</p><p>投资要点&nbsp;摘要</p><p>高毛利原料药放量驱动，2026H1业绩高增。</p></div></div></body></html>`))
	}))
	defer server.Close()
	detail, err := NewClient(WithDataBaseURL(server.URL)).MarketReportContent(context.Background(), "stock", "AP202610161830626430")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "博瑞医药(688166)\n投资要点 摘要\n高毛利原料药放量驱动，2026H1业绩高增。"
	if detail.Content != want {
		t.Fatalf("content=%q want=%q", detail.Content, want)
	}
	if detail.Kind != "stock" || detail.ID != "AP202610161830626430" || !strings.HasSuffix(detail.URL, "/report/info/AP202610161830626430.html") || detail.Meta.Source != "eastmoney:report" {
		t.Fatalf("unexpected detail %+v", detail)
	}
}

func TestMarketReportContentUsesIndustryPageAndRejectsInvalidID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/report/zw_industry.jshtml" || r.URL.Query().Get("infocode") != "AP202610171830635569" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Write([]byte(`<html><body><div class="ctx-body"><div class="ctx-content"><p>军工电子行情回顾</p><p>中航证券 张超</p></div></div></body></html>`))
	}))
	defer server.Close()
	client := NewClient(WithDataBaseURL(server.URL))
	detail, err := client.MarketReportContent(context.Background(), "industry", "AP202610171830635569")
	if err != nil || detail.Content != "军工电子行情回顾\n中航证券 张超" || detail.Kind != "industry" {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	for _, id := range []string{"", "AP2026/../etc", strings.Repeat("a", 60)} {
		if _, err := client.MarketReportContent(context.Background(), "stock", id); err == nil {
			t.Fatalf("accepted invalid report id %q", id)
		}
	}
}

func TestMarketReportContentReportsMissingBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div class="ctx-body"><p>正文加载中</p></div></body></html>`))
	}))
	defer server.Close()
	if _, err := NewClient(WithDataBaseURL(server.URL)).MarketReportContent(context.Background(), "stock", "AP1"); err == nil {
		t.Fatal("expected error when report body is unavailable")
	}
}
