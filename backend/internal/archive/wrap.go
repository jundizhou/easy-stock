package archive

import (
	"context"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// Record 是落盘到 JSONL 的统一包装：带上采集时间与原始来源，便于离线复用/审计。
type Record[T any] struct {
	ArchivedAt time.Time `json:"archived_at"`
	Kind       string    `json:"kind"` // "realtime" | "news"
	Source     string    `json:"source,omitempty"`
	Payload    T         `json:"payload"`
}

// realtimeKey 实时报价去重键：同一标的同一次成交时间只存一份。
// 盘中价格会变动，所以带上 TradeTime；盘后重复拉取同一快照会被去重。
func realtimeKey(r Record[foundation.Quote]) string {
	q := r.Payload
	if q.Symbol == "" {
		return ""
	}
	if q.TradeTime.IsZero() {
		// 无成交时间（盘后/停牌）时退化为「标的+价格」，价格未变即视为重复。
		return q.Symbol + "|" + strconv.FormatFloat(q.Price, 'f', 4, 64)
	}
	return q.Symbol + "|" + q.TradeTime.Format(time.RFC3339)
}

// newsKey 资讯去重键：优先用源 ID，缺 ID 时退化为标题。
// 财联社等源只提供滚动窗口，逐周期拉取会反复命中同一批，靠此去重。
func newsKey(r Record[foundation.NewsItem]) string {
	it := r.Payload
	if it.ID != "" {
		return "id:" + it.ID
	}
	title := strings.TrimSpace(it.Title)
	if title == "" {
		return ""
	}
	return "title:" + title
}

// RealtimeProvider 与 httpapi.RealtimeProvider 签名一致。
type RealtimeProvider interface {
	Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error)
}

// NewsProvider 与 httpapi.NewsProvider 签名一致。
type NewsProvider interface {
	LatestNews(ctx context.Context, limit int) ([]foundation.NewsItem, error)
}

// RealtimeArchiver 包装实时报价 provider：成功返回后把行情逐条落盘。落盘失败不阻断返回。
type RealtimeArchiver struct {
	inner RealtimeProvider
	opts  Options
	wr    *Writer[Record[foundation.Quote]]
}

func NewRealtimeArchiver(inner RealtimeProvider, root string, opts Options) (*RealtimeArchiver, error) {
	wr, err := New[Record[foundation.Quote]](root, "realtime")
	if err != nil {
		return nil, err
	}
	// [ES_ARCHIVE_DEDUP] 启用跨周期去重。
	wr.WithKey(realtimeKey)
	return &RealtimeArchiver{inner: inner, opts: opts, wr: wr}, nil
}

func (a *RealtimeArchiver) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	quotes, err := a.inner.Realtime(ctx, symbols)
	if err != nil || len(quotes) == 0 {
		return quotes, err
	}
	if !a.opts.enabled() {
		return quotes, nil
	}
	records := make([]Record[foundation.Quote], 0, len(quotes))
	now := time.Now()
	for _, q := range quotes {
		records = append(records, Record[foundation.Quote]{
			ArchivedAt: now,
			Kind:       "realtime",
			Source:     q.Meta.Source,
			Payload:    q,
		})
	}
	if a.opts.Batch {
		_, _ = a.wr.AppendBatch(records)
	} else {
		for _, r := range records {
			_, _ = a.wr.Append(r)
		}
	}
	return quotes, nil
}

// Report 暴露归档状态。
func (a *RealtimeArchiver) Report() Status { return a.wr.Report() }

// NewsArchiver 包装资讯 provider：成功返回后把资讯逐条落盘。落盘失败不阻断返回。
type NewsArchiver struct {
	inner NewsProvider
	opts  Options
	wr    *Writer[Record[foundation.NewsItem]]
}

func NewNewsArchiver(inner NewsProvider, root string, opts Options) (*NewsArchiver, error) {
	wr, err := New[Record[foundation.NewsItem]](root, "news")
	if err != nil {
		return nil, err
	}
	// [ES_ARCHIVE_DEDUP] 启用跨周期去重：滚动窗口重复拉取不再重复落盘。
	wr.WithKey(newsKey)
	return &NewsArchiver{inner: inner, opts: opts, wr: wr}, nil
}

func (a *NewsArchiver) LatestNews(ctx context.Context, limit int) ([]foundation.NewsItem, error) {
	items, err := a.inner.LatestNews(ctx, limit)
	if err != nil || len(items) == 0 {
		return items, err
	}
	if !a.opts.enabled() {
		return items, nil
	}
	records := make([]Record[foundation.NewsItem], 0, len(items))
	now := time.Now()
	for _, it := range items {
		records = append(records, Record[foundation.NewsItem]{
			ArchivedAt: now,
			Kind:       "news",
			Source:     it.Meta.Source,
			Payload:    it,
		})
	}
	if a.opts.Batch {
		_, _ = a.wr.AppendBatch(records)
	} else {
		for _, r := range records {
			_, _ = a.wr.Append(r)
		}
	}
	return items, nil
}

// Report 暴露归档状态。
func (a *NewsArchiver) Report() Status { return a.wr.Report() }

// Options 控制归档行为。
type Options struct {
	// Disabled 为 true 时只计算不落盘（通常用于测试或临时关闭）。
	Disabled bool
	// Batch 为 true 时整批写一行数组（默认 false：逐条 JSONL）。
	Batch bool
}

func (o Options) enabled() bool { return !o.Disabled }
