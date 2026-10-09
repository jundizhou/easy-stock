package sector

import (
	"context"
	"errors"
	"strings"
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
		if len(first.Data[0].LeaderStocks) != 1 || first.Data[0].Provisional || first.Data[0].DailyStrengthScore <= 0 || first.Stage != "enriched" {
			t.Fatalf("ready industry strength was hidden behind slow membership: %+v", first)
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

func finalRadarProgress(t *testing.T, provider *RadarProvider) foundation.ThemeProgress {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var final foundation.ThemeProgress
	provider.ProgressiveOverviews(ctx, func(value foundation.ThemeProgress) { final = value })
	if final.Refreshing || len(final.Steps) == 0 {
		t.Fatalf("refresh did not finish: %+v", final)
	}
	return final
}

func TestProgressiveOverviewPreservesIndustryStrengthWhenKaipanlaFails(t *testing.T) {
	now := time.Date(2026, 10, 6, 13, 0, 0, 0, time.FixedZone("CST", 8*3600))
	for _, tc := range []struct {
		name   string
		source RadarSnapshotSource
	}{
		{name: "unavailable"},
		{name: "expired", source: fakeRadarSource{snapshot: duanxianxia.Snapshot{
			ID: "old", TradeDate: "2026-09-24", Themes: []duanxianxia.Theme{{Code: "801001", Name: "芯片", Rank: 1}},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := NewRadarProvider(tc.source, nil, nil, RadarProviderConfig{
				Now: func() time.Time { return now },
				IndustryMomentum: fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{
					{Code: "bio", Name: "生物制品", Score: 88},
				}},
			})
			final := finalRadarProgress(t, provider)
			if final.Steps["industry"] != "ready" || final.Steps["kaipanla"] != "error" || final.Steps["strength"] != "error" {
				t.Fatalf("unexpected source states: %+v", final)
			}
			if len(final.Data) != 1 || final.Data[0].Provisional || final.Data[0].DailyStrengthScore <= 0 || final.Data[0].FiveDayStrengthScore <= 0 || final.Stage != "enriched" {
				t.Fatalf("valid industry scores were hidden: %+v", final)
			}
			if !final.Meta.Stale || !strings.Contains(final.Meta.FallbackReason, final.Errors["kaipanla"]) {
				t.Fatalf("source failure was omitted from metadata: %+v", final)
			}
			if final.Data[0].TradeDate != "2026-09-30" {
				t.Fatalf("holiday industry date should be the last session: %+v", final.Data[0])
			}
		})
	}
}

func TestProgressiveOverviewUsesIndustryScoreWhenMatchedKaipanlaStrengthFails(t *testing.T) {
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	provider := NewRadarProvider(fakeRadarSource{snapshot: duanxianxia.Snapshot{
		ID: "s", TradeDate: "2026-09-17", Themes: []duanxianxia.Theme{
			{Code: "801001", Name: "芯片", Rank: 1},
			{Code: "803023", Name: "AI应用", Rank: 2},
		},
	}}, &fakeRadarStrengthFallback{err: errors.New("constituents unavailable")}, nil, RadarProviderConfig{
		Now: func() time.Time { return now },
		IndustryMomentum: fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{
			{Code: "semi", Name: "半导体", Score: 82},
		}},
	})
	final := finalRadarProgress(t, provider)
	chip, found := findRadarOverview(final.Data, "芯片")
	if !found || chip.Provisional || chip.Source != radarFusionSource || chip.DailyStrengthScore != chip.IndustryDailyScore-radarSinglePenalty || chip.FiveDayStrengthScore != chip.IndustryFiveDayScore-radarSinglePenalty {
		t.Fatalf("unfinished Kaipanla strength replaced the industry score: %+v", final)
	}
	ai, found := findRadarOverview(final.Data, "AI应用")
	if !found || !ai.Provisional {
		t.Fatalf("unscored standalone theme must stay provisional: %+v", final.Data)
	}
	if final.Steps["kaipanla"] != "ready" || final.Steps["strength"] != "error" || !final.Meta.Stale || !strings.Contains(final.Meta.FallbackReason, "题材强度暂不可用") {
		t.Fatalf("strength failure was not reported: %+v", final)
	}
}

func TestProgressiveOverviewReadinessIsPerThemeWhenIndustryFails(t *testing.T) {
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	provider := NewRadarProvider(fakeRadarSource{snapshot: duanxianxia.Snapshot{
		ID: "s", TradeDate: "2026-09-17", Themes: []duanxianxia.Theme{
			{Code: "801001", Name: "芯片", Rank: 1},
			{Code: "803023", Name: "AI应用", Rank: 2},
		},
	}}, nil, nil, RadarProviderConfig{
		Now:              func() time.Time { return now },
		IndustryMomentum: fakeIndustryMomentumSource{err: errors.New("industry unavailable")},
	})
	// Zero is a valid computed score; map membership, not its numeric value,
	// distinguishes it from a theme absent from a partial strength cache.
	provider.strengthAttemptAt = now
	provider.strengthCache = map[string]themeStrengthScore{"801001": {daily: 0, fiveDay: 0}}
	final := finalRadarProgress(t, provider)
	chip, found := findRadarOverview(final.Data, "芯片")
	if !found || chip.Provisional || final.Stage != "enriched" {
		t.Fatalf("ready Kaipanla theme was hidden by industry failure: %+v", final)
	}
	ai, found := findRadarOverview(final.Data, "AI应用")
	if !found || !ai.Provisional {
		t.Fatalf("missing individual strength was marked ready: %+v", final.Data)
	}
	if final.Steps["strength"] != "ready" || final.Steps["industry"] != "error" || !final.Meta.Stale || !strings.Contains(final.Meta.FallbackReason, "industry unavailable") {
		t.Fatalf("industry failure was not reported: %+v", final)
	}
}

func TestRadarHolidaySnapshotMatchesProgressiveAndNormalDelivery(t *testing.T) {
	now := time.Date(2026, 10, 6, 13, 0, 0, 0, time.FixedZone("CST", 8*3600))
	provider := NewRadarProvider(fakeRadarSource{snapshot: duanxianxia.Snapshot{
		ID: "holiday", TradeDate: "2026-09-30", FetchedAt: now,
		Themes: []duanxianxia.Theme{{Code: "801001", Name: "芯片", Rank: 1}},
	}}, nil, nil, RadarProviderConfig{
		Now: func() time.Time { return now },
		IndustryMomentum: fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{
			{Code: "semi", Name: "半导体", Score: 82},
		}},
	})
	provider.strengthAttemptAt = now
	provider.strengthCache = map[string]themeStrengthScore{"801001": {daily: 80, fiveDay: 75}}
	items, meta, err := provider.Overviews(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	final := finalRadarProgress(t, provider)
	if len(items) != 1 || len(final.Data) != 1 || len(final.Errors) != 0 || final.Steps["kaipanla"] != "ready" || final.Steps["strength"] != "ready" {
		t.Fatalf("valid holiday snapshot was rejected: normal=%+v progressive=%+v", items, final)
	}
	if final.Data[0].Provisional || final.Data[0].DailyStrengthScore != items[0].DailyStrengthScore || final.Data[0].FiveDayStrengthScore != items[0].FiveDayStrengthScore {
		t.Fatalf("delivery modes disagree on holiday strength: normal=%+v progressive=%+v", items, final.Data)
	}
	for _, value := range []foundation.SourceMeta{meta, final.Meta} {
		if value.Stale || value.CarryForward || value.TradeDate != "2026-09-30" || value.FallbackReason != "" {
			t.Fatalf("latest session was reported stale during closure: %+v", value)
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
