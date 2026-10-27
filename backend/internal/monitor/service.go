// monitor.Service 面向 HTTP 的门面：规则/批次的校验保存、试算、
// 异动视图与大盘概览。engine 的并发评估不经过这里。
package monitor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// SharedMarketSnapshot returns a detached read-only copy for market-wide facts.
// Initial/expired reads use the existing single-flight cache, so a disabled or
// idle monitor does not leave the cockpit dependent on opening another page.
func (e *Engine) SharedMarketSnapshot(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	_, fetchErr := e.snapshotRowsWithTTL(ctx, time.Minute)
	e.mu.Lock()
	defer e.mu.Unlock()
	meta := foundation.SourceMeta{
		Source: e.status.SnapshotSource, TradeDate: e.snapTradeDate, FetchedAt: e.snapSourceAt,
		Stale: e.status.SnapshotStale, FallbackReason: e.status.SnapshotWarning,
	}
	if meta.FetchedAt.IsZero() {
		meta.FetchedAt = e.snapFetchedAt
	}
	if len(e.snapRows) == 0 {
		if fetchErr != nil {
			return nil, meta, fetchErr
		}
		return nil, meta, errors.New("全市场行情快照尚未就绪")
	}
	if fetchErr != nil {
		return append([]foundation.MarketSnapshotRow(nil), e.snapRows...), meta, fetchErr
	}
	return append([]foundation.MarketSnapshotRow(nil), e.snapRows...), meta, errorOrNil(e.snapError)
}

// idPrefixes 集中生成 ID，避免散落的字符串拼接把「批次托管规则」这个
// 约定打散在各处。
func lotPriceRuleID(lotID string) string { return "lot:" + lotID + ":price" }
func lotDateRuleID(lotID string) string  { return "lot:" + lotID + ":date" }

func positionLotID(account, symbol string) string {
	return "pos:" + account + ":" + symbol
}

func newID(prefix string) string {
	// 时间戳 + 计数足够；ID 不需要不可猜测性，但需要单调可读。
	return fmt.Sprintf("%s_%x", prefix, time.Now().UnixNano())
}

// ---------------------------------------------------------------- 作用域预览

// ScopeTarget 是某个作用域当前命中的一支标的。编辑器直接展示它，
// 让「持仓 / 关注仓」不再是黑盒：选完范围就能看见到底盯的是哪几支票。
type ScopeTarget struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name,omitempty"`
}

// SetInspectionHoldings 收下巡检页当前草稿的持仓清单。巡检组合保存在
// 浏览器本地，后台引擎读不到，只能由前端推一份过来；不推就退回历史报告。
func (e *Engine) SetInspectionHoldings(ctx context.Context, targets []ScopeTarget) (int, string, error) {
	defer e.invalidateRadar()
	if err := e.store.SaveScopeSnapshot(ctx, ScopeInspection, targets); err != nil {
		return 0, "", err
	}
	stored, updatedAt, err := e.store.LoadScopeSnapshot(ctx, ScopeInspection)
	if err != nil {
		return len(targets), "", err
	}
	return len(stored), updatedAt, nil
}

// inspectionTargets 解析「巡检持仓」作用域：前端同步的快照优先，
// 那是用户在巡检页面上真正看到的清单；没有同步过才回退到最近一次报告。
// 两者都没有时返回空——安静沉默，不猜测用户的持仓。
func (e *Engine) inspectionTargets(ctx context.Context) []ScopeTarget {
	if e.store != nil {
		// updatedAt 非空即代表前端同步过：哪怕清单为空也要以它为准，
		// 用户把计划删空了就该没持仓，不能又冒出历史报告里的老清单。
		if stored, updatedAt, err := e.store.LoadScopeSnapshot(ctx, ScopeInspection); err == nil && updatedAt != "" {
			return stored
		}
	}
	if e.inspectionHoldings != nil {
		if targets, err := e.inspectionHoldings.LatestInspectionHoldings(ctx); err == nil {
			return targets
		}
	}
	return nil
}

// ScopeTargets 返回各作用域当前命中的标的，与引擎判定共用同一份
// 数据源，避免「界面说有 5 只、实际只盯 3 只」。
// 巡检持仓与外部持仓源合并为「持仓」：两份清单取并集去重（巡检在前），
// ScopeInspection 键保留同一份合并结果作兼容，历史前端不会因此报错。
// 取不到列表时返回空切片而不是报错：作用域预览失败不该挡住编辑规则。
func (e *Engine) ScopeTargets(ctx context.Context) map[string][]ScopeTarget {
	result := map[string][]ScopeTarget{ScopePositions: {}, ScopeWatchlist: {}, ScopeInspection: {}}
	snap := e.snapshotIndex()
	nameOf := func(symbol string) string {
		if row, ok := snap[symbol]; ok {
			return row.Name
		}
		if code := sixDigitCode(symbol); code != "" {
			if row, ok := snap[code]; ok {
				return row.Name
			}
		}
		return ""
	}
	merged := make([]ScopeTarget, 0)
	seen := map[string]bool{}
	for _, target := range e.inspectionTargets(ctx) {
		if target.Symbol == "" || seen[target.Symbol] {
			continue
		}
		seen[target.Symbol] = true
		name := target.Name
		if name == "" {
			name = nameOf(target.Symbol)
		}
		merged = append(merged, ScopeTarget{Symbol: target.Symbol, Name: name})
	}
	if e.portfolio != nil {
		if holdings, err := e.portfolio.Holdings(ctx); err == nil {
			for _, holding := range holdings {
				if holding.Quantity <= 0 || seen[holding.Symbol] {
					continue
				}
				seen[holding.Symbol] = true
				name := holding.Name
				if name == "" {
					name = nameOf(holding.Symbol)
				}
				merged = append(merged, ScopeTarget{Symbol: holding.Symbol, Name: name})
			}
		}
	}
	result[ScopePositions] = merged
	result[ScopeInspection] = merged
	if e.store != nil {
		if watch, err := e.store.ListWatchlist(ctx); err == nil {
			list := make([]ScopeTarget, 0, len(watch))
			for _, entry := range watch {
				name := entry.Name
				if name == "" {
					name = nameOf(entry.Symbol)
				}
				list = append(list, ScopeTarget{Symbol: entry.Symbol, Name: name})
			}
			result[ScopeWatchlist] = list
		}
	}
	return result
}

// ---------------------------------------------------------------- 关注仓

// WatchItem 是关注仓页面上的一行：清单本身来自 store，
// 现价与涨跌幅先吃引擎已有的全市场快照，快照缺的再走实时接口兜底。
type WatchItem struct {
	Symbol        string     `json:"symbol"`
	Name          string     `json:"name,omitempty"`
	Note          string     `json:"note,omitempty"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
	Last          float64    `json:"last,omitempty"`
	ChangePercent *float64   `json:"change_percent,omitempty"`
	// Momentum5 是近 5 个交易日累计涨跌幅（%）：关注仓要看的是"这票最近在走什么"，
	// 单日涨跌只是噪声。日线不足 6 根时留空，前端显示 —。
	Momentum5 *float64 `json:"momentum_5d,omitempty"`
	HaveQuote bool     `json:"have_quote,omitempty"`
}

// Watchlist 返回关注仓全部条目，并尽力补行情：先吃引擎已有的全市场快照
// （盘中 10 秒内的新鲜数据），快照缺的标的——周末快照源常不可用、
// 刚加的标的还没进快照——再走实时接口兜底，避免页面整列显示 —。
func (e *Engine) Watchlist(ctx context.Context) ([]WatchItem, error) {
	entries, err := e.store.ListWatchlist(ctx)
	if err != nil {
		return nil, err
	}
	snap := e.snapshotIndex()
	lookup := func(symbol string) (foundation.MarketSnapshotRow, bool) {
		if row, ok := snap[symbol]; ok {
			return row, true
		}
		if code := sixDigitCode(symbol); code != "" {
			if row, ok := snap[code]; ok {
				return row, true
			}
		}
		return foundation.MarketSnapshotRow{}, false
	}
	items := make([]WatchItem, 0, len(entries))
	missing := make([]string, 0)
	for _, entry := range entries {
		item := WatchItem{Symbol: entry.Symbol, Name: entry.Name, Note: entry.Note, CreatedAt: entry.CreatedAt}
		if row, ok := lookup(entry.Symbol); ok && row.Price > 0 && !row.Suspended {
			item.HaveQuote = true
			item.Last = row.Price
			change := row.ChangePercent
			item.ChangePercent = &change
			if item.Name == "" {
				item.Name = row.Name
			}
		} else {
			missing = append(missing, entry.Symbol)
		}
		items = append(items, item)
	}
	if len(missing) > 0 {
		quotes := e.realtimeQuoteIndex(ctx, missing)
		for i := range items {
			if items[i].HaveQuote {
				continue
			}
			quote, ok := quotes[items[i].Symbol]
			if !ok || quote.Price <= 0 {
				continue
			}
			items[i].HaveQuote = true
			items[i].Last = quote.Price
			change := quote.ChangePercent
			items[i].ChangePercent = &change
			if items[i].Name == "" {
				items[i].Name = quote.Name
			}
		}
	}
	e.fillWatchMomentum5(ctx, items, e.now())
	return items, nil
}

// fillWatchMomentum5 给关注仓每一行补近 5 日涨跌幅。
// 日线走 e.dailyBars 的当日缓存，命中后基本不产生请求；关注仓通常只有几十只，
// 用 4 个 worker 并发取，避免串行把首屏拖到几秒。
func (e *Engine) fillWatchMomentum5(ctx context.Context, items []WatchItem, now time.Time) {
	if len(items) == 0 {
		return
	}
	const workers = 4
	indexes := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range indexes {
				bars, ok := e.dailyBars(ctx, items[index].Symbol, 6, now)
				if !ok || len(bars) < 6 {
					continue
				}
				last, base := bars[len(bars)-1].Close, bars[len(bars)-6].Close
				if last <= 0 || base <= 0 {
					continue
				}
				value := round2((last/base - 1) * 100)
				items[index].Momentum5 = &value
			}
		}()
	}
	for index := range items {
		select {
		case indexes <- index:
		case <-ctx.Done():
			close(indexes)
			wg.Wait()
			return
		}
	}
	close(indexes)
	wg.Wait()
}

// AddWatchlist 加入一支标的；名称缺了就按已有快照补。
func (e *Engine) AddWatchlist(ctx context.Context, symbol, name, note string) error {
	defer e.invalidateRadar()
	if name == "" {
		snap := e.snapshotIndex()
		if row, ok := snap[symbol]; ok {
			name = row.Name
		} else if code := sixDigitCode(symbol); code != "" {
			if row, ok := snap[code]; ok {
				name = row.Name
			}
		}
	}
	return e.store.AddWatchlist(ctx, symbol, name, note)
}

// RemoveWatchlist / ClearWatchlist 是 HTTP 层的直通入口。
func (e *Engine) RemoveWatchlist(ctx context.Context, symbol string) error {
	defer e.invalidateRadar()
	return e.store.RemoveWatchlist(ctx, symbol)
}

// UpdateWatchlistNote 二次编辑备注：与 AddWatchlist 的 upsert 不同，
// 这里允许把备注清成空串。
func (e *Engine) UpdateWatchlistNote(ctx context.Context, symbol, note string) error {
	if e.store == nil {
		return errors.New("存储不可用")
	}
	return e.store.UpdateWatchlistNote(ctx, symbol, note)
}

func (e *Engine) ClearWatchlist(ctx context.Context) error {
	defer e.invalidateRadar()
	return e.store.ClearWatchlist(ctx)
}

// ---------------------------------------------------------------- 规则 CRUD

func (e *Engine) Rules(ctx context.Context, ruleType string) ([]Rule, error) {
	return e.store.ListRules(ctx, ruleType, false)
}

// Events/DeleteEvent/ClearEvents/Runs 是给 HTTP 层的只读与清理入口。
func (e *Engine) Events(ctx context.Context, filter EventFilter) ([]Event, error) {
	return e.store.ListEvents(ctx, filter)
}

func (e *Engine) EventCount(ctx context.Context, filter EventFilter) (int, error) {
	return e.store.CountEvents(ctx, filter)
}

func (e *Engine) DeleteEvent(ctx context.Context, id int64) error {
	return e.store.DeleteEvent(ctx, id)
}

func (e *Engine) ClearEvents(ctx context.Context, before int64) error {
	return e.store.ClearEvents(ctx, before)
}

func (e *Engine) Runs(ctx context.Context, limit int) ([]Run, error) {
	return e.store.ListRuns(ctx, limit)
}

// SaveRule 校验并保存一条规则；批次规则走 SaveLot 的派生逻辑，不直接到这里。
func (e *Engine) SaveRule(ctx context.Context, input Rule) (Rule, error) {
	defer e.invalidateRadar()
	if strings.TrimSpace(input.ID) == "" {
		input.ID = newID("rule")
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = utcNow()
	}
	input.UpdatedAt = utcNow()
	rule, err := NormalizeRule(input)
	if err != nil {
		return Rule{}, err
	}
	if rule.Scope == ScopeAll && rule.Filters.MaxSymbols == 0 {
		// 没写上限的全市场规则给一个保守默认，避免一轮扫描无边界。
		rule.Filters.MaxSymbols = scanRowLimit
	}
	if err := e.store.UpsertRule(ctx, rule); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

func (e *Engine) RemoveRule(ctx context.Context, id string) error {
	defer e.invalidateRadar()
	rules, err := e.store.ListRules(ctx, "", false)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.ID != id {
			continue
		}
		if rule.Source == SourceLot {
			// 批次托管规则删掉，等于这个批次不再有止盈止损监控。
			// 直接把批次标成「不再托管」并清掉派生规则——仓位本身、模拟盘
			// 持仓、持仓作用域都不受影响，下次同步也不会把规则生回来。
			return e.StopLotMonitoring(ctx, rule.LotID)
		}
		return e.store.DeleteRule(ctx, id)
	}
	return fmt.Errorf("规则不存在")
}

// StopLotMonitoring 让一个批次退出监控：批次记录保留（持仓/巡检照旧），
// 只停用托管并删除派生的止盈止损、到期提醒规则。
func (e *Engine) StopLotMonitoring(ctx context.Context, lotID string) error {
	defer e.invalidateRadar()
	if strings.TrimSpace(lotID) == "" {
		return fmt.Errorf("规则不属于任何批次")
	}
	lot, ok, err := e.store.GetLot(ctx, lotID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("批次不存在")
	}
	lot.Detached = true
	lot.Enabled = false
	lot.UpdatedAt = utcNow()
	if err := e.store.UpsertLot(ctx, lot); err != nil {
		return err
	}
	if err := e.store.DeleteRule(ctx, lotPriceRuleID(lotID)); err != nil {
		return err
	}
	return e.store.DeleteRule(ctx, lotDateRuleID(lotID))
}

func (e *Engine) ToggleRule(ctx context.Context, id string, enabled bool) error {
	defer e.invalidateRadar()
	rules, err := e.store.ListRules(ctx, "", false)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.ID != id {
			continue
		}
		if rule.Source == SourceLot {
			// 批次托管的启停跟随批次本身：单独开规则而没有批次，
			// 用户会以为批次还在监控里。注意这里要用批次 ID，
			// 规则 ID 形如 lot:<批次ID>:price，直接传会报「批次不存在」。
			if err := e.store.SetLotEnabled(ctx, rule.LotID, enabled); err != nil {
				return err
			}
		}
		return e.store.SetRuleEnabled(ctx, id, enabled)
	}
	return fmt.Errorf("规则不存在")
}

// ---------------------------------------------------------------- 持仓批次

// SaveLot 的校验口径写在单独函数里，便于 tests 直接对齐。
func normalizeLot(lot *Lot) error {
	lot.Symbol = strings.TrimSpace(lot.Symbol)
	if lot.Symbol == "" {
		return errors.New("标的代码不能为空")
	}
	lot.Name = strings.TrimSpace(lot.Name)
	if lot.Source == "" {
		lot.Source = SourceManual
	}
	if lot.Source != SourceManual && lot.Source != SourcePosition {
		return errors.New("批次来源无效")
	}
	if lot.Quantity <= 0 || lot.Quantity > 1e9 {
		return errors.New("数量需要大于 0")
	}
	if lot.CostPrice <= 0 {
		return errors.New("成本价需要大于 0")
	}
	if lot.TargetPct < 0 || lot.TargetPct > 1000 {
		return errors.New("止盈百分比无效")
	}
	if lot.StopPct < 0 || lot.StopPct >= 100 {
		return errors.New("止损百分比无效")
	}
	if lot.LeadDays < 0 || lot.LeadDays > 90 {
		return errors.New("提前天数需要在 0～90 之间")
	}
	for _, dateField := range []struct{ label, value string }{
		{"买入日期", lot.BuyDate}, {"提醒日期", lot.RemindDate},
	} {
		if strings.TrimSpace(dateField.value) == "" {
			continue
		}
		if _, err := time.ParseInLocation("2006-01-02", dateField.value, foundation.AStockLocation); err != nil {
			return fmt.Errorf("%s无效", dateField.label)
		}
	}
	lot.Enabled = true
	return nil
}

// SaveLot 新建或更新批次，并同步生成/刷新两条托管规则。
// source=position 的批次只读，编辑入口在这里直接拒绝。
func (e *Engine) SaveLot(ctx context.Context, input Lot) (Lot, error) {
	defer e.invalidateRadar()
	if strings.TrimSpace(input.ID) == "" {
		input.ID = newID("lot")
	}
	if err := normalizeLot(&input); err != nil {
		return Lot{}, err
	}
	if input.Source == SourcePosition {
		_, ok, err := e.store.GetLot(ctx, input.ID)
		if err == nil && ok && !input.Detached {
			return Lot{}, errors.New("模拟盘同步批次是只读的，如需修改请先转为手动批次")
		}
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = utcNow()
	}
	if err := e.store.UpsertLot(ctx, input); err != nil {
		return Lot{}, err
	}
	if err := e.ensureLotRules(ctx, input); err != nil {
		return Lot{}, err
	}
	return input, nil
}

// ensureLotRules 生成/刷新批次的止盈止损与到期两条托管规则。
// 规则 ID 按 lot.ID 确定，因此重复保存就是刷新而不是堆积。
func (e *Engine) ensureLotRules(ctx context.Context, lot Lot) error {
	if lot.Source == SourcePosition && lot.Detached {
		// 已 detach 的批次不再自动派生规则：它们的手动版本由用户自己管理。
		return nil
	}
	if priceRule, ok := lotPriceRule(lot); ok {
		if err := e.store.UpsertRule(ctx, priceRule); err != nil {
			return err
		}
	} else if err := e.store.DeleteRule(ctx, lotPriceRuleID(lot.ID)); err != nil {
		return err
	}
	if dateRule, ok := lotDateRule(lot); ok {
		if err := e.store.UpsertRule(ctx, dateRule); err != nil {
			return err
		}
	} else if err := e.store.DeleteRule(ctx, lotDateRuleID(lot.ID)); err != nil {
		return err
	}
	return nil
}

func lotPriceRule(lot Lot) (Rule, bool) {
	conditions := make([]Condition, 0, 2)
	if lot.TargetPct > 0 {
		conditions = append(conditions, Condition{Field: "last", Op: OpGTE, Value: roundPrice(lot.CostPrice * (1 + lot.TargetPct/100))})
	}
	if lot.StopPct > 0 {
		conditions = append(conditions, Condition{Field: "last", Op: OpLTE, Value: roundPrice(lot.CostPrice * (1 - lot.StopPct/100))})
	}
	if len(conditions) == 0 {
		return Rule{}, false
	}
	rule := Rule{
		ID:   lotPriceRuleID(lot.ID),
		Name: fmt.Sprintf("批次止盈止损 %s", lot.Symbol),
		Type: RuleTypePrice, Enabled: lot.Enabled, Severity: SeverityCritical,
		Scope: ScopeSymbols, Symbols: []string{lot.Symbol},
		Conditions: conditions, Logic: "or", CooldownSeconds: 300,
		Source: SourceLot, LotID: lot.ID,
		CreatedAt: lot.CreatedAt, UpdatedAt: lot.UpdatedAt,
	}
	normalized, err := NormalizeRule(rule)
	return normalized, err == nil
}

func lotDateRule(lot Lot) (Rule, bool) {
	if strings.TrimSpace(lot.RemindDate) == "" {
		return Rule{}, false
	}
	rule := Rule{
		ID:   lotDateRuleID(lot.ID),
		Name: fmt.Sprintf("批次到期提醒 %s", lot.Symbol),
		Type: RuleTypeDate, Enabled: lot.Enabled, Severity: SeverityWarning,
		// 到期提醒没有行情语义；scope 恒为 all，避免「symbols 却没有代码」的校验死锁。
		Scope:      ScopeAll,
		Conditions: []Condition{{Field: "days_to_remind", Op: OpLTE, Value: float64(lot.LeadDays)}},
		Logic:      "and", CooldownSeconds: 3600,
		Source: SourceLot, LotID: lot.ID, RemindDate: lot.RemindDate, LeadDays: lot.LeadDays,
		CreatedAt: lot.CreatedAt, UpdatedAt: lot.UpdatedAt,
	}
	normalized, err := NormalizeRule(rule)
	return normalized, err == nil
}

func (e *Engine) RemoveLot(ctx context.Context, id string) error {
	defer e.invalidateRadar()
	if _, ok, err := e.store.GetLot(ctx, id); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("批次不存在")
	}
	if err := e.store.DeleteRule(ctx, lotPriceRuleID(id)); err != nil {
		return err
	}
	if err := e.store.DeleteRule(ctx, lotDateRuleID(id)); err != nil {
		return err
	}
	return e.store.DeleteLot(ctx, id)
}

func (e *Engine) DetachLot(ctx context.Context, id string) (Lot, error) {
	defer e.invalidateRadar()
	lot, ok, err := e.store.GetLot(ctx, id)
	if err != nil {
		return Lot{}, err
	}
	if !ok {
		return Lot{}, fmt.Errorf("批次不存在")
	}
	if lot.Source != SourcePosition {
		return lot, fmt.Errorf("仅模拟盘同步批次支持转手动")
	}
	newLot := lot
	newLot.ID = newID("lot")
	newLot.Source = SourceManual
	newLot.PositionAccount = ""
	newLot.CreatedAt = utcNow()
	newLot.UpdatedAt = utcNow()
	if err := e.store.UpsertLot(ctx, newLot); err != nil {
		return Lot{}, err
	}
	lot.Detached = true
	lot.Enabled = false
	lot.UpdatedAt = utcNow()
	if err := e.store.UpsertLot(ctx, lot); err != nil {
		return Lot{}, err
	}
	// 原只读批次的规则退场：替代手动批次有它自己的托管规则。
	_ = e.store.DeleteRule(ctx, lotPriceRuleID(lot.ID))
	_ = e.store.DeleteRule(ctx, lotDateRuleID(lot.ID))
	return newLot, nil
}

// SyncPositions 从持仓源同步模拟盘/实盘持仓为只读批次。
// 幂等：账户+代码唯一；已清仓的批次归档（enabled=false）不删除。
func (e *Engine) SyncPositions(ctx context.Context) (int, error) {
	defer e.invalidateRadar()
	if e.portfolio == nil {
		return 0, errors.New("未配置持仓源，无法同步")
	}
	holdings, err := e.portfolio.Holdings(ctx)
	if err != nil {
		return 0, err
	}
	previous, err := e.store.ListLots(ctx, SourcePosition)
	if err != nil {
		return 0, err
	}
	active := map[string]bool{}
	synced := 0
	for _, holding := range holdings {
		id := positionLotID(holding.AccountID, holding.Symbol)
		if holding.Quantity <= 0 {
			continue
		}
		active[id] = true
		existing, ok, err := e.store.GetLot(ctx, id)
		if err != nil {
			return synced, err
		}
		lot := Lot{
			ID: id, Symbol: holding.Symbol, Name: holding.Name,
			Quantity: holding.Quantity, CostPrice: holding.AverageCost,
			BuyDate: holding.LastBuyDate, Source: SourcePosition,
			PositionAccount: holding.AccountID, Enabled: true,
		}
		// 用户把这个批次退出监控后（删掉托管规则），同步只刷新持仓事实，
		// 不把托管状态和规则生回来——否则删了下次同步又出现。
		if ok && existing.Detached {
			lot.Detached = true
			lot.Enabled = false
		}
		// 止盈止损价反算百分比，只在两侧都有意义时写入：缺一侧就让那一侧沉默。
		if holding.TakeProfitPrice > 0 && holding.AverageCost > 0 {
			if target := (holding.TakeProfitPrice - holding.AverageCost) / holding.AverageCost * 100; target > 0 {
				lot.TargetPct = round2(target)
			}
		}
		if holding.StopLossPrice > 0 && holding.AverageCost > 0 {
			if stop := (holding.AverageCost - holding.StopLossPrice) / holding.AverageCost * 100; stop > 0 {
				lot.StopPct = round2(stop)
			}
		}
		if err := e.store.UpsertLot(ctx, lot); err != nil {
			return synced, err
		}
		if err := e.ensureLotRules(ctx, lot); err != nil {
			return synced, err
		}
		synced++
		_ = existing
	}
	// 持仓源成功返回的是完整清单；清仓标的通常直接消失，而非返回数量 0。
	for _, lot := range previous {
		if !active[lot.ID] && !lot.Detached && (lot.Enabled || lot.Quantity > 0) {
			if err := e.store.archivePositionLot(ctx, lot.ID); err != nil {
				return synced, err
			}
		}
	}
	return synced, nil
}

// syncPositionsIfEmpty 在批次表完全为空时做一次冷启动同步：
// 监控中心「持仓」页签与「持仓」作用域都依赖批次表，空表会让用户
// 以为系统没有在盯任何东西。已有批次（哪怕是 disabled）说明用户
// 可能改过止盈止损或转了手动，冷启动同步不做、避免抹掉手工状态。
func (e *Engine) syncPositionsIfEmpty(ctx context.Context) (int, error) {
	if e.portfolio == nil {
		return 0, nil
	}
	lots, err := e.store.ListLots(ctx, "")
	if err != nil || len(lots) > 0 {
		return 0, err
	}
	return e.SyncPositions(ctx)
}

// ---------------------------------------------------------------- 视图

type LotView struct {
	Lot
	Price        float64 `json:"price"`
	MarketValue  float64 `json:"market_value"`
	PnL          float64 `json:"pnl"`
	PnLPct       float64 `json:"pnl_pct"`
	TargetPrice  float64 `json:"target_price"`
	StopPrice    float64 `json:"stop_price"`
	ToTargetPct  float64 `json:"to_target_pct"`
	ToStopPct    float64 `json:"to_stop_pct"`
	DaysToRemind *int    `json:"days_to_remind,omitempty"`
	Status       string  `json:"status"` // holding/profit_hit/stop_hit/due/closed
}

func (e *Engine) Lots(ctx context.Context, source string) ([]LotView, error) {
	lots, err := e.store.ListLots(ctx, source)
	if err != nil {
		return nil, err
	}
	symbols := make([]string, 0, len(lots))
	for _, lot := range lots {
		symbols = append(symbols, lot.Symbol)
	}
	quotes, _ := e.fetchQuotes(ctx, symbols)
	today, _ := time.ParseInLocation("2006-01-02", time.Now().In(foundation.AStockLocation).Format("2006-01-02"), foundation.AStockLocation)
	views := make([]LotView, 0, len(lots))
	for _, lot := range lots {
		view := LotView{Lot: lot}
		if lot.TargetPct > 0 {
			view.TargetPrice = roundPrice(lot.CostPrice * (1 + lot.TargetPct/100))
		}
		if lot.StopPct > 0 {
			view.StopPrice = roundPrice(lot.CostPrice * (1 - lot.StopPct/100))
		}
		if quote, ok := quotes[lot.Symbol]; ok && quote.Price > 0 {
			view.Price = quote.Price
			view.MarketValue = quote.Price * float64(lot.Quantity)
			view.PnL = (quote.Price - lot.CostPrice) * float64(lot.Quantity)
			view.PnLPct = (quote.Price - lot.CostPrice) / lot.CostPrice * 100
			if view.TargetPrice > 0 {
				view.ToTargetPct = (view.TargetPrice - quote.Price) / quote.Price * 100
			}
			if view.StopPrice > 0 {
				view.ToStopPct = (quote.Price - view.StopPrice) / quote.Price * 100
			}
		} else {
			view.MarketValue = lot.CostPrice * float64(lot.Quantity)
		}
		if strings.TrimSpace(lot.RemindDate) != "" {
			if deadline, err := time.ParseInLocation("2006-01-02", lot.RemindDate, foundation.AStockLocation); err == nil {
				days := int(deadline.Sub(today).Hours() / 24)
				view.DaysToRemind = &days
			}
		}
		switch {
		case !lot.Enabled:
			view.Status = "closed"
		case lot.TargetPct > 0 && lot.StopPct > 0 && view.Price >= view.TargetPrice && view.TargetPrice > 0:
			view.Status = "profit_hit"
		case lot.StopPct > 0 && view.StopPrice > 0 && view.Price <= view.StopPrice && view.Price > 0:
			view.Status = "stop_hit"
		case view.DaysToRemind != nil && *view.DaysToRemind <= 0:
			view.Status = "due"
		default:
			view.Status = "holding"
		}
		views = append(views, view)
	}
	return views, nil
}

// AbnormalItem 是异动视图的一行。Signals 汇总命中的信号键，
// Level 只对偏离值有意义（triggered/near/watch）。
type AbnormalItem struct {
	Symbol         string   `json:"symbol"`
	Name           string   `json:"name"`
	Board          string   `json:"board"`
	Price          float64  `json:"price"`
	ChangePercent  float64  `json:"change_percent"`
	TurnoverRate   float64  `json:"turnover_rate"`
	VolumeRatio    float64  `json:"volume_ratio"`
	Amount         float64  `json:"amount"`
	LimitPrice     float64  `json:"limit_price,omitempty"`
	LimitDownPrice float64  `json:"limit_down_price,omitempty"`
	Signals        []string `json:"signals"`
	Level          string   `json:"level,omitempty"`
	// Severe 表示按该板块的 10/30 日阈值估算越线，未含公告重置与同向异动次数。
	Severe  bool    `json:"severe,omitempty"`
	Dev3    float64 `json:"dev_3d,omitempty"`
	Dev10   float64 `json:"dev_10d,omitempty"`
	Dev30   float64 `json:"dev_30d,omitempty"`
	NewHigh bool    `json:"new_high,omitempty"`
	NewLow  bool    `json:"new_low,omitempty"`
	// 交易所异动口径的预警扩展：最接近触发的窗口、剩余空间与估算触发价。
	WorstKey     string  `json:"worst_key,omitempty"`
	Threshold    float64 `json:"threshold,omitempty"`
	Direction    string  `json:"direction,omitempty"`
	Remaining    float64 `json:"remaining,omitempty"`
	TriggerPrice float64 `json:"trigger_price,omitempty"`
	Events10d    int     `json:"events_10d,omitempty"`
	// LastTriggerDate 是最近一次越过异常波动阈值的交易日（YYYY-MM-DD）：
	// 监控个股的「触发日期」。空串 = 最近 30 个交易日都没越过阈值。
	LastTriggerDate    string           `json:"last_trigger_date,omitempty"`
	Windows            []AbnormalWindow `json:"windows,omitempty"`
	WindowAvailability map[string]bool  `json:"window_availability,omitempty"`
	IsST               bool             `json:"is_st,omitempty"`
	// Monitored = 用户从异动页侧栏加入监控的个股（关注仓）：
	// 没走到阈值 50% 也持续计算并展示，页面侧栏据此渲染折叠列表。
	Monitored    bool       `json:"monitored,omitempty"`
	DailyAsOf    string     `json:"daily_as_of,omitempty"`
	CalculatedAt *time.Time `json:"calculated_at,omitempty"`
	Historical   bool       `json:"historical"`
}

type AbnormalView struct {
	Tab                string         `json:"tab"`
	TradeDate          string         `json:"trade_date"`
	Items              []AbnormalItem `json:"items"`
	Total              int            `json:"total"`
	GeneratedAt        time.Time      `json:"generated_at"`
	SnapshotAgeSeconds int64          `json:"snapshot_age_seconds"`
	SnapshotAt         string         `json:"snapshot_at,omitempty"`
	SnapshotSource     string         `json:"snapshot_source,omitempty"`
	SnapshotStale      bool           `json:"snapshot_stale"`
	Error              string         `json:"error,omitempty"`
}

// AbnormalTabs 是异动页允许的子视图。
func AbnormalTabs() []string {
	return []string{"limit_up", "broken", "limit_down", "high_low", "volume", "deviation"}
}

func (e *Engine) Abnormal(ctx context.Context, tab string, limit int) (AbnormalView, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	valid := false
	for _, known := range AbnormalTabs() {
		if known == tab {
			valid, tab = true, known
			break
		}
	}
	if !valid {
		tab = "limit_up"
	}
	view := AbnormalView{Tab: tab, Items: []AbnormalItem{}}
	rows, err := e.snapshotRows(ctx)
	status := e.CurrentStatus()
	view.SnapshotAt, view.SnapshotSource, view.SnapshotStale = status.LastSnapshotAt, status.SnapshotSource, status.SnapshotStale
	if err != nil {
		view.GeneratedAt = e.now()
		// 快照源抖动不该把整页打死：偏离值只依赖日线 + 基准指数，
		// 持仓/关注仓/规则标的仍能照常算，只是少了全市场涨跌幅榜。
		if e.fillAbnormalWithoutSnapshot(ctx, &view) {
			warning := "全市场快照不可用，已降级为持仓 / 关注仓 / 规则标的范围的异动计算"
			if view.Error != "" {
				warning += "；" + view.Error
			}
			view.Error = warning
			// -1 = 没有快照可比，页面显示「—」而不是误报成 0 分钟。
			view.SnapshotAgeSeconds = -1
			return view, nil
		}
		view.Error = err.Error()
		return view, err
	}
	view.SnapshotAgeSeconds, view.TradeDate = status.SnapshotAgeSeconds, time.Now().In(foundation.AStockLocation).Format("2006-01-02")
	switch tab {
	case "limit_up", "broken", "limit_down":
		for _, row := range rows {
			if row.Suspended || row.Price <= 0 || row.PreviousClose <= 0 {
				continue
			}
			var signals []string
			state := limitState(row)
			switch tab {
			case "limit_up":
				if state == 1 {
					signals = []string{"limit_up"}
				} else {
					continue
				}
			case "broken":
				if state == 2 {
					signals = []string{"broken_limit"}
				} else {
					continue
				}
			case "limit_down":
				if IsLimitDown(row) {
					signals = []string{"limit_down"}
				} else {
					continue
				}
			}
			item := abnormalItem(row)
			item.LimitPrice = LimitPrice(row.Board, row.IsST, row.PreviousClose)
			item.LimitDownPrice = LimitDownPrice(row.Board, row.IsST, row.PreviousClose)
			item.Signals = signals
			view.Items = append(view.Items, item)
		}
		sortAbnormalByAmount(view.Items)
	case "volume":
		for _, row := range rows {
			if row.Suspended || row.Price <= 0 {
				continue
			}
			if row.VolumeRatio >= 2 && (row.TurnoverRate >= 3 || row.Amount >= 5e8) {
				item := abnormalItem(row)
				item.Signals = []string{"volume"}
				view.Items = append(view.Items, item)
			}
		}
		sort.SliceStable(view.Items, func(i, j int) bool { return view.Items[i].VolumeRatio > view.Items[j].VolumeRatio })
	case "high_low", "deviation":
		candidates := e.deviationCandidates(ctx, rows)
		// 视图即时计算：结果既回写缓存，也直接作为本页数据源。
		// 不能只读缓存——引擎暂停时缓存不会刷新，页面会停在旧偏离值上，
		// 而现价/今日涨幅来自最新快照，两个数字对不上就会像"数据算错了"。
		var freshDev map[string]abnormalDeviationResult
		if len(candidates) > 0 {
			results := e.computeCandidates(ctx, candidates, e.now())
			freshDev = results.abnormal
			e.mu.Lock()
			for symbol, dev := range results.dev {
				e.deviations[symbol] = dev
			}
			for symbol, flags := range results.highLow {
				e.highLow[symbol] = flags
			}
			e.mu.Unlock()
		}
		var eventCounts map[string]int
		if tab == "deviation" {
			eventCounts = e.deviationEventCounts(ctx, 10)
		}
		// 侧栏加入监控的个股：没苗头也要留在列表里持续展示距离。
		monitored := map[string]bool{}
		if tab == "deviation" {
			monitored = e.monitoredSymbols(ctx)
		}
		for _, row := range rows {
			if row.Suspended || row.Price <= 0 {
				continue
			}
			item := abnormalItem(row)
			if tab == "high_low" {
				e.mu.Lock()
				cached, ok := e.highLow[row.Symbol]
				e.mu.Unlock()
				if !ok || (!cached.High && !cached.Low) {
					continue
				}
				item.NewHigh, item.NewLow, item.Signals = cached.High, cached.Low, []string{}
				if cached.High {
					item.Signals = append(item.Signals, "high_60d")
				}
				if cached.Low {
					item.Signals = append(item.Signals, "low_60d")
				}
			} else {
				// 偏离值取本次请求现算的结果（见上方 freshDev），
				// 保证与同行的现价/今日涨幅是同一时点。
				cached, ok := freshDev[row.Symbol]
				if !ok {
					continue
				}
				item.Dev3, item.Dev10, item.Dev30 = cached.Dev3, cached.Dev10, cached.Dev30
				item.DailyAsOf, item.Historical = cached.DailyAsOf, cached.Stale
				if !cached.FetchedAt.IsZero() {
					calculatedAt := cached.FetchedAt
					item.CalculatedAt = &calculatedAt
				}
				projectionPrice := row.Price
				if cached.Stale {
					projectionPrice = 0
					view.Error = "部分标的日线尚未包含当前交易日，偏离值按历史收盘计算，暂不估算触发价"
				}
				item.Windows = availableAbnormalWindows(evalAbnormalWindows(cached.Dev3, cached.Dev10, cached.Dev30, row.Board, row.IsST, projectionPrice, cached.Base3, cached.Base10, cached.Base30), cached.Available)
				item.WindowAvailability = abnormalWindowAvailability(cached.Available)
				item.Level = abnormalLevel(item.Windows)
				item.Severe = abnormalSevere(item.Windows)
				// 预警页只收「已触发 / 接近阈值」，观察级只有用户加了监控才留：
				// 观察级只是涨了点，长期挂在列表里既不是异动，也没有入口去掉。
				item.Monitored = monitored[row.Symbol]
				if !abnormalWorthListing(item.Level, item.Monitored) {
					continue
				}
				if item.Monitored || item.Level == "triggered" {
					// 触发日期只能从逐日明细反查：只给监控个股与已触发的个股补算
					// （数量少、日线与指数都已有当日缓存，不会多打行情接口）。
					if calc, err := e.AbnormalCalc(ctx, row.Symbol); err == nil {
						item.LastTriggerDate = lastAbnormalTriggerDate(calc.Rows, calc.Board, calc.IsST)
					}
				}
				worst := worstAbnormalWindow(item.Windows)
				item.WorstKey, item.Threshold, item.Direction = worst.Key, worst.Threshold, worst.Direction
				item.Remaining, item.TriggerPrice = worst.Remaining, worst.TriggerPrice
				item.Signals = []string{"deviation"}
				if eventCounts != nil {
					item.Events10d = eventCounts[row.Symbol]
				}
			}
			view.Items = append(view.Items, item)
		}
		// 监控个股可能不在全市场快照行里（停牌、快照缺行）：
		// 补走一遍单票计算路径，保证侧栏折叠列表每只都有当前状态。
		if tab == "deviation" && len(monitored) > 0 {
			seen := make(map[string]bool, len(view.Items))
			for _, item := range view.Items {
				seen[item.Symbol] = true
			}
			missing := make([]string, 0, len(monitored))
			for symbol := range monitored {
				if !seen[symbol] {
					missing = append(missing, symbol)
				}
			}
			sort.Strings(missing)
			for _, symbol := range missing {
				item, ok := e.abnormalItemForSymbol(ctx, symbol, eventCounts)
				if !ok {
					continue
				}
				view.Items = append(view.Items, item)
			}
		}
		if tab == "deviation" {
			sort.SliceStable(view.Items, func(i, j int) bool {
				ai, bi := abnormalDeviation(view.Items[i]), abnormalDeviation(view.Items[j])
				return ai > bi
			})
		}
	}
	if len(view.Items) > limit {
		view.Total, view.Items = len(view.Items), view.Items[:limit]
	} else {
		view.Total = len(view.Items)
	}
	view.GeneratedAt = e.now()
	return view, nil
}

func abnormalDeviation(item AbnormalItem) float64 {
	return maxAbs(item.Dev3, item.Dev10, item.Dev30)
}

func maxAbs(values ...float64) float64 {
	result := 0.0
	for _, value := range values {
		if value < 0 {
			value = -value
		}
		if value > result {
			result = value
		}
	}
	return result
}

// deviationBoardBase 取触发阈值：每个板块的 3 日偏离触发值不同，
// 「接近度」必须用同一个基准除才可与 0.5/0.7/1.0 比较。
func deviationBoardBase(board string, dev deviationResult) float64 {
	threshold := DeviationThreshold(board)
	_ = dev
	return threshold
}

func abnormalItem(row foundation.MarketSnapshotRow) AbnormalItem {
	return AbnormalItem{
		Symbol: row.Symbol, Name: row.Name, Board: row.Board,
		Price: row.Price, ChangePercent: row.ChangePercent,
		TurnoverRate: row.TurnoverRate, VolumeRatio: row.VolumeRatio, Amount: row.Amount,
		Level: "",
	}
}

func sortAbnormalByAmount(items []AbnormalItem) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Amount > items[j].Amount })
}

// ---------------------------------------------------------------- 概览与试算

type OverviewView struct {
	Status           Status  `json:"engine"`
	EventsToday      int     `json:"events_today"`
	CriticalToday    int     `json:"critical_today"`
	RuleCount        int     `json:"rule_count"`
	EnabledRuleCount int     `json:"enabled_rule_count"`
	LotCount         int     `json:"lot_count"`
	EnabledLotCount  int     `json:"enabled_lot_count"`
	PositionLotCount int     `json:"position_lot_count"`
	Latest           []Event `json:"latest_events"`
}

func (e *Engine) Overview(ctx context.Context) (OverviewView, error) {
	view := OverviewView{Latest: []Event{}}
	view.Status = e.CurrentStatus()
	tradeDate := time.Now().In(foundation.AStockLocation).Format("2006-01-02")
	rules, err := e.store.ListRules(ctx, "", false)
	if err != nil {
		return view, err
	}
	view.RuleCount = len(rules)
	for _, rule := range rules {
		if rule.Enabled {
			view.EnabledRuleCount++
		}
	}
	lots, _ := e.store.ListLots(ctx, "")
	for _, lot := range lots {
		// 已退出监控（删掉托管规则）的批次不再计入「持仓批次」，
		// 否则删了规则这里还显示原来那么多批，像没删掉。
		if lot.Detached {
			continue
		}
		view.LotCount++
		if lot.Source == SourcePosition {
			view.PositionLotCount++
		}
		if lot.Enabled {
			view.EnabledLotCount++
		}
	}
	events, err := e.store.ListEvents(ctx, EventFilter{TradeDate: tradeDate, Limit: 10})
	if err == nil {
		view.Latest = events
	}
	total, critical, err := e.store.EventSummary(ctx, tradeDate)
	if err == nil {
		view.EventsToday, view.CriticalToday = total, critical
	}
	return view, nil
}

type TestOutcome struct {
	WouldFire int            `json:"would_fire"`
	Total     int            `json:"total"`
	Samples   []TestSample   `json:"samples"`
	Evaluated int            `json:"evaluated"`
	Note      string         `json:"note,omitempty"`
	Details   map[string]any `json:"-"`
}

type TestSample struct {
	Symbol        string   `json:"symbol"`
	Name          string   `json:"name"`
	Price         float64  `json:"price"`
	ChangePercent float64  `json:"change_percent"`
	Signals       []string `json:"signals"`
}

// TestRule 用当前快照/行情做一次干跑：不写状态、不落事件、不发通知。
// 改完阈值立刻知道会不会响——没这个能力，规则只能靠行情撞一次来验证。
func (e *Engine) TestRule(ctx context.Context, rule Rule) (TestOutcome, error) {
	normalized, err := NormalizeRule(rule)
	if err != nil {
		return TestOutcome{}, err
	}
	outcome := TestOutcome{Samples: []TestSample{}}
	sampleLimit := 10
	appendCase := func(obs Observation, symbol, name string) {
		outcome.Total++
		hit := EvaluateRule(normalized, obs)
		if len(hit) == 0 {
			return
		}
		Kinds := make([]string, 0, len(hit))
		for _, signal := range hit {
			Kinds = append(Kinds, signal.Kind)
		}
		outcome.WouldFire++
		if len(outcome.Samples) < sampleLimit {
			outcome.Samples = append(outcome.Samples, TestSample{
				Symbol: symbol, Name: name, Price: obs.Price,
				ChangePercent: obs.ChangePercent, Signals: Kinds,
			})
		}
	}
	deviationSamples := func(symbols []string) map[string]abnormalDeviationResult {
		if normalized.Type != RuleTypeDeviation {
			return nil
		}
		return e.computeCandidates(ctx, symbols, e.now()).abnormal
	}
	mergeSample := func(obs *Observation, samples map[string]abnormalDeviationResult, symbol string) {
		if normalized.Type != RuleTypeDeviation {
			return
		}
		obs.HaveDeviation = false
		obs.DeviationAvailable = [3]bool{}
		if sample, ok := samples[symbol]; ok && !sample.Stale {
			obs.DeviationAvailable = sample.Available
			obs.Deviation3, obs.Deviation10, obs.Deviation30 = sample.Dev3, sample.Dev10, sample.Dev30
		}
	}
	switch {
	case rule.Type == RuleTypeMarket:
		rows, err := e.snapshotRows(ctx)
		if err != nil {
			return outcome, err
		}
		appendCase(Observation{Market: e.marketStats(ctx, rows)}, "", "大盘")
	case rule.Type == RuleTypeSector:
		ranks := e.sectorRankIndex(ctx)
		if len(ranks) == 0 {
			return outcome, fmt.Errorf("板块榜单不可用，无法试算")
		}
		stats := e.sectorStats(ctx, normalized.Symbols, ranks, e.now())
		for _, code := range normalized.Symbols {
			stat := stats[code]
			appendCase(Observation{Sector: stat}, code, stat.Name)
		}
	case rule.Scope == ScopeAll:
		rows, err := e.snapshotRows(ctx)
		if err != nil {
			return outcome, err
		}
		limit := normalized.Filters.MaxSymbols
		if limit <= 0 || limit > scanRowLimit {
			limit = scanRowLimit
		}
		accepted := make([]foundation.MarketSnapshotRow, 0, limit)
		for _, row := range rows {
			if len(accepted) >= limit {
				break
			}
			if !normalized.Filters.accepts(row) {
				continue
			}
			accepted = append(accepted, row)
		}
		signalResults := map[string]signalResult{}
		var deviationResults map[string]abnormalDeviationResult
		if normalized.Type == RuleTypeDeviation {
			symbols := make([]string, 0, len(accepted))
			for _, row := range accepted {
				symbols = append(symbols, row.Symbol)
			}
			deviationResults = deviationSamples(symbols)
		}
		if normalized.Type == RuleTypeSignal && len(accepted) > 0 {
			// 技术信号要逐只读日线，串行扫全市场会把试算拖到超时：
			// 借用候选池那条 8 路并发通道，顺带回填当日缓存。
			symbols := make([]string, 0, len(accepted))
			for _, row := range accepted {
				symbols = append(symbols, row.Symbol)
			}
			signalResults = e.computeCandidates(ctx, symbols, e.now()).signals
			for symbol, result := range signalResults {
				e.mu.Lock()
				e.signals[symbol] = result
				e.mu.Unlock()
			}
		}
		for _, row := range accepted {
			observation := FromSnapshotRow(row)
			e.mergeHighLow(row.Symbol, &observation)
			e.mergeDeviation(row.Symbol, &observation)
			mergeSample(&observation, deviationResults, row.Symbol)
			if result, ok := signalResults[row.Symbol]; ok {
				observation.HaveSignals = true
				observation.Signals = result.flags
			}
			appendCase(observation, row.Symbol, row.Name)
		}
	default:
		symbols, err := e.resolveRuleSymbols(ctx, normalized)
		if err != nil {
			return outcome, err
		}
		quotes, _ := e.fetchQuotes(ctx, symbols)
		snapIndex := e.snapshotIndex()
		costs := e.costBySymbol()
		deviationResults := deviationSamples(symbols)
		for _, symbol := range symbols {
			row, rowOK := snapIndex[symbol]
			quote, quoteOK := quotes[symbol]
			if !rowOK && !quoteOK {
				continue
			}
			observation := observationFromQuote(quote, quoteOK, row, rowOK, costs[symbol])
			e.mergeHighLow(symbol, &observation)
			e.mergeDeviation(symbol, &observation)
			mergeSample(&observation, deviationResults, symbol)
			e.mergeSignals(ctx, symbol, &observation)
			appendCase(observation, symbol, quoteName(quote, quoteOK, row, rowOK))
		}
	}
	if outcome.Evaluated == 0 {
		outcome.Evaluated = outcome.Total
	}
	if outcome.Total == 0 {
		outcome.Note = "当前没有可评估的标的，请检查作用域或行情源"
	}
	return outcome, nil
}
