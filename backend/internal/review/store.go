package review

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	if strings.TrimSpace(path) == "" {
		path = ":memory:"
	}
	dataSource := path
	if path == ":memory:" {
		dataSource = "file:easy-stock-reviews?mode=memory&cache=shared"
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create review data directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", dataSource)
	if err != nil {
		return nil, fmt.Errorf("open review database: %w", err)
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
		CREATE TABLE IF NOT EXISTS review_posts (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			external_id TEXT NOT NULL,
			author_id TEXT NOT NULL,
			author_name TEXT NOT NULL,
			title TEXT NOT NULL,
			digest TEXT NOT NULL DEFAULT '',
			content_text TEXT NOT NULL DEFAULT '',
			cover_url TEXT NOT NULL DEFAULT '',
			original_url TEXT NOT NULL UNIQUE,
			published_at TEXT NOT NULL,
			fetched_at TEXT NOT NULL,
			related_stocks TEXT NOT NULL DEFAULT '[]',
			related_themes TEXT NOT NULL DEFAULT '[]',
			read_status INTEGER NOT NULL DEFAULT 0,
			favorite_status INTEGER NOT NULL DEFAULT 0,
			ai_summary TEXT NOT NULL DEFAULT '',
			ai_key_points TEXT NOT NULL DEFAULT '[]',
			ai_outlook TEXT NOT NULL DEFAULT '',
			ai_analyzed_at TEXT NOT NULL DEFAULT '',
			ai_error TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS review_posts_source_time ON review_posts(source, published_at DESC);
		CREATE INDEX IF NOT EXISTS review_posts_author_time ON review_posts(author_id, published_at DESC);
		CREATE TABLE IF NOT EXISTS review_deleted_authors (
			source TEXT NOT NULL,
			author_id TEXT NOT NULL,
			author_name TEXT NOT NULL DEFAULT '',
			deleted_at TEXT NOT NULL,
			PRIMARY KEY(source, author_id)
		);
		CREATE TABLE IF NOT EXISTS review_subscriptions (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			name TEXT NOT NULL,
			homepage_url TEXT NOT NULL,
			external_id TEXT NOT NULL DEFAULT '',
			config_id TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			last_sync_at TEXT NOT NULL DEFAULT '',
			next_sync_at TEXT NOT NULL DEFAULT '',
			last_status TEXT NOT NULL DEFAULT 'pending',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			UNIQUE(source, homepage_url)
		);
		CREATE TABLE IF NOT EXISTS review_daily_summaries (
			trade_date TEXT PRIMARY KEY,
			content_json TEXT NOT NULL,
			article_count INTEGER NOT NULL,
			author_count INTEGER NOT NULL,
			generated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS review_daily_summary_jobs (
			trade_date TEXT PRIMARY KEY,
			window_start TEXT NOT NULL DEFAULT '',
			window_end TEXT NOT NULL DEFAULT '',
			freshness_rule TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			stage TEXT NOT NULL,
			completed_authors INTEGER NOT NULL DEFAULT 0,
			total_authors INTEGER NOT NULL DEFAULT 0,
			article_count INTEGER NOT NULL DEFAULT 0,
			message TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			completed_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS review_daily_validations (
			summary_date TEXT PRIMARY KEY,
			verification_date TEXT NOT NULL DEFAULT '',
			content_json TEXT NOT NULL,
			score REAL NOT NULL DEFAULT 0,
			coverage REAL NOT NULL DEFAULT 0,
			generated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS review_daily_validation_jobs (
			summary_date TEXT PRIMARY KEY,
			verification_date TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			stage TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			completed_at TEXT NOT NULL DEFAULT ''
		);
	`)
	if err != nil {
		return fmt.Errorf("migrate review database: %w", err)
	}
	for _, statement := range []string{
		`ALTER TABLE review_posts ADD COLUMN ai_summary TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE review_posts ADD COLUMN ai_key_points TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE review_posts ADD COLUMN ai_outlook TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE review_posts ADD COLUMN ai_analyzed_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE review_posts ADD COLUMN ai_error TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE review_subscriptions ADD COLUMN config_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE review_daily_summary_jobs ADD COLUMN window_start TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE review_daily_summary_jobs ADD COLUMN window_end TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE review_daily_summary_jobs ADD COLUMN freshness_rule TEXT NOT NULL DEFAULT ''`,
	} {
		if _, alterErr := s.db.ExecContext(ctx, statement); alterErr != nil && !strings.Contains(strings.ToLower(alterErr.Error()), "duplicate column") {
			return fmt.Errorf("migrate review database columns: %w", alterErr)
		}
	}
	return nil
}

func (s *Store) UpsertPost(ctx context.Context, post Post) (Post, error) {
	stocks, _ := json.Marshal(nonNilStrings(post.RelatedStocks))
	themes, _ := json.Marshal(nonNilStrings(post.RelatedThemes))
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO review_posts (
			id, source, external_id, author_id, author_name, title, digest, content_text,
			cover_url, original_url, published_at, fetched_at, related_stocks, related_themes,
			read_status, favorite_status, ai_summary, ai_key_points, ai_outlook, ai_analyzed_at, ai_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(original_url) DO UPDATE SET
			source=excluded.source,
			external_id=excluded.external_id,
			author_id=excluded.author_id,
			author_name=excluded.author_name,
			title=excluded.title,
			digest=excluded.digest,
			content_text=excluded.content_text,
			cover_url=excluded.cover_url,
			published_at=excluded.published_at,
			fetched_at=excluded.fetched_at,
			related_stocks=excluded.related_stocks,
			related_themes=excluded.related_themes,
			ai_summary=CASE WHEN excluded.ai_summary != '' THEN excluded.ai_summary ELSE review_posts.ai_summary END,
			ai_key_points=CASE WHEN excluded.ai_key_points != '[]' THEN excluded.ai_key_points ELSE review_posts.ai_key_points END,
			ai_outlook=CASE WHEN excluded.ai_outlook != '' THEN excluded.ai_outlook ELSE review_posts.ai_outlook END,
			ai_analyzed_at=CASE WHEN excluded.ai_analyzed_at != '' THEN excluded.ai_analyzed_at ELSE review_posts.ai_analyzed_at END,
			ai_error=excluded.ai_error
	`, post.ID, post.Source, post.ExternalID, post.AuthorID, post.AuthorName, post.Title, post.Digest,
		post.ContentText, post.CoverURL, post.OriginalURL, post.PublishedAt.UTC().Format(time.RFC3339),
		post.FetchedAt.UTC().Format(time.RFC3339), string(stocks), string(themes), boolInt(post.Read), boolInt(post.Favorite),
		post.AISummary, mustJSON(post.AIKeyPoints), post.AIOutlook, formatOptionalTime(post.AIAnalyzedAt), post.AIError)
	if err != nil {
		return Post{}, fmt.Errorf("save review post: %w", err)
	}
	return s.GetPostByURL(ctx, post.OriginalURL)
}

func (s *Store) GetPost(ctx context.Context, id string) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, postSelect+` WHERE id = ?`, id))
}

func (s *Store) GetPostByURL(ctx context.Context, originalURL string) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, postSelect+` WHERE original_url = ?`, originalURL))
}

func (s *Store) DeletePost(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return sql.ErrNoRows
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM review_posts WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete review post: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted review post count: %w", err)
	}
	if deleted == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteAuthor(ctx context.Context, source, authorID string) (AuthorDeleteResult, error) {
	source = strings.TrimSpace(source)
	authorID = strings.TrimSpace(authorID)
	if source == "" || authorID == "" {
		return AuthorDeleteResult{}, sql.ErrNoRows
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthorDeleteResult{}, fmt.Errorf("begin delete review author: %w", err)
	}
	defer tx.Rollback()

	var authorName string
	if err := tx.QueryRowContext(ctx, `SELECT author_name FROM review_posts WHERE source=? AND author_id=? ORDER BY published_at DESC LIMIT 1`, source, authorID).Scan(&authorName); err != nil {
		return AuthorDeleteResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO review_deleted_authors (source,author_id,author_name,deleted_at)
		VALUES(?,?,?,?) ON CONFLICT(source,author_id) DO UPDATE SET author_name=excluded.author_name,deleted_at=excluded.deleted_at`,
		source, authorID, authorName, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return AuthorDeleteResult{}, fmt.Errorf("remember deleted review author: %w", err)
	}
	postResult, err := tx.ExecContext(ctx, `DELETE FROM review_posts WHERE source=? AND author_id=?`, source, authorID)
	if err != nil {
		return AuthorDeleteResult{}, fmt.Errorf("delete review author posts: %w", err)
	}
	postsDeleted, err := postResult.RowsAffected()
	if err != nil {
		return AuthorDeleteResult{}, fmt.Errorf("read deleted review author post count: %w", err)
	}
	subscriptionsDeleted, err := deleteAuthorSubscriptions(ctx, tx, source, authorID)
	if err != nil {
		return AuthorDeleteResult{}, err
	}
	for _, table := range []string{"review_daily_summaries", "review_daily_summary_jobs", "review_daily_validations", "review_daily_validation_jobs"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table); err != nil {
			return AuthorDeleteResult{}, fmt.Errorf("clear review summary cache %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return AuthorDeleteResult{}, fmt.Errorf("commit delete review author: %w", err)
	}
	return AuthorDeleteResult{
		AuthorID: authorID, AuthorName: authorName, Source: source,
		PostsDeleted: postsDeleted, SubscriptionsDeleted: subscriptionsDeleted, SummaryCacheCleared: true,
	}, nil
}

func (s *Store) IsAuthorDeleted(ctx context.Context, source, authorID string) (bool, error) {
	var deleted int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM review_deleted_authors WHERE source=? AND author_id=?)`, strings.TrimSpace(source), strings.TrimSpace(authorID)).Scan(&deleted)
	if err != nil {
		return false, fmt.Errorf("check deleted review author: %w", err)
	}
	return deleted == 1, nil
}

func deleteAuthorSubscriptions(ctx context.Context, tx *sql.Tx, source, authorID string) (int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,name,homepage_url,external_id FROM review_subscriptions WHERE source=?`, source)
	if err != nil {
		return 0, fmt.Errorf("list review author subscriptions: %w", err)
	}
	type subscriptionIdentity struct{ id, name, homepageURL, externalID string }
	items := []subscriptionIdentity{}
	for rows.Next() {
		var item subscriptionIdentity
		if err := rows.Scan(&item.id, &item.name, &item.homepageURL, &item.externalID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan review author subscription: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close review author subscriptions: %w", err)
	}
	deleted := int64(0)
	for _, item := range items {
		identityID := stableID(source + "\n" + firstValue(item.externalID, item.homepageURL))[:24]
		nameID := stableID(source + "\n" + strings.TrimSpace(item.name))[:24]
		if authorID != identityID && authorID != nameID {
			continue
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM review_subscriptions WHERE id=?`, item.id)
		if err != nil {
			return 0, fmt.Errorf("delete review author subscription: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("read deleted review author subscription count: %w", err)
		}
		deleted += count
	}
	return deleted, nil
}

func (s *Store) HasPostBySourceExternalID(ctx context.Context, source, externalID string) (bool, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM review_posts WHERE source=? AND external_id=?)`, strings.TrimSpace(source), strings.TrimSpace(externalID)).Scan(&exists); err != nil {
		return false, fmt.Errorf("check review post: %w", err)
	}
	return exists == 1, nil
}

func (s *Store) ListPosts(ctx context.Context, query Query) ([]Post, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if source := strings.TrimSpace(query.Source); source != "" && source != "all" {
		where = append(where, "source = ?")
		args = append(args, source)
	}
	if authorID := strings.TrimSpace(query.AuthorID); authorID != "" && authorID != "all" {
		where = append(where, "author_id = ?")
		args = append(args, authorID)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		where = append(where, "(title LIKE ? OR digest LIKE ? OR content_text LIKE ? OR author_name LIKE ?)")
		pattern := "%" + keyword + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}
	condition := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_posts WHERE `+condition, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count review posts: %w", err)
	}
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset := max(query.Offset, 0)
	rows, err := s.db.QueryContext(ctx, postSelect+` WHERE `+condition+` ORDER BY published_at DESC, fetched_at DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list review posts: %w", err)
	}
	defer rows.Close()
	posts := []Post{}
	for rows.Next() {
		post, scanErr := scanPost(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		posts = append(posts, post)
	}
	return posts, total, rows.Err()
}

func (s *Store) ListPostsBetween(ctx context.Context, start, end time.Time, limit int) ([]Post, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, postSelect+` WHERE published_at >= ? AND published_at < ? ORDER BY published_at DESC, fetched_at DESC LIMIT ?`,
		start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, fmt.Errorf("list review posts between dates: %w", err)
	}
	defer rows.Close()
	posts := []Post{}
	for rows.Next() {
		post, scanErr := scanPost(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		posts = append(posts, post)
	}
	return posts, rows.Err()
}

func (s *Store) SaveDailySummary(ctx context.Context, summary DailySummary) (DailySummary, error) {
	content, err := json.Marshal(summary)
	if err != nil {
		return DailySummary{}, fmt.Errorf("encode daily review summary: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO review_daily_summaries (trade_date,content_json,article_count,author_count,generated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(trade_date) DO UPDATE SET content_json=excluded.content_json, article_count=excluded.article_count, author_count=excluded.author_count, generated_at=excluded.generated_at`,
		summary.TradeDate, string(content), summary.ArticleCount, summary.AuthorCount, summary.GeneratedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return DailySummary{}, fmt.Errorf("save daily review summary: %w", err)
	}
	return s.GetDailySummary(ctx, summary.TradeDate)
}

func (s *Store) GetDailySummary(ctx context.Context, tradeDate string) (DailySummary, error) {
	var content string
	if err := s.db.QueryRowContext(ctx, `SELECT content_json FROM review_daily_summaries WHERE trade_date=?`, strings.TrimSpace(tradeDate)).Scan(&content); err != nil {
		return DailySummary{}, err
	}
	var summary DailySummary
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
		return DailySummary{}, fmt.Errorf("decode daily review summary: %w", err)
	}
	return summary, nil
}

func (s *Store) LatestDailySummaryBefore(ctx context.Context, beforeDate string) (DailySummary, error) {
	var content string
	query := `SELECT content_json FROM review_daily_summaries`
	args := []any{}
	if strings.TrimSpace(beforeDate) != "" {
		query += ` WHERE trade_date < ?`
		args = append(args, strings.TrimSpace(beforeDate))
	}
	query += ` ORDER BY trade_date DESC LIMIT 1`
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&content); err != nil {
		return DailySummary{}, err
	}
	var summary DailySummary
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
		return DailySummary{}, fmt.Errorf("decode latest daily review summary: %w", err)
	}
	return summary, nil
}

func (s *Store) SaveDailyValidation(ctx context.Context, validation DailyValidation) (DailyValidation, error) {
	content, err := json.Marshal(validation)
	if err != nil {
		return DailyValidation{}, fmt.Errorf("encode daily review validation: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO review_daily_validations (summary_date,verification_date,content_json,score,coverage,generated_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(summary_date) DO UPDATE SET verification_date=excluded.verification_date, content_json=excluded.content_json, score=excluded.score, coverage=excluded.coverage, generated_at=excluded.generated_at`,
		validation.SummaryDate, validation.VerificationDate, string(content), validation.Score, validation.Coverage, validation.GeneratedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return DailyValidation{}, fmt.Errorf("save daily review validation: %w", err)
	}
	return s.GetDailyValidation(ctx, validation.SummaryDate)
}

func (s *Store) GetDailyValidation(ctx context.Context, summaryDate string) (DailyValidation, error) {
	var content string
	if err := s.db.QueryRowContext(ctx, `SELECT content_json FROM review_daily_validations WHERE summary_date=?`, strings.TrimSpace(summaryDate)).Scan(&content); err != nil {
		return DailyValidation{}, err
	}
	var validation DailyValidation
	if err := json.Unmarshal([]byte(content), &validation); err != nil {
		return DailyValidation{}, fmt.Errorf("decode daily review validation: %w", err)
	}
	return validation, nil
}

func (s *Store) SaveDailyValidationJob(ctx context.Context, job DailyValidationJob) (DailyValidationJob, error) {
	now := time.Now().UTC()
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO review_daily_validation_jobs (
		summary_date,verification_date,status,stage,message,error,started_at,updated_at,completed_at
	) VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(summary_date) DO UPDATE SET
		verification_date=excluded.verification_date, status=excluded.status, stage=excluded.stage,
		message=excluded.message, error=excluded.error, started_at=excluded.started_at,
		updated_at=excluded.updated_at, completed_at=excluded.completed_at`,
		job.SummaryDate, job.VerificationDate, job.Status, job.Stage, job.Message, job.Error,
		formatOptionalTime(job.StartedAt), formatOptionalTime(job.UpdatedAt), formatOptionalTime(job.CompletedAt))
	if err != nil {
		return DailyValidationJob{}, fmt.Errorf("save daily validation job: %w", err)
	}
	return s.GetDailyValidationJob(ctx, job.SummaryDate)
}

func (s *Store) GetDailyValidationJob(ctx context.Context, summaryDate string) (DailyValidationJob, error) {
	var job DailyValidationJob
	var startedAt, updatedAt, completedAt string
	err := s.db.QueryRowContext(ctx, `SELECT summary_date,verification_date,status,stage,message,error,started_at,updated_at,completed_at
		FROM review_daily_validation_jobs WHERE summary_date=?`, strings.TrimSpace(summaryDate)).Scan(
		&job.SummaryDate, &job.VerificationDate, &job.Status, &job.Stage, &job.Message, &job.Error, &startedAt, &updatedAt, &completedAt)
	if err != nil {
		return DailyValidationJob{}, err
	}
	job.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	job.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	job.CompletedAt, _ = time.Parse(time.RFC3339, completedAt)
	if _, resultErr := s.GetDailyValidation(ctx, job.SummaryDate); resultErr == nil {
		job.ResultAvailable = true
	}
	return job, nil
}

func (s *Store) SaveDailySummaryJob(ctx context.Context, job DailySummaryJob) (DailySummaryJob, error) {
	now := time.Now().UTC()
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO review_daily_summary_jobs (
		trade_date,window_start,window_end,freshness_rule,status,stage,completed_authors,total_authors,article_count,message,error,started_at,updated_at,completed_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(trade_date) DO UPDATE SET
		window_start=excluded.window_start, window_end=excluded.window_end, freshness_rule=excluded.freshness_rule,
		status=excluded.status, stage=excluded.stage, completed_authors=excluded.completed_authors,
		total_authors=excluded.total_authors, article_count=excluded.article_count, message=excluded.message,
		error=excluded.error, started_at=excluded.started_at, updated_at=excluded.updated_at, completed_at=excluded.completed_at`,
		job.TradeDate, formatOptionalTime(job.WindowStart), formatOptionalTime(job.WindowEnd), job.FreshnessRule,
		job.Status, job.Stage, job.CompletedAuthors, job.TotalAuthors, job.ArticleCount,
		job.Message, job.Error, formatOptionalTime(job.StartedAt), formatOptionalTime(job.UpdatedAt), formatOptionalTime(job.CompletedAt))
	if err != nil {
		return DailySummaryJob{}, fmt.Errorf("save daily summary job: %w", err)
	}
	return s.GetDailySummaryJob(ctx, job.TradeDate)
}

func (s *Store) GetDailySummaryJob(ctx context.Context, tradeDate string) (DailySummaryJob, error) {
	var job DailySummaryJob
	var windowStart, windowEnd, startedAt, updatedAt, completedAt string
	err := s.db.QueryRowContext(ctx, `SELECT trade_date,window_start,window_end,freshness_rule,status,stage,completed_authors,total_authors,article_count,message,error,started_at,updated_at,completed_at
		FROM review_daily_summary_jobs WHERE trade_date=?`, strings.TrimSpace(tradeDate)).Scan(
		&job.TradeDate, &windowStart, &windowEnd, &job.FreshnessRule, &job.Status, &job.Stage, &job.CompletedAuthors, &job.TotalAuthors, &job.ArticleCount,
		&job.Message, &job.Error, &startedAt, &updatedAt, &completedAt)
	if err != nil {
		return DailySummaryJob{}, err
	}
	job.WindowStart, _ = time.Parse(time.RFC3339, windowStart)
	job.WindowEnd, _ = time.Parse(time.RFC3339, windowEnd)
	job.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	job.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	job.CompletedAt, _ = time.Parse(time.RFC3339, completedAt)
	return job, nil
}

func (s *Store) ListAuthors(ctx context.Context, source string) ([]Author, error) {
	query := `SELECT author_id, source, author_name, COUNT(*), MAX(published_at) FROM review_posts`
	args := []any{}
	if strings.TrimSpace(source) != "" && source != "all" {
		query += ` WHERE source = ?`
		args = append(args, source)
	}
	query += ` GROUP BY author_id, source, author_name ORDER BY MAX(published_at) DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list review authors: %w", err)
	}
	defer rows.Close()
	authors := []Author{}
	for rows.Next() {
		var author Author
		var latest string
		if err := rows.Scan(&author.ID, &author.Source, &author.Name, &author.PostCount, &latest); err != nil {
			return nil, err
		}
		author.LatestAt, _ = time.Parse(time.RFC3339, latest)
		authors = append(authors, author)
	}
	return authors, rows.Err()
}

const postSelect = `SELECT id, source, external_id, author_id, author_name, title, digest, content_text,
	cover_url, original_url, published_at, fetched_at, related_stocks, related_themes,
	read_status, favorite_status, ai_summary, ai_key_points, ai_outlook, ai_analyzed_at, ai_error FROM review_posts`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPost(row rowScanner) (Post, error) {
	var post Post
	var publishedAt, fetchedAt, stocks, themes, keyPoints, analyzedAt string
	var readStatus, favoriteStatus int
	err := row.Scan(&post.ID, &post.Source, &post.ExternalID, &post.AuthorID, &post.AuthorName,
		&post.Title, &post.Digest, &post.ContentText, &post.CoverURL, &post.OriginalURL,
		&publishedAt, &fetchedAt, &stocks, &themes, &readStatus, &favoriteStatus,
		&post.AISummary, &keyPoints, &post.AIOutlook, &analyzedAt, &post.AIError)
	if err != nil {
		return Post{}, err
	}
	post.PublishedAt, _ = time.Parse(time.RFC3339, publishedAt)
	post.FetchedAt, _ = time.Parse(time.RFC3339, fetchedAt)
	_ = json.Unmarshal([]byte(stocks), &post.RelatedStocks)
	_ = json.Unmarshal([]byte(themes), &post.RelatedThemes)
	_ = json.Unmarshal([]byte(keyPoints), &post.AIKeyPoints)
	if analyzedAt != "" {
		post.AIAnalyzedAt, _ = time.Parse(time.RFC3339, analyzedAt)
	}
	post.RelatedStocks = nonNilStrings(post.RelatedStocks)
	post.RelatedThemes = nonNilStrings(post.RelatedThemes)
	post.AIKeyPoints = nonNilStrings(post.AIKeyPoints)
	post.Read = readStatus != 0
	post.Favorite = favoriteStatus != 0
	return post, nil
}

func (s *Store) SaveAnalysis(ctx context.Context, id, summary string, keyPoints []string, outlook, analysisError string) (Post, error) {
	analyzedAt := ""
	if analysisError == "" {
		analyzedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE review_posts SET ai_summary=?, ai_key_points=?, ai_outlook=?, ai_analyzed_at=?, ai_error=? WHERE id=?`,
		summary, mustJSON(keyPoints), outlook, analyzedAt, analysisError, id)
	if err != nil {
		return Post{}, fmt.Errorf("save review analysis: %w", err)
	}
	return s.GetPost(ctx, id)
}

func (s *Store) UpsertSubscription(ctx context.Context, sub Subscription) (Subscription, error) {
	if sub.ID == "" {
		sub.ID = stableID(sub.Source + "\n" + sub.HomepageURL)
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO review_subscriptions
		(id,source,name,homepage_url,external_id,config_id,enabled,last_sync_at,next_sync_at,last_status,last_error,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(source,homepage_url) DO UPDATE SET name=excluded.name, external_id=excluded.external_id, config_id=excluded.config_id, enabled=excluded.enabled`,
		sub.ID, sub.Source, sub.Name, sub.HomepageURL, sub.ExternalID, sub.ConfigID, boolInt(sub.Enabled), formatOptionalTime(sub.LastSyncAt), formatOptionalTime(sub.NextSyncAt), firstValue(sub.LastStatus, "pending"), sub.LastError, sub.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Subscription{}, fmt.Errorf("save subscription: %w", err)
	}
	return s.GetSubscriptionByURL(ctx, sub.Source, sub.HomepageURL)
}

func (s *Store) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,name,homepage_url,external_id,config_id,enabled,last_sync_at,next_sync_at,last_status,last_error,created_at FROM review_subscriptions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Subscription{}
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, sub)
	}
	return items, rows.Err()
}

func (s *Store) GetSubscription(ctx context.Context, id string) (Subscription, error) {
	return scanSubscription(s.db.QueryRowContext(ctx, `SELECT id,source,name,homepage_url,external_id,config_id,enabled,last_sync_at,next_sync_at,last_status,last_error,created_at FROM review_subscriptions WHERE id=?`, id))
}

func (s *Store) GetSubscriptionByURL(ctx context.Context, source, homepageURL string) (Subscription, error) {
	return scanSubscription(s.db.QueryRowContext(ctx, `SELECT id,source,name,homepage_url,external_id,config_id,enabled,last_sync_at,next_sync_at,last_status,last_error,created_at FROM review_subscriptions WHERE source=? AND homepage_url=?`, source, homepageURL))
}

func (s *Store) DeleteSubscription(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM review_subscriptions WHERE id=?`, id)
	return err
}

func (s *Store) SetSubscriptionSync(ctx context.Context, id, status, syncError string, next time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE review_subscriptions SET last_sync_at=?,next_sync_at=?,last_status=?,last_error=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339), next.UTC().Format(time.RFC3339), status, syncError, id)
	return err
}

func scanSubscription(row rowScanner) (Subscription, error) {
	var sub Subscription
	var enabled int
	var lastSync, nextSync, created string
	err := row.Scan(&sub.ID, &sub.Source, &sub.Name, &sub.HomepageURL, &sub.ExternalID, &sub.ConfigID, &enabled, &lastSync, &nextSync, &sub.LastStatus, &sub.LastError, &created)
	if err != nil {
		return Subscription{}, err
	}
	sub.Enabled = enabled != 0
	sub.LastSyncAt, _ = time.Parse(time.RFC3339, lastSync)
	sub.NextSyncAt, _ = time.Parse(time.RFC3339, nextSync)
	sub.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return sub, nil
}

func stableID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}
func mustJSON(values []string) string {
	data, _ := json.Marshal(nonNilStrings(values))
	return string(data)
}
func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
func firstValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
