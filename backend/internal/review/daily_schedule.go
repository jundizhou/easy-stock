package review

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

type DailyScheduleConfig struct {
	Enabled  bool     `json:"enabled"`
	Time     string   `json:"time"`
	Channels []string `json:"channels"`
}
type DailySchedule struct {
	DailyScheduleConfig
	NextRunAt       time.Time `json:"next_run_at"`
	TargetTradeDate string    `json:"target_trade_date"`
	LastRunAt       time.Time `json:"last_run_at"`
	LastTargetDate  string    `json:"last_target_date"`
	LastSummaryDate string    `json:"last_summary_date"`
	LastWindowStart time.Time `json:"last_window_start"`
	LastWindowEnd   time.Time `json:"last_window_end"`
	LastError       string    `json:"last_error,omitempty"`
	RetryAt         time.Time `json:"retry_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (s *Store) readScheduleState(ctx context.Context, key string, value any) error {
	var raw string
	if err := s.db.QueryRowContext(ctx, "SELECT content_json FROM review_schedule_state WHERE key=?", key).Scan(&raw); err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), value)
}
func (s *Store) writeScheduleState(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO review_schedule_state(key,content_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET content_json=excluded.content_json", key, string(raw))
	return err
}
func (s *Store) GetDailySchedule(ctx context.Context) (DailySchedule, error) {
	value := DailySchedule{DailyScheduleConfig: DailyScheduleConfig{Time: "22:00", Channels: []string{}}}
	err := s.readScheduleState(ctx, "daily", &value)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return value, err
}

type DailyScheduler struct {
	mu         sync.Mutex
	store      *Store
	automation *Automation
	calendar   TradingCalendar
	closed     bool
}

func NewDailyScheduler(store *Store, a *Automation, c TradingCalendar) *DailyScheduler {
	return &DailyScheduler{store: store, automation: a, calendar: c}
}
func (s *DailyScheduler) Close() { s.mu.Lock(); s.closed = true; s.mu.Unlock() }
func (s *DailyScheduler) Save(ctx context.Context, c DailyScheduleConfig, now time.Time) (DailySchedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return DailySchedule{}, errors.New("定时复盘服务已关闭")
	}
	parsed, err := time.Parse("15:04", c.Time)
	if err != nil || parsed.Format("15:04") != c.Time {
		return DailySchedule{}, errors.New("请选择有效的复盘时间（北京时间）")
	}
	seen := map[string]bool{}
	for _, ch := range c.Channels {
		if (ch != "feishu" && ch != "dingtalk") || seen[ch] {
			return DailySchedule{}, errors.New("请选择有效且不重复的通知渠道")
		}
		seen[ch] = true
	}
	old, err := s.store.GetDailySchedule(ctx)
	if err != nil {
		return DailySchedule{}, err
	}
	c.Channels = append([]string{}, c.Channels...)
	old.DailyScheduleConfig = c
	old.UpdatedAt = now
	old.RetryAt = time.Time{}
	old.LastError = ""
	old.NextRunAt = time.Time{}
	old.TargetTradeDate = ""
	if c.Enabled {
		old.NextRunAt, old.TargetTradeDate, err = s.next(ctx, c.Time, now, old.LastTargetDate)
		if err != nil {
			return DailySchedule{}, err
		}
	}
	return old, s.store.writeScheduleState(ctx, "daily", old)
}
func (s *DailyScheduler) next(ctx context.Context, clock string, after time.Time, lastTarget string) (time.Time, string, error) {
	parsed, _ := time.Parse("15:04", clock)
	for day, n := dateOnly(after), 0; n < 90; day, n = day.AddDate(0, 0, 1), n+1 {
		slot := day.Add(time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute)
		target := day.AddDate(0, 0, 1)
		label := target.Format("2006-01-02")
		if !slot.After(after) || label <= lastTarget {
			continue
		}
		open, err := s.calendar.IsTradingDay(ctx, target)
		if err != nil {
			return time.Time{}, "", err
		}
		if open {
			return slot.UTC(), label, nil
		}
	}
	return time.Time{}, "", errors.New("交易日历尚无未来可执行日期")
}

// Catch up only before the target market opens. Old forecasts must not be sent
// after that trading session has already begun.
func (s *DailyScheduler) Tick(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	v, err := s.store.GetDailySchedule(ctx)
	if err != nil || !v.Enabled {
		return err
	}
	if now.Before(v.RetryAt) {
		return nil
	}
	fail := func(e error) error {
		v.LastError = e.Error()
		v.RetryAt = now.Add(5 * time.Minute)
		_ = s.store.writeScheduleState(ctx, "daily", v)
		return e
	}
	if v.NextRunAt.IsZero() {
		v.NextRunAt, v.TargetTradeDate, err = s.next(ctx, v.Time, now, v.LastTargetDate)
		if err != nil {
			return fail(err)
		}
		return s.store.writeScheduleState(ctx, "daily", v)
	}
	if now.Before(v.NextRunAt) {
		return nil
	}
	target, err := time.ParseInLocation("2006-01-02", v.TargetTradeDate, shanghaiLocation())
	if err != nil {
		return fail(err)
	}
	open, err := s.calendar.IsTradingDay(ctx, target)
	if err != nil {
		return fail(err)
	}
	if !open || !now.Before(target.Add(9*time.Hour+30*time.Minute)) {
		v.NextRunAt, v.TargetTradeDate, err = s.next(ctx, v.Time, now, v.LastTargetDate)
		if err != nil {
			return fail(err)
		}
		v.LastError = ""
		v.RetryAt = time.Time{}
		return s.store.writeScheduleState(ctx, "daily", v)
	}
	// The report covers the latest completed close through the scheduled cutoff,
	// including intervening weekends and holidays, without future article times.
	session := dateOnly(v.NextRunAt)
	for n := 0; n < 90; n++ {
		available, e := s.calendar.IsTradingDay(ctx, session)
		if e != nil {
			return fail(e)
		}
		if available && session.Add(15*time.Hour).Before(v.NextRunAt) {
			break
		}
		session = session.AddDate(0, 0, -1)
		if n == 89 {
			return fail(errors.New("无法确定复盘文章的起始交易日"))
		}
	}
	window := reviewFreshnessWindow{TradeDate: session.Format("2006-01-02"), Start: session.Add(15 * time.Hour), End: v.NextRunAt.In(shanghaiLocation()), Rule: "自定义文章时间窗口"}
	next := v
	next.LastRunAt = now
	next.LastTargetDate = v.TargetTradeDate
	next.LastSummaryDate = window.TradeDate
	next.LastWindowStart = window.Start
	next.LastWindowEnd = window.End
	next.LastError = ""
	next.RetryAt = time.Time{}
	// Compute the following slot on the next tick, so an unavailable future
	// calendar never blocks a known valid run today.
	next.NextRunAt = time.Time{}
	next.TargetTradeDate = ""
	_, err = s.automation.startSummary(ctx, true, window, &next)
	if errors.Is(err, errDailySummaryBusy) {
		return nil
	}
	if err != nil {
		return fail(errors.New("定时复盘启动失败，请检查模型配置；5 分钟后重试"))
	}
	return nil
}

var errDailySummaryBusy = errors.New("已有复盘正在执行")

// The running job and consumed slot commit together, preventing duplicate
// launches across crashes between job creation and schedule advancement.
func (s *Store) saveScheduledSummary(ctx context.Context, job DailySummaryJob, v DailySchedule) (DailySummaryJob, error) {
	state, err := json.Marshal(v)
	if err != nil {
		return job, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return job, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_daily_summary_jobs(trade_date,window_start,window_end,freshness_rule,status,stage,message,started_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(trade_date) DO UPDATE SET window_start=excluded.window_start,window_end=excluded.window_end,freshness_rule=excluded.freshness_rule,status=excluded.status,stage=excluded.stage,message=excluded.message,started_at=excluded.started_at,updated_at=excluded.updated_at,completed_at='',error='',completed_authors=0,total_authors=0,article_count=0`, job.TradeDate, formatOptionalTime(job.WindowStart), formatOptionalTime(job.WindowEnd), job.FreshnessRule, job.Status, job.Stage, job.Message, formatOptionalTime(job.StartedAt), formatOptionalTime(job.UpdatedAt)); err != nil {
		return job, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_schedule_state(key,content_json) VALUES('daily',?) ON CONFLICT(key) DO UPDATE SET content_json=excluded.content_json`, string(state)); err != nil {
		return job, err
	}
	return job, tx.Commit()
}
