package chanscreener

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestDecodePayloadSuccess(t *testing.T) {
	payload := []byte(`{"ok":true,"symbol":"600519.SH","period":"day",
		"range":{"start":"2024-01-02","end":"2024-06-03","bars":100},
		"structure":{"bars":100,"bi_count":12,"bi":[{"direction":"up","start_price":1.0,"end_price":2.0}]},
		"summary":{"score":66.5,"stance":"偏多","tone":"up","reasons":["a"],"conclusion":"c"}}`)
	var result AnalyzeResult
	if err := decodePayload(payload, &result); err != nil {
		t.Fatalf("decodePayload failed: %v", err)
	}
	if !result.OK || result.Symbol != "600519.SH" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Structure.BiCount != 12 || len(result.Structure.Bi) != 1 || result.Structure.Bi[0].Direction != "up" {
		t.Fatalf("structure not decoded: %+v", result.Structure)
	}
	if result.Summary.Score != 66.5 || result.Summary.Stance != "偏多" {
		t.Fatalf("summary not decoded: %+v", result.Summary)
	}
}

func TestDecodePayloadFailureEnvelope(t *testing.T) {
	var result ScreenResult
	err := decodePayload([]byte(`{"ok":false,"error":"获取K线失败: 超时"}`), &result)
	if err == nil || err.Error() != "获取K线失败: 超时" {
		t.Fatalf("expected business error, got %v", err)
	}
}

func TestDecodePayloadRejectsMissingOK(t *testing.T) {
	var result AnalyzeResult
	if err := decodePayload([]byte(`{"symbol":"x"}`), &result); err == nil {
		t.Fatal("expected error for missing ok field")
	}
}

func TestNormalizePeriod(t *testing.T) {
	cases := map[string]string{
		"": "day", "day": "day", "D": "day", "daily": "day",
		"W": "week", "weekly": "week", "monthly": "month",
		"60min": "60min", "5min": "5min",
	}
	for input, want := range cases {
		if got := normalizePeriod(input); got != want {
			t.Errorf("normalizePeriod(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestClampLimit(t *testing.T) {
	if got := clampLimit(0); got != defaultLimit {
		t.Errorf("clampLimit(0) = %d, want %d", got, defaultLimit)
	}
	if got := clampLimit(999999); got != maxLimit {
		t.Errorf("clampLimit overflow = %d, want %d", got, maxLimit)
	}
	if got := clampLimit(300); got != 300 {
		t.Errorf("clampLimit(300) = %d", got)
	}
}

func TestDecodeScreenResult(t *testing.T) {
	payload := []byte(`{"ok":true,"period":"day","scanned":2,"matched":1,"failed":0,
		"errors":[],"results":[
			{"symbol":"600519.SH","name":"贵州茅台","last_close":1700.0,"matched":true,
			 "match_reasons":["二买（3根K线前）"],"score":82.0,"stance":"偏多",
			 "last_bsp":{"is_buy":true,"types":["2"],"labels":["二买"],"type_str":"2","time":"2024-05-20","price":1650.0,"bar_index":96,"is_sure":true},
			 "zs_state":"above","bi_direction":"up","bi_count":18,"zs_count":3,"reasons":[]},
			{"symbol":"000001.SZ","last_close":11.0,"matched":false,"match_reasons":[],"score":45.0,"stance":"中性","zs_state":"inside","bi_direction":"down","bi_count":10,"zs_count":1,"reasons":[]}
		],"elapsed_ms":3210,"engine":"chan.py"}`)
	var result ScreenResult
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if result.Matched != 1 || len(result.Results) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	first := result.Results[0]
	if first.LastBSP == nil || first.LastBSP.TypeStr == "" || len(first.LastBSP.Labels) == 0 {
		t.Fatalf("last_bsp not decoded: %+v", first.LastBSP)
	}
	if first.LastBSP.Types[0] != "2" {
		t.Fatalf("unexpected bsp type: %+v", first.LastBSP.Types)
	}
}

func TestUnavailableReasonWhenMissing(t *testing.T) {
	svc := NewService(Config{
		PythonPath: filepath.Join(t.TempDir(), "missing-python"),
		ScriptPath: filepath.Join(t.TempDir(), "missing-script.py"),
	})
	if svc.Available() {
		t.Fatal("expected unavailable service")
	}
	if reason := svc.UnavailableReason(); reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestServiceUnavailableError(t *testing.T) {
	svc := NewService(Config{
		PythonPath: filepath.Join(t.TempDir(), "missing-python"),
		ScriptPath: filepath.Join(t.TempDir(), "missing-script.py"),
	})
	if _, err := svc.Analyze(context.Background(), AnalyzeRequest{Symbol: "600519.SH"}); err == nil {
		t.Fatal("expected error from unavailable service")
	}
	if _, err := svc.Screen(context.Background(), ScreenRequest{Symbols: []string{"600519.SH"}}); err == nil {
		t.Fatal("expected error from unavailable service")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	svc := NewService(Config{DisableCache: false, Timeout: time.Second})
	svc.store("k", "v")
	if got, ok := svc.lookup("k"); !ok || got != "v" {
		t.Fatalf("cache round trip failed: %v %v", got, ok)
	}
}
