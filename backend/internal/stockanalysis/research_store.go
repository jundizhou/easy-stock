package stockanalysis

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

type ResearchJob struct {
	ID           string                `json:"id"`
	Request      ResearchRequest       `json:"request"`
	Status       string                `json:"status"`
	Stage        string                `json:"stage"`
	Message      string                `json:"message"`
	Error        string                `json:"error,omitempty"`
	StartedAt    time.Time             `json:"started_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
	CompletedAt  *time.Time            `json:"completed_at,omitempty"`
	Analysis     *Analysis             `json:"analysis,omitempty"`
	Snapshot     *ResearchSnapshot     `json:"snapshot,omitempty"`
	Verification *ResearchVerification `json:"verification,omitempty"`
}

type ResearchJobSummary struct {
	ID        string          `json:"id"`
	Request   ResearchRequest `json:"request"`
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	Stage     string          `json:"stage"`
	Message   string          `json:"message"`
	Headline  string          `json:"headline"`
	Score     int             `json:"score"`
	StartedAt time.Time       `json:"started_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func (job ResearchJob) Public() ResearchJob {
	job.Snapshot = nil
	if job.Analysis != nil && job.Analysis.ResearchReport == nil {
		baseline := QuantitativeOnly(*job.Analysis)
		job.Analysis = &baseline
	}
	return job
}
func (job ResearchJob) Summary() ResearchJobSummary {
	item := ResearchJobSummary{ID: job.ID, Request: job.Request, Status: job.Status, Stage: job.Stage, Message: job.Message, StartedAt: job.StartedAt, UpdatedAt: job.UpdatedAt, Name: job.Request.Symbol}
	if job.Analysis != nil {
		item.Name = job.Analysis.Name
		if job.Analysis.ResearchReport != nil {
			item.Headline = job.Analysis.Conclusion.Headline
		}
		item.Score = job.Analysis.Scorecard.Overall
	}
	return item
}

type ResearchStore struct{ db *sql.DB }

func OpenResearchStore(path string) (*ResearchStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = ":memory:"
	}
	dsn := path
	if path == ":memory:" {
		dsn = "file:stock-research-" + NewResearchID() + "?mode=memory&cache=shared"
	} else if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000;
	CREATE TABLE IF NOT EXISTS stock_research_jobs(id TEXT PRIMARY KEY,status TEXT NOT NULL,updated_at TEXT NOT NULL,content_json TEXT NOT NULL);
	CREATE INDEX IF NOT EXISTS stock_research_recent ON stock_research_jobs(updated_at DESC);
	CREATE TABLE IF NOT EXISTS stock_research_snapshots(id TEXT NOT NULL,version INTEGER NOT NULL,content_json TEXT NOT NULL,PRIMARY KEY(id,version));
	CREATE TABLE IF NOT EXISTS stock_research_verifications(job_id TEXT NOT NULL,checked_at TEXT NOT NULL,content_json TEXT NOT NULL,PRIMARY KEY(job_id,checked_at));`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &ResearchStore{db: db}, nil
}

func (s *ResearchStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *ResearchStore) Save(ctx context.Context, job ResearchJob) error {
	encoded, err := json.Marshal(job)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO stock_research_jobs(id,status,updated_at,content_json) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,updated_at=excluded.updated_at,content_json=excluded.content_json`, job.ID, job.Status, job.UpdatedAt.Format(time.RFC3339Nano), string(encoded))
	if err != nil {
		return err
	}
	if job.Snapshot != nil {
		data, err := json.Marshal(job.Snapshot)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO stock_research_snapshots(id,version,content_json) VALUES(?,?,?)`, job.Snapshot.ID, job.Snapshot.Version, string(data)); err != nil {
			return err
		}
		var existing string
		if err = tx.QueryRowContext(ctx, `SELECT content_json FROM stock_research_snapshots WHERE id=? AND version=?`, job.Snapshot.ID, job.Snapshot.Version).Scan(&existing); err != nil {
			return err
		}
		if existing != string(data) {
			return fmt.Errorf("同一版本的研究快照不可改写")
		}
	}
	return tx.Commit()
}

func (s *ResearchStore) Get(ctx context.Context, id string) (ResearchJob, error) {
	var data string
	if err := s.db.QueryRowContext(ctx, `SELECT content_json FROM stock_research_jobs WHERE id=?`, id).Scan(&data); err != nil {
		return ResearchJob{}, err
	}
	var job ResearchJob
	if err := json.Unmarshal([]byte(data), &job); err != nil {
		return job, err
	}
	err := s.db.QueryRowContext(ctx, `SELECT content_json FROM stock_research_verifications WHERE job_id=? ORDER BY checked_at DESC LIMIT 1`, id).Scan(&data)
	if err == nil {
		var check ResearchVerification
		if json.Unmarshal([]byte(data), &check) == nil {
			job.Verification = &check
		}
	} else if err != sql.ErrNoRows {
		return job, err
	}
	return job, nil
}

func (s *ResearchStore) List(ctx context.Context, limit int) ([]ResearchJobSummary, error) {
	if limit < 1 || limit > 100 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM stock_research_jobs ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ResearchJobSummary{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var job ResearchJob
		if err := json.Unmarshal([]byte(data), &job); err != nil {
			return nil, err
		}
		items = append(items, job.Summary())
	}
	return items, rows.Err()
}

func (s *ResearchStore) MarkInterrupted(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM stock_research_jobs WHERE status IN ('queued','running')`)
	if err != nil {
		return err
	}
	jobs := []ResearchJob{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return err
		}
		var job ResearchJob
		if err := json.Unmarshal([]byte(data), &job); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, job)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		now := time.Now().UTC()
		job.Status = "interrupted"
		job.Stage = "interrupted"
		job.Message = "后台服务重启，研究已中断；已保存的量化数据仍可查看"
		job.CompletedAt = &now
		job.UpdatedAt = now
		if err := s.Save(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

func (s *ResearchStore) Delete(ctx context.Context, id string) error {
	job, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.Status == "running" || job.Status == "queued" {
		return fmt.Errorf("请先停止正在执行的研究")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM stock_research_jobs WHERE id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM stock_research_verifications WHERE job_id=?`, id); err != nil {
		return err
	}
	if job.Snapshot != nil {
		if _, err = tx.ExecContext(ctx, `DELETE FROM stock_research_snapshots WHERE id=?`, job.Snapshot.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *ResearchStore) SaveVerification(ctx context.Context, id string, verification ResearchVerification) error {
	data, err := json.Marshal(verification)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO stock_research_verifications(job_id,checked_at,content_json) SELECT ?,?,? WHERE EXISTS(SELECT 1 FROM stock_research_jobs WHERE id=? AND status NOT IN ('queued','running'))`, id, verification.CheckedAt.Format(time.RFC3339Nano), string(data), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return fmt.Errorf("报告不存在或尚未完成")
	}
	return err
}
