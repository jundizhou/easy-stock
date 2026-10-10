package portfolioinspection

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestScheduleSaveWaitsForOtherWriter(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "portfolio.db")
	reader, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	tx, err := writer.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO portfolio_inspection_schedules(plan_id,content_json) VALUES('other','{}')`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := NewScheduler(reader, nil).Save(ctx, "plan", ScheduleConfig{Interval: 3, Unit: "days", StartDate: "2026-10-11", Time: "15:30", Request: Request{PortfolioPlanID: "plan"}}, time.Now())
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("save should wait for the write lock, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("save after lock release:", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("save did not recover after writer finished")
	}
	if _, err = reader.GetSchedule(ctx, "plan"); err != nil {
		t.Fatal("schedule was not persisted", err)
	}
}
