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
	var aShares, hk, ordered []string
	seen := make(map[string]bool, len(symbols))
	for _, symbol := range symbols {
		n, err := foundation.NormalizeSymbol(symbol)
		if err != nil {
			return nil, err
		}
		if seen[n.Canonical] {
			continue
		}
		seen[n.Canonical] = true
		ordered = append(ordered, n.Canonical)
		if n.Market == "HK" {
			hk = append(hk, n.Canonical)
		} else {
			aShares = append(aShares, n.Canonical)
		}
	}
	// Tencent supplies HK trading currency and instrument class. A shares keep the Sina book.
	bySymbol := map[string]foundation.Quote{}
	load := func(targets []string, first, second RealtimeProvider) {
		for _, source := range []RealtimeProvider{first, second} {
			if len(targets) == 0 || ctx.Err() != nil {
				return
			}
			quotes, err := source.Realtime(ctx, targets)
			if err != nil {
				continue
			}
			for _, q := range quotes {
				bySymbol[q.Symbol] = q
			}
			missing := make([]string, 0, len(targets))
			for _, symbol := range targets {
				if _, ok := bySymbol[symbol]; !ok {
					missing = append(missing, symbol)
				}
			}
			targets = missing
		}
	}
	load(aShares, p.primary, p.fallback)
	load(hk, p.fallback, p.primary)
	quotes := make([]foundation.Quote, 0, len(bySymbol))
	for _, symbol := range ordered {
		if q, ok := bySymbol[symbol]; ok {
			quotes = append(quotes, q)
		}
	}
	if len(quotes) > 0 {
		return quotes, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, errors.New("实时行情数据源暂不可用，请稍后重试")
}
