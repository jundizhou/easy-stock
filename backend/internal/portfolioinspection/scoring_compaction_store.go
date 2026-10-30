package portfolioinspection

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type compactionHooks struct {
	load     func(context.Context, string) (string, bool, error)
	save     func(context.Context, string, string) error
	progress func(string)
}
type compactionHooksKey struct{}

func (s *Store) compactionHooks(jobID string, progress func(string)) *compactionHooks {
	return &compactionHooks{
		load: func(ctx context.Context, digest string) (string, bool, error) {
			var summary string
			err := s.db.QueryRowContext(ctx, `SELECT summary FROM portfolio_compaction_checkpoints WHERE job_id=? AND input_digest=?`, jobID, digest).Scan(&summary)
			if errors.Is(err, sql.ErrNoRows) {
				return "", false, nil
			}
			return summary, err == nil && validCompactionSummary(summary), err
		},
		save: func(ctx context.Context, digest, summary string) error {
			// A completed chunk survives a cancellation racing its response.
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_, err := s.db.ExecContext(saveCtx, `INSERT INTO portfolio_compaction_checkpoints(job_id,input_digest,summary) VALUES(?,?,?) ON CONFLICT(job_id,input_digest) DO UPDATE SET summary=excluded.summary`, jobID, digest, summary)
			return err
		},
		progress: progress,
	}
}

// Copy private checkpoints so each resumed job is independent of deletion of
// its predecessor. A checkpoint is reused only after matching its full digest.
func (s *Store) copyCompactionCheckpoints(ctx context.Context, from, to string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO portfolio_compaction_checkpoints(job_id,input_digest,summary) SELECT ?,input_digest,summary FROM portfolio_compaction_checkpoints WHERE job_id=?`, to, from)
	return err
}
