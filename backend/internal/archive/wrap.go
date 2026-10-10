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

// [ES_NEWS_RELEVANCE] 资讯相关性标记（V0.2 任务五 P2）。
//
// 背景：实测 10-09 归档 33 条唯一资讯，主题材命中率仅 27%，
// 其余为海外宏观/地缘资讯（加拿大央行、印度股票、ICC 制裁等），
// 与 A 股题材无关却会计入题材资讯量 → 稀释真实信号。
//
// 方案：在归档时用 Tags 字段写入 "relevance:LEVEL" 标记。
//
//	· **不丢弃任何资讯**（审计需要完整原始流）
//	· 下游聚合可按标记过滤，纯 Python 侧实现（news_relevance.py）
//	· Go 侧只做轻量关键词判别，避免引入重依赖
const (
	relevanceRelevant   = "relevance:RELEVANT"
	relevanceWeak       = "relevance:WEAK"
	relevanceIrrelevant = "relevance:IRRELEVANT"
)

// irrelevantMarkers 明显与 A 股题材无关的特征词（海外宏观/地缘）。
// 与 Python 侧 news_relevance.py 的 GEO/IRRELEVANT 模式保持同源语义。
var irrelevantMarkers = []string{
	// 地缘 / 国际政治
	"霍尔木兹", "红海", "以色列", "乌克兰", "北约", "国际刑事法院",
	"特朗普", "白宫", "美国国会", "参议院", "众议院", "欧盟委员会",
	// 非中国央行
	"美联储", "加拿大央行", "欧洲央行", "日本央行", "英国央行", "澳洲联储",
	// 海外市场
	"印度股票", "日经", "韩国综指", "德国DAX", "富时", "标普", "纳斯达克",
	"道琼斯", "美债", "加元", "卢比",
	// 其他无关
	"疾控中心", "麻疹", "新闻联播", "盘前要闻一览",
}

// weakMarkers 弱信号特征词（可能是题材线索，但需配合其它证据）。
var weakMarkers = []string{
	"涨价", "提价", "供需紧张", "缺货", "中标", "订单", "签约",
	"战略合作", "补贴", "试点", "业绩预增", "超预期",
}

// classifyNewsRelevance 轻量相关性判别，返回 relevance 标记。
//
// 优先级：无关特征 > 弱信号 > 默认 RELEVANT。
// 之所以「默认 RELEVANT」：Go 侧无主题表，宁可放过不可错杀；
// 精确的题材命中判定由 Python 侧 news_relevance.py 补充。
func classifyNewsRelevance(title, content string) string {
	text := title + " " + content
	for _, m := range irrelevantMarkers {
		if strings.Contains(text, m) {
			return relevanceIrrelevant
		}
	}
	for _, m := range weakMarkers {
		if strings.Contains(text, m) {
			return relevanceWeak
		}
	}
	return relevanceRelevant
}

// tagRelevance 把 relevance 标记注入 NewsItem.Tags（幂等）。
func tagRelevance(it foundation.NewsItem) foundation.NewsItem {
	const prefix = "relevance:"
	for _, t := range it.Tags {
		if strings.HasPrefix(t, prefix) {
			return it // 已有标记，不重复注入
		}
	}
	level := classifyNewsRelevance(it.Title, it.Content)
	tags := make([]string, 0, len(it.Tags)+1)
	tags = append(tags, it.Tags...)
	it.Tags = append(tags, level)
	return it
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
	// [ES_ARCHIVE_DEDUP] 启用跨周期去重（含跨进程持久化层）。
	attachDedup(wr, realtimeKey, opts)
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
	// [ES_ARCHIVE_DEDUP] 启用跨周期去重（含跨进程持久化层）：
	// 滚动窗口重复拉取不再重复落盘。
	attachDedup(wr, newsKey, opts)
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
		// [ES_NEWS_RELEVANCE] 注入相关性标记：归档保留全量，
		// 但下游可按 relevance:* 过滤噪声（海外宏观/地缘资讯）。
		payload := it
		if !a.opts.NoRelevanceTag {
			payload = tagRelevance(it)
		}
		records = append(records, Record[foundation.NewsItem]{
			ArchivedAt: now,
			Kind:       "news",
			Source:     payload.Meta.Source,
			Payload:    payload,
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
	// NoPersistentDedup 为 true 时关闭跨进程持久化去重（退回纯内存去重）。
	//
	// [ES_ARCHIVE_DEDUP_PERSIST] 默认开启：归档调度器在一天内可能被启动多次
	// （实测 10-09 启动 8 次），而 seen 是内存 map，每次重启清零 → 同一批滚动
	// 数据被重复落盘（realtime 重复率实测 91.7%）。开启后按日落盘键集合，
	// 重启仍能去重。仅在需要对照实验或排障时关闭。
	NoPersistentDedup bool
	// NoRelevanceTag 为 true 时不给资讯注入 relevance 标记。
	//
	// [ES_NEWS_RELEVANCE] 默认开启：给每条资讯写入 relevance:LEVEL 标记，
	// 供下游过滤海外宏观/地缘噪声（实测占比 54.5%）。
	// 归档仍保留全量资讯，标记只影响下游聚合。
	NoRelevanceTag bool
}

func (o Options) enabled() bool { return !o.Disabled }

// persistentDedupEnabled 是否启用跨进程持久化去重。
func (o Options) persistentDedupEnabled() bool { return !o.NoPersistentDedup }

// attachDedup 为 Writer 挂上去重层（默认含持久化层）。
//
// 持久化层初始化失败时**静默降级**为纯内存去重：归档属于旁路能力，
// 绝不能因为去重键文件写不了而阻断主流程（与 archive 包既有约定一致）。
func attachDedup[T any](wr *Writer[T], keyOf func(T) string, opts Options) {
	if !opts.persistentDedupEnabled() {
		wr.WithKey(keyOf)
		return
	}
	ps, err := NewPersistentSeen(wr.Dir())
	if err != nil {
		wr.WithKey(keyOf)
		return
	}
	wr.WithKeyPersistent(keyOf, ps)
}
