package dailyanalysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = ":memory:"
	}
	dataSource := path
	if path == ":memory:" {
		dataSource = fmt.Sprintf("file:easy-stock-daily-analysis-%d?mode=memory&cache=shared", time.Now().UnixNano())
	} else if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create daily analysis data directory: %w", err)
	}
	db, err := sql.Open("sqlite", dataSource)
	if err != nil {
		return nil, fmt.Errorf("open daily analysis database: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS daily_analysis_jobs (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			stage TEXT NOT NULL,
			trigger_kind TEXT NOT NULL DEFAULT '',
			content_json TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			completed_at TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS daily_analysis_jobs_updated ON daily_analysis_jobs(updated_at DESC);
		CREATE TABLE IF NOT EXISTS daily_analysis_config (
			id TEXT PRIMARY KEY,
			content_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("migrate daily analysis database: %w", err)
	}
	return nil
}

func (s *Store) Save(ctx context.Context, job Job) (Job, error) {
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = time.Now().UTC()
	}
	content, err := json.Marshal(job)
	if err != nil {
		return Job{}, fmt.Errorf("encode daily analysis job: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO daily_analysis_jobs (id,status,stage,trigger_kind,content_json,started_at,updated_at,completed_at)
		VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,stage=excluded.stage,content_json=excluded.content_json,
		started_at=excluded.started_at,updated_at=excluded.updated_at,completed_at=excluded.completed_at`,
		job.ID, job.Status, job.Stage, job.Trigger, string(content), formatTime(job.StartedAt), formatTime(job.UpdatedAt), formatTime(job.CompletedAt))
	if err != nil {
		return Job{}, fmt.Errorf("save daily analysis job: %w", err)
	}
	return job, nil
}

func (s *Store) Get(ctx context.Context, id string) (Job, error) {
	var content string
	if err := s.db.QueryRowContext(ctx, `SELECT content_json FROM daily_analysis_jobs WHERE id=?`, strings.TrimSpace(id)).Scan(&content); err != nil {
		return Job{}, err
	}
	var job Job
	if err := json.Unmarshal([]byte(content), &job); err != nil {
		return Job{}, fmt.Errorf("decode daily analysis job: %w", err)
	}
	return job, nil
}

func (s *Store) List(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 30 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM daily_analysis_jobs ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list daily analysis jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]Job, 0, limit)
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		var job Job
		if err := json.Unmarshal([]byte(content), &job); err != nil {
			return nil, fmt.Errorf("decode daily analysis job: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) MarkInterrupted(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM daily_analysis_jobs WHERE status='running'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return err
		}
		var job Job
		if json.Unmarshal([]byte(content), &job) == nil {
			jobs = append(jobs, job)
		}
	}
	for _, job := range jobs {
		job.Status = "interrupted"
		job.Stage = "interrupted"
		job.Error = "应用或后台服务曾重启，本次自选股日报已中断，可重新开始"
		job.Message = job.Error
		job.UpdatedAt = time.Now().UTC()
		job.CompletedAt = job.UpdatedAt
		if _, err := s.Save(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LoadConfig(ctx context.Context) (Config, error) {
	var content string
	err := s.db.QueryRowContext(ctx, `SELECT content_json FROM daily_analysis_config WHERE id=?`, configKey).Scan(&content)
	if err == sql.ErrNoRows {
		return Config{RunHour: 15, RunMinute: 30}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal([]byte(content), &config); err != nil {
		return Config{}, fmt.Errorf("decode daily analysis config: %w", err)
	}
	if config.RunHour <= 0 && config.RunMinute <= 0 && !config.AutoRun {
		config.RunHour, config.RunMinute = 15, 30
	}
	return config, nil
}

func (s *Store) SaveConfig(ctx context.Context, config Config) (Config, error) {
	content, err := json.Marshal(config)
	if err != nil {
		return Config{}, fmt.Errorf("encode daily analysis config: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO daily_analysis_config (id,content_json,updated_at) VALUES (?,?,?)
		ON CONFLICT(id) DO UPDATE SET content_json=excluded.content_json,updated_at=excluded.updated_at`,
		configKey, string(content), formatTime(time.Now().UTC()))
	if err != nil {
		return Config{}, fmt.Errorf("save daily analysis config: %w", err)
	}
	return config, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
