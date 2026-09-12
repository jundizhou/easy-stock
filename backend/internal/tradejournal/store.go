package tradejournal

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
		dataSource = fmt.Sprintf("file:easy-stock-trade-journal-%d?mode=memory&cache=shared", time.Now().UnixNano())
	} else if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create trade journal data directory: %w", err)
	}
	db, err := sql.Open("sqlite", dataSource)
	if err != nil {
		return nil, fmt.Errorf("open trade journal database: %w", err)
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
		CREATE TABLE IF NOT EXISTS trade_journal_history (
			id TEXT PRIMARY KEY,
			filename TEXT NOT NULL DEFAULT '',
			analyzed_at TEXT NOT NULL,
			content_json TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS trade_journal_history_time ON trade_journal_history(analyzed_at DESC);
	`)
	if err != nil {
		return fmt.Errorf("migrate trade journal database: %w", err)
	}
	return nil
}

func (s *Store) Save(ctx context.Context, entry HistoryEntry) (HistoryEntry, error) {
	if entry.AnalyzedAt.IsZero() {
		entry.AnalyzedAt = time.Now().UTC()
	}
	content, err := json.Marshal(entry)
	if err != nil {
		return HistoryEntry{}, fmt.Errorf("encode trade journal entry: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO trade_journal_history (id,filename,analyzed_at,content_json) VALUES (?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET filename=excluded.filename,analyzed_at=excluded.analyzed_at,content_json=excluded.content_json`,
		entry.ID, entry.Filename, formatTime(entry.AnalyzedAt), string(content))
	if err != nil {
		return HistoryEntry{}, fmt.Errorf("save trade journal entry: %w", err)
	}
	return entry, nil
}

func (s *Store) List(ctx context.Context, limit int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 30 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT content_json FROM trade_journal_history ORDER BY analyzed_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list trade journal entries: %w", err)
	}
	defer rows.Close()
	entries := make([]HistoryEntry, 0, limit)
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		var entry HistoryEntry
		if err := json.Unmarshal([]byte(content), &entry); err != nil {
			return nil, fmt.Errorf("decode trade journal entry: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (HistoryEntry, error) {
	var content string
	if err := s.db.QueryRowContext(ctx, `SELECT content_json FROM trade_journal_history WHERE id=?`, strings.TrimSpace(id)).Scan(&content); err != nil {
		return HistoryEntry{}, err
	}
	var entry HistoryEntry
	if err := json.Unmarshal([]byte(content), &entry); err != nil {
		return HistoryEntry{}, fmt.Errorf("decode trade journal entry: %w", err)
	}
	return entry, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM trade_journal_history WHERE id=?`, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("delete trade journal entry: %w", err)
	}
	return nil
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
