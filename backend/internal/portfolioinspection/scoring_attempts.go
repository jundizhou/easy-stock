package portfolioinspection

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Kept in a private diagnostic table, not the job JSON returned to the UI.
// Prompt includes the frozen evidence/action catalogs actually used by the call.
type scoringAttempt struct {
	Number          int            `json:"number"`
	PromptVersion   string         `json:"prompt_version"`
	ContractVersion string         `json:"contract_version"`
	EvaluatedAt     time.Time      `json:"evaluated_at"`
	RecordedAt      time.Time      `json:"recorded_at"`
	InputDigest     string         `json:"input_digest"`
	Prompt          string         `json:"prompt"`
	RawResponse     string         `json:"raw_response,omitempty"`
	Response        string         `json:"response"`
	Issues          []ScoringIssue `json:"issues,omitempty"`
}

func (s *Store) saveScoringAttempt(ctx context.Context, jobID string, attempt scoringAttempt) error {
	data, err := json.Marshal(attempt)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO portfolio_scoring_attempts(job_id,attempt,content_json) VALUES(?,?,?)`, jobID, attempt.Number, string(data))
	if err != nil {
		return fmt.Errorf("保存组合校验现场失败: %w", err)
	}
	return nil
}

func (s *Store) lastScoringAttempt(ctx context.Context, jobID string) (*scoringAttempt, error) {
	var data string
	if err := s.db.QueryRowContext(ctx, `SELECT content_json FROM portfolio_scoring_attempts WHERE job_id=? ORDER BY attempt DESC LIMIT 1`, jobID).Scan(&data); err != nil {
		return nil, err
	}
	var attempt scoringAttempt
	if err := json.Unmarshal([]byte(data), &attempt); err != nil {
		return nil, err
	}
	return &attempt, nil
}
