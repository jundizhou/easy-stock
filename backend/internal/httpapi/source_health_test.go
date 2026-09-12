package httpapi

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunSourceProbesReportsPerSourceResultInOrder(t *testing.T) {
	probes := []sourceProbe{
		{ID: "ok", Name: "正常源", Category: "quote", Check: func(ctx context.Context) error { return nil }},
		{ID: "fail", Name: "失败源", Category: "news", Check: func(ctx context.Context) error {
			return errors.New("connection reset by peer")
		}},
		{ID: "empty", Name: "空数据源", Category: "theme", Check: func(ctx context.Context) error {
			return sourceProbeError("开盘啦涨停池")
		}},
		{ID: "nocheck", Name: "无探测源", Category: "misc"},
	}

	results := runSourceProbes(context.Background(), probes)
	if len(results) != len(probes) {
		t.Fatalf("results=%d want %d", len(results), len(probes))
	}
	for index, probe := range probes {
		if results[index].ID != probe.ID {
			t.Fatalf("result %d id=%q want %q (order must match probe order)", index, results[index].ID, probe.ID)
		}
	}
	if !results[0].OK {
		t.Fatalf("healthy probe should be OK: %+v", results[0])
	}
	if results[1].OK || results[1].Message == "" {
		t.Fatalf("failing probe must carry a message: %+v", results[1])
	}
	if results[2].OK || !strings.Contains(results[2].Message, "返回空数据") {
		t.Fatalf("empty payload must be reported as failure: %+v", results[2])
	}
	if !results[3].OK {
		t.Fatalf("probe without check should be treated as healthy: %+v", results[3])
	}
}

func TestSourceHealthCacheReusesFreshResults(t *testing.T) {
	var calls int32
	probes := []sourceProbe{{
		ID: "counted", Name: "计数源", Category: "quote",
		Check: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	}}

	cache := newSourceHealthCache(time.Minute)
	first := cache.load(context.Background(), probes)
	second := cache.load(context.Background(), probes)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("unexpected results: %d / %d", len(first), len(second))
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fresh cache should probe once, probed %d times", got)
	}

	stale := newSourceHealthCache(time.Minute)
	stale.load(context.Background(), probes)
	// Windows 时钟粒度可能让相邻两次 time.Now() 相等，显式回拨时间而不是依赖极短 TTL。
	stale.mu.Lock()
	stale.checkedAt = time.Now().Add(-2 * time.Minute)
	stale.mu.Unlock()
	stale.load(context.Background(), probes)
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expired cache should probe again, calls=%d want 3", got)
	}
}

func TestSourceProbeMessageTruncatesLongErrors(t *testing.T) {
	long := strings.Repeat("上游连接失败", 20)
	message := sourceProbeMessage(errors.New(long))
	if runes := []rune(message); len(runes) > 49 {
		t.Fatalf("message should be truncated, got %d runes", len(runes))
	}
	if !strings.HasSuffix(message, "…") {
		t.Fatalf("truncated message should end with an ellipsis: %q", message)
	}
	if sourceProbeMessage(errors.New("")) != "探测失败" {
		t.Fatalf("empty error should fall back to 探测失败")
	}
}
