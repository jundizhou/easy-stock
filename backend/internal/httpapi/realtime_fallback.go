package httpapi

import (
	"context"
	"errors"

	"easy-stock/backend/internal/foundation"
)

type realtimeFallback struct {
	primary  RealtimeProvider
	fallback RealtimeProvider
}

func (p realtimeFallback) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	quotes, err := p.primary.Realtime(ctx, symbols)
	if err == nil && len(quotes) > 0 {
		return quotes, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	quotes, fallbackErr := p.fallback.Realtime(ctx, symbols)
	if fallbackErr == nil && len(quotes) > 0 {
		return quotes, nil
	}
	return nil, errors.New("实时行情数据源暂不可用，请稍后重试")
}
