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
}

type RadarSnapshotSource interface {
	Snapshot(ctx context.Context) (duanxianxia.Snapshot, duanxianxia.FetchMeta, error)
	SnapshotByID(ctx context.Context, id string) (duanxianxia.Snapshot, bool, error)
}

type IndustryMomentumSource interface {
	IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error)
}

type RadarProvider struct {
	source            RadarSnapshotSource
	industry          IndustryMomentumSource
	fallback          RadarFallback
	quotes            QuoteProvider
	now               func() time.Time
	fallbackFill      int
	strengthTTL       time.Duration
	strengthMu        sync.Mutex
	strengthAttemptAt time.Time
	strengthCache     map[string]themeStrengthScore
	industryLeaderMu  sync.RWMutex
	industryLeaders   map[string]radarIndustryLeader
}

type RadarProviderConfig struct {
	Now                 func() time.Time
	IndustryMomentum    IndustryMomentumSource
	FallbackFillLimit   int
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
	strengthTTL := config.RealtimeStrengthTTL
	if strengthTTL < 10*time.Minute {
		strengthTTL = 10 * time.Minute
	}
	return &RadarProvider{
		source: source, industry: config.IndustryMomentum, fallback: fallback, quotes: quotes, now: now,
		fallbackFill: fallbackFill, strengthTTL: strengthTTL,
		strengthCache: map[string]themeStrengthScore{}, industryLeaders: map[string]radarIndustryLeader{},
	}
}

func (p *RadarProvider) Overviews(ctx context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	start := time.Now()
	var snapshot duanxianxia.Snapshot
	var fetchMeta duanxianxia.FetchMeta
	var snapshotErr error
	var industries []foundation.MarketIndustryMomentum
	var industryMeta foundation.SourceMeta
	var industryErr error
	var wg sync.WaitGroup

	if p.source != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot, fetchMeta, snapshotErr = p.source.Snapshot(ctx)
		}()
	} else {
		snapshotErr = fmt.Errorf("kaipanla radar source is unavailable")
	}
	if p.industry != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			industries, industryMeta, industryErr = p.industry.IndustryMomentum(ctx, max(24, p.fallbackFill))
		}()
	} else {
		industryErr = fmt.Errorf("industry momentum source is unavailable")
	}
	wg.Wait()

	items, meta, err := p.fusedOverviews(ctx, snapshot, fetchMeta, snapshotErr, industries, industryMeta, industryErr)
	meta.LatencyMS = time.Since(start).Milliseconds()
	return items, meta, err
}

func (p *RadarProvider) Build(ctx context.Context, themeID string) (foundation.SectorMap, error) {
	return p.BuildSnapshot(ctx, themeID, "")
}

func (p *RadarProvider) BuildSnapshot(ctx context.Context, themeID string, snapshotID string) (foundation.SectorMap, error) {
	themeID = strings.TrimSpace(themeID)
	if fusion, ok := parseRadarFusionThemeID(themeID); ok {
		return p.buildFusionSnapshot(ctx, themeID, fusion, snapshotID)
	}
	if strings.HasPrefix(themeID, radarFusionThemePrefix) {
		return foundation.SectorMap{}, fmt.Errorf("invalid fused radar theme: %s", themeID)
	}
	if industry, ok := parseRadarIndustryThemeID(themeID); ok {
		return p.buildIndustrySnapshot(ctx, themeID, industry, p.industryLeader(themeID))
	}
	if !strings.HasPrefix(themeID, "kpl:") {
		if p.fallback == nil {
			return foundation.SectorMap{}, fmt.Errorf("sector map fallback is unavailable")
		}
		return p.fallback.Build(ctx, themeID)
	}
	return p.buildKaipanlaSnapshot(ctx, themeID, snapshotID)
}

func (p *RadarProvider) buildIndustrySnapshot(ctx context.Context, themeID string, industry radarIndustryThemeRef, leader radarIndustryLeader) (foundation.SectorMap, error) {
	if p.fallback != nil {
		result, err := p.fallback.Build(ctx, themeID)
		if err == nil {
			if sectorMapStockCount(result) == 0 {
				p.addIndustryLeaderFallback(ctx, leader, &result, "行业成分股暂不可用，已保留行业领涨股。")
			}
			return result, nil
		} else if leader.Symbol == "" {
			return foundation.SectorMap{}, err
		} else {
			result = emptyIndustrySectorMap(themeID, industry, p.now())
			p.addIndustryLeaderFallback(ctx, leader, &result, fmt.Sprintf("行业成分映射失败，已保留行业领涨股：%s", err.Error()))
			return result, nil
		}
	}
	if leader.Symbol == "" {
		return foundation.SectorMap{}, fmt.Errorf("sector map fallback is unavailable")
	}
	result := emptyIndustrySectorMap(themeID, industry, p.now())
	p.addIndustryLeaderFallback(ctx, leader, &result, "行业成分映射暂不可用，已保留行业领涨股。")
	return result, nil
}

func (p *RadarProvider) addIndustryLeaderFallback(ctx context.Context, leader radarIndustryLeader, result *foundation.SectorMap, warning string) {
	if result == nil || leader.Symbol == "" || len(result.Groups) == 0 || len(result.Groups[0].Nodes) == 0 {
		return
	}
	stock := foundation.BoardStock{
		Symbol:        leader.Symbol,
		Name:          leader.Name,
		ChangePercent: leader.ChangePercent,
		RankScore:     100,
		RankRole:      "行业领涨",
		Meta:          foundation.SourceMeta{Source: radarIndustrySource, FetchedAt: p.now()},
	}
	if p.quotes != nil {
		if quotes, err := p.quotes.Realtime(ctx, []string{leader.Symbol}); err == nil && len(quotes) > 0 {
			quote := quotes[0]
			stock.Name = firstNonEmptyRadar(quote.Name, stock.Name)
			stock.Price = quote.Price
			stock.Change = quote.Change
			stock.ChangePercent = quote.ChangePercent
			stock.Meta = quote.Meta
		}
	}
	node := &result.Groups[0].Nodes[0]
	node.Stocks = mergeBoardStock(node.Stocks, stock)
	node.StockSource = firstNonEmptyRadar(node.StockSource, radarIndustrySource+":leader")
	node.MatchStatus = "matched"
	node.MatchedBy = append(node.MatchedBy, "industry-leader")
	node.Warnings = append(node.Warnings, warning)
}

func (p *RadarProvider) rememberIndustryLeaders(items []foundation.MarketIndustryMomentum) {
	leaders := make(map[string]radarIndustryLeader, len(items))
	for _, item := range items {
		if item.LeaderSymbol == "" {
			continue
		}
		leaders[radarIndustryThemeID(item.Code, item.Name)] = radarIndustryLeader{
			Symbol: item.LeaderSymbol, Name: item.LeaderName, ChangePercent: item.LeaderChangePercent,
		}
	}
	p.industryLeaderMu.Lock()
	defer p.industryLeaderMu.Unlock()
	p.industryLeaders = leaders
}

func (p *RadarProvider) industryLeader(themeID string) radarIndustryLeader {
	p.industryLeaderMu.RLock()
	defer p.industryLeaderMu.RUnlock()
	return p.industryLeaders[themeID]
}

func emptyIndustrySectorMap(themeID string, industry radarIndustryThemeRef, now time.Time) foundation.SectorMap {
	return foundation.SectorMap{
		Theme:     themeID,
		Name:      industry.Name,
		Tabs:      []string{industry.Name},
		ThemeTabs: []foundation.SectorMapTab{{ID: themeID, Name: industry.Name}},
		Groups: []foundation.SectorMapGroup{{
			ID: "industry_members", Name: industry.Name,
			Nodes: []foundation.SectorMapNode{{ID: "industry_core", Name: industry.Name, Description: "依据行业趋势强度对应的行业归属筛选成分股。", Stocks: []foundation.BoardStock{}}},
		}},
		Meta: foundation.SourceMeta{Source: radarIndustrySource, FetchedAt: now, FallbackReason: "行业成分映射暂不可用"},
	}
}

func sectorMapStockCount(sectorMap foundation.SectorMap) int {
	count := 0
	for _, group := range sectorMap.Groups {
		for _, node := range group.Nodes {
			count += len(node.Stocks)
		}
	}
	return count
}

func (p *RadarProvider) buildFusionSnapshot(
	ctx context.Context,
	themeID string,
	fusion radarFusionThemeRef,
	snapshotID string,
) (foundation.SectorMap, error) {
	result, err := p.buildKaipanlaSnapshot(ctx, "kpl:"+fusion.KaipanlaCode, snapshotID)
	if err != nil {
		return foundation.SectorMap{}, err
	}
	result.Theme = themeID
	result.ThemeTabs = []foundation.SectorMapTab{{ID: themeID, Name: result.Name}}
	result.Meta.Source = radarFusionSource

	if p.fallback == nil {
		appendKaipanlaWarning(&result, "行业成分股映射暂不可用。")
		return result, nil
	}
	industryID := radarIndustryThemeID(fusion.IndustryCode, fusion.IndustryName)
	industryMap, industryErr := p.fallback.Build(ctx, industryID)
	if industryErr != nil {
		appendKaipanlaWarning(&result, fmt.Sprintf("行业“%s”成分股映射失败：%s", fusion.IndustryName, industryErr.Error()))
		return result, nil
	}
	mergeFusionIndustryStocks(fusion.IndustryName, industryMap, &result)
	if industryMap.Meta.FetchedAt.After(result.Meta.FetchedAt) {
		result.Meta.FetchedAt = industryMap.Meta.FetchedAt
	}
	return result, nil
}

func (p *RadarProvider) buildKaipanlaSnapshot(ctx context.Context, themeID string, snapshotID string) (foundation.SectorMap, error) {
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
