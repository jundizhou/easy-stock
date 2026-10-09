package portfolioinspection

import (
	"context"
	"encoding/json"
)

// Independent snapshots survive removal of source inspection/research history.
func (s *Store) InitOptimizations(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS portfolio_optimization_jobs(id TEXT PRIMARY KEY, source_id TEXT NOT NULL, fingerprint TEXT NOT NULL UNIQUE, content_json TEXT NOT NULL, updated_at TEXT NOT NULL); CREATE INDEX IF NOT EXISTS portfolio_optimization_source ON portfolio_optimization_jobs(source_id,updated_at DESC);`)
	return err
}
func (s *Store) SaveOptimization(ctx context.Context, id, source, fingerprint string, data []byte, updated string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO portfolio_optimization_jobs VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET content_json=excluded.content_json,updated_at=excluded.updated_at`, id, source, fingerprint, string(data), updated)
	return err
}
func (s *Store) GetOptimization(ctx context.Context, id string) (json.RawMessage, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT content_json FROM portfolio_optimization_jobs WHERE id=?`, id).Scan(&data)
	return json.RawMessage(data), err
}
func (s *Store) FindOptimization(ctx context.Context, fingerprint string) (json.RawMessage, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT content_json FROM portfolio_optimization_jobs WHERE fingerprint=?`, fingerprint).Scan(&data)
	return json.RawMessage(data), err
}
func (s *Store) ListOptimizations(ctx context.Context, source string) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM portfolio_optimization_jobs WHERE (?='' OR source_id=?) ORDER BY updated_at DESC`, source, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(data))
	}
	return out, rows.Err()
}

func (s *Store) OptimizationSummaries(ctx context.Context, source string) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT json_object('id',id,'source_id',source_id,'status',json_extract(content_json,'$.status'),'stage',json_extract(content_json,'$.stage'),'outcome',json_extract(content_json,'$.outcome'),'started_at',json_extract(content_json,'$.started_at'),'updated_at',updated_at) FROM portfolio_optimization_jobs WHERE source_id=? ORDER BY updated_at DESC LIMIT 20`, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(raw))
	}
	return out, rows.Err()
}
func (s *Store) RunningOptimizations(ctx context.Context) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM portfolio_optimization_jobs WHERE json_extract(content_json,'$.status')='running'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(raw))
	}
	return out, rows.Err()
}
