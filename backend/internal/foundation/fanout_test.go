package foundation

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestFanOutAllSucceed(t *testing.T) {
	items := make([]int, 100)
	var processed int64
	errs := FanOut(context.Background(), items, 8, func(ctx context.Context, item int) error {
		atomic.AddInt64(&processed, 1)
		return nil
	})
	if errs != nil {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if processed != 100 {
		t.Fatalf("expected 100 processed, got %d", processed)
	}
}

func TestFanOutCollectsErrors(t *testing.T) {
	items := []int{0, 1, 2, 3, 4}
	errs := FanOut(context.Background(), items, 4, func(ctx context.Context, item int) error {
		if item%2 == 0 {
			return nil
		}
		return context.DeadlineExceeded
	})
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(errs))
	}
	for idx, e := range errs {
		if e == nil || idx%2 != 1 {
			t.Fatalf("unexpected err at %d: %v", idx, e)
		}
	}
}

func TestFanOutDeadlineStopsWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var processed int64
	FanOut(ctx, make([]int, 1000), 8, func(ctx context.Context, item int) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		atomic.AddInt64(&processed, 1)
		return nil
	})
	// 20ms 内（8 worker × 50ms 任务）不可能跑完，确保 deadline 生效。
	if processed >= 100 {
		t.Fatalf("expected far fewer processed under deadline, got %d", processed)
	}
}

func TestFanOutEmpty(t *testing.T) {
	errs := FanOut(context.Background(), []int{}, 8, func(ctx context.Context, item int) error {
		t.Fatalf("handler must not be called for empty input")
		return nil
	})
	if errs != nil {
		t.Fatalf("expected nil errs for empty input, got %v", errs)
	}
}
