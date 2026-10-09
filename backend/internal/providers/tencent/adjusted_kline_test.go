package tencent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
