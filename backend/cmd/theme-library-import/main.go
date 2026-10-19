// Command theme-library-import builds the independent 题材库 database from the
// public BigAMap / 开盘啦 theme library API.
//
// The raw responses are archived first, then a curated snapshot is written to
// backend/internal/themelibrary/seed.json (embedded into the backend) and to an
// optional SQLite file for inspection. The server never fetches upstream at
// runtime for this snapshot; the daily refresh reuses themelibrary.BuildSeed.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/themelibrary"
)

type options struct {
	baseURL     string
	rawDir      string
	seedPath    string
	dbPath      string
	limit       int
	minStocks   int
	concurrency int
	refresh     bool
}

func main() {
	var opts options
	flag.StringVar(&opts.baseURL, "base-url", themelibrary.DefaultBaseURL, "BigAMap base URL")
	flag.StringVar(&opts.rawDir, "raw-dir", filepath.Join("backend", "data", "theme-library", "raw"), "directory for the archived raw JSON")
	flag.StringVar(&opts.seedPath, "seed", filepath.Join("backend", "internal", "themelibrary", "seed.json"), "embedded seed JSON output")
	flag.StringVar(&opts.dbPath, "db", filepath.Join("backend", "data", "theme-library.db"), "optional SQLite output (empty to skip)")
	flag.IntVar(&opts.limit, "limit", 50, "number of active themes to import; 0 imports every theme")
	flag.IntVar(&opts.minStocks, "min-stocks", 5, "minimum upstream stock count for a theme to be considered active")
	flag.IntVar(&opts.concurrency, "concurrency", 6, "parallel detail requests")
	flag.BoolVar(&opts.refresh, "refresh", false, "re-fetch every detail even when the raw archive already exists")
	flag.Parse()
	if opts.concurrency <= 0 {
		opts.concurrency = 1
	}
	if err := run(context.Background(), opts); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, opts options) error {
	if err := os.MkdirAll(filepath.Join(opts.rawDir, "themes"), 0o700); err != nil {
		return fmt.Errorf("create raw archive: %w", err)
	}
	client := themelibrary.NewClient(opts.baseURL)

	list, err := loadList(ctx, client, opts)
	if err != nil {
		return err
	}
	fmt.Printf("list: %d themes, upstream updatedAt=%s\n", len(list.Items), list.UpdatedAt)

	details, failures, err := loadDetails(ctx, client, opts, list.Items)
	if err != nil {
		return err
	}
	fmt.Printf("details: %d available, %d failed\n", len(details), len(failures))

	seed := themelibrary.BuildSeed(list, details, themelibrary.BuildOptions{
		Limit:     opts.limit,
		MinStocks: opts.minStocks,
		BaseURL:   opts.baseURL,
	})
	if seed.ThemeCount == 0 {
		return fmt.Errorf("no themes selected; check -min-stocks and the raw archive")
	}
	if err := writeJSON(opts.seedPath, seed); err != nil {
		return err
	}
	seedInfo, _ := os.Stat(opts.seedPath)
	fmt.Printf("seed: %d themes -> %s (%s)\n", seed.ThemeCount, opts.seedPath, humanBytes(seedInfo.Size()))

	if strings.TrimSpace(opts.dbPath) != "" {
		store, err := themelibrary.OpenWithSeed(opts.dbPath, seed)
		if err != nil {
			return err
		}
		if err := store.Close(); err != nil {
			return err
		}
		dbInfo, _ := os.Stat(opts.dbPath)
		fmt.Printf("database: %s (%s)\n", opts.dbPath, humanBytes(dbInfo.Size()))
	}

	stats := seedStats(seed)
	fmt.Printf("selection: %d themes, %d groups, %d memberships, %d unique stocks\n", stats.themes, stats.groups, stats.relations, stats.uniqueStocks)
	if len(failures) > 0 {
		fmt.Printf("warning: %d detail requests failed; selection skipped those themes\n", len(failures))
	}
	return nil
}

func loadList(ctx context.Context, client *themelibrary.Client, opts options) (themelibrary.RawList, error) {
	path := filepath.Join(opts.rawDir, "list.json")
	if !opts.refresh {
		var cached themelibrary.RawList
		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &cached) == nil && len(cached.Items) > 0 {
			return cached, nil
		}
	}
	list, err := client.List(ctx)
	if err != nil {
		return themelibrary.RawList{}, fmt.Errorf("fetch theme list: %w", err)
	}
	if len(list.Items) == 0 {
		return themelibrary.RawList{}, fmt.Errorf("theme list is empty")
	}
	if err := writeJSON(path, list); err != nil {
		return themelibrary.RawList{}, err
	}
	return list, nil
}

type detailResult struct {
	id     string
	detail themelibrary.RawTheme
	err    error
}

func loadDetails(ctx context.Context, client *themelibrary.Client, opts options, items []themelibrary.RawListItem) (map[string]themelibrary.RawTheme, []string, error) {
	details := make(map[string]themelibrary.RawTheme, len(items))
	failures := make([]string, 0)
	queue := make(chan themelibrary.RawListItem)
	results := make(chan detailResult)
	var wg sync.WaitGroup
	for worker := 0; worker < opts.concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range queue {
				path := filepath.Join(opts.rawDir, "themes", item.ID+".json")
				if !opts.refresh {
					var cached themelibrary.RawTheme
					if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &cached) == nil && cached.ID != "" {
						results <- detailResult{id: item.ID, detail: cached}
						continue
					}
				}
				requestCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
				detail, err := client.Detail(requestCtx, item.ID)
				cancel()
				if err == nil {
					err = writeJSON(path, detail)
				}
				results <- detailResult{id: item.ID, detail: detail, err: err}
			}
		}()
	}
	go func() {
		for _, item := range items {
			queue <- item
		}
		close(queue)
		wg.Wait()
		close(results)
	}()
	for result := range results {
		if result.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", result.id, result.err))
			continue
		}
		details[result.id] = result.detail
	}
	manifest := map[string]any{
		"source":       client.BaseURL + "/api/v1/public/theme-library",
		"fetched_at":   time.Now().UTC().Format(time.RFC3339),
		"item_count":   len(items),
		"detail_count": len(details),
		"failures":     failures,
	}
	if err := writeJSON(filepath.Join(opts.rawDir, "manifest.json"), manifest); err != nil {
		return nil, nil, err
	}
	return details, failures, nil
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

type seedStatsValue struct {
	themes       int
	groups       int
	relations    int
	uniqueStocks int
}

func seedStats(seed themelibrary.Seed) seedStatsValue {
	stats := seedStatsValue{themes: len(seed.Themes)}
	seen := make(map[string]struct{})
	for _, theme := range seed.Themes {
		stats.groups += len(theme.Groups)
		for _, group := range theme.Groups {
			stats.relations += len(group.Stocks)
			for _, stock := range group.Stocks {
				seen[stock.Code] = struct{}{}
			}
		}
	}
	stats.uniqueStocks = len(seen)
	return stats
}

func humanBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}
