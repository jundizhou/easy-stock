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
