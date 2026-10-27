package monitor

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

var (
	errAbnormalSymbolRequired = errors.New("请先选择要计算的标的")
	errAbnormalSnapshotMiss   = errors.New("拉不到该标的的日线行情，请稍后重试")
)

// 异动预警窗口估算：偏离值 = 个股区间涨跌幅 - 基准指数同期涨跌幅。
// 这里只评估滚动窗口，不代表交易所公告认定，也不含公告后的重置与同向异动次数。
//
//	一般异动（3 日窗口，按收盘价）：主板 ±20、创业/科创 ±30、
//	主板 ST ±20、北交所 ±40。
//	沪深长窗口：10 日 +100%/-50%，30 日 +200%/-70%；
//	北交所：10 日 +150%/-60%，30 日 +300%/-75%。
const (
	AbnormalWindow3d  = "3d"
	AbnormalWindow10d = "10d"
	AbnormalWindow30d = "30d"
)

// AbnormalWindow 是单个窗口的评估快照。
type AbnormalWindow struct {
	Key           string  `json:"key"`
	Label         string  `json:"label"`
	Dev           float64 `json:"dev"`
	Threshold     float64 `json:"threshold"`
	ThresholdUp   float64 `json:"threshold_up"`
	ThresholdDown float64 `json:"threshold_down"`
	Direction     string  `json:"direction"`
	Progress      float64 `json:"progress"`
	Remaining     float64 `json:"remaining"`
	TriggerPrice  float64 `json:"trigger_price"`
}

// abnormalThreshold3d 返回该标的现行 3 日窗口阈值。
func abnormalThreshold3d(board string, isST bool) float64 {
	// 2026-07-06 起沪深主板风险警示股票的异常波动阈值已由 ±12% 上调至 ±20%，
	// 与普通股一致：深交所《交易规则》2026 修订 5.4.3、上交所 5.4.2
	// 都只列主板 20 / 创业板科创 30，不再单列 ST。
	// isST 留在签名里：页面要展示 ST 身份，规则若再分化不必改调用方。
	_ = isST
	return DeviationThreshold(board)
}

func abnormalLongThresholds(board string) (up10, down10, up30, down30 float64) {
	if board == foundation.BoardBSE {
		return 150, 60, 300, 75
	}
	return 100, 50, 200, 70
}

// evalAbnormalWindows 对 3/10/30 日窗口分别评估。Progress 取偏离指向
// 方向的阈值（10/30 日上下阈值不对称），Remaining 是沿该方向继续走到
// 触发还差的偏离值（百分点，负值代表已越过阈值）。
func evalAbnormalWindows(dev3, dev10, dev30 float64, board string, isST bool, price float64, windowBases ...float64) []AbnormalWindow {
	t3 := abnormalThreshold3d(board, isST)
	up10, down10, up30, down30 := abnormalLongThresholds(board)
	specs := []struct {
		key, label string
		dev        float64
		up, down   float64
	}{
		{AbnormalWindow3d, "3日偏离", dev3, t3, t3},
		{AbnormalWindow10d, "10日偏离", dev10, up10, down10},
		{AbnormalWindow30d, "30日偏离", dev30, up30, down30},
	}
	windows := make([]AbnormalWindow, 0, len(specs))
	for i, spec := range specs {
		direction := "up"
		threshold := spec.up
		if spec.dev < 0 {
			direction, threshold = "down", spec.down
		}
		progress := 0.0
		if threshold > 0 {
			progress = math.Abs(spec.dev) / threshold
		}
		remaining := threshold - math.Abs(spec.dev)
		trigger := 0.0
		if price > 0 && i < len(windowBases) && windowBases[i] > 0 {
			// 偏离的百分点相对窗口起始价；保持当前基准指数点位不变估算。
			delta := windowBases[i] * remaining / 100
			if direction == "up" {
				trigger = round2(price + delta)
			} else {
				trigger = round2(price - delta)
			}
		}
		windows = append(windows, AbnormalWindow{
			Key: spec.key, Label: spec.label,
			Dev: round2(spec.dev), Threshold: threshold,
			ThresholdUp: spec.up, ThresholdDown: spec.down,
			// 判定保留原始比例，显示时再格式化，避免 99.6% 被舍入成已越线。
			Direction: direction, Progress: progress,
			Remaining: round2(remaining), TriggerPrice: trigger,
		})
	}
	return windows
}

func abnormalWindowAvailability(available [3]bool) map[string]bool {
	return map[string]bool{AbnormalWindow3d: available[0], AbnormalWindow10d: available[1], AbnormalWindow30d: available[2]}
}

// 缺少样本或指数端点的窗口不返回数值，不把缺失值伪装成正常的 0。
func availableAbnormalWindows(windows []AbnormalWindow, available [3]bool) []AbnormalWindow {
	valid := make([]AbnormalWindow, 0, len(windows))
	for i, window := range windows {
		if i < len(available) && available[i] {
			valid = append(valid, window)
		}
	}
	return valid
}

// worstAbnormalWindow 取最接近触发的窗口（Progress 最大）。
func worstAbnormalWindow(windows []AbnormalWindow) AbnormalWindow {
	worst := windows[0]
	for _, window := range windows[1:] {
		if window.Progress > worst.Progress {
			worst = window
		}
	}
	return worst
}

// abnormalTriggeredOn 判断某个偏离值是否越过该窗口的阈值。
// 10/30 日上下阈值不对称，所以上/下分别给。
func abnormalTriggeredOn(dev *float64, up, down float64) bool {
	if dev == nil {
		return false
	}
	return *dev >= up || *dev <= -down
}

// lastAbnormalTriggerDate 从逐日偏离明细里找最近一次越过阈值的交易日。
// 「触发日期」是用户加监控后最想知道的一件事（哪一天开始算异动），
// 逐日明细里本来就是同一个口径，直接反查，不依赖规则引擎是否配了规则。
// 返回空串表示最近 30 个交易日内没有越过任何窗口阈值。
func lastAbnormalTriggerDate(rows []AbnormalCalcRow, board string, isST bool) string {
	t3 := abnormalThreshold3d(board, isST)
	up10, down10, up30, down30 := abnormalLongThresholds(board)
	last := ""
	for _, row := range rows {
		if !abnormalTriggeredOn(row.Dev3, t3, t3) &&
			!abnormalTriggeredOn(row.Dev10, up10, down10) &&
			!abnormalTriggeredOn(row.Dev30, up30, down30) {
			continue
		}
		if row.Date > last {
			last = row.Date
		}
	}
	return last
}

// abnormalLevel 由最接近触发的窗口决定分级，阈值与页面视图保持一致：
// 触发 ≥1.0、接近 ≥0.7、观察 ≥0.5。
// abnormalLevel 把三个窗口的最大进度折成页面上的状态：
// 越过阈值 = 已触发；70% 以上 = 接近阈值；50% 以上 = 观察；其余正常。
func abnormalLevel(windows []AbnormalWindow) string {
	worst := 0.0
	for _, window := range windows {
		if window.Progress > worst {
			worst = window.Progress
		}
	}
	switch {
	case worst >= 1:
		return "triggered"
	case worst >= 0.7:
		return "near"
	case worst >= 0.5:
		return "watch"
	default:
		return "normal"
	}
}

// abnormalSevere 判断 10 日、30 日长窗口是否达到其所属板块的估算阈值。
// 3 日窗口不参与本项分类；估算越线不等同于交易所公告认定。
func abnormalSevere(windows []AbnormalWindow) bool {
	for _, window := range windows {
		if window.Key != AbnormalWindow10d && window.Key != AbnormalWindow30d {
			continue
		}
		if window.Progress >= 1 {
			return true
		}
	}
	return false
}

// abnormalWorthListing 决定一只票该不该出现在预警列表里。
//
// 规则：已触发 / 接近阈值进列表；用户自己加了监控的一律保留（没苗头也要能看到距离）。
// 观察级（阈值 50%~70%）不进——那只是"涨了点"，不是异动；硬塞进来会让列表
// 长期挂着一只根本不该管的票（比如持仓里涨了 13% 的个股），而且页面没有入口
// 把它去掉，用户只能看着它一直在。
func abnormalWorthListing(level string, monitored bool) bool {
	if monitored {
		return true
	}
	return level == "triggered" || level == "near"
}

// deviationEventCounts 统计最近 N 个交易日内每只标的的偏离触发次数，
// 用于「多次异动」。按交易日历截取范围，不因期间无事件而向前扩展；
// 没配偏离规则时没有事件，自然返回空表。
func (e *Engine) deviationEventCounts(ctx context.Context, days int) map[string]int {
	if days < 1 {
		return nil
	}
	end := foundation.LatestAStockSession(e.now())
	day, err := time.ParseInLocation("2006-01-02", end, foundation.AStockLocation)
	if err != nil {
		return nil
	}
	for remaining := days - 1; remaining > 0; {
		day = day.AddDate(0, 0, -1)
		if foundation.IsAStockTradingDay(day) {
			remaining--
		}
	}
	counts, _ := e.store.DeviationCounts(ctx, day.Format("2006-01-02"), end)
	return counts
}

// ---------------------------------------------------------------- 单票计算器

// AbnormalCalcRow 是计算过程明细的一行：与 bigamap 的逐日表同构。
type AbnormalCalcRow struct {
	Date        string   `json:"date"`
	StockChange float64  `json:"stock_change"`
	IndexChange *float64 `json:"index_change,omitempty"`
	Dev3        *float64 `json:"dev_3d,omitempty"`
	Dev10       *float64 `json:"dev_10d,omitempty"`
	Dev30       *float64 `json:"dev_30d,omitempty"`
}

// AbnormalCalcView 是单票异动计算结果。
type AbnormalCalcView struct {
	Symbol             string            `json:"symbol"`
	Name               string            `json:"name"`
	Board              string            `json:"board"`
	IsST               bool              `json:"is_st"`
	Price              float64           `json:"price"`
	ChangePercent      float64           `json:"change_percent"`
	BenchmarkID        string            `json:"benchmark_id,omitempty"`
	BenchmarkLabel     string            `json:"benchmark_label,omitempty"`
	Windows            []AbnormalWindow  `json:"windows,omitempty"`
	WindowAvailability map[string]bool   `json:"window_availability"`
	Level              string            `json:"level,omitempty"`
	Rows               []AbnormalCalcRow `json:"rows,omitempty"`
	// LastTriggerDate 是最近一次越过阈值的交易日：展开明细里直接读它，
	// 不用前端再自己扫一遍逐日表。
	LastTriggerDate string    `json:"last_trigger_date,omitempty"`
	GeneratedAt     time.Time `json:"generated_at"`
	Error           string    `json:"error,omitempty"`
}

// abnormalBenchmarkLabels 与 deviationIndexID 的映射一一对应。
var abnormalBenchmarkLabels = map[string]string{
	"sse_a":        "上证A股指数",
	"szse_a":       "深证A股指数",
	"chinext_comp": "创业板综指",
	"star50":       "科创50",
	"bse50":        "北证50",
}

// AbnormalCalc 计算一只标的的异动窗口与近 30 个交易日明细。
// 逐日明细按交易日对齐指数收盘价，停牌日直接缺列。
func (e *Engine) AbnormalCalc(ctx context.Context, symbol string) (AbnormalCalcView, error) {
	view := AbnormalCalcView{Symbol: symbol, GeneratedAt: e.now(), WindowAvailability: abnormalWindowAvailability([3]bool{})}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return view, errAbnormalSymbolRequired
	}
	view.Symbol = symbol
	// 单票计算只需要这一只的行情，不依赖全市场快照：快照源抖动时
	// 也能按代码段推断板块、用日线末根收盘价兜底。
	row, inSnapshot := e.snapshotSymbolRow(symbol)
	bars, haveBars := e.dailyBars(ctx, symbol, 120, view.GeneratedAt)
	if !haveBars {
		return view, errAbnormalSnapshotMiss
	}
	board := foundation.DetectBoard(symbol, "")
	if inSnapshot {
		board = row.Board
		view.Name, view.IsST = row.Name, row.IsST
		view.Price, view.ChangePercent = row.Price, row.ChangePercent
	} else {
		last := bars[len(bars)-1].Close
		view.Price = round2(last)
		if len(bars) >= 2 && bars[len(bars)-2].Close > 0 {
			view.ChangePercent = round2((last/bars[len(bars)-2].Close - 1) * 100)
		}
	}
	// 快照行里也可能没带名称（快照源只给了价格和涨跌幅），
	// 名称缺失统一回落到 symbolNames：页面不能只剩一串代码。
	if view.Name == "" {
		view.Name = symbolName(e.symbolNames(ctx, []string{symbol}), symbol)
	}
	view.Board = board
	indexID := deviationIndexID(board, symbol)
	if indexID == "" {
		view.Error = "该板块暂无可用基准指数数据"
		return view, nil
	}
	view.BenchmarkID = indexID
	view.BenchmarkLabel = abnormalBenchmarkLabels[indexID]
	indexBars, haveIndex := e.indexBars(ctx, indexID, view.GeneratedAt)
	if !haveIndex {
		view.Error = "基准指数数据不足，暂无法计算偏离值"
		return view, nil
	}
	// 指数收盘按日期对齐到个股日线：停牌缺口显式标记，逐日表不再错位。
	aligned := alignIndexToBars(bars, indexBars)
	if aligned[len(aligned)-1] <= 0 {
		view.Error = "基准指数缺少最近交易日数据，暂无法计算偏离值"
		return view, nil
	}
	// 单票详情同样用实时快照价覆盖日线 today-bar 收盘价：偏离值与触发判断必须
	// 和页面现价同步跳动，否则看起来像没接实时行情。bars 是共享缓存切片，
	// 不能直接改末根，复制末根后再覆盖。
	calcBars := bars
	currentSession := barDate(bars[len(bars)-1]) == foundation.LatestAStockSession(view.GeneratedAt)
	projectionPrice := view.Price
	if inSnapshot && row.Price > 0 && currentSession {
		last := bars[len(bars)-1]
		last.Close = row.Price
		calcBars = append(bars[:len(bars)-1:len(bars)-1], last)
	}
	if !currentSession {
		view.Error = "日线尚未包含当前交易日，偏离值暂按历史收盘计算"
		projectionPrice = 0
	}
	view.Rows = abnormalCalcRows(calcBars, aligned, 30)
	// 触发日期与逐日明细同一口径：展开行和监控清单都直接用它。
	view.LastTriggerDate = lastAbnormalTriggerDate(view.Rows, board, view.IsST)
	dev3, ok3 := deviationWindowDev(calcBars, aligned, 3)
	dev10, ok10 := deviationWindowDev(calcBars, aligned, 10)
	dev30, ok30 := deviationWindowDev(calcBars, aligned, 30)
	available := [3]bool{ok3, ok10, ok30}
	view.WindowAvailability = abnormalWindowAvailability(available)
	view.Windows = availableAbnormalWindows(evalAbnormalWindows(dev3, dev10, dev30, board, view.IsST, projectionPrice, abnormalWindowBases(calcBars)...), available)
	if !ok3 || !ok10 || !ok30 {
		if view.Error != "" {
			view.Error += "；"
		}
		view.Error += "部分窗口样本不足或基准数据缺失，未就绪窗口不参与判定"
	}
	if len(view.Windows) > 0 {
		view.Level = abnormalLevel(view.Windows)
	}
	return view, nil
}

// abnormalCalcRows 生成最近 days 个交易日的逐日明细：当日涨跌幅 +
// 以该日为终点的 N 日累计偏离。指数缺列时只保留个股涨跌幅。
func abnormalCalcRows(bars []foundation.KLine, alignedIndex []float64, days int) []AbnormalCalcRow {
	n := len(bars)
	start := n - days
	if start < 1 {
		start = 1
	}
	rows := make([]AbnormalCalcRow, 0, n-start)
	for i := start; i < n; i++ {
		row := AbnormalCalcRow{Date: bars[i].Time.In(foundation.AStockLocation).Format("2006-01-02")}
		if bars[i-1].Close > 0 {
			row.StockChange = round2((bars[i].Close/bars[i-1].Close - 1) * 100)
		}
		if alignedIndex[i] > 0 && alignedIndex[i-1] > 0 {
			indexChange := round2((alignedIndex[i]/alignedIndex[i-1] - 1) * 100)
			row.IndexChange = &indexChange
		}
		appendDev := func(target **float64, window int) {
			if i < window || alignedIndex[i] <= 0 || alignedIndex[i-window] <= 0 || bars[i-window].Close <= 0 {
				return
			}
			dev := round2((bars[i].Close/bars[i-window].Close - alignedIndex[i]/alignedIndex[i-window]) * 100)
			*target = &dev
		}
		appendDev(&row.Dev3, 3)
		appendDev(&row.Dev10, 10)
		appendDev(&row.Dev30, 30)
		rows = append(rows, row)
	}
	return rows
}

// deviationWindowDev 计算以最后一根棒为终点的 N 日偏离值；
// 与 deviationValue 同口径，但走按日对齐后的指数序列。
func deviationWindowDev(bars []foundation.KLine, alignedIndex []float64, days int) (float64, bool) {
	n := len(bars)
	if n <= days || alignedIndex[n-1] <= 0 || alignedIndex[n-1-days] <= 0 {
		return 0, false
	}
	if bars[n-1-days].Close <= 0 {
		return 0, false
	}
	stock := (bars[n-1].Close/bars[n-1-days].Close - 1) * 100
	index := (alignedIndex[n-1]/alignedIndex[n-1-days] - 1) * 100
	return round2(stock - index), true
}

func abnormalWindowBases(bars []foundation.KLine) []float64 {
	bases := make([]float64, 3)
	for i, days := range []int{3, 10, 30} {
		if len(bars) > days {
			bases[i] = bars[len(bars)-1-days].Close
		}
	}
	return bases
}

// knownSymbols 是不依赖全市场快照就能枚举出来的标的集合：
// 持仓 + 关注仓 + 启用规则显式指定的标的。
func (e *Engine) knownSymbols(ctx context.Context) []string {
	set := map[string]bool{}
	for _, target := range e.inspectionTargets(ctx) {
		if target.Symbol != "" {
			set[target.Symbol] = true
		}
	}
	// 用户加入监控的个股（monitor 自己的关注仓）优先：这些标的无论
	// 有没有苗头都要出状态，池子里不能漏掉它们。
	if e.store != nil {
		if entries, err := e.store.ListWatchlist(ctx); err == nil {
			for _, entry := range entries {
				if strings.TrimSpace(entry.Symbol) != "" {
					set[entry.Symbol] = true
				}
			}
		}
	}
	if e.portfolio != nil {
		if holdings, err := e.portfolio.Holdings(ctx); err == nil {
			for _, holding := range holdings {
				if holding.Quantity > 0 && strings.TrimSpace(holding.Symbol) != "" {
					set[holding.Symbol] = true
				}
			}
		}
		if watch, err := e.portfolio.WatchSymbols(ctx); err == nil {
			for _, symbol := range watch {
				if strings.TrimSpace(symbol) != "" {
					set[symbol] = true
				}
			}
		}
	}
	for _, symbol := range e.enabledSymbols(ctx) {
		set[symbol] = true
	}
	symbols := make([]string, 0, len(set))
	for symbol := range set {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

// monitoredSymbols 是用户从异动页侧栏加入监控的个股集合（关注仓）：
// 这些标的即使没走到阈值 50% 也持续计算并展示，页面侧栏据此折叠列表。
func (e *Engine) monitoredSymbols(ctx context.Context) map[string]bool {
	set := map[string]bool{}
	if e == nil || e.store == nil {
		return set
	}
	entries, err := e.store.ListWatchlist(ctx)
	if err != nil {
		return set
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.Symbol) != "" {
			set[entry.Symbol] = true
		}
	}
	return set
}

// abnormalItemForSymbol 把单票计算器结果转成列表行：监控个股可能不在
// 全市场快照里（停牌/快照缺行），这条路径保证侧栏每只监控个股都有状态。
// 数据不足（日线拉不到、上市未满窗口）时返回 false，由调用方跳过。
func (e *Engine) abnormalItemForSymbol(ctx context.Context, symbol string, eventCounts map[string]int) (AbnormalItem, bool) {
	calc, err := e.AbnormalCalc(ctx, symbol)
	if err != nil || len(calc.Windows) == 0 {
		return AbnormalItem{}, false
	}
	item := AbnormalItem{
		Symbol: symbol, Name: calc.Name, Board: calc.Board,
		Price: calc.Price, ChangePercent: calc.ChangePercent,
		Level: calc.Level, Windows: calc.Windows, WindowAvailability: calc.WindowAvailability,
		IsST: calc.IsST, Monitored: true,
		Severe:  abnormalSevere(calc.Windows),
		Signals: []string{"deviation"},
	}
	for _, window := range calc.Windows {
		switch window.Key {
		case AbnormalWindow3d:
			item.Dev3 = window.Dev
		case AbnormalWindow10d:
			item.Dev10 = window.Dev
		case AbnormalWindow30d:
			item.Dev30 = window.Dev
		}
	}
	worst := worstAbnormalWindow(calc.Windows)
	item.WorstKey, item.Threshold, item.Direction = worst.Key, worst.Threshold, worst.Direction
	item.Remaining, item.TriggerPrice = worst.Remaining, worst.TriggerPrice
	// 触发日期从同一份逐日明细反查：侧栏「异动名单个股」要展示它。
	item.LastTriggerDate = lastAbnormalTriggerDate(calc.Rows, calc.Board, calc.IsST)
	if len(calc.Rows) > 0 {
		item.DailyAsOf = calc.Rows[len(calc.Rows)-1].Date
		item.Historical = item.DailyAsOf != foundation.LatestAStockSession(calc.GeneratedAt)
		item.CalculatedAt = &calc.GeneratedAt
	}
	if eventCounts != nil {
		item.Events10d = eventCounts[symbol]
	}
	return item, true
}

// fillAbnormalWithoutSnapshot 在全市场快照不可用时兜底算一轮异动。
// 返回是否有可展示的行——有行时页面不报错，只在顶部给一句降级说明。
func (e *Engine) fillAbnormalWithoutSnapshot(ctx context.Context, view *AbnormalView) bool {
	symbols := e.knownSymbols(ctx)
	if len(symbols) == 0 {
		return false
	}
	monitored := e.monitoredSymbols(ctx)
	results := e.computeCandidates(ctx, symbols, e.now())
	counts := e.deviationEventCounts(ctx, 10)
	// 快照拿不到时名称按 关注仓 → 持仓/巡检 → 实时行情 逐层补。
	names := e.symbolNames(ctx, symbols)
	for symbol, dev := range results.abnormal {
		quote := results.quote[symbol]
		if quote.Price <= 0 {
			continue
		}
		board := foundation.DetectBoard(symbol, "")
		projectionPrice := quote.Price
		if dev.Stale {
			projectionPrice = 0
			view.Error = "部分标的日线尚未包含当前交易日，偏离值按历史收盘计算，暂不估算触发价"
		}
		windows := availableAbnormalWindows(evalAbnormalWindows(dev.Dev3, dev.Dev10, dev.Dev30, board, false, projectionPrice, dev.Base3, dev.Base10, dev.Base30), dev.Available)
		level := abnormalLevel(windows)
		// 降级模式同理：已触发 / 接近阈值才进列表，观察级只有用户监控才留。
		if !abnormalWorthListing(level, monitored[symbol]) {
			continue
		}
		worst := worstAbnormalWindow(windows)
		item := AbnormalItem{
			Symbol: symbol, Board: board, Price: quote.Price, ChangePercent: quote.ChangePercent,
			Signals: []string{"deviation"}, Level: level, Severe: abnormalSevere(windows),
			Dev3: dev.Dev3, Dev10: dev.Dev10, Dev30: dev.Dev30,
			Windows: windows, WindowAvailability: abnormalWindowAvailability(dev.Available), WorstKey: worst.Key, Threshold: worst.Threshold,
			Direction: worst.Direction, Remaining: worst.Remaining, TriggerPrice: worst.TriggerPrice,
			Events10d: counts[symbol], Monitored: monitored[symbol],
			DailyAsOf: dev.DailyAsOf, Historical: dev.Stale,
		}
		if !dev.FetchedAt.IsZero() {
			calculatedAt := dev.FetchedAt
			item.CalculatedAt = &calculatedAt
		}
		if row, ok := e.snapshotSymbolRow(symbol); ok {
			item.Name, item.IsST = row.Name, row.IsST
		}
		if item.Name == "" {
			item.Name = symbolName(names, symbol)
		}
		view.Items = append(view.Items, item)
	}
	sort.SliceStable(view.Items, func(i, j int) bool {
		left, right := view.Items[i], view.Items[j]
		return maxAbs(left.Dev3, left.Dev10, left.Dev30) > maxAbs(right.Dev3, right.Dev10, right.Dev30)
	})
	if limit := 200; len(view.Items) > limit {
		view.Items = view.Items[:limit]
	}
	view.Total = len(view.Items)
	view.TradeDate = e.now().In(foundation.AStockLocation).Format("2006-01-02")
	return len(view.Items) > 0
}

// symbolName 按「完整代码 → 六位代码」查名字：数据源给的后缀格式不统一
// （有的带 .SZ/.SH，有的只有六位），两侧都登记才不会查漏。
func symbolName(names map[string]string, symbol string) string {
	if name := names[symbol]; name != "" {
		return name
	}
	if code := sixDigitCode(symbol); code != "" {
		return names[code]
	}
	return ""
}

// symbolNames 汇总「代码 → 名称」：关注仓 + 持仓/巡检 + 实时行情兜底。
// 全市场快照不可用时名称只能这样一层层补——列表不能退化成一串裸代码，
// 页面上的「标的」列要能读出中文名。
func (e *Engine) symbolNames(ctx context.Context, symbols []string) map[string]string {
	names := map[string]string{}
	add := func(symbol, name string) {
		if strings.TrimSpace(symbol) == "" || strings.TrimSpace(name) == "" {
			return
		}
		names[symbol] = name
		// 六位代码同义登记：调用方拿到的代码可能不带交易所后缀。
		if code := sixDigitCode(symbol); code != "" {
			names[code] = name
		}
	}
	if e.store != nil {
		if entries, err := e.store.ListWatchlist(ctx); err == nil {
			for _, entry := range entries {
				add(entry.Symbol, entry.Name)
			}
		}
	}
	// 持仓与巡检清单自带名称，且已合并去重。
	targets := e.ScopeTargets(ctx)
	for _, scope := range []string{ScopePositions, ScopeInspection, ScopeWatchlist} {
		for _, target := range targets[scope] {
			add(target.Symbol, target.Name)
		}
	}
	// 剩下的（规则标的、涨跌幅榜候选）走实时行情问一次名称。
	missing := make([]string, 0)
	for _, symbol := range symbols {
		if symbolName(names, symbol) == "" {
			missing = append(missing, symbol)
		}
	}
	if len(missing) > 0 && e.realtime != nil {
		if quotes, err := e.realtime.Realtime(ctx, missing); err == nil {
			for _, quote := range quotes {
				add(quote.Symbol, quote.Name)
			}
		}
	}
	return names
}

// alignIndexToBars 把指数收盘序列按交易日对齐到个股日线。
// 个股停牌日指数没有对应位置，填 0 交给调用方跳过，避免下标错位。
func alignIndexToBars(bars []foundation.KLine, indexBars []foundation.KLine) []float64 {
	indexByDate := make(map[string]float64, len(indexBars))
	for _, bar := range indexBars {
		indexByDate[bar.Time.In(foundation.AStockLocation).Format("2006-01-02")] = bar.Close
	}
	aligned := make([]float64, len(bars))
	for i, bar := range bars {
		aligned[i] = indexByDate[bar.Time.In(foundation.AStockLocation).Format("2006-01-02")]
	}
	return aligned
}
