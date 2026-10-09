package themeindex

import (
	"context"
	"errors"
	"sync"
	"time"
)

type entry[T any] struct {
	value   T
	err     error
	expires time.Time
}
type flight[T any] struct {
	done  chan struct{}
	value T
	err   error
}
type cache[T any] struct {
	mu       sync.Mutex
	items    map[string]entry[T]
	flights  map[string]*flight[T]
	capacity int
}

func newCache[T any](capacity int) *cache[T] {
	return &cache[T]{items: map[string]entry[T]{}, flights: map[string]*flight[T]{}, capacity: capacity}
}

func (c *cache[T]) invalidate(key string, errorsOnly bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !errorsOnly {
		delete(c.items, key)
		return
	}
	for key, value := range c.items {
		if value.err != nil {
			delete(c.items, key)
		}
	}
}
func (c *cache[T]) load(ctx context.Context, key string, now time.Time, ttl time.Duration, loader func(context.Context) (T, error)) (T, error) {
	c.mu.Lock()
	if v, ok := c.items[key]; ok && now.Before(v.expires) {
		c.mu.Unlock()
		return v.value, v.err
	}
	if f, ok := c.flights[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		case <-f.done:
			if ctx.Err() == nil && (errors.Is(f.err, context.Canceled) || errors.Is(f.err, context.DeadlineExceeded)) {
				return c.load(ctx, key, now, ttl, loader)
			}
			return f.value, f.err
		}
	}
	f := &flight[T]{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()
	f.value, f.err = loader(ctx)
	c.mu.Lock()
	delete(c.flights, key)
	if !errors.Is(f.err, context.Canceled) && !errors.Is(f.err, context.DeadlineExceeded) {
		if f.err != nil {
			ttl = 15 * time.Second
		}
		for k, v := range c.items {
			if !now.Before(v.expires) {
				delete(c.items, k)
			}
		}
		if len(c.items) >= c.capacity {
			var oldest string
			var expires time.Time
			for k, v := range c.items {
				if oldest == "" || v.expires.Before(expires) {
					oldest, expires = k, v.expires
				}
			}
			delete(c.items, oldest)
		}
		c.items[key] = entry[T]{value: f.value, err: f.err, expires: now.Add(ttl)}
	}
	close(f.done)
	c.mu.Unlock()
	return f.value, f.err
}
