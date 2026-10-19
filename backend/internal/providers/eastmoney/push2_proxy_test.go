package eastmoney

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestPush2ProxyAcceptsOnlyQuoteEndpoints(t *testing.T) {
	proxy := NewPush2Proxy("http://127.0.0.1:1", "token")
	accepted := []string{
		"https://push2.eastmoney.com/api/qt/clist/get?pn=1",
		"https://push2delay.eastmoney.com/api/qt/ulist.np/get?secids=1.000001",
		"https://push2his.eastmoney.com/api/qt/stock/kline/get?secid=1.000001",
		"https://90.push2.eastmoney.com/api/qt/stock/fflow/kline/get?secid=90.BK0429",
		"https://7.push2.eastmoney.com/api/qt/stock/trends2/get?secid=90.BK0429",
	}
	for _, rawURL := range accepted {
		if !proxy.accepts(rawURL) {
			t.Fatalf("accepts(%q) = false, want true", rawURL)
		}
	}
	rejected := []string{
		"https://push2ex.eastmoney.com/getTopicZTPool?dpt=wz.ztzt",
		"https://data.eastmoney.com/dataapi/bkzj/getbkzj?key=f62",
		"https://push2.eastmoney.com/dataapi/bkzj/getbkzj",
		"https://push2.eastmoney.com",
		"https://push2.eastmoney.com.evil.example/api/x",
		"https://evilpush2.eastmoney.com/api/x",
		"https://90.push2.eastmoney.com.cn/api/x",
		"http://127.0.0.1:20003/api/v1/market/sector-flows",
	}
	for _, rawURL := range rejected {
		if proxy.accepts(rawURL) {
			t.Fatalf("accepts(%q) = true, want false", rawURL)
		}
	}
	// 未注入桥接（网页版）时任何地址都不走转发。
	var missing *Push2Proxy
	if missing.accepts("https://push2.eastmoney.com/api/qt/clist/get") {
		t.Fatal("nil proxy must not accept requests")
	}
}

func TestPush2ProxyFetchesThroughBridge(t *testing.T) {
	var gotPath string
	var gotTarget string
	var gotToken string
	upstreamBody := `{"rc":0,"data":{"diff":[{"f12":"BK0429"}]}}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		gotTarget = request.URL.Query().Get("url")
		gotToken = request.Header.Get("X-A-Stock-Browser-Token")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"status":       200,
				"content_type": "application/json",
				"body":         upstreamBody,
			},
		})
	}))
	defer server.Close()

	proxy := NewPush2Proxy(server.URL+"/", "secret")
	target := "https://90.push2.eastmoney.com/api/qt/stock/fflow/kline/get?secid=90.BK0429&klt=1"
	body, err := proxy.get(context.Background(), target)
	if err != nil {
		t.Fatalf("get() error = %v", err)
	}
	if string(body) != upstreamBody {
		t.Fatalf("body = %s, want %s", body, upstreamBody)
	}
	if gotPath != "/v1/eastmoney/push2" {
		t.Fatalf("path = %s, want /v1/eastmoney/push2", gotPath)
	}
	if gotTarget != target {
		t.Fatalf("forwarded url = %s, want %s", gotTarget, target)
	}
	if gotToken != "secret" {
		t.Fatalf("token = %q, want secret", gotToken)
	}
}

func TestPush2ProxySurfacesBridgeFailures(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": false, "error": "net::ERR_CONNECTION_CLOSED"})
	}))
	defer failing.Close()
	if _, err := NewPush2Proxy(failing.URL, "secret").get(context.Background(), "https://push2.eastmoney.com/api/qt/clist/get"); err == nil {
		t.Fatal("expected bridge failure to be reported")
	} else if !strings.Contains(err.Error(), "ERR_CONNECTION_CLOSED") {
		t.Fatalf("error = %v, want bridge message", err)
	}

	upstreamFailed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"ok":   true,
			"data": map[string]any{"status": 502, "body": ""},
		})
	}))
	defer upstreamFailed.Close()
	if _, err := NewPush2Proxy(upstreamFailed.URL, "secret").get(context.Background(), "https://push2.eastmoney.com/api/qt/clist/get"); err == nil {
		t.Fatal("expected upstream status failure to be reported")
	} else if !strings.Contains(err.Error(), "上游返回 502") {
		t.Fatalf("error = %v, want upstream status", err)
	}
}

func TestClientPrefersPush2ProxyOverDirectRequest(t *testing.T) {
	var forwarded int
	bridge := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		forwarded++
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"status": 200,
				"body":   `{"rc":0,"data":{"diff":[{"f12":"BK0429"}]}}`,
			},
		})
	}))
	defer bridge.Close()

	direct := 0
	client := NewClient(
		WithPush2Proxy(NewPush2Proxy(bridge.URL, "secret")),
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			direct++
			return nil, errors.New("EOF")
		})}),
	)
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			Diff []struct {
				Code string `json:"f12"`
			} `json:"diff"`
		} `json:"data"`
	}
	if err := client.getJSONWithHeaders(context.Background(), "https://push2.eastmoney.com/api/qt/clist/get?pn=1", nil, &payload); err != nil {
		t.Fatalf("getJSONWithHeaders() error = %v", err)
	}
	// push2 直连注定失败，桌面端有桥接时不应该再发直连请求（失败请求同样会被上游计数）。
	if direct != 0 {
		t.Fatalf("direct requests = %d, want 0", direct)
	}
	if forwarded != 1 || payload.Data.Diff[0].Code != "BK0429" {
		t.Fatalf("forwarded = %d, payload = %+v", forwarded, payload)
	}
}

func TestClientFallsBackToDirectRequestWhenBridgeFails(t *testing.T) {
	var forwarded []string
	bridge := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		forwarded = append(forwarded, request.URL.Query().Get("url"))
		writer.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": false, "error": "net::ERR_EMPTY_RESPONSE"})
	}))
	defer bridge.Close()

	client := NewClient(
		WithPush2Proxy(NewPush2Proxy(bridge.URL, "secret")),
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"rc":0,"data":{"diff":[{"f12":"BK0429","f14":"半导体"}]}}`)),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		})}),
	)
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			Diff []struct {
				Code string `json:"f12"`
				Name string `json:"f14"`
			} `json:"diff"`
		} `json:"data"`
	}
	if err := client.getJSONWithHeaders(context.Background(), "https://push2.eastmoney.com/api/qt/clist/get?pn=1", nil, &payload); err != nil {
		t.Fatalf("getJSONWithHeaders() error = %v", err)
	}
	if len(payload.Data.Diff) != 1 || payload.Data.Diff[0].Code != "BK0429" || payload.Data.Diff[0].Name != "半导体" {
		t.Fatalf("payload = %+v, want direct fallback diff", payload)
	}
	if len(forwarded) != 1 || forwarded[0] != "https://push2.eastmoney.com/api/qt/clist/get?pn=1" {
		t.Fatalf("forwarded = %v", forwarded)
	}
}

func TestClientReportsBothErrorsWhenBridgeAndDirectFail(t *testing.T) {
	bridge := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": false, "error": "net::ERR_EMPTY_RESPONSE"})
	}))
	defer bridge.Close()

	client := NewClient(
		WithPush2Proxy(NewPush2Proxy(bridge.URL, "secret")),
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("EOF")
		})}),
	)
	var payload map[string]any
	err := client.getJSONWithHeaders(context.Background(), "https://push2.eastmoney.com/api/qt/clist/get?pn=1", nil, &payload)
	if err == nil {
		t.Fatal("expected both transports to fail")
	}
	// 两端的原因都要带上，排查时才知道是桥接断了还是上游拒绝了。
	if !strings.Contains(err.Error(), "EOF") || !strings.Contains(err.Error(), "ERR_EMPTY_RESPONSE") {
		t.Fatalf("error = %v, want both causes", err)
	}
}

func TestClientKeepsDirectPathWithoutProxy(t *testing.T) {
	requests := 0
	client := NewClient(
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return nil, errors.New("EOF")
		})}),
	)
	var payload map[string]any
	err := client.getJSONWithHeaders(context.Background(), "https://push2.eastmoney.com/api/qt/clist/get?pn=1", nil, &payload)
	if err == nil || !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("error = %v, want direct EOF", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestClientDoesNotProxyNonPush2Hosts(t *testing.T) {
	var forwarded int
	bridge := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		forwarded++
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "data": map[string]any{"status": 200, "body": "{}"}})
	}))
	defer bridge.Close()

	client := NewClient(
		WithPush2Proxy(NewPush2Proxy(bridge.URL, "secret")),
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("EOF")
		})}),
	)
	var payload map[string]any
	if err := client.getJSONWithHeaders(context.Background(), "https://data.eastmoney.com/dataapi/bkzj/getbkzj?key=f62", nil, &payload); err == nil {
		t.Fatal("expected direct failure for non-push2 host")
	}
	if forwarded != 0 {
		t.Fatalf("forwarded = %d, want 0", forwarded)
	}
}
