package tencent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRealtimeParsesDatedQuotesAndSkipsInvalidRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "sh600519,sz000001" {
			t.Errorf("quote symbols = %q", got)
		}
		for _, key := range []string{"sh600519", "sz000001"} {
			fields := make([]string, 35)
			fields[1], fields[2], fields[3], fields[4], fields[5] = "测试", key[2:], "101", "100", "99"
			fields[30], fields[31], fields[32], fields[33], fields[34] = "20261105150200", "1", "1.00", "102", "98"
			if key == "sz000001" {
				fields[30] = "invalid"
			}
			fmt.Fprintf(w, "v_%s=\"%s\";", key, strings.Join(fields, "~"))
		}
	}))
	defer server.Close()
	quotes, err := NewClient(WithQuoteBaseURL(server.URL), WithHTTPClient(server.Client())).Realtime(context.Background(), []string{"600519.SH", "000001.SZ", "600519.SH"})
	if err != nil || len(quotes) != 1 {
		t.Fatalf("quotes=%+v err=%v", quotes, err)
	}
	quote := quotes[0]
	if quote.Symbol != "600519.SH" || quote.ChangePercent != 1 || quote.TradeTime.Format("2006-01-02") != "2026-11-05" || quote.Meta.Source != "tencent:stock-quote" {
		t.Fatalf("quote=%+v", quote)
	}
}
