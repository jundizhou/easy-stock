package stockanalysis

import (
	"context"
	"encoding/json"
	"time"
)

// Enumerate the reuse index, never the UI's truncated history list.
func (s *ResearchStore) RecentReusable(ctx context.Context, asOf time.Time, limit int) ([]ResearchJob, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT j.content_json FROM stock_research_reuse_index i JOIN stock_research_jobs j ON j.id=i.id WHERE i.valid=1 AND i.completed_ns>? AND i.completed_ns<=? ORDER BY i.completed_ns DESC LIMIT ?`, asOf.Add(-ResearchReuseWindow).UnixNano(), asOf.UnixNano(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []ResearchJob{}
	seen := map[string]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var j ResearchJob
		if err := json.Unmarshal([]byte(raw), &j); err != nil {
			return nil, err
		}
		if SuccessfulResearch(j) && !seen[j.Request.Symbol] {
			seen[j.Request.Symbol] = true
			jobs = append(jobs, j)
		}
	}
	return jobs, rows.Err()
}
