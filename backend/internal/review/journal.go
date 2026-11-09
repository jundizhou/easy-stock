package review

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"
)

// Journals are user-authored notes, separate from imported articles and AI jobs.
type Journal struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	EntryDate string    `json:"entry_date"`
	Content   string    `json:"content"`
	Revision  int       `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

var ErrJournalConflict = errors.New("journal was edited elsewhere")

const journalSelect = `SELECT id, title, entry_date, content, revision, created_at, updated_at FROM review_journals`

func scanJournal(row interface{ Scan(...any) error }) (Journal, error) {
	var entry Journal
	var created, updated string
	err := row.Scan(&entry.ID, &entry.Title, &entry.EntryDate, &entry.Content, &entry.Revision, &created, &updated)
	if err != nil {
		return entry, err
	}
	entry.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return entry, err
	}
	entry.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return entry, err
}

func (s *Store) GetJournal(ctx context.Context, id string) (Journal, error) {
	return scanJournal(s.db.QueryRowContext(ctx, journalSelect+` WHERE id = ?`, id))
}

func (s *Store) CreateJournal(ctx context.Context, entry Journal) (Journal, error) {
	entry.ID = "journal-" + rand.Text()
	entry.CreatedAt = time.Now().UTC()
	entry.UpdatedAt = entry.CreatedAt
	entry.Revision = 1
	_, err := s.db.ExecContext(ctx, `INSERT INTO review_journals (id, title, entry_date, content, revision, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Title, entry.EntryDate, entry.Content, entry.Revision,
		entry.CreatedAt.Format(time.RFC3339Nano), entry.UpdatedAt.Format(time.RFC3339Nano))
	return entry, err
}

func (s *Store) UpdateJournal(ctx context.Context, entry Journal) (Journal, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE review_journals SET title = ?, entry_date = ?, content = ?, revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?`,
		entry.Title, entry.EntryDate, entry.Content, time.Now().UTC().Format(time.RFC3339Nano), entry.ID, entry.Revision)
	if err != nil {
		return Journal{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Journal{}, err
	}
	if count == 0 {
		if _, err := s.GetJournal(ctx, entry.ID); err != nil {
			return Journal{}, err
		}
		return Journal{}, ErrJournalConflict
	}
	return s.GetJournal(ctx, entry.ID)
}

func (s *Store) ListJournals(ctx context.Context, keyword string, limit, offset int) ([]Journal, int, error) {
	keyword = strings.TrimSpace(keyword)
	where := ` WHERE (? = '' OR instr(lower(title), lower(?)) > 0 OR instr(lower(content), lower(?)) > 0 OR instr(entry_date, ?) > 0)`
	args := []any{keyword, keyword, keyword, keyword}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_journals`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, journalSelect+where+` ORDER BY entry_date DESC, updated_at DESC, id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	entries := make([]Journal, 0)
	for rows.Next() {
		entry, err := scanJournal(rows)
		if err != nil {
			return nil, 0, err
		}
		entries = append(entries, entry)
	}
	return entries, total, rows.Err()
}
