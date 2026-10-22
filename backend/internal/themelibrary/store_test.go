package themelibrary

import (
	"context"
	"strings"
	"testing"
)

func testSeed() Seed {
	return Seed{
		SchemaVersion:   1,
		Source:          "bigamap",
		SourceName:      "开盘啦题材库",
		SourceURL:       "https://www.bigamap.cn/app",
		APIURL:          "https://www.bigamap.cn/api/v1/public/theme-library",
		SourceUpdatedAt: "2026-10-19T09:41:51Z",
		GeneratedAt:     "2026-10-19T10:00:00Z",
		Selection:       "test",
		ThemeCount:      2,
		Themes: []Theme{
			{
				SourceID:       "42",
				Name:           "5G-A",
				Description:    "<p>5G-A 商用部署</p>",
				Introduction:   "<p>题材相关新闻：</p><p>中国移动发布 5G-A。</p>",
				ChangePercent:  0.67,
				StockCount:     2,
				LimitUpCount:   1,
				Hot:            8977,
				ThemeUpdatedAt: "2026-07-24T06:33:04Z",
				AsOf:           "2026-10-19T09:41:51Z",
				Groups: []Group{
					{
						SourceID:      "262",
						Name:          "天线",
						Path:          []string{"天线"},
						ChangePercent: 0.69,
						Stocks: []Stock{
							{Code: "002792", Name: "通宇通讯", Price: 26.69, ChangePercent: 1.56, Hot: 466, Tags: []string{"5G", "6G"}, Description: "<p>通信天线供应商</p>"},
							{Code: "002446", Name: "盛路通信", Price: 7.11, ChangePercent: -1.42, Hot: 375},
						},
					},
					{
						SourceID:       "300",
						Name:           "射频 · 滤波器",
						ParentSourceID: "299",
						ParentName:     "射频",
						Path:           []string{"射频", "滤波器"},
						Stocks: []Stock{
							{Code: "002792", Name: "通宇通讯", Price: 26.69, ChangePercent: 1.56, IsLimitUp: true, Hot: 466},
						},
					},
				},
			},
			{
				SourceID:    "100",
				Name:        "测试题材",
				Description: "仅用于测试",
				StockCount:  1,
				Hot:         10,
				Groups: []Group{
					{SourceID: "1", Name: "测试分组", Stocks: []Stock{{Code: "600000", Name: "浦发银行"}}},
				},
			},
		},
	}
}

func TestStoreListSearchAndDetail(t *testing.T) {
	store, err := OpenWithSeed(":memory:", testSeed())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	library, err := store.List(ctx, Filter{Sort: "hot", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !library.Available || library.Total != 2 || len(library.Items) != 2 {
		t.Fatalf("unexpected library: %+v", library)
	}
	if library.Items[0].SourceID != "42" || library.Items[0].GroupCount != 2 {
		t.Fatalf("unexpected first item: %+v", library.Items[0])
	}
	if library.Stats.Themes != 2 || library.Stats.Groups != 3 || library.Stats.Relations != 4 || library.Stats.Stocks != 3 {
		t.Fatalf("unexpected stats: %+v", library.Stats)
	}
	if library.Meta.SourceUpdatedAt != "2026-10-19T09:41:51Z" || library.Meta.GeneratedAt != "2026-10-19T10:00:00Z" {
		t.Fatalf("unexpected meta: %+v", library.Meta)
	}

	byStock, err := store.List(ctx, Filter{Query: "002446", Limit: 10})
	if err != nil {
		t.Fatalf("search by stock: %v", err)
	}
	if byStock.Total != 1 || byStock.Items[0].SourceID != "42" {
		t.Fatalf("unexpected stock search: %+v", byStock)
	}
	byName, err := store.List(ctx, Filter{Query: "5G", Limit: 10})
	if err != nil {
		t.Fatalf("search by name: %v", err)
	}
	if byName.Total != 1 {
		t.Fatalf("unexpected name search: %+v", byName)
	}
	byGroup, err := store.List(ctx, Filter{Query: "滤波器", Limit: 10})
	if err != nil {
		t.Fatalf("search by group: %v", err)
	}
	if byGroup.Total != 1 || byGroup.Items[0].SourceID != "42" {
		t.Fatalf("unexpected group search: %+v", byGroup)
	}

	byLimitUp, err := store.List(ctx, Filter{Sort: "limit", Limit: 10})
	if err != nil {
		t.Fatalf("sort by limit up: %v", err)
	}
	if len(byLimitUp.Items) != 2 || byLimitUp.Items[0].SourceID != "42" {
		t.Fatalf("unexpected limit-up sort: %+v", byLimitUp)
	}

	detail, err := store.Get(ctx, "42")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if detail.Name != "5G-A" || len(detail.Groups) != 2 || detail.RelationCount != 3 {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	if strings.Contains(detail.Introduction, "<p>") || !strings.Contains(detail.Introduction, "中国移动") {
		t.Fatalf("introduction not sanitized: %q", detail.Introduction)
	}
	if detail.Groups[1].Path[0] != "射频" || detail.Groups[1].ParentName != "射频" {
		t.Fatalf("unexpected group path: %+v", detail.Groups[1])
	}
	if detail.Groups[0].Stocks[0].Tags[0] != "5G" || detail.Groups[0].Stocks[0].Description != "通信天线供应商" {
		t.Fatalf("unexpected stock: %+v", detail.Groups[0].Stocks[0])
	}
	if !detail.Groups[1].Stocks[0].IsLimitUp {
		t.Fatalf("limit-up flag not restored")
	}

	if _, err := store.Get(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEmbeddedSeedIsUsable(t *testing.T) {
	seed, err := EmbeddedSeed()
	if err != nil {
		t.Fatalf("embedded seed: %v", err)
	}
	if seed.ThemeCount < 30 || len(seed.Themes) != seed.ThemeCount {
		t.Fatalf("unexpected embedded seed size: %d/%d", seed.ThemeCount, len(seed.Themes))
	}
	if strings.Contains(seed.Themes[0].Introduction, "<p>") {
		t.Fatalf("embedded introduction still contains HTML")
	}
	if seed.SourceUpdatedAt == "" || seed.GeneratedAt == "" {
		t.Fatalf("embedded seed is missing timestamps")
	}
}

func TestSanitizeHTML(t *testing.T) {
	input := `<p style="font-family: 宋体"><strong>题材相关新闻：</strong></p><p>第一段&nbsp;内容</p><ul><li>要点一</li><li>要点二</li></ul><script>alert(1)</script>`
	got := SanitizeHTML(input)
	for _, forbidden := range []string{"<p", "<script", "alert(1)", "&nbsp;"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("sanitized output still contains %q: %q", forbidden, got)
		}
	}
	for _, expected := range []string{"题材相关新闻：", "第一段 内容", "要点一", "要点二"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("sanitized output missing %q: %q", expected, got)
		}
	}
}
