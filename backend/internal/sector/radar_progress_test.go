package sector

import (
	"context"
	"errors"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

type delayedRadarSource struct {
	fakeRadarSource
	release chan struct{}
}

func (s delayedRadarSource) Snapshot(ctx context.Context) (duanxianxia.Snapshot, duanxianxia.FetchMeta, error) {
	select {
	case <-s.release:
		return s.fakeRadarSource.Snapshot(ctx)
	case <-ctx.Done():
		return duanxianxia.Snapshot{}, duanxianxia.FetchMeta{}, ctx.Err()
	}
}

func TestProgressiveOverviewPublishesIndustryBeforeSlowMembership(t *testing.T) {
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	source := delayedRadarSource{fakeRadarSource: fakeRadarSource{snapshot: duanxianxia.Snapshot{ID: "snapshot", TradeDate: "2026-09-17", FetchedAt: now, Themes: []duanxianxia.Theme{{Code: "1", Name: "通信", Rank: 1, Leaders: []duanxianxia.Leader{{Symbol: "000001.SZ", Name: "测试", Rank: 1}}}}}}, release: make(chan struct{})}
	provider := NewRadarProvider(source, fakeRadarFallback{}, nil, RadarProviderConfig{Now: func() time.Time { return now }, IndustryMomentum: fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{{Code: "i1", Name: "通信", Score: 80, LeaderSymbol: "000001.SZ", LeaderName: "测试"}}}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan foundation.ThemeProgress, 8)
	go provider.ProgressiveOverviews(ctx, func(value foundation.ThemeProgress) { updates <- value })
	select {
	case first := <-updates:
		if len(first.Data) == 0 || first.Steps["industry"] != "ready" || first.Steps["kaipanla"] != "loading" {
			t.Fatalf("did not publish fast source: %+v", first)
		}
		if len(first.Data[0].LeaderStocks) != 1 || !first.Data[0].Provisional {
			t.Fatalf("missing preview membership: %+v", first.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("fast source blocked behind slow membership")
	}
	close(source.release)
	for {
		select {
		case update := <-updates:
			if !update.Refreshing {
				if update.Steps["kaipanla"] != "ready" {
					t.Fatal(update)
				}
				return
			}
		case <-time.After(time.Second):
			t.Fatal("refresh failed to reach terminal state")
		}
	}
}

type forbiddenRadarFallback struct{}

func (forbiddenRadarFallback) Build(context.Context, string) (foundation.SectorMap, error) {
	panic("leader preview must not load full constituents")
}

type forbiddenRadarQuotes struct{}

func (forbiddenRadarQuotes) Realtime(context.Context, []string) ([]foundation.Quote, error) {
	panic("leader preview must not fetch quotes")
}

func TestLeaderPreviewDoesNotWaitForRemoteData(t *testing.T) {
	source := fakeRadarSource{snapshot: duanxianxia.Snapshot{ID: "s", TradeDate: "2026-09-17", Themes: []duanxianxia.Theme{{Code: "1", Name: "通信", Leaders: []duanxianxia.Leader{{Symbol: "000001.SZ", Name: "测试", Rank: 1}}}}}}
	provider := NewRadarProvider(source, forbiddenRadarFallback{}, forbiddenRadarQuotes{}, RadarProviderConfig{})
	result, err := provider.BuildLeaders(context.Background(), "kpl:1", "s")
	if err != nil || len(result.Groups[0].Nodes[0].Stocks) != 1 {
		t.Fatalf("preview: %+v %v", result, err)
	}
	_, err = provider.BuildLeaders(context.Background(), "kpl:1", "expired")
	if !errors.Is(err, ErrSnapshotExpired) {
		t.Fatalf("expected typed expired snapshot: %v", err)
	}
}
