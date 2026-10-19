// Package sectorflowsample 把板块资金流的「当前累计值」按分钟落库，
// 让分时曲线可以脱离上游的分钟序列接口独立拼接。
//
// 东方财富的板块分时资金（push2 fflow/kline）会被按路径拦截，而板块资金流榜单
// （dataapi bkzj）稳定可用，并且榜单里的主力净流入本身就是一个累计值。
// 因此只要定时把榜单快照记下来，就能在本地还原出一条当日累计曲线：
// 上游分钟序列可用时直接用它，不可用时回退到这份本地采样。
package sectorflowsample

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"easy-stock/backend/internal/sqliteutil"
)

// RetentionDays 是本地采样的保留天数：只用于盘中降级，不需要长期历史。
const RetentionDays = 45

// Sample 是某个板块在某一分钟的资金流快照，数值为当日累计值。
type Sample struct {
	TradeDate     string
	Minute        string // 交易时间，形如 13:03
	Code          string // 板块代码，形如 BK0917
	Name          string
	Dimension     string // concept / industry
	Main          float64
	SuperLarge    float64
	Large         float64
	Medium        float64
	Small         float64
	ChangePercent float64
	SampledAt     time.Time
	// HasFlow / HasPrice 标记这一分钟是否真的有对应口径的数据。
	//
	// 采样器每次同时写入资金与涨跌幅，两个标记都为真；上游分时资金曲线回填时
	// 只有资金字段，涨跌幅要等价格曲线回填补齐，因此用标记区分，避免把 0 当成真实值
	// 画进另一种曲线。两个标记都为假时按整行采样处理（兼容旧数据与测试）。
	HasFlow  bool
	HasPrice bool
}

// flow / price 标记归一化：未显式设置时视为整行采样。
func (s Sample) flags() (bool, bool) {
	if !s.HasFlow && !s.HasPrice {
		return true, true
	}
	return s.HasFlow, s.HasPrice
}

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		path = ":memory:"
	}
	dataSource := path
	if path == ":memory:" {
		dataSource = "file:easy-stock-sector-flow-samples?mode=memory&cache=shared"
	} else if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create sector flow sample data directory: %w", err)
	}
	db, err := sqliteutil.Open(dataSource)
	if err != nil {
		return nil, fmt.Errorf("open sector flow sample database: %w", err)
	}
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
		CREATE TABLE IF NOT EXISTS sector_flow_samples (
			trade_date TEXT NOT NULL,
			code TEXT NOT NULL,
			minute TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			dimension TEXT NOT NULL DEFAULT '',
			main_net REAL NOT NULL DEFAULT 0,
			super_large_net REAL NOT NULL DEFAULT 0,
			large_net REAL NOT NULL DEFAULT 0,
			medium_net REAL NOT NULL DEFAULT 0,
			small_net REAL NOT NULL DEFAULT 0,
			change_percent REAL NOT NULL DEFAULT 0,
			sampled_at TEXT NOT NULL,
			has_flow INTEGER NOT NULL DEFAULT 1,
			has_price INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (trade_date, code, minute)
		);
	`)
	if err != nil {
		return fmt.Errorf("migrate sector flow sample database: %w", err)
	}
	// 旧库没有两个可用性标记：先加列再回填默认值（旧行都是整行采样）。
	for _, column := range []struct {
		name string
		ddl  string
	}{
		{"has_flow", `ALTER TABLE sector_flow_samples ADD COLUMN has_flow INTEGER NOT NULL DEFAULT 1`},
		{"has_price", `ALTER TABLE sector_flow_samples ADD COLUMN has_price INTEGER NOT NULL DEFAULT 1`},
	} {
		exists, err := s.columnExists(ctx, column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := s.db.ExecContext(ctx, column.ddl); err != nil {
			return fmt.Errorf("migrate sector flow sample column %s: %w", column.name, err)
		}
	}
	return nil
}

func (s *Store) columnExists(ctx context.Context, name string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(sector_flow_samples)`)
	if err != nil {
		return false, fmt.Errorf("inspect sector flow sample table: %w", err)
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

// Save 以 (trade_date, code, minute) 为键写入快照；同一分钟内重复采样只保留最新一次。
func (s *Store) Save(ctx context.Context, samples []Sample) (int, error) {
	if s == nil || s.db == nil || len(samples) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin sector flow sample transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO sector_flow_samples(
			trade_date, code, minute, name, dimension,
			main_net, super_large_net, large_net, medium_net, small_net,
			change_percent, sampled_at, has_flow, has_price
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(trade_date, code, minute) DO UPDATE SET
			name=excluded.name,
			dimension=excluded.dimension,
			main_net=excluded.main_net,
			super_large_net=excluded.super_large_net,
			large_net=excluded.large_net,
			medium_net=excluded.medium_net,
			small_net=excluded.small_net,
			change_percent=excluded.change_percent,
			sampled_at=excluded.sampled_at,
			has_flow=MAX(sector_flow_samples.has_flow, excluded.has_flow),
			has_price=MAX(sector_flow_samples.has_price, excluded.has_price)
	`)
	if err != nil {
		return 0, fmt.Errorf("prepare sector flow sample upsert: %w", err)
	}
	defer statement.Close()

	saved := 0
	for _, sample := range samples {
		if strings.TrimSpace(sample.Code) == "" || strings.TrimSpace(sample.TradeDate) == "" || strings.TrimSpace(sample.Minute) == "" {
			continue
		}
		sampledAt := sample.SampledAt
		if sampledAt.IsZero() {
			sampledAt = time.Now()
		}
		hasFlow, hasPrice := sample.flags()
		if _, err := statement.ExecContext(ctx,
			sample.TradeDate, sample.Code, sample.Minute, sample.Name, sample.Dimension,
			sample.Main, sample.SuperLarge, sample.Large, sample.Medium, sample.Small,
			sample.ChangePercent, sampledAt.UTC().Format(time.RFC3339Nano),
			boolInt(hasFlow), boolInt(hasPrice),
		); err != nil {
			return saved, fmt.Errorf("save sector flow sample %s %s: %w", sample.Code, sample.Minute, err)
		}
		saved++
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit sector flow samples: %w", err)
	}
	return saved, nil
}

// SaveFlowBackfill 用上游分时资金曲线回填本地采样：只写资金字段，
// 不覆盖同一分钟可能已有的涨跌幅（价格曲线的回填走 SavePriceBackfill）。
func (s *Store) SaveFlowBackfill(ctx context.Context, samples []Sample) (int, error) {
	return s.saveBackfill(ctx, samples, true)
}

// SavePriceBackfill 用上游分时价格曲线回填本地采样：只写涨跌幅。
func (s *Store) SavePriceBackfill(ctx context.Context, samples []Sample) (int, error) {
	return s.saveBackfill(ctx, samples, false)
}

// saveBackfill 是两种回填的公共实现：插入新分钟时只把本口径标记为可用，
// 命中已有分钟时只更新本口径的数值，另一口径的数值与标记保持不变。
func (s *Store) saveBackfill(ctx context.Context, samples []Sample, flow bool) (int, error) {
	if s == nil || s.db == nil || len(samples) == 0 {
		return 0, nil
	}
	statement := `
		INSERT INTO sector_flow_samples(
			trade_date, code, minute, name, dimension,
			main_net, super_large_net, large_net, medium_net, small_net,
			change_percent, sampled_at, has_flow, has_price
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, 1, 0)
		ON CONFLICT(trade_date, code, minute) DO UPDATE SET
			name=CASE WHEN excluded.name <> '' THEN excluded.name ELSE sector_flow_samples.name END,
			main_net=excluded.main_net,
			super_large_net=excluded.super_large_net,
			large_net=excluded.large_net,
			medium_net=excluded.medium_net,
			small_net=excluded.small_net,
			sampled_at=excluded.sampled_at,
			has_flow=1`
	if !flow {
		statement = `
		INSERT INTO sector_flow_samples(
			trade_date, code, minute, name, dimension,
			change_percent, sampled_at, has_flow, has_price
		) VALUES (?, ?, ?, ?, ?, ?, ?, 0, 1)
		ON CONFLICT(trade_date, code, minute) DO UPDATE SET
			name=CASE WHEN excluded.name <> '' THEN excluded.name ELSE sector_flow_samples.name END,
			change_percent=excluded.change_percent,
			sampled_at=excluded.sampled_at,
			has_price=1`
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin sector flow backfill transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	prepared, err := tx.PrepareContext(ctx, statement)
	if err != nil {
		return 0, fmt.Errorf("prepare sector flow backfill: %w", err)
	}
	defer prepared.Close()

	saved := 0
	for _, sample := range samples {
		if strings.TrimSpace(sample.Code) == "" || strings.TrimSpace(sample.TradeDate) == "" || strings.TrimSpace(sample.Minute) == "" {
			continue
		}
		sampledAt := sample.SampledAt
		if sampledAt.IsZero() {
			sampledAt = time.Now()
		}
		var err error
		if flow {
			_, err = prepared.ExecContext(ctx,
				sample.TradeDate, sample.Code, sample.Minute, sample.Name, sample.Dimension,
				sample.Main, sample.SuperLarge, sample.Large, sample.Medium, sample.Small,
				sampledAt.UTC().Format(time.RFC3339Nano),
			)
		} else {
			_, err = prepared.ExecContext(ctx,
				sample.TradeDate, sample.Code, sample.Minute, sample.Name, sample.Dimension,
				sample.ChangePercent, sampledAt.UTC().Format(time.RFC3339Nano),
			)
		}
		if err != nil {
			return saved, fmt.Errorf("backfill sector flow sample %s %s: %w", sample.Code, sample.Minute, err)
		}
		saved++
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit sector flow backfill: %w", err)
	}
	return saved, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// DaySamples 返回指定交易日、指定板块的采样点，按分钟升序。
func (s *Store) DaySamples(ctx context.Context, tradeDate string, codes []string) ([]Sample, error) {
	if s == nil || s.db == nil || strings.TrimSpace(tradeDate) == "" {
		return nil, nil
	}
	query := `
		SELECT trade_date, code, minute, name, dimension,
			main_net, super_large_net, large_net, medium_net, small_net,
			change_percent, sampled_at, has_flow, has_price
		FROM sector_flow_samples
		WHERE trade_date = ?`
	arguments := []any{tradeDate}
	if len(codes) > 0 {
		placeholders := make([]string, len(codes))
		for index, code := range codes {
			placeholders[index] = "?"
			arguments = append(arguments, code)
		}
		query += " AND code IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY code ASC, minute ASC"

	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query sector flow samples: %w", err)
	}
	defer rows.Close()

	samples := make([]Sample, 0, len(codes)*32)
	for rows.Next() {
		var sample Sample
		var sampledAt string
		var hasFlow, hasPrice int
		if err := rows.Scan(
			&sample.TradeDate, &sample.Code, &sample.Minute, &sample.Name, &sample.Dimension,
			&sample.Main, &sample.SuperLarge, &sample.Large, &sample.Medium, &sample.Small,
			&sample.ChangePercent, &sampledAt, &hasFlow, &hasPrice,
		); err != nil {
			return nil, err
		}
		sample.SampledAt, _ = time.Parse(time.RFC3339Nano, sampledAt)
		sample.HasFlow = hasFlow != 0
		sample.HasPrice = hasPrice != 0
		samples = append(samples, sample)
	}
	return samples, rows.Err()
}

// LatestTradeDate 返回采样表里最近一个有数据的交易日，用于当天还没采样时回退展示。
func (s *Store) LatestTradeDate(ctx context.Context) (string, error) {
	if s == nil || s.db == nil {
		return "", nil
	}
	var tradeDate string
	err := s.db.QueryRowContext(ctx, `SELECT trade_date FROM sector_flow_samples ORDER BY trade_date DESC LIMIT 1`).Scan(&tradeDate)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query latest sector flow sample date: %w", err)
	}
	return tradeDate, nil
}

// PurgeBefore 删除早于指定交易日的采样，控制库体积。
func (s *Store) PurgeBefore(ctx context.Context, tradeDate string) error {
	if s == nil || s.db == nil || strings.TrimSpace(tradeDate) == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sector_flow_samples WHERE trade_date < ?`, tradeDate); err != nil {
		return fmt.Errorf("purge sector flow samples: %w", err)
	}
	return nil
}
