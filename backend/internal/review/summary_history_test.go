package review

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestSummaryHistorySurvivesRestartAndPaginatesByDate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reviews.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	for _, date := range []string{"2026-10-20", "2026-10-19", "2026-10-21"} {
		if _, err := store.SaveDailySummary(ctx, DailySummary{TradeDate: date, GeneratedAt: now, ArticleCount: 5, AuthorCount: 2, ExecutiveSummary: "已保存的观点"}); err != nil {
			t.Fatal(err)
		}
	}
	// Regeneration replaces the day's result rather than creating another date.
	if _, err := store.SaveDailySummary(ctx, DailySummary{TradeDate: "2026-10-20", GeneratedAt: now.Add(time.Hour), ArticleCount: 8, AuthorCount: 3, ExecutiveSummary: "修订后的观点"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	items, total, err := store.ListDailySummaries(ctx, 2, 0)
	if err != nil || total != 3 || len(items) != 2 {
		t.Fatalf("first page: items=%+v total=%d err=%v", items, total, err)
	}
	if items[0].TradeDate != "2026-10-21" || items[1].TradeDate != "2026-10-20" || items[1].ArticleCount != 8 || items[1].AuthorCount != 3 || !items[1].GeneratedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("unexpected history: %+v", items)
	}
	items, total, err = store.ListDailySummaries(ctx, 2, 2)
	if err != nil || total != 3 || len(items) != 1 || items[0].TradeDate != "2026-10-19" {
		t.Fatalf("second page: items=%+v total=%d err=%v", items, total, err)
	}
	items, total, err = store.ListDailySummaries(ctx, 2, 3)
	if err != nil || total != 3 || items == nil || len(items) != 0 {
		t.Fatalf("empty page: items=%+v total=%d err=%v", items, total, err)
	}
	summary, err := store.GetDailySummary(ctx, "2026-10-20")
	if err != nil || summary.ExecutiveSummary != "修订后的观点" {
		t.Fatalf("persisted detail: summary=%+v err=%v", summary, err)
	}
}

func TestSummaryCacheIgnoresFreshnessLabelChanges(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	window := effectiveReviewWindow(time.Now())
	summary, err := store.SaveDailySummary(ctx, DailySummary{
		TradeDate: window.TradeDate, WindowStart: window.Start, WindowEnd: window.End,
		FreshnessRule: "昨日的窗口说明", PromptVersion: dailySummaryPromptVersion, GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveDailySummaryJob(ctx, cachedDailySummaryJob(summary)); err != nil {
		t.Fatal(err)
	}
	automation := NewAutomation(store, nil, nil, http.DefaultClient, "", nil)
	got, err := automation.GetTodaySummary(ctx)
	if err != nil || got == nil || got.TradeDate != summary.TradeDate {
		t.Fatalf("summary should survive a label change: summary=%+v err=%v", got, err)
	}
	job, err := automation.GetTodaySummaryJob(ctx)
	if err != nil || job.Status != "succeeded" || !job.SummaryAvailable {
		t.Fatalf("job should survive a label change: job=%+v err=%v", job, err)
	}
	// Generation still requires the current prompt version; history reads do not.
	summary.PromptVersion = "legacy-prompt"
	if _, err := store.SaveDailySummary(ctx, summary); err != nil {
		t.Fatal(err)
	}
	if got, err := automation.GetTodaySummary(ctx); err != nil || got != nil {
		t.Fatalf("outdated generation cache: summary=%+v err=%v", got, err)
	}
	if got, err := store.GetDailySummary(ctx, summary.TradeDate); err != nil || got.PromptVersion != "legacy-prompt" {
		t.Fatalf("history must remain readable: summary=%+v err=%v", got, err)
	}
}
