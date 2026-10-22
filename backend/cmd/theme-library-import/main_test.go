package main

import (
	"testing"

	"easy-stock/backend/internal/themelibrary"
)

func TestFlattenGroupsUsesTopLevelFallback(t *testing.T) {
	groups := themelibrary.FlattenGroups(nil, []themelibrary.RawStock{
		{Code: "000938", Name: "紫光股份", Price: 25.6, ChangePercent: 1.106, Hot: 19454},
		{Code: "000938", Name: "紫光股份"},
	})
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if groups[0].SourceID != "top-level" || groups[0].Name != "全部成员" || groups[0].StockCount != 1 {
		t.Fatalf("unexpected fallback group: %+v", groups[0])
	}
	if groups[0].Stocks[0].Code != "000938" || groups[0].Stocks[0].Price != 25.6 {
		t.Fatalf("unexpected fallback stock: %+v", groups[0].Stocks[0])
	}
}

func TestFlattenGroupsKeepsUncategorizedTopLevelStocks(t *testing.T) {
	groups := themelibrary.FlattenGroups(
		[]themelibrary.RawCategory{{
			ID:     "262",
			Name:   "天线",
			Stocks: []themelibrary.RawStock{{Code: "002792", Name: "通宇通讯"}},
		}},
		[]themelibrary.RawStock{
			{Code: "002792", Name: "通宇通讯"},
			{Code: "600000", Name: "浦发银行"},
		},
	)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups[0].Name != "天线" || groups[0].StockCount != 1 {
		t.Fatalf("unexpected category group: %+v", groups[0])
	}
	if groups[1].SourceID != "top-level-other" || groups[1].Name != "其他成员" || groups[1].StockCount != 1 {
		t.Fatalf("unexpected extra group: %+v", groups[1])
	}
	if groups[1].Stocks[0].Code != "600000" {
		t.Fatalf("unexpected extra stock: %+v", groups[1].Stocks[0])
	}
}
