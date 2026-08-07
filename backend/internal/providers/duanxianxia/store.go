package duanxianxia

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
			return nil, fmt.Errorf("create theme radar data directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", dataSource)
	if err != nil {
		return nil, fmt.Errorf("open theme radar database: %w", err)
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
		CREATE TABLE IF NOT EXISTS duanxianxia_snapshots (
			id TEXT PRIMARY KEY,
			trade_date TEXT NOT NULL,
			fetched_at_ms INTEGER NOT NULL,
			payload_json TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_duanxianxia_snapshots_fetched_at
			ON duanxianxia_snapshots(fetched_at_ms DESC);
		CREATE TABLE IF NOT EXISTS duanxianxia_limit_up_snapshots (
			id TEXT PRIMARY KEY,
			trade_date TEXT NOT NULL,
			fetched_at_ms INTEGER NOT NULL,
			payload_json TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_duanxianxia_limit_up_snapshots_fetched_at
			ON duanxianxia_limit_up_snapshots(fetched_at_ms DESC);
		CREATE TABLE IF NOT EXISTS duanxianxia_sync_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			last_attempt_ms INTEGER NOT NULL DEFAULT 0,
			next_allowed_ms INTEGER NOT NULL DEFAULT 0,
			last_success_ms INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT ''
		);
		INSERT OR IGNORE INTO duanxianxia_sync_state(id) VALUES (1);
	`)
	if err != nil {
		return fmt.Errorf("migrate theme radar database: %w", err)
	}
	return nil
}

func (s *Store) TryBegin(ctx context.Context, now time.Time, interval time.Duration) (bool, SyncState, error) {
	nowMS := now.UnixMilli()
	result, err := s.db.ExecContext(ctx, `
		UPDATE duanxianxia_sync_state
		SET last_attempt_ms=?, next_allowed_ms=?
		WHERE id=1 AND next_allowed_ms<=?
	`, nowMS, now.Add(interval).UnixMilli(), nowMS)
	if err != nil {
		return false, SyncState{}, fmt.Errorf("reserve theme radar refresh: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, SyncState{}, err
	}
	state, err := s.State(ctx)
	return rows == 1, state, err
}

func (s *Store) MarkError(ctx context.Context, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE duanxianxia_sync_state SET last_error=? WHERE id=1`, strings.TrimSpace(message))
	if err != nil {
		return fmt.Errorf("save theme radar refresh error: %w", err)
	}
	return nil
}

func (s *Store) SaveSuccess(ctx context.Context, snapshot Snapshot) error {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode theme radar snapshot: %w", err)
	}
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO duanxianxia_snapshots(id, trade_date, fetched_at_ms, payload_json)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			trade_date=excluded.trade_date,
			fetched_at_ms=excluded.fetched_at_ms,
			payload_json=excluded.payload_json
	`, snapshot.ID, snapshot.TradeDate, snapshot.FetchedAt.UnixMilli(), string(payload)); err != nil {
		return fmt.Errorf("save theme radar snapshot: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE duanxianxia_sync_state
		SET last_success_ms=?, last_error=''
		WHERE id=1
	`, snapshot.FetchedAt.UnixMilli()); err != nil {
		return fmt.Errorf("save theme radar success state: %w", err)
	}
	// Keep one authoritative snapshot per trading day so yesterday's Kaipanla
	// theme memberships survive later five-minute refreshes.
	if _, err := transaction.ExecContext(ctx, `
		DELETE FROM duanxianxia_snapshots
		WHERE trade_date=? AND id<>?
	`, snapshot.TradeDate, snapshot.ID); err != nil {
		return fmt.Errorf("deduplicate theme radar snapshots: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		DELETE FROM duanxianxia_snapshots
		WHERE id NOT IN (
			SELECT id FROM duanxianxia_snapshots ORDER BY fetched_at_ms DESC LIMIT 16
		)
	`); err != nil {
		return fmt.Errorf("trim theme radar snapshots: %w", err)
	}
	return transaction.Commit()
}

func (s *Store) SaveLimitUpSuccess(ctx context.Context, snapshot LimitUpPoolSnapshot) error {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode limit-up pool snapshot: %w", err)
	}
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO duanxianxia_limit_up_snapshots(id, trade_date, fetched_at_ms, payload_json)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			trade_date=excluded.trade_date,
			fetched_at_ms=excluded.fetched_at_ms,
			payload_json=excluded.payload_json
	`, snapshot.ID, snapshot.TradeDate, snapshot.FetchedAt.UnixMilli(), string(payload)); err != nil {
		return fmt.Errorf("save limit-up pool snapshot: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE duanxianxia_sync_state
		SET last_success_ms=?, last_error=''
		WHERE id=1
	`, snapshot.FetchedAt.UnixMilli()); err != nil {
		return fmt.Errorf("save limit-up pool success state: %w", err)
	}
	// Keep one authoritative snapshot per trading day. Without this cleanup,
	// five-minute intraday refreshes would evict yesterday's Kaipanla pool.
	if _, err := transaction.ExecContext(ctx, `
		DELETE FROM duanxianxia_limit_up_snapshots
		WHERE trade_date=? AND id<>?
	`, snapshot.TradeDate, snapshot.ID); err != nil {
		return fmt.Errorf("deduplicate limit-up pool snapshots: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		DELETE FROM duanxianxia_limit_up_snapshots
		WHERE id NOT IN (
			SELECT id FROM duanxianxia_limit_up_snapshots ORDER BY fetched_at_ms DESC LIMIT 16
		)
	`); err != nil {
		return fmt.Errorf("trim limit-up pool snapshots: %w", err)
	}
	return transaction.Commit()
}

func (s *Store) Latest(ctx context.Context) (Snapshot, bool, error) {
	snapshots, err := s.Recent(ctx, 1)
	if err != nil {
		return Snapshot{}, false, err
	}
	if len(snapshots) == 0 {
		return Snapshot{}, false, nil
	}
	return snapshots[0], true, nil
}

func (s *Store) Recent(ctx context.Context, limit int) ([]Snapshot, error) {
	if limit <= 0 {
		limit = 16
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT snapshots.payload_json
		FROM duanxianxia_snapshots AS snapshots
		JOIN (
			SELECT trade_date, MAX(fetched_at_ms) AS fetched_at_ms
			FROM duanxianxia_snapshots
			GROUP BY trade_date
		) AS latest
		ON latest.trade_date=snapshots.trade_date AND latest.fetched_at_ms=snapshots.fetched_at_ms
		ORDER BY snapshots.trade_date DESC, snapshots.fetched_at_ms DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("read recent theme radar snapshots: %w", err)
	}
	defer rows.Close()
	snapshots := make([]Snapshot, 0, limit)
	seenDates := map[string]struct{}{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan recent theme radar snapshot: %w", err)
		}
		var snapshot Snapshot
		if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
			return nil, fmt.Errorf("decode theme radar snapshot: %w", err)
		}
		if _, exists := seenDates[snapshot.TradeDate]; exists {
			continue
		}
		seenDates[snapshot.TradeDate] = struct{}{}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent theme radar snapshots: %w", err)
	}
	return snapshots, nil
}

func (s *Store) Get(ctx context.Context, id string) (Snapshot, bool, error) {
	return s.readSnapshot(ctx, "SELECT payload_json FROM duanxianxia_snapshots WHERE id=?", id)
}

func (s *Store) LatestLimitUp(ctx context.Context) (LimitUpPoolSnapshot, bool, error) {
	snapshots, err := s.RecentLimitUps(ctx, 1)
	if err != nil {
		return LimitUpPoolSnapshot{}, false, err
	}
	if len(snapshots) == 0 {
		return LimitUpPoolSnapshot{}, false, nil
	}
	return snapshots[0], true, nil
}

func (s *Store) RecentLimitUps(ctx context.Context, limit int) ([]LimitUpPoolSnapshot, error) {
	if limit <= 0 {
		limit = 8
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT snapshots.payload_json
		FROM duanxianxia_limit_up_snapshots AS snapshots
		JOIN (
			SELECT trade_date, MAX(fetched_at_ms) AS fetched_at_ms
			FROM duanxianxia_limit_up_snapshots
			GROUP BY trade_date
		) AS latest
		ON latest.trade_date=snapshots.trade_date AND latest.fetched_at_ms=snapshots.fetched_at_ms
		ORDER BY snapshots.trade_date DESC, snapshots.fetched_at_ms DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("read recent limit-up pool snapshots: %w", err)
	}
	defer rows.Close()
	snapshots := make([]LimitUpPoolSnapshot, 0, limit)
	seenDates := map[string]struct{}{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan recent limit-up pool snapshot: %w", err)
		}
		var snapshot LimitUpPoolSnapshot
		if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
			return nil, fmt.Errorf("decode limit-up pool snapshot: %w", err)
		}
		if _, exists := seenDates[snapshot.TradeDate]; exists {
			continue
		}
		seenDates[snapshot.TradeDate] = struct{}{}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent limit-up pool snapshots: %w", err)
	}
	return snapshots, nil
}

func (s *Store) readSnapshot(ctx context.Context, query string, args ...any) (Snapshot, bool, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&payload)
	if err == sql.ErrNoRows {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("read theme radar snapshot: %w", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		return Snapshot{}, false, fmt.Errorf("decode theme radar snapshot: %w", err)
	}
	return snapshot, true, nil
}

func (s *Store) State(ctx context.Context) (SyncState, error) {
	var attemptMS, nextMS, successMS int64
	var state SyncState
	err := s.db.QueryRowContext(ctx, `
		SELECT last_attempt_ms, next_allowed_ms, last_success_ms, last_error
		FROM duanxianxia_sync_state WHERE id=1
	`).Scan(&attemptMS, &nextMS, &successMS, &state.LastError)
	if err != nil {
		return SyncState{}, fmt.Errorf("read theme radar sync state: %w", err)
	}
	state.LastAttemptAt = millisTime(attemptMS)
	state.NextAllowedAt = millisTime(nextMS)
	state.LastSuccessAt = millisTime(successMS)
	return state, nil
}

func millisTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value)
}
