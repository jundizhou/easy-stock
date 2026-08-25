package sector

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/narrative"
)

type BoardProvider interface {
	Boards(ctx context.Context, keyword string, limit int) ([]foundation.Board, error)
	BoardStocks(ctx context.Context, boardCode string, limit int) ([]foundation.BoardStock, error)
}

type QuoteProvider interface {
	Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error)
}

type LimitUpProvider interface {
	RecentLimitUps(ctx context.Context, lookbackDays int) ([]foundation.LimitUpEvent, error)
}

type StockCatalogProvider interface {
	StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error)
}

type MapperOption func(*Mapper)

type Mapper struct {
	provider      BoardProvider
	quoteProvider QuoteProvider
	limitUp       LimitUpProvider
	stockCatalog  StockCatalogProvider
}

func NewMapper(provider BoardProvider, opts ...MapperOption) *Mapper {
	mapper := &Mapper{provider: provider}
	for _, opt := range opts {
		opt(mapper)
	}
	return mapper
}

func WithQuoteProvider(provider QuoteProvider) MapperOption {
	return func(m *Mapper) {
		m.quoteProvider = provider
	}
}

func WithLimitUpProvider(provider LimitUpProvider) MapperOption {
	return func(m *Mapper) {
		m.limitUp = provider
	}
}

func WithStockCatalogProvider(provider StockCatalogProvider) MapperOption {
	return func(m *Mapper) {
		m.stockCatalog = provider
	}
}

func (m *Mapper) Build(ctx context.Context, themeID string) (foundation.SectorMap, error) {
	if m.provider == nil {
		return foundation.SectorMap{}, fmt.Errorf("board provider is required")
	}
	theme, ok := FindTheme(strings.TrimSpace(themeID))
	if !ok {
		return foundation.SectorMap{}, fmt.Errorf("unknown sector theme: %s", themeID)
	}

	start := time.Now()
	boards, boardErr, catalog, catalogErr := m.loadBoardsAndCatalog(ctx)
	if boardErr != nil && len(catalog) == 0 {
		return foundation.SectorMap{}, fmt.Errorf("board list unavailable: %v; stock catalog unavailable: %w", boardErr, catalogErr)
	}
	catalogIndex := newCatalogIndex(catalog)
	limitEvents := []foundation.LimitUpEvent{}
	if m.limitUp != nil {
		if events, limitErr := m.limitUp.RecentLimitUps(ctx, 12); limitErr == nil {
			limitEvents = events
		}
	}

	tabs := themeTabs()
	_, dynamic := narrative.ThemeName(theme.ID)
	if dynamic || strings.HasPrefix(theme.ID, radarMappedThemePrefix) || strings.HasPrefix(theme.ID, radarIndustryThemePrefix) {
		tabs = []foundation.SectorMapTab{{ID: theme.ID, Name: theme.Name}}
	}
	result := foundation.SectorMap{
		Theme:     theme.ID,
		Name:      theme.Name,
		Tabs:      append([]string(nil), theme.Tabs...),
		ThemeTabs: tabs,
		Groups:    make([]foundation.SectorMapGroup, 0, len(theme.Groups)),
		Meta: foundation.SourceMeta{
			Source:    "sector-map:eastmoney",
			FetchedAt: time.Now(),
			LatencyMS: time.Since(start).Milliseconds(),
		},
	}

	for _, group := range theme.Groups {
		outGroup := foundation.SectorMapGroup{
			ID:    group.ID,
			Name:  group.Name,
			Nodes: make([]foundation.SectorMapNode, 0, len(group.Nodes)),
		}
		for _, node := range group.Nodes {
			outGroup.Nodes = append(outGroup.Nodes, m.buildNode(ctx, node, boards, catalogIndex, limitEvents, catalogErr))
		}
		result.Groups = append(result.Groups, outGroup)
	}
	return result, nil
}

func (m *Mapper) Overviews(ctx context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	if m.provider == nil {
		return nil, foundation.SourceMeta{}, fmt.Errorf("board provider is required")
	}
	start := time.Now()
	boards, boardErr, catalog, catalogErr := m.loadBoardsAndCatalog(ctx)
	var events []foundation.LimitUpEvent
	var trendErr error
	if m.limitUp != nil {
		events, trendErr = m.limitUp.RecentLimitUps(ctx, 12)
	}
	if len(catalog) > 0 && len(events) > 0 {
		items := buildTrendOverviews(events, catalog)
		if len(items) > 0 {
			return items, foundation.SourceMeta{
				Source:    "trend-overview:eastmoney:limit-up+stock-selection",
				FetchedAt: time.Now(),
				LatencyMS: time.Since(start).Milliseconds(),
				TradeDate: items[0].TradeDate,
			}, nil
		}
	}
	if boardErr != nil && len(catalog) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("board list unavailable: %v; stock catalog unavailable: %v; trend data unavailable: %v", boardErr, catalogErr, trendErr)
	}
	catalogIndex := newCatalogIndex(catalog)
	items := make([]foundation.ThemeOverview, 0, len(Themes()))
	for _, theme := range Themes() {
		items = append(items, buildThemeOverview(theme, boards, catalogIndex))
	}
	meta := foundation.SourceMeta{
		Source:    "theme-overview:eastmoney:stock-selection",
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	return items, meta, nil
}

func (m *Mapper) loadBoardsAndCatalog(ctx context.Context) (
	[]foundation.Board,
	error,
	[]foundation.StockCatalogEntry,
	error,
) {
	var boards []foundation.Board
	var boardErr error
	var catalog []foundation.StockCatalogEntry
	var catalogErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		boardCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		boards, boardErr = m.provider.Boards(boardCtx, "", 600)
	}()

	if m.stockCatalog != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			catalog, catalogErr = m.stockCatalog.StockCatalog(ctx)
		}()
	}
	wg.Wait()
	if boardErr != nil {
		boards = []foundation.Board{}
	}
	return boards, boardErr, catalog, catalogErr
}

func buildThemeOverview(theme Theme, boards []foundation.Board, catalog catalogIndex) foundation.ThemeOverview {
	overview := foundation.ThemeOverview{Theme: theme.ID, Name: theme.Name}
	flowBoards := map[string]struct{}{}
	for _, group := range theme.Groups {
		overview.TotalNodes += len(group.Nodes)
		for _, node := range group.Nodes {
			board, _, ok := matchBoard(node, boards)
			catalogStocks := catalogStocksForNode(catalog, node, 0)
			catalogChange := averageChangePercent(catalogStocks)
			changePercent := catalogChange
			if ok {
				if board.ChangePercent != 0 || len(catalogStocks) == 0 {
					changePercent = board.ChangePercent
				}
				if _, exists := flowBoards[board.Code]; !exists {
					overview.MainNetInflow += board.MainNetInflow
					flowBoards[board.Code] = struct{}{}
				}
			} else if len(catalogStocks) == 0 {
				continue
			}
			overview.MatchedNodes++
			overview.ChangePercent += changePercent
			if changePercent > 0 {
				overview.RisingNodes++
			} else if changePercent < 0 {
				overview.FallingNodes++
			}
			if overview.TopNode == "" || changePercent > overview.TopNodeChangePercent {
				overview.TopNode = node.Name
				overview.TopNodeChangePercent = changePercent
			}
		}
	}
	if overview.MatchedNodes > 0 {
		overview.ChangePercent /= float64(overview.MatchedNodes)
	}
	return overview
}

func themeTabs() []foundation.SectorMapTab {
	themes := Themes()
	tabs := make([]foundation.SectorMapTab, 0, len(themes))
	for _, theme := range themes {
		tabs = append(tabs, foundation.SectorMapTab{ID: theme.ID, Name: theme.Name})
	}
	return tabs
}

func (m *Mapper) buildNode(
	ctx context.Context,
	node Node,
	boards []foundation.Board,
	catalog catalogIndex,
	limitEvents []foundation.LimitUpEvent,
	catalogErr error,
) foundation.SectorMapNode {
	out := foundation.SectorMapNode{
		ID:          node.ID,
		Name:        node.Name,
		Description: node.Description,
		MatchStatus: "unmatched",
		Stocks:      []foundation.BoardStock{},
	}
	board, matchedBy, boardMatched := matchBoard(node, boards)
	if boardMatched {
		out.BoardCode = board.Code
		out.BoardName = board.Name
		out.BoardSource = board.Meta.Source
		out.ChangePercent = board.ChangePercent
		out.MainNetInflow = board.MainNetInflow
		out.MatchStatus = "matched"
		out.MatchedBy = matchedBy

		if len(catalog) == 0 {
			stocks, err := m.provider.BoardStocks(ctx, board.Code, 30)
			if err == nil && len(stocks) > 0 {
				out.Stocks = append([]foundation.BoardStock(nil), stocks...)
				out.StockSource = "eastmoney:board-constituents"
			}
		}
	} else {
		out.MatchedBy = append([]string(nil), node.BoardKeywords...)
	}

	catalogStocks := catalogStocksForNode(catalog, node, 0)
	for _, stock := range catalogStocks {
		out.Stocks = mergeBoardStock(out.Stocks, stock)
	}
	if len(catalogStocks) > 0 {
		if out.StockSource == "" {
			out.StockSource = "eastmoney:stock-selection"
		} else if !strings.Contains(out.StockSource, "stock-selection") {
			out.StockSource += "+stock-selection"
		}
		out.MatchStatus = "matched"
		if node.Narrative != "" {
			out.MatchedBy = append(out.MatchedBy, "catalog:trading-narrative")
		} else {
			out.MatchedBy = append(out.MatchedBy, "catalog:industry+concept")
		}
	}
	if len(out.Stocks) == 0 && boardMatched && len(catalog) > 0 {
		if stocks, err := m.provider.BoardStocks(ctx, board.Code, 30); err == nil && len(stocks) > 0 {
			out.Stocks = append([]foundation.BoardStock(nil), stocks...)
			out.StockSource = "eastmoney:board-constituents"
			out.MatchStatus = "matched"
			out.MatchedBy = append(out.MatchedBy, "board-constituents:fallback")
		}
	}
	m.hydrateLimitUpCandidates(ctx, node, catalog, limitEvents, &out)
	if out.ChangePercent == 0 && len(out.Stocks) > 0 {
		out.ChangePercent = averageChangePercent(out.Stocks)
	}
	if len(out.Stocks) == 0 {
		if catalogErr != nil {
			out.Warnings = append(out.Warnings, "东方财富股票目录暂不可用")
		} else if !boardMatched {
			out.Warnings = append(out.Warnings, "未匹配到题材成分")
		} else {
			out.Warnings = append(out.Warnings, "未获取到股票行情")
		}
	}
	return out
}

type rankedCatalogStock struct {
	stock foundation.BoardStock
	score int
}

type indexedCatalogEntry struct {
	stock       foundation.StockCatalogEntry
	industryKey string
	memberships []string
}

type catalogIndex []indexedCatalogEntry

type normalizedCatalogTerm struct {
	raw        string
	normalized string
}

var membershipNormalizer = strings.NewReplacer(
	"概念", "",
	"行业", "",
	"（医疗）", "",
	"(医疗)", "",
	"ⅲ", "",
	" ", "",
	"/", "",
)

func newCatalogIndex(catalog []foundation.StockCatalogEntry) catalogIndex {
	index := make(catalogIndex, 0, len(catalog))
	for _, entry := range catalog {
		memberships := make([]string, 0, len(entry.Concepts)+1)
		industryKey := normalizeMembership(entry.Industry)
		if industryKey != "" {
			memberships = append(memberships, industryKey)
		}
		for _, concept := range entry.Concepts {
			if normalized := normalizeMembership(concept); normalized != "" {
				memberships = append(memberships, normalized)
			}
		}
		index = append(index, indexedCatalogEntry{
			stock:       entry,
			industryKey: industryKey,
			memberships: memberships,
		})
	}
	return index
}

func catalogStocksForNode(catalog catalogIndex, node Node, limit int) []foundation.BoardStock {
	if len(catalog) == 0 {
		return []foundation.BoardStock{}
	}
	rawTerms := append([]string{node.Name}, node.BoardKeywords...)
	terms := make([]normalizedCatalogTerm, 0, len(rawTerms))
	for _, term := range rawTerms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		terms = append(terms, normalizedCatalogTerm{raw: term, normalized: normalizeMembership(term)})
	}
	matches := make([]rankedCatalogStock, 0, 64)
	for _, entry := range catalog {
		score := catalogNodeMembershipScore(entry, node, terms)
		if score == 0 {
			continue
		}
		matches = append(matches, rankedCatalogStock{stock: entry.stock.BoardStock, score: score})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		if matches[i].stock.ChangePercent != matches[j].stock.ChangePercent {
			return matches[i].stock.ChangePercent > matches[j].stock.ChangePercent
		}
		return matches[i].stock.Amount > matches[j].stock.Amount
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	stocks := make([]foundation.BoardStock, 0, len(matches))
	for _, match := range matches {
		stocks = append(stocks, match.stock)
	}
	return stocks
}

func catalogNodeMembershipScore(entry indexedCatalogEntry, node Node, terms []normalizedCatalogTerm) int {
	if node.Industry != "" {
		score := membershipMatchScore(entry.industryKey, normalizedCatalogTerm{
			raw:        node.Industry,
			normalized: normalizeMembership(node.Industry),
		})
		if score > 0 {
			return score + 10
		}
		return 0
	}
	if node.Narrative != "" {
		if _, matched := narrative.Memberships(entry.stock.Concepts)[node.Narrative]; matched {
			return 120
		}
		return 0
	}
	return catalogMembershipScore(entry, terms)
}

func catalogMembershipScore(entry indexedCatalogEntry, terms []normalizedCatalogTerm) int {
	best := 0
	for _, membership := range entry.memberships {
		for _, term := range terms {
			score := membershipMatchScore(membership, term)
			if score > 0 && membership == entry.industryKey {
				score += 5
			}
			best = max(best, score)
		}
	}
	return best
}

func membershipMatchScore(membership string, term normalizedCatalogTerm) int {
	if strings.EqualFold(membership, term.raw) {
		return 100
	}
	if membership == "" || term.normalized == "" {
		return 0
	}
	if membership == term.normalized {
		return 92
	}
	if len([]rune(membership)) >= 3 && len([]rune(term.normalized)) >= 3 &&
		(strings.Contains(membership, term.normalized) || strings.Contains(term.normalized, membership)) {
		return 70
	}
	return 0
}

func normalizeMembership(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return membershipNormalizer.Replace(value)
}

func (m *Mapper) hydrateLimitUpCandidates(
	ctx context.Context,
	node Node,
	catalog catalogIndex,
	events []foundation.LimitUpEvent,
	out *foundation.SectorMapNode,
) {
	if len(events) == 0 {
		return
	}
	type summary struct {
		Latest    foundation.LimitUpEvent
		FirstDate time.Time
		MaxStreak int
		Days      int
		Count     int
		Events    int
	}
	bySymbol := map[string]summary{}
	for _, event := range events {
		if !limitEventMatchesNode(event, node, catalog) {
			continue
		}
		current, exists := bySymbol[event.Symbol]
		if !exists || event.Date.After(current.Latest.Date) {
			current.Latest = event
		}
		if current.FirstDate.IsZero() || event.Date.Before(current.FirstDate) {
			current.FirstDate = event.Date
		}
		current.MaxStreak = max(current.MaxStreak, event.Streak)
		current.Days = max(current.Days, event.Days)
		current.Count = max(current.Count, event.Count)
		current.Events++
		bySymbol[event.Symbol] = current
	}
	if len(bySymbol) == 0 {
		return
	}
	symbols := make([]string, 0, len(bySymbol))
	for symbol := range bySymbol {
		symbols = append(symbols, symbol)
	}
	quotesBySymbol := map[string]foundation.Quote{}
	if m.quoteProvider != nil {
		if quotes, err := m.quoteProvider.Realtime(ctx, symbols); err == nil {
			for _, quote := range quotes {
				quotesBySymbol[quote.Symbol] = quote
			}
		}
	}
	for symbol, item := range bySymbol {
		latest := item.Latest
		stock := foundation.BoardStock{
			Symbol:         symbol,
			Name:           latest.Name,
			Price:          latest.Price,
			ChangePercent:  latest.ChangePercent,
			Amount:         latest.Amount,
			FloatMarketCap: latest.FloatMarketCap,
			LimitUpStreak:  item.MaxStreak,
			LimitUpDays:    item.Days,
			LimitUpCount:   max(item.Count, item.Events),
			FirstLimitTime: latest.FirstLimitTime,
			LastLimitTime:  latest.LastLimitTime,
			FirstLimitDate: item.FirstDate.Format("2006-01-02"),
			LastLimitDate:  latest.Date.Format("2006-01-02"),
			LimitRegime:    limitRegime(symbol),
			Meta:           latest.Meta,
		}
		if quote, ok := quotesBySymbol[symbol]; ok {
			stock.Name = quote.Name
			stock.Price = quote.Price
			stock.Change = quote.Change
			stock.ChangePercent = quote.ChangePercent
			stock.Meta = quote.Meta
		}
		out.Stocks = mergeBoardStock(out.Stocks, stock)
	}
	if out.StockSource == "" {
		out.StockSource = "eastmoney:recent-limit-up"
	} else if !strings.Contains(out.StockSource, "recent-limit-up") {
		out.StockSource += "+recent-limit-up"
	}
}

func limitEventMatchesNode(event foundation.LimitUpEvent, node Node, catalog catalogIndex) bool {
	if entry, ok := findCatalogEntry(catalog, event.Symbol); ok {
		if node.Narrative != "" {
			_, matched := narrative.Memberships(entry.stock.Concepts)[node.Narrative]
			return matched
		}
		rawTerms := append([]string{node.Name}, node.BoardKeywords...)
		terms := make([]normalizedCatalogTerm, 0, len(rawTerms))
		for _, term := range rawTerms {
			term = strings.TrimSpace(term)
			if term != "" {
				terms = append(terms, normalizedCatalogTerm{raw: term, normalized: normalizeMembership(term)})
			}
		}
		return catalogMembershipScore(entry, terms) > 0
	}
	industry := strings.TrimSpace(event.Industry)
	if industry == "" {
		return false
	}
	candidates := append([]string{node.Name}, node.BoardKeywords...)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if industry == candidate {
			return true
		}
		if len([]rune(candidate)) >= 3 && len([]rune(industry)) >= 3 && strings.Contains(industry, candidate) {
			return true
		}
	}
	return false
}

func findCatalogEntry(catalog catalogIndex, symbol string) (indexedCatalogEntry, bool) {
	for _, entry := range catalog {
		if entry.stock.Symbol == symbol {
			return entry, true
		}
	}
	return indexedCatalogEntry{}, false
}

func mergeBoardStock(stocks []foundation.BoardStock, incoming foundation.BoardStock) []foundation.BoardStock {
	for i := range stocks {
		if stocks[i].Symbol != incoming.Symbol {
			continue
		}
		current := stocks[i]
		current.Name = firstNonEmptyString(incoming.Name, current.Name)
		if incoming.Price > 0 {
			current.Price = incoming.Price
			current.Change = incoming.Change
			current.ChangePercent = incoming.ChangePercent
		}
		current.Amount = max(current.Amount, incoming.Amount)
		current.FloatMarketCap = max(current.FloatMarketCap, incoming.FloatMarketCap)
		current.LimitUpStreak = max(current.LimitUpStreak, incoming.LimitUpStreak)
		current.LimitUpDays = max(current.LimitUpDays, incoming.LimitUpDays)
		current.LimitUpCount = max(current.LimitUpCount, incoming.LimitUpCount)
		current.FirstLimitTime = firstNonEmptyString(incoming.FirstLimitTime, current.FirstLimitTime)
		current.LastLimitTime = firstNonEmptyString(incoming.LastLimitTime, current.LastLimitTime)
		current.FirstLimitDate = firstNonEmptyString(incoming.FirstLimitDate, current.FirstLimitDate)
		current.LastLimitDate = firstNonEmptyString(incoming.LastLimitDate, current.LastLimitDate)
		current.LimitRegime = firstNonEmptyString(incoming.LimitRegime, current.LimitRegime)
		if incoming.Meta.Source != "" {
			current.Meta = incoming.Meta
		}
		stocks[i] = current
		return stocks
	}
	return append(stocks, incoming)
}

func limitRegime(symbol string) string {
	code := strings.SplitN(symbol, ".", 2)[0]
	if strings.HasPrefix(code, "30") || strings.HasPrefix(code, "68") {
		return "20cm"
	}
	if strings.HasPrefix(code, "8") || strings.HasPrefix(code, "4") {
		return "30cm"
	}
	return "10cm"
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func averageChangePercent(stocks []foundation.BoardStock) float64 {
	if len(stocks) == 0 {
		return 0
	}
	total := 0.0
	count := 0
	for _, stock := range stocks {
		total += stock.ChangePercent
		count++
	}
	return total / float64(count)
}

func matchBoard(node Node, boards []foundation.Board) (foundation.Board, []string, bool) {
	if node.BoardCode != "" {
		for _, board := range boards {
			if strings.EqualFold(board.Code, node.BoardCode) {
				return board, []string{"code:" + node.BoardCode}, true
			}
		}
	}

	for _, keyword := range node.BoardKeywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		for _, board := range boards {
			if board.Name == keyword {
				return board, []string{"keyword:" + keyword}, true
			}
		}
	}
	for _, keyword := range node.BoardKeywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		for _, board := range boards {
			if strings.Contains(board.Name, keyword) {
				return board, []string{"keyword:" + keyword}, true
			}
		}
	}
	return foundation.Board{}, nil, false
}
