package stockanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestResearchStoreImmutableVersionsRecoveryAndDeletion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "research.db")
	store, err := OpenResearchStore(path)
	if err != nil {
		t.Fatal(err)
	}
	analysis, snapshot := researchFixture(t)
	job := ResearchJob{ID: NewResearchID(), Request: ResearchRequest{Symbol: analysis.Symbol}, Status: "running", UpdatedAt: time.Now(), Analysis: &analysis, Snapshot: &snapshot}
	if err := store.Save(ctx, job); err != nil {
		t.Fatal(err)
	}
	snapshot.Limitations = append(snapshot.Limitations, "new limitation")
	if err := store.Save(ctx, job); err == nil {
		t.Fatal("same snapshot version was overwritten")
	}
	snapshot.Version++
	if err := store.Save(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, job.ID); err == nil {
		t.Fatal("deleted running job")
	}
	if err := store.SaveVerification(ctx, job.ID, ResearchVerification{CheckedAt: time.Now()}); err == nil {
		t.Fatal("verified running job")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenResearchStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.MarkInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(ctx, job.ID)
	if err != nil || loaded.Status != "interrupted" || loaded.Analysis == nil || loaded.Snapshot.Version != 2 {
		t.Fatalf("recovery failed: %+v %v", loaded, err)
	}
	if err := store.SaveVerification(ctx, job.ID, ResearchVerification{CheckedAt: time.Now(), Summary: "no accuracy claim"}); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Get(ctx, job.ID)
	if err != nil || loaded.Verification == nil {
		t.Fatal("verification not persisted")
	}
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM stock_research_snapshots`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("snapshot versions: %d %v", count, err)
	}
	if err := store.Delete(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, job.ID); err == nil {
		t.Fatal("deleted job still exists")
	}
	if err := store.SaveVerification(ctx, job.ID, ResearchVerification{CheckedAt: time.Now()}); err == nil {
		t.Fatal("orphan verification written")
	}
}

func TestResearchServiceDeduplicatesCancelsAndBoundsQueue(t *testing.T) {
	store, err := OpenResearchStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var concurrent, peak atomic.Int32
	runner := func(ctx context.Context, r ResearchRequest, publish ResearchPublisher) (Analysis, *ResearchSnapshot, error) {
		active := concurrent.Add(1)
		defer concurrent.Add(-1)
		for old := peak.Load(); active > old && !peak.CompareAndSwap(old, active); old = peak.Load() {
		}
		if err := publish("collecting", "loading", nil, nil); err != nil {
			return Analysis{}, nil, err
		}
		<-ctx.Done()
		return Analysis{}, nil, ctx.Err()
	}
	service := NewResearchService(store, runner)
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := service.Start(ctx, ResearchRequest{Symbol: "600519"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.Start(ctx, ResearchRequest{Symbol: "600519.SH", Purpose: "observe", Horizon: "swing"})
	if err != nil || again.ID != first.ID {
		t.Fatal("duplicate active job")
	}
	for _, symbol := range []string{"300750", "601138", "688981", "600770", "000001", "000002", "000003"} {
		if _, err := service.Start(ctx, ResearchRequest{Symbol: symbol}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Start(ctx, ResearchRequest{Symbol: "000004"}); !errors.Is(err, ErrResearchBusy) {
		t.Fatalf("unbounded queue: %v", err)
	}
	if !service.Cancel(first.ID) {
		t.Fatal("cancel not delivered")
	}
	job, err := service.Wait(ctx, first.ID)
	if err != nil || job.Status != "cancelled" {
		t.Fatalf("cancel: %s %v", job.Status, err)
	}
	service.Close()
	if peak.Load() > 2 {
		t.Fatalf("exceeded worker limit: %d", peak.Load())
	}
	if _, err := service.Start(ctx, ResearchRequest{Symbol: "600519"}); err == nil {
		t.Fatal("closed service accepted work")
	}
}

func TestResearchServicePersistsFailureAndRecoversWorkerPanic(t *testing.T) {
	store, err := OpenResearchStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewResearchService(store, func(context.Context, ResearchRequest, ResearchPublisher) (Analysis, *ResearchSnapshot, error) {
		panic("test panic")
	})
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	job, err := service.Start(ctx, ResearchRequest{Symbol: "600519"})
	if err != nil {
		t.Fatal(err)
	}
	job, err = service.Wait(ctx, job.ID)
	if err != nil || job.Status != "failed" {
		t.Fatalf("panic not contained: %+v %v", job, err)
	}
	data, _ := json.Marshal(job.Public())
	var public map[string]any
	_ = json.Unmarshal(data, &public)
	if _, exists := public["snapshot"]; exists {
		t.Fatal("poll payload duplicated full snapshot")
	}
}

func TestResearchServiceQuantitativeCompletesWithoutAI(t *testing.T) {
	store, err := OpenResearchStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewResearchService(store, func(_ context.Context, request ResearchRequest, _ ResearchPublisher) (Analysis, *ResearchSnapshot, error) {
		return Analysis{Symbol: request.Symbol, AI: AISynthesisStatus{Status: "skipped"}}, nil, nil
	})
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	job, err := service.Start(ctx, ResearchRequest{Symbol: "000930", AnalysisLevel: ResearchLevelQuantitative})
	if err != nil {
		t.Fatal(err)
	}
	job, err = service.Wait(ctx, job.ID)
	if err != nil || job.Status != "succeeded" || job.Analysis.AI.Status == "ready" || job.Analysis.ResearchReport != nil {
		t.Fatalf("quantitative completion = %+v, error = %v", job, err)
	}
}
