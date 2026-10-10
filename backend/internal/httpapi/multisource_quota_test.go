package httpapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

// quotaAwareNews 记录收到的 limit，用于验证配额分配。
type quotaAwareNews struct {
	name      string
	gotLimits []int
	// makeItems 按请求的 limit 生成对应数量的条目
	makeItems func(limit int, name string) []foundation.NewsItem
}

func (f *quotaAwareNews) LatestNews(_ context.Context, limit int) ([]foundation.NewsItem, error) {
	f.gotLimits = append(f.gotLimits, limit)
	if f.makeItems == nil {
		return nil, nil
	}
	return f.makeItems(limit, f.name), nil
}

func mkItems(limit int, name string) []foundation.NewsItem {
	out := make([]foundation.NewsItem, 0, limit)
	base := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	for i := 0; i < limit; i++ {
		out = append(out, foundation.NewsItem{
			ID:          fmt.Sprintf("%s-%d", name, i),
			Title:       fmt.Sprintf("%s item %d", name, i),
			PublishedAt: base.Add(-time.Duration(i) * time.Minute),
			Meta:        foundation.SourceMeta{Source: name},
		})
	}
	return out
}

// TestQuotaFor 验证配额均分（含余数分配）。
func TestQuotaFor(t *testing.T) {
	cases := []struct {
		idx, n, limit, want int
	}{
		{0, 2, 20, 10}, // 20/2 = 10
		{1, 2, 20, 10},
		{0, 3, 20, 7}, // 20/3 = 6 余 2 → 前 2 个源各 +1
		{1, 3, 20, 7},
		{2, 3, 20, 6},
		{0, 1, 20, 20}, // 单源拿全部
		{0, 3, 2, 1},   // 小 limit：前 2 个各 1，第 3 个至少 1（不饿死）
		{1, 3, 2, 1},
		{2, 3, 2, 1},
	}
	for _, c := range cases {
		got := quotaFor(c.idx, c.n, c.limit)
		if got != c.want {
			t.Errorf("quotaFor(%d,%d,%d) = %d, want %d",
				c.idx, c.n, c.limit, got, c.want)
		}
	}
}

// TestMultiNewsQuotaPreventsMonopoly 是本修复的核心验收测试。
//
// 复现原缺陷：主源发布密集会占满全部名额，备用源被系统性挤出。
// 修复后两个源都应出现在结果中（各有配额）。
func TestMultiNewsQuotaPreventsMonopoly(t *testing.T) {
	primary := &quotaAwareNews{name: "cls", makeItems: mkItems}
	secondary := &quotaAwareNews{name: "eastmoney", makeItems: mkItems}

	m := NewMultiNews(primary, secondary)
	out, err := m.LatestNews(context.Background(), 20)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// 断言 1：两个源都被请求，且配额均分
	if len(primary.gotLimits) != 1 || primary.gotLimits[0] != 10 {
		t.Fatalf("主源配额 = %v, want [10]", primary.gotLimits)
	}
	if len(secondary.gotLimits) != 1 || secondary.gotLimits[0] != 10 {
		t.Fatalf("备源配额 = %v, want [10]", secondary.gotLimits)
	}

	// 断言 2：结果中两个源都有代表（按 source 计数）
	counts := map[string]int{}
	for _, it := range out {
		counts[it.Meta.Source]++
	}
	if counts["cls"] == 0 {
		t.Fatalf("结果中 cls 条数 = 0，配额分配失败: %v", counts)
	}
	if counts["eastmoney"] == 0 {
		t.Fatalf("结果中 eastmoney 条数 = 0（单源垄断未被修复）: %v", counts)
	}
	if len(out) != 20 {
		t.Fatalf("结果总数 = %d, want 20", len(out))
	}
}

// TestMultiNewsSourceLabeled 验证主源也会被正确标记 source。
//
// 原实现只在**回退源**时改写 Meta.Source，主源成功时保持 provider 自己写的内容。
// 若 provider 未填 source，归档侧 `Record.Source` 会为空 → 无法区分多源。
func TestMultiNewsSourceLabeled(t *testing.T) {
	// 模拟「provider 不填 Meta.Source」的情况
	blank := func(limit int, _ string) []foundation.NewsItem {
		out := make([]foundation.NewsItem, 0, limit)
		for i := 0; i < limit; i++ {
			out = append(out, foundation.NewsItem{
				ID: fmt.Sprintf("blank-%d", i), Title: fmt.Sprintf("t%d", i),
			})
		}
		return out
	}
	p := &quotaAwareNews{name: "cls", makeItems: blank}
	s := &quotaAwareNews{name: "eastmoney", makeItems: blank}

	m := NewMultiNews(p, s)
	out, err := m.LatestNews(context.Background(), 4)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, it := range out {
		if it.Meta.Source == "" {
			t.Fatalf("条目 %q 的 Meta.Source 为空 → 归档无法区分来源", it.ID)
		}
	}
}

// TestMultiNewsFallbackStillWorks 确保配额改造未破坏故障回退。
func TestMultiNewsFallbackStillWorks(t *testing.T) {
	primary := &quotaAwareNews{name: "cls"} // 返回 nil, nil → 视为失败
	secondary := &quotaAwareNews{name: "eastmoney", makeItems: mkItems}

	m := NewMultiNews(primary, secondary)
	out, err := m.LatestNews(context.Background(), 10)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("主源失败时应由备源顶上，结果不应为空")
	}
	for _, it := range out {
		if it.Meta.Source != "eastmoney" {
			t.Fatalf("回退后来源 = %q, want eastmoney", it.Meta.Source)
		}
	}
}
