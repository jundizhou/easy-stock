// Package monitor 实现监控中心：规则、触发、异动与持仓止盈止损。
//
// 它不承载交易执行，只负责三个问题：
// 一条规则成立了吗？成立过几次？要不要发通知？为了让"判定"可以被独立测试，
// 所有的阈值比较放在 rules.go 的纯函数里，存储与调度都在本文件之外。
package monitor

import (
	"encoding/json"
	"time"
)

// 规则类型。signal 是内置技术信号库（signals.go）：MA 交叉、MACD、
// 布林带、放量缩量，全部"当日事件"语义。sector 是板块监控（sector.go）：
// 规则对象是板块而不是个股。strategy/ladder 依赖参考工程
// 内部的策略信号流，本项目暂无可对应的事件源，不做。
const (
	RuleTypePrice     = "price"     // 价格与涨跌幅阈值
	RuleTypeVolume    = "volume"    // 量比 / 换手 / 成交额
	RuleTypeAbnormal  = "abnormal"  // 涨停 / 炸板 / 跌停 / 新高新低
	RuleTypeDeviation = "deviation" // 交易所异常波动偏离值
	RuleTypeMarket    = "market"    // 大盘：指数涨跌、涨跌家数、涨停家数
	RuleTypeDate      = "date"      // 持仓到期提醒
	RuleTypeSignal    = "signal"    // 内置技术信号库
	RuleTypeSector    = "sector"    // 板块：涨跌幅、资金流、快速异动、广度
)

// 比较算子。cross_* 需要 engine 保存上一次观测值（monitor_state）。
const (
	OpGTE       = "gte"
	OpLTE       = "lte"
	OpBetween   = "between"
	OpCrossUp   = "cross_up"
	OpCrossDown = "cross_down"
	OpEqual     = "eq"
)

// 严重级别与触发结果的落库状态。
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"

	NotifySkipped = "skipped"
	NotifyQueued  = "queued"
	NotifyFailed  = "failed"

	SourceManual   = "manual"
	SourceLot      = "lot"
	SourcePosition = "position"
)

// scope 与市场板块的取值集合。
// inspection 是持仓AI巡检里用户手动维护的持仓组合。
const (
	ScopeSymbols    = "symbols"
	ScopeAll        = "all"
	ScopeWatchlist  = "watchlist"
	ScopePositions  = "positions"
	ScopeInspection = "inspection"
)

// 布尔型 condition 的取值在这里写死，避免调用方随手填个 0.5 也能通过校验。
const booleanFieldValue = 1

// Condition 是一条阈值比较。布尔字段（limit_up 等）的 op 被忽略，
// value 为 0 表示"该信号不成立"，1 表示"成立"。
type Condition struct {
	Field  string   `json:"field"`
	Op     string   `json:"op"`
	Value  float64  `json:"value"`
	Value2 *float64 `json:"value2,omitempty"`
}

// RuleFilters 只对 scope=all 生效：全市场扫描必须能收窄，否则
// 一条"涨停提醒"一天就会产出上千条事件。
type RuleFilters struct {
	Boards      []string `json:"boards,omitempty"`
	ExcludeST   bool     `json:"exclude_st"`
	MinTurnover float64  `json:"min_turnover,omitempty"`
	MinAmount   float64  `json:"min_amount,omitempty"`
	MaxSymbols  int      `json:"max_symbols,omitempty"`
}

type Rule struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Type            string      `json:"type"`
	Enabled         bool        `json:"enabled"`
	Severity        string      `json:"severity"`
	Scope           string      `json:"scope"`
	Symbols         []string    `json:"symbols,omitempty"`
	Filters         RuleFilters `json:"filters,omitempty"`
	Conditions      []Condition `json:"conditions"`
	Logic           string      `json:"logic"`
	CooldownSeconds int         `json:"cooldown_seconds"`
	NotifyChannels  []string    `json:"notify_channels,omitempty"`
	Source          string      `json:"source"`
	LotID           string      `json:"lot_id,omitempty"`
	// RemindDate / LeadDays 只被 type=date 使用：到期提醒没有行情语义，
	// 它需要的全部信息就是"哪一天、提前几天"。
	RemindDate      string     `json:"remind_date,omitempty"`
	LeadDays        int        `json:"lead_days,omitempty"`
	TriggerCount    int        `json:"trigger_count"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// Lot 是一笔手动录入的持仓批次，止盈/止损/到期提醒都挂在它上面。
type Lot struct {
	ID              string    `json:"id"`
	Symbol          string    `json:"symbol"`
	Name            string    `json:"name"`
	Quantity        int       `json:"quantity"`
	CostPrice       float64   `json:"cost_price"`
	BuyDate         string    `json:"buy_date"`
	TargetPct       float64   `json:"target_pct"`
	StopPct         float64   `json:"stop_pct"`
	RemindDate      string    `json:"remind_date"`
	LeadDays        int       `json:"lead_days"`
	Source          string    `json:"source"`
	PositionAccount string    `json:"position_account,omitempty"`
	Detached        bool      `json:"detached"`
	Enabled         bool      `json:"enabled"`
	Note            string    `json:"note,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Event 是一次触发记录。rule_name/rule_type 是快照：规则改名或删除后，
// 历史记录仍然要能自解释。
type Event struct {
	ID            int64          `json:"id"`
	RuleID        string         `json:"rule_id"`
	RuleName      string         `json:"rule_name"`
	RuleType      string         `json:"rule_type"`
	Symbol        string         `json:"symbol,omitempty"`
	Name          string         `json:"name,omitempty"`
	Severity      string         `json:"severity"`
	TriggerKind   string         `json:"trigger_kind"`
	Message       string         `json:"message"`
	Detail        map[string]any `json:"detail,omitempty"`
	Price         float64        `json:"price,omitempty"`
	ChangePercent float64        `json:"change_percent,omitempty"`
	TradeDate     string         `json:"trade_date,omitempty"`
	NotifyStatus  string         `json:"notify_status"`
	NotifyError   string         `json:"notify_error,omitempty"`
	NotifiedAt    *time.Time     `json:"notified_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// EventFilter 是事件列表的查询参：全部可选，全部组合都走同一份 SQL 拼装。
type EventFilter struct {
	RuleID      string
	RuleType    string
	Symbol      string
	Severity    string
	TradeDate   string
	SinceID     int64
	Limit       int
	OldestFirst bool
}

// Setting 集合。Enabled=false 时引擎完全空转——这是刻意的默认，
// 升级部署不应让后台突然开始轮询上游。
type Settings struct {
	Enabled             bool   `json:"enabled"`
	QuoteIntervalSec    int    `json:"quote_interval_sec"`
	SnapshotIntervalSec int    `json:"snapshot_interval_sec"`
	QuietHours          string `json:"quiet_hours"` // "23:00-08:30"，空表示不静默
	DigestSeconds       int    `json:"digest_seconds"`
}

func DefaultSettings() Settings {
	return Settings{
		QuoteIntervalSec:    3,
		SnapshotIntervalSec: 15,
		QuietHours:          "23:00-08:30",
		DigestSeconds:       300,
	}
}

// Signal 是一次符合条件的最小事实：哪个字段、什么值、超过什么阈值。
type Signal struct {
	ConditionIndex int
	Kind           string
	Field          string
	Value          float64
	Threshold      float64
	Message        string
}

// RuleEventPair 把"哪条规则"和"哪个信号"绑在一起输出，emit 阶段一次遍历生成事件。
type RuleEventPair struct {
	Rule   Rule
	Symbol string
	Name   string
	Signal Signal
}

// 常用的 JSON 快捷转换。事件的 detail 与规则的符号列表都以 JSON 文本落库，
// 转换失败在这些场景里只意味着字段缺失，不值得让一轮扫描失败。
func marshalJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func unmarshalJSON(data string, target any) {
	_ = json.Unmarshal([]byte(data), target)
}
