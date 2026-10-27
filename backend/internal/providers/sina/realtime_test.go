package sina

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestRealtimeBookPreservesSharesAndMissingLiquidity(t *testing.T) {
	fields := strings.Split("股名,10,10,11,11,10,0,0,100000,500000000,12345,10.99,0,0,0,0,0,0,0,0,250,11,0,0,0,0,0,0,0,0,2026-10-26,10:00:00", ",")
	book := parseRealtimeBook(fields)
	if book == nil || book.BidQuantity != 12345 || book.AskQuantity != 250 || book.AskPrice != 11 {
		t.Fatalf("book units/indices lost: %+v", book)
	}
	symbol, _ := foundation.NormalizeSymbol("600002.SH")
	quotes, err := parseRealtime(`var hq_str_sh600002="`+strings.Join(fields, ",")+`";`, []foundation.Symbol{symbol}, foundation.SourceMeta{Source: "sina"})
	if err != nil || len(quotes) != 1 || quotes[0].Book == nil || quotes[0].Book.AskQuantity != 250 {
		t.Fatalf("book not wired into actual parser: %+v %v", quotes, err)
	}
	fields[20], fields[21] = "0", "0"
	if book = parseRealtimeBook(fields); book == nil || book.AskQuantity != 0 {
		t.Fatalf("valid sealed book should stay observed: %+v", book)
	}
	for _, invalid := range []string{"", "--", "NaN", "+Inf", "-1", "1.5"} {
		fields[20] = invalid
		if got := parseRealtimeBook(fields); got != nil {
			t.Errorf("invalid quantity %s must be absent: %+v", invalid, got)
		}
	}
	if parseRealtimeBook(fields[:10]) != nil {
		t.Fatal("missing fields must not become zero liquidity")
	}
}

func TestClientRealtimeParsesSinaResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("list"); got != "sz000001" {
			t.Fatalf("list = %q, want sz000001", got)
		}
		w.Write([]byte(`var hq_str_sz000001="平安银行,10.00,9.80,10.50,10.80,9.90,10.49,10.50,123456,123456789.00,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,2026-06-12,15:00:00,00";`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	got, err := client.Realtime(context.Background(), []string{"000001.SZ"})
	if err != nil {
		t.Fatalf("Realtime returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(Realtime) = %d, want 1", len(got))
	}
	if got[0].Symbol != "000001.SZ" || got[0].Name != "平安银行" || got[0].Price != 10.50 {
		t.Fatalf("unexpected quote: %+v", got[0])
	}
	if got[0].ChangePercent <= 0 {
		t.Fatalf("ChangePercent = %f, want positive", got[0].ChangePercent)
	}
	if got[0].Volume != 123456 || got[0].Amount != 123456789 {
		t.Fatalf("session liquidity discarded: %+v", got[0])
	}
	if want := time.Date(2026, 6, 12, 7, 0, 0, 0, time.UTC); !got[0].TradeTime.Equal(want) {
		t.Fatalf("market timestamp = %s, want %s", got[0].TradeTime, want)
	}
	if got[0].Meta.Source != "sina" || got[0].Meta.SourceURL == "" {
		t.Fatalf("unexpected meta: %+v", got[0].Meta)
	}
}

func TestClientRealtimeHonorsGB18030EvenWhenBytesAreValidUTF8(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=GB18030")
		body := append([]byte(`var hq_str_sz300377="`), []byte{0xd3, 0xae, 0xca, 0xb1, 0xca, 0xa4}...)
		body = append(body, []byte(`,14.34,14.51,15.60,15.76,14.34,15.59,15.60,98575264,1509805385.46,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,2026-08-27,15:35:45,00";`)...)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	got, err := client.Realtime(context.Background(), []string{"300377.SZ"})
	if err != nil {
		t.Fatalf("Realtime returned error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "赢时胜" {
		t.Fatalf("GB18030 name decoded incorrectly: %+v", got)
	}
}

func TestParseKLineSupportsIntradayTime(t *testing.T) {
	body := `callback([{"day":"2026-08-12 14:35","open":"11.000","high":"11.250","low":"10.880","close":"11.240","volume":"203235546"}]);`
	got, err := parseKLineJSONP(body, "000001.SZ", foundation.SourceMeta{})
	if err != nil {
		t.Fatalf("parseKLineJSONP returned error: %v", err)
	}
	if got[0].Time.Hour() != 14 || got[0].Time.Minute() != 35 {
		t.Fatalf("Time = %v, want 14:35", got[0].Time)
	}
}

func TestClientKLineParsesSinaJSONPResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("symbol"); got != "sz000001" {
			t.Fatalf("symbol = %q, want sz000001", got)
		}
		w.Write([]byte(`callback([{"day":"2026-06-12","open":"11.000","high":"11.250","low":"10.880","close":"11.240","volume":"203235546"}]);`))
	}))
	defer server.Close()

	client := NewClient(WithKLineBaseURL(server.URL))
	got, err := client.KLine(context.Background(), "000001.SZ", "day", 1)
	if err != nil {
		t.Fatalf("KLine returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(KLine) = %d, want 1", len(got))
	}
	if got[0].Symbol != "000001.SZ" || got[0].Close != 11.240 || got[0].Volume != 203235546 {
		t.Fatalf("unexpected kline: %+v", got[0])
	}
	if got[0].Amount != 0 {
		t.Fatalf("amount must stay unknown when daily source omits it: %+v", got[0])
	}
	if got[0].Meta.Source != "sina" || got[0].Meta.SourceURL == "" {
		t.Fatalf("unexpected meta: %+v", got[0].Meta)
	}
}

func TestClientKLineMonthlyPeriods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("scale") != "7200" || query.Get("symbol") != "sz000001" || query.Get("datalen") != "2" {
			t.Errorf("unexpected monthly request: %s", r.URL)
			http.Error(w, "invalid monthly request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`callback([{"day":"2026-08-31","open":"10.00","high":"12.00","low":"9.50","close":"11.00","volume":"20000"},{"day":"2026-09-30","open":"11.00","high":"13.00","low":"10.50","close":"12.00","volume":"30000"}]);`))
	}))
	defer server.Close()
	client := NewClient(WithKLineBaseURL(server.URL))
	for _, period := range []string{"month", "monthly", "MONTH", "103", "7200", " month "} {
		t.Run(period, func(t *testing.T) {
			got, err := client.KLine(context.Background(), "000001.SZ", period, 2)
			if err != nil {
				t.Fatalf("monthly KLine returned error: %v", err)
			}
			if len(got) != 2 {
				t.Fatalf("monthly KLine returned %d bars, want 2", len(got))
			}
			if got[0].Time.Format("2006-01-02") != "2026-08-31" || got[1].Time.Format("2006-01-02") != "2026-09-30" || got[1].Close != 12 || got[1].Meta.Source != "sina" {
				t.Fatalf("unexpected monthly bars: %+v", got)
			}
		})
	}
}
