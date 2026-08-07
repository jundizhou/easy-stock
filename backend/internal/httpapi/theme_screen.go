package httpapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

type themeSnapshot struct {
	sectorMap foundation.SectorMap
	id        string
	expiresAt time.Time
}

type themeSnapshotFlight struct {
	done     chan struct{}
	snapshot themeSnapshot
	err      error
}

type themeSnapshotCache struct {
	mu       sync.Mutex
	ttl      time.Duration
	items    map[string]themeSnapshot
	inflight map[string]*themeSnapshotFlight
}

func newThemeSnapshotCache(ttl time.Duration) *themeSnapshotCache {
	return &themeSnapshotCache{
		ttl:      ttl,
		items:    map[string]themeSnapshot{},
		inflight: map[string]*themeSnapshotFlight{},
	}
}

func (c *themeSnapshotCache) load(
	ctx context.Context,
	key string,
	loader func(context.Context) (foundation.SectorMap, error),
) (themeSnapshot, error) {
	now := time.Now()
	c.mu.Lock()
	if cached, ok := c.items[key]; ok && now.Before(cached.expiresAt) {
		c.mu.Unlock()
		return cached, nil
	}
	if flight, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-flight.done:
			return flight.snapshot, flight.err
		case <-ctx.Done():
			return themeSnapshot{}, ctx.Err()
		}
	}
	flight := &themeSnapshotFlight{done: make(chan struct{})}
	c.inflight[key] = flight
	c.mu.Unlock()

	sectorMap, err := loader(ctx)
	if err == nil {
		fetchedAt := sectorMap.Meta.FetchedAt
		if fetchedAt.IsZero() {
			fetchedAt = time.Now()
		}
		flight.snapshot = themeSnapshot{
			sectorMap: sectorMap,
			id:        fmt.Sprintf("%s-%d", key, fetchedAt.UnixMilli()),
			expiresAt: time.Now().Add(c.ttl),
		}
	}
	flight.err = err

	c.mu.Lock()
	delete(c.inflight, key)
	if err == nil {
		c.items[key] = flight.snapshot
	}
	close(flight.done)
	c.mu.Unlock()
	return flight.snapshot, flight.err
}

type themeScreenPagination struct {
	Page       int  `json:"page"`
	PageSize   int  `json:"page_size"`
	Total      int  `json:"total"`
	TotalPages int  `json:"total_pages"`
	HasMore    bool `json:"has_more"`
}

type themeScreenData struct {
	Map        foundation.SectorMap  `json:"map"`
	Pagination themeScreenPagination `json:"pagination"`
	SnapshotID string                `json:"snapshot_id"`
	Sort       string                `json:"sort"`
}

type themeCandidate struct {
	Stock      foundation.BoardStock
	NodeIDs    []string
	NodeNames  []string
	Occurrence int
	RankScore  int
	RankRole   string
}

func (s *Server) themeScreenHandler(w http.ResponseWriter, r *http.Request) {
	if s.sectorMap == nil {
		writeError(w, http.StatusServiceUnavailable, "sector map provider is unavailable")
		return
	}
	theme := strings.TrimSpace(r.URL.Query().Get("theme"))
	if theme == "" {
		theme = "semiconductor_materials"
	}
	page, err := positiveIntQuery(r, "page", 1, 10_000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pageSize, err := positiveIntQuery(r, "page_size", 20, 50)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sortBy := firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("sort")), "rank_score")
	if !validThemeSort(sortBy) {
		writeError(w, http.StatusBadRequest, "sort must be rank_score, change_percent, amount, or limit_up_streak")
		return
	}
	lane := firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("lane")), "all")
	if lane != "all" && lane != "10cm" && lane != "20cm" && lane != "30cm" {
		writeError(w, http.StatusBadRequest, "lane must be all, 10cm, 20cm, or 30cm")
		return
	}
	nodeFilter := strings.TrimSpace(r.URL.Query().Get("node"))
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	snapshotID := strings.TrimSpace(r.URL.Query().Get("snapshot_id"))

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cacheKey := theme
	if snapshotID != "" {
		cacheKey += "@" + snapshotID
	}
	snapshot, err := s.themeSnapshots.load(ctx, cacheKey, func(loadCtx context.Context) (foundation.SectorMap, error) {
		if provider, ok := s.sectorMap.(SnapshotSectorMapProvider); ok {
			return provider.BuildSnapshot(loadCtx, theme, snapshotID)
		}
		return s.sectorMap.Build(loadCtx, theme)
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	allCandidates := rankThemeCandidates(flattenThemeCandidates(snapshot.sectorMap))
	filtered := filterThemeCandidates(allCandidates, nodeFilter, lane, query)
	sortThemeCandidates(filtered, sortBy)
	total := len(filtered)
	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := min(start+pageSize, total)
	pageCandidates := filtered[start:end]

	data := themeScreenData{
		Map:        trimSectorMap(snapshot.sectorMap, allCandidates, pageCandidates),
		Pagination: themeScreenPagination{Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages, HasMore: end < total},
		SnapshotID: snapshot.id,
		Sort:       sortBy,
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func positiveIntQuery(r *http.Request, name string, fallback int, maximum int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > maximum {
		return 0, fmt.Errorf("%s must be between 1 and %d", name, maximum)
	}
	return value, nil
}

func validThemeSort(value string) bool {
	switch value {
	case "rank_score", "change_percent", "amount", "limit_up_streak":
		return true
	default:
		return false
	}
}

func flattenThemeCandidates(sectorMap foundation.SectorMap) []themeCandidate {
	bySymbol := map[string]*themeCandidate{}
	for _, group := range sectorMap.Groups {
		for _, node := range group.Nodes {
			for _, stock := range node.Stocks {
				candidate, exists := bySymbol[stock.Symbol]
				if !exists {
					copy := themeCandidate{Stock: stock}
					candidate = &copy
					bySymbol[stock.Symbol] = candidate
				} else {
					candidate.Stock = mergeThemeStock(candidate.Stock, stock)
				}
				if !containsString(candidate.NodeIDs, node.ID) {
					candidate.NodeIDs = append(candidate.NodeIDs, node.ID)
					candidate.NodeNames = append(candidate.NodeNames, node.Name)
					candidate.Occurrence++
				}
			}
		}
	}
	items := make([]themeCandidate, 0, len(bySymbol))
	for _, candidate := range bySymbol {
		items = append(items, *candidate)
	}
	return items
}

func mergeThemeStock(current foundation.BoardStock, incoming foundation.BoardStock) foundation.BoardStock {
	if incoming.Name != "" {
		current.Name = incoming.Name
	}
	if incoming.Price > 0 {
		current.Price = incoming.Price
		current.Change = incoming.Change
		current.ChangePercent = incoming.ChangePercent
	}
	current.Volume = max(current.Volume, incoming.Volume)
	current.Amount = max(current.Amount, incoming.Amount)
	current.TotalMarketCap = max(current.TotalMarketCap, incoming.TotalMarketCap)
	current.FloatMarketCap = max(current.FloatMarketCap, incoming.FloatMarketCap)
	if math.Abs(incoming.MainNetInflow) > math.Abs(current.MainNetInflow) {
		current.MainNetInflow = incoming.MainNetInflow
	}
	current.LimitUpStreak = max(current.LimitUpStreak, incoming.LimitUpStreak)
	current.LimitUpDays = max(current.LimitUpDays, incoming.LimitUpDays)
	current.LimitUpCount = max(current.LimitUpCount, incoming.LimitUpCount)
	current.FirstLimitTime = firstNonEmpty(incoming.FirstLimitTime, current.FirstLimitTime)
	current.LastLimitTime = firstNonEmpty(incoming.LastLimitTime, current.LastLimitTime)
	current.FirstLimitDate = firstNonEmpty(incoming.FirstLimitDate, current.FirstLimitDate)
	current.LastLimitDate = firstNonEmpty(incoming.LastLimitDate, current.LastLimitDate)
	current.LimitRegime = firstNonEmpty(incoming.LimitRegime, current.LimitRegime)
	if incoming.Meta.Source != "" {
		current.Meta = incoming.Meta
	}
	return current
}

func rankThemeCandidates(items []themeCandidate) []themeCandidate {
	amounts := make([]float64, 0, len(items))
	changesByLane := map[string][]float64{}
	streaksByLane := map[string][]float64{}
	for i := range items {
		lane := stockLimitRegime(items[i].Stock.Symbol)
		items[i].Stock.LimitRegime = lane
		amounts = append(amounts, items[i].Stock.Amount)
		changesByLane[lane] = append(changesByLane[lane], items[i].Stock.ChangePercent)
		streaksByLane[lane] = append(streaksByLane[lane], float64(items[i].Stock.LimitUpStreak))
	}
	sort.Float64s(amounts)
	for lane := range changesByLane {
		sort.Float64s(changesByLane[lane])
		sort.Float64s(streaksByLane[lane])
	}
	for i := range items {
		stock := items[i].Stock
		sourceScore := stock.RankScore
		sourceRole := stock.RankRole
		lane := stock.LimitRegime
		attention := percentileValue(amounts, stock.Amount)
		strength := percentileValue(changesByLane[lane], stock.ChangePercent)
		streakRank := percentileValue(streaksByLane[lane], float64(stock.LimitUpStreak))
		height := math.Min(100, float64(stock.LimitUpStreak*24+stock.LimitUpDays*7+stock.LimitUpCount*3))
		if stock.LimitUpStreak == 0 {
			height = math.Min(55, 18+math.Max(0, stock.ChangePercent)*2.5)
		}
		centrality := math.Min(100, float64(items[i].Occurrence*28))
		score := int(math.Round(height*0.38 + streakRank*0.12 + strength*0.2 + attention*0.18 + centrality*0.12))
		score = min(max(score, 0), 100)
		role := basicRankRole(stock, score, attention, strength, items[i].Occurrence)
		if sourceScore > 0 {
			score = max(score, sourceScore)
			if sourceRole != "" {
				role = sourceRole
			}
		}
		items[i].RankScore = score
		items[i].RankRole = role
		items[i].Stock.RankScore = score
		items[i].Stock.RankRole = role
	}
	return items
}

func basicRankRole(stock foundation.BoardStock, score int, attention float64, strength float64, occurrence int) string {
	if stock.ChangePercent < -3 && score < 40 {
		return "掉队"
	}
	if stock.LimitUpStreak >= 2 || (stock.LimitUpStreak >= 1 && score >= 68) {
		return "高度龙头候选"
	}
	if score >= 70 && attention >= 70 {
		return "容量核心候选"
	}
	if score >= 62 || occurrence >= 3 {
		return "核心候选"
	}
	if stock.ChangePercent >= 3 && strength >= 65 {
		return "补涨候选"
	}
	if score >= 44 {
		return "中位跟随"
	}
	return "低位观察"
}

func percentileValue(sorted []float64, value float64) float64 {
	if len(sorted) <= 1 {
		return 50
	}
	index := sort.Search(len(sorted), func(i int) bool { return sorted[i] >= value })
	return float64(index) / float64(len(sorted)-1) * 100
}

func filterThemeCandidates(items []themeCandidate, node string, lane string, query string) []themeCandidate {
	filtered := make([]themeCandidate, 0, len(items))
	for _, item := range items {
		if node != "" && node != "all" && !containsString(item.NodeIDs, node) && !containsString(item.NodeNames, node) {
			continue
		}
		if lane != "all" && item.Stock.LimitRegime != lane {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(item.Stock.Symbol + " " + item.Stock.Name + " " + strings.Join(item.NodeNames, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func sortThemeCandidates(items []themeCandidate, sortBy string) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch sortBy {
		case "change_percent":
			if a.Stock.ChangePercent != b.Stock.ChangePercent {
				return a.Stock.ChangePercent > b.Stock.ChangePercent
			}
		case "amount":
			if a.Stock.Amount != b.Stock.Amount {
				return a.Stock.Amount > b.Stock.Amount
			}
		case "limit_up_streak":
			if a.Stock.LimitUpStreak != b.Stock.LimitUpStreak {
				return a.Stock.LimitUpStreak > b.Stock.LimitUpStreak
			}
		default:
			aLeaderRank := kaipanlaLeaderRank(a.Stock.RankRole)
			bLeaderRank := kaipanlaLeaderRank(b.Stock.RankRole)
			if aLeaderRank > 0 || bLeaderRank > 0 {
				if aLeaderRank == 0 {
					return false
				}
				if bLeaderRank == 0 {
					return true
				}
				if aLeaderRank != bLeaderRank {
					return aLeaderRank < bLeaderRank
				}
			}
			if a.RankScore != b.RankScore {
				return a.RankScore > b.RankScore
			}
		}
		if a.RankScore != b.RankScore {
			return a.RankScore > b.RankScore
		}
		if a.Stock.ChangePercent != b.Stock.ChangePercent {
			return a.Stock.ChangePercent > b.Stock.ChangePercent
		}
		return a.Stock.Symbol < b.Stock.Symbol
	})
}

func kaipanlaLeaderRank(role string) int {
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(role), "龙"))
	switch value {
	case "一":
		return 1
	case "二":
		return 2
	case "三":
		return 3
	case "四":
		return 4
	case "五":
		return 5
	}
	rank, err := strconv.Atoi(value)
	if err != nil || rank < 1 || rank > 5 {
		return 0
	}
	return rank
}

func trimSectorMap(full foundation.SectorMap, all []themeCandidate, page []themeCandidate) foundation.SectorMap {
	pageStocks := make(map[string]foundation.BoardStock, len(page))
	for _, candidate := range page {
		pageStocks[candidate.Stock.Symbol] = candidate.Stock
	}
	counts := map[string]int{}
	for _, candidate := range all {
		for _, nodeID := range candidate.NodeIDs {
			counts[nodeID]++
		}
	}

	trimmed := full
	trimmed.Groups = make([]foundation.SectorMapGroup, 0, len(full.Groups))
	for _, group := range full.Groups {
		outGroup := group
		outGroup.Nodes = make([]foundation.SectorMapNode, 0, len(group.Nodes))
		for _, node := range group.Nodes {
			outNode := node
			outNode.CandidateCount = counts[node.ID]
			outNode.Stocks = make([]foundation.BoardStock, 0, len(page))
			for _, original := range node.Stocks {
				if stock, ok := pageStocks[original.Symbol]; ok {
					outNode.Stocks = append(outNode.Stocks, stock)
				}
			}
			outGroup.Nodes = append(outGroup.Nodes, outNode)
		}
		trimmed.Groups = append(trimmed.Groups, outGroup)
	}
	return trimmed
}

func stockLimitRegime(symbol string) string {
	code := strings.SplitN(symbol, ".", 2)[0]
	if strings.HasPrefix(code, "30") || strings.HasPrefix(code, "68") {
		return "20cm"
	}
	if strings.HasPrefix(code, "8") || strings.HasPrefix(code, "4") || strings.HasPrefix(code, "92") {
		return "30cm"
	}
	return "10cm"
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
