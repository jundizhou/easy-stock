package sector

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

type RadarFallback interface {
	Build(ctx context.Context, themeID string) (foundation.SectorMap, error)
	Overviews(ctx context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error)
}

type RadarSnapshotSource interface {
	Snapshot(ctx context.Context) (duanxianxia.Snapshot, duanxianxia.FetchMeta, error)
	SnapshotByID(ctx context.Context, id string) (duanxianxia.Snapshot, bool, error)
}

type RadarProvider struct {
	source            RadarSnapshotSource
	fallback          RadarFallback
	quotes            QuoteProvider
	now               func() time.Time
	fallbackFill      int
	fallbackTTL       time.Duration
	strengthTTL       time.Duration
	fallbackMu        sync.Mutex
	fallbackAt        time.Time
	fallbackList      []foundation.ThemeOverview
	fallbackMeta      foundation.SourceMeta
	strengthMu        sync.Mutex
	strengthAttemptAt time.Time
	strengthCache     map[string]themeStrengthScore
}

type RadarProviderConfig struct {
	Now                 func() time.Time
	FallbackFillLimit   int
	FallbackCacheTTL    time.Duration
	RealtimeStrengthTTL time.Duration
}

func NewRadarProvider(source RadarSnapshotSource, fallback RadarFallback, quotes QuoteProvider, config RadarProviderConfig) *RadarProvider {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	fallbackFill := config.FallbackFillLimit
	if fallbackFill <= 0 {
		fallbackFill = 16
	}
	fallbackTTL := config.FallbackCacheTTL
	if fallbackTTL <= 0 {
		fallbackTTL = 30 * time.Second
	}
	strengthTTL := config.RealtimeStrengthTTL
	if strengthTTL < 10*time.Minute {
		strengthTTL = 10 * time.Minute
	}
	return &RadarProvider{
		source: source, fallback: fallback, quotes: quotes, now: now,
		fallbackFill: fallbackFill, fallbackTTL: fallbackTTL, strengthTTL: strengthTTL,
		strengthCache: map[string]themeStrengthScore{},
	}
}

func (p *RadarProvider) Overviews(ctx context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	start := time.Now()
	snapshot, fetchMeta, err := p.source.Snapshot(ctx)
	if err != nil || len(snapshot.Themes) == 0 {
		items, meta, fallbackErr := p.fallbackOverviews(ctx)
		if fallbackErr != nil {
			if err != nil {
				return nil, foundation.SourceMeta{}, fmt.Errorf("duanxianxia unavailable: %v; fallback unavailable: %w", err, fallbackErr)
			}
			return nil, foundation.SourceMeta{}, fallbackErr
		}
		if err != nil {
			meta.FallbackReason = err.Error()
		}
		return items, meta, nil
	}

	selectedThemes := append([]duanxianxia.Theme(nil), snapshot.Themes...)
	quoteLookup := p.quoteLookup(ctx, selectedThemes)
	strengthScores := p.realtimeStrengthScores(ctx, selectedThemes)
	carryForward := snapshot.TradeDate != shanghaiDate(p.now())
	items := make([]foundation.ThemeOverview, 0, max(p.fallbackFill, len(selectedThemes)+1))
	for _, theme := range selectedThemes {
		items = append(items, p.themeOverview(snapshot, theme, quoteLookup, carryForward, strengthScores[theme.Code]))
	}

	today := shanghaiDate(p.now())
	localItems := []foundation.ThemeOverview{}
	if carryForward || len(items) < p.fallbackFill {
		if values, _, localErr := p.fallbackOverviews(ctx); localErr == nil {
			localItems = values
		}
	}
	if carryForward && isWeekdayShanghai(p.now()) {
		if len(localItems) > 0 && localItems[0].TradeDate == today && isNewLocalFirst(localItems[0], snapshot.Themes) {
			provisional := localItems[0]
			provisional.Source = "local-trend"
			provisional.TradeDate = today
			provisional.SnapshotID = snapshot.ID
			provisional.Provisional = true
			provisional.CarryForward = false
			if len(items) == 0 {
				items = append(items, provisional)
			} else {
				items = append(items, foundation.ThemeOverview{})
				copy(items[2:], items[1:])
				items[1] = provisional
			}
		}
	}
	for _, local := range localItems {
		if len(items) >= p.fallbackFill {
			break
		}
		if hasThemeOverview(items, local) {
			continue
		}
		supplement := local
		supplement.Source = "local-fallback"
		supplement.SnapshotID = ""
		supplement.CarryForward = false
		supplement.Provisional = false
		items = append(items, supplement)
	}
	for index := range items {
		items[index].SourceRank = index + 1
	}

	fallbackReason := strings.TrimSpace(fetchMeta.RefreshError)
	if fallbackReason == "" && carryForward {
		fallbackReason = "开盘啦尚未更新，沿用上一交易日"
	}
	meta := foundation.SourceMeta{
		Source:         duanxianxia.SourceID,
		SourceURL:      duanxianxia.DefaultBaseURL + "/web/platerotat",
		FetchedAt:      snapshot.FetchedAt,
		LatencyMS:      time.Since(start).Milliseconds(),
		Stale:          carryForward || fallbackReason != "",
		TradeDate:      snapshot.TradeDate,
		SnapshotID:     snapshot.ID,
		NextRefreshAt:  timePointer(fetchMeta.NextAllowedAt),
		FallbackReason: fallbackReason,
		CarryForward:   carryForward,
	}
	return items, meta, nil
}

func (p *RadarProvider) Build(ctx context.Context, themeID string) (foundation.SectorMap, error) {
	return p.BuildSnapshot(ctx, themeID, "")
}

func (p *RadarProvider) BuildSnapshot(ctx context.Context, themeID string, snapshotID string) (foundation.SectorMap, error) {
	if !strings.HasPrefix(themeID, "kpl:") {
		return p.fallback.Build(ctx, themeID)
	}
	var snapshot duanxianxia.Snapshot
	if snapshotID != "" {
		stored, exists, err := p.source.SnapshotByID(ctx, snapshotID)
		if err != nil {
			return foundation.SectorMap{}, err
		}
		if !exists {
			return foundation.SectorMap{}, fmt.Errorf("theme snapshot %s is no longer available", snapshotID)
		}
		snapshot = stored
	} else {
		current, _, err := p.source.Snapshot(ctx)
		if err != nil {
			return foundation.SectorMap{}, err
		}
		snapshot = current
	}
	code := strings.TrimPrefix(themeID, "kpl:")
	theme, ok := snapshot.FindTheme(code)
	if !ok {
		return foundation.SectorMap{}, fmt.Errorf("unknown kaipanla theme: %s", code)
	}

	symbols := make([]string, 0, len(theme.Leaders))
	for _, leader := range theme.Leaders {
		symbols = append(symbols, leader.Symbol)
	}
	quotes := map[string]foundation.Quote{}
	if p.quotes != nil && len(symbols) > 0 {
		if values, err := p.quotes.Realtime(ctx, symbols); err == nil {
			for _, quote := range values {
				quotes[quote.Symbol] = quote
			}
		}
	}
	stocks := make([]foundation.BoardStock, 0, len(theme.Leaders))
	for _, leader := range theme.Leaders {
		stock := foundation.BoardStock{
			Symbol:    leader.Symbol,
			Name:      leader.Name,
			RankScore: max(60, 104-leader.Rank*8),
			RankRole:  leader.Role,
			Meta: foundation.SourceMeta{
				Source:       duanxianxia.SourceID,
				SourceURL:    duanxianxia.DefaultBaseURL + "/web/platerotat",
				FetchedAt:    snapshot.FetchedAt,
				TradeDate:    snapshot.TradeDate,
				SnapshotID:   snapshot.ID,
				CarryForward: snapshot.TradeDate != shanghaiDate(p.now()),
			},
		}
		if quote, exists := quotes[leader.Symbol]; exists {
			stock.Price = quote.Price
			stock.Change = quote.Change
			stock.ChangePercent = quote.ChangePercent
		}
		stocks = append(stocks, stock)
	}
	changePercent := averageStockChange(stocks)
	carryForward := snapshot.TradeDate != shanghaiDate(p.now())
	warnings := []string{}
	if carryForward {
		warnings = append(warnings, "开盘啦尚未更新，题材归属沿用上一交易日；行情指标使用当前数据。")
	}
	if theme.NoLeaders {
		warnings = append(warnings, "开盘啦标记该题材当日无领涨股。")
	} else if !theme.LeadersLoaded && len(theme.Leaders) == 0 {
		warnings = append(warnings, "该题材不在开盘啦领涨股优先请求范围内，个股池使用东财题材映射补充。")
	}
	meta := foundation.SourceMeta{
		Source:       duanxianxia.SourceID,
		SourceURL:    duanxianxia.DefaultBaseURL + "/web/platerotat",
		FetchedAt:    snapshot.FetchedAt,
		Stale:        carryForward,
		TradeDate:    snapshot.TradeDate,
		SnapshotID:   snapshot.ID,
		CarryForward: carryForward,
	}
	result := foundation.SectorMap{
		Theme: themeID,
		Name:  theme.Name,
		Tabs:  []string{"开盘啦领涨"},
		ThemeTabs: []foundation.SectorMapTab{{
			ID: themeID, Name: theme.Name,
		}},
		Groups: []foundation.SectorMapGroup{{
			ID:   "kaipanla",
			Name: "开盘啦领涨",
			Nodes: []foundation.SectorMapNode{{
				ID:             "kaipanla_leaders",
				Name:           "龙一至龙五",
				Description:    "题材归属与领涨顺序来自开盘啦板块；实时行情和领导力指标由本地行情源补充。",
				BoardCode:      theme.Code,
				BoardName:      theme.Name,
				BoardSource:    duanxianxia.SourceID,
				ChangePercent:  changePercent,
				Stocks:         stocks,
				StockSource:    duanxianxia.SourceID,
				MatchStatus:    "matched",
				MatchedBy:      []string{"开盘啦龙一至龙五"},
				Warnings:       warnings,
				CandidateCount: len(stocks),
			}},
		}},
		Meta: meta,
	}
	p.mergeFallbackStocks(ctx, theme, &result)
	return result, nil
}

func (p *RadarProvider) themeOverview(
	snapshot duanxianxia.Snapshot,
	theme duanxianxia.Theme,
	quotes map[string]foundation.Quote,
	carryForward bool,
	strength themeStrengthScore,
) foundation.ThemeOverview {
	total := 0.0
	matched := 0
	rising := 0
	falling := 0
	leaders := make([]string, 0, len(theme.Leaders))
	for _, leader := range theme.Leaders {
		leaders = append(leaders, leader.Name)
		if quote, exists := quotes[leader.Symbol]; exists {
			total += quote.ChangePercent
			matched++
			if quote.ChangePercent > 0 {
				rising++
			} else if quote.ChangePercent < 0 {
				falling++
			}
		}
	}
	changePercent := 0.0
	if matched > 0 {
		changePercent = total / float64(matched)
	}
	activeDays := min(len(theme.History), 5)
	score := min(100, max(35, 102-theme.Rank*10+activeDays*2))
	topNode := ""
	if len(theme.Leaders) > 0 {
		topNode = theme.Leaders[0].Name
	}
	return foundation.ThemeOverview{
		Theme:                "kpl:" + theme.Code,
		Name:                 theme.Name,
		ChangePercent:        changePercent,
		RisingNodes:          rising,
		FallingNodes:         falling,
		MatchedNodes:         matched,
		TotalNodes:           len(theme.Leaders),
		TopNode:              topNode,
		TrendScore:           score,
		DailyStrengthScore:   strength.daily,
		FiveDayStrengthScore: strength.fiveDay,
		TrendStage:           kaipanlaTrendStage(theme),
		LimitUpCount:         len(theme.Leaders),
		ActiveDays:           activeDays,
		Leaders:              leaders,
		Source:               duanxianxia.SourceID,
		ProviderRank:         theme.Rank,
		SourceStrength:       theme.Strength,
		TradeDate:            snapshot.TradeDate,
		SnapshotID:           snapshot.ID,
		CarryForward:         carryForward,
		TopNodeChangePercent: quoteChange(quotes, firstLeaderSymbol(theme)),
	}
}

type themeStrengthScore struct {
	daily   int
	fiveDay int
}

func (p *RadarProvider) quoteLookup(ctx context.Context, themes []duanxianxia.Theme) map[string]foundation.Quote {
	result := map[string]foundation.Quote{}
	if p.quotes == nil {
		return result
	}
	symbols := []string{}
	seen := map[string]struct{}{}
	for _, theme := range themes {
		for _, leader := range theme.Leaders {
			if _, exists := seen[leader.Symbol]; exists {
				continue
			}
			seen[leader.Symbol] = struct{}{}
			symbols = append(symbols, leader.Symbol)
		}
	}
	if len(symbols) == 0 {
		return result
	}
	quotes, err := p.quotes.Realtime(ctx, symbols)
	if err != nil {
		return result
	}
	for _, quote := range quotes {
		result[quote.Symbol] = quote
	}
	return result
}

func isNewLocalFirst(candidate foundation.ThemeOverview, prior []duanxianxia.Theme) bool {
	if normalizeThemeName(candidate.Name) == "" {
		return false
	}
	for _, theme := range prior {
		if kaipanlaThemeMatchesOverview(theme, candidate) {
			return false
		}
	}
	return true
}

func normalizeThemeName(name string) string {
	value := strings.ToLower(strings.TrimSpace(name))
	replacer := strings.NewReplacer(" ", "", "　", "", "-", "", "_", "", "概念", "", "板块", "")
	return replacer.Replace(value)
}

func kaipanlaTrendStage(theme duanxianxia.Theme) string {
	if len(theme.History) <= 1 {
		return "发酵"
	}
	current := theme.History[0].Rank
	previous := theme.History[1].Rank
	if current < previous {
		return "主升"
	}
	if current > previous {
		return "分歧"
	}
	if len(theme.History) >= 3 {
		return "扩散"
	}
	return "发酵"
}

func averageStockChange(stocks []foundation.BoardStock) float64 {
	if len(stocks) == 0 {
		return 0
	}
	total := 0.0
	count := 0
	for _, stock := range stocks {
		if stock.Price <= 0 && stock.ChangePercent == 0 {
			continue
		}
		total += stock.ChangePercent
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func firstLeaderSymbol(theme duanxianxia.Theme) string {
	if len(theme.Leaders) == 0 {
		return ""
	}
	return theme.Leaders[0].Symbol
}

func quoteChange(quotes map[string]foundation.Quote, symbol string) float64 {
	if quote, exists := quotes[symbol]; exists {
		return quote.ChangePercent
	}
	return 0
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func shanghaiDate(value time.Time) string {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	return value.In(location).Format("2006-01-02")
}

func isWeekdayShanghai(value time.Time) bool {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	weekday := value.In(location).Weekday()
	return weekday != time.Saturday && weekday != time.Sunday
}
