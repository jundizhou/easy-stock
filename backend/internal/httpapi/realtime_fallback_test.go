package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/stockanalysis"
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

func TestMixedRealtimeUsesHKMetadataAndMainlandPrimary(t *testing.T) {
	mainland := &stubRealtime{quotes: []foundation.Quote{{Symbol: "600519.SH", Price: 20}}}
	hk := &stubRealtime{quotes: []foundation.Quote{{Symbol: "02800.HK", InstrumentType: "etf", Currency: "HKD"}}}
	provider := realtimeFallback{primary: mainland, fallback: hk}
	quotes, err := provider.Realtime(context.Background(), []string{"02800.HK", "600519.SH", "hk02800"})
	if err != nil || len(quotes) != 2 || quotes[0].InstrumentType != "etf" || quotes[1].Symbol != "600519.SH" || mainland.calls != 1 || hk.calls != 1 {
		t.Fatalf("quotes=%+v calls=%d/%d err=%v", quotes, mainland.calls, hk.calls, err)
	}
}

func TestHongKongResearchRequiresConfirmedEquity(t *testing.T) {
	for _, tc := range []struct {
		symbol, kind string
		allowed      bool
	}{
		{"00700.HK", "stock", true}, {"02800.HK", "etf", false}, {"00700.HK", "", false},
	} {
		server := Server{realtimeProvider: &stubRealtime{quotes: []foundation.Quote{{Symbol: tc.symbol, InstrumentType: tc.kind}}}}
		_, err := server.resolveResearchInstrument(context.Background(), stockanalysis.ResearchRequest{Symbol: tc.symbol}, true)
		if (err == nil) != tc.allowed {
			t.Errorf("%s/%s err=%v", tc.symbol, tc.kind, err)
		}
	}
}
