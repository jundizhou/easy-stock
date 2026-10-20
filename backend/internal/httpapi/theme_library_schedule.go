package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/themelibrary"
)

// RunThemeLibraryDailyRefresh re-pulls the upstream 题材库 after the close each
// trading day. An empty refresh time disables the task. When the local snapshot
// missed the most recent slot (for example the machine was off), it refreshes once
// at startup.
func (s *Server) RunThemeLibraryDailyRefresh(ctx context.Context) {
	if s.themeLibrary == nil || strings.TrimSpace(s.themeLibraryRefreshAt) == "" {
		return
	}
	hour, minute, err := parseThemeLibraryClock(s.themeLibraryRefreshAt)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("level=warn event=scheduler_error feature=market-data task=theme_library_daily_refresh error=%q", err.Error())
		}
		return
	}
	s.logSchedulerLifecycle(ctx, "market-data", "theme_library_daily_refresh", func() {
		if last := lastThemeLibrarySlot(time.Now(), hour, minute); !last.IsZero() {
			if meta, err := s.themeLibrary.Meta(ctx); err == nil && !themeLibraryRefreshedSince(meta.GeneratedAt, last) {
				s.refreshThemeLibrary(ctx)
			}
		}
		for {
			next := nextThemeLibrarySlot(time.Now(), hour, minute)
			timer := time.NewTimer(time.Until(next))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				s.refreshThemeLibrary(ctx)
			}
		}
	})
}

func (s *Server) refreshThemeLibrary(ctx context.Context) {
	startedAt := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	client := themelibrary.NewClient(themelibrary.DefaultBaseURL)
	seed, err := themelibrary.FetchSeed(runCtx, client, themelibrary.BuildOptions{
		MinStocks: 5,
		BaseURL:   themelibrary.DefaultBaseURL,
	}, 6)
	if err == nil {
		err = s.themeLibrary.Replace(runCtx, seed)
	}
	duration := time.Since(startedAt).Milliseconds()
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("level=warn event=scheduler_error feature=market-data task=theme_library_daily_refresh duration_ms=%d error=%q", duration, err.Error())
		}
		return
	}
	if s.logger != nil {
		s.logger.Printf("level=info event=scheduler_run feature=market-data task=theme_library_daily_refresh themes=%d groups=%d duration_ms=%d", seed.ThemeCount, countThemeLibraryGroups(seed), duration)
	}
}

func countThemeLibraryGroups(seed themelibrary.Seed) int {
	total := 0
	for _, theme := range seed.Themes {
		total += len(theme.Groups)
	}
	return total
}

func parseThemeLibraryClock(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, 0, fmt.Errorf("theme library refresh time %q must be HH:mm: %w", value, err)
	}
	return parsed.Hour(), parsed.Minute(), nil
}

// nextThemeLibrarySlot returns the first trading-day slot strictly after now.
func nextThemeLibrarySlot(now time.Time, hour int, minute int) time.Time {
	for offset := 0; offset < 14; offset++ {
		day := now.AddDate(0, 0, offset)
		slot := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, now.Location())
		if slot.After(now) && foundation.IsAStockTradingDay(slot) {
			return slot
		}
	}
	return now.Add(24 * time.Hour)
}

// lastThemeLibrarySlot returns the most recent trading-day slot at or before now,
// or the zero time when none is found within two weeks.
func lastThemeLibrarySlot(now time.Time, hour int, minute int) time.Time {
	for offset := 0; offset < 14; offset++ {
		day := now.AddDate(0, 0, -offset)
		slot := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, now.Location())
		if !slot.After(now) && foundation.IsAStockTradingDay(slot) {
			return slot
		}
	}
	return time.Time{}
}

func themeLibraryRefreshedSince(generatedAt string, slot time.Time) bool {
	generated, err := time.Parse(time.RFC3339, strings.TrimSpace(generatedAt))
	if err != nil {
		return false
	}
	return !generated.Before(slot)
}
