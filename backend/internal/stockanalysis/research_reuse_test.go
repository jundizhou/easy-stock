package stockanalysis

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func reusableJob(id, symbol string, at time.Time) ResearchJob {
	a := Analysis{Symbol: symbol, AnalysisID: id, AI: AISynthesisStatus{Status: "ready"}, ResearchReport: &ResearchReport{ResearchSynthesis: ResearchSynthesis{Headline: "测试研究", Thesis: ResearchClaim{Text: "有证据的研究"}}, Validation: "references_checked", GeneratedAt: at}}
	return ResearchJob{ID: id, Request: ResearchRequest{Symbol: symbol, Purpose: "observe", Horizon: "short", AnalysisLevel: ResearchLevelQuick}, Status: "succeeded", CompletedAt: &at, UpdatedAt: at, Analysis: &a}
}
func TestResearchReuseWindowSurvivesMigrationAndDoesNotRenew(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "research.db")
	store, err := OpenResearchStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := reusableJob("old", "600519.SH", now.Add(-23*time.Hour))
	old.UpdatedAt = now
	if err := store.Save(ctx, old); err != nil {
		t.Fatal(err)
	}
	// More than a UI history page: query must use the indexed symbol, never List(30).
	for i := 0; i < 40; i++ {
		j := reusableJob(NewResearchID(), "000001.SZ", now)
		if err := store.Save(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	failed := reusableJob("latest-failed", "600519.SH", now)
	failed.Status = "failed"
	if err := store.Save(ctx, failed); err != nil {
		t.Fatal(err)
	}
	got, err := store.FindReusable(ctx, "600519", now)
	if err != nil || got.ID != old.ID {
		t.Fatalf("fallback %s %v", got.ID, err)
	}
	if err := store.SaveVerification(ctx, old.ID, ResearchVerification{CheckedAt: now, Summary: "查看不续期"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindReusable(ctx, "600519", now.Add(time.Hour)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("24h boundary %v", err)
	}
	if _, err := store.FindReusable(ctx, "600519", now.Add(time.Hour-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DELETE FROM stock_research_reuse_index`); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenResearchStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err = store.FindReusable(ctx, "600519", now)
	if err != nil || got.ID != old.ID {
		t.Fatalf("backfill %v", err)
	}
	baseline := reusableJob("baseline", "300750.SZ", now)
	baseline.Analysis.ResearchReport = nil
	if err := store.Save(ctx, baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindReusable(ctx, "300750", now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("baseline reused")
	}
	if err := store.Delete(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindReusable(ctx, "600519", now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("deleted report reused")
	}
}
func TestPortfolioResolveSharesBySymbolWithoutOwningCancellation(t *testing.T) {
	store, err := OpenResearchStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var calls atomic.Int32
	release := make(chan struct{})
	runner := func(ctx context.Context, r ResearchRequest, _ ResearchPublisher) (Analysis, *ResearchSnapshot, error) {
		calls.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
			return Analysis{}, nil, ctx.Err()
		}
		j := reusableJob("unused", r.Symbol, time.Now())
		return *j.Analysis, nil, nil
	}
	service := NewResearchService(store, runner)
	defer service.Close()
	ctx := context.Background()
	first, err := service.Start(ctx, ResearchRequest{Symbol: "600519", Purpose: "observe", Horizon: "short"})
	if err != nil {
		t.Fatal(err)
	}
	cost := 999.0
	var wg sync.WaitGroup
	var bad atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, origin, err := service.ResolveForPortfolio(ctx, ResearchRequest{Symbol: "600519.SH", Purpose: "holding", Horizon: "medium", CostPrice: &cost}, time.Now(), false, "")
			if err != nil || j.ID != first.ID || origin != "shared_running" {
				bad.Add(1)
			}
		}()
	}
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatal("same-symbol join failed")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.Wait(canceled, first.ID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	done, err := service.Wait(ctx, first.ID)
	if err != nil || !SuccessfulResearch(done) {
		t.Fatalf("shared canceled %v", err)
	}
	got, origin, err := service.ResolveForPortfolio(ctx, ResearchRequest{Symbol: "600519", Purpose: "holding", CostPrice: &cost, Horizon: "swing"}, time.Now(), false, "")
	if err != nil || got.ID != first.ID || origin != "reused" || calls.Load() != 1 {
		t.Fatalf("changed input repeated research %s %v %d", origin, err, calls.Load())
	}
	forced, origin, err := service.ResolveForPortfolio(ctx, ResearchRequest{Symbol: "600519", Purpose: "holding"}, time.Now(), true, "")
	if err != nil || origin != "new" || forced.ID == first.ID {
		t.Fatal("explicit refresh failed")
	}
	if _, err := service.Wait(ctx, forced.ID); err != nil {
		t.Fatal(err)
	}
}
func TestResearchReuseReadErrorDoesNotCreateAnotherTask(t *testing.T) {
	store, _ := OpenResearchStore("")
	var calls atomic.Int32
	service := NewResearchService(store, func(context.Context, ResearchRequest, ResearchPublisher) (Analysis, *ResearchSnapshot, error) {
		calls.Add(1)
		return Analysis{}, nil, nil
	})
	defer service.Close()
	store.Close()
	if _, _, err := service.ResolveForPortfolio(context.Background(), ResearchRequest{Symbol: "600519"}, time.Now(), false, ""); err == nil {
		t.Fatal("DB error treated as miss")
	}
	if calls.Load() != 0 {
		t.Fatal("unnecessary research")
	}
}
