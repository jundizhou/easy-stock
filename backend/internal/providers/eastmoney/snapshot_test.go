package eastmoney

import (
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

func snapshotTestRows() []marketQuoteRow {
	return []marketQuoteRow{
		{symbol: "600519", name: "贵州茅台", close: 1500, changePct: 5.1, amount: 3e9, turnoverRate: 1.2},
		{symbol: "300750", name: "宁德时代", close: 380, changePct: 3.0, amount: 9e9, turnoverRate: 2.4},
		{symbol: "000001", name: "平安银行", close: 11, changePct: 0.5, amount: 1.5e9, turnoverRate: 0.8},
		{symbol: "601127", name: "赛力斯", close: 90, changePct: -3.4, amount: 6e9, turnoverRate: 8.5},
		{symbol: "688981", name: "中芯国际", close: 50, changePct: -7.2, amount: 4e9, turnoverRate: 3.3},
		// 停牌行固定放最后，聚合测试切掉它；parse 阶段的丢弃逻辑由 TestParseSnapshotRowsDropsSuspended 覆盖。
		{symbol: "002230", name: "科大讯飞", close: 0, changePct: 0, amount: 0, turnoverRate: 0},
	}
}

func TestAggregateSnapshotBreadth(t *testing.T) {
	// 停牌行（价格与涨跌幅全 0）在 parse 阶段已被丢弃，聚合前不应出现
	rows := snapshotTestRows()[:len(snapshotTestRows())-1]
	breadth := aggregateSnapshot(rows, foundation.SourceMeta{Source: "test"})

	if breadth.Total != 5 {
		t.Fatalf("total = %d, want 5", breadth.Total)
	}
	if breadth.Up != 3 || breadth.Down != 2 || breadth.Flat != 0 {
		t.Fatalf("breadth up/flat/down = %d/%d/%d, want 3/0/2", breadth.Up, breadth.Flat, breadth.Down)
	}
	if breadth.StrongUp != 2 || breadth.StrongDown != 2 {
		t.Fatalf("strong up/down = %d/%d, want 2/2", breadth.StrongUp, breadth.StrongDown)
	}
	if breadth.UpRatio <= 59 || breadth.UpRatio >= 61 {
		t.Fatalf("up_ratio = %.2f, want ~60", breadth.UpRatio)
	}
	if breadth.MedianPct != 0.5 {
		t.Fatalf("median = %.2f, want 0.5 (sorted: -7.2 -3.4 0.5 3.0 5.1)", breadth.MedianPct)
	}
	if breadth.TotalAmount != 2.35e10 {
		t.Fatalf("total amount = %v", breadth.TotalAmount)
	}
}

func TestAggregateSnapshotDistribution(t *testing.T) {
	rows := snapshotTestRows()[:len(snapshotTestRows())-1]
	breadth := aggregateSnapshot(rows, foundation.SourceMeta{Source: "test"})
	got := map[string]int{}
	for _, bucket := range breadth.Distribution {
		got[bucket.Label] = bucket.Count
		bearSide := strings.HasPrefix(bucket.Label, "-") || strings.HasPrefix(bucket.Label, "≤")
		if bucket.Positive == bearSide {
			t.Errorf("bucket %q positive=%v inconsistent", bucket.Label, bucket.Positive)
		}
	}
	// 10 档总和必须等于样本数，且各档命中正确
	sum := 0
	for _, count := range got {
		sum += count
	}
	if sum != 5 {
		t.Fatalf("distribution sums to %d, want 5: %v", sum, got)
	}
	if got["5~7"] != 1 || got["3~5"] != 1 || got["-5~-3"] != 1 || got["≤-7"] != 1 || got["0~1"] != 1 {
		t.Fatalf("unexpected distribution: %v", got)
	}
}

func TestAggregateSnapshotTopLists(t *testing.T) {
	rows := snapshotTestRows()[:len(snapshotTestRows())-1]
	breadth := aggregateSnapshot(rows, foundation.SourceMeta{Source: "test"})

	if len(breadth.TopGainers) == 0 || breadth.TopGainers[0].Symbol != "600519" {
		t.Fatalf("top gainer = %+v, want 600519 first", breadth.TopGainers)
	}
	if len(breadth.TopLosers) == 0 || breadth.TopLosers[0].Symbol != "688981" {
		t.Fatalf("top loser = %+v, want 688981 first", breadth.TopLosers)
	}
	if len(breadth.TurnoverLeaders) == 0 || breadth.TurnoverLeaders[0].Symbol != "300750" {
		t.Fatalf("turnover leader = %+v, want 300750 (9e9) first", breadth.TurnoverLeaders)
	}
	if len(breadth.ActiveLeaders) == 0 || breadth.ActiveLeaders[0].Symbol != "601127" {
		t.Fatalf("active leader = %+v, want 601127 (8.5%%) first", breadth.ActiveLeaders)
	}
}

func TestParseSnapshotRowsDropsSuspended(t *testing.T) {
	diff := []map[string]any{
		{"f12": "600519", "f14": "贵州茅台", "f2": 1500.5, "f3": 5.1, "f6": 3e9, "f8": 1.2},
		{"f12": "000000", "f14": "停牌股", "f2": "-", "f3": "-", "f6": "-", "f8": "-"},
	}
	rows := parseSnapshotRows(diff)
	if len(rows) != 1 || rows[0].symbol != "600519" {
		t.Fatalf("rows = %+v, want only 600519 (suspended row dropped)", rows)
	}
	if rows[0].close != 1500.5 || rows[0].changePct != 5.1 {
		t.Fatalf("parsed values wrong: %+v", rows[0])
	}
}

func TestNormalizeEastSymbol(t *testing.T) {
	cases := map[string]string{
		"SZ000001": "000001.SZ",
		"sh600519": "600519.SH",
		"BJ430047": "430047.BJ",
		"":         "",
		"600519":   "",
	}
	for input, want := range cases {
		if got := normalizeEastSymbol(input); got != want {
			t.Errorf("normalizeEastSymbol(%q) = %q, want %q", input, got, want)
		}
	}
}
