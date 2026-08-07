package marketemotion

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
	dataSource := strings.TrimSpace(path)
	if dataSource == "" {
		dataSource = ":memory:"
	}
	if dataSource != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dataSource), 0o700); err != nil {
			return nil, fmt.Errorf("create market emotion data directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", dataSource)
	if err != nil {
		return nil, fmt.Errorf("open market emotion database: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS market_emotion_snapshots (
			trade_date TEXT PRIMARY KEY,
			payload_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS market_emotion_sync_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			last_attempt_date TEXT NOT NULL DEFAULT '',
			last_success_date TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		);
		INSERT OR IGNORE INTO market_emotion_sync_state(id) VALUES (1);
	`)
	if err != nil {
		return fmt.Errorf("migrate market emotion database: %w", err)
	}
	return nil
}

func (s *Store) Upsert(ctx context.Context, snapshot Snapshot) error {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode market emotion snapshot: %w", err)
	}
	updatedAt := snapshot.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO market_emotion_snapshots(trade_date, payload_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(trade_date) DO UPDATE SET
			payload_json=excluded.payload_json,
			updated_at=excluded.updated_at
	`, snapshot.TradeDate, string(payload), updatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save market emotion snapshot: %w", err)
	}
	return nil
}

func (s *Store) List(ctx context.Context, limit int) ([]Snapshot, error) {
	if limit <= 0 || limit > 500 {
		limit = 120
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT payload_json FROM (
			SELECT trade_date, payload_json
			FROM market_emotion_snapshots
			ORDER BY trade_date DESC
			LIMIT ?
		) ORDER BY trade_date ASC
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list market emotion snapshots: %w", err)
	}
	defer rows.Close()
	items := make([]Snapshot, 0, limit)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var snapshot Snapshot
		if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
			return nil, fmt.Errorf("decode market emotion snapshot: %w", err)
		}
		items = append(items, snapshot)
	}
	return items, rows.Err()
}

func (s *Store) SyncState(ctx context.Context) (SyncState, error) {
	var state SyncState
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT last_attempt_date, last_success_date, last_error, updated_at
		FROM market_emotion_sync_state WHERE id = 1
	`).Scan(&state.LastAttemptDate, &state.LastSuccessDate, &state.LastError, &updatedAt)
	if err != nil {
		return SyncState{}, fmt.Errorf("read market emotion sync state: %w", err)
	}
	if updatedAt != "" {
		state.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	}
	return state, nil
}

func (s *Store) SaveSyncState(ctx context.Context, state SyncState) error {
	updatedAt := state.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE market_emotion_sync_state
		SET last_attempt_date=?, last_success_date=?, last_error=?, updated_at=?
		WHERE id=1
	`, state.LastAttemptDate, state.LastSuccessDate, state.LastError, updatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save market emotion sync state: %w", err)
	}
	return nil
}
