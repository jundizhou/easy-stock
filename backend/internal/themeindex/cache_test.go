package themeindex

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCanceledRequestDoesNotPoisonCacheOrActiveWaiter(t *testing.T) {
	c := newCache[int](2)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	leader := make(chan error, 1)
	go func() {
		_, err := c.load(ctx, "key", testNow, time.Minute, func(ctx context.Context) (int, error) {
			close(started)
			<-ctx.Done()
			return 0, ctx.Err()
		})
		leader <- err
	}()
	<-started
	waiter := make(chan error, 1)
	go func() {
		value, err := c.load(context.Background(), "key", testNow, time.Minute, func(context.Context) (int, error) { return 42, nil })
		if err == nil && value != 42 {
			err = errors.New("waiter received canceled value")
		}
		waiter <- err
	}()
	cancel()
	if err := <-leader; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader cancellation: %v", err)
	}
	if err := <-waiter; err != nil {
		t.Fatalf("active waiter: %v", err)
	}
	value, err := c.load(context.Background(), "key", testNow, time.Minute, func(context.Context) (int, error) {
		t.Fatal("successful retry was not cached")
		return 0, nil
	})
	if err != nil || value != 42 {
		t.Fatalf("cached retry: %d %v", value, err)
	}
}

func TestRefreshClearsFailedHistoriesWhileReusingValidHistories(t *testing.T) {
	c := newCache[int](2)
	_, _ = c.load(context.Background(), "valid", testNow, time.Minute, func(context.Context) (int, error) { return 7, nil })
	_, _ = c.load(context.Background(), "failed", testNow, time.Minute, func(context.Context) (int, error) { return 0, errors.New("offline") })
	c.invalidate("", true)
	value, err := c.load(context.Background(), "failed", testNow, time.Minute, func(context.Context) (int, error) { return 8, nil })
	if err != nil || value != 8 {
		t.Fatalf("failed history did not retry: %d %v", value, err)
	}
	value, err = c.load(context.Background(), "valid", testNow, time.Minute, func(context.Context) (int, error) {
		t.Fatal("refresh discarded valid shared stock history")
		return 0, nil
	})
	if err != nil || value != 7 {
		t.Fatalf("valid history: %d %v", value, err)
	}
}
