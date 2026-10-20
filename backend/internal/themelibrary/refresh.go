package themelibrary

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// BuildOptions controls which upstream themes become active in a snapshot.
type BuildOptions struct {
	// Limit keeps the hottest N active themes; 0 keeps every eligible theme.
	Limit int
	// MinStocks is the minimum upstream stock count for a theme to be active.
	MinStocks int
	// BaseURL is the upstream base used for the snapshot's api_url metadata.
	BaseURL string
}

// FlattenGroups walks the two-level category tree and keeps every node that
// directly owns stocks. Names carry the full path because the same leaf label
// (for example 光模块) can appear under different parents. Some upstream
// themes have no category tree and only expose their members at the top level;
// those members are kept in a single fallback group instead of being dropped.
func FlattenGroups(categories []RawCategory, topLevel []RawStock) []Group {
	groups := make([]Group, 0, len(categories)+1)
	categoryCodes := make(map[string]struct{})
	var walk func(category RawCategory, parentPath []string, parentID string, parentName string)
	walk = func(category RawCategory, parentPath []string, parentID string, parentName string) {
		currentPath := append(append([]string{}, parentPath...), category.Name)
		if len(category.Stocks) > 0 {
			stocks := make([]Stock, 0, len(category.Stocks))
			seen := make(map[string]struct{}, len(category.Stocks))
			for _, stock := range category.Stocks {
				if _, ok := seen[stock.Code]; ok {
					continue
				}
				seen[stock.Code] = struct{}{}
				categoryCodes[stock.Code] = struct{}{}
				stocks = append(stocks, convertRawStock(stock))
			}
			groups = append(groups, Group{
				SourceID:       category.ID,
				Name:           strings.Join(currentPath, " · "),
				ParentSourceID: parentID,
				ParentName:     parentName,
				Path:           currentPath,
				ChangePercent:  category.ChangePercent,
				LimitUpCount:   category.LimitUpCount,
				StockCount:     len(stocks),
				Stocks:         stocks,
			})
		}
		for _, child := range category.Children {
			walk(child, currentPath, category.ID, category.Name)
		}
	}
	for _, category := range categories {
		walk(category, nil, "", "")
	}

	extras := make([]Stock, 0, len(topLevel))
	seen := make(map[string]struct{}, len(topLevel))
	for _, stock := range topLevel {
		if _, ok := categoryCodes[stock.Code]; ok {
			continue
		}
		if _, ok := seen[stock.Code]; ok {
			continue
		}
		seen[stock.Code] = struct{}{}
		extras = append(extras, convertRawStock(stock))
	}
	if len(extras) > 0 {
		name := "其他成员"
		sourceID := "top-level-other"
		if len(groups) == 0 {
			name = "全部成员"
			sourceID = "top-level"
		}
		groups = append(groups, Group{
			SourceID:   sourceID,
			Name:       name,
			Path:       []string{name},
			StockCount: len(extras),
			Stocks:     extras,
		})
	}
	return groups
}

func convertRawStock(stock RawStock) Stock {
	return Stock{
		Code:          stock.Code,
		Name:          stock.Name,
		Price:         stock.Price,
		ChangePercent: stock.ChangePercent,
		IsLimitUp:     stock.IsLimitUp,
		Hot:           stock.Hot,
		Tags:          stock.Tags,
		Description:   SanitizeHTML(stock.Description),
	}
}

// BuildSeed selects active themes from an upstream list and their details and
// flattens them into the local snapshot shape.
func BuildSeed(list RawList, details map[string]RawTheme, opts BuildOptions) Seed {
	selected := make([]RawListItem, 0, len(list.Items))
	for _, item := range list.Items {
		if item.StockCount < opts.MinStocks {
			continue
		}
		if _, ok := details[item.ID]; !ok {
			continue
		}
		selected = append(selected, item)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].Hot != selected[j].Hot {
			return selected[i].Hot > selected[j].Hot
		}
		if selected[i].UpdatedAt != selected[j].UpdatedAt {
			return selected[i].UpdatedAt > selected[j].UpdatedAt
		}
		return selected[i].Name < selected[j].Name
	})
	if opts.Limit > 0 && len(selected) > opts.Limit {
		selected = selected[:opts.Limit]
	}

	themes := make([]Theme, 0, len(selected))
	for _, item := range selected {
		detail := details[item.ID]
		groups := FlattenGroups(detail.Categories, detail.Stocks)
		description := SanitizeHTML(item.Description)
		if description == "" {
			description = SanitizeHTML(detail.Description)
		}
		themes = append(themes, Theme{
			SourceID:       item.ID,
			Name:           item.Name,
			Description:    description,
			Introduction:   SanitizeHTML(detail.Introduction),
			ChangePercent:  item.ChangePercent,
			StockCount:     item.StockCount,
			LimitUpCount:   item.LimitUpCount,
			RisingCount:    item.RisingCount,
			FallingCount:   item.FallingCount,
			FlatCount:      item.FlatCount,
			Hot:            item.Hot,
			ThemeUpdatedAt: item.UpdatedAt,
			AsOf:           list.UpdatedAt,
			SourceURL:      LibraryPageURL,
			SourceAPIURL:   DetailURL(item.ID),
			Groups:         groups,
		})
	}

	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	selection := fmt.Sprintf("按热度取前 %d 个题材（股票数不少于 %d；同一题材内跨细分重复的标的保留在各自细分中）", len(themes), opts.MinStocks)
	if opts.Limit == 0 {
		selection = fmt.Sprintf("导入全部题材（股票数不少于 %d；同一题材内跨细分重复的标的保留在各自细分中）", opts.MinStocks)
	}
	return Seed{
		SchemaVersion:   1,
		Source:          "bigamap",
		SourceName:      "开盘啦题材库",
		SourceURL:       LibraryPageURL,
		APIURL:          baseURL + "/api/v1/public/theme-library",
		SourceUpdatedAt: list.UpdatedAt,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		Selection:       selection,
		ThemeCount:      len(themes),
		Themes:          themes,
	}
}

// FetchSeed pulls the upstream list and every theme detail, then builds a
// snapshot. Failed detail requests are retried once; if any still fail the
// snapshot is not built, so a partial upstream never shrinks the local library.
func FetchSeed(ctx context.Context, client *Client, opts BuildOptions, concurrency int) (Seed, error) {
	list, err := client.List(ctx)
	if err != nil {
		return Seed{}, fmt.Errorf("fetch theme list: %w", err)
	}
	if len(list.Items) == 0 {
		return Seed{}, fmt.Errorf("theme list is empty")
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	details := make(map[string]RawTheme, len(list.Items))
	pending := make([]RawListItem, 0, len(list.Items))
	for _, item := range list.Items {
		if item.StockCount >= opts.MinStocks {
			pending = append(pending, item)
		}
	}
	for attempt := 0; attempt < 2 && len(pending) > 0; attempt++ {
		fetched, failed := fetchDetails(ctx, client, pending, concurrency)
		for id, detail := range fetched {
			details[id] = detail
		}
		pending = failed
		if err := ctx.Err(); err != nil {
			return Seed{}, err
		}
	}
	if len(pending) > 0 {
		return Seed{}, fmt.Errorf("%d theme details failed after retry; keeping previous snapshot", len(pending))
	}
	seed := BuildSeed(list, details, opts)
	if seed.ThemeCount == 0 {
		return Seed{}, fmt.Errorf("no themes selected from upstream list")
	}
	return seed, nil
}

func fetchDetails(ctx context.Context, client *Client, items []RawListItem, concurrency int) (map[string]RawTheme, []RawListItem) {
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		fetched = make(map[string]RawTheme, len(items))
		failed  = make([]RawListItem, 0)
		queue   = make(chan RawListItem)
	)
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range queue {
				requestCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
				detail, err := client.Detail(requestCtx, item.ID)
				cancel()
				mu.Lock()
				if err != nil {
					failed = append(failed, item)
				} else {
					fetched[item.ID] = detail
				}
				mu.Unlock()
			}
		}()
	}
	for _, item := range items {
		queue <- item
	}
	close(queue)
	wg.Wait()
	return fetched, failed
}
