package eastmoney

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStockNewsSearchParsesDatedExcerptsAndEncodesQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any
		if r.URL.Path != "/search/jsonp" || json.Unmarshal([]byte(r.URL.Query().Get("param")), &params) != nil || params["keyword"] != "晶晨股份 端侧AI" {
			t.Errorf("invalid query %s", r.URL)
		}
		if params["param"].(map[string]any)["cmsArticleWebOld"].(map[string]any)["pageSize"] != float64(30) {
			t.Error("query limit exceeded")
		}
		w.Write([]byte(`easyStockNews({"code":0,"result":{"cmsArticleWebOld":[{"code":"a1","title":"<em>晶晨股份</em>新品 &amp; 进展","content":"端侧NPU产品研发","date":"2026-09-30 14:28:00","mediaName":"测试媒体","url":"https://example.com/a1"},{"code":"a2","title":"晶晨股份新品 &amp; 进展","date":"2026-09-30"},{"title":"晶晨股份消息","date":"unknown"}]}});`))
	}))
	defer server.Close()
	items, err := NewClient(WithNewsSearchBaseURL(server.URL)).SearchStockNews(context.Background(), "688099.SH", "晶晨股份 端侧AI", 100)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if items[0].Title != "晶晨股份新品 & 进展" || items[0].PublishedAt.Format(time.RFC3339) != "2026-09-30T14:28:00+08:00" || !strings.Contains(items[0].Content, "非全文") || !items[1].PublishedAt.IsZero() {
		t.Fatalf("lost provenance: %+v", items)
	}
}

func TestStockNewsSearchRejectsProviderErrorsAndMalformedData(t *testing.T) {
	for _, body := range []string{`easyStockNews({"code":500});`, `<html>gateway error</html>`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		_, err := NewClient(WithNewsSearchBaseURL(server.URL)).SearchStockNews(context.Background(), "688099", "", 5)
		server.Close()
		if err == nil {
			t.Fatalf("accepted bad provider body: %s", body)
		}
	}
}
