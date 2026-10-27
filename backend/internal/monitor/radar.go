package monitor

// 智能雷达：监控中心的「反向智能」。
//
// 规则模式的因果关系是"用户先写条件，系统才盯"；雷达反过来——
// 系统主动扫描持仓与关注列表，把技术信号、均线形态、量能、
// 60 日区间位置拼成一份带理由的分级结论（机会/风险/观察），
// 用户从"配置员"变成"看结论的人"。
//
// 雷达只读不写：不发通知、不落事件、更不碰订单。它消费的每
// 一项事实都与规则评估同一来源（dailyBars 缓存 + 快照行），
// 不会为了扫描多拉一条数据链路。

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// InspectionConclusion 是持仓AI巡检给出的单标的分析结论快照。
// 巡检是异步 AI 任务，雷达只读最近一次成功报告里的结论文本，
// 不解析情绪、不虚构方向——文本本身就是巡检模块的事实输出。
type InspectionConclusion struct {
	Symbol         string
	Conclusion     string // 组合中的持有判断
	Action         string // 建议动作（观察类动作的说明文本）
	ActionPriority string // 观察 / 保持 / 优先处理
	EvaluatedAt    string // 报告生成日 YYYY-MM-DD
}

// InspectionProvider 由 httpapi 装上：它知道巡检服务在哪。
type InspectionProvider interface {
	LatestInspectionConclusions(ctx context.Context) ([]InspectionConclusion, error)
}

// InspectionHoldingsProvider 给出巡检组合里用户手动维护的持仓清单。
// 与结论接口分开：规则作用域只需要「哪些票」，不需要结论。
type InspectionHoldingsProvider interface {
	LatestInspectionHoldings(ctx context.Context) ([]ScopeTarget, error)
}

// inspectionEscalate 是巡检结论里"必须让人看见"的优先级。
// 只有它会把手头没有技术信号的标的抬进雷达（观察级），
// 其余结论只给已有发现做补充理由，避免持仓全量刷屏。
const inspectionEscalate = "优先处理"

// 信号对雷达评分的权重：正分看多、负分看空。交叉与 MACD 是事件级
// 事实，权重高于形态与位置；量能只提供佐证。
var radarSignalWeights = map[string]float64{
	SignalMACDGolden:     3,
	SignalMACDDead:       -3,
	SignalMA5UpMA20:      2.5,
	SignalMA5DownMA20:    -2.5,
	SignalMA20UpMA60:     2,
	SignalMA20DownMA60:   -2,
	SignalBollUpperBreak: 1,
	SignalBollLowerBreak: -1.5,
	SignalVolDouble:      1,
	SignalVolShrink:      -0.5,
}

// radarScoreMin 是升到「机会」或跌到「风险」的净分门槛。
// 单一弱信号（比如只有缩量 -0.5）不够格，只进「观察」。
const radarScoreMin = 2

// RadarFinding 是一只标的的雷达结论。
type RadarFinding struct {
	Symbol        string   `json:"symbol"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"` // opportunity / risk / watch
	Score         float64  `json:"score"`
	Tags          []string `json:"tags"`
	Reasons       []string `json:"reasons"`
	Price         float64  `json:"price"`
	ChangePercent float64  `json:"change_percent"`
	MAState       string   `json:"ma_state"`
	VolRatio5     float64  `json:"vol_ratio_5"`
	Position60    float64  `json:"position_60d"`
	HavePos60     bool     `json:"have_position_60d"`
	Momentum5     float64  `json:"momentum_5d"`
	IsPosition    bool     `json:"is_position"`
	// Source 是这只票进入雷达池的来源：持仓 / 关注仓 / 规则标的。
	// 持仓票往往既在持仓又在关注仓，按 持仓 > 关注仓 > 规则标的 取一个。
	Source  string   `json:"source,omitempty"`
	Signals []string `json:"signals"`
	// Closes 是近 30 日收盘序列（已四舍五入到分）：给前端画行内迷你走势图。
	// 缺数据的标的留空，前端对应列显示 —。
	Closes        []float64      `json:"closes,omitempty"`
	SuggestedRule *RadarRuleSeed `json:"suggested_rule,omitempty"`
}

// RadarRuleSeed 是一键转盯盘规则的预填内容：结构就是规则本体，
// 前端拿到后直接走既有创建接口，不再发明第二种格式。
type RadarRuleSeed struct {
	Name            string      `json:"name"`
	Type            string      `json:"type"`
	Severity        string      `json:"severity"`
	Scope           string      `json:"scope"`
	Symbols         []string    `json:"symbols"`
	Conditions      []Condition `json:"conditions"`
	Logic           string      `json:"logic"`
	CooldownSeconds int         `json:"cooldown_seconds"`
}

// RadarMover 是扫描池里当日涨跌靠前的标的：雷达整体风平浪静时，
// 前端拿它回答"那今天谁动了"，空态不再是白板。
type RadarMover struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	ChangePercent float64 `json:"change_percent"`
	VolumeRatio   float64 `json:"volume_ratio"`
}

// RadarMarket 是扫描时点的大盘环境：给结论提供"今天是普涨还是普跌"的语境。
// 复用大盘规则同一份 marketStats（宽度统计 + 指数收盘），不新增数据链路。
type RadarIndex struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	ChangePct float64 `json:"change_pct"`
}

type RadarMarket struct {
	HaveIndexes    bool         `json:"have_indexes"`
	IndexChangePct float64      `json:"index_change_pct"`
	Indexes        []RadarIndex `json:"indexes,omitempty"`
	HaveBreadth    bool         `json:"have_breadth"`
	UpCount        int          `json:"up_count"`
	DownCount      int          `json:"down_count"`
	LimitUpCount   int          `json:"limit_up_count"`
	TotalAmount    float64      `json:"total_amount"` // 两市成交额（亿元），快照汇总
}

// RadarReport 是一次扫描的完整输出。
type RadarReport struct {
	Scanned     int            `json:"scanned"`
	Skipped     int            `json:"skipped"`
	TradeDate   string         `json:"trade_date"`
	GeneratedAt string         `json:"generated_at"`
	Summary     string         `json:"summary"`
	Counts      map[string]int `json:"counts"`
	Findings    []RadarFinding `json:"findings"`
	Market      *RadarMarket   `json:"market,omitempty"`
	Movers      []RadarMover   `json:"movers,omitempty"`
	Error       string         `json:"error,omitempty"`
	// Counts 保留返回列表的统计；完整扫描统计单独提供，不受展示上限影响。
	TotalCounts   map[string]int     `json:"total_counts"`
	TotalFindings int                `json:"total_findings"`
	SourceCounts  map[string]int     `json:"source_counts"`
	Targets       []RadarTargetState `json:"targets"`
}

// RadarTargetState 为每个入池标的保留结果，包含未进入前 40 条和数据不足的标的。
type RadarTargetState struct {
	Symbol    string `json:"symbol"`
	Name      string `json:"name,omitempty"`
	Source    string `json:"source"`
	Status    string `json:"status"`
	Kind      string `json:"kind,omitempty"`
	Reason    string `json:"reason,omitempty"`
	DailyAsOf string `json:"daily_as_of,omitempty"`
}

// radarFindingFromBars 是雷达的纯计算核：给定标的代码、日线与可选快照行、
// 持仓AI巡检结论，产出一条结论。代码必须由调用方传入——快照行不可用时
// 结论里也得有代码，否则「一键转规则」会拿到空标的。
func radarFindingFromBars(symbol string, bars []foundation.KLine, row *foundation.MarketSnapshotRow, isPosition bool, inspection *InspectionConclusion) (RadarFinding, bool) {
	closes, volumes := closedSeries(bars)
	if len(closes) < 6 {
		return RadarFinding{}, false
	}
	finding := RadarFinding{Symbol: symbol, IsPosition: isPosition, Tags: []string{}, Reasons: []string{}, Signals: []string{}}
	finding.Price = closes[len(closes)-1]
	if len(closes) >= 2 && closes[len(closes)-2] > 0 {
		finding.ChangePercent = round2((closes[len(closes)-1]/closes[len(closes)-2] - 1) * 100)
	}
	if row != nil {
		finding.Name = row.Name
		if row.Symbol != "" {
			finding.Symbol = row.Symbol
		}
		if row.Price > 0 {
			finding.Price = row.Price
			finding.ChangePercent = row.ChangePercent
		}
	}
	finding.Price = round2(finding.Price)

	// 技术信号：事件级事实，逐条计分并写理由。
	signals := computeSignals(bars)
	score := 0.0
	if len(signals) > 0 {
		ids := signalIDs()
		sort.SliceStable(ids, func(i, j int) bool {
			return math.Abs(radarSignalWeights[ids[i]]) > math.Abs(radarSignalWeights[ids[j]])
		})
		for _, id := range ids {
			if !signals[id] {
				continue
			}
			finding.Signals = append(finding.Signals, id)
			finding.Tags = append(finding.Tags, signalLabel(id))
			if text, ok := signalDescriptions[id]; ok {
				finding.Reasons = append(finding.Reasons, signalLabel(id)+"："+text)
			} else {
				finding.Reasons = append(finding.Reasons, signalLabel(id))
			}
			score += radarSignalWeights[id]
		}
	}

	// 均线形态：5/20/60 三线的相对位置。
	ma5, ma20, ma60 := sma(closes, 5, 0), sma(closes, 20, 0), sma(closes, 60, 0)
	switch {
	case ma5 > 0 && ma20 > 0 && ma60 > 0 && ma5 > ma20 && ma20 > ma60:
		finding.MAState = "多头排列"
		finding.Tags = append(finding.Tags, "多头排列")
		finding.Reasons = append(finding.Reasons, "均线多头排列（MA5>MA20>MA60），短线强于中线")
		score += 1.5
	case ma5 > 0 && ma20 > 0 && ma60 > 0 && ma5 < ma20 && ma20 < ma60:
		finding.MAState = "空头排列"
		finding.Tags = append(finding.Tags, "空头排列")
		finding.Reasons = append(finding.Reasons, "均线空头排列（MA5<MA20<MA60），趋势偏弱")
		score -= 1.5
	case ma5 > 0 && ma20 > 0:
		finding.MAState = "纠缠"
	}

	// 量能：今日成交对 5 日均量的倍数。
	if len(volumes) >= 6 {
		avg5 := (volumes[len(volumes)-6] + volumes[len(volumes)-5] + volumes[len(volumes)-4] +
			volumes[len(volumes)-3] + volumes[len(volumes)-2]) / 5
		if avg5 > 0 {
			finding.VolRatio5 = round2(volumes[len(volumes)-1] / avg5)
			if finding.VolRatio5 >= 2 {
				finding.Tags = append(finding.Tags, fmt.Sprintf("放量 %.1f 倍", finding.VolRatio5))
				finding.Reasons = append(finding.Reasons, fmt.Sprintf("成交量为 5 日均量的 %.1f 倍，资金明显异动", finding.VolRatio5))
			} else if finding.VolRatio5 <= 0.5 {
				finding.Tags = append(finding.Tags, fmt.Sprintf("缩量 %.1f 倍", finding.VolRatio5))
				finding.Reasons = append(finding.Reasons, fmt.Sprintf("成交量只有 5 日均量的 %.1f 倍，交投转淡", finding.VolRatio5))
			}
		}
	}

	// 60 日区间位置：现价落在近 60 个收盘日的相对高度（0~100）。
	if len(closes) >= 20 {
		window := closes
		if len(window) > 60 {
			window = window[len(window)-60:]
		}
		maxClose, minClose := window[0], window[0]
		for _, close := range window[1:] {
			if close > maxClose {
				maxClose = close
			}
			if close < minClose {
				minClose = close
			}
		}
		if maxClose > minClose {
			finding.HavePos60 = true
			finding.Position60 = round2((closes[len(closes)-1] - minClose) / (maxClose - minClose) * 100)
			switch {
			case finding.Position60 >= 90:
				finding.Tags = append(finding.Tags, "60日高位")
				finding.Reasons = append(finding.Reasons, fmt.Sprintf("现价处于 60 日区间 %.0f%% 高位，接近阶段高点", finding.Position60))
			case finding.Position60 <= 10:
				finding.Tags = append(finding.Tags, "60日低位")
				finding.Reasons = append(finding.Reasons, fmt.Sprintf("现价处于 60 日区间 %.0f%% 低位，接近阶段低点", finding.Position60))
			}
		}
	}

	// 近 5 日动量。
	if len(closes) >= 6 && closes[len(closes)-6] > 0 {
		finding.Momentum5 = round2((closes[len(closes)-1]/closes[len(closes)-6] - 1) * 100)
		if finding.Momentum5 >= 10 {
			finding.Tags = append(finding.Tags, fmt.Sprintf("5日+%.0f%%", finding.Momentum5))
			finding.Reasons = append(finding.Reasons, fmt.Sprintf("近 5 日累计上涨 %.1f%%，短线过热需要回踩确认", finding.Momentum5))
		} else if finding.Momentum5 <= -10 {
			finding.Tags = append(finding.Tags, fmt.Sprintf("5日%.0f%%", finding.Momentum5))
			finding.Reasons = append(finding.Reasons, fmt.Sprintf("近 5 日累计下跌 %.1f%%，跌势偏急", finding.Momentum5))
		}
	}

	// 近 30 日收盘序列：行内迷你走势图的数据源，四舍五入到分位减小 payload。
	spark := closes
	if len(spark) > 30 {
		spark = spark[len(spark)-30:]
	}
	finding.Closes = make([]float64, len(spark))
	for index, value := range spark {
		finding.Closes[index] = round2(value)
	}

	finding.Score = round2(score)
	// 巡检结论是事实层补充：技术面有事才进结论的标的顺带展示巡检
	// 判断；巡检标记"优先处理"的持仓即使技术面安静也要进观察。
	hasTechnical := len(finding.Signals) > 0 || len(finding.Reasons) > 0
	if inspection != nil && strings.TrimSpace(inspection.Conclusion+inspection.Action) != "" {
		if strings.TrimSpace(inspection.Conclusion) != "" {
			finding.Reasons = append(finding.Reasons, fmt.Sprintf("持仓AI巡检（%s）：%s", inspection.EvaluatedAt, inspection.Conclusion))
		}
		if strings.TrimSpace(inspection.Action) != "" {
			finding.Reasons = append(finding.Reasons, fmt.Sprintf("巡检建议：%s", inspection.Action))
		}
		finding.Tags = append(finding.Tags, "AI巡检")
		if inspection.ActionPriority == inspectionEscalate {
			finding.Tags = append(finding.Tags, "巡检优先处理")
			hasTechnical = true
		}
	}
	switch {
	case score >= radarScoreMin:
		finding.Kind = "opportunity"
	case score <= -radarScoreMin:
		finding.Kind = "risk"
	case hasTechnical:
		finding.Kind = "watch"
	default:
		return RadarFinding{}, false
	}

	finding.SuggestedRule = radarSuggestedRule(finding)
	return finding, true
}

// radarSuggestedRule 给每条结论都准备一份可一键创建的规则预填。
// 优先用权重最高的已触发信号；纯形态/位置类结论（「观察」里最常见）没有事件级
// 信号，就用当时下判断的那条技术事实（60 日位置、量比、涨跌幅）建等价规则——
// 否则有的行有「转为规则」、有的行没有，用户看到的就是一个残缺的操作列。
func radarSuggestedRule(finding RadarFinding) *RadarRuleSeed {
	severity := SeverityInfo
	if finding.Kind == "risk" {
		severity = SeverityWarning
	}
	seed := &RadarRuleSeed{
		Severity:        severity,
		Scope:           ScopeSymbols,
		Symbols:         []string{finding.Symbol},
		Logic:           "and",
		CooldownSeconds: 3600,
	}
	if len(finding.Signals) > 0 {
		top := finding.Signals[0]
		seed.Name = fmt.Sprintf("雷达盯盘·%s %s", finding.displayName(), signalLabel(top))
		seed.Type = RuleTypeSignal
		seed.Conditions = []Condition{{Field: top, Op: OpEqual, Value: booleanFieldValue}}
		return seed
	}
	switch {
	case finding.HavePos60 && finding.Position60 >= 80:
		seed.Name = fmt.Sprintf("雷达盯盘·%s 接近60日高点", finding.displayName())
		seed.Type = RuleTypeAbnormal
		seed.Conditions = []Condition{{Field: "high_60d", Op: OpEqual, Value: booleanFieldValue}}
	case finding.HavePos60 && finding.Position60 <= 20:
		seed.Name = fmt.Sprintf("雷达盯盘·%s 接近60日低点", finding.displayName())
		seed.Type = RuleTypeAbnormal
		seed.Conditions = []Condition{{Field: "low_60d", Op: OpEqual, Value: booleanFieldValue}}
	case finding.VolRatio5 >= 1.5:
		seed.Name = fmt.Sprintf("雷达盯盘·%s 放量", finding.displayName())
		seed.Type = RuleTypeVolume
		seed.Conditions = []Condition{{Field: "volume_ratio", Op: OpGTE, Value: 1.5}}
	case finding.Kind == "risk":
		seed.Name = fmt.Sprintf("雷达盯盘·%s 跌幅达3%%", finding.displayName())
		seed.Type = RuleTypePrice
		seed.Conditions = []Condition{{Field: "change_pct", Op: OpLTE, Value: -3.0}}
	default:
		seed.Name = fmt.Sprintf("雷达盯盘·%s 涨幅达3%%", finding.displayName())
		seed.Type = RuleTypePrice
		seed.Conditions = []Condition{{Field: "change_pct", Op: OpGTE, Value: 3.0}}
	}
	return seed
}

// mapValues 把快照索引摊平成切片：marketStats 只认按行遍历的输入。
func mapValues(rows map[string]foundation.MarketSnapshotRow) []foundation.MarketSnapshotRow {
	values := make([]foundation.MarketSnapshotRow, 0, len(rows))
	for _, row := range rows {
		values = append(values, row)
	}
	return values
}

func (f RadarFinding) displayName() string {
	if name := f.Name; name != "" {
		return name
	}
	return f.Symbol
}

// sixDigitCode 取标的代码里的 6 位数字主体，用于对齐不同来源的
// 代码口径（"002100.SZ" 与 "002100" 应指向同一只股票）。
func sixDigitCode(symbol string) string {
	digits := make([]byte, 0, 6)
	for index := 0; index < len(symbol); index++ {
		if symbol[index] >= '0' && symbol[index] <= '9' {
			digits = append(digits, symbol[index])
		}
	}
	if len(digits) < 6 {
		return ""
	}
	return string(digits[len(digits)-6:])
}

// radarCacheTTL：雷达按需扫描，60 秒内的重复点击复用同一份结论。
// 扫描一只标的一根日线请求，几百只的持仓+自选不能每次点都全量重拉。
const radarCacheTTL = 60 * time.Second

// 雷达结论的来源标签：前端的「来自」列直接显示这几个词。
const (
	radarSourcePosition = "持仓"
	radarSourceWatch    = "关注仓"
	radarSourceRule     = "规则标的"
)

// radarMaxFindings 是返回给前端的结论条数上限：排序后只留最值得看的。
const radarMaxFindings = 40

// RadarScan 执行一次雷达扫描：持仓 + 关注列表 + 规则指定标的。
func (e *Engine) invalidateRadar() {
	e.mu.Lock()
	e.radarGeneration++
	e.radarFetchedAt = time.Time{}
	e.mu.Unlock()
}

func (e *Engine) RadarScan(ctx context.Context, force ...bool) RadarReport {
	e.radarScanMu.Lock()
	defer e.radarScanMu.Unlock()
	now := e.now()
	e.mu.Lock()
	if !(len(force) > 0 && force[0]) && e.radarFetchedAt.After(now.Add(-radarCacheTTL)) && e.radarReport != nil {
		report := *e.radarReport
		e.mu.Unlock()
		return report
	}
	generation := e.radarGeneration
	e.mu.Unlock()
	report := e.radarScanUncached(ctx, now)
	e.mu.Lock()
	if generation == e.radarGeneration {
		e.radarReport = &report
		e.radarFetchedAt = now
	}
	e.mu.Unlock()
	return report
}

func (e *Engine) radarScanUncached(ctx context.Context, now time.Time) RadarReport {
	report := RadarReport{
		TradeDate:    now.In(foundation.AStockLocation).Format("2006-01-02"),
		GeneratedAt:  now.Format(time.RFC3339),
		Counts:       map[string]int{},
		Findings:     []RadarFinding{},
		TotalCounts:  map[string]int{},
		SourceCounts: map[string]int{},
		Targets:      []RadarTargetState{},
	}
	// 扫描范围是用户关心的标的本身：巡检持仓 + 外部持仓源 + 关注仓 + 规则指定。
	// 巡检持仓排在最前——那是用户自己在巡检页维护的组合，往往才是"我的持仓"。
	// 不含全市场涨幅榜——那是异动页的职责，雷达对准"我自己的池子"。
	symbols := map[string]bool{}
	positions := map[string]bool{}
	names := map[string]string{}
	// sources 记「这票是从哪个池子进来的」：持仓 > 关注仓 > 规则标的。
	sources := map[string]string{}
	if targets := e.inspectionTargets(ctx); len(targets) > 0 {
		for _, target := range targets {
			if target.Symbol == "" {
				continue
			}
			symbols[target.Symbol] = true
			positions[target.Symbol] = true
			if strings.TrimSpace(target.Name) != "" {
				names[target.Symbol] = target.Name
			}
		}
	}
	if e.portfolio != nil {
		if holdings, err := e.portfolio.Holdings(ctx); err == nil {
			for _, holding := range holdings {
				if holding.Quantity > 0 {
					symbols[holding.Symbol] = true
					positions[holding.Symbol] = true
					if strings.TrimSpace(holding.Name) != "" {
						names[holding.Symbol] = holding.Name
					}
				}
			}
		}
	}
	if e.store != nil {
		if watch, err := e.store.ListWatchlist(ctx); err == nil {
			for _, entry := range watch {
				symbols[entry.Symbol] = true
				if _, ok := sources[entry.Symbol]; !ok {
					sources[entry.Symbol] = radarSourceWatch
				}
				if strings.TrimSpace(entry.Name) != "" {
					names[entry.Symbol] = entry.Name
				}
			}
		}
	}
	// 持仓优先级最高：先写的持仓来源不被关注仓覆盖，这里统一补一层。
	for symbol := range positions {
		sources[symbol] = radarSourcePosition
	}
	for _, symbol := range e.enabledSymbols(ctx) {
		symbols[symbol] = true
		if _, ok := sources[symbol]; !ok {
			sources[symbol] = radarSourceRule
		}
	}
	// 快照与持仓两边的代码口径不一（002100.SZ / 002100），
	// 来源表按六位代码再铺一份，查的时候两边都试。
	for symbol, value := range sources {
		if code := sixDigitCode(symbol); code != "" && code != symbol {
			if _, ok := sources[code]; !ok {
				sources[code] = value
			}
		}
	}
	// 不同来源的纯代码与交易所后缀表示同一标的，只扫描一次。
	canonical := map[string]string{}
	sourcePriority := map[string]int{radarSourcePosition: 3, radarSourceWatch: 2, radarSourceRule: 1}
	for _, symbol := range keys(symbols) {
		key := sixDigitCode(symbol)
		if key == "" {
			key = symbol
		}
		previous, exists := canonical[key]
		if !exists || sourcePriority[sources[symbol]] > sourcePriority[sources[previous]] {
			canonical[key] = symbol
		}
	}
	targets := make([]string, 0, len(canonical))
	for _, symbol := range canonical {
		targets = append(targets, symbol)
		report.SourceCounts[sources[symbol]]++
	}
	sort.Strings(targets)
	report.Scanned = len(targets)
	if len(targets) == 0 {
		report.Summary = "扫描范围是巡检持仓与关注仓，先用任意一处添加标的再来扫描"
		return report
	}

	rows := map[string]foundation.MarketSnapshotRow{}
	// byCode 是纯数字代码索引：持仓侧给 "002100.SZ"，快照侧可能给
	// "002100"，两侧口径不一就得靠 6 位数字对齐。
	byCode := map[string]foundation.MarketSnapshotRow{}
	if snapshotRows, err := e.snapshotRows(ctx); err == nil {
		for _, row := range snapshotRows {
			rows[row.Symbol] = row
			if code := sixDigitCode(row.Symbol); code != "" {
				byCode[code] = row
			}
		}
	}
	// 持仓AI巡检结论：一次取整份最新报告，按代码索引。
	inspections := map[string]*InspectionConclusion{}
	if e.inspection != nil {
		if conclusions, err := e.inspection.LatestInspectionConclusions(ctx); err == nil {
			for _, conclusion := range conclusions {
				code := sixDigitCode(conclusion.Symbol)
				if code == "" {
					code = conclusion.Symbol
				}
				item := conclusion
				inspections[code] = &item
			}
		} else if e.logger != nil {
			e.logger.Printf("level=warn event=monitor_radar_inspection_failed error=%q", err.Error())
		}
	}

	// 大盘语境：宽度统计与指数涨跌复用大盘规则同一份 marketStats，
	// 两市成交额由快照顺带汇总，不给雷达加新的数据链路。
	stats := e.marketStats(ctx, mapValues(rows))
	totalAmount := 0.0
	for _, row := range rows {
		if row.Suspended || row.Price <= 0 {
			continue
		}
		totalAmount += row.Amount
	}
	indexes := make([]RadarIndex, 0, len(stats.Indexes))
	for _, quote := range stats.Indexes {
		indexes = append(indexes, RadarIndex{ID: quote.ID, Name: quote.Name, ChangePct: round2(quote.ChangePct)})
	}
	report.Market = &RadarMarket{
		HaveIndexes:    stats.HaveIndexes,
		IndexChangePct: round2(stats.IndexChangePct),
		Indexes:        indexes,
		HaveBreadth:    stats.HaveBreadth,
		UpCount:        stats.UpCount,
		DownCount:      stats.DownCount,
		LimitUpCount:   stats.LimitUpCount,
		TotalAmount:    round2(totalAmount / 1e8),
	}

	var mu sync.Mutex
	states := make(map[string]RadarTargetState, len(targets))
	for _, symbol := range targets {
		states[symbol] = RadarTargetState{Symbol: symbol, Name: names[symbol], Source: sources[symbol], Status: "pending"}
	}
	finishStates := func() {
		for _, symbol := range targets {
			report.Targets = append(report.Targets, states[symbol])
		}
	}
	var wg sync.WaitGroup
	pages := make(chan string)
	findings := make([]RadarFinding, 0, len(targets))
	workers := 8
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for symbol := range pages {
				bars, ok := e.dailyBars(ctx, symbol, 120, now)
				closes, _ := closedSeries(bars)
				if !ok || len(closes) < 6 {
					mu.Lock()
					report.Skipped++
					state := states[symbol]
					state.Status, state.Reason = "insufficient_data", "日线不可用或不足 6 根"
					states[symbol] = state
					mu.Unlock()
					continue
				}
				var row *foundation.MarketSnapshotRow
				if cached, ok := rows[symbol]; ok {
					row = &cached
				} else if code := sixDigitCode(symbol); code != "" {
					if cached, ok := byCode[code]; ok {
						row = &cached
					}
				}
				finding, ok := radarFindingFromBars(symbol, bars, row, positions[symbol], inspections[sixDigitCode(symbol)])
				mu.Lock()
				state := states[symbol]
				state.Status, state.DailyAsOf = "evaluated", barDate(bars[len(bars)-1])
				if row != nil && row.Name != "" {
					state.Name = row.Name
				}
				if ok {
					state.Kind = finding.Kind
				}
				states[symbol] = state
				mu.Unlock()
				if !ok {
					continue
				}
				if finding.Name == "" {
					finding.Name = names[symbol]
				}
				finding.Source = sources[symbol]
				// 名称兜底发生在计算核之外，预填规则名要跟着换成股票名：
				// 「雷达盯盘·天康生物 MA5 上穿 MA20」比带代码更好认。
				// 用 seed 重算而不是拼字符串，信号/形态两条分支的命名规则只留一处。
				if finding.Name != "" {
					finding.SuggestedRule = radarSuggestedRule(finding)
				}
				mu.Lock()
				findings = append(findings, finding)
				mu.Unlock()
			}
		}()
	}
	for _, symbol := range targets {
		select {
		case pages <- symbol:
		case <-ctx.Done():
			close(pages)
			wg.Wait()
			finishStates()
			report.Error = "扫描被中断，稍后再试"
			return report
		}
	}
	close(pages)
	wg.Wait()
	finishStates()

	// 按净分绝对值排序：机会与风险都是"值得马上看"的，幅度代表强度。
	sort.SliceStable(findings, func(i, j int) bool {
		return math.Abs(findings[i].Score) > math.Abs(findings[j].Score)
	})
	for _, finding := range findings {
		report.TotalCounts[finding.Kind]++
	}
	report.TotalFindings = len(findings)
	if len(findings) > radarMaxFindings {
		findings = findings[:radarMaxFindings]
	}
	for _, finding := range findings {
		report.Counts[finding.Kind]++
	}
	report.Findings = findings
	report.Scanned = len(targets)
	// 池内当日异动：整体风平浪静时填空态用。按涨跌绝对值取前 6。
	movers := []RadarMover{}
	for _, symbol := range targets {
		row, ok := rows[symbol]
		if !ok {
			if code := sixDigitCode(symbol); code != "" {
				row, ok = byCode[code]
			}
		}
		if !ok || row.Suspended || row.Price <= 0 {
			continue
		}
		movers = append(movers, RadarMover{
			Symbol:        symbol,
			Name:          row.Name,
			ChangePercent: row.ChangePercent,
			VolumeRatio:   row.VolumeRatio,
		})
	}
	sort.SliceStable(movers, func(i, j int) bool {
		return math.Abs(movers[i].ChangePercent) > math.Abs(movers[j].ChangePercent)
	})
	if len(movers) > 6 {
		movers = movers[:6]
	}
	report.Movers = movers
	// 没进入结论的标的不算失败：雷达的价值是筛出"有事发生"的少数。
	report.Summary = fmt.Sprintf("扫描 %d 只标的：机会 %d、风险 %d、观察 %d",
		report.Scanned, report.Counts["opportunity"], report.Counts["risk"], report.Counts["watch"])
	if report.Skipped > 0 {
		report.Summary += fmt.Sprintf("（%d 只数据不足已跳过）", report.Skipped)
	}
	return report
}
