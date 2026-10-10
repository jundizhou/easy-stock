package portfolioinspection

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Schedules use China standard time, independent of the host's local timezone.
var scheduleZone = time.FixedZone("Asia/Shanghai", 8*60*60)

type ScheduleConfig struct {
	Enabled   bool     `json:"enabled"`
	Interval  int      `json:"interval"`
	Unit      string   `json:"unit"`
	StartDate string   `json:"start_date"`
	Time      string   `json:"time"`
	Channels  []string `json:"channels"`
	Request   Request  `json:"request"`
}

type Schedule struct {
	ScheduleConfig
	NextRunAt time.Time `json:"next_run_at"`
	LastRunAt time.Time `json:"last_run_at"`
	LastJobID string    `json:"last_job_id,omitempty"`
	LastError string    `json:"last_error,omitempty"`
	RetryAt   time.Time `json:"retry_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (c ScheduleConfig) anchor() (time.Time, error) {
	if c.Interval < 1 || c.Interval > 365 || !oneOf(c.Unit, "days", "weeks", "months") {
		return time.Time{}, errors.New("巡检间隔须为 1 至 365 天、周或月")
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", c.StartDate+" "+c.Time, scheduleZone)
	if err != nil || t.Format("2006-01-02") != c.StartDate || t.Format("15:04") != c.Time || t.Year() < 2000 || t.Year() > 9998 {
		return time.Time{}, errors.New("请选择有效的巡检开始日期与时间（北京时间）")
	}
	return t, nil
}

// NextScheduleTime returns the first slot strictly after after. Monthly slots
// retain the original day, clamping to month-end without drifting in March.
func NextScheduleTime(c ScheduleConfig, after time.Time) (time.Time, error) {
	anchor, err := c.anchor()
	if err != nil {
		return time.Time{}, err
	}
	if after.Before(anchor) {
		return anchor.UTC(), nil
	}
	if c.Unit != "months" {
		days := c.Interval
		if c.Unit == "weeks" {
			days *= 7
		}
		step := time.Duration(days) * 24 * time.Hour
		return anchor.Add((after.Sub(anchor)/step + 1) * step).UTC(), nil
	}
	local := after.In(scheduleZone)
	months := (local.Year()-anchor.Year())*12 + int(local.Month()-anchor.Month())
	n := months / c.Interval
	for {
		first := time.Date(anchor.Year(), anchor.Month()+time.Month(n*c.Interval), 1, anchor.Hour(), anchor.Minute(), 0, 0, scheduleZone)
		lastDay := first.AddDate(0, 1, -1).Day()
		day := anchor.Day()
		if day > lastDay {
			day = lastDay
		}
		candidate := first.AddDate(0, 0, day-1)
		if candidate.After(after) {
			return candidate.UTC(), nil
		}
		n++
	}
}

func (s *Store) GetSchedule(ctx context.Context, planID string) (Schedule, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT content_json FROM portfolio_inspection_schedules WHERE plan_id=?`, planID).Scan(&raw)
	if err != nil {
		return Schedule{}, err
	}
	var value Schedule
	err = json.Unmarshal([]byte(raw), &value)
	return value, err
}

func (s *Store) saveSchedule(ctx context.Context, value Schedule) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO portfolio_inspection_schedules(plan_id,content_json) VALUES(?,?) ON CONFLICT(plan_id) DO UPDATE SET content_json=excluded.content_json`, value.Request.PortfolioPlanID, string(raw))
	return err
}

func (s *Store) schedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM portfolio_inspection_schedules ORDER BY json_extract(content_json,'$.next_run_at'), plan_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Schedule{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var value Schedule
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// Persist the task and consume its scheduled slot together. A process restart
// must never launch the same slot again after a job has already been saved.
func (s *Store) saveScheduledJob(ctx context.Context, job Job, value Schedule) error {
	jobRaw, err := json.Marshal(job)
	if err != nil {
		return err
	}
	scheduleRaw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO portfolio_inspection_jobs(id,status,stage,content_json,started_at,updated_at,completed_at) VALUES(?,?,?,?,?,?,?)`, job.ID, job.Status, job.Stage, string(jobRaw), formatTime(job.StartedAt), formatTime(job.UpdatedAt), ""); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE portfolio_inspection_schedules SET content_json=? WHERE plan_id=?`, string(scheduleRaw), value.Request.PortfolioPlanID); err != nil {
		return err
	}
	return tx.Commit()
}

type Scheduler struct {
	mu      sync.Mutex
	store   *Store
	service *Service
	closed  bool
}

func NewScheduler(store *Store, service *Service) *Scheduler {
	return &Scheduler{store: store, service: service}
}

func (s *Scheduler) Save(ctx context.Context, id string, c ScheduleConfig, now time.Time) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.store == nil {
		return Schedule{}, errors.New("定时巡检服务不可用")
	}
	if id == "" || id != strings.TrimSpace(id) || len(id) > 128 || c.Request.PortfolioPlanID != id || len([]rune(c.Request.PortfolioPlanName)) > 40 {
		return Schedule{}, errors.New("持仓方案信息无效")
	}
	if _, err := c.anchor(); err != nil {
		return Schedule{}, err
	}
	c.Channels = append([]string{}, c.Channels...)
	seen := map[string]bool{}
	for _, channel := range c.Channels {
		if !oneOf(channel, "feishu", "dingtalk") || seen[channel] {
			return Schedule{}, errors.New("请选择有效且不重复的通知渠道")
		}
		seen[channel] = true
	}
	c.Request.SourceOptimizationID = ""
	c.Request.ForceSymbols = nil
	if c.Enabled {
		normalized, err := normalizeRequest(c.Request)
		if err != nil {
			return Schedule{}, err
		}
		c.Request = normalized
	}
	previous, err := s.store.GetSchedule(ctx, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, err
	}
	value := Schedule{ScheduleConfig: c, UpdatedAt: now.UTC(), LastRunAt: previous.LastRunAt, LastJobID: previous.LastJobID}
	if c.Enabled {
		if previous.Enabled && previous.Interval == c.Interval && previous.Unit == c.Unit && previous.StartDate == c.StartDate && previous.Time == c.Time {
			value.NextRunAt = previous.NextRunAt
		} else {
			value.NextRunAt, err = NextScheduleTime(c, now)
			if err != nil {
				return Schedule{}, err
			}
		}
	}
	return value, s.store.saveSchedule(ctx, value)
}

func (s *Scheduler) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.store == nil {
		return errors.New("定时巡检服务不可用")
	}
	_, err := s.store.db.ExecContext(ctx, `DELETE FROM portfolio_inspection_schedules WHERE plan_id=?`, id)
	return err
}

func (s *Scheduler) Close() { s.mu.Lock(); s.closed = true; s.mu.Unlock() }

func (s *Scheduler) Tick(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.store == nil || ctx.Err() != nil {
		return nil
	}
	values, err := s.store.schedules(ctx)
	if err != nil {
		return err
	}
	for _, value := range values {
		if !value.Enabled || value.NextRunAt.IsZero() || value.NextRunAt.After(now) || value.RetryAt.After(now) {
			continue
		}
		next, err := NextScheduleTime(value.ScheduleConfig, now)
		if err != nil {
			return err
		}
		updated := value
		updated.NextRunAt, updated.LastRunAt, updated.UpdatedAt = next, now.UTC(), now.UTC()
		updated.LastError, updated.RetryAt = "", time.Time{}
		_, err = s.service.start(ctx, value.Request, nil, &updated)
		if err == nil {
			continue
		}
		if errors.Is(err, ErrJobRunning) {
			return nil
		} // Keep the due slot until the running inspection finishes.
		value.LastError = "定时巡检启动失败，请检查模型配置与本地服务；5 分钟后重试"
		value.RetryAt, value.UpdatedAt = now.Add(5*time.Minute).UTC(), now.UTC()
		if err := s.store.saveSchedule(ctx, value); err != nil {
			return fmt.Errorf("save schedule retry: %w", err)
		}
	}
	return nil
}
