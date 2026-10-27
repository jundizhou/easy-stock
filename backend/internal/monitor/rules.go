package monitor

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// limitPricePercent 是各板块的涨跌幅限制。北交所 30% 不是笔误；
// ST 创业板/科创板没有特殊档位，仍为 20%。
func limitPricePercent(board string, isST bool) float64 {
	switch board {
	case foundation.BoardSTAR, foundation.BoardChiNext:
		return 20
	case foundation.BoardBSE:
		return 30
	default:
		// 2026-07-06 起主板风险警示股票与普通股均为 10%。
		return 10
	}
}

// LimitPrice 计算涨停价，LimitDownPrice 计算跌停价。
//
// 交易所的口径是"四舍五入到分"。用 round 而不是截断：截断会把
// 例如 10.03 * 1.1 = 11.033 算成 11.03（相同结果）但 10.06 * 1.1 = 11.066
// 截断成 11.06 而正确是 11.07——差一分钱就会把已涨停判成未涨停。
func LimitPrice(board string, isST bool, previousClose float64) float64 {
	return roundPrice(previousClose * (1 + limitPricePercent(board, isST)/100))
}

func LimitDownPrice(board string, isST bool, previousClose float64) float64 {
	return roundPrice(previousClose * (1 - limitPricePercent(board, isST)/100))
}

// priceEpsilon 允许半分钱的浮点误差。行情源偶尔给出三位小数，
// 直接用 == 判等会把"其实是涨停"判掉。
const priceEpsilon = 0.005

func roundPrice(value float64) float64 {
	return math.Round(value*100) / 100
}

// limitState 返回一层信息的涨停状态：0=普通，1=涨停，2=炸板。
func limitState(row foundation.MarketSnapshotRow) int {
	if row.Suspended || row.PreviousClose <= 0 || row.Price <= 0 {
		return 0
	}
	limit := LimitPrice(row.Board, row.IsST, row.PreviousClose)
	atLimit := row.Price > limit-priceEpsilon
	touchedLimit := row.High > limit-priceEpsilon
	switch {
	case atLimit:
		return 1
	case touchedLimit:
		return 2
	default:
		return 0
	}
}

// IsLimitDown 判断是否封死跌停。
func IsLimitDown(row foundation.MarketSnapshotRow) bool {
	if row.Suspended || row.PreviousClose <= 0 || row.Price <= 0 {
		return false
	}
	return row.Price < LimitDownPrice(row.Board, row.IsST, row.PreviousClose)+priceEpsilon
}

// GapLimit 判断低开跌停附近（跳空跌停开盘）：
// 开盘价即触及跌停价。用于把"低开跌停"从普通跌停里区分出来，避免把
// 集合竞价的一次低开当成盘中砸到跌停。
func GapLimit(row foundation.MarketSnapshotRow) bool {
	if row.Suspended || row.PreviousClose <= 0 || row.Open <= 0 {
		return false
	}
	return row.Open < LimitDownPrice(row.Board, row.IsST, row.PreviousClose)+priceEpsilon
}

// ---------------------------------------------------------------- 规则校验

func validRuleTypes() map[string]bool {
	return map[string]bool{
		RuleTypePrice: true, RuleTypeVolume: true, RuleTypeAbnormal: true,
		RuleTypeDeviation: true, RuleTypeMarket: true, RuleTypeDate: true,
		RuleTypeSignal: true, RuleTypeSector: true,
	}
}

func validScopes() map[string]bool {
	return map[string]bool{ScopeSymbols: true, ScopeAll: true, ScopeWatchlist: true, ScopePositions: true, ScopeInspection: true}
}

func allowedFields(ruleType string) map[string]bool {
	switch ruleType {
	case RuleTypePrice:
		return map[string]bool{"last": true, "change_pct": true, "cost_distance_pct": true}
	case RuleTypeVolume:
		return map[string]bool{"volume_ratio": true, "turnover_rate": true, "amount": true}
	case RuleTypeAbnormal:
		return map[string]bool{"limit_up": true, "broken_limit": true, "limit_down": true, "high_60d": true, "low_60d": true}
	case RuleTypeDeviation:
		return map[string]bool{"dev_3d": true, "dev_10d": true, "dev_30d": true}
	case RuleTypeMarket:
		return map[string]bool{"index_change_pct": true, "up_count": true, "down_count": true, "limit_up_count": true}
	case RuleTypeDate:
		return map[string]bool{"days_to_remind": true}
	case RuleTypeSignal:
		fields := map[string]bool{}
		for _, id := range signalIDs() {
			fields[id] = true
		}
		return fields
	case RuleTypeSector:
		return map[string]bool{
			"sector_change": true, "sector_flow": true, "sector_flow_delta": true,
			"sector_speed": true, "sector_breadth": true,
			"sector_limit_up": true, "sector_limit_down": true,
		}
	default:
		return nil
	}
}

// NormalizeRule 校验并补全一条规则。所有字段都以"调用方给的 JSON"为准，
// 缺省值在这里统一补齐，这样 store 与 engine 不需要重复防御。
func NormalizeRule(rule Rule) (Rule, error) {
	if !validRuleTypes()[rule.Type] {
		return rule, fmt.Errorf("不支持的规则类型 %q", rule.Type)
	}
	if !validScopes()[rule.Scope] {
		return rule, fmt.Errorf("不支持的作用域 %q", rule.Scope)
	}
	// 技术信号支持全市场作用域：语义是"全市场里谁今天出现了这个信号"。
	// 成本由一个交易日一份的日线缓存兜底（同一天只打一次上游），
	// 扫描规模由 Filters（板块/ST/成交额/上限）收窄，见 engine.signalRuleSymbols。
	if rule.Type == RuleTypeSector {
		// 板块规则没有持仓/全市场语义：symbols 就是板块代码清单。
		rule.Scope = ScopeSymbols
		rule.Symbols = uniqueTrim(rule.Symbols)
		if len(rule.Symbols) == 0 {
			return rule, fmt.Errorf("板块规则至少需要选择一个板块")
		}
		if len(rule.Symbols) > sectorMaxPerRule {
			return rule, fmt.Errorf("板块规则最多选择 %d 个板块", sectorMaxPerRule)
		}
		for index, code := range rule.Symbols {
			upper := strings.ToUpper(code)
			// 两个代码空间：BK*=东财行业/概念榜单，TH*=系统题材库题材。
			if !strings.HasPrefix(upper, "BK") && !isThemeCode(upper) {
				return rule, fmt.Errorf("板块代码 %q 无效，应为 BK 开头的板块代码或 TH 开头的题材编号", code)
			}
			rule.Symbols[index] = upper
		}
	}
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Severity = strings.TrimSpace(rule.Severity)
	switch rule.Severity {
	case SeverityInfo, SeverityWarning, SeverityCritical:
	default:
		rule.Severity = SeverityWarning
	}
	rule.Logic = strings.ToLower(strings.TrimSpace(rule.Logic))
	rule.Logic = map[string]string{"or": "or"}[rule.Logic]
	if rule.Logic == "" {
		rule.Logic = "and"
	}
	if rule.CooldownSeconds == 0 {
		rule.CooldownSeconds = 300
	}
	if rule.CooldownSeconds < 30 {
		rule.CooldownSeconds = 30
	}
	if rule.CooldownSeconds > 86400 {
		rule.CooldownSeconds = 86400
	}
	rule.Source = map[string]string{SourceLot: SourceLot, SourcePosition: SourcePosition}[rule.Source]
	if rule.Source == "" {
		rule.Source = SourceManual
	}
	rule.Symbols = uniqueTrim(rule.Symbols)
	switch {
	case rule.Scope == ScopeSymbols && len(rule.Symbols) == 0:
		return rule, fmt.Errorf("指定标的的范围至少需要一个代码")
	case rule.Scope != ScopeSymbols:
		rule.Symbols = nil
	}
	boards := make([]string, 0, len(rule.Filters.Boards))
	for _, board := range rule.Filters.Boards {
		switch strings.TrimSpace(board) {
		case foundation.BoardMain, foundation.BoardChiNext, foundation.BoardSTAR, foundation.BoardBSE:
			boards = append(boards, board)
		}
	}
	rule.Filters.Boards = boards
	if typeMarket := rule.Type == RuleTypeMarket; typeMarket {
		// 大盘规则没有标的维度，scope 恒为 all。
		rule.Scope = ScopeAll
		rule.Symbols = nil
	}
	if rule.Type == RuleTypeDate {
		if rule.RemindDate != "" {
			if _, err := time.ParseInLocation("2006-01-02", rule.RemindDate, time.Local); err != nil {
				return rule, fmt.Errorf("提醒日期无效")
			}
		}
		if rule.LeadDays < 0 || rule.LeadDays > 90 {
			return rule, fmt.Errorf("提前天数需要在 0～90 之间")
		}
	}
	if rule.Type == RuleTypePrice && rule.Source == SourceLot {
		// 批次托管的止盈止损必须 critical：它直接触发卖出决策。
		rule.Severity = SeverityCritical
	}
	cleaned, err := normalizeConditions(rule.Type, rule.Conditions, rule.Logic)
	if err != nil {
		return rule, err
	}
	rule.Conditions = cleaned
	rule.NotifyChannels = uniqueTrim(rule.NotifyChannels)
	return rule, nil
}

func normalizeConditions(ruleType string, conditions []Condition, logic string) ([]Condition, error) {
	fields := allowedFields(ruleType)
	if len(conditions) == 0 {
		return nil, fmt.Errorf("至少需要一条条件")
	}
	cleaned := make([]Condition, 0, len(conditions))
	for _, condition := range conditions {
		condition.Field = strings.TrimSpace(condition.Field)
		condition.Op = strings.TrimSpace(condition.Op)
		if !fields[condition.Field] {
			return nil, fmt.Errorf("字段 %q 不适用于该规则类型", condition.Field)
		}
		if isBooleanField(ruleType, condition.Field) {
			// 布尔字段忽略算子，把任何非零值归一为 1，避免下游逐个特判。
			condition.Op = OpGTE
			if condition.Value != 0 {
				condition.Value = booleanFieldValue
			}
		} else {
			switch condition.Op {
			case OpGTE, OpLTE, OpBetween, OpCrossUp, OpCrossDown, OpEqual:
			default:
				return nil, fmt.Errorf("不支持的比较算子 %q", condition.Op)
			}
			if condition.Op == OpBetween {
				if condition.Value2 == nil {
					return nil, fmt.Errorf("between 条件需要上下界")
				}
			}
		}
		cleaned = append(cleaned, condition)
	}
	// or 语义下规则通常只有条件独立成立即触发；and 语义下缺失字段
	// 会让整条规则沉默，这属于规则作者的语义选择，不再二次归一。
	return cleaned, nil
}

func isBooleanField(ruleType, field string) bool {
	return ruleType == RuleTypeAbnormal || ruleType == RuleTypeSignal
}

// EvaluateRule 把规则与一份观测对照，返回全部成立的信号。
// 这是纯函数：不写状态、不发通知、不在意时间。
func EvaluateRule(rule Rule, obs Observation) []Signal {
	signals := make([]Signal, 0, len(rule.Conditions))
	satisfied := 0
	for conditionIndex, condition := range rule.Conditions {
		value, ok := observationValue(obs, condition.Field)
		if !ok {
			if rule.Logic == "and" {
				// AND 语义要求每个字段都有值；缺值宁可不触发也不误报。
				return nil
			}
			continue
		}
		if !conditionSatisfied(condition, value) {
			continue
		}
		satisfied++
		signals = append(signals, Signal{
			ConditionIndex: conditionIndex,
			Kind:           signalKind(rule.Type, condition.Field),
			Field:          condition.Field,
			Value:          value,
			Threshold:      condition.Value,
			Message:        conditionMessage(rule.Type, condition, value),
		})
	}
	if satisfied == 0 {
		return nil
	}
	if rule.Logic == "or" {
		return signals
	}
	if satisfied < len(rule.Conditions) {
		return nil
	}
	return signals
}

func signalKind(ruleType, field string) string {
	switch ruleType {
	case RuleTypeDate:
		return "lot_due"
	case RuleTypePrice:
		return "price_" + field
	case RuleTypeSignal:
		return "signal_" + field
	default:
		return ruleType + "_" + field
	}
}

func conditionSatisfied(condition Condition, value float64) bool {
	if isAbnormalField(condition.Field) {
		// 布尔字段：条件 value=1 表示要求成立，0 表示要求不成立。
		boolean := value != 0
		return (condition.Value != 0) == boolean
	}
	switch condition.Op {
	case OpEqual:
		return value == condition.Value
	case OpGTE, OpCrossUp:
		return value >= condition.Value
	case OpLTE, OpCrossDown:
		return value <= condition.Value
	case OpBetween:
		low, high := condition.Value, orZero(condition.Value2)
		if high < low {
			low, high = high, low
		}
		return value >= low && value <= high
	default:
		// cross 算子由 engine 的状态机处理，这里只算"当前值已过线"，
		// 保证试算接口在没有历史值时也能给出方向一致的答案。
		return value >= condition.Value
	}
}

func isAbnormalField(field string) bool {
	switch field {
	case "limit_up", "broken_limit", "limit_down", "high_60d", "low_60d":
		return true
	}
	return false
}

func orZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func observationValue(obs Observation, field string) (float64, bool) {
	switch field {
	case "last":
		return obs.Price, obs.Price > 0
	case "change_pct":
		return obs.ChangePercent, true
	case "cost_distance_pct":
		if !obs.HaveCost || obs.CostPrice <= 0 || obs.Price <= 0 {
			return 0, false
		}
		return (obs.Price - obs.CostPrice) / obs.CostPrice * 100, true
	case "volume_ratio":
		return obs.VolumeRatio, obs.VolumeRatio > 0
	case "turnover_rate":
		return obs.TurnoverRate, obs.VolumeRatio > 0 || obs.TurnoverRate > 0
	case "amount":
		return obs.Amount, obs.Amount > 0
	case "limit_up":
		return boolValue(limitState(obs.SnapshotRow) == 1), true
	case "broken_limit":
		return boolValue(limitState(obs.SnapshotRow) == 2), true
	case "limit_down":
		return boolValue(IsLimitDown(obs.SnapshotRow)), true
	case "high_60d":
		return boolValue(obs.NewHigh60), obs.HaveHighLow
	case "low_60d":
		return boolValue(obs.NewLow60), obs.HaveHighLow
	case "dev_3d":
		return obs.Deviation3, obs.HaveDeviation || obs.DeviationAvailable[0]
	case "dev_10d":
		return obs.Deviation10, obs.HaveDeviation || obs.DeviationAvailable[1]
	case "dev_30d":
		return obs.Deviation30, obs.HaveDeviation || obs.DeviationAvailable[2]
	case "index_change_pct":
		return obs.Market.IndexChangePct, obs.Market.HaveIndexes
	case "up_count":
		return float64(obs.Market.UpCount), obs.Market.HaveBreadth
	case "down_count":
		return float64(obs.Market.DownCount), obs.Market.HaveBreadth
	case "limit_up_count":
		return float64(obs.Market.LimitUpCount), obs.Market.HaveBreadth
	case "days_to_remind":
		return float64(obs.DaysToRemind), obs.HaveRemind
	case "sector_change":
		return obs.Sector.ChangePercent, obs.Sector.Have
	case "sector_flow":
		// 榜单给的是元，规则与展示统一用亿，避免用户输 500000000。
		return obs.Sector.MainNetInflow / 1e8, obs.Sector.HaveFlow
	case "sector_flow_delta":
		return obs.Sector.FlowDelta / 1e8, obs.Sector.HaveFlowDelta
	case "sector_speed":
		return obs.Sector.Speed, obs.Sector.HaveSpeed
	case "sector_breadth":
		// 0~100 的百分数，与原项目「普涨 ≥80%」同一读法。
		return obs.Sector.Breadth, obs.Sector.HaveBreadth
	case "sector_limit_up":
		return float64(obs.Sector.LimitUp), obs.Sector.HaveBreadth
	case "sector_limit_down":
		return float64(obs.Sector.LimitDown), obs.Sector.HaveBreadth
	default:
		if isSignalField(field) {
			return boolValue(obs.Signals[field]), obs.HaveSignals
		}
		return 0, false
	}
}

func boolValue(v bool) float64 {
	if v {
		return booleanFieldValue
	}
	return 0
}

// conditionMessage 把一条成立的信号翻译成中文一句话。
func conditionMessage(ruleType string, condition Condition, value float64) string {
	if isAbnormalField(condition.Field) {
		return abnormalMessage(condition.Field, value != 0)
	}
	if ruleType == RuleTypeSignal && isSignalField(condition.Field) {
		// 技术信号只在成立时产出消息：一句话说清是哪个信号。
		return signalMessage(condition.Field)
	}
	name := condition.Label()
	if name == "" {
		name = condition.Field
	}
	switch condition.Op {
	case OpBetween:
		return fmt.Sprintf("%s %.2f 处于 %.2f~%.2f", name, value, condition.Value, orZero(condition.Value2))
	case OpEqual, OpGTE, OpLTE, OpCrossUp, OpCrossDown:
		return fmt.Sprintf("%s %.2f %s %.2f", name, value, opWord(condition.Op), condition.Value)
	}
	return fmt.Sprintf("%s %.2f", name, value)
}

func opWord(op string) string {
	switch op {
	case OpGTE, OpEqual:
		return "≥"
	case OpLTE:
		return "≤"
	case OpCrossUp:
		return "上穿"
	case OpCrossDown:
		return "下穿"
	}
	return "满足"
}

func abnormalMessage(field string, on bool) string {
	labels := map[string]string{
		"limit_up": "涨停", "broken_limit": "炸板", "limit_down": "跌停",
		"high_60d": "60日新高", "low_60d": "60日新低",
	}
	label, ok := labels[field]
	if !ok {
		label = field
	}
	if !on {
		return label + " 状态解除"
	}
	return label
}

// uniqueTrim 去重与空白，空串不算一个元素。
func uniqueTrim(values []string) []string {
	seen := map[string]bool{}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	return cleaned
}

// fieldLabels 把内部字段名翻成中文。它同时给消息与前端展示用，
// 改这里就能同时改两处文案。
var fieldLabels = map[string]string{
	"last":              "现价",
	"change_pct":        "涨跌幅%",
	"cost_distance_pct": "距成本%",
	"volume_ratio":      "量比",
	"turnover_rate":     "换手率%",
	"amount":            "成交额",
	"dev_3d":            "3日偏离%",
	"dev_10d":           "10日偏离%",
	"dev_30d":           "30日偏离%",
	"index_change_pct":  "指数涨跌幅%",
	"up_count":          "上涨家数",
	"down_count":        "下跌家数",
	"limit_up_count":    "涨停家数",
	"days_to_remind":    "距提醒日",
	"sector_change":     "板块涨跌幅%",
	"sector_flow":       "板块主力净额(亿)",
	"sector_flow_delta": "30分钟净额变化(亿)",
	"sector_speed":      "5分钟涨跌变化%",
	"sector_breadth":    "成分上涨占比%",
	"sector_limit_up":   "板块涨停家数",
	"sector_limit_down": "板块跌停家数",
}

// Label 返回字段中文名，未知字段返回空串（调用方回落到原始字段名）。
func (c Condition) Label() string {
	return fieldLabels[c.Field]
}

// joinSymbols 是列表展示用的 scope 摘要；超过 4 个代码折叠成 "xxx 等 n 只"。
func joinSymbols(symbols []string) string {
	if len(symbols) == 0 {
		return ""
	}
	if len(symbols) <= 4 {
		return strings.Join(symbols, "、")
	}
	return strings.Join(symbols[:4], "、") + " 等 " + strconv.Itoa(len(symbols)) + " 只"
}
