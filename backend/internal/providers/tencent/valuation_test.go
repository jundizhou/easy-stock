package tencent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValuationBatchPreservesTrailingBasisAndMissingFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "sh600519,sz000001,sh600000" {
			t.Errorf("not one deduplicated batch: %s", r.URL)
		}
		for _, symbol := range []string{"sh600519", "sz000001", "sh600000"} {
			f := make([]string, 54)
			f[2] = symbol[2:]
			f[30] = "20260930161458"
			f[39] = "19.32"
			f[46] = "6.26"
			f[52] = "17.67"
			f[53] = "19.11"
			if symbol == "sz000001" {
				f[39] = "--"
				f[46] = "NaN"
			}
			if symbol == "sh600000" {
				f[2] = "600999"
			}
			fmt.Fprintf(w, "v_%s=\"%s\";\n", symbol, strings.Join(f, "~"))
		}
	}))
	defer server.Close()
	c := NewClient(WithQuoteBaseURL(server.URL), WithHTTPClient(server.Client()))
	values, err := c.StockValuations(context.Background(), []string{"600519.SH", "000001.SZ", "600000.SH", "600519.SH"})
	if err != nil || len(values) != 2 {
		t.Fatal(values, err)
	}
	v := values[0]
	if v.PETTM == nil || *v.PETTM != 19.32 || v.PB == nil || *v.PB != 6.26 {
		t.Fatal("TTM confused with dynamic/static PE", v)
	}
	_, offset := v.TradeTime.Zone()
	if offset != 8*3600 || v.Meta.Source != "tencent:stock-valuation" || v.Meta.FetchedAt.IsZero() {
		t.Fatal(v)
	}
	if values[1].PETTM != nil || values[1].PB != nil {
		t.Fatal("missing became zero/cheap", values[1])
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.StockValuations(ctx, []string{"600519.SH"}); err == nil {
		t.Fatal("cancellation lost")
	}
	if !v.Current("600519.SH", time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("holiday valuation incorrectly expired")
	}
}
