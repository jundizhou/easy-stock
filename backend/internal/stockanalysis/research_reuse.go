package stockanalysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"easy-stock/backend/internal/foundation"
)

const ResearchReuseWindow = 24 * time.Hour

// ResearchCompletedAt never uses UpdatedAt: viewing and verification cannot renew a report.
func ResearchCompletedAt(job ResearchJob) time.Time {
	if job.CompletedAt != nil {
		return *job.CompletedAt
	}
	if job.Analysis != nil && job.Analysis.ResearchReport != nil {
		return job.Analysis.ResearchReport.GeneratedAt
	}
	return time.Time{}
}

func SuccessfulResearch(job ResearchJob) bool {
	if job.Status != "succeeded" || job.Analysis == nil || job.Analysis.AI.Status != "ready" || job.Analysis.ResearchReport == nil {
		return false
	}
	r := job.Analysis.ResearchReport
	if r.Validation != "references_checked" || r.Headline == "" || r.Thesis.Text == "" || ResearchCompletedAt(job).IsZero() {
		return false
	}
	symbol, err := foundation.NormalizeSymbol(job.Request.Symbol)
	if err != nil {
		return false
	}
	actual, err := foundation.NormalizeSymbol(job.Analysis.Symbol)
	return err == nil && actual.Canonical == symbol.Canonical
}

type reuseExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func saveReuseIndex(ctx context.Context, db reuseExecutor, job ResearchJob) error {
	symbol, err := foundation.NormalizeSymbol(job.Request.Symbol)
	canonical := job.Request.Symbol
	if err == nil {
		canonical = symbol.Canonical
	}
	completed := int64(0)
	if t := ResearchCompletedAt(job); !t.IsZero() {
		completed = t.UnixNano()
	}
	_, err = db.ExecContext(ctx, `INSERT INTO stock_research_reuse_index(id,symbol,completed_ns,valid) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET symbol=excluded.symbol,completed_ns=excluded.completed_ns,valid=excluded.valid`, job.ID, canonical, completed, SuccessfulResearch(job))
	return err
}

func (s *ResearchStore) migrateReuseIndex(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS stock_research_reuse_index(id TEXT PRIMARY KEY,symbol TEXT NOT NULL,completed_ns INTEGER NOT NULL,valid INTEGER NOT NULL); CREATE INDEX IF NOT EXISTS stock_research_reuse_lookup ON stock_research_reuse_index(symbol,valid,completed_ns DESC);`); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT j.content_json FROM stock_research_jobs j LEFT JOIN stock_research_reuse_index i ON i.id=j.id WHERE i.id IS NULL`)
	if err != nil {
		return err
	}
	var jobs []ResearchJob
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return err
		}
		var job ResearchJob
		if err := json.Unmarshal([]byte(data), &job); err != nil {
			rows.Close()
			return fmt.Errorf("迁移个股报告索引: %w", err)
		}
		jobs = append(jobs, job)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, job := range jobs {
		if err := saveReuseIndex(ctx, tx, job); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *ResearchStore) FindReusable(ctx context.Context, symbol string, asOf time.Time) (ResearchJob, error) {
	return s.findReusable(ctx, symbol, asOf, asOf)
}

func (s *ResearchStore) findReusable(ctx context.Context, symbol string, asOf, until time.Time) (ResearchJob, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return ResearchJob{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT j.content_json FROM stock_research_reuse_index i JOIN stock_research_jobs j ON j.id=i.id WHERE i.symbol=? AND i.valid=1 AND i.completed_ns>? AND i.completed_ns<=? ORDER BY i.completed_ns DESC`, normalized.Canonical, asOf.Add(-ResearchReuseWindow).UnixNano(), until.UnixNano())
	if err != nil {
		return ResearchJob{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return ResearchJob{}, err
		}
		var job ResearchJob
		if err := json.Unmarshal([]byte(data), &job); err != nil {
			return ResearchJob{}, err
		}
		if SuccessfulResearch(job) {
			return RevalidateReusableResearch(job), nil
		}
	}
	if err := rows.Err(); err != nil {
		return ResearchJob{}, err
	}
	return ResearchJob{}, sql.ErrNoRows
}

// ResolveForPortfolio atomically reuses a report, joins a live task, or creates one.
// A portfolio owns its wait, never the lifetime of a shared stock research task.
func (s *ResearchService) ResolveForPortfolio(ctx context.Context, request ResearchRequest, asOf time.Time, force bool, resumeID string) (ResearchJob, string, error) {
	request, err := NormalizeResearchRequest(request)
	if err != nil {
		return ResearchJob{}, "", err
	}
	if s == nil || s.store == nil || s.run == nil {
		return ResearchJob{}, "", errors.New("研究任务服务不可用")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initializationError != nil {
		return ResearchJob{}, "", s.initializationError
	}
	if s.closed {
		return ResearchJob{}, "", errors.New("研究服务正在关闭")
	}
	if !force {
		job, err := s.store.findReusable(ctx, request.Symbol, asOf, time.Now().UTC())
		if err == nil {
			return job, "reused", nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ResearchJob{}, "", fmt.Errorf("读取可复用个股报告失败: %w", err)
		}
	}
	for _, active := range s.active {
		if active.symbol != request.Symbol {
			continue
		}
		job, err := s.store.Get(ctx, active.id)
		if err != nil {
			return ResearchJob{}, "", err
		}
		// A completed task may still be removing itself from the active registry.
		if job.Status == "queued" || job.Status == "running" {
			return job, "shared_running", nil
		}
		if !force && SuccessfulResearch(job) && asOf.Sub(ResearchCompletedAt(job)) < ResearchReuseWindow && !ResearchCompletedAt(job).After(asOf) {
			return RevalidateReusableResearch(job), "reused", nil
		}
	}
	var resume *ResearchJob
	if resumeID != "" && !force {
		previous, err := s.store.Get(ctx, resumeID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ResearchJob{}, "", err
		}
		if err == nil && previous.Request.Symbol == request.Symbol && previous.Public().ResumeAvailable && previous.Analysis != nil {
			resume = &previous
			request = previous.Request
		}
	}
	job, err := s.startLocked(ctx, request, resume)
	return job, "new", err
}
