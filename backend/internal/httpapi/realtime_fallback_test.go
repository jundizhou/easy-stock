package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

type stubRealtime struct {
	quotes []foundation.Quote
	err    error
	calls  int
}

func (p *stubRealtime) Realtime(context.Context, []string) ([]foundation.Quote, error) {
	p.calls++
	return p.quotes, p.err
}

func TestRealtimeFallbackUsesSecondaryOnlyAfterPrimaryFailure(t *testing.T) {
	primary := &stubRealtime{err: errors.New("Get https://hq.sinajs.cn/very-long-url: timeout")}
	secondary := &stubRealtime{quotes: []foundation.Quote{{Symbol: "600519.SH"}}}
	provider := realtimeFallback{primary: primary, fallback: secondary}
	quotes, err := provider.Realtime(context.Background(), []string{"600519.SH"})
	if err != nil || len(quotes) != 1 || primary.calls != 1 || secondary.calls != 1 {
		t.Fatalf("fallback quotes=%+v err=%v calls=%d/%d", quotes, err, primary.calls, secondary.calls)
	}
	primary.quotes, primary.err = quotes, nil
	_, _ = provider.Realtime(context.Background(), []string{"600519.SH"})
	if secondary.calls != 1 {
		t.Fatal("secondary called after successful primary")
	}
	primary.err, secondary.err = errors.New("timeout"), errors.New("timeout")
	secondary.quotes = nil
	_, err = provider.Realtime(context.Background(), []string{"600519.SH"})
	if err == nil || strings.Contains(err.Error(), "https://") {
		t.Fatalf("raw upstream error leaked: %v", err)
	}
}
