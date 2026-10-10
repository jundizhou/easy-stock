package review

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testCalendar func(context.Context, time.Time) (bool, error)

func (c testCalendar) IsTradingDay(ctx context.Context, d time.Time) (bool, error) { return c(ctx, d) }
func scheduleDate(t *testing.T, v string) time.Time {
	t.Helper()
	d, e := time.ParseInLocation("2006-01-02 15:04", v, shanghaiLocation())
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func scheduleStore(t *testing.T) *Store {
	t.Helper()
	s, e := OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func fixtureCalendar() TradingCalendar {
	return testCalendar(func(_ context.Context, d time.Time) (bool, error) {
		date := d.Format("2006-01-02")
		return d.Weekday() != time.Saturday && d.Weekday() != time.Sunday && !(date >= "2026-10-01" && date <= "2026-10-07"), nil
	})
}
func TestDailyScheduleExchangeSlots(t *testing.T) {
	s := NewDailyScheduler(scheduleStore(t), nil, fixtureCalendar())
	for _, tt := range []struct{ after, clock, want, target string }{
		{"2026-10-09 23:00", "22:00", "2026-10-11 22:00", "2026-10-12"},
		{"2026-09-30 12:00", "22:00", "2026-10-07 22:00", "2026-10-08"},
		{"2026-10-12 18:00", "21:35", "2026-10-12 21:35", "2026-10-13"},
		{"2026-10-12 22:00", "22:00", "2026-10-13 22:00", "2026-10-14"},
	} {
		got, target, err := s.next(context.Background(), tt.clock, scheduleDate(t, tt.after).UTC(), "")
		if err != nil || !got.Equal(scheduleDate(t, tt.want)) || target != tt.target {
			t.Fatalf("%+v got %v %s %v", tt, got, target, err)
		}
	}
}
func TestDailySchedulePersistenceBusyAndDuplicatePrevention(t *testing.T) {
	ctx := context.Background()
	store := scheduleStore(t)
	a := NewAutomation(store, nil, nil, nil, "", &fakeBrowserPrompter{})
	done := make(chan DailySummaryJob, 2)
	a.SetDailySummaryCompleted(func(j DailySummaryJob, _ *DailySummary) { done <- j })
	s := NewDailyScheduler(store, a, fixtureCalendar())
	now := scheduleDate(t, "2026-10-11 21:00")
	v, err := s.Save(ctx, DailyScheduleConfig{Enabled: true, Time: "22:00", Channels: []string{"dingtalk"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	a.dailySummaryRunning = true
	if err = s.Tick(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	waiting, _ := store.GetDailySchedule(ctx)
	if !waiting.NextRunAt.Equal(v.NextRunAt) || !waiting.LastRunAt.IsZero() {
		t.Fatal("busy task consumed slot")
	}
	a.dailySummaryRunning = false
	if err = s.Tick(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	select {
	case j := <-done:
		if j.ScheduledTargetDate != "2026-10-12" || len(j.NotificationChannels) != 1 || !j.WindowStart.Equal(scheduleDate(t, "2026-10-09 15:00")) || !j.WindowEnd.Equal(scheduleDate(t, "2026-10-11 22:00")) {
			t.Fatalf("wrong job: %+v", j)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled job did not finish")
	}
	// The consumed slot persists even if the model fails or the process restarts.
	restarted := NewDailyScheduler(store, a, fixtureCalendar())
	if err = restarted.Tick(ctx, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetDailySchedule(ctx)
	if got.LastTargetDate != "2026-10-12" || got.TargetTradeDate != "2026-10-13" {
		t.Fatalf("invalid schedule: %+v", got)
	}
	if err = restarted.Tick(ctx, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(done) != 0 {
		t.Fatal("duplicate delivery")
	}
	// Saving a different time must not create another run for the same target day.
	got, err = restarted.Save(ctx, DailyScheduleConfig{Enabled: true, Time: "23:30"}, now.Add(2*time.Hour))
	if err != nil || got.TargetTradeDate != "2026-10-13" {
		t.Fatal("configuration edit repeated target", err)
	}
}
func TestDailyScheduleMissedOpeningAndCalendarFailure(t *testing.T) {
	ctx := context.Background()
	store := scheduleStore(t)
	s := NewDailyScheduler(store, nil, fixtureCalendar())
	now := scheduleDate(t, "2026-10-11 21:00")
	if _, e := s.Save(ctx, DailyScheduleConfig{Enabled: true, Time: "22:00"}, now); e != nil {
		t.Fatal(e)
	}
	if e := s.Tick(ctx, scheduleDate(t, "2026-10-12 09:30")); e != nil {
		t.Fatal(e)
	}
	v, _ := store.GetDailySchedule(ctx)
	if v.TargetTradeDate != "2026-10-13" || !v.LastRunAt.IsZero() {
		t.Fatal("expired forecast launched")
	}
	s.calendar = testCalendar(func(context.Context, time.Time) (bool, error) { return false, errors.New("calendar unavailable") })
	if e := s.Tick(ctx, scheduleDate(t, "2026-10-12 22:00")); e == nil {
		t.Fatal("calendar failure ignored")
	}
	v, _ = store.GetDailySchedule(ctx)
	if v.LastError == "" || v.RetryAt.IsZero() || !v.LastRunAt.IsZero() {
		t.Fatal("failure not persisted")
	}
	if _, e := s.Save(ctx, DailyScheduleConfig{Time: "22:00", Enabled: false}, now); e != nil {
		t.Fatal("disable must work offline", e)
	}
	for _, c := range []DailyScheduleConfig{{Time: "25:00"}, {Time: "22:00", Channels: []string{"email"}}, {Time: "22:00", Channels: []string{"feishu", "feishu"}}} {
		if _, e := s.Save(ctx, c, now); e == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
func TestScheduledJobAndSlotRollbackTogether(t *testing.T) {
	s := scheduleStore(t)
	ctx := context.Background()
	_, err := s.db.Exec(`CREATE TRIGGER reject_schedule BEFORE INSERT ON review_schedule_state BEGIN SELECT RAISE(ABORT,'fixture'); END`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.saveScheduledSummary(ctx, DailySummaryJob{TradeDate: "2026-10-09", Status: "running", Stage: "preparing"}, DailySchedule{})
	if err == nil {
		t.Fatal("expected transaction failure")
	}
	_, err = s.GetDailySummaryJob(ctx, "2026-10-09")
	if err == nil {
		t.Fatal("orphan job persisted")
	}
}
func TestDailySummaryMarkdownCompleteAndPrivate(t *testing.T) {
	long := strings.Repeat("完整作者观点", 4000)
	r := &DailySummary{ExecutiveSummary: "综合判断", MarketFramework: DailyMarketFramework{Cycle: "周期判断"}, Consensus: []DailyConsensus{{Conclusion: "共识判断", Evidence: []string{"private-evidence"}}}, Scenarios: []DailyScenario{{Name: "情景判断", Invalidation: "失效条件"}}, Directions: []DailyDirectionView{{Name: "方向判断"}}, TomorrowFocus: []DailyStockView{{Name: "关注股票"}}, TomorrowPlaybook: DailyPlaybook{Intraday: []string{"盘中计划"}}, AuthorViews: []DailyAuthorView{{Author: "作者测试", CoreView: long, TomorrowOutlook: "作者展望", Evidence: []string{"private-quote"}}}, GenerationErrors: []string{"private-error"}, Sources: []DailySummarySource{{URL: "private-url"}}}
	got := DailySummaryMarkdown(DailySummaryJob{Status: "partial", ScheduledTargetDate: "2026-10-12", Error: "private-error"}, r)
	for _, want := range []string{"综合判断", "周期判断", "共识判断", "情景判断", "失效条件", "方向判断", "关注股票", "盘中计划", "作者测试", long, "作者展望", "未完成", "2026-10-12"} {
		if !strings.Contains(got, want) {
			t.Fatal("missing report result")
		}
	}
	if strings.Contains(got, "private-") {
		t.Fatal("evidence or error leaked")
	}
	if !strings.Contains(DailySummaryMarkdown(DailySummaryJob{Status: "failed"}, nil), "尚未生成") {
		t.Fatal("missing failure state")
	}
}
