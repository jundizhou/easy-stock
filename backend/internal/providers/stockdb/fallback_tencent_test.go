package stockdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTencentDailySource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		param := r.URL.Query().Get("param")
		if param == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := map[string]any{
			"code": 0,
			"data": map[string]any{
				"sh600519": map[string]any{
					"qfqday": []map[string]any{
						{"date": "2024-01-02", "open": 10, "close": 11, "high": 12, "low": 9, "volume": 100},
						{"date": "2024-01-03", "open": 11, "close": 13, "high": 14, "low": 10, "volume": 120},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	src := NewTencentDailySource(srv.URL)
	bars, err := src.DailyBars(context.Background(), "600519", 10)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(bars) != 2 {
		t.Fatalf("expected 2 bars, got %d", len(bars))
	}
	if bars[0].Date != 20240102 || bars[1].Date != 20240103 {
		t.Fatalf("unexpected dates: %+v", bars)
	}
	if bars[1].Close != 13 || bars[0].Open != 10 {
		t.Fatalf("unexpected OHLC: %+v", bars[1])
	}
}

func TestTencentDailySourceNoData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{}})
	}))
	defer srv.Close()
	src := NewTencentDailySource(srv.URL)
	if _, err := src.DailyBars(context.Background(), "600519", 10); err == nil {
		t.Fatalf("expected error for missing data")
	}
}

func TestTencentDailySourceBadStatus(t *testing.T) {
	// 指向一个必然拒绝连接的地址，制造连接错误（镜像本地源宕机时的故障转移触发）。
	src := NewTencentDailySource("http://127.0.0.1:1")
	if _, err := src.DailyBars(context.Background(), "600519", 10); err == nil {
		t.Fatalf("expected connection error")
	}
}

func TestMarketPrefix(t *testing.T) {
	cases := map[string]string{
		"600519": "sh",
		"000001": "sz",
		"300750": "sz",
		"830799": "bj",
		"688001": "sh",
	}
	for code, want := range cases {
		if got := marketPrefix(code); got != want {
			t.Fatalf("marketPrefix(%s)=%s want %s", code, got, want)
		}
	}
}
