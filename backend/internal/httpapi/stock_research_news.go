package httpapi

import (
	"context"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/stockanalysis"
)

func selectStockResearchNews(items []foundation.NewsItem, symbol, name string, cutoff time.Time, limit int) []foundation.NewsItem {
	code := strings.Split(symbol, ".")[0]
	matches := func(text string) bool {
		return strings.Contains(text, code) || (name != "" && strings.Contains(text, name))
	}
	result := []foundation.NewsItem{}
	seen := map[string]bool{}
	for _, item := range items {
		if !matches(item.Title+" "+item.Content) || strings.TrimSpace(item.Title) == "" {
			continue
		}
		if !item.PublishedAt.IsZero() && (item.PublishedAt.After(cutoff) || item.PublishedAt.Before(cutoff.AddDate(0, 0, -60))) {
			continue
		}
		if seen[item.Title] || (item.URL != "" && seen[item.URL]) {
			continue
		}
		seen[item.Title] = true
		if item.URL != "" {
			seen[item.URL] = true
		}
		result = append(result, item)
	}
	// Company-specific stories take priority over market-wide ranking tables
	// which happen to contain the stock code in a long list.
	sort.SliceStable(result, func(i, j int) bool {
		left, right := matches(result[i].Title), matches(result[j].Title)
		if left != right {
			return left
		}
		return result[i].PublishedAt.After(result[j].PublishedAt)
	})
	return result[:min(max(limit, 0), len(result))]
}

func (s *Server) collectStockResearchNews(ctx context.Context, symbol, name, query string, cutoff time.Time) ([]foundation.NewsItem, error) {
	if s.stockNewsSearch == nil {
		return nil, nil
	}
	keyword := strings.TrimSpace(name)
	if keyword == "" {
		keyword = strings.Split(symbol, ".")[0]
	}
	if query != "" {
		// News search accepts a phrase. Preserve event/date terms instead of
		// reducing a company-prefixed query to the company name again.
		for _, identity := range []string{name, symbol, strings.Split(symbol, ".")[0]} {
			if identity != "" {
				query = strings.ReplaceAll(query, identity, " ")
			}
		}
		terms := strings.FieldsFunc(query, func(r rune) bool { return strings.ContainsRune(" \t\n，,。；;：:、（）()？?", r) })
		seen := map[string]bool{}
		for _, term := range terms {
			if !seen[term] {
				keyword += " " + term
				seen[term] = true
			}
		}
	}
	searchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	items, err := s.stockNewsSearch.SearchStockNews(searchCtx, symbol, keyword, 30)
	if err != nil {
		return nil, err
	}
	return selectStockResearchNews(items, symbol, name, cutoff, 10), nil
}

func stockNewsResearchSources(items []foundation.NewsItem, captured time.Time) []stockanalysis.ResearchSource {
	result := []stockanalysis.ResearchSource{}
	for _, item := range items {
		source := stockanalysis.NewResearchSource("news", item.Title, item.Content, item.Meta.Source, item.URL, item.PublishedAt, captured)
		source.ContentStatus = "excerpt"
		result = append(result, source)
	}
	return result
}
