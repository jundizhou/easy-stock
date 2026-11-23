package httpapi

import (
	"context"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/themeindex"
)

type adjustedHistoryFallback interface {
	AdjustedKLine(context.Context, string, int) ([]foundation.KLine, error)
}

// Only providers serving forward-adjusted bars may back theme constituent returns.
type adjustedDailyBars struct{ provider KLineProvider }

func (p adjustedDailyBars) AdjustedKLine(ctx context.Context, symbol string, limit int) ([]foundation.KLine, error) {
	return p.provider.KLine(ctx, symbol, "day", limit)
}

type themeIndexHistory struct {
	primary    adjustedHistoryFallback
	fallback   adjustedHistoryFallback
	mu         sync.Mutex
	failures   int
	retryAfter time.Time
}

func (h *themeIndexHistory) load(ctx context.Context, symbol string, limit int) ([]foundation.KLine, error) {
	h.mu.Lock()
	tryPrimary := !time.Now().Before(h.retryAfter)
	h.mu.Unlock()
	if tryPrimary {
		primaryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		lines, err := h.primary.AdjustedKLine(primaryCtx, symbol, limit)
		cancel()
		lines = themeindex.CleanBars(lines, time.Now())
		h.mu.Lock()
		if err == nil && len(lines) >= 2 {
			h.failures = 0
			h.retryAfter = time.Time{}
			h.mu.Unlock()
			return lines, nil
		}
		if ctx.Err() == nil {
			h.failures++
			if h.failures >= 3 {
				h.retryAfter = time.Now().Add(30 * time.Second)
			}
		}
		h.mu.Unlock()
	}
	fallbackCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return h.fallback.AdjustedKLine(fallbackCtx, symbol, limit)
}
