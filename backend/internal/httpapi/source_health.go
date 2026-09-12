package httpapi

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// sourceProbeTimeout bounds a single upstream probe. Probes run in parallel, so
// this is also the worst-case latency of a cold status check.
const sourceProbeTimeout = 4 * time.Second

// sourceHealthCacheTTL keeps the radar page from probing every upstream on each
// load; the status popover is informational, not a live console.
const sourceHealthCacheTTL = 60 * time.Second

// sourceProbe performs one cheap liveness check for a single data source.
type sourceProbe struct {
	ID       string
	Name     string
	Category string
	Check    func(ctx context.Context) error
}

// sourceHealthCache stores the most recent probe results.
type sourceHealthCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	checkedAt time.Time
	results   []foundation.SourceHealth
}

func newSourceHealthCache(ttl time.Duration) *sourceHealthCache {
	if ttl <= 0 {
		ttl = sourceHealthCacheTTL
	}
	return &sourceHealthCache{ttl: ttl}
}

// load returns cached results when fresh and otherwise probes every source.
func (c *sourceHealthCache) load(ctx context.Context, probes []sourceProbe) []foundation.SourceHealth {
	c.mu.Lock()
	if len(c.results) > 0 && time.Since(c.checkedAt) < c.ttl {
		cached := c.results
		c.mu.Unlock()
		return cached
	}
	c.mu.Unlock()

	results := runSourceProbes(ctx, probes)
	if len(results) == 0 {
		return results
	}
	c.mu.Lock()
	c.results = results
	c.checkedAt = time.Now()
	c.mu.Unlock()
	return results
}

// runSourceProbes probes every source concurrently so one slow upstream cannot
// delay the others.
func runSourceProbes(ctx context.Context, probes []sourceProbe) []foundation.SourceHealth {
	if len(probes) == 0 {
		return nil
	}
	results := make([]foundation.SourceHealth, len(probes))
	var waitGroup sync.WaitGroup
	for index, probe := range probes {
		waitGroup.Add(1)
		go func(index int, probe sourceProbe) {
			defer waitGroup.Done()
			health := foundation.SourceHealth{
				ID:        probe.ID,
				Name:      probe.Name,
				Category:  probe.Category,
				OK:        true,
				CheckedAt: time.Now(),
			}
			if probe.Check != nil {
				probeCtx, cancel := context.WithTimeout(ctx, sourceProbeTimeout)
				defer cancel()
				if err := probe.Check(probeCtx); err != nil {
					health.OK = false
					health.Message = sourceProbeMessage(err)
				}
			}
			results[index] = health
		}(index, probe)
	}
	waitGroup.Wait()
	return results
}

// sourceProbeMessage keeps upstream errors short enough for the status popover.
func sourceProbeMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "探测失败"
	}
	if runes := []rune(message); len(runes) > 48 {
		message = string(runes[:48]) + "…"
	}
	return message
}

// sourceProbeError reports an empty upstream payload as a failure, so a source
// that answers with nothing is not counted as healthy.
func sourceProbeError(what string) error {
	return errSourceProbeEmpty{what: what}
}

// errSourceProbeUnavailable marks a source that this build cannot reach at all
// (for example an unconfigured client).
var errSourceProbeUnavailable = errors.New("未配置")

type errSourceProbeEmpty struct {
	what string
}

func (e errSourceProbeEmpty) Error() string {
	return e.what + "返回空数据"
}
