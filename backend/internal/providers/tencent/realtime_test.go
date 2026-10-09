package tencent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestRealtimeGBK(t *testing.T) {
	// 构造 1:1 对齐 quant 的 `~` 字段（name=1 price=3 昨收=4 今开=5 涨跌=31 涨跌幅=32 最高=33 最低=34 时间=30）
	fields := make([]string, 47)
	fields[1] = "平安银行"
	fields[3] = "11.50"  // 现价
	fields[4] = "11.45"  // 昨收
	fields[5] = "11.48"  // 今开
	fields[30] = "15:00:00"
	fields[31] = "0.05"  // 涨跌
	fields[32] = "0.44"  // 涨跌幅%
	fields[33] = "11.60" // 最高
	fields[34] = "11.40" // 最低
	payload := "v_sz000001=\"" + strings.Join(fields, "~") + "\";"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=GBK")
		enc, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(payload))
		if err != nil {
			t.Fatalf("encode gbk: %v", err)
		}
		_, _ = w.Write(enc)
	}))
	defer srv.Close()

	c := NewClient(WithQuoteBaseURL(srv.URL))
	quotes, err := c.Realtime(context.Background(), []string{"000001.SZ"})
	if err != nil {
		t.Fatalf("Realtime error: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("expected 1 quote, got %d", len(quotes))
	}
	q := quotes[0]
	if q.Symbol != "000001.SZ" {
		t.Errorf("symbol = %q", q.Symbol)
	}
	if q.Name != "平安银行" {
		t.Errorf("name = %q", q.Name)
	}
	if q.Price != 11.50 {
		t.Errorf("price = %v", q.Price)
	}
	if q.PreviousClose != 11.45 {
		t.Errorf("prevClose = %v", q.PreviousClose)
	}
	if q.Open != 11.48 {
		t.Errorf("open = %v", q.Open)
	}
	if q.High != 11.60 {
		t.Errorf("high = %v", q.High)
	}
	if q.Low != 11.40 {
		t.Errorf("low = %v", q.Low)
	}
	if q.Change != 0.05 {
		t.Errorf("change = %v", q.Change)
	}
	if q.ChangePercent != 0.44 {
		t.Errorf("pct = %v", q.ChangePercent)
	}
	if q.Meta.Source != "tencent" {
		t.Errorf("meta source = %q", q.Meta.Source)
	}
	if q.TradeTime.IsZero() {
		t.Errorf("trade time should be parsed")
	}
}
