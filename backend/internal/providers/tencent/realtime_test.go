package tencent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestRealtimeParsesHongKongRowsWithSlashTimestamps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "hk00700" {
			t.Errorf("quote symbols = %q", got)
		}
		fields := make([]string, 76)
		fields[1], fields[2] = "腾讯控股", "00700"
		fields[3], fields[4], fields[5] = "344.000", "348.400", "341.000"
		fields[30] = "2026/11/20 16:08:37"
		fields[31], fields[32], fields[33], fields[34] = "-4.400", "-1.26", "349.600", "339.600"
		fields[60], fields[63], fields[75] = "100", "GP", "HKD"
		fields[36], fields[37] = "41094469.0", "14089992865.680"
		// 上游是 GB18030，客户端按该编码解码；测试也写 GB18030 字节。
		body := fmt.Sprintf("v_hk00700=\"%s\";", strings.Join(fields, "~"))
		encoded, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(body))
		_, _ = w.Write(encoded)
	}))
	defer server.Close()

	quotes, err := NewClient(WithQuoteBaseURL(server.URL), WithHTTPClient(server.Client())).Realtime(context.Background(), []string{"00700.HK"})
	if err != nil || len(quotes) != 1 {
		t.Fatalf("quotes=%+v err=%v", quotes, err)
	}
	quote := quotes[0]
	if quote.Symbol != "00700.HK" || quote.Name != "腾讯控股" || quote.Price != 344 || quote.Currency != "HKD" || quote.BoardLot != 100 || quote.InstrumentType != "stock" {
		t.Fatalf("quote=%+v", quote)
	}
	if stamp := quote.TradeTime.Format("2006-01-02 15:04:05"); stamp != "2026-11-20 16:08:37" {
		t.Fatalf("hk trade time = %q", stamp)
	}
	if quote.Volume != 41094469 || quote.Amount != 14089992865.68 {
		t.Fatalf("hk session totals discarded: %+v", quote)
	}
}

func TestRealtimeSkipsHongKongRowWithAStockTimestamp(t *testing.T) {
	// 港股代码配 A 股时间格式属于上游异常，宁可丢弃也不写入错误时点。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields := make([]string, 70)
		fields[1], fields[2], fields[3] = "腾讯控股", "00700", "344.000"
		fields[30] = "20261120160837"
		fmt.Fprintf(w, "v_hk00700=\"%s\";", strings.Join(fields, "~"))
	}))
	defer server.Close()
	if _, err := NewClient(WithQuoteBaseURL(server.URL), WithHTTPClient(server.Client())).Realtime(context.Background(), []string{"00700.HK"}); err == nil {
		t.Fatal("expected no quotes error")
	}
}

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

func TestHongKongFundMetadata(t *testing.T) {
	fields := make([]string, 76)
	fields[2], fields[3], fields[30], fields[60], fields[63], fields[75] = "09040", "27", "2026/11/20 16:00:00", "100", "GP-FUND", "USD"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "v_hk09040=%q;", strings.Join(fields, "~"))
	}))
	defer server.Close()
	quotes, err := NewClient(WithQuoteBaseURL(server.URL)).Realtime(context.Background(), []string{"09040.HK"})
	if err != nil || len(quotes) != 1 || quotes[0].InstrumentType != "etf" || quotes[0].Currency != "USD" {
		t.Fatalf("quotes=%+v err=%v", quotes, err)
	}
}
