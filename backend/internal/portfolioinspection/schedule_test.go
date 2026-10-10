package portfolioinspection

import (
	"context"
	"database/sql"
	"easy-stock/backend/internal/stockanalysis"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func scheduleDate(t *testing.T, value string) time.Time {
	t.Helper()
	date, err := time.ParseInLocation("2006-01-02 15:04", value, scheduleZone)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func TestNextScheduleTimeCalendarBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, start, unit, after, want string
		interval                       int
	}{
		{"first future slot", "2026-10-10", "days", "2026-10-09 16:00", "2026-10-10 15:30", 3},
		{"three calendar days", "2026-10-10", "days", "2026-10-10 15:30", "2026-10-13 15:30", 3},
		{"missed slots skipped", "2026-10-10", "days", "2026-10-20 16:00", "2026-10-22 15:30", 3},
		{"two weeks", "2026-10-10", "weeks", "2026-10-10 15:30", "2026-10-24 15:30", 2},
		{"month end", "2026-01-31", "months", "2026-02-01 00:00", "2026-02-28 15:30", 1},
		{"no month end drift", "2026-01-31", "months", "2026-02-28 15:30", "2026-03-31 15:30", 1},
		{"leap year", "2024-01-31", "months", "2024-02-01 00:00", "2024-02-29 15:30", 1},
		{"year rollover", "2026-11-30", "months", "2026-12-01 00:00", "2027-01-30 15:30", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NextScheduleTime(ScheduleConfig{StartDate: tc.start, Time: "15:30", Unit: tc.unit, Interval: tc.interval}, scheduleDate(t, tc.after).UTC())
			if err != nil || !got.Equal(scheduleDate(t, tc.want)) {
				t.Fatalf("got %s (%v), want %s", got, err, tc.want)
			}
		})
	}
	for _, c := range []ScheduleConfig{
		{StartDate: "2026-02-30", Time: "15:30", Unit: "days", Interval: 1},
		{StartDate: "2026-10-10", Time: "24:00", Unit: "days", Interval: 1},
		{StartDate: "2026-10-10", Time: "15:30", Unit: "days", Interval: 0},
		{StartDate: "2026-10-10", Time: "15:30", Unit: "hours", Interval: 1},
	} {
		if _, err := NextScheduleTime(c, time.Now()); err == nil {
			t.Fatalf("invalid schedule accepted: %+v", c)
		}
	}
}

func TestSchedulesPersistCatchUpOnceAndWaitForRunningInspection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "portfolio.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	request, results, _, report := scoreFixture()
	request.PortfolioPlanID, request.PortfolioPlanName = "plan-a", "均衡组合"
	cfg := ScheduleConfig{Enabled: true, Interval: 3, Unit: "days", StartDate: "2026-10-10", Time: "15:30", Channels: []string{"dingtalk"}, Request: request}
	scheduler := NewScheduler(store, nil)
	if _, err := scheduler.Save(ctx, "plan-a", cfg, scheduleDate(t, "2026-10-09 12:00")); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	raw, _ := json.Marshal(report)
	service := NewService(store, &scoreGateway{content: string(raw)}, nil, nil)
	release := make(chan struct{})
	service.ConfigureResearch(func(ctx context.Context, _ Holding, _ Request, _ time.Time, _ bool, _ string, _ func(HoldingResult)) (HoldingResult, error) {
		select {
		case <-release:
			return results[0], nil
		case <-ctx.Done():
			return HoldingResult{}, ctx.Err()
		}
	}, nil)
	defer service.Close()
	scheduler = NewScheduler(store, service)
	now := scheduleDate(t, "2026-10-20 16:00")
	if err := scheduler.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	a, err := store.GetSchedule(ctx, "plan-a")
	if err != nil || a.LastJobID == "" || !a.NextRunAt.Equal(scheduleDate(t, "2026-10-22 15:30")) {
		t.Fatalf("bad catchup %+v %v", a, err)
	}
	job, err := store.Get(ctx, a.LastJobID)
	if err != nil || job.ScheduleID != "plan-a" || len(job.NotificationChannels) != 1 || job.NotificationChannels[0] != "dingtalk" {
		t.Fatalf("missing routing %+v %v", job, err)
	}
	// Another plan remains due while A runs, even across repeated ticks.
	cfg.Request.PortfolioPlanID = "plan-b"
	if _, err := scheduler.Save(ctx, "plan-b", cfg, scheduleDate(t, "2026-10-09 12:00")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := scheduler.Tick(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	jobs, _ := store.List(ctx, 30)
	if len(jobs) != 1 {
		t.Fatalf("duplicate/overlapping jobs: %d", len(jobs))
	}
	b, _ := store.GetSchedule(ctx, "plan-b")
	if b.LastJobID != "" || b.NextRunAt.After(now) {
		t.Fatal("busy service consumed next plan's slot")
	}
	close(release)
	service.wg.Wait()
	if err := scheduler.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	jobs, _ = store.List(ctx, 30)
	if len(jobs) != 2 {
		t.Fatalf("waiting plan not started: %d", len(jobs))
	}
	// Restarted scheduler must not launch consumed slots.
	restarted := NewScheduler(store, service)
	if err := restarted.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	jobs, _ = store.List(ctx, 30)
	if len(jobs) != 2 {
		t.Fatal("restart repeated a consumed slot")
	}
	if err := scheduler.Delete(ctx, "plan-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSchedule(ctx, "plan-b"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("delete: %v", err)
	}
	cfg.Request.PortfolioPlanID = "plan-a"
	cfg.Enabled = false
	if _, err := scheduler.Save(ctx, "plan-a", cfg, now); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Tick(ctx, now.AddDate(0, 1, 0)); err != nil {
		t.Fatal(err)
	}
	jobs, _ = store.List(ctx, 30)
	if len(jobs) != 2 {
		t.Fatal("disabled/deleted schedule ran")
	}
}

func TestScheduleValidationUpdateAndRetry(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Missing model should retain the due slot and back off instead of losing it.
	service := NewService(store, nil, func(context.Context, string) (analysis stockanalysis.Analysis, err error) { return }, nil)
	defer service.Close()
	scheduler := NewScheduler(store, service)
	req, _, _, _ := scoreFixture()
	req.PortfolioPlanID = "plan"
	cfg := ScheduleConfig{Enabled: true, Interval: 1, Unit: "months", StartDate: "2026-01-31", Time: "15:30", Request: req}
	before := scheduleDate(t, "2026-01-30 12:00")
	original, err := scheduler.Save(ctx, "plan", cfg, before)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Request.Holdings[0].Weight = 40
	updated, err := scheduler.Save(ctx, "plan", cfg, scheduleDate(t, "2026-02-01 00:00"))
	if err != nil || !updated.NextRunAt.Equal(original.NextRunAt) {
		t.Fatal("snapshot update lost a due slot", err)
	}
	now := scheduleDate(t, "2026-02-01 00:00")
	if err := scheduler.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	failed, _ := store.GetSchedule(ctx, "plan")
	if failed.LastError == "" || !failed.RetryAt.Equal(now.Add(5*time.Minute)) || !failed.NextRunAt.Equal(original.NextRunAt) {
		t.Fatalf("bad retry %+v", failed)
	}
	if err := scheduler.Tick(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	again, _ := store.GetSchedule(ctx, "plan")
	if !again.RetryAt.Equal(failed.RetryAt) {
		t.Fatal("retry did not back off")
	}
	cfg.Channels = []string{"email"}
	if _, err := scheduler.Save(ctx, "plan", cfg, now); err == nil {
		t.Fatal("unknown channel accepted")
	}
	cfg.Channels = nil
	cfg.Request.Holdings = nil
	if _, err := scheduler.Save(ctx, "plan", cfg, now); err == nil {
		t.Fatal("enabled empty holdings accepted")
	}
	cfg.Enabled = false
	if _, err := scheduler.Save(ctx, "plan", cfg, now); err != nil {
		t.Fatal("cannot disable invalid draft", err)
	}
}

func TestScheduledJobAndSlotCommitTogether(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	req, _, _, _ := scoreFixture()
	req.PortfolioPlanID = "plan"
	service := NewService(store, &scoreGateway{}, func(context.Context, string) (analysis stockanalysis.Analysis, err error) { return }, nil)
	defer service.Close()
	scheduler := NewScheduler(store, service)
	cfg := ScheduleConfig{Enabled: true, Interval: 3, Unit: "days", StartDate: "2026-10-10", Time: "15:30", Request: req}
	initial, err := scheduler.Save(ctx, "plan", cfg, scheduleDate(t, "2026-10-09 12:00"))
	if err != nil {
		t.Fatal(err)
	}
	// Force the second write to fail after the task insert within the transaction.
	_, err = store.db.Exec(`CREATE TRIGGER fail_schedule_update BEFORE UPDATE ON portfolio_inspection_schedules BEGIN SELECT RAISE(ABORT,'test write failure'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Tick(ctx, scheduleDate(t, "2026-10-10 16:00")); err == nil {
		t.Fatal("expected persistence failure")
	}
	jobs, err := store.List(ctx, 30)
	if err != nil || len(jobs) != 0 {
		t.Fatal("failed schedule commit left an orphan job", err)
	}
	current, err := store.GetSchedule(ctx, "plan")
	if err != nil || !current.NextRunAt.Equal(initial.NextRunAt) || current.LastJobID != "" {
		t.Fatal("failed transaction consumed slot", err)
	}
}

func TestResumeScheduledInspectionKeepsNotificationSelection(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	req, results, _, report := scoreFixture()
	req.PortfolioPlanID = "plan"
	raw, _ := json.Marshal(report)
	service := NewService(store, &scoreGateway{content: string(raw)}, func(context.Context, string) (analysis stockanalysis.Analysis, err error) { return }, nil)
	defer service.Close()
	previous := Job{ID: "interrupted-schedule", Status: "partial", ScheduleID: "plan", NotificationChannels: []string{"feishu"}, Request: req, Results: results}
	if _, err := store.Save(ctx, previous); err != nil {
		t.Fatal(err)
	}
	next, err := service.Resume(ctx, previous.ID)
	if err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	if next.ScheduleID != "plan" || len(next.NotificationChannels) != 1 || next.NotificationChannels[0] != "feishu" {
		t.Fatalf("resume lost notification routing: %+v", next)
	}
}
