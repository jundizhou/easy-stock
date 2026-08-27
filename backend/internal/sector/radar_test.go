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

type fakeRadarSource struct {
	snapshot duanxianxia.Snapshot
	meta     duanxianxia.FetchMeta
}

func (f fakeRadarSource) Snapshot(ctx context.Context) (duanxianxia.Snapshot, duanxianxia.FetchMeta, error) {
	return f.snapshot, f.meta, nil
}

func (f fakeRadarSource) SnapshotByID(ctx context.Context, id string) (duanxianxia.Snapshot, bool, error) {
	return f.snapshot, id == f.snapshot.ID, nil
}

type fakeRadarFallback struct {
	items         []foundation.ThemeOverview
	sectorMap     foundation.SectorMap
	sectorMaps    map[string]foundation.SectorMap
	builtThemeIDs *[]string
}

type fakeIndustryMomentumSource struct {
	items []foundation.MarketIndustryMomentum
	meta  foundation.SourceMeta
	err   error
}

func (f fakeIndustryMomentumSource) IndustryMomentum(context.Context, int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	return append([]foundation.MarketIndustryMomentum(nil), f.items...), f.meta, f.err
}

type fakeRadarStrengthFallback struct {
	fakeRadarFallback
	calls  int
	stocks map[string][]foundation.BoardStock
	err    error
}

func (f *fakeRadarStrengthFallback) radarThemeConstituents(ctx context.Context, themeIDs []string) (map[string][]foundation.BoardStock, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	result := make(map[string][]foundation.BoardStock, len(themeIDs))
	for _, themeID := range themeIDs {
		result[themeID] = append([]foundation.BoardStock(nil), f.stocks[themeID]...)
	}
	return result, nil
}

type fakeRadarStrengthQuotes struct {
	calls   int
	batches [][]string
	quotes  map[string]foundation.Quote
}

func (f *fakeRadarStrengthQuotes) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	f.calls++
	f.batches = append(f.batches, append([]string(nil), symbols...))
	result := make([]foundation.Quote, 0, len(symbols))
	for _, symbol := range symbols {
		if quote, exists := f.quotes[symbol]; exists {
			result = append(result, quote)
		}
	}
	return result, nil
}

func (f fakeRadarFallback) Overviews(ctx context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	return append([]foundation.ThemeOverview(nil), f.items...), foundation.SourceMeta{Source: "local-trend"}, nil
}

func (f fakeRadarFallback) Build(ctx context.Context, themeID string) (foundation.SectorMap, error) {
	if f.builtThemeIDs != nil {
		*f.builtThemeIDs = append(*f.builtThemeIDs, themeID)
	}
	if mapped, exists := f.sectorMaps[themeID]; exists {
		result := mapped
		result.Theme = themeID
		return result, nil
	}
	if f.sectorMap.Theme != "" || len(f.sectorMap.Groups) > 0 {
		result := f.sectorMap
		result.Theme = themeID
		return result, nil
	}
	return foundation.SectorMap{Theme: themeID, Name: "本地题材", Meta: foundation.SourceMeta{Source: "local-trend"}}, nil
}

func TestRadarProviderFusesIndustryAndKaipanlaWithoutLocalTrend(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	snapshot := duanxianxia.Snapshot{
		ID: "snapshot-current", TradeDate: "2026-08-07", FetchedAt: now,
		Themes: []duanxianxia.Theme{
			{Code: "801001", Name: "芯片", Rank: 1, Strength: 9800, History: []duanxianxia.RankPoint{{TradeDate: "2026-08-07", Rank: 1}}},
			{Code: "803023", Name: "AI应用", Rank: 2, Strength: 8600},
		},
	}
	industry := fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{
		{Code: "BK1036", Name: "半导体", ChangePercent: 3, FiveDayChangePercent: 8, TwentyDayChange: 12, Score: 88},
		{Code: "BK0801", Name: "农业", ChangePercent: 2, FiveDayChangePercent: 5, TwentyDayChange: 6, Score: 74},
	}, meta: foundation.SourceMeta{Source: "test:industry", FetchedAt: now, TradeDate: "2026-08-07"}}
	provider := NewRadarProvider(
		fakeRadarSource{snapshot: snapshot},
		fakeRadarFallback{items: []foundation.ThemeOverview{{Theme: "compute_rental", Name: "算力租赁", TrendScore: 91, TradeDate: "2026-08-07"}}},
		nil,
		RadarProviderConfig{Now: func() time.Time { return now }, IndustryMomentum: industry, FallbackFillLimit: 4},
	)
	items, meta, err := provider.Overviews(context.Background())
	if err != nil {
		t.Fatalf("Overviews: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items=%+v", items)
	}
	chip, found := findRadarOverview(items, "芯片")
	if !found || chip.Source != radarFusionSource || chip.IndustryDailyScore == 0 || chip.KaipanlaDailyScore == 0 {
		t.Fatalf("chip was not fused: %+v", items)
	}
	fusion, ok := parseRadarFusionThemeID(chip.Theme)
	if !ok || fusion.KaipanlaCode != "801001" || fusion.IndustryCode != "BK1036" || fusion.IndustryName != "半导体" {
		t.Fatalf("fused theme identity lost its source pair: theme=%q ref=%+v", chip.Theme, fusion)
	}
	if chip.DailyStrengthScore != fusedRadarScore(chip.IndustryDailyScore, chip.KaipanlaDailyScore) {
		t.Fatalf("fused score is not equally weighted: %+v", chip)
	}
	if _, found := findRadarOverview(items, "算力租赁"); found {
		t.Fatalf("local trend leaked into fused candidates: %+v", items)
	}
	if meta.Source != radarFusionSource || chip.Provisional {
		t.Fatalf("unexpected fused metadata: item=%+v meta=%+v", chip, meta)
	}
}

func TestRadarProviderUsesIndustryWhenKaipanlaIsUnavailable(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	industry := fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{
		{Code: "BK1", Name: "煤炭开采", ChangePercent: 3, FiveDayChangePercent: 6, TwentyDayChange: 9, Score: 82},
	}, meta: foundation.SourceMeta{Source: "test:industry", FetchedAt: now}}
	provider := NewRadarProvider(
		nil,
		fakeRadarFallback{items: []foundation.ThemeOverview{{Theme: "local", Name: "本地趋势", TrendScore: 99}}},
		nil,
		RadarProviderConfig{Now: func() time.Time { return now }, IndustryMomentum: industry, FallbackFillLimit: 4},
	)

	items, meta, err := provider.Overviews(context.Background())
	if err != nil {
		t.Fatalf("Overviews: %v", err)
	}
	if len(items) != 1 || items[0].Name != "煤炭开采" || items[0].Source != radarIndustrySource {
		t.Fatalf("industry-only fallback failed: %+v", items)
	}
	if meta.Source != radarIndustrySource || !strings.Contains(meta.FallbackReason, "kaipanla") {
		t.Fatalf("unexpected industry-only metadata: %+v", meta)
	}
}

func TestRadarProviderKeepsIndustryLeaderWhenConstituentsAreEmpty(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	themeID := radarIndustryThemeID("pt01801039", "非金属材料Ⅱ")
	fallback := fakeRadarFallback{sectorMap: foundation.SectorMap{
		Name: "非金属材料Ⅱ",
		Groups: []foundation.SectorMapGroup{{ID: "industry_members", Name: "非金属材料Ⅱ", Nodes: []foundation.SectorMapNode{{
			ID: "industry_core", Name: "非金属材料Ⅱ", Stocks: []foundation.BoardStock{},
		}}}},
	}}
	quotes := &fakeRadarStrengthQuotes{quotes: map[string]foundation.Quote{
		"688300.SH": {Symbol: "688300.SH", Name: "联瑞新材", Price: 186.24, ChangePercent: 20, Meta: foundation.SourceMeta{Source: "test"}},
	}}
	provider := NewRadarProvider(nil, fallback, quotes, RadarProviderConfig{Now: func() time.Time { return now }})
	provider.rememberIndustryLeaders([]foundation.MarketIndustryMomentum{{
		Code: "pt01801039", Name: "非金属材料Ⅱ", LeaderSymbol: "688300.SH", LeaderName: "联瑞新材", LeaderChangePercent: 20,
	}})

	sectorMap, err := provider.Build(context.Background(), themeID)
	if err != nil {
		t.Fatalf("Build industry leader fallback failed: %v", err)
	}
	stock := sectorMap.Groups[0].Nodes[0].Stocks[0]
	if stock.Symbol != "688300.SH" || stock.RankRole != "行业领涨" || stock.Price != 186.24 {
		t.Fatalf("unexpected industry leader fallback: %+v", stock)
	}
}

func TestCalculateRealtimeThemeStrengthUsesConstituentBreadthAndLimitActivity(t *testing.T) {
	stocks := []foundation.BoardStock{
		{Symbol: "600001.SH", Name: "主题龙头", Price: 10, ChangePercent: 1},
		{Symbol: "600002.SH", Name: "主题强势", Price: 10, ChangePercent: 4},
		{Symbol: "600003.SH", Name: "主题跟随", Price: 10, ChangePercent: 2},
		{Symbol: "600004.SH", Name: "主题回落", Price: 10, ChangePercent: -1},
	}
	quotes := map[string]foundation.Quote{
		"600001.SH": {Symbol: "600001.SH", Price: 10.95, PreviousClose: 10, ChangePercent: 9.5},
	}

	if score := calculateRealtimeThemeStrength(stocks, quotes); score != 76 {
		t.Fatalf("score=%d want=76", score)
	}
}

func TestDailyAndFiveDayStrengthUseTheSameConstituentFormula(t *testing.T) {
	stocks := []foundation.BoardStock{
		{Symbol: "600001.SH", Name: "主题龙头", Price: 10},
		{Symbol: "600002.SH", Name: "主题强势", Price: 10},
		{Symbol: "600003.SH", Name: "主题跟随", Price: 10},
		{Symbol: "600004.SH", Name: "主题回落", Price: 10},
	}
	values := []float64{9.5, 4, 2, -1}
	changes := map[string]stockStrengthChange{}
	for index, stock := range stocks {
		changes[stock.Symbol] = stockStrengthChange{
			daily: values[index], fiveDay: values[index], dailyValid: true, fiveDayValid: true,
		}
	}
	daily := calculateThemeStrength(stocks, changes, func(change stockStrengthChange) (float64, bool) {
		return change.daily, change.dailyValid
	})
	fiveDay := calculateThemeStrength(stocks, changes, func(change stockStrengthChange) (float64, bool) {
		return change.fiveDay, change.fiveDayValid
	})
	if daily != 76 || fiveDay != daily {
		t.Fatalf("daily=%d fiveDay=%d", daily, fiveDay)
	}
}

func TestRealtimeStrengthCachesBothWindowsForAtLeastTenMinutes(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	current := now
	theme := duanxianxia.Theme{Code: "801001", Name: "芯片", Rank: 1}
	fallback := &fakeRadarStrengthFallback{stocks: map[string][]foundation.BoardStock{
		"semiconductor": {{Symbol: "600001.SH", Name: "芯片股", Price: 10, ChangePercent: 4, FiveDayChangePercent: 6}},
	}}
	provider := NewRadarProvider(
		fakeRadarSource{}, fallback, nil,
		RadarProviderConfig{Now: func() time.Time { return current }, RealtimeStrengthTTL: time.Minute},
	)

	first := provider.realtimeStrengthScores(context.Background(), []duanxianxia.Theme{theme})
	current = current.Add(2 * time.Minute)
	second := provider.realtimeStrengthScores(context.Background(), []duanxianxia.Theme{theme})
	if fallback.calls != 1 || first[theme.Code].daily == 0 || first[theme.Code].fiveDay == 0 || second[theme.Code] != first[theme.Code] {
		t.Fatalf("cache before ten minutes failed: calls=%d first=%v second=%v", fallback.calls, first, second)
	}

	current = now.Add(10 * time.Minute)
	provider.realtimeStrengthScores(context.Background(), []duanxianxia.Theme{theme})
	if fallback.calls != 2 {
		t.Fatalf("refresh calls=%d want=2", fallback.calls)
	}
}

func TestRealtimeStrengthKeepsBothWindowsWhenRefreshFails(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	current := now
	theme := duanxianxia.Theme{Code: "801001", Name: "芯片", Rank: 1}
	fallback := &fakeRadarStrengthFallback{stocks: map[string][]foundation.BoardStock{
		"semiconductor": {{Symbol: "600001.SH", Name: "芯片股", Price: 10, ChangePercent: 6, FiveDayChangePercent: 8}},
	}}
	provider := NewRadarProvider(fakeRadarSource{}, fallback, nil, RadarProviderConfig{Now: func() time.Time { return current }})

	first := provider.realtimeStrengthScores(context.Background(), []duanxianxia.Theme{theme})
	fallback.err = errors.New("eastmoney unavailable")
	current = now.Add(10 * time.Minute)
	second := provider.realtimeStrengthScores(context.Background(), []duanxianxia.Theme{theme})
	third := provider.realtimeStrengthScores(context.Background(), []duanxianxia.Theme{theme})
	if first[theme.Code].daily == 0 || first[theme.Code].fiveDay == 0 || second[theme.Code] != first[theme.Code] || third[theme.Code] != first[theme.Code] {
		t.Fatalf("failed refresh did not preserve cache: first=%v second=%v third=%v", first, second, third)
	}
	if fallback.calls != 2 {
		t.Fatalf("failed refresh retried too soon: calls=%d", fallback.calls)
	}
}

func TestRealtimeStrengthDeduplicatesQuotesAcrossThemes(t *testing.T) {
	themes := []duanxianxia.Theme{
		{Code: "801001", Name: "芯片", Rank: 1},
		{Code: "801807", Name: "算力", Rank: 2},
	}
	fallback := &fakeRadarStrengthFallback{stocks: map[string][]foundation.BoardStock{
		"semiconductor": {
			{Symbol: "600001.SH", Name: "共同成分", Price: 10, ChangePercent: 2, FiveDayChangePercent: 3},
			{Symbol: "600002.SH", Name: "芯片成分", Price: 10, ChangePercent: 3, FiveDayChangePercent: 4},
		},
		"eastmoney-map:801807": {
			{Symbol: "600001.SH", Name: "共同成分", Price: 10, ChangePercent: 2, FiveDayChangePercent: 3},
			{Symbol: "600003.SH", Name: "算力成分", Price: 10, ChangePercent: 4, FiveDayChangePercent: 5},
		},
	}}
	quotes := &fakeRadarStrengthQuotes{quotes: map[string]foundation.Quote{
		"600001.SH": {Symbol: "600001.SH", Price: 10, ChangePercent: 2},
		"600002.SH": {Symbol: "600002.SH", Price: 10, ChangePercent: 3},
		"600003.SH": {Symbol: "600003.SH", Price: 10, ChangePercent: 4},
	}}
	provider := NewRadarProvider(fakeRadarSource{}, fallback, quotes, RadarProviderConfig{})

	scores, err := provider.calculateRealtimeStrengthScores(context.Background(), themes)
	if err != nil || scores["801001"].daily == 0 || scores["801001"].fiveDay == 0 || scores["801807"].daily == 0 || scores["801807"].fiveDay == 0 {
		t.Fatalf("calculateRealtimeStrengthScores: scores=%v err=%v", scores, err)
	}
	if quotes.calls != 1 || len(quotes.batches) != 1 || len(quotes.batches[0]) != 3 {
		t.Fatalf("quotes were not deduplicated: calls=%d batches=%v", quotes.calls, quotes.batches)
	}
}

func TestRadarProviderDoesNotDuplicateYesterdayTheme(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	snapshot := duanxianxia.Snapshot{
		ID: "snapshot-yesterday", TradeDate: "2026-08-06", FetchedAt: now.Add(-time.Hour),
		Themes: []duanxianxia.Theme{{Code: "801159", Name: "机器人概念", Rank: 1}},
	}
	provider := NewRadarProvider(
		fakeRadarSource{snapshot: snapshot},
		fakeRadarFallback{items: []foundation.ThemeOverview{{Theme: "robot", Name: "机器人", TrendScore: 91, TradeDate: "2026-08-07"}}},
		nil,
		RadarProviderConfig{Now: func() time.Time { return now }, FallbackFillLimit: 3},
	)
	items, _, err := provider.Overviews(context.Background())
	if err != nil {
		t.Fatalf("Overviews: %v", err)
	}
	if len(items) != 1 || items[0].Name != "机器人概念" {
		t.Fatalf("unexpected duplicate handling: %+v", items)
	}
}

func TestRadarProviderDoesNotInsertMappedFallbackThemeAsProvisional(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	snapshot := duanxianxia.Snapshot{
		ID: "snapshot-yesterday", TradeDate: "2026-08-06", FetchedAt: now.Add(-time.Hour),
		Themes: []duanxianxia.Theme{{Code: "801001", Name: "芯片", Rank: 1}},
	}
	provider := NewRadarProvider(
		fakeRadarSource{snapshot: snapshot},
		fakeRadarFallback{items: []foundation.ThemeOverview{{
			Theme: "trend:semiconductor", Name: "半导体芯片", TrendScore: 100, TradeDate: "2026-08-07",
		}}},
		nil,
		RadarProviderConfig{Now: func() time.Time { return now }, FallbackFillLimit: 3},
	)

	items, _, err := provider.Overviews(context.Background())
	if err != nil {
		t.Fatalf("Overviews: %v", err)
	}
	if len(items) != 1 || items[0].Name != "芯片" || items[0].Provisional {
		t.Fatalf("mapped EastMoney theme duplicated Kaipanla theme: %+v", items)
	}
}

func TestRadarProviderNeverUsesLocalTrendAsCandidate(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	snapshot := duanxianxia.Snapshot{
		ID: "snapshot-yesterday", TradeDate: "2026-08-06", FetchedAt: now.Add(-time.Hour),
		Themes: []duanxianxia.Theme{
			{Code: "801660", Name: "通信", Rank: 1},
			{Code: "801001", Name: "芯片", Rank: 2},
		},
	}
	provider := NewRadarProvider(
		fakeRadarSource{snapshot: snapshot},
		fakeRadarFallback{items: []foundation.ThemeOverview{{Theme: "compute_rental", Name: "算力租赁", TrendScore: 91, TradeDate: "2026-08-06"}}},
		nil,
		RadarProviderConfig{Now: func() time.Time { return now }, FallbackFillLimit: 3},
	)
	items, _, err := provider.Overviews(context.Background())
	if err != nil {
		t.Fatalf("Overviews: %v", err)
	}
	if len(items) != 2 || items[0].Name != "通信" || items[1].Name != "芯片" {
		t.Fatalf("local trend must not enter the radar list: %+v", items)
	}
}

func TestRadarProviderInterleavesIndustryAndKaipanlaCandidates(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	snapshot := duanxianxia.Snapshot{
		ID: "snapshot-current", TradeDate: "2026-08-07", FetchedAt: now,
		Themes: []duanxianxia.Theme{
			{Code: "1", Name: "并购重组", Rank: 1, Strength: 9000},
			{Code: "2", Name: "低空经济", Rank: 2, Strength: 8000},
			{Code: "3", Name: "数据要素", Rank: 3, Strength: 7000},
		},
	}
	industry := fakeIndustryMomentumSource{items: []foundation.MarketIndustryMomentum{
		{Code: "BK1", Name: "农业", ChangePercent: 4, FiveDayChangePercent: 8, TwentyDayChange: 10, Score: 90},
		{Code: "BK2", Name: "煤炭开采", ChangePercent: 3, FiveDayChangePercent: 6, TwentyDayChange: 8, Score: 82},
		{Code: "BK3", Name: "房地产服务", ChangePercent: 2, FiveDayChangePercent: 5, TwentyDayChange: 7, Score: 76},
	}, meta: foundation.SourceMeta{Source: "test:industry", FetchedAt: now}}
	provider := NewRadarProvider(
		fakeRadarSource{snapshot: snapshot},
		fakeRadarFallback{items: []foundation.ThemeOverview{
			{Theme: "local-west", Name: "西部大开发", TrendScore: 99, TradeDate: "2026-08-07"},
		}},
		nil,
		RadarProviderConfig{Now: func() time.Time { return now }, IndustryMomentum: industry, FallbackFillLimit: 6},
	)
	items, _, err := provider.Overviews(context.Background())
	if err != nil {
		t.Fatalf("Overviews: %v", err)
	}
	if len(items) != 6 {
		t.Fatalf("items=%+v", items)
	}
	industryCount := 0
	kaipanlaCount := 0
	for _, item := range items[:4] {
		if item.Source == radarIndustrySource {
			industryCount++
		} else if item.Source == duanxianxia.SourceID {
			kaipanlaCount++
		}
	}
	if absRadar(industryCount-kaipanlaCount) > 1 {
		t.Fatalf("sources were not interleaved: %+v", items)
	}
	if _, found := findRadarOverview(items, "西部大开发"); found {
		t.Fatalf("local trend leaked into interleaved candidates: %+v", items)
	}
}

func findRadarOverview(items []foundation.ThemeOverview, name string) (foundation.ThemeOverview, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return foundation.ThemeOverview{}, false
}

func TestRadarProviderBuildsLeaderMapFromSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	snapshot := duanxianxia.Snapshot{
		ID: "snapshot-current", TradeDate: "2026-08-07", FetchedAt: now,
		Themes: []duanxianxia.Theme{{
			Code: "801807", Name: "算力", Rank: 1,
			Leaders: []duanxianxia.Leader{{Rank: 1, Role: "龙一", Symbol: "603629.SH", Name: "利通电子"}},
		}},
	}
	provider := NewRadarProvider(fakeRadarSource{snapshot: snapshot}, fakeRadarFallback{}, nil, RadarProviderConfig{Now: func() time.Time { return now }})
	sectorMap, err := provider.BuildSnapshot(context.Background(), "kpl:801807", snapshot.ID)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	stock := sectorMap.Groups[0].Nodes[0].Stocks[0]
	if stock.Symbol != "603629.SH" || stock.RankRole != "龙一" || stock.RankScore != 96 {
		t.Fatalf("unexpected leader stock: %+v", stock)
	}
}

func TestRadarProviderMapsKaipanlaThemeAndMergesFallbackStocks(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	snapshot := duanxianxia.Snapshot{
		ID: "snapshot-current", TradeDate: "2026-08-07", FetchedAt: now,
		Themes: []duanxianxia.Theme{{
			Code: "801001", Name: "芯片", Rank: 1, LeadersLoaded: true,
			Leaders: []duanxianxia.Leader{{Rank: 1, Role: "龙一", Symbol: "603629.SH", Name: "利通电子"}},
		}},
	}
	builtThemeIDs := []string{}
	fallback := fakeRadarFallback{
		sectorMap: foundation.SectorMap{Name: "半导体芯片", Groups: []foundation.SectorMapGroup{{
			ID: "fallback", Name: "炒作主线", Nodes: []foundation.SectorMapNode{{
				ID: "core", Name: "半导体芯片", StockSource: "eastmoney:stock-selection",
				Stocks: []foundation.BoardStock{{Symbol: "688001.SH", Name: "华兴芯片"}},
			}},
		}}},
		builtThemeIDs: &builtThemeIDs,
	}
	provider := NewRadarProvider(fakeRadarSource{snapshot: snapshot}, fallback, nil, RadarProviderConfig{Now: func() time.Time { return now }})
	sectorMap, err := provider.BuildSnapshot(context.Background(), "kpl:801001", snapshot.ID)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(builtThemeIDs) != 1 || builtThemeIDs[0] != "semiconductor" {
		t.Fatalf("unexpected mapped fallback theme: %v", builtThemeIDs)
	}
	if len(sectorMap.Groups) != 2 || sectorMap.Groups[1].Name != "东财映射 · 炒作主线" {
		t.Fatalf("fallback groups not merged: %+v", sectorMap.Groups)
	}
	if stock := sectorMap.Groups[1].Nodes[0].Stocks[0]; stock.Symbol != "688001.SH" {
		t.Fatalf("fallback stock missing: %+v", stock)
	}
}

func TestRadarProviderFusedScreenMergesLeaderMappedAndIndustryStocks(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	snapshot := duanxianxia.Snapshot{
		ID: "snapshot-current", TradeDate: "2026-08-07", FetchedAt: now,
		Themes: []duanxianxia.Theme{{
			Code: "801660", Name: "通信", Rank: 1, LeadersLoaded: true,
			Leaders: []duanxianxia.Leader{{Rank: 1, Role: "龙一", Symbol: "002792.SZ", Name: "通宇通讯"}},
		}},
	}
	industry := radarIndustryThemeRef{Code: "pt01801047", Name: "通信"}
	industryID := radarIndustryThemeID(industry.Code, industry.Name)
	builtThemeIDs := []string{}
	fallback := fakeRadarFallback{
		sectorMaps: map[string]foundation.SectorMap{
			"eastmoney-map:801660": {
				Name: "通信技术", Groups: []foundation.SectorMapGroup{{
					ID: "mapped", Name: "通信技术", Nodes: []foundation.SectorMapNode{{
						ID: "mapped_core", Name: "通信技术", Stocks: []foundation.BoardStock{{Symbol: "600050.SH", Name: "中国联通"}},
					}},
				}},
			},
			industryID: {
				Name: "通信", Groups: []foundation.SectorMapGroup{{
					ID: "industry_members", Name: "通信", Nodes: []foundation.SectorMapNode{{
						ID: "industry_core", Name: "通信", Stocks: []foundation.BoardStock{{Symbol: "000063.SZ", Name: "中兴通讯"}},
					}},
				}},
			},
		},
		builtThemeIDs: &builtThemeIDs,
	}
	provider := NewRadarProvider(fakeRadarSource{snapshot: snapshot}, fallback, nil, RadarProviderConfig{Now: func() time.Time { return now }})
	fusionID := radarFusionThemeID("801660", industry)

	sectorMap, err := provider.BuildSnapshot(context.Background(), fusionID, snapshot.ID)
	if err != nil {
		t.Fatalf("BuildSnapshot fused theme: %v", err)
	}
	stocks := sectorMapStockSymbols(sectorMap)
	for _, symbol := range []string{"002792.SZ", "600050.SH", "000063.SZ"} {
		if _, exists := stocks[symbol]; !exists {
			t.Fatalf("fused stock pool missing %s: %+v", symbol, stocks)
		}
	}
	if len(builtThemeIDs) != 2 || builtThemeIDs[0] != "eastmoney-map:801660" || builtThemeIDs[1] != industryID {
		t.Fatalf("unexpected fused fallback requests: %v", builtThemeIDs)
	}
	if sectorMap.Theme != fusionID || sectorMap.Meta.Source != radarFusionSource {
		t.Fatalf("unexpected fused sector map identity: %+v", sectorMap)
	}
}

func sectorMapStockSymbols(sectorMap foundation.SectorMap) map[string]struct{} {
	stocks := map[string]struct{}{}
	for _, group := range sectorMap.Groups {
		for _, node := range group.Nodes {
			for _, stock := range node.Stocks {
				stocks[stock.Symbol] = struct{}{}
			}
		}
	}
	return stocks
}

func TestMappedFallbackThemeIDUsesConfiguredEastMoneyMapping(t *testing.T) {
	tests := []struct {
		code string
		name string
		want string
	}{
		{code: "801660", name: "通信", want: "eastmoney-map:801660"},
		{code: "801001", name: "芯片", want: "semiconductor"},
		{code: "801235", name: "化工", want: "chemical"},
		{code: "801843", name: "商业航天", want: "aerospace"},
		{code: "801045", name: "医药", want: "healthcare"},
	}
	for _, test := range tests {
		got, _ := mappedFallbackThemeID(test.code, test.name)
		if got != test.want {
			t.Fatalf("mappedFallbackThemeID(%q, %q)=%q want=%q", test.code, test.name, got, test.want)
		}
	}
}

func TestKaipanlaEastMoneyMappingCoversRollingTwentyDayCatalog(t *testing.T) {
	expected := map[string]string{
		"801660": "通信", "801001": "芯片", "801159": "机器人概念", "803023": "AI应用", "801220": "食品饮料",
		"801346": "智能电网", "801120": "电力", "801878": "端侧AI", "801572": "中报增长", "801045": "医药",
		"801843": "商业航天", "801074": "核电", "801807": "算力", "801199": "汽车零部件", "801250": "并购重组",
		"801004": "锂电池", "801088": "有色金属", "801027": "银行", "801612": "脑机接口", "801062": "军工",
		"801080": "煤炭", "801235": "化工", "801314": "ST板块", "801082": "ST摘帽", "801273": "股权转让",
		"801071": "保险", "801322": "芬太尼替代", "801787": "实控人变更", "803029": "物理AI", "801048": "黄金",
		"801057": "石油石化", "801117": "港口", "801035": "酿酒", "801085": "人工智能", "801445": "元器件",
		"801733": "中特估", "801081": "证券", "801973": "保健品", "801248": "智能驾驶", "801184": "包装印刷",
		"801031": "文化传媒", "801653": "霍乱概念", "801095": "游戏", "801676": "地产链", "801694": "非金属材料",
		"801254": "防脱发", "801414": "教育", "801040": "造纸", "801137": "醋酸", "801862": "高股息精选",
		"801123": "服装家纺", "801856": "破净股概念", "801464": "农业", "801373": "ETC", "801033": "国有企业",
		"801631": "次新股", "801313": "金融概念", "801234": "航运", "801595": "北交所", "801443": "转基因",
	}
	if len(radarThemeMappings) != len(expected) {
		t.Fatalf("mapping count=%d want=%d", len(radarThemeMappings), len(expected))
	}
	for code, name := range expected {
		mapping, exists := lookupRadarThemeMapping(code, name)
		if !exists {
			t.Fatalf("missing mapping for %s %s", code, name)
		}
		if mapping.KaipanlaName != name || mapping.EastMoneyName == "" || (mapping.StaticThemeID == "" && len(mapping.EastMoneyTerms) == 0) {
			t.Fatalf("invalid mapping for %s %s: %+v", code, name, mapping)
		}
		themeID, _ := mappedFallbackThemeID(code, name)
		if _, ok := FindTheme(themeID); !ok {
			t.Fatalf("mapped theme %s is not buildable for %s", themeID, name)
		}
	}
}
