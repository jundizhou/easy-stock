package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"easy-stock/backend/internal/providers/stockdb"
)

func TestNormalizeBoardSymbol(t *testing.T) {
	cases := map[string]string{
		"000001":     "000001",
		"000001.SZ":  "000001",
		"600519.SH":  "600519",
		"sz000001":   "000001",
		" 600519 ":   "600519",
		"830799.BJ":  "830799",
		"SZ:000001":  "000001",
		"":           "",
		"ABC":        "",
		"60051":      "",
		"60051X.SH":  "",
	}
	for input, want := range cases {
		if got := normalizeBoardSymbol(input); got != want {
			t.Errorf("normalizeBoardSymbol(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRankBoardRefs(t *testing.T) {
	refs := []boardRef{
		{Code: "BK1", Name: "某申万三级", Category: "申万三级"},
		{Code: "BK2", Name: "银行", Category: "概念"},
		{Code: "BK3", Name: "某申万二级", Category: "申万二级"},
		{Code: "BK4", Name: "股份制银行", Category: "申万一级"},
		{Code: "BK5", Name: "保险", Category: "概念"},
	}
	got := rankBoardRefs(refs, 3)
	if len(got) != 3 {
		t.Fatalf("limit=3 应截断为 3，实际 %d", len(got))
	}
	if got[0].Code != "BK4" || got[0].Category != "申万一级" {
		t.Fatalf("第一条应为申万一级，实际 %+v", got[0])
	}
	// 概念内按名称稳定排序：保险 < 银行
	if got[1].Name != "保险" || got[2].Name != "银行" {
		t.Fatalf("概念排序异常：%s, %s", got[1].Name, got[2].Name)
	}
	if empty := rankBoardRefs(nil, 12); empty == nil || len(empty) != 0 {
		t.Fatalf("空输入应返回空切片而非 nil")
	}
}

func TestAtoiInRange(t *testing.T) {
	if v, ok := atoiInRange("12", 1, 60); !ok || v != 12 {
		t.Fatalf("atoiInRange(12) = %d,%v", v, ok)
	}
	if _, ok := atoiInRange("0", 1, 60); ok {
		t.Fatal("0 应越界")
	}
	if _, ok := atoiInRange("61", 1, 60); ok {
		t.Fatal("61 应越界")
	}
	if _, ok := atoiInRange("3x", 1, 60); ok {
		t.Fatal("非数字应拒绝")
	}
}

// TestSymbolBoardsLive 需要本地 7899 在线，且显式 STOCKDB_LIVE=1：
//
//	STOCKDB_LIVE=1 go test ./internal/httpapi/ -run TestSymbolBoardsLive -v
func TestSymbolBoardsLive(t *testing.T) {
	if os.Getenv("STOCKDB_LIVE") == "" {
		t.Skip("set STOCKDB_LIVE=1 to run live test against 127.0.0.1:7899")
	}
	server := &Server{stockDBProvider: stockdb.NewClient()}
	started := time.Now()
	symbolMap, _, err := server.ensureBoardIndex(context.Background())
	if err != nil {
		t.Fatalf("ensureBoardIndex 失败: %v", err)
	}
	t.Logf("板块索引构建 %s，覆盖 %d 只个股", time.Since(started), len(symbolMap))

	refs := symbolMap["000001"]
	if len(refs) == 0 {
		t.Fatal("000001 反查为空")
	}
	ranked := rankBoardRefs(refs, 12)
	t.Logf("000001 归属 %d 个板块，Top%d:", len(refs), len(ranked))
	for i, ref := range ranked {
		t.Logf("  %d. [%s] %s (%s)", i+1, ref.Category, ref.Name, ref.Code)
	}
	if ranked[0].Category != "申万一级" && ranked[0].Category != "概念" {
		t.Fatalf("排序首位类目异常: %s", ranked[0].Category)
	}
}
