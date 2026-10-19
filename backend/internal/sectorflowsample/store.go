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
			PRIMARY KEY (trade_date, code, minute)
		);
	`)
	if err != nil {
		return fmt.Errorf("migrate sector flow sample database: %w", err)
	}
	return nil
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
			change_percent, sampled_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(trade_date, code, minute) DO UPDATE SET
			name=excluded.name,
			dimension=excluded.dimension,
			main_net=excluded.main_net,
			super_large_net=excluded.super_large_net,
			large_net=excluded.large_net,
			medium_net=excluded.medium_net,
			small_net=excluded.small_net,
			change_percent=excluded.change_percent,
			sampled_at=excluded.sampled_at
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
		if _, err := statement.ExecContext(ctx,
			sample.TradeDate, sample.Code, sample.Minute, sample.Name, sample.Dimension,
			sample.Main, sample.SuperLarge, sample.Large, sample.Medium, sample.Small,
			sample.ChangePercent, sampledAt.UTC().Format(time.RFC3339Nano),
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

// DaySamples 返回指定交易日、指定板块的采样点，按分钟升序。
func (s *Store) DaySamples(ctx context.Context, tradeDate string, codes []string) ([]Sample, error) {
	if s == nil || s.db == nil || strings.TrimSpace(tradeDate) == "" {
		return nil, nil
	}
	query := `
		SELECT trade_date, code, minute, name, dimension,
			main_net, super_large_net, large_net, medium_net, small_net,
			change_percent, sampled_at
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
		if err := rows.Scan(
			&sample.TradeDate, &sample.Code, &sample.Minute, &sample.Name, &sample.Dimension,
			&sample.Main, &sample.SuperLarge, &sample.Large, &sample.Medium, &sample.Small,
			&sample.ChangePercent, &sampledAt,
		); err != nil {
			return nil, err
		}
		sample.SampledAt, _ = time.Parse(time.RFC3339Nano, sampledAt)
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
