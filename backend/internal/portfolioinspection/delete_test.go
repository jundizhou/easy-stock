package portfolioinspection

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestDeleteInspectionPersistsAndPreservesIndependentData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "portfolio.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	svc := &Service{store: store}
	for _, id := range []string{"remove", "keep"} {
		if _, err := store.Save(ctx, Job{ID: id, Status: "partial", Request: Request{PortfolioPlanID: "plan"}}); err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= 2; n++ {
			if err := store.saveScoringAttempt(ctx, id, scoringAttempt{Number: n, Response: "private attempt"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	schedule := Schedule{ScheduleConfig: ScheduleConfig{Enabled: true, Request: Request{PortfolioPlanID: "plan"}}, LastJobID: "remove", LastRunAt: time.Now().UTC(), NextRunAt: time.Now().UTC().Add(time.Hour)}
	if err := store.saveSchedule(ctx, schedule); err != nil {
		t.Fatal(err)
	}
	if err := store.InitOptimizations(ctx); err != nil {
		t.Fatal(err)
	}
	const snapshot = `{"status":"running","source_report":{"id":"remove"}}`
	if err := store.SaveOptimization(ctx, "optimization", "remove", "fingerprint", []byte(snapshot), "now"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, "remove"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, "remove"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted record still available: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM portfolio_scoring_attempts WHERE job_id='remove'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("attempts remain: %d %v", count, err)
	}
	if attempt, err := store.lastScoringAttempt(ctx, "keep"); err != nil || attempt.Number != 2 {
		t.Fatalf("unrelated attempts changed: %+v %v", attempt, err)
	}
	if data, err := store.GetOptimization(ctx, "optimization"); err != nil || string(data) != snapshot {
		t.Fatalf("independent optimization changed: %s %v", data, err)
	}
	if current, err := store.GetSchedule(ctx, "plan"); err != nil || !reflect.DeepEqual(current, schedule) {
		t.Fatalf("schedule changed: %+v %v", current, err)
	}
	if err := svc.Delete(ctx, "remove"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing record: %v", err)
	}
	store.Close()
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for _, filter := range [][]string{nil, {"plan"}} {
		jobs, err := reopened.List(ctx, 12, filter...)
		if err != nil || len(jobs) != 1 || jobs[0].ID != "keep" {
			t.Fatalf("history after reopen: %+v %v", jobs, err)
		}
	}
}

func TestDeleteInspectionRejectsRunningAndFinalizingJobs(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	svc := &Service{store: store}
	for _, status := range []string{"running", "succeeded", "partial", "failed", "interrupted", "cancelled"} {
		if _, err := store.Save(ctx, Job{ID: status, Status: status}); err != nil {
			t.Fatal(err)
		}
		// A worker can still be saving notifications after persisting a final status.
		svc.runningID = status
		if err := svc.Delete(ctx, status); !errors.Is(err, ErrDeleteRunning) {
			t.Fatalf("deleted active %s worker: %v", status, err)
		}
		svc.runningID = ""
		err := svc.Delete(ctx, status)
		if status == "running" {
			if !errors.Is(err, ErrDeleteRunning) {
				t.Fatalf("deleted persisted running job: %v", err)
			}
		} else if err != nil {
			t.Fatalf("delete %s: %v", status, err)
		}
	}
}

func TestDeleteInspectionRollsBackWhenAttemptCleanupFails(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.Save(ctx, Job{ID: "job", Status: "failed"}); err != nil {
		t.Fatal(err)
	}
	if err := store.saveScoringAttempt(ctx, "job", scoringAttempt{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER fail_attempt_delete BEFORE DELETE ON portfolio_scoring_attempts BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	svc := &Service{store: store}
	if err := svc.Delete(ctx, "job"); err == nil {
		t.Fatal("expected transaction failure")
	}
	if _, err := store.Get(ctx, "job"); err != nil {
		t.Fatalf("record not restored: %v", err)
	}
	if _, err := store.lastScoringAttempt(ctx, "job"); err != nil {
		t.Fatalf("attempt not restored: %v", err)
	}
}

func TestDeleteInspectionCannotBeResurrectedByConcurrentBinding(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	svc := &Service{store: store}
	for i := 0; i < 20; i++ {
		if _, err := store.Save(ctx, Job{ID: "legacy", Status: "failed"}); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := svc.BindPlan(ctx, "legacy", "plan", "方案"); err != nil && !errors.Is(err, sql.ErrNoRows) {
				t.Errorf("bind: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := svc.Delete(ctx, "legacy"); err != nil {
				t.Errorf("delete: %v", err)
			}
		}()
		wg.Wait()
		if _, err := store.Get(ctx, "legacy"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("record resurrected: %v", err)
		}
	}
}
