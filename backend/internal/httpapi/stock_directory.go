package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

type stockDirectoryEntry struct {
	Symbol string `json:"symbol"`
	Code   string `json:"code"`
	Name   string `json:"name"`
}

type stockDirectoryData struct {
	Stocks    []stockDirectoryEntry `json:"stocks"`
	Total     int                   `json:"total"`
	Source    string                `json:"source"`
	UpdatedAt time.Time             `json:"updated_at"`
	ExpiresAt time.Time             `json:"expires_at"`
	Stale     bool                  `json:"stale"`
}

type stockDirectorySnapshot struct {
	data      stockDirectoryData
	expiresAt time.Time
}

type stockDirectoryFlight struct {
	done chan struct{}
	data stockDirectoryData
	err  error
}

type stockDirectoryCache struct {
	mu       sync.Mutex
	ttl      time.Duration
	snapshot stockDirectorySnapshot
	inflight *stockDirectoryFlight
}

func newStockDirectoryCache(ttl time.Duration) *stockDirectoryCache {
	return &stockDirectoryCache{ttl: ttl}
}

func (c *stockDirectoryCache) load(ctx context.Context, provider StockDirectoryProvider) (stockDirectoryData, error) {
	now := time.Now()
	c.mu.Lock()
	if !c.snapshot.expiresAt.IsZero() && now.Before(c.snapshot.expiresAt) {
		data := cloneStockDirectoryData(c.snapshot.data)
		c.mu.Unlock()
		return data, nil
	}
	if c.inflight != nil {
		flight := c.inflight
		c.mu.Unlock()
		select {
		case <-flight.done:
			return cloneStockDirectoryData(flight.data), flight.err
		case <-ctx.Done():
			return stockDirectoryData{}, ctx.Err()
		}
	}
	flight := &stockDirectoryFlight{done: make(chan struct{})}
	c.inflight = flight
	stale := cloneStockDirectoryData(c.snapshot.data)
	c.mu.Unlock()

	catalog, err := provider.StockCatalog(ctx)
	if err == nil {
		flight.data = buildStockDirectory(catalog, time.Now().Add(c.ttl))
		if len(flight.data.Stocks) == 0 {
			err = errors.New("stock directory returned no stocks")
		}
	}
	if err != nil && len(stale.Stocks) > 0 {
		stale.Stale = true
		flight.data = stale
		err = nil
	}
	flight.err = err

	c.mu.Lock()
	c.inflight = nil
	if err == nil && !flight.data.Stale {
		c.snapshot = stockDirectorySnapshot{data: cloneStockDirectoryData(flight.data), expiresAt: flight.data.ExpiresAt}
	}
	close(flight.done)
	c.mu.Unlock()
	return cloneStockDirectoryData(flight.data), flight.err
}

func buildStockDirectory(catalog []foundation.StockCatalogEntry, expiresAt time.Time) stockDirectoryData {
	entries := make([]stockDirectoryEntry, 0, len(catalog))
	seen := make(map[string]struct{}, len(catalog))
	updatedAt := time.Time{}
	source := ""
	for _, item := range catalog {
		symbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
		name := strings.TrimSpace(item.Name)
		if symbol == "" || name == "" {
			continue
		}
		if _, exists := seen[symbol]; exists {
			continue
		}
		seen[symbol] = struct{}{}
		code, _, _ := strings.Cut(symbol, ".")
		entries = append(entries, stockDirectoryEntry{Symbol: symbol, Code: code, Name: name})
		if item.Meta.FetchedAt.After(updatedAt) {
			updatedAt = item.Meta.FetchedAt
		}
		if source == "" {
			source = item.Meta.Source
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Code == entries[j].Code {
			return entries[i].Symbol < entries[j].Symbol
		}
		return entries[i].Code < entries[j].Code
	})
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	return stockDirectoryData{
		Stocks: entries, Total: len(entries), Source: source,
		UpdatedAt: updatedAt, ExpiresAt: expiresAt,
	}
}

func cloneStockDirectoryData(data stockDirectoryData) stockDirectoryData {
	data.Stocks = append([]stockDirectoryEntry(nil), data.Stocks...)
	return data
}

func (s *Server) stockDirectoryHandler(w http.ResponseWriter, r *http.Request) {
	if s.stockDirectory == nil {
		writeError(w, http.StatusServiceUnavailable, "stock directory provider is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	data, err := s.stockDirectories.load(ctx, s.stockDirectory)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}
