// Package screener 提供面向全 A 股的策略选股引擎。
//
// 两级策略：
//   - snapshot：基于东财全市场实时快照字段（量比/换手/市值/净流入/多日涨幅）
//     的条件筛选，毫秒级内存过滤，覆盖大部分经典场景；
//   - kline：基于日 K 指标（MA/MACD/RSI/KDJ/BOLL/量价）的形态策略，只对
//     受控规模的股票池计算（快照命中池 + 成交额保底池，默认上限 500 只），
//     避免全市场逐只拉 K 线打爆上游。
package screener

import "time"

// Kind 区分策略依赖的数据层级。
type Kind string

const (
	// KindSnapshot 只需要全市场快照字段即可判断。
	KindSnapshot Kind = "snapshot"
	// KindKline 需要逐只拉日 K 并计算技术指标。
	KindKline Kind = "kline"
)

// ParamSpec 描述一个可在前端调节的数值参数。
type ParamSpec struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	Default float64 `json:"default"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Step    float64 `json:"step"`
	Unit    string  `json:"unit,omitempty"`
}

// Strategy 是一个内置选股策略的元数据。
type Strategy struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Category    string      `json:"category"`
	Kind        Kind        `json:"kind"`
	Description string      `json:"description"`
	Params      []ParamSpec `json:"params,omitempty"`
}

// Options 是一次选股运行的运行选项。
type Options struct {
	// ExcludeST 排除 ST / *ST / 退市整理股。
	ExcludeST bool `json:"exclude_st"`
	// ExcludeNew 排除上市不足 60 个交易日的次新股（按可得K线数量判断）。
	ExcludeNew bool `json:"exclude_new"`
	// MinAmount 亿为单位的最小成交额过滤，0 表示不过滤。
	MinAmountYi float64 `json:"min_amount_yi"`
	// KlineUniverseLimit 是 K 线策略实际计算的最大股票数。
	KlineUniverseLimit int `json:"kline_universe_limit"`
}

// Request 描述一次选股运行。
type Request struct {
	StrategyIDs []string `json:"strategy_ids"`
	Options     Options  `json:"options"`
}

// Hit 是一只命中策略的股票。
type Hit struct {
	Symbol        string             `json:"symbol"`
	Name          string             `json:"name"`
	Close         float64            `json:"close"`
	ChangePercent float64            `json:"change_percent"`
	Amount        float64            `json:"amount"`
	TurnoverRate  float64            `json:"turnover_rate"`
	VolumeRatio   float64            `json:"volume_ratio"`
	FloatCapYi    float64            `json:"float_cap_yi"`
	Strategies    []string           `json:"strategies"`
	Details       map[string]string  `json:"details,omitempty"`
	Indicators    map[string]float64 `json:"indicators,omitempty"`
}

// Result 是一次选股运行的输出。
type Result struct {
	Strategies   []string      `json:"strategies"`
	Options      Options       `json:"options"`
	Scanned      int           `json:"scanned"`
	Matched      int           `json:"matched"`
	KlineCount   int           `json:"kline_count"`
	KlineFailed  int           `json:"kline_failed"`
	SnapshotMs   int64         `json:"snapshot_ms"`
	KlineMs      int64         `json:"kline_ms"`
	ElapsedMs    int64         `json:"elapsed_ms"`
	Hits         []Hit         `json:"hits"`
	FailedStocks []FailedStock `json:"failed_stocks,omitempty"`
	GeneratedAt  time.Time     `json:"generated_at"`
	Warnings     []string      `json:"warnings,omitempty"`
}

// FailedStock 记录 K 线拉取失败的单只股票。
type FailedStock struct {
	Symbol string `json:"symbol"`
	Error  string `json:"error"`
}
