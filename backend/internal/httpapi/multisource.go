package httpapi

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/cls"
	"easy-stock/backend/internal/providers/eastmoney"
	"easy-stock/backend/internal/providers/sina"
	"easy-stock/backend/internal/providers/tencent"
)

// 以下多源包装镜像 quant-stock 的「多接口共用 / 故障转移 / 冷却」设计，
// 把 sina+tencent（实时报价）、cls+eastmoney（实时资讯）做成双源，
// 从架构上消除「单接口被封被限制」的单点。

const (
	multiSourceCooldown = 30 * time.Second // 连续失败 2 次后冷却时长
	multiSourceMaxFail  = 2
)

type multiEntry struct {
	name          string
	provider      any // RealtimeProvider 或 NewsProvider
	failures      int
	cooldownUntil time.Time
}

func sourceName(p any, idx int) string {
	switch p.(type) {
	case *sina.Client:
		return "sina"
	case *tencent.Client:
		return "tencent"
	case *cls.Client:
		return "cls"
	case *eastmoney.Client:
		return "eastmoney"
	default:
		return fmt.Sprintf("source-%d", idx)
	}
}

// ----------------------------------------------------------------------------
// 实时报价：sina（主） + tencent（备）
// ----------------------------------------------------------------------------

type multiRealtime struct {
	mu      sync.Mutex
	entries []multiEntry
}

// NewMultiRealtime 构建实时报价多源。第一个参数为首选源（通常 sina），
// 其余为依次回退的备用源（通常 tencent）。任一源连续失败 2 次进入 30s 冷却，
// 期间自动跳过，改由健康源服务，避免反复访问被封/被限流的源。
func NewMultiRealtime(primary RealtimeProvider, others ...RealtimeProvider) RealtimeProvider {
	all := append([]RealtimeProvider{primary}, others...)
	m := &multiRealtime{}
	for i, p := range all {
		m.entries = append(m.entries, multiEntry{name: sourceName(p, i), provider: p})
	}
	return m
}

func (m *multiRealtime) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	m.mu.Lock()
	now := time.Now()
	pending := make([]int, 0, len(m.entries))
	for i, e := range m.entries {
		if now.Before(e.cooldownUntil) {
			continue
		}
		pending = append(pending, i)
	}
	m.mu.Unlock()

	var lastErr error
	for _, i := range pending {
		provider := m.entries[i].provider.(RealtimeProvider)
		quotes, err := provider.Realtime(ctx, symbols)
		if err != nil || len(quotes) == 0 {
			lastErr = err
			m.markFailure(i)
			continue
		}
		m.markSuccess(i)
		if i != 0 {
			for k := range quotes {
				if quotes[k].Meta.FallbackReason == "" {
					quotes[k].Meta.FallbackReason = "primary source unavailable, served by " + m.entries[i].name
				}
				quotes[k].Meta.Source = m.entries[i].name
			}
		}
		return quotes, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("all realtime sources failed")
}

func (m *multiRealtime) markFailure(i int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[i].failures++
	if m.entries[i].failures >= multiSourceMaxFail {
		m.entries[i].cooldownUntil = time.Now().Add(multiSourceCooldown)
	}
}

func (m *multiRealtime) markSuccess(i int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[i].failures = 0
	m.entries[i].cooldownUntil = time.Time{}
}

// Statuses 暴露各源健康状态（供 /api/v1/sources 等端点使用）。
func (m *multiRealtime) Statuses() []foundation.SourceHealth {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]foundation.SourceHealth, 0, len(m.entries))
	now := time.Now()
	for _, e := range m.entries {
		ok := !now.Before(e.cooldownUntil)
		msg := ""
		if !ok {
			msg = "cooldown (consecutive failures)"
		}
		out = append(out, foundation.SourceHealth{
			ID:        e.name,
			Name:      e.name + " (realtime)",
			Category:  "quote",
			OK:        ok,
			Message:   msg,
			CheckedAt: now,
		})
	}
	return out
}

// ----------------------------------------------------------------------------
// 实时资讯：cls 财联社快讯（主） + eastmoney 公告（备）
// ----------------------------------------------------------------------------

type multiNews struct {
	mu      sync.Mutex
	entries []multiEntry
}

// NewMultiNews 构建实时资讯多源。第一个参数为首选源（通常 cls 快讯），
// 其余为回退源（通常 eastmoney 公告）。多源正常时**按配额合并**去重返回，
// 单源失败时由另一源顶上，同样带冷却保护。
//
// [ES_NEWS_QUOTA] 配额分配（修正单源垄断）：
// 早期实现给**每个源**都传完整 limit，合并后统一截断到 limit。
// 由于主源 cls 快讯发布密集、时间戳更新，排序后几乎占满全部名额，
// eastmoney 公告被系统性挤出（实测 10-09 归档 44 条 100% 为 cls）。
// 现在改为：limit 在 N 个源之间**均分**（余数给主源），
// 使每个源都有固定配额 → 保证多源真实生效。
func NewMultiNews(primary NewsProvider, others ...NewsProvider) NewsProvider {
	all := append([]NewsProvider{primary}, others...)
	m := &multiNews{}
	for i, p := range all {
		m.entries = append(m.entries, multiEntry{name: sourceName(p, i), provider: p})
	}
	return m
}

// quotaFor 计算第 i 个源的取数配额（共 n 个源，总 limit）。
// 均分，余数优先给靠前的源（主源）。
func quotaFor(idx, n, limit int) int {
	if n <= 0 {
		return limit
	}
	base := limit / n
	rem := limit % n
	if idx < rem {
		return base + 1
	}
	if base <= 0 {
		return 1 // 至少给 1，避免源被完全饿死
	}
	return base
}

func (m *multiNews) LatestNews(ctx context.Context, limit int) ([]foundation.NewsItem, error) {
	if limit <= 0 {
		limit = 20
	}
	m.mu.Lock()
	now := time.Now()
	pending := make([]int, 0, len(m.entries))
	for i, e := range m.entries {
		if now.Before(e.cooldownUntil) {
			continue
		}
		pending = append(pending, i)
	}
	total := len(m.entries)
	m.mu.Unlock()

	merged := make([]foundation.NewsItem, 0, limit)
	var lastErr error
	for _, i := range pending {
		provider := m.entries[i].provider.(NewsProvider)
		// 按配额取数，而非每个源都取 limit
		q := quotaFor(i, total, limit)
		items, err := provider.LatestNews(ctx, q)
		if err != nil || len(items) == 0 {
			lastErr = err
			m.markFailure(i)
			continue
		}
		m.markSuccess(i)
		// 标记真实来源（主源此前不改写，导致归档无法区分多源）
		for k := range items {
			if items[k].Meta.Source == "" {
				items[k].Meta.Source = m.entries[i].name
			}
		}
		merged = append(merged, items...)
	}
	if len(merged) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("all news sources returned no items")
	}

	// 去重（按 ID，退化标题）
	seen := make(map[string]struct{}, len(merged))
	deduped := make([]foundation.NewsItem, 0, len(merged))
	for _, it := range merged {
		key := it.ID
		if key == "" {
			key = it.Title
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, it)
	}
	sort.SliceStable(deduped, func(a, b int) bool {
		ta, tb := deduped[a].PublishedAt, deduped[b].PublishedAt
		if ta.IsZero() != tb.IsZero() {
			return !ta.IsZero()
		}
		return ta.After(tb)
	})
	if len(deduped) > limit {
		deduped = deduped[:limit]
	}
	return deduped, nil
}

func (m *multiNews) markFailure(i int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[i].failures++
	if m.entries[i].failures >= multiSourceMaxFail {
		m.entries[i].cooldownUntil = time.Now().Add(multiSourceCooldown)
	}
}

func (m *multiNews) markSuccess(i int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[i].failures = 0
	m.entries[i].cooldownUntil = time.Time{}
}

func (m *multiNews) Statuses() []foundation.SourceHealth {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]foundation.SourceHealth, 0, len(m.entries))
	now := time.Now()
	for _, e := range m.entries {
		ok := !now.Before(e.cooldownUntil)
		msg := ""
		if !ok {
			msg = "cooldown (consecutive failures)"
		}
		out = append(out, foundation.SourceHealth{
			ID:        e.name,
			Name:      e.name + " (news)",
			Category:  "news",
			OK:        ok,
			Message:   msg,
			CheckedAt: now,
		})
	}
	return out
}
