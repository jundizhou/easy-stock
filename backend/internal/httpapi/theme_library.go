package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/themelibrary"
)

func (s *Server) themeLibraryListHandler(w http.ResponseWriter, r *http.Request) {
	if s.themeLibrary == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"data": themelibrary.Library{
				Available: false,
				Message:   "题材库数据库尚未初始化",
				Items:     []themelibrary.Theme{},
			},
		})
		return
	}
	limit, err := marketLimitQuery(r, 100, 300)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 60 {
		query = string([]rune(query)[:60])
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	library, err := s.themeLibrary.List(ctx, themelibrary.Filter{
		Query: query,
		Sort:  strings.TrimSpace(r.URL.Query().Get("sort")),
		Limit: limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "题材库读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": library, "meta": themeLibraryMeta(library.Meta, nil)})
}

func (s *Server) themeLibraryDetailHandler(w http.ResponseWriter, r *http.Request) {
	if s.themeLibrary == nil {
		writeError(w, http.StatusServiceUnavailable, "题材库数据库尚未初始化")
		return
	}
	sourceID := strings.TrimSpace(r.PathValue("id"))
	if sourceID == "" {
		writeError(w, http.StatusBadRequest, "theme id is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	theme, err := s.themeLibrary.Get(ctx, sourceID)
	if errors.Is(err, themelibrary.ErrNotFound) {
		writeError(w, http.StatusNotFound, "题材不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "题材详情读取失败")
		return
	}
	meta, err := s.themeLibrary.Meta(ctx)
	if err != nil {
		meta = themelibrary.Meta{}
	}
	live := s.themeLibraryLiveQuotes(r.Context(), theme)
	writeJSON(w, http.StatusOK, map[string]any{"data": theme, "meta": themeLibraryMeta(meta, &theme), "live": live})
}

type themeLibraryLiveQuote struct {
	Price         float64   `json:"price"`
	ChangePercent float64   `json:"change_percent"`
	TradeTime     time.Time `json:"trade_time,omitempty"`
}

// themeLibraryLiveQuotes returns current quotes for the theme's members, keyed by
// the upstream stock code. Failures are silent: the caller falls back to the snapshot.
func (s *Server) themeLibraryLiveQuotes(parent context.Context, theme themelibrary.Theme) map[string]themeLibraryLiveQuote {
	if s.realtimeProvider == nil {
		return nil
	}
	codeByCanonical := map[string]string{}
	symbols := []string{}
	for _, group := range theme.Groups {
		for _, stock := range group.Stocks {
			normalized, err := foundation.NormalizeSymbol(stock.Code)
			if err != nil {
				continue
			}
			if _, ok := codeByCanonical[normalized.Canonical]; ok {
				continue
			}
			codeByCanonical[normalized.Canonical] = stock.Code
			symbols = append(symbols, stock.Code)
		}
	}
	if len(symbols) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 6*time.Second)
	defer cancel()
	live := map[string]themeLibraryLiveQuote{}
	const batchSize = 100
	for start := 0; start < len(symbols); start += batchSize {
		end := min(start+batchSize, len(symbols))
		quotes, err := s.realtimeProvider.Realtime(ctx, symbols[start:end])
		if err != nil {
			continue
		}
		for _, quote := range quotes {
			normalized, err := foundation.NormalizeSymbol(quote.Symbol)
			if err != nil {
				continue
			}
			code, ok := codeByCanonical[normalized.Canonical]
			if !ok || quote.Price <= 0 {
				continue
			}
			live[code] = themeLibraryLiveQuote{
				Price:         quote.Price,
				ChangePercent: quote.ChangePercent,
				TradeTime:     quote.TradeTime,
			}
		}
	}
	return live
}

func themeLibraryMeta(meta themelibrary.Meta, theme *themelibrary.Theme) foundation.SourceMeta {
	fetchedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(meta.GeneratedAt))
	if err != nil {
		fetchedAt = time.Now().UTC()
	}
	tradeDate := strings.TrimSpace(meta.SourceUpdatedAt)
	if len(tradeDate) > 10 {
		tradeDate = tradeDate[:10]
	}
	if theme != nil && strings.TrimSpace(theme.AsOf) != "" {
		tradeDate = strings.TrimSpace(theme.AsOf)
	}
	if len(tradeDate) > 10 {
		tradeDate = tradeDate[:10]
	}
	sourceURL := strings.TrimSpace(meta.SourceURL)
	if sourceURL == "" {
		sourceURL = themelibrary.LibraryPageURL
	}
	return foundation.SourceMeta{
		Source:          "bigamap",
		SourceURL:       sourceURL,
		AvailableFields: []string{"themes", "groups", "stocks", "introduction"},
		FetchedAt:       fetchedAt,
		Stale:           true,
		TradeDate:       tradeDate,
		SnapshotID:      meta.GeneratedAt,
		FallbackReason:  "静态题材库快照：题材与成员来自开盘啦公开题材库，价格和涨跌为更新时点数据",
	}
}
