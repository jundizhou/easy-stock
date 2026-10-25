package review

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newValidationTestAutomation(t *testing.T, date string) (*Automation, *Store) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.SaveDailySummary(context.Background(), DailySummary{TradeDate: date}); err != nil {
		t.Fatal(err)
	}
	return NewAutomation(store, nil, nil, nil, ""), store
}

func TestDailyValidationWaitingForNextSessionClose(t *testing.T) {
	automation, _ := newValidationTestAutomation(t, "2026-10-23")
	for _, now := range []time.Time{
		time.Date(2026, 10, 25, 12, 0, 0, 0, shanghaiLocation()),
		time.Date(2026, 10, 26, 14, 59, 0, 0, shanghaiLocation()),
	} {
		job, err := automation.getDailyValidationJob(context.Background(), "2026-10-23", now)
		if err != nil || job.Status != "waiting" || job.CanStart || job.VerificationDate != "2026-10-26" || !strings.Contains(job.Message, "周一") {
			t.Fatalf("waiting job=%+v err=%v", job, err)
		}
	}
	job, err := automation.getDailyValidationJob(context.Background(), "2026-10-23", time.Date(2026, 10, 26, 15, 0, 0, 0, shanghaiLocation()))
	if err != nil || job.Status != "idle" || !job.CanStart || !strings.Contains(job.Message, "尚未运行") {
		t.Fatalf("idle job=%+v err=%v", job, err)
	}
	date, _, err := DailyValidationWindow("2026-09-30")
	if err != nil || date != "2026-10-08" {
		t.Fatalf("holiday date=%s err=%v", date, err)
	}
}

func TestDailyValidationRecoversInterruptedAndExpiredJobs(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "restart", true: "timeout"}[active], func(t *testing.T) {
			automation, store := newValidationTestAutomation(t, "2026-10-22")
			now := time.Date(2026, 10, 25, 12, 0, 0, 0, shanghaiLocation())
			if _, err := store.SaveDailyValidationJob(context.Background(), DailyValidationJob{SummaryDate: "2026-10-22", Status: "running", Stage: "explaining", StartedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}); err != nil {
				t.Fatal(err)
			}
			if active {
				runCtx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				automation.dailyValidationRunning["2026-10-22"] = &dailyValidationRun{ctx: runCtx, cancel: cancel}
			}
			job, err := automation.getDailyValidationJob(context.Background(), "2026-10-22", now)
			if err != nil || job.Status != "failed" || job.Stage != "interrupted" || job.CompletedAt.IsZero() || !job.CanStart {
				t.Fatalf("recovered job=%+v err=%v", job, err)
			}
			stored, err := store.GetDailyValidationJob(context.Background(), "2026-10-22")
			if err != nil || stored.Status != "failed" || automation.dailyValidationRunning["2026-10-22"] != nil {
				t.Fatalf("stored job=%+v err=%v", stored, err)
			}
		})
	}
}

func TestDailyValidationRejectsWrongSessionEvidence(t *testing.T) {
	for _, date := range []string{"", "2026-10-23", "2026-10-24", "2026-10-27"} {
		if err := ValidateDailyValidationSnapshot("2026-10-23", DailyValidationSnapshot{TradeDate: date}); err == nil {
			t.Fatalf("accepted snapshot date %q", date)
		}
	}
	valid := DailyValidationSnapshot{TradeDate: "2026-10-26", CapturedAt: time.Date(2026, 10, 26, 15, 0, 0, 0, shanghaiLocation())}
	if err := ValidateDailyValidationSnapshot("2026-10-23", valid); err != nil {
		t.Fatal(err)
	}
	valid.Indexes = []DailyValidationIndex{{Name: "上证指数", TradeDate: "2026-10-23"}}
	if err := ValidateDailyValidationSnapshot("2026-10-23", valid); err == nil {
		t.Fatal("accepted mixed-date index evidence")
	}
}

func TestDailyValidationRejectsFutureRunAndInvalidCache(t *testing.T) {
	automation, store := newValidationTestAutomation(t, "2099-10-23")
	if _, err := automation.StartDailyValidation(context.Background(), "2099-10-23", false, func(context.Context, DailySummary) (DailyValidationSnapshot, error) {
		t.Error("collector must not run before verification close")
		return DailyValidationSnapshot{}, nil
	}); err == nil {
		t.Fatal("accepted future validation")
	}
	if _, err := store.SaveDailyValidation(context.Background(), DailyValidation{SummaryDate: "2099-10-23", VerificationDate: "2099-10-23"}); err != nil {
		t.Fatal(err)
	}
	if _, err := automation.GetDailyValidation(context.Background(), "2099-10-23"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("invalid cache err=%v", err)
	}
}

func TestDailyValidationLiveJobIsNotReclaimed(t *testing.T) {
	automation, store := newValidationTestAutomation(t, "2026-10-22")
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, shanghaiLocation())
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	automation.dailyValidationRunning["2026-10-22"] = &dailyValidationRun{ctx: runCtx, cancel: cancel}
	if _, err := store.SaveDailyValidationJob(context.Background(), DailyValidationJob{SummaryDate: "2026-10-22", Status: "running", Stage: "explaining", StartedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	job, err := automation.getDailyValidationJob(context.Background(), "2026-10-22", now)
	if err != nil || job.Status != "running" || job.CanStart {
		t.Fatalf("live job=%+v err=%v", job, err)
	}
}

func TestDailyValidationWorkerRejectsSameDayEvidenceAndCanRetry(t *testing.T) {
	automation, store := newValidationTestAutomation(t, "2026-10-19")
	ctx := context.Background()
	if _, err := automation.StartDailyValidation(ctx, "2026-10-19", false, func(context.Context, DailySummary) (DailyValidationSnapshot, error) {
		return DailyValidationSnapshot{TradeDate: "2026-10-19"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	waitForValidation := func(status string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			job, err := automation.GetDailyValidationJob(ctx, "2026-10-19")
			if err != nil {
				t.Fatal(err)
			}
			if job.Status == status {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("validation did not reach %s", status)
	}
	waitForValidation("failed")
	if _, err := store.GetDailyValidation(ctx, "2026-10-19"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("invalid result was saved: %v", err)
	}
	if _, err := automation.StartDailyValidation(ctx, "2026-10-19", true, func(context.Context, DailySummary) (DailyValidationSnapshot, error) {
		return DailyValidationSnapshot{TradeDate: "2026-10-20", CapturedAt: time.Date(2026, 10, 20, 16, 0, 0, 0, shanghaiLocation())}, nil
	}); err != nil {
		t.Fatal(err)
	}
	waitForValidation("succeeded")
	if validation, err := automation.GetDailyValidation(ctx, "2026-10-19"); err != nil || validation.VerificationDate != "2026-10-20" {
		t.Fatalf("validation=%+v err=%v", validation, err)
	}
}

func TestDailyValidationLateWorkerCannotOverwriteRetry(t *testing.T) {
	automation, store := newValidationTestAutomation(t, "2026-10-19")
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	defer close(release)
	oldCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	oldRun := &dailyValidationRun{ctx: oldCtx, cancel: cancel}
	automation.dailyValidationRunning["2026-10-19"] = oldRun
	oldJob := DailyValidationJob{SummaryDate: "2026-10-19", Status: "running", Stage: "collecting", StartedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour)}
	if _, err := store.SaveDailyValidationJob(ctx, oldJob); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(done)
		automation.runDailyValidation(DailySummary{TradeDate: "2026-10-19"}, func(context.Context, DailySummary) (DailyValidationSnapshot, error) {
			close(started)
			<-release
			return DailyValidationSnapshot{TradeDate: "2026-10-20", DataQuality: []string{"late-result"}}, nil
		}, oldRun, oldJob)
	}()
	<-started
	job, err := automation.GetDailyValidationJob(ctx, "2026-10-19")
	if err != nil || job.Status != "failed" {
		t.Fatalf("reclaimed job=%+v err=%v", job, err)
	}
	if _, err := automation.StartDailyValidation(ctx, "2026-10-19", true, func(context.Context, DailySummary) (DailyValidationSnapshot, error) {
		return DailyValidationSnapshot{TradeDate: "2026-10-20", DataQuality: []string{"retry-result"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err = automation.GetDailyValidationJob(ctx, "2026-10-19")
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status != "succeeded" {
		t.Fatalf("retry job=%+v", job)
	}
	release <- struct{}{}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("late worker did not finish")
	}
	validation, err := automation.GetDailyValidation(ctx, "2026-10-19")
	if err != nil || len(validation.Snapshot.DataQuality) != 1 || validation.Snapshot.DataQuality[0] != "retry-result" {
		t.Fatalf("late worker replaced retry: validation=%+v err=%v", validation, err)
	}
}

func TestBuildDailyValidationScoresMarketThemeAndStockEvidence(t *testing.T) {
	summary := DailySummary{
		TradeDate:             "2026-08-13",
		GeneratedAt:           time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC),
		MarketRegime:          "修复",
		TomorrowOutlook:       "偏强情景优先，关注主线延续",
		Directions:            []DailyDirectionView{{Name: "机器人", Trigger: "核心股保持强势"}},
		TomorrowFocus:         []DailyStockView{{Name: "样本科技", Symbol: "600001.SH", Logic: "核心承接"}},
		VerificationChecklist: []string{"核心股不能出现连续负反馈"},
	}
	snapshot := DailyValidationSnapshot{
		TradeDate: "2026-08-14",
		Emotion:   &DailyValidationEmotion{Phase: "启动/修复", EmotionScore: 72, LimitUpCount: 45, BrokenCount: 8, MaxStreak: 6, PreviousLimitUpRet: 2.4},
		Indexes:   []DailyValidationIndex{{Name: "上证指数", ChangePercent: 1.1}},
		Themes:    []DailyValidationTheme{{Name: "机器人", ChangePercent: 3.4, RisingCount: 28, LimitUpCount: 5, Stage: "发酵"}},
		Stocks:    []DailyValidationStock{{Name: "样本科技", Symbol: "600001.SH", Open: 10.2, High: 11.2, Low: 10, Price: 11, PreviousClose: 10, ChangePercent: 10, Matched: true}},
	}
	validation := BuildDailyValidation(summary, snapshot)
	if validation.Market.Verdict != "correct" {
		t.Fatalf("market verdict = %s, want correct", validation.Market.Verdict)
	}
	if validation.Scenario.ActualKey != "strong" || validation.Scenario.Verdict != "correct" {
		t.Fatalf("scenario = %+v", validation.Scenario)
	}
	if len(validation.Directions) != 1 || validation.Directions[0].Verdict != "correct" {
		t.Fatalf("directions = %+v", validation.Directions)
	}
	if len(validation.Stocks) != 1 || validation.Stocks[0].Verdict != "correct" {
		t.Fatalf("stocks = %+v", validation.Stocks)
	}
	if validation.Coverage <= 0 || validation.Score <= 0 || validation.SummaryHash == "" {
		t.Fatalf("score metadata = %+v", validation)
	}
}

func TestBuildDailyValidationDoesNotScoreMissingDataAsWrong(t *testing.T) {
	validation := BuildDailyValidation(DailySummary{TradeDate: "2026-08-13", MarketRegime: "修复", TomorrowFocus: []DailyStockView{{Name: "未知股", Logic: "待观察"}}}, DailyValidationSnapshot{TradeDate: "2026-08-14"})
	if validation.Market.Verdict != "unverified" || validation.Stocks[0].Verdict != "unverified" {
		t.Fatalf("missing data should be unverified: %+v", validation)
	}
	if validation.Coverage != 0 || validation.Score != 0 {
		t.Fatalf("unverified data should not affect score: score=%v coverage=%v", validation.Score, validation.Coverage)
	}
}
