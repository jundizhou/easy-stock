package httpapi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"easy-stock/backend/internal/foundation"
)

type chainFakeSnapshot struct {
	rows []foundation.MarketSnapshotRow
	err  error
}

func (f chainFakeSnapshot) MarketSnapshot(context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	return f.rows, foundation.SourceMeta{Source: "fake"}, f.err
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
