package stockanalysis

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestResearchQueueDoesNotConsumeFrozenExecutionBudget(t *testing.T) {
	store, err := OpenResearchStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	block := make(chan struct{})
	started := make(chan ResearchBudget, 3)
	var prepared, released atomic.Int32
	prepare := func(ctx context.Context, request ResearchRequest) (context.Context, ResearchBudget, func(), error) {
		prepared.Add(1)
		budget := ResearchBudget{StageTimeoutSeconds: 1, TotalTimeoutSeconds: 1, ReasoningEffort: request.Symbol}
		if _, ok := ctx.Deadline(); ok {
			t.Error("queue already has an execution deadline")
		}
		return ctx, budget, func() { released.Add(1) }, nil
	}
	runner := func(ctx context.Context, request ResearchRequest, _ ResearchPublisher) (Analysis, *ResearchSnapshot, error) {
		budget, ok := ResearchExecutionBudget(ctx)
		deadline, bounded := ctx.Deadline()
		if !ok || !bounded || budget.ReasoningEffort != request.Symbol || time.Until(deadline) < 800*time.Millisecond {
			t.Error("execution lost its frozen plan or waited away its budget")
		}
		started <- budget
		if request.Symbol != "000003.SZ" {
			<-block
		}
		return Analysis{Symbol: request.Symbol, AI: AISynthesisStatus{Status: "ready"}}, nil, nil
	}
	service := NewResearchServiceWithPreparation(store, runner, prepare)
	defer service.Close()
	defer func() {
		select {
		case <-block:
		default:
			close(block)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for _, symbol := range []string{"000001", "000002"} {
		if _, err := service.Start(ctx, ResearchRequest{Symbol: symbol}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("worker did not start")
		}
	}
	queued, err := service.Start(ctx, ResearchRequest{Symbol: "000003"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.Start(ctx, queued.Request)
	if err != nil || again.ID != queued.ID || prepared.Load() != 3 {
		t.Fatal("deduplicated work allocated another model snapshot")
	}
	// Hold both workers beyond the entire execution allowance. The queued
	// task must still get its full allowance when one of them is released.
	time.Sleep(1100 * time.Millisecond)
	close(block)
	job, err := service.Wait(ctx, queued.ID)
	if err != nil || job.Status != "succeeded" || job.Budget == nil || job.Budget.ReasoningEffort != queued.Request.Symbol {
		t.Fatalf("queue exhausted the job budget: job=%+v err=%v", job, err)
	}
	service.Close()
	if released.Load() != 3 {
		t.Fatal("frozen model snapshots leaked")
	}
}

func TestResearchCancellationReleasesQueuedModelSnapshot(t *testing.T) {
	store, err := OpenResearchStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	started := make(chan struct{}, 2)
	var released atomic.Int32
	prepare := func(ctx context.Context, request ResearchRequest) (context.Context, ResearchBudget, func(), error) {
		return ctx, ResearchBudgetFor(request, 300*time.Second, "max"), func() { released.Add(1) }, nil
	}
	runner := func(ctx context.Context, _ ResearchRequest, _ ResearchPublisher) (Analysis, *ResearchSnapshot, error) {
		started <- struct{}{}
		<-ctx.Done()
		return Analysis{}, nil, ctx.Err()
	}
	service := NewResearchServiceWithPreparation(store, runner, prepare)
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, symbol := range []string{"000001", "000002"} {
		if _, err := service.Start(ctx, ResearchRequest{Symbol: symbol}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("worker did not start")
		}
	}
	queued, err := service.Start(ctx, ResearchRequest{Symbol: "000003"})
	if err != nil || !service.Cancel(queued.ID) {
		t.Fatal("queued cancellation failed")
	}
	job, err := service.Wait(ctx, queued.ID)
	if err != nil || job.Status != "cancelled" {
		t.Fatal("queued cancellation was lost")
	}
	service.Close()
	if released.Load() != 3 {
		t.Fatal("cancelled snapshot leaked")
	}
}
