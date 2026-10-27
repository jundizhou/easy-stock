// monitor.Engine 是监控中心的常驻评估循环。
//
// 与其他取数调度器不同的是，它还承担「按规则判定」这一步，为了避免
// 判定与数据耦合，规则评估在 rules.go 里是纯函数，engine 只负责装
// Observation、调度任务与投递事件。
package monitor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/runtimelog"
)

type RealtimeProvider interface {
	Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error)
}

type SnapshotProvider interface {
	MarketSnapshot(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error)
}

type KLineProvider interface {
	KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error)
}

type IndexSeriesProvider interface {
	MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error)
}

// Holding 是 engine 视角的一笔持仓：字段恰好够算成本与止盈止损。
// 行情与持仓提供方通过这些字段对接，监控不依赖交易执行模块。
type Holding struct {
	Symbol          string
	Name            string
	Quantity        int
	AverageCost     float64
	StopLossPrice   float64
	TakeProfitPrice float64
	LastBuyDate     string
	AccountID       string
}

type PortfolioSource interface {
	Holdings(ctx context.Context) ([]Holding, error)
	WatchSymbols(ctx context.Context) ([]string, error)
}

// NotifyFunc 由 httpapi 装上：它知道通知事件开关与 Dispatcher 的存在。
// 返回值即事件落库使用的投递状态与失败原因。
type NotifyFunc func(ctx context.Context, event Event) (string, error)

type EngineConfig struct {
	Store              *Store
	Realtime           RealtimeProvider
	Snapshot           SnapshotProvider
	KLine              KLineProvider
	Index              IndexSeriesProvider
	Portfolio          PortfolioSource
	Inspection         InspectionProvider
	InspectionHoldings InspectionHoldingsProvider
	SectorRank         SectorRankProvider
	SectorIntraday     SectorIntradayProvider
	SectorCatalog      SectorCatalogProvider
	ThemeCatalog       ThemeCatalogProvider
	Settings           func() Settings
	Notify             NotifyFunc
	Logger             *log.Logger
}

type Status struct {
	Running            bool   `json:"running"`
	Enabled            bool   `json:"enabled"`
	SnapshotAgeSeconds int64  `json:"snapshot_age_seconds"`
	SnapshotCount      int    `json:"snapshot_count"`
	SnapshotError      string `json:"snapshot_error,omitempty"`
	SnapshotSource     string `json:"snapshot_source,omitempty"`
	SnapshotStale      bool   `json:"snapshot_stale"`
	SnapshotWarning    string `json:"snapshot_warning,omitempty"`
	SessionOpen        bool   `json:"session_open"`
	LastSnapshotAt     string `json:"last_snapshot_at,omitempty"`
	LastQuoteAt        string `json:"last_quote_at,omitempty"`
	LastRunAt          string `json:"last_run_at,omitempty"`
	LastError          string `json:"last_error,omitempty"`
	RuleCount          int    `json:"rule_count"`
	LotCount           int    `json:"lot_count"`
}

// Engine 持有缓存与常驻循环的所有权。缓存由后台任务写、HTTP 只读视图读，
// 一把互斥锁是最简单且够用的并发模型。
type Engine struct {
	store              *Store
	realtime           RealtimeProvider
	snapshot           SnapshotProvider
	kline              KLineProvider
	index              IndexSeriesProvider
	portfolio          PortfolioSource
	inspection         InspectionProvider
	inspectionHoldings InspectionHoldingsProvider
	sectorRank         SectorRankProvider
	sectorIntraday     SectorIntradayProvider
	catalogSrc         SectorCatalogProvider
	themeCatalog       ThemeCatalogProvider
	settings           func() Settings
	notify             NotifyFunc
	logger             *log.Logger
	now                func() time.Time

	mu            sync.Mutex
	snapRows      []foundation.MarketSnapshotRow
	snapBySymbol  map[string]foundation.MarketSnapshotRow
	snapFetchedAt time.Time
	snapTradeDate string
	snapSourceAt  time.Time
	snapError     string
	snapFetching  bool
	fetchDone     chan struct{}

	watchQuotes     map[string]foundation.Quote
	watchQuotesAt   time.Time
	watchQuoteTimes map[string]time.Time

	sectorRanks       map[string]foundation.SectorFlowRank
	sectorRankAt      time.Time
	catalog           []foundation.StockCatalogEntry
	catalogAt         time.Time
	themes            map[int]ThemeSummary
	themesAt          time.Time
	themeMembersCache map[int][]string
	themeMembersAt    time.Time

	highLow     map[string]highLowResult
	deviations  map[string]deviationResult
	klines      map[string]klineEntry
	signals     map[string]signalResult
	indexSeries map[string]cachedIndexSeries

	pendingDigest []pendingEvent
	nextDigest    time.Time

	radarReport    *RadarReport
	radarFetchedAt time.Time

	// liveBus 是监控中心实时价广播总线：快照刷新后把价推给所有订阅者（SSE）。
	liveBus *liveBus

	status          Status
	radarScanMu     sync.Mutex
	radarGeneration uint64
	lastPruneDay    string
	lastDateRun     string
}

type pendingEvent struct {
	id       int64
	severity string
	event    Event
	attempts int
}

type highLowResult struct {
	High      bool
	Low       bool
	FetchedAt time.Time
}

type deviationResult struct {
	Dev3, Dev10, Dev30    float64
	Base3, Base10, Base30 float64
	FetchedAt             time.Time
	Stale                 bool
	DailyAsOf             string
	Available             [3]bool
}

type klineEntry struct {
	bars      []foundation.KLine
	day       string
	FetchedAt time.Time
}

type cachedIndexSeries struct {
	closes    []float64
	bars      []foundation.KLine
	FetchedAt time.Time
}

func NewEngine(cfg EngineConfig) *Engine {
	if cfg.Settings == nil {
		defaults := DefaultSettings()
		cfg.Settings = func() Settings { return defaults }
	}
	return &Engine{
		store:              cfg.Store,
		realtime:           cfg.Realtime,
		snapshot:           cfg.Snapshot,
		kline:              cfg.KLine,
		index:              cfg.Index,
		portfolio:          cfg.Portfolio,
		inspection:         cfg.Inspection,
		inspectionHoldings: cfg.InspectionHoldings,
		sectorRank:         cfg.SectorRank,
		sectorIntraday:     cfg.SectorIntraday,
		catalogSrc:         cfg.SectorCatalog,
		themeCatalog:       cfg.ThemeCatalog,
		settings:           cfg.Settings,
		notify:             cfg.Notify,
		logger:             cfg.Logger,
		now:                time.Now,

		snapBySymbol: map[string]foundation.MarketSnapshotRow{},
		liveBus:      newLiveBus(),
		highLow:      map[string]highLowResult{},
		deviations:   map[string]deviationResult{},
		klines:       map[string]klineEntry{},
		signals:      map[string]signalResult{},
		indexSeries:  map[string]cachedIndexSeries{},
	}
}

// Run 常驻循环，ctx 取消即返回。到期提醒在启动时先跑一次：
// 重启进程不能让它沉默一整天。
func (e *Engine) Run(ctx context.Context) {
	e.mu.Lock()
	e.status.Running = true
	e.mu.Unlock()
	defer func() { e.mu.Lock(); e.status.Running = false; e.mu.Unlock() }()
	liveCtx, stopLive := context.WithCancel(ctx)
	liveDone := make(chan struct{})
	go func() {
		defer close(liveDone)
		e.runLiveQuotes(liveCtx)
	}()
	defer func() { stopLive(); <-liveDone }()

	// 重启遗留的 pending 摘要已经不可能被 flush，置为 skipped 留痕。
	_ = e.store.FailPendingDigest(ctx)
	// 启动时把持仓源同步成批次：「持仓」页签与「持仓」作用域必须指向同一批票。
	// 只在批次为空时同步——已经存在的批次可能被用户改过止盈止损或转手动，
	// 每次启动覆盖会抹掉这些手工调整。失败静默：独立部署模式没有持仓源。
	if _, err := e.syncPositionsIfEmpty(ctx); err != nil {
		_ = err
	}
	_, _, sessionOpen := sessionState(e.now())
	if sessionOpen {
		e.runDateRules(ctx, e.now())
		_, tradeDate, _ := sessionState(e.now())
		e.lastDateRun = tradeDate
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var nextQuotes, nextSnapshot, nextDeviation, nextSector time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := e.now()
		settings := e.settings()
		e.mu.Lock()
		e.status.Enabled = settings.Enabled
		e.mu.Unlock()
		if !settings.Enabled {
			// 关闭状态清空所有 due 时间：下次打开从「下一秒」开始，
			// 不把关闭期间的欠账一口气补上。
			nextQuotes, nextSnapshot, nextDeviation, nextSector = time.Time{}, time.Time{}, time.Time{}, time.Time{}
			continue
		}
		e.flushDigest(ctx, now)
		trading, tradeDate, sessionOpen := sessionState(now)
		if !trading {
			continue
		}
		if nextQuotes.IsZero() {
			// 首轮或从关闭状态恢复：快任务立即跑一轮，其余任务错开。
			nextQuotes = now
			nextSnapshot = now.Add(2 * time.Second)
			nextDeviation = now.Add(30 * time.Second)
			nextSector = now.Add(10 * time.Second)
		}
		if sessionOpen && !nextQuotes.After(now) {
			nextQuotes = now.Add(secondsMax(settings.QuoteIntervalSec, 3))
			e.runQuotesTask(ctx, now, tradeDate)
		}
		if sessionOpen && !nextSnapshot.After(now) {
			nextSnapshot = now.Add(secondsMax(settings.SnapshotIntervalSec, 10))
			e.runSnapshotTask(ctx, now, tradeDate)
		}
		if e.lastDateRun != tradeDate {
			e.runDateRules(ctx, now)
			e.lastDateRun = tradeDate
		}
		if sessionOpen && !nextDeviation.After(now) {
			nextDeviation = now.Add(60 * time.Second)
			e.runDeviationTask(ctx, now, tradeDate)
		}
		if sessionOpen && !nextSector.After(now) {
			nextSector = now.Add(60 * time.Second)
			e.runSectorTask(ctx, now, tradeDate)
		}
		e.maybePrune(ctx, now)
	}
}

func secondsMax(value, minimum int) time.Duration {
	if value < minimum {
		value = minimum
	}
	return time.Duration(value) * time.Second
}

// sessionState 返回今天是否 A 股交易日、交易日与是否盘中。
// 集合竞价只算交易日不算盘中：那时撮合价还没有意义。
func sessionState(now time.Time) (trading bool, tradeDate string, sessionOpen bool) {
	local := now.In(foundation.AStockLocation)
	tradeDate = local.Format("2006-01-02")
	if !foundation.IsAStockTradingDay(local) {
		return false, tradeDate, false
	}
	clockMinutes := local.Hour()*60 + local.Minute()
	within := func(start, end int) bool { return clockMinutes >= start && clockMinutes < end }
	trading = true
	sessionOpen = within(9*60+30, 11*60+30) || within(13*60, 15*60)
	return trading, tradeDate, sessionOpen
}

// ---------------------------------------------------------------- 任务

type taskResult struct {
	evaluated int
	err       error
}

func (e *Engine) recordRun(ctx context.Context, task string, startedAt time.Time, result taskResult, triggered int) {
	run := Run{
		Task: task, StartedAt: startedAt, FinishedAt: e.now(), Triggered: triggered,
		DurationMS: time.Since(startedAt).Milliseconds(),
	}
	if result.err != nil {
		run.Error = result.err.Error()
	}
	runCtx := ctx
	if ctx.Err() != nil {
		runCtx = context.WithoutCancel(ctx)
	}
	_ = e.store.AppendRun(runCtx, run)
	e.mu.Lock()
	e.status.LastRunAt = run.FinishedAt.Format(time.RFC3339)
	if run.Error != "" {
		e.status.LastError = run.Error
	}
	e.mu.Unlock()
}

// quoteRules 是快任务会评估的类型。
var quoteRules = map[string]bool{RuleTypePrice: true, RuleTypeVolume: true, RuleTypeAbnormal: true}

// runQuotesTask：3 秒档。覆盖 symbols/watchlist/positions scope 的规则；
// scope=all 的规则由 15 秒快照任务负责，两处分摊避免重复评估同一批标的。
func (e *Engine) runQuotesTask(ctx context.Context, now time.Time, tradeDate string) {
	startedAt := now
	rules, err := e.store.ListRules(ctx, "", true)
	if err != nil {
		e.recordRun(ctx, "quotes", startedAt, taskResult{err: err}, 0)
		return
	}
	targets := map[string]bool{}
	scopeSymbols := map[string][]string{}
	for _, rule := range rules {
		if !quoteRules[rule.Type] || rule.Scope == ScopeAll {
			continue
		}
		symbols, err := e.resolveRuleSymbols(ctx, rule)
		if err != nil {
			continue
		}
		scopeSymbols[rule.ID] = symbols
		for _, symbol := range symbols {
			targets[symbol] = true
		}
	}
	quotes, fetchErr := e.fetchQuotes(ctx, keys(targets))
	if fetchErr != nil && len(quotes) == 0 {
		e.recordRun(ctx, "quotes", startedAt, taskResult{err: fetchErr}, 0)
		return
	}
	snapIndex := e.snapshotIndex()
	var snapshotErr error
	if len(snapIndex) > 0 {
		snapshotErr = e.snapshotRuleError(now, tradeDate)
		if snapshotErr != nil {
			snapIndex = nil
		}
	}
	costs := e.costBySymbol()
	triggered := 0
	for _, rule := range rules {
		if !quoteRules[rule.Type] || rule.Scope == ScopeAll {
			continue
		}
		for _, symbol := range scopeSymbols[rule.ID] {
			row, rowOK := snapIndex[symbol]
			quote, quoteOK := quotes[symbol]
			rowOK = rowOK && (row.TradeDate == "" || row.TradeDate == tradeDate)
			quoteOK = quoteOK && quoteUsableForRule(quote, now, tradeDate)
			if !rowOK && !quoteOK {
				continue
			}
			observation := observationFromQuote(quote, quoteOK, row, rowOK, costs[symbol])
			triggered += e.evaluateRule(ctx, rule, observation, symbol, quoteName(quote, quoteOK, row, rowOK), now, tradeDate)
		}
	}
	e.mu.Lock()
	e.status.LastQuoteAt = now.Format(time.RFC3339)
	e.mu.Unlock()
	e.recordRun(ctx, "quotes", startedAt, taskResult{err: errors.Join(fetchErr, snapshotErr)}, triggered)
}

func quoteUsableForRule(quote foundation.Quote, now time.Time, tradeDate string) bool {
	return quote.Price > 0 && !quote.Meta.Stale && !quote.Meta.CarryForward &&
		(quote.Meta.TradeDate == "" || quote.Meta.TradeDate == tradeDate) &&
		(quote.TradeTime.IsZero() || quote.TradeTime.In(foundation.AStockLocation).Format("2006-01-02") == tradeDate) &&
		(quote.Meta.FetchedAt.IsZero() || now.Sub(quote.Meta.FetchedAt) <= time.Minute)
}

func quoteName(quote foundation.Quote, quoteOK bool, row foundation.MarketSnapshotRow, rowOK bool) string {
	if quoteOK && strings.TrimSpace(quote.Name) != "" {
		return quote.Name
	}
	if rowOK {
		return row.Name
	}
	return ""
}

// observationFromQuote 装一份实时行情观测。量比与换手不是逐秒字段，
// 快照行存在时以它兜底——晚 15 秒远比「没有值」好。
func observationFromQuote(quote foundation.Quote, haveQuote bool, row foundation.MarketSnapshotRow, haveRow bool, cost float64) Observation {
	observation := Observation{}
	if haveRow {
		observation.SnapshotRow = row
		observation.Price = row.Price
		observation.ChangePercent = row.ChangePercent
		observation.VolumeRatio = row.VolumeRatio
		observation.TurnoverRate = row.TurnoverRate
		observation.Amount = row.Amount
	}
	if haveQuote && quote.Price > 0 {
		observation.Price = quote.Price
		observation.ChangePercent = quote.ChangePercent
		observation.SnapshotRow.Price = quote.Price
		observation.SnapshotRow.ChangePercent = quote.ChangePercent
		observation.SnapshotRow.High = quote.High
		observation.SnapshotRow.Low = quote.Low
		observation.SnapshotRow.PreviousClose = quote.PreviousClose
		if quote.Amount > 0 {
			observation.Amount = quote.Amount
		}
		if strings.TrimSpace(observation.SnapshotRow.Symbol) == "" && quote.Symbol != "" {
			observation.SnapshotRow.Symbol = quote.Symbol
			observation.SnapshotRow.Name = quote.Name
			observation.SnapshotRow.Board = foundation.DetectBoard(quote.Symbol, quote.Name)
			observation.SnapshotRow.IsST = foundation.IsST(quote.Name)
		}
	}
	if cost > 0 {
		observation.HaveCost = true
		observation.CostPrice = cost
	}
	return observation
}

func (e *Engine) fetchQuotes(ctx context.Context, symbols []string) (map[string]foundation.Quote, error) {
	quotes := map[string]foundation.Quote{}
	if e.realtime == nil || len(symbols) == 0 {
		return quotes, nil
	}
	var lastErr error
	for start := 0; start < len(symbols); start += 200 {
		end := start + 200
		if end > len(symbols) {
			end = len(symbols)
		}
		batch, err := e.realtime.Realtime(ctx, symbols[start:end])
		if err != nil {
			lastErr = err
			continue
		}
		for _, quote := range batch {
			quotes[quote.Symbol] = quote
		}
	}
	return quotes, lastErr
}

func keys(set map[string]bool) []string {
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// realtimeQuoteIndex 是关注仓行情的兜底通道：全市场快照缺这些标的时
// 才会调用。前端每次轮询都拉 Watchlist，这里压一层 15 秒 TTL 缓存，
// 失败时短时间回退旧缓存，避免周末把实时源打挂。
func (e *Engine) realtimeQuoteIndex(ctx context.Context, symbols []string) map[string]foundation.Quote {
	if e.realtime == nil || len(symbols) == 0 {
		return nil
	}
	now := e.now()
	result := make(map[string]foundation.Quote, len(symbols))
	missing := make([]string, 0, len(symbols))
	e.mu.Lock()
	for _, symbol := range symbols {
		at := e.watchQuoteTimes[symbol]
		if at.IsZero() {
			at = e.watchQuotesAt
		}
		if quote, ok := e.watchQuotes[symbol]; ok && now.Sub(at) < 15*time.Second {
			result[symbol] = quote
		} else {
			missing = append(missing, symbol)
		}
	}
	e.mu.Unlock()
	if len(missing) == 0 {
		return result
	}

	quotes, err := e.fetchQuotes(ctx, missing)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.watchQuotes == nil {
		e.watchQuotes = map[string]foundation.Quote{}
	}
	if e.watchQuoteTimes == nil {
		e.watchQuoteTimes = map[string]time.Time{}
	}
	for _, symbol := range missing {
		if quote, ok := quotes[symbol]; ok {
			result[symbol] = quote
			e.watchQuotes[symbol], e.watchQuoteTimes[symbol] = quote, now
		} else if err != nil {
			at := e.watchQuoteTimes[symbol]
			if at.IsZero() {
				at = e.watchQuotesAt
			}
			if quote, ok := e.watchQuotes[symbol]; ok && now.Sub(at) < 60*time.Second {
				result[symbol] = quote
			}
		}
	}
	return result
}

// resolveRuleSymbols 把 scope 翻译成代码集合。positions/watchlist 每次重读：
// 两份列表都是用户可改的，缓存会引入「买了却不在监控里」的危险不一致。
func (e *Engine) resolveRuleSymbols(ctx context.Context, rule Rule) ([]string, error) {
	switch rule.Scope {
	case ScopeSymbols:
		return rule.Symbols, nil
	case ScopePositions, ScopeInspection:
		// 巡检持仓与外部持仓源合并为一个「持仓」：两份清单取并集去重
		// （巡检在前），inspection 作为历史规则的别名继续生效。
		symbols := make([]string, 0)
		for _, target := range e.inspectionTargets(ctx) {
			if target.Symbol != "" {
				symbols = append(symbols, target.Symbol)
			}
		}
		if e.portfolio != nil {
			if holdings, err := e.portfolio.Holdings(ctx); err == nil {
				seen := map[string]bool{}
				for _, symbol := range symbols {
					seen[symbol] = true
				}
				for _, holding := range holdings {
					if holding.Quantity > 0 && !seen[holding.Symbol] {
						symbols = append(symbols, holding.Symbol)
						seen[holding.Symbol] = true
					}
				}
			}
		}
		return symbols, nil
	case ScopeWatchlist:
		if e.store == nil {
			return nil, nil
		}
		watch, err := e.store.ListWatchlist(ctx)
		if err != nil {
			return nil, err
		}
		symbols := make([]string, 0, len(watch))
		for _, entry := range watch {
			symbols = append(symbols, entry.Symbol)
		}
		return symbols, nil
	default:
		return nil, nil
	}
}

// costBySymbol 是所有启用批次按数量的加权平均成本。
func (e *Engine) costBySymbol() map[string]float64 {
	lots, err := e.store.ListLots(context.Background(), "")
	if err != nil {
		return nil
	}
	weights := map[string]float64{}
	totalCost := map[string]float64{}
	for _, lot := range lots {
		if !lot.Enabled || lot.Quantity <= 0 {
			continue
		}
		weights[lot.Symbol] += float64(lot.Quantity)
		totalCost[lot.Symbol] += float64(lot.Quantity) * lot.CostPrice
	}
	costs := map[string]float64{}
	for symbol, weight := range weights {
		if weight > 0 {
			costs[symbol] = totalCost[symbol] / weight
		}
	}
	return costs
}

// scanRowLimit 是 scope=all 规则单轮最多评估的行数：没写 MaxSymbols 的
// 规则按成交额取流动性前 800 只，防止一条规则把一轮扫描拖慢半秒。
const scanRowLimit = 800

// runSnapshotTask：15 秒档。scope=all 的规则与大盘规则都吃全市场快照。
func (e *Engine) runSnapshotTask(ctx context.Context, now time.Time, tradeDate string) {
	startedAt := now
	rows, err := e.snapshotRows(ctx)
	if err == nil {
		err = e.snapshotRuleError(e.now(), tradeDate)
	}
	if err != nil {
		e.recordRun(ctx, "snapshot", startedAt, taskResult{err: err}, 0)
		return
	}
	currentRows := make([]foundation.MarketSnapshotRow, 0, len(rows))
	for _, row := range rows {
		if row.TradeDate == "" || row.TradeDate == tradeDate {
			currentRows = append(currentRows, row)
		}
	}
	if len(currentRows) == 0 {
		e.recordRun(ctx, "snapshot", startedAt, taskResult{err: errors.New("快照没有可用于当前交易日的行情，暂停本轮预警评估")}, 0)
		return
	}
	rows = currentRows
	rules, err := e.store.ListRules(ctx, "", true)
	if err != nil {
		e.recordRun(ctx, "snapshot", startedAt, taskResult{err: err}, 0)
		return
	}
	stats := e.marketStats(ctx, rows)
	if err := e.snapshotRuleError(e.now(), tradeDate); err != nil {
		e.recordRun(ctx, "snapshot", startedAt, taskResult{err: err}, 0)
		return
	}
	costs := e.costBySymbol()
	triggered, evaluated := 0, 0
	for _, rule := range rules {
		switch rule.Type {
		case RuleTypeMarket:
			observation := Observation{Market: stats}
			triggered += e.evaluateRule(ctx, rule, observation, "", "大盘", now, tradeDate)
			evaluated++
		case RuleTypePrice, RuleTypeVolume, RuleTypeAbnormal:
			if rule.Scope != ScopeAll {
				continue
			}
			scanned := 0
			limit := rule.Filters.MaxSymbols
			if limit <= 0 || limit > scanRowLimit {
				limit = scanRowLimit
			}
			for _, row := range rows {
				if !rule.Filters.accepts(row) || (row.TradeDate != "" && row.TradeDate != tradeDate) {
					continue
				}
				if scanned >= limit {
					break
				}
				scanned++
				observation := FromSnapshotRow(row)
				if cost, ok := costs[row.Symbol]; ok {
					observation.HaveCost, observation.CostPrice = true, cost
				}
				e.mergeHighLow(row.Symbol, &observation)
				evaluated++
				triggered += e.evaluateRule(ctx, rule, observation, row.Symbol, row.Name, now, tradeDate)
			}
		}
	}
	e.recordRun(ctx, "snapshot", startedAt, taskResult{evaluated: evaluated}, triggered)
}

// publishLiveTicks 把当前全市场快照的价与涨幅广播到实时价总线。
// 同时以完整代码（002100.SZ）与 6 位纯数字（002100）两种 key 写入，
// 让前端无论持有哪种形式的 symbol 都能命中。
func (e *Engine) publishLiveTicks() {
	if e.liveBus == nil {
		return
	}
	e.mu.Lock()
	bySymbol := e.snapBySymbol
	fetchedAt := e.snapFetchedAt
	e.mu.Unlock()
	if len(bySymbol) == 0 || fetchedAt.IsZero() {
		return
	}
	ticks := make([]LiveTick, 0, len(bySymbol)*2)
	ts := fetchedAt.UnixMilli()
	for _, row := range bySymbol {
		if row.Price <= 0 {
			continue
		}
		ticks = append(ticks, LiveTick{Symbol: row.Symbol, Price: round2(row.Price), ChangePercent: round2(row.ChangePercent), FetchedAt: ts})
		if code := sixDigitCode(row.Symbol); code != "" && code != row.Symbol {
			ticks = append(ticks, LiveTick{Symbol: code, Price: round2(row.Price), ChangePercent: round2(row.ChangePercent), FetchedAt: ts})
		}
	}
	e.liveBus.publish(ticks)
}

// LiveTicks 返回实时价总线里属于 symbols 的子集；symbols 为空表示全部。
func (e *Engine) LiveTicks(symbols map[string]bool) []LiveTick {
	return e.liveBus.snapshot(symbols)
}

// SubscribeLiveTicks 订阅实时价总线，返回信号 channel 与退订函数。
func (e *Engine) SubscribeLiveTicks() (<-chan struct{}, func()) {
	return e.liveBus.subscribe()
}

func (f RuleFilters) accepts(row foundation.MarketSnapshotRow) bool {
	if row.Suspended || row.Price <= 0 {
		return false
	}
	if len(f.Boards) > 0 && !containsString(f.Boards, row.Board) {
		return false
	}
	if f.ExcludeST && row.IsST {
		return false
	}
	if f.MinTurnover > 0 && row.TurnoverRate < f.MinTurnover {
		return false
	}
	if f.MinAmount > 0 && row.Amount < f.MinAmount {
		return false
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (e *Engine) marketStats(ctx context.Context, rows []foundation.MarketSnapshotRow) MarketStats {
	stats := MarketStats{HaveBreadth: true}
	for _, row := range rows {
		if row.Suspended || row.Price <= 0 {
			continue
		}
		switch {
		case row.ChangePercent > 0:
			stats.UpCount++
		case row.ChangePercent < 0:
			stats.DownCount++
		}
		if limitState(row) == 1 {
			stats.LimitUpCount++
		}
	}
	// 四个宽基指数并行取：各自有 5 分钟缓存，首轮多三次请求，之后直接命中。
	now := e.now()
	quotes := make([]IndexQuote, len(monitorIndexQuotes))
	var wg sync.WaitGroup
	for index, item := range monitorIndexQuotes {
		wg.Add(1)
		go func(index int, item IndexQuote) {
			defer wg.Done()
			closes, ok := e.indexCloses(ctx, item.ID, now)
			if !ok {
				return
			}
			item.ChangePct = indexChangePct(closes)
			quotes[index] = item
		}(index, item)
	}
	wg.Wait()
	for _, quote := range quotes {
		if quote.ID != "" {
			stats.Indexes = append(stats.Indexes, quote)
		}
	}
	if closes, ok := e.indexCloses(ctx, "sse", now); ok {
		stats.IndexChangePct = indexChangePct(closes)
		stats.HaveIndexes = true
	}
	return stats
}

// monitorIndexQuotes 是「今日市场」并列展示的顺序：沪、深、创业板、科创板。
var monitorIndexQuotes = []IndexQuote{
	{ID: "sse", Name: "上证指数"},
	{ID: "szse", Name: "深证成指"},
	{ID: "chinext", Name: "创业板指"},
	{ID: "star50", Name: "科创50"},
}

// indexChangePct 取指数序列的当日涨跌：最后一根是今天、倒数第二根是昨收。
func indexChangePct(closes []float64) float64 {
	if len(closes) < 2 {
		return 0
	}
	last, previous := closes[len(closes)-1], closes[len(closes)-2]
	if previous <= 0 {
		return 0
	}
	return (last - previous) / previous * 100
}

func (e *Engine) indexCloses(ctx context.Context, id string, now time.Time) ([]float64, bool) {
	e.mu.Lock()
	cached, ok := e.indexSeries[id]
	e.mu.Unlock()
	if ok && !indexCacheStale(cached, now) && len(cached.closes) > 1 {
		return cached.closes, true
	}
	if e.index == nil {
		return nil, false
	}
	series, err := e.index.MarketIndexSeries(ctx, id, "daily", 40)
	if err != nil || len(series.Lines) < 2 {
		return nil, false
	}
	closes := make([]float64, 0, len(series.Lines))
	for _, bar := range series.Lines {
		closes = append(closes, bar.Close)
	}
	e.mu.Lock()
	e.indexSeries[id] = cachedIndexSeries{closes: closes, bars: series.Lines, FetchedAt: now}
	e.mu.Unlock()
	return closes, true
}

// indexBars 返回带日期的指数日线，供逐日明细按交易日对齐。
// 复用 indexCloses 的缓存与拉取路径，避免同一份序列存两份。
func (e *Engine) indexBars(ctx context.Context, id string, now time.Time) ([]foundation.KLine, bool) {
	if _, ok := e.indexCloses(ctx, id, now); !ok {
		return nil, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	cached := e.indexSeries[id]
	return cached.bars, true
}

// snapshotRows 取全市场快照，10 秒缓存；并发请求通过 snapFetching 单飞。
// 失败且有旧缓存时返回旧数据（不算错误），页面会看到 snapError 提示。
func (e *Engine) snapshotRows(ctx context.Context) ([]foundation.MarketSnapshotRow, error) {
	return e.snapshotRowsWithTTL(ctx, 10*time.Second)
}

func (e *Engine) snapshotRowsWithTTL(ctx context.Context, ttl time.Duration) ([]foundation.MarketSnapshotRow, error) {
	e.mu.Lock()
	if len(e.snapRows) > 0 && e.now().Sub(e.snapFetchedAt) < ttl {
		rows := e.snapRows
		e.mu.Unlock()
		return rows, nil
	}
	if e.snapFetching {
		done := e.fetchDone
		e.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.snapRows, errorOrNil(e.snapError)
	}
	provider := e.snapshot
	e.snapFetching = true
	e.fetchDone = make(chan struct{})
	e.mu.Unlock()

	rows, meta, fetchErr := ctxSnapshot(ctx, provider)
	e.mu.Lock()
	e.snapFetching = false
	close(e.fetchDone)
	defer func() {
		e.mu.Unlock()
		if fetchErr == nil {
			// 懒加载和后台刷新走同一出口；推送不等待慢速规则评估。
			e.publishLiveTicks()
		}
	}()
	switch {
	case fetchErr != nil && len(rows) == 0 && len(e.snapRows) == 0:
		e.snapError = fetchErr.Error()
		return nil, fmt.Errorf("全市场快照不可用: %w", fetchErr)
	case fetchErr != nil:
		e.snapError = fetchErr.Error()
	default:
		e.snapError = ""
		e.status.SnapshotSource = meta.Source
		e.status.SnapshotStale = meta.Stale || meta.CarryForward
		e.status.SnapshotWarning = meta.FallbackReason
		e.snapTradeDate = meta.TradeDate
		e.snapSourceAt = meta.FetchedAt
		e.snapRows = rows
		e.snapBySymbol = make(map[string]foundation.MarketSnapshotRow, len(rows))
		for _, row := range rows {
			e.snapBySymbol[row.Symbol] = row
		}
		e.snapFetchedAt = e.now()
	}
	e.status.LastSnapshotAt = e.snapFetchedAt.Format(time.RFC3339)
	e.status.SnapshotCount = len(e.snapRows)
	e.status.SnapshotError = e.snapError
	return e.snapRows, nil
}

// 展示允许回退旧缓存，产生新预警必须使用有效的当日行情。
func (e *Engine) snapshotRuleError(now time.Time, tradeDate string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.snapError != "" {
		return fmt.Errorf("监控行情不可用: %s", e.snapError)
	}
	if len(e.snapRows) == 0 || e.snapFetchedAt.IsZero() || now.Sub(e.snapFetchedAt) > time.Minute ||
		(!e.snapSourceAt.IsZero() && now.Sub(e.snapSourceAt) > time.Minute) ||
		e.status.SnapshotStale || (e.snapTradeDate != "" && e.snapTradeDate != tradeDate) {
		return errors.New("监控行情未就绪或已过期，暂停本轮预警评估")
	}
	return nil
}

// ctxSnapshot 把 provider 调用收敛成一个可判 nil 的入口。
func ctxSnapshot(ctx context.Context, provider SnapshotProvider) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	if provider == nil {
		return nil, foundation.SourceMeta{}, errors.New("未配置全市场行情源")
	}
	return provider.MarketSnapshot(ctx)
}

func (e *Engine) snapshotIndex() map[string]foundation.MarketSnapshotRow {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapBySymbol
}

func errorOrNil(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return errors.New(text)
}

func (e *Engine) mergeHighLow(symbol string, observation *Observation) {
	e.mu.Lock()
	cached, ok := e.highLow[symbol]
	e.mu.Unlock()
	if ok {
		observation.HaveHighLow = true
		observation.NewHigh60 = cached.High
		observation.NewLow60 = cached.Low
	}
}

func (e *Engine) mergeDeviation(symbol string, observation *Observation) {
	e.mu.Lock()
	cached, ok := e.deviations[symbol]
	e.mu.Unlock()
	if ok && !cached.Stale {
		observation.HaveDeviation = cached.Available == [3]bool{} || cached.Available == [3]bool{true, true, true}
		observation.DeviationAvailable = cached.Available
		observation.Deviation3 = cached.Dev3
		observation.Deviation10 = cached.Dev10
		observation.Deviation30 = cached.Dev30
	}
}

// mergeSignals 把技术信号并入观测。优先用候选池缓存（同一交易日
// 日线不变）；缓存没有就现算一份并回填——试算接口走这条路，
// 保证引擎还没跑到候选池时 signal 规则也能给出答案。
func (e *Engine) mergeSignals(ctx context.Context, symbol string, observation *Observation) {
	today := e.now().In(foundation.AStockLocation).Format("2006-01-02")
	e.mu.Lock()
	cached, ok := e.signals[symbol]
	e.mu.Unlock()
	if ok && cached.tradeDate == today {
		observation.HaveSignals = true
		observation.Signals = cached.flags
		return
	}
	if e.kline == nil {
		return
	}
	bars, ok := e.dailyBars(ctx, symbol, 120, e.now())
	if !ok {
		return
	}
	flags := computeSignals(bars)
	if len(flags) == 0 {
		return
	}
	e.mu.Lock()
	e.signals[symbol] = signalResult{flags: flags, tradeDate: today}
	e.mu.Unlock()
	observation.HaveSignals = true
	observation.Signals = flags
}

// runDateRules：到期提醒。按交易日一天一次，进程启动时也跑一次。
func (e *Engine) runDateRules(ctx context.Context, now time.Time) {
	_, tradeDate, _ := sessionState(now)
	if tradeDate == "" {
		return
	}
	rules, err := e.store.ListRules(ctx, RuleTypeDate, true)
	if err != nil {
		return
	}
	today, _ := time.ParseInLocation("2006-01-02", tradeDate, foundation.AStockLocation)
	for _, rule := range rules {
		if rule.RemindDate == "" {
			continue
		}
		deadline, err := time.ParseInLocation("2006-01-02", rule.RemindDate, foundation.AStockLocation)
		if err != nil {
			continue
		}
		days := int(deadline.Sub(today).Hours() / 24)
		if days < 0 {
			days = 0
		}
		condition := Condition{Field: "days_to_remind", Op: OpLTE, Value: float64(maxInt(rule.LeadDays, 0))}
		message := fmt.Sprintf("到期提醒 %s（还剩 %d 天）", rule.RemindDate, days)
		if days == 0 {
			message = fmt.Sprintf("今天就是提醒日（%s）", rule.RemindDate)
		}
		symbol := lotSymbol(ctx, e.store, rule.LotID)
		name := ""
		if lot, ok, _ := e.store.GetLot(ctx, rule.LotID); ok {
			name = lot.Name
		}
		event := e.prepareEvent(Event{
			RuleID: rule.ID, RuleName: rule.Name, RuleType: rule.Type, Symbol: symbol,
			Name: name, Severity: rule.Severity, TriggerKind: "lot_due", Message: message,
			TradeDate: tradeDate,
			Detail:    map[string]any{"remind_date": rule.RemindDate, "days": days, "lead_days": rule.LeadDays},
		})
		allowed, err := e.store.observeEvent(ctx, rule, rule.LotID, condition, float64(days), now, tradeDate, true, &event)
		if err != nil {
			e.recordRun(ctx, "date", now, taskResult{err: err}, 0)
		} else if allowed {
			e.deliverEvent(ctx, event)
		}
	}
}

func maxInt(value, alternative int) int {
	if value >= alternative {
		return value
	}
	return alternative
}

func lotSymbol(ctx context.Context, store *Store, lotID string) string {
	if lot, ok, err := store.GetLot(ctx, lotID); err == nil && ok {
		return lot.Symbol
	}
	return ""
}

// deviationIndexID 返回标的应对比的指数。北交所暂无可用指数序列，
// 对齐不了任何指数就说明口径失真，直接不参与偏离值评估。
// deviationIndexID 取交易所异动口径的基准指数：沪主板=上证A股指数、
// 深主板=深证A股指数、创业板=创业板综指、科创板=科创50、北交所=北证50。
// 注意不能用上证指数/深证成指：成分不同，深市标的的偏离值会和公告对不上。
func deviationIndexID(board, symbol string) string {
	switch board {
	case foundation.BoardChiNext:
		return "chinext_comp"
	case foundation.BoardSTAR:
		return "star50"
	case foundation.BoardBSE:
		return "bse50"
	case foundation.BoardMain:
		if strings.HasPrefix(symbol, "6") {
			return "sse_a"
		}
		return "szse_a"
	default:
		return ""
	}
}

// DeviationThresholds 是各板块 3 日偏离值触发阈值（交易所异常波动口径）。
// 长窗口的分板块阈值由 abnormalLongThresholds 提供。
var DeviationThresholds = map[string]float64{
	foundation.BoardMain:    20,
	foundation.BoardChiNext: 30,
	foundation.BoardSTAR:    30,
	foundation.BoardBSE:     40,
}

func DeviationThreshold(board string) float64 {
	if value, ok := DeviationThresholds[board]; ok {
		return value
	}
	return 20
}

// candidateLimit 是一轮偏离计算最多拉日线的数量。
const candidateLimit = 400

type candidateResults struct {
	dev map[string]deviationResult
	// abnormal 保留每个窗口的可用性，规则只依赖自己用到的窗口；dev 保留完整窗口集合。
	abnormal map[string]abnormalDeviationResult
	highLow  map[string]highLowResult
	signals  map[string]signalResult
	// quote 优先吃全市场快照实时价（与智能雷达同口径）；快照缺失时回落日线末两根兜底，
	// 保证上游抖动时持仓/关注仓这些已知标的仍能算出偏离值并落到页面上。
	quote map[string]candidateQuote
}

type abnormalDeviationResult struct {
	deviationResult
	Available [3]bool
}

type candidateQuote struct {
	Price         float64
	ChangePercent float64
}

// signalResult 是一只标的"今天发生了哪些技术信号"。按交易日缓存，
// 同一天内不重复计算。
type signalResult struct {
	flags     map[string]bool
	tradeDate string
}

// runDeviationTask：算候选池的偏离值与 60 日高低点并评估相应规则。
// 结果进缓存，快照任务与页面视图共用，不再单独拉数据。
func (e *Engine) runDeviationTask(ctx context.Context, now time.Time, tradeDate string) {
	startedAt := now
	rows, err := e.snapshotRows(ctx)
	if err == nil {
		err = e.snapshotRuleError(e.now(), tradeDate)
	}
	if err != nil {
		e.recordRun(ctx, "deviation", startedAt, taskResult{err: err}, 0)
		return
	}
	candidates := e.deviationCandidates(ctx, rows)
	// 技术信号规则的标的也要进候选池：它可能不在偏离/新高低标的里。
	// 全市场信号规则按快照 + Filters 展开，其余作用域仍是显式清单。
	if signalSymbols := e.signalRuleSymbols(ctx, rows); len(signalSymbols) > 0 {
		seen := map[string]bool{}
		for _, symbol := range candidates {
			seen[symbol] = true
		}
		for _, symbol := range signalSymbols {
			if !seen[symbol] {
				candidates = append(candidates, symbol)
			}
		}
	}
	if len(candidates) == 0 {
		e.recordRun(ctx, "deviation", startedAt, taskResult{}, 0)
		return
	}
	results := e.computeCandidates(ctx, candidates, now)
	for symbol, partial := range results.abnormal {
		result := partial.deviationResult
		result.Available = partial.Available
		e.mu.Lock()
		result.FetchedAt = now
		e.deviations[symbol] = result
		e.mu.Unlock()
	}
	for symbol, result := range results.highLow {
		e.mu.Lock()
		e.highLow[symbol] = highLowResult{High: result.High, Low: result.Low, FetchedAt: now}
		e.mu.Unlock()
	}
	for symbol, result := range results.signals {
		e.mu.Lock()
		e.signals[symbol] = result
		e.mu.Unlock()
	}
	deviationRules, err := e.store.ListRules(ctx, RuleTypeDeviation, true)
	if err != nil {
		deviationRules = nil
	}
	highLowRules := e.highLowRules(ctx)
	signalRules, err := e.store.ListRules(ctx, RuleTypeSignal, true)
	if err != nil {
		signalRules = nil
	}
	triggered, evaluated := 0, 0
	evaluate := func(rule Rule, symbol string, prepare func(*Observation)) {
		if e.snapshotRuleError(e.now(), tradeDate) != nil {
			return
		}
		covered, err := e.ruleCoversSymbol(ctx, rule, symbol)
		if err != nil || !covered {
			return
		}
		row, ok := e.snapshotSymbolRow(symbol)
		if !ok || (row.TradeDate != "" && row.TradeDate != tradeDate) {
			return
		}
		observation := FromSnapshotRow(row)
		prepare(&observation)
		evaluated++
		triggered += e.evaluateRule(ctx, rule, observation, symbol, row.Name, now, tradeDate)
	}
	for symbol, dev := range results.abnormal {
		if dev.Stale {
			continue
		}
		for _, rule := range deviationRules {
			evaluate(rule, symbol, func(observation *Observation) {
				observation.DeviationAvailable = dev.Available
				observation.Deviation3, observation.Deviation10, observation.Deviation30 = dev.Dev3, dev.Dev10, dev.Dev30
			})
		}
	}
	for symbol, flags := range results.highLow {
		for _, rule := range highLowRules {
			evaluate(rule, symbol, func(observation *Observation) {
				observation.HaveHighLow = true
				observation.NewHigh60, observation.NewLow60 = flags.High, flags.Low
			})
		}
	}
	for symbol, result := range results.signals {
		for _, rule := range signalRules {
			// 只评估覆盖该标的的规则，避免一条规则被全池重复评估。
			covered, err := e.ruleCoversSymbol(ctx, rule, symbol)
			if err != nil || !covered {
				continue
			}
			evaluate(rule, symbol, func(observation *Observation) {
				observation.HaveSignals = true
				observation.Signals = result.flags
			})
		}
	}
	e.recordRun(ctx, "deviation", startedAt, taskResult{evaluated: evaluated}, triggered)
}

// signalRuleSymbols 汇总全部启用 signal 规则解析出的标的并集。
// scope=all 的规则没有显式标的：按全市场快照 + Filters 收窄，
// 与 runSnapshotTask 同口径（先过滤再按 MaxSymbols/scanRowLimit 截断）。
func (e *Engine) signalRuleSymbols(ctx context.Context, rows []foundation.MarketSnapshotRow) []string {
	rules, err := e.store.ListRules(ctx, RuleTypeSignal, true)
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, rule := range rules {
		if rule.Scope == ScopeAll {
			limit := rule.Filters.MaxSymbols
			if limit <= 0 || limit > scanRowLimit {
				limit = scanRowLimit
			}
			added := 0
			for _, row := range rows {
				if added >= limit {
					break
				}
				if !rule.Filters.accepts(row) {
					continue
				}
				added++
				set[row.Symbol] = true
			}
			continue
		}
		symbols, err := e.resolveRuleSymbols(ctx, rule)
		if err != nil {
			continue
		}
		for _, symbol := range symbols {
			set[symbol] = true
		}
	}
	return keys(set)
}

// ruleCoversSymbol 判断一条规则的作用域是否覆盖某只标的。
// scope=all 没有标的清单，覆盖率由 Filters 决定：先过板块/ST/成交额门槛，
// 再谈这条规则要不要评估它。
func (e *Engine) ruleCoversSymbol(ctx context.Context, rule Rule, symbol string) (bool, error) {
	if rule.Scope == ScopeAll {
		row, ok := e.snapshotSymbolRow(symbol)
		if !ok {
			return false, nil
		}
		return rule.Filters.accepts(row), nil
	}
	symbols, err := e.resolveRuleSymbols(ctx, rule)
	if err != nil {
		return false, err
	}
	for _, item := range symbols {
		if item == symbol {
			return true, nil
		}
	}
	return false, nil
}

// highLowRules 取条件里含 high_60d/low_60d 的异动规则。
func (e *Engine) highLowRules(ctx context.Context) []Rule {
	all, err := e.store.ListRules(ctx, RuleTypeAbnormal, true)
	if err != nil {
		return nil
	}
	rules := make([]Rule, 0, len(all))
	for _, rule := range all {
		for _, condition := range rule.Conditions {
			if condition.Field == "high_60d" || condition.Field == "low_60d" {
				rules = append(rules, rule)
				break
			}
		}
	}
	return rules
}

func (e *Engine) snapshotSymbolRow(symbol string) (foundation.MarketSnapshotRow, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	row, ok := e.snapBySymbol[symbol]
	return row, ok
}

// deviationCandidates 构造候选池：positions + watchlist + 规则指定 +
// 当日涨幅前 200 / 跌幅前 100，去重后上限 candidateLimit。
func (e *Engine) deviationCandidates(ctx context.Context, rows []foundation.MarketSnapshotRow) []string {
	candidates := map[string]bool{}
	priority := e.knownSymbols(ctx)
	add := func(symbol string) {
		if strings.TrimSpace(symbol) != "" {
			candidates[symbol] = true
		}
	}
	for _, symbol := range priority {
		add(symbol)
	}
	if e.portfolio != nil {
		if holdings, err := e.portfolio.Holdings(ctx); err == nil {
			for _, holding := range holdings {
				if holding.Quantity > 0 {
					add(holding.Symbol)
				}
			}
		}
		if watch, err := e.portfolio.WatchSymbols(ctx); err == nil {
			for _, symbol := range watch {
				add(symbol)
			}
		}
	}
	for _, symbol := range e.enabledSymbols(ctx) {
		add(symbol)
	}
	byChange := make([]foundation.MarketSnapshotRow, 0, len(rows))
	for _, row := range rows {
		if row.Suspended || row.Price <= 0 || row.PreviousClose <= 0 {
			continue
		}
		byChange = append(byChange, row)
	}
	sort.SliceStable(byChange, func(i, j int) bool { return byChange[i].ChangePercent > byChange[j].ChangePercent })
	for index, row := range byChange {
		if index >= 200 {
			break
		}
		add(row.Symbol)
	}
	sort.SliceStable(byChange, func(i, j int) bool { return byChange[i].ChangePercent < byChange[j].ChangePercent })
	for index, row := range byChange {
		if index >= 100 {
			break
		}
		add(row.Symbol)
	}
	// 用户明确指定的范围优先且不被自动候选截断。
	symbols := append([]string(nil), priority...)
	for _, symbol := range priority {
		delete(candidates, symbol)
	}
	for _, symbol := range keys(candidates) {
		if len(symbols) >= candidateLimit {
			break
		}
		symbols = append(symbols, symbol)
	}
	return symbols
}

// enabledSymbols 汇总所有启用规则的显式标的。
func (e *Engine) enabledSymbols(ctx context.Context) []string {
	rules, err := e.store.ListRules(ctx, "", true)
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, rule := range rules {
		for _, symbol := range rule.Symbols {
			set[symbol] = true
		}
	}
	return keys(set)
}

// computeCandidates 并行计算候选池的偏离值与 60 日高低点。
// 并发 8 路；失败安静地少算一只——候选池的价值在于覆盖常见触发位置，
// 单只失败不值得让整轮失败。
func (e *Engine) computeCandidates(ctx context.Context, symbols []string, now time.Time) candidateResults {
	results := candidateResults{
		dev:      map[string]deviationResult{},
		abnormal: map[string]abnormalDeviationResult{},
		highLow:  map[string]highLowResult{},
		signals:  map[string]signalResult{},
		quote:    map[string]candidateQuote{},
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	pages := make(chan string)
	workers := 8
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for symbol := range pages {
				// 120 根同时喂饱 MA60 交叉（需 61 根收盘价）、布林与 MACD；
				// 60 日高低只取尾部，不受影响。
				bars, ok := e.dailyBars(ctx, symbol, 120, now)
				if !ok {
					continue
				}
				if high, low, have := highLowFlags(bars); have {
					mu.Lock()
					results.highLow[symbol] = highLowResult{High: high, Low: low}
					mu.Unlock()
				}
				if flags := computeSignals(bars); len(flags) > 0 {
					mu.Lock()
					results.signals[symbol] = signalResult{flags: flags, tradeDate: now.In(foundation.AStockLocation).Format("2006-01-02")}
					mu.Unlock()
				}
				row, inSnapshot := e.snapshotSymbolRow(symbol)
				board := row.Board
				if !inSnapshot {
					// 快照缺失时按代码段推板块，基准指数才不会跟着丢。
					board = foundation.DetectBoard(symbol, "")
				}
				// 现价/今日涨幅优先吃全市场快照的实时价（10 秒级），与智能雷达同口径；
				// 快照缺失才回落日线末根收盘价。偏离值的「今日」那一根也用实时价覆盖，
				// 否则日线 today-bar 走 push2his 盘中滞后、最快 60 秒才重拉，
				// 页面现价在跳、偏离值不动，看起来就像「异动预警没接实时行情」。
				if inSnapshot && row.Price > 0 {
					mu.Lock()
					results.quote[symbol] = candidateQuote{
						Price:         round2(row.Price),
						ChangePercent: round2(row.ChangePercent),
					}
					mu.Unlock()
				} else if len(bars) >= 2 && bars[len(bars)-2].Close > 0 {
					lastClose := bars[len(bars)-1].Close
					mu.Lock()
					results.quote[symbol] = candidateQuote{
						Price:         round2(lastClose),
						ChangePercent: round2((lastClose/bars[len(bars)-2].Close - 1) * 100),
					}
					mu.Unlock()
				}
				indexID := deviationIndexID(board, symbol)
				if indexID == "" {
					continue
				}
				// 与单票计算器同口径：指数按交易日对齐到个股日线。
				// 个股停牌时直接按下标配对会把停牌前的 K 线当成 N 日前，偏离值随之失真。
				indexBars, ok := e.indexBars(ctx, indexID, now)
				if !ok {
					continue
				}
				aligned := alignIndexToBars(bars, indexBars)
				// 用实时快照价覆盖日线 today-bar 的收盘价，让偏离值随快照行情跳动。
				// bars 是共享缓存切片，不能直接改末根，复制末根后再覆盖。
				devBars := bars
				if inSnapshot && row.Price > 0 && barDate(bars[len(bars)-1]) == foundation.LatestAStockSession(now) {
					last := bars[len(bars)-1]
					last.Close = row.Price
					devBars = append(bars[:len(bars)-1:len(bars)-1], last)
				}
				dev3, ok3 := deviationWindowDev(devBars, aligned, 3)
				dev10, ok10 := deviationWindowDev(devBars, aligned, 10)
				dev30, ok30 := deviationWindowDev(devBars, aligned, 30)
				if ok3 || ok10 || ok30 {
					bases := abnormalWindowBases(devBars)
					result := deviationResult{Dev3: dev3, Dev10: dev10, Dev30: dev30, Base3: bases[0], Base10: bases[1], Base30: bases[2], FetchedAt: now, DailyAsOf: barDate(devBars[len(devBars)-1]), Stale: barDate(devBars[len(devBars)-1]) != foundation.LatestAStockSession(now)}
					mu.Lock()
					results.abnormal[symbol] = abnormalDeviationResult{deviationResult: result, Available: [3]bool{ok3, ok10, ok30}}
					if ok3 && ok10 && ok30 {
						results.dev[symbol] = result
					}
					mu.Unlock()
				}
			}
		}()
	}
	for _, symbol := range symbols {
		select {
		case pages <- symbol:
		case <-ctx.Done():
			close(pages)
			wg.Wait()
			return results
		}
	}
	close(pages)
	wg.Wait()
	return results
}

// dailyBars 取一只标的的日线并按交易日缓存：历史棒一天内不会变，
// 但最后一根是今天的实时棒，盘中给一个短 TTL 让它跟上行情；
// 收盘后仍按天缓存，不做无谓的重复拉取。
func (e *Engine) dailyBars(ctx context.Context, symbol string, limit int, now time.Time) ([]foundation.KLine, bool) {
	today := now.In(foundation.AStockLocation).Format("2006-01-02")
	e.mu.Lock()
	cached, ok := e.klines[symbol]
	e.mu.Unlock()
	if ok && cached.day == today && len(cached.bars) >= limit && !klineCacheStale(cached, now) {
		return cached.bars, true
	}
	if e.kline == nil {
		return nil, false
	}
	bars, err := e.kline.KLine(ctx, symbol, "daily", limit)
	if err != nil || len(bars) == 0 {
		return nil, false
	}
	e.mu.Lock()
	e.klines[symbol] = klineEntry{bars: bars, day: today, FetchedAt: now}
	e.mu.Unlock()
	return bars, true
}

// klineCacheStale 判断当日日线缓存是否需要盘中刷新。
// 日线缓存按「交易日」整天复用，但序列最后一根是今天的实时棒：
// 集合竞价之后价格一直在动，缓存一旦建立就冻住，会让偏离值 /
// 触发判断停在首次取数的时点——页面上现价在跳、偏离值不动，
// 看起来就像「异动预警没接实时行情」。若首取发生在开盘前，
// 序列里甚至还没有今天这根棒，整天都是昨日口径。
func klineCacheStale(cached klineEntry, now time.Time) bool {
	local := now.In(foundation.AStockLocation)
	if !foundation.IsAStockTradingDay(local) {
		return false
	}
	minute := local.Hour()*60 + local.Minute()
	if minute < 9*60+15 || minute > 15*60+5 {
		return false
	}
	return now.Sub(cached.FetchedAt) >= time.Minute
}

// indexCacheStale 判断基准指数日线缓存是否需要刷新。
// 指数序列绝大多数时间是固定的历史值，只有最后一根是今天的实时棒；
// 交易时段的指数今日点若一直用缓存，偏离值的「市场中性」修正会停在首取时点，
// 与实时快照里的个股现价对不上。交易时段给它一个短 TTL，收盘后按长缓存兜底。
func indexCacheStale(cached cachedIndexSeries, now time.Time) bool {
	age := now.Sub(cached.FetchedAt)
	local := now.In(foundation.AStockLocation)
	if foundation.IsAStockTradingDay(local) {
		minute := local.Hour()*60 + local.Minute()
		if minute >= 9*60+15 && minute <= 15*60+5 {
			return age >= 20*time.Second
		}
	}
	return age >= 30*time.Minute
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

// highLowFlags 从日线判断 60 日新高/新低。今天那根棒不参与比较：
// 「当前价是否突破了自己手中那根棒」没有意义。
func highLowFlags(bars []foundation.KLine) (high bool, low bool, have bool) {
	if len(bars) < 7 {
		return false, false, false
	}
	historic := bars
	today := bars[len(bars)-1].Time.In(foundation.AStockLocation).Format("2006-01-02")
	if today == foundation.LatestAStockSession(time.Now()) {
		historic = bars[:len(bars)-1]
	}
	if len(historic) == 0 {
		return false, false, false
	}
	if len(historic) > 60 {
		historic = historic[len(historic)-60:]
	}
	var maxHigh, minLow float64
	for index, bar := range historic {
		if index == 0 || bar.High > maxHigh {
			maxHigh = bar.High
		}
		if index == 0 || bar.Low < minLow {
			minLow = bar.Low
		}
	}
	if maxHigh <= 0 || minLow <= 0 {
		return false, false, false
	}
	price := bars[len(bars)-1].Close
	return price > maxHigh+priceEpsilon, price < minLow-priceEpsilon, true
}

// ---------------------------------------------------------------- 规则评估与事件派发

// conditionFor 保留命中条件的身份，同字段的止盈和止损不能混用。
func conditionFor(rule Rule, signal Signal) Condition {
	if signal.ConditionIndex >= 0 && signal.ConditionIndex < len(rule.Conditions) {
		condition := rule.Conditions[signal.ConditionIndex]
		if condition.Field == signal.Field {
			return condition
		}
	}
	for _, condition := range rule.Conditions {
		if condition.Field == signal.Field && condition.Value == signal.Threshold {
			return condition
		}
	}
	return Condition{Field: signal.Field, Op: OpGTE}
}

// evaluateRule 返回本轮实际发出的事件数。
func (e *Engine) evaluateRule(ctx context.Context, rule Rule, observation Observation, symbol, name string, now time.Time, tradeDate string) int {
	signals := EvaluateRule(rule, observation)
	byCondition := make(map[int]Signal, len(signals))
	for _, signal := range signals {
		byCondition[signal.ConditionIndex] = signal
	}
	count := 0
	for index, condition := range rule.Conditions {
		value, known := observationValue(observation, condition.Field)
		if !known {
			continue
		}
		signal, eligible := byCondition[index]
		event := e.prepareEvent(e.buildEvent(rule, observation, symbol, name, signal, tradeDate))
		allowed, err := e.store.observeEvent(ctx, rule, symbol, condition, value, now, tradeDate, eligible, &event)
		if err != nil {
			e.recordRun(ctx, "event", now, taskResult{err: err}, 0)
			continue
		}
		if !allowed {
			continue
		}
		e.deliverEvent(ctx, event)
		count++
	}
	return count
}

func (e *Engine) buildEvent(rule Rule, observation Observation, symbol, name string, signal Signal, tradeDate string) Event {
	condition := conditionFor(rule, signal)
	message := eventMessage(rule, symbol, name, signal)
	if rule.Type == RuleTypeSector {
		// 板块事件附带领涨成员：提醒到手就知道这一波是谁在拉。
		// 数据来自快照聚合（零额外请求），题材/成分缺失时自然没有。
		if movers := sectorMoversText(observation.Sector.TopMovers); movers != "" {
			message += "；领涨 " + movers
		}
	}
	return Event{
		RuleID: rule.ID, RuleName: rule.Name, RuleType: rule.Type, Symbol: symbol,
		Name: name, Severity: rule.Severity, TriggerKind: signal.Kind,
		Message: message,
		Detail: map[string]any{
			"field":     signal.Field,
			"value":     signal.Value,
			"threshold": signal.Threshold,
			"op":        condition.Op,
			"logic":     rule.Logic,
		},
		Price:         observation.Price,
		ChangePercent: observation.ChangePercent,
		TradeDate:     tradeDate,
	}
}

// sectorMoversText 把领涨成员拼成 "寒武纪（688256）+9.90%" 形式。
func sectorMoversText(movers []SectorMover) string {
	parts := make([]string, 0, len(movers))
	for _, mover := range movers {
		if mover.Symbol == "" && mover.Name == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s（%s）%+.2f%%", mover.Name, mover.Symbol, mover.ChangePercent))
	}
	return strings.Join(parts, "、")
}

func eventMessage(rule Rule, symbol, name string, signal Signal) string {
	subject := signal.Message
	if symbol != "" {
		display := symbol
		if strings.TrimSpace(name) != "" {
			display = name + "（" + symbol + "）"
		}
		subject = display + "：" + signal.Message
	}
	return rule.Name + " · " + subject
}

// dispatch 决定这条事件的通知路径并落库。
// info 只落库；warning 进摘要窗口；critical 立即外发；静默时段只拦非 critical。
func (e *Engine) dispatch(ctx context.Context, event Event) {
	event = e.prepareEvent(event)
	id, err := e.store.AppendEvent(ctx, event)
	if err != nil {
		e.recordRun(ctx, "event", e.now(), taskResult{err: err}, 0)
		return
	}
	event.ID = id
	e.deliverEvent(ctx, event)
}

func (e *Engine) prepareEvent(event Event) Event {
	now := e.now()
	settings := e.settings()
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now
	}
	event.NotifyStatus = NotifySkipped
	switch {
	case event.Severity == SeverityInfo:
		// 保持 skipped：info 只供页面阅读。
	case quietNow(now, settings.QuietHours) && event.Severity != SeverityCritical:
		event.NotifyError = "静默时段"
	case event.Severity == SeverityCritical && e.notify != nil:
		event.NotifyStatus = NotifyQueued
	case event.Severity == SeverityWarning:
		event.NotifyStatus = NotifyPending
	}
	return event
}

// 外发只处理已成功落库的事件；失败状态保留在原事件上供查询。
func (e *Engine) deliverEvent(ctx context.Context, event Event) {
	now := e.now()
	settings := e.settings()
	if event.Severity == SeverityCritical && event.NotifyStatus == NotifyQueued && e.notify != nil {
		status, err := e.notify(ctx, event)
		if err != nil {
			event.NotifyStatus = NotifyFailed
			event.NotifyError = err.Error()
		} else if status != "" {
			event.NotifyStatus = status
		} else {
			event.NotifyStatus = NotifyQueued
		}
		if err := e.store.UpdateEventNotify(ctx, event.ID, event.NotifyStatus, event.NotifyError, now); err != nil {
			e.recordRun(ctx, "notify", now, taskResult{err: err}, 0)
		}
	}
	if event.NotifyStatus == NotifyPending {
		e.mu.Lock()
		e.pendingDigest = append(e.pendingDigest, pendingEvent{id: event.ID, severity: event.Severity, event: event})
		if e.nextDigest.IsZero() {
			e.nextDigest = now.Add(secondsMax(settings.DigestSeconds, 30))
		}
		e.mu.Unlock()
	}
}

// quietNow 判断是否处于静默时段，"23:00-08:30" 这种跨零点区间也支持。
func quietNow(now time.Time, window string) bool {
	window = strings.TrimSpace(window)
	if window == "" {
		return false
	}
	parts := strings.SplitN(window, "-", 2)
	if len(parts) != 2 {
		return false
	}
	parse := func(text string) int {
		var hour, minute int
		if _, err := fmt.Sscanf(strings.TrimSpace(text), "%02d:%02d", &hour, &minute); err != nil {
			return -1
		}
		return hour*60 + minute
	}
	start, end := parse(parts[0]), parse(parts[1])
	if start < 0 || end < 0 || start == end {
		return false
	}
	local := now.In(foundation.AStockLocation)
	current := local.Hour()*60 + local.Minute()
	if start < end {
		return current >= start && current < end
	}
	// 跨零点：23:00-08:30 表示 23:00 之后或 08:30 之前。
	return current >= start || current < end
}

// flushDigest 把累积的 warning 事件合并成一条摘要外发。
func (e *Engine) flushDigest(ctx context.Context, now time.Time) {
	e.mu.Lock()
	if len(e.pendingDigest) == 0 || e.nextDigest.IsZero() || now.Before(e.nextDigest) {
		e.mu.Unlock()
		return
	}
	pending := e.pendingDigest
	e.pendingDigest = nil
	e.nextDigest = time.Time{}
	e.mu.Unlock()
	if e.notify == nil {
		for _, item := range pending {
			_ = e.store.UpdateEventNotify(ctx, item.id, NotifySkipped, "未配置通知", now)
		}
		return
	}
	lines := []string{fmt.Sprintf("共 %d 条监控提醒", len(pending))}
	for _, item := range pending {
		lines = append(lines, "- "+item.event.Message)
	}
	digest := Event{
		RuleID: "digest", RuleName: "监控提醒汇总", RuleType: "digest",
		Severity: SeverityWarning, TriggerKind: "digest",
		Message:   strings.Join(lines, "\n"),
		TradeDate: now.In(foundation.AStockLocation).Format("2006-01-02"),
	}
	status, err := e.notify(ctx, digest)
	if err != nil && e.logger != nil {
		e.logger.Printf("level=warn event=monitor_digest_failed error=%q", runtimelog.Redact(err.Error()))
	}
	for _, item := range pending {
		notifyStatus, notifyError := NotifyMerged, ""
		if err != nil {
			notifyStatus, notifyError = NotifyFailed, runtimelog.Redact(err.Error())
			item.attempts++
			if item.attempts < 3 {
				e.mu.Lock()
				e.pendingDigest = append(e.pendingDigest, item)
				if e.nextDigest.IsZero() {
					e.nextDigest = now.Add(time.Duration(item.attempts) * 30 * time.Second)
				}
				e.mu.Unlock()
			}
		} else if status == NotifySkipped || status == NotifyFailed {
			notifyStatus = status
			notifyError = "通知未启用或未配置"
		}
		_ = e.store.UpdateEventNotify(ctx, item.id, notifyStatus, notifyError, now)
	}
}

// maybePrune 每个交易日 15:35 后清理一次：事件 30 天、运行记录 7 天。
func (e *Engine) maybePrune(ctx context.Context, now time.Time) {
	local := now.In(foundation.AStockLocation)
	tradeDate := local.Format("2006-01-02")
	if tradeDate == e.lastPruneDay || local.Hour() < 15 || (local.Hour() == 15 && local.Minute() < 35) {
		return
	}
	e.lastPruneDay = tradeDate
	if err := e.store.Prune(ctx, 30, 168, now); err != nil && e.logger != nil {
		e.logger.Printf("level=warn event=monitor_prune_failed error=%q", runtimelog.Redact(err.Error()))
	}
}

// CurrentStatus 是只读视图，给 overview 接口和页面状态条。
func (e *Engine) CurrentStatus() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	status := e.status
	_, _, status.SessionOpen = sessionState(e.now())
	if !e.snapFetchedAt.IsZero() {
		status.SnapshotAgeSeconds = int64(e.now().Sub(e.snapFetchedAt).Seconds())
	}
	status.SnapshotStale = status.SnapshotStale || e.snapError != "" || (status.SessionOpen && status.SnapshotAgeSeconds > 60)
	if e.snapError != "" {
		status.SnapshotWarning = e.snapError
	}
	return status
}

// 投递状态常量：pending 只在「排队等摘要」期间存在，flush 后变为 merged。
const (
	NotifyPending = "pending"
	NotifyMerged  = "merged"
)
