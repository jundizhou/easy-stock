package foundation

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestApplyBrowserHeadersFillsDefaults(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://hq.sinajs.cn/list=sh600519", nil)
	ApplyBrowserHeaders(req)
	if req.Header.Get("User-Agent") == "" {
		t.Errorf("user-agent should be set")
	}
	if req.Header.Get("Accept-Language") == "" {
		t.Errorf("accept-language should be set")
	}
}

func TestApplyBrowserHeadersKeepsExplicit(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Set("User-Agent", "custom-ua")
	ApplyBrowserHeaders(req)
	if req.Header.Get("User-Agent") != "custom-ua" {
		t.Errorf("explicit UA should not be overridden, got %q", req.Header.Get("User-Agent"))
	}
}

func TestThrottleEnforcesInterval(t *testing.T) {
	host := "throttle-test.example.com"
	interval := 80 * time.Millisecond
	start := time.Now()
	for i := 0; i < 3; i++ {
		if !Throttle(context.Background(), host, interval) {
			t.Fatalf("throttle cancelled unexpectedly")
		}
	}
	elapsed := time.Since(start)
	// 3 次请求，间隔 2 次 => 至少 2*interval
	if elapsed < 2*interval {
		t.Errorf("throttle too fast: %v (want >= %v)", elapsed, 2*interval)
	}
}

func TestThrottleRespectsContextCancel(t *testing.T) {
	host := "throttle-cancel.example.com"
	interval := 500 * time.Millisecond
	// 先占位一次
	_ = Throttle(context.Background(), host, interval)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Throttle(ctx, host, interval) {
		t.Errorf("throttle should return false when ctx already cancelled")
	}
}

func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"https://hq.sinajs.cn/list=sh600519": "hq.sinajs.cn",
		"http://127.0.0.1:7899/?cmd=get":     "127.0.0.1",
		"https://qt.gtimg.cn/q=sh600519":     "qt.gtimg.cn",
	}
	for in, want := range cases {
		if got := HostOf(in); got != want {
			t.Errorf("HostOf(%q) = %q, want %q", in, got, want)
		}
	}
}
