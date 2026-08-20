package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

const hotStockRankLimit = 100

type hotStockEntry struct {
	Symbol         string         `json:"symbol"`
	Code           string         `json:"code"`
	Name           string         `json:"name"`
	SourceCount    int            `json:"source_count"`
	ConsensusScore int            `json:"consensus_score"`
	Ranks          map[string]int `json:"ranks"`
}

type hotStockSourceStatus struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Available bool      `json:"available"`
	Count     int       `json:"count"`
	Error     string    `json:"error,omitempty"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
}

type hotStockRankData struct {
	Stocks    []hotStockEntry        `json:"stocks"`
	Sources   []hotStockSourceStatus `json:"sources"`
	Total     int                    `json:"total"`
	UpdatedAt time.Time              `json:"updated_at"`
	ExpiresAt time.Time              `json:"expires_at"`
	Stale     bool                   `json:"stale"`
}

type hotStockRankCache struct {
	mu       sync.Mutex
	ttl      time.Duration
	snapshot hotStockRankData
}

func newHotStockRankCache(ttl time.Duration) *hotStockRankCache {
	return &hotStockRankCache{ttl: ttl}
}

func (cache *hotStockRankCache) current(force bool) (hotStockRankData, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if force || len(cache.snapshot.Stocks) == 0 || !time.Now().Before(cache.snapshot.ExpiresAt) {
		return hotStockRankData{}, false
	}
	return cloneHotStockRankData(cache.snapshot), true
}

func (cache *hotStockRankCache) latest() hotStockRankData {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cloneHotStockRankData(cache.snapshot)
}

func (cache *hotStockRankCache) store(data hotStockRankData) {
	if len(data.Stocks) == 0 {
		return
	}
	cache.mu.Lock()
	cache.snapshot = cloneHotStockRankData(data)
	cache.mu.Unlock()
}

func cloneHotStockRankData(data hotStockRankData) hotStockRankData {
	data.Stocks = append([]hotStockEntry(nil), data.Stocks...)
	for index := range data.Stocks {
		data.Stocks[index].Ranks = cloneHotStockRanks(data.Stocks[index].Ranks)
	}
	data.Sources = append([]hotStockSourceStatus(nil), data.Sources...)
	return data
}

func cloneHotStockRanks(ranks map[string]int) map[string]int {
	cloned := make(map[string]int, len(ranks))
	for source, rank := range ranks {
		cloned[source] = rank
	}
	return cloned
}

func (s *Server) hotStockRanksHandler(w http.ResponseWriter, r *http.Request) {
	if s.hotStockProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "hot stock provider is unavailable")
		return
	}
	force := r.URL.Query().Get("refresh") == "1"
	if cached, ok := s.hotStockRanks.current(force); ok {
		writeJSON(w, http.StatusOK, map[string]any{"data": cached})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	listChannel := make(chan []foundation.HotStockRankList, 1)
	directoryChannel := make(chan stockDirectoryData, 1)
	go func() {
		listChannel <- s.hotStockProvider.HotStockRanks(ctx, hotStockRankLimit)
	}()
	go func() {
		if s.stockDirectory == nil {
			directoryChannel <- stockDirectoryData{}
			return
		}
		directory, _ := s.stockDirectories.load(ctx, s.stockDirectory)
		directoryChannel <- directory
	}()

	lists := <-listChannel
	directory := <-directoryChannel
	data := buildHotStockRankData(lists, directory.Stocks, time.Now().Add(s.hotStockRanks.ttl))
	if len(data.Stocks) == 0 {
		if stale := s.hotStockRanks.latest(); len(stale.Stocks) > 0 {
			stale.Stale = true
			writeJSON(w, http.StatusOK, map[string]any{"data": stale})
			return
		}
	}
	s.hotStockRanks.store(data)
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func buildHotStockRankData(lists []foundation.HotStockRankList, directory []stockDirectoryEntry, expiresAt time.Time) hotStockRankData {
	names := make(map[string]string, len(directory))
	for _, stock := range directory {
		names[stock.Symbol] = stock.Name
	}
	stocks := make(map[string]*hotStockEntry, hotStockRankLimit*2)
	sources := make([]hotStockSourceStatus, 0, len(lists))
	updatedAt := time.Time{}
	for _, list := range lists {
		sources = append(sources, hotStockSourceStatus{
			ID: list.Source, Name: list.SourceName, Available: len(list.Items) > 0,
			Count: len(list.Items), Error: list.Error, FetchedAt: list.FetchedAt,
		})
		if list.FetchedAt.After(updatedAt) {
			updatedAt = list.FetchedAt
		}
		for _, ranked := range list.Items {
			if ranked.Rank < 1 || ranked.Rank > hotStockRankLimit {
				continue
			}
			stock := stocks[ranked.Symbol]
			if stock == nil {
				code, _, _ := strings.Cut(ranked.Symbol, ".")
				stock = &hotStockEntry{Symbol: ranked.Symbol, Code: code, Name: strings.TrimSpace(ranked.Name), Ranks: map[string]int{}}
				stocks[ranked.Symbol] = stock
			}
			if directoryName := strings.TrimSpace(names[ranked.Symbol]); directoryName != "" {
				stock.Name = directoryName
			} else if stock.Name == "" {
				stock.Name = strings.TrimSpace(ranked.Name)
			}
			if previous, exists := stock.Ranks[list.Source]; !exists || ranked.Rank < previous {
				stock.Ranks[list.Source] = ranked.Rank
			}
		}
	}

	items := make([]hotStockEntry, 0, len(stocks))
	for _, stock := range stocks {
		if stock.Name == "" {
			stock.Name = stock.Code
		}
		stock.SourceCount = len(stock.Ranks)
		for _, rank := range stock.Ranks {
			stock.ConsensusScore += hotStockRankLimit + 1 - rank
		}
		items = append(items, *stock)
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].SourceCount != items[right].SourceCount {
			return items[left].SourceCount > items[right].SourceCount
		}
		if items[left].ConsensusScore != items[right].ConsensusScore {
			return items[left].ConsensusScore > items[right].ConsensusScore
		}
		leftBest, rightBest := bestHotStockRank(items[left].Ranks), bestHotStockRank(items[right].Ranks)
		if leftBest != rightBest {
			return leftBest < rightBest
		}
		return items[left].Symbol < items[right].Symbol
	})
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	return hotStockRankData{Stocks: items, Sources: sources, Total: len(items), UpdatedAt: updatedAt, ExpiresAt: expiresAt}
}

func bestHotStockRank(ranks map[string]int) int {
	best := hotStockRankLimit + 1
	for _, rank := range ranks {
		if rank < best {
			best = rank
		}
	}
	return best
}
