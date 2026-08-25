package portfolioinspection

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
		dataSource = fmt.Sprintf("file:easy-stock-portfolio-%d?mode=memory&cache=shared", time.Now().UnixNano())
	} else if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create portfolio inspection data directory: %w", err)
	}
	db, err := sql.Open("sqlite", dataSource)
	if err != nil {
		return nil, fmt.Errorf("open portfolio inspection database: %w", err)
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
		CREATE TABLE IF NOT EXISTS portfolio_inspection_jobs (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			stage TEXT NOT NULL,
			content_json TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			completed_at TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS portfolio_inspection_jobs_updated ON portfolio_inspection_jobs(updated_at DESC);
	`)
	if err != nil {
		return fmt.Errorf("migrate portfolio inspection database: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Save(ctx context.Context, job Job) (Job, error) {
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = time.Now().UTC()
	}
	content, err := json.Marshal(job)
	if err != nil {
		return Job{}, fmt.Errorf("encode portfolio inspection job: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO portfolio_inspection_jobs (id,status,stage,content_json,started_at,updated_at,completed_at)
		VALUES (?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,stage=excluded.stage,content_json=excluded.content_json,
		started_at=excluded.started_at,updated_at=excluded.updated_at,completed_at=excluded.completed_at`, job.ID, job.Status, job.Stage, string(content), formatTime(job.StartedAt), formatTime(job.UpdatedAt), formatTime(job.CompletedAt))
	if err != nil {
		return Job{}, fmt.Errorf("save portfolio inspection job: %w", err)
	}
	return job, nil
}

func (s *Store) Get(ctx context.Context, id string) (Job, error) {
	var content string
	if err := s.db.QueryRowContext(ctx, `SELECT content_json FROM portfolio_inspection_jobs WHERE id=?`, strings.TrimSpace(id)).Scan(&content); err != nil {
		return Job{}, err
	}
	var job Job
	if err := json.Unmarshal([]byte(content), &job); err != nil {
		return Job{}, fmt.Errorf("decode portfolio inspection job: %w", err)
	}
	return job, nil
}

func (s *Store) List(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 30 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM portfolio_inspection_jobs ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list portfolio inspection jobs: %w", err)
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
			return nil, fmt.Errorf("decode portfolio inspection job: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) MarkInterrupted(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM portfolio_inspection_jobs WHERE status='running'`)
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
		job.Error = "应用或后台服务曾重启，本次巡检已中断，可保留原持仓重新开始"
		job.Message = job.Error
		job.UpdatedAt = time.Now().UTC()
		job.CompletedAt = job.UpdatedAt
		if _, err := s.Save(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
