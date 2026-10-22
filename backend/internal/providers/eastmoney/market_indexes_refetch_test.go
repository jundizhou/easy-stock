package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 东财 ulist 批量行情偶发只返回部分条目（日经225、韩国KOSPI、台湾加权最常见），
// 导致巡查快照里这几个指数时有时无。MarketIndexes 必须对缺失的 secid 补抓一次。
func TestMarketIndexesRefetchesMissingSecids(t *testing.T) {
	const firstResponse = `{"rc":0,"data":{"diff":[{"f12":"000001","f13":1,"f14":"上证指数","f2":3456.7,"f3":1.25,"f4":42.6,"f124":1786410000},{"f12":"HSI","f13":100,"f14":"恒生指数","f2":25000,"f3":-0.5,"f4":-120,"f124":1786410000}]}}`
	const retryResponse = `{"rc":0,"data":{"diff":[{"f12":"N225","f13":100,"f14":"日经225","f2":41000,"f3":0.8,"f4":320,"f124":1786410000},{"f12":"KS11","f13":100,"f14":"韩国KOSPI","f2":2600,"f3":-0.3,"f4":-8,"f124":1786410000},{"f12":"TWII","f13":100,"f14":"台湾加权","f2":22500,"f3":0.5,"f4":110,"f124":1786410000}]}}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/qt/ulist.np/get" {
			http.NotFound(w, r)
			return
		}
		// 补抓请求只带缺失的 secids；上游对该请求正常返回缺失的三个指数。
		if strings.Contains(r.URL.Query().Get("secids"), "100.N225") && !strings.Contains(r.URL.Query().Get("secids"), "1.000001") {
			_, _ = w.Write([]byte(retryResponse))
			return
		}
		_, _ = w.Write([]byte(firstResponse))
	}))
	defer upstream.Close()

	client := NewClient(WithQuoteBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	indexes, _, err := client.MarketIndexes(context.Background(), "global")
	if err != nil {
		t.Fatalf("MarketIndexes err=%v", err)
	}
	got := map[string]bool{}
	for _, item := range indexes {
		got[item.ID] = true
	}
	// 首次只回了 sse/hsi，补抓应把 fake 里提供的 nikkei/kospi/taiwan 补齐；
	// 其余目录指数 fake 未提供，仍缺席但不能报错、不能重复。
	if len(indexes) != 5 {
		t.Fatalf("expected 5 unique indexes, got %d: %+v", len(indexes), indexes)
	}
	for _, want := range []string{"sse", "hsi", "nikkei", "kospi", "taiwan"} {
		if !got[want] {
			t.Fatalf("index %q missing after refetch: %+v", want, indexes)
		}
	}
}
