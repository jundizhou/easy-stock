package themelibrary

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"easy-stock/backend/internal/sqliteutil"
)

var ErrNotFound = errors.New("theme not found")

var memorySequence uint64

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	seed, err := EmbeddedSeed()
	if err != nil {
		return nil, err
	}
	return OpenWithSeed(path, seed)
}

// OpenWithSeed opens the independent 题材库 database and, when the embedded
// snapshot is newer than the local copy, replaces the read-only reference data.
func OpenWithSeed(path string, seed Seed) (*Store, error) {
	dataSource := strings.TrimSpace(path)
	if dataSource == "" || dataSource == ":memory:" {
		dataSource = fmt.Sprintf("file:easy-stock-theme-library-%d?mode=memory&cache=shared", atomic.AddUint64(&memorySequence, 1))
	} else if err := os.MkdirAll(filepath.Dir(dataSource), 0o700); err != nil {
		return nil, fmt.Errorf("create theme library data directory: %w", err)
	}
	db, err := sqliteutil.Open(dataSource)
	if err != nil {
		return nil, fmt.Errorf("open theme library database: %w", err)
	}
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.syncSeed(context.Background(), seed); err != nil {
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
		CREATE TABLE IF NOT EXISTS library_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS themes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			introduction TEXT NOT NULL DEFAULT '',
			change_percent REAL NOT NULL DEFAULT 0,
			stock_count INTEGER NOT NULL DEFAULT 0,
			limit_up_count INTEGER NOT NULL DEFAULT 0,
			rising_count INTEGER NOT NULL DEFAULT 0,
			falling_count INTEGER NOT NULL DEFAULT 0,
			flat_count INTEGER NOT NULL DEFAULT 0,
			hot REAL NOT NULL DEFAULT 0,
			theme_updated_at TEXT NOT NULL DEFAULT '',
			as_of TEXT NOT NULL DEFAULT '',
			source_url TEXT NOT NULL DEFAULT '',
			source_api_url TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS theme_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			theme_id INTEGER NOT NULL REFERENCES themes(id) ON DELETE CASCADE,
			source_id TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			parent_source_id TEXT NOT NULL DEFAULT '',
			parent_name TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '[]',
			change_percent REAL NOT NULL DEFAULT 0,
			limit_up_count INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			UNIQUE(theme_id, source_id)
		);
		CREATE TABLE IF NOT EXISTS theme_stocks (
			theme_id INTEGER NOT NULL REFERENCES themes(id) ON DELETE CASCADE,
			group_id INTEGER NOT NULL REFERENCES theme_groups(id) ON DELETE CASCADE,
			code TEXT NOT NULL,
			name TEXT NOT NULL,
			price REAL NOT NULL DEFAULT 0,
			change_percent REAL NOT NULL DEFAULT 0,
			is_limit_up INTEGER NOT NULL DEFAULT 0,
			hot REAL NOT NULL DEFAULT 0,
			tags TEXT NOT NULL DEFAULT '[]',
			description TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (theme_id, group_id, code)
		);
		CREATE INDEX IF NOT EXISTS idx_theme_groups_theme ON theme_groups(theme_id, sort_order);
		CREATE INDEX IF NOT EXISTS idx_theme_stocks_theme ON theme_stocks(theme_id, group_id, sort_order);
		CREATE INDEX IF NOT EXISTS idx_theme_stocks_code ON theme_stocks(code);
	`)
	if err != nil {
		return fmt.Errorf("migrate theme library database: %w", err)
	}
	for _, column := range []struct {
		name string
		ddl  string
	}{
		{"parent_source_id", `ALTER TABLE theme_groups ADD COLUMN parent_source_id TEXT NOT NULL DEFAULT ''`},
		{"parent_name", `ALTER TABLE theme_groups ADD COLUMN parent_name TEXT NOT NULL DEFAULT ''`},
		{"path", `ALTER TABLE theme_groups ADD COLUMN path TEXT NOT NULL DEFAULT '[]'`},
	} {
		exists, err := s.columnExists(ctx, "theme_groups", column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := s.db.ExecContext(ctx, column.ddl); err != nil {
			return fmt.Errorf("migrate theme group column %s: %w", column.name, err)
		}
	}
	return nil
}

func (s *Store) columnExists(ctx context.Context, table string, name string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, fmt.Errorf("inspect %s table: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			index      int
			column     string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&index, &column, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return false, err
		}
		if column == name {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) syncSeed(ctx context.Context, seed Seed) error {
	if len(seed.Themes) == 0 {
		return nil
	}
	if seed.ThemeCount == 0 {
		seed.ThemeCount = len(seed.Themes)
	}
	current, err := s.metaValue(ctx, "generated_at")
	if err != nil {
		return err
	}
	count, err := s.themeCount(ctx)
	if err != nil {
		return err
	}
	// Only a strictly newer embedded snapshot replaces local data, so a daily
	// refresh written at runtime is not rolled back to an older seed on restart.
	if count > 0 && current >= seed.GeneratedAt {
		return nil
	}
	return s.Replace(ctx, seed)
}

func (s *Store) Replace(ctx context.Context, seed Seed) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin theme library import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM theme_stocks; DELETE FROM theme_groups; DELETE FROM themes;`); err != nil {
		return fmt.Errorf("clear theme library: %w", err)
	}
	_, _ = tx.ExecContext(ctx, `DELETE FROM sqlite_sequence WHERE name IN ('themes','theme_groups')`)

	now := time.Now().UTC().Format(time.RFC3339)
	generatedAt := strings.TrimSpace(seed.GeneratedAt)
	if generatedAt == "" {
		generatedAt = now
	}
	sourceURL := strings.TrimSpace(seed.SourceURL)
	if sourceURL == "" {
		sourceURL = LibraryPageURL
	}
	asOf := strings.TrimSpace(seed.SourceUpdatedAt)
	if len(asOf) >= 10 {
		asOf = asOf[:10]
	}
	themes := seed.Themes
	if seed.ThemeCount == 0 {
		seed.ThemeCount = len(themes)
	}
	for index := range themes {
		if err := insertTheme(ctx, tx, themes[index], asOf, sourceURL); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO library_meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "theme_count", strconv.Itoa(len(themes))); err != nil {
		return fmt.Errorf("store theme library count: %w", err)
	}
	meta := map[string]string{
		"schema_version":    strconv.Itoa(seed.SchemaVersion),
		"source":            seed.Source,
		"source_name":       seed.SourceName,
		"source_url":        sourceURL,
		"api_url":           seed.APIURL,
		"source_updated_at": seed.SourceUpdatedAt,
		"generated_at":      generatedAt,
		"selection":         seed.Selection,
	}
	for key, value := range meta {
		if _, err := tx.ExecContext(ctx, `INSERT INTO library_meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return fmt.Errorf("store theme library meta %s: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit theme library import: %w", err)
	}
	return nil
}

func insertTheme(ctx context.Context, tx *sql.Tx, theme Theme, defaultAsOf string, defaultSourceURL string) error {
	stockCount := theme.StockCount
	if stockCount <= 0 {
		stockCount = uniqueStockCount(theme.Groups)
	}
	limitUpCount := theme.LimitUpCount
	if limitUpCount <= 0 {
		limitUpCount = limitUpRelations(theme.Groups)
	}
	themeAsOf := strings.TrimSpace(theme.AsOf)
	if themeAsOf == "" {
		themeAsOf = defaultAsOf
	}
	if len(themeAsOf) > 10 {
		themeAsOf = themeAsOf[:10]
	}
	sourceURL := strings.TrimSpace(theme.SourceURL)
	if sourceURL == "" {
		sourceURL = defaultSourceURL
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO themes(
			source_id, name, description, introduction, change_percent, stock_count,
			limit_up_count, rising_count, falling_count, flat_count, hot,
			theme_updated_at, as_of, source_url, source_api_url
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`, theme.SourceID, theme.Name, SanitizeHTML(theme.Description), SanitizeHTML(theme.Introduction),
		theme.ChangePercent, stockCount, limitUpCount, theme.RisingCount, theme.FallingCount, theme.FlatCount,
		theme.Hot, theme.ThemeUpdatedAt, themeAsOf, sourceURL, theme.SourceAPIURL)
	if err != nil {
		return fmt.Errorf("insert theme %s: %w", theme.SourceID, err)
	}
	themeID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	for groupIndex := range theme.Groups {
		group := theme.Groups[groupIndex]
		path, err := json.Marshal(group.Path)
		if err != nil {
			path = []byte("[]")
		}
		groupResult, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO theme_groups(theme_id, source_id, name, parent_source_id, parent_name, path, change_percent, limit_up_count, sort_order)
			VALUES(?,?,?,?,?,?,?,?,?)
		`, themeID, group.SourceID, group.Name, group.ParentSourceID, group.ParentName, string(path), group.ChangePercent, group.LimitUpCount, groupIndex)
		if err != nil {
			return fmt.Errorf("insert theme group %s/%s: %w", theme.SourceID, group.SourceID, err)
		}
		groupID, err := groupResult.LastInsertId()
		if err != nil {
			return err
		}
		if groupID == 0 {
			if err := tx.QueryRowContext(ctx, `SELECT id FROM theme_groups WHERE theme_id=? AND source_id=?`, themeID, group.SourceID).Scan(&groupID); err != nil {
				return err
			}
		}
		seen := make(map[string]struct{}, len(group.Stocks))
		for stockIndex, stock := range group.Stocks {
			if _, exists := seen[stock.Code]; exists {
				continue
			}
			seen[stock.Code] = struct{}{}
			tags, err := json.Marshal(stock.Tags)
			if err != nil {
				tags = []byte("[]")
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO theme_stocks(
					theme_id, group_id, code, name, price, change_percent,
					is_limit_up, hot, tags, description, sort_order
				) VALUES(?,?,?,?,?,?,?,?,?,?,?)
			`, themeID, groupID, stock.Code, stock.Name, stock.Price, stock.ChangePercent,
				boolInt(stock.IsLimitUp), stock.Hot, string(tags), SanitizeHTML(stock.Description), stockIndex); err != nil {
				return fmt.Errorf("insert theme stock %s/%s: %w", theme.SourceID, stock.Code, err)
			}
		}
	}
	return nil
}

func (s *Store) List(ctx context.Context, filter Filter) (Library, error) {
	meta, err := s.Meta(ctx)
	if err != nil {
		return Library{}, err
	}
	stats, err := s.Stats(ctx)
	if err != nil {
		return Library{}, err
	}
	where, args := listWhere(filter.Query)
	total, err := s.countThemes(ctx, where, args)
	if err != nil {
		return Library{}, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 300 {
		limit = 300
	}
	order := listOrder(filter.Sort)
	query := `
		SELECT t.id, t.source_id, t.name, t.description, t.change_percent, t.stock_count,
			t.limit_up_count, t.rising_count, t.falling_count, t.flat_count, t.hot,
			t.theme_updated_at, t.as_of, t.source_url, t.source_api_url,
			(SELECT COUNT(*) FROM theme_groups g WHERE g.theme_id=t.id),
			(SELECT COUNT(*) FROM theme_stocks ts WHERE ts.theme_id=t.id)
		FROM themes t
		WHERE ` + where + `
		ORDER BY ` + order + `
		LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, append(args, limit)...)
	if err != nil {
		return Library{}, fmt.Errorf("list themes: %w", err)
	}
	defer rows.Close()
	items := make([]Theme, 0, min(limit, total))
	for rows.Next() {
		var theme Theme
		if err := rows.Scan(&theme.ID, &theme.SourceID, &theme.Name, &theme.Description, &theme.ChangePercent,
			&theme.StockCount, &theme.LimitUpCount, &theme.RisingCount, &theme.FallingCount, &theme.FlatCount,
			&theme.Hot, &theme.ThemeUpdatedAt, &theme.AsOf, &theme.SourceURL, &theme.SourceAPIURL,
			&theme.GroupCount, &theme.RelationCount); err != nil {
			return Library{}, fmt.Errorf("scan theme: %w", err)
		}
		items = append(items, theme)
	}
	if err := rows.Err(); err != nil {
		return Library{}, err
	}
	library := Library{Meta: meta, Stats: stats, Total: total, Items: items}
	library.Available = stats.Themes > 0
	if !library.Available {
		library.Message = "题材库尚未导入数据"
	}
	return library, nil
}

func (s *Store) Get(ctx context.Context, sourceID string) (Theme, error) {
	sourceID = strings.TrimSpace(sourceID)
	var theme Theme
	err := s.db.QueryRowContext(ctx, `
		SELECT id, source_id, name, description, introduction, change_percent, stock_count,
			limit_up_count, rising_count, falling_count, flat_count, hot,
			theme_updated_at, as_of, source_url, source_api_url
		FROM themes
		WHERE source_id=? OR CAST(id AS TEXT)=?
		LIMIT 1
	`, sourceID, sourceID).Scan(&theme.ID, &theme.SourceID, &theme.Name, &theme.Description, &theme.Introduction,
		&theme.ChangePercent, &theme.StockCount, &theme.LimitUpCount, &theme.RisingCount, &theme.FallingCount,
		&theme.FlatCount, &theme.Hot, &theme.ThemeUpdatedAt, &theme.AsOf, &theme.SourceURL, &theme.SourceAPIURL)
	if errors.Is(err, sql.ErrNoRows) {
		return Theme{}, ErrNotFound
	}
	if err != nil {
		return Theme{}, fmt.Errorf("get theme: %w", err)
	}

	type groupRow struct {
		ID             int64
		SourceID       string
		Name           string
		ParentSourceID string
		ParentName     string
		Path           string
		ChangePercent  float64
		LimitUpCount   int
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source_id, name, parent_source_id, parent_name, path, change_percent, limit_up_count
		FROM theme_groups WHERE theme_id=? ORDER BY sort_order, id
	`, theme.ID)
	if err != nil {
		return Theme{}, fmt.Errorf("list theme groups: %w", err)
	}
	groups := make([]Group, 0)
	groupIndex := make(map[int64]int)
	for rows.Next() {
		var row groupRow
		if err := rows.Scan(&row.ID, &row.SourceID, &row.Name, &row.ParentSourceID, &row.ParentName, &row.Path, &row.ChangePercent, &row.LimitUpCount); err != nil {
			rows.Close()
			return Theme{}, err
		}
		var path []string
		if row.Path != "" {
			_ = json.Unmarshal([]byte(row.Path), &path)
		}
		groupIndex[row.ID] = len(groups)
		groups = append(groups, Group{
			SourceID:       row.SourceID,
			Name:           row.Name,
			ParentSourceID: row.ParentSourceID,
			ParentName:     row.ParentName,
			Path:           path,
			ChangePercent:  row.ChangePercent,
			LimitUpCount:   row.LimitUpCount,
			Stocks:         []Stock{},
		})
	}
	if err := rows.Close(); err != nil {
		return Theme{}, err
	}
	if err := rows.Err(); err != nil {
		return Theme{}, err
	}

	stockRows, err := s.db.QueryContext(ctx, `
		SELECT group_id, code, name, price, change_percent, is_limit_up, hot, tags, description
		FROM theme_stocks WHERE theme_id=? ORDER BY group_id, sort_order, hot DESC, code
	`, theme.ID)
	if err != nil {
		return Theme{}, fmt.Errorf("list theme stocks: %w", err)
	}
	defer stockRows.Close()
	for stockRows.Next() {
		var (
			groupID   int64
			stock     Stock
			isLimitUp int
			tags      string
		)
		if err := stockRows.Scan(&groupID, &stock.Code, &stock.Name, &stock.Price, &stock.ChangePercent, &isLimitUp, &stock.Hot, &tags, &stock.Description); err != nil {
			return Theme{}, err
		}
		stock.IsLimitUp = isLimitUp != 0
		if tags != "" {
			_ = json.Unmarshal([]byte(tags), &stock.Tags)
		}
		if index, ok := groupIndex[groupID]; ok {
			groups[index].Stocks = append(groups[index].Stocks, stock)
		}
		theme.RelationCount++
	}
	if err := stockRows.Err(); err != nil {
		return Theme{}, err
	}
	for index := range groups {
		groups[index].StockCount = len(groups[index].Stocks)
	}
	theme.Groups = groups
	theme.GroupCount = len(groups)
	if theme.StockCount <= 0 {
		theme.StockCount = uniqueStockCount(groups)
	}
	return theme, nil
}

func (s *Store) Meta(ctx context.Context) (Meta, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM library_meta`)
	if err != nil {
		return Meta{}, fmt.Errorf("read theme library meta: %w", err)
	}
	defer rows.Close()
	values := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return Meta{}, err
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return Meta{}, err
	}
	version, _ := strconv.Atoi(values["schema_version"])
	return Meta{
		SchemaVersion:   version,
		Source:          values["source"],
		SourceName:      values["source_name"],
		SourceURL:       values["source_url"],
		APIURL:          values["api_url"],
		SourceUpdatedAt: values["source_updated_at"],
		GeneratedAt:     values["generated_at"],
		Selection:       values["selection"],
	}, nil
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM themes`).Scan(&stats.Themes); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM theme_groups`).Scan(&stats.Groups); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM theme_stocks`).Scan(&stats.Relations); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT code) FROM theme_stocks`).Scan(&stats.Stocks); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

func (s *Store) metaValue(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM library_meta WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read theme library meta %s: %w", key, err)
	}
	return value, nil
}

func (s *Store) themeCount(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM themes`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count themes: %w", err)
	}
	return count, nil
}

func (s *Store) countThemes(ctx context.Context, where string, args []any) (int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM themes t WHERE `+where, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count themes: %w", err)
	}
	return total, nil
}

func listWhere(query string) (string, []any) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "1=1", nil
	}
	pattern := "%" + escapeLike(query) + "%"
	return `(t.name LIKE ? ESCAPE '\' OR t.description LIKE ? ESCAPE '\' OR EXISTS (
		SELECT 1 FROM theme_groups g WHERE g.theme_id=t.id AND (g.name LIKE ? ESCAPE '\' OR g.path LIKE ? ESCAPE '\')
	) OR EXISTS (
		SELECT 1 FROM theme_stocks ts WHERE ts.theme_id=t.id AND (ts.code LIKE ? ESCAPE '\' OR ts.name LIKE ? ESCAPE '\')
	))`, []any{pattern, pattern, pattern, pattern, pattern, pattern}
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

func listOrder(sortKey string) string {
	switch strings.ToLower(strings.TrimSpace(sortKey)) {
	case "change":
		return "t.change_percent DESC, t.hot DESC, t.name ASC"
	case "limit", "limit_up":
		return "t.limit_up_count DESC, t.change_percent DESC, t.hot DESC, t.name ASC"
	case "stocks":
		return "t.stock_count DESC, t.hot DESC, t.name ASC"
	case "updated":
		return "t.theme_updated_at DESC, t.hot DESC, t.name ASC"
	case "name":
		return "t.name ASC"
	default:
		return "t.hot DESC, t.name ASC"
	}
}

func uniqueStockCount(groups []Group) int {
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, stock := range group.Stocks {
			seen[stock.Code] = struct{}{}
		}
	}
	return len(seen)
}

func limitUpRelations(groups []Group) int {
	count := 0
	for _, group := range groups {
		for _, stock := range group.Stocks {
			if stock.IsLimitUp {
				count++
			}
		}
	}
	return count
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
