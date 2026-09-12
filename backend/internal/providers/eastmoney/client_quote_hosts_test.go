package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQuoteRequestVariantsRotatesOnlyQuoteHosts(t *testing.T) {
	client := NewClient()

	quoteVariants := client.quoteRequestVariants("https://push2.eastmoney.com/api/qt/clist/get?fs=m:90+t:2")
	if len(quoteVariants) < 2 {
		t.Fatalf("expected fallback hosts for push2 url, got %v", quoteVariants)
	}
	if !strings.Contains(quoteVariants[1], "push2delay.eastmoney.com") {
		t.Fatalf("expected push2delay to be tried first, got %v", quoteVariants)
	}
	seen := map[string]bool{}
	for _, variant := range quoteVariants {
		host := strings.SplitN(strings.TrimPrefix(variant, "https://"), "/", 2)[0]
		if seen[host] {
			t.Fatalf("duplicate host %s in %v", host, quoteVariants)
		}
		seen[host] = true
	}

	otherVariants := client.quoteRequestVariants("https://datacenter-web.eastmoney.com/api/data/v1/get?reportName=RPT_X")
	if len(otherVariants) != 1 {
		t.Fatalf("non-quote host must not be rewritten, got %v", otherVariants)
	}
	if otherVariants[0] != "https://datacenter-web.eastmoney.com/api/data/v1/get?reportName=RPT_X" {
		t.Fatalf("non-quote url changed: %v", otherVariants)
	}
}

func TestIndustryMomentumFallsBackToReachableQuoteHost(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // simulate an unreachable push2 host

	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rc":0,"data":{"diff":[{"f12":"BK1452","f14":"卫浴电器","f3":6.45,"f8":1.1,"f62":294045993,"f104":2,"f105":1,"f109":3.2,"f24":9.9,"f128":"样本股份","f136":10.0}]}}`))
	}))
	defer live.Close()

	client := NewClient(WithQuoteBaseURL(deadURL), WithQuoteFallbackBaseURLs(live.URL))
	items, _, err := client.IndustryMomentum(context.Background(), 5)
	if err != nil {
		t.Fatalf("IndustryMomentum should survive a dead primary host: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items=%d want 1", len(items))
	}
	if items[0].RisingCount != 2 || items[0].FallingCount != 1 {
		t.Fatalf("breadth not parsed from fallback host: rising=%d falling=%d", items[0].RisingCount, items[0].FallingCount)
	}
	if items[0].Name != "卫浴电器" || items[0].ChangePercent != 6.45 {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}
