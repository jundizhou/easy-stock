package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type chainFakeSnapshot struct {
	rows []foundation.MarketSnapshotRow
	err  error
	meta foundation.SourceMeta
}

func (f chainFakeSnapshot) MarketSnapshot(context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	meta := f.meta
	if meta.Source == "" {
		meta.Source = "fake"
	}
	return f.rows, meta, f.err
}

type chainSnapshotFunc func(context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error)

func (f chainSnapshotFunc) MarketSnapshot(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	return f(ctx)
}

func shortMarketRows(n int) []foundation.MarketSnapshotRow {
	rows := make([]foundation.MarketSnapshotRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, foundation.MarketSnapshotRow{Symbol: fmt.Sprintf("%06d", i), Name: "X", Price: 1})
	}
	return rows
}

// 上游把 clist 每页行数砍到 100 后，东财会"成功"地只回 100 行；
// 链路必须把这种残缺结果当失败处理，落到下一个源，否则监控永远只扫 100 只票。
func TestMonitorSnapshotChainFallsBackOnTruncatedMarket(t *testing.T) {
	chain := newMonitorSnapshotChain(
		chainFakeSnapshot{rows: shortMarketRows(100)},
		chainFakeSnapshot{rows: shortMarketRows(fullMarketMinRows)},
	)
	rows, _, err := chain.MarketSnapshot(context.Background())
	if err != nil {
		t.Fatalf("应落到第二个源：%v", err)
	}
	if len(rows) != fullMarketMinRows {
		t.Fatalf("应采用第二个源的完整数据，实际 %d 行", len(rows))
	}
}

// 两个源都残缺时，返回行数较多的那份并标 Stale，而不是报错让监控空转。
func TestMonitorSnapshotChainKeepsBestPartial(t *testing.T) {
	chain := newMonitorSnapshotChain(
		chainFakeSnapshot{rows: shortMarketRows(100)},
		chainFakeSnapshot{rows: shortMarketRows(500)},
	)
	rows, meta, err := chain.MarketSnapshot(context.Background())
	if err != nil {
		t.Fatalf("残缺数据也应可用：%v", err)
	}
	if len(rows) != 500 {
		t.Fatalf("应保留行数最多的一份，实际 %d 行", len(rows))
	}
	if !meta.Stale {
		t.Fatal("残缺快照必须标 Stale")
	}
}

// 两个源都彻底失败时照常报错。
func TestMonitorSnapshotChainAllFailed(t *testing.T) {
	chain := newMonitorSnapshotChain(
		chainFakeSnapshot{err: errors.New("a down")},
		chainFakeSnapshot{err: errors.New("b down")},
	)
	if _, _, err := chain.MarketSnapshot(context.Background()); err == nil {
		t.Fatal("两个源都失败时应返回错误")
	}
}

func TestMonitorSnapshotChainFallsBackOnFlaggedFullSnapshot(t *testing.T) {
	for _, flag := range []string{"stale", "carry_forward", "error"} {
		t.Run(flag, func(t *testing.T) {
			primary := chainFakeSnapshot{rows: shortMarketRows(fullMarketMinRows + 100), meta: foundation.SourceMeta{Source: "primary"}}
			primary.meta.Stale = flag == "stale"
			primary.meta.CarryForward = flag == "carry_forward"
			if flag == "error" {
				primary.err = errors.New("page 59 EOF")
			}
			chain := newMonitorSnapshotChain(primary, chainFakeSnapshot{
				rows: shortMarketRows(fullMarketMinRows), meta: foundation.SourceMeta{Source: "backup"},
			})
			rows, meta, err := chain.MarketSnapshot(context.Background())
			if err != nil || meta.Source != "backup" || meta.Stale || meta.CarryForward || len(rows) != fullMarketMinRows {
				t.Fatalf("flagged primary must yield to healthy backup: rows=%d meta=%+v err=%v", len(rows), meta, err)
			}
		})
	}
}

func TestMonitorSnapshotChainRetainsPartialProvenanceWhenBackupFails(t *testing.T) {
	fetchedAt := time.Date(2026, 10, 28, 10, 39, 50, 0, time.UTC)
	chain := newMonitorSnapshotChain(
		chainFakeSnapshot{rows: shortMarketRows(1200), meta: foundation.SourceMeta{
			Source: "primary", FetchedAt: fetchedAt, Stale: true, FallbackReason: "page 59 EOF",
		}},
		chainFakeSnapshot{rows: shortMarketRows(1100), meta: foundation.SourceMeta{
			Source: "backup", Stale: true, FallbackReason: "page 53 timeout",
		}},
	)
	rows, meta, err := chain.MarketSnapshot(context.Background())
	if err != nil || len(rows) != 1200 || !meta.Stale || meta.Source != "primary" || !meta.FetchedAt.Equal(fetchedAt) || meta.FallbackReason != "page 59 EOF" {
		t.Fatalf("partial data must retain its source, failure and original timestamp: rows=%d meta=%+v err=%v", len(rows), meta, err)
	}
}

func TestMonitorSnapshotChainReservesDeadlineForBackup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	backupCalled := false
	chain := newMonitorSnapshotChain(
		chainSnapshotFunc(func(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
			<-ctx.Done()
			return nil, foundation.SourceMeta{}, ctx.Err()
		}),
		chainSnapshotFunc(func(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
			backupCalled = true
			if err := ctx.Err(); err != nil {
				t.Fatalf("primary exhausted the backup deadline: %v", err)
			}
			return shortMarketRows(fullMarketMinRows), foundation.SourceMeta{Source: "backup"}, nil
		}),
	)
	_, meta, err := chain.MarketSnapshot(ctx)
	if err != nil || !backupCalled || meta.Source != "backup" {
		t.Fatalf("healthy backup should run after primary timeout: called=%t meta=%+v err=%v", backupCalled, meta, err)
	}
}

func TestMonitorSnapshotChainBoundsCallsWithoutParentDeadline(t *testing.T) {
	chain := newMonitorSnapshotChain(chainSnapshotFunc(func(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 12*time.Second {
			t.Fatalf("provider call lacks a bounded deadline: %v %t", deadline, ok)
		}
		return nil, foundation.SourceMeta{}, errors.New("unavailable")
	}))
	if _, _, err := chain.MarketSnapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("source failure must remain visible: %v", err)
	}
}

func TestMonitorSnapshotChainGivesLastBackupRemainingParentDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	parentDeadline, _ := ctx.Deadline()
	backupCalled := false
	chain := newMonitorSnapshotChain(
		chainSnapshotFunc(func(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 12*time.Second {
				t.Fatal("primary must still reserve time for the backup")
			}
			return nil, foundation.SourceMeta{}, errors.New("primary disconnected early")
		}),
		chainSnapshotFunc(func(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
			backupCalled = true
			deadline, ok := ctx.Deadline()
			if !ok || !deadline.Equal(parentDeadline) || time.Until(deadline) <= 12*time.Second {
				t.Fatalf("backup discarded remaining parent budget: deadline=%v parent=%v", deadline, parentDeadline)
			}
			return shortMarketRows(fullMarketMinRows), foundation.SourceMeta{Source: "backup"}, nil
		}),
	)
	_, meta, err := chain.MarketSnapshot(ctx)
	if err != nil || !backupCalled || meta.Source != "backup" {
		t.Fatalf("remaining budget did not reach healthy backup: %+v, %v", meta, err)
	}
}

func TestMonitorSnapshotChainStopsOnParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	chain := newMonitorSnapshotChain(chainSnapshotFunc(func(context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
		t.Fatal("cancelled request must not start a provider")
		return nil, foundation.SourceMeta{}, nil
	}))
	if _, _, err := chain.MarketSnapshot(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected original cancellation, got %v", err)
	}
}
