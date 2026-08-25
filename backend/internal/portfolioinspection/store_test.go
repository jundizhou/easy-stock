package portfolioinspection

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsAndMarksRunningJobsInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portfolio.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "job-1", Status: "running", Stage: "analyzing_stocks", Request: Request{TraderProfile: ProfileBalanced, Holdings: []Holding{{Symbol: "600519.SH", Weight: 20}}}, TotalStocks: 1, StartedAt: time.Now().UTC()}
	if _, err := store.Save(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.MarkInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "interrupted" || loaded.CompletedAt.IsZero() || loaded.Error == "" {
		t.Fatalf("unexpected recovered job: %+v", loaded)
	}
}
