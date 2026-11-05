package tencent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type adjustedKLineTransport func(*http.Request) (*http.Response, error)

func (transport adjustedKLineTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestAdjustedKLineUsesCurrentDefaultEndpoint(t *testing.T) {
	client := NewClient(WithHTTPClient(&http.Client{Transport: adjustedKLineTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "https" || request.URL.Host != "web.ifzq.gtimg.cn" || request.URL.Path != "/appstock/app/newfqkline/get" {
			t.Fatalf("unexpected adjusted history endpoint: %s", request.URL)
		}
		if request.URL.Query().Get("param") != "sh600000,day,,,301,qfq" {
			t.Fatalf("not forward adjusted: %s", request.URL.RawQuery)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"sh600000":{"qfqday":[["2026-09-30","10","11","12","9","100"]]}}}`)), Header: make(http.Header), Request: request}, nil
	})}))
	lines, err := client.AdjustedKLine(context.Background(), "600000.SH", 301)
	if err != nil || len(lines) != 1 || lines[0].Close != 11 || lines[0].Meta.Source != "tencent:qfq-kline" || !strings.Contains(lines[0].Meta.SourceURL, "/newfqkline/get?") {
		t.Fatalf("adjusted: %+v %v", lines, err)
	}
}

func TestAdjustedKLineNeverFallsBackToRawPrices(t *testing.T) {
	for _, adjusted := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("param") != "sh600000,day,,,301,qfq" {
				t.Errorf("not forward adjusted: %s", r.URL.RawQuery)
			}
			field := "day"
			if adjusted {
				field = "qfqday"
			}
			fmt.Fprintf(w, `{"code":0,"data":{"sh600000":{"%s":[["2026-09-30","10","11","12","9","100"]]}}}`, field)
		}))
		client := NewClient(WithKLineBaseURL(server.URL))
		lines, err := client.AdjustedKLine(context.Background(), "600000.SH", 301)
		server.Close()
		if adjusted && (err != nil || len(lines) != 1 || lines[0].Close != 11 || lines[0].Meta.Source != "tencent:qfq-kline") {
			t.Fatalf("adjusted: %+v %v", lines, err)
		}
		if !adjusted && err == nil {
			t.Fatal("accepted unadjusted prices as adjusted")
		}
	}
}
