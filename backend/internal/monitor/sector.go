package monitor

// sector.go：板块监控。规则对象是"板块"而不是个股：symbols 存东财
// 板块代码（BK 开头）或系统题材库题材（TH+数字），观测由四路数据拼出来——
//
//	板块榜单（industry/concept）→ 板块涨跌幅、当日主力净额；
//	板块分时序列             → 近 5 分钟拉升/回落、近 30 分钟净额变化；
//	全市场快照 × 成分目录     → 成分上涨占比、涨停/跌停家数；
//	题材库（TH）             → 成员股快照实时聚合涨跌幅/广度/涨跌停。
//
// 题材维度没有东财 BK 代码，拿不到板块分时与主力净额，相关维度
// 自然沉默（Have*=false）——缺能力不装有能力。

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/runtimelog"
)

const (
	// sectorScanLimit 是单轮分时取数的板块上限。分时接口按板块逐条
	// 取数，规则里选再多，一轮也只扫前 40 个代码。
	sectorScanLimit = 40
	// sectorMaxPerRule 是单条规则允许的板块数（与原项目口径一致 ≤20）。
	sectorMaxPerRule = 20
	// sectorSpeedMinutes 是"快速异动"的观察窗口。
	sectorSpeedMinutes = 5
	// sectorFlowDeltaMinutes 是资金净额变化的观察窗口。
	sectorFlowDeltaMinutes = 30
	// sectorRankTTL / sectorCatalogTTL 是两份缓存的寿命。榜单 60 秒足够
	// 跟上盘中节奏；成分目录一天变不了几次，6 小时刷新一次。
	sectorRankTTL    = 60 * time.Second
	sectorCatalogTTL = 6 * time.Hour
)

// SectorRankProvider 提供板块资金流榜单（dimension = industry / concept）。
// 方法签名与 httpapi 的行情总览服务一致，注入零适配。
type SectorRankProvider interface {
	SectorFlowRanking(ctx context.Context, dimension string, limit int) ([]foundation.SectorFlowRank, foundation.SourceMeta, error)
}

// SectorIntradayProvider 提供板块分时资金流与分时价格。
type SectorIntradayProvider interface {
	SectorFlowIntraday(ctx context.Context, codes []string) ([]foundation.SectorFlowSeries, foundation.SourceMeta, error)
	SectorPriceIntraday(ctx context.Context, codes []string) ([]foundation.SectorPriceSeries, foundation.SourceMeta, error)
}

// SectorCatalogProvider 提供股票 → 行业/概念归属目录，用于广度统计。
type SectorCatalogProvider interface {
	StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error)
}

// ThemeSummary 是题材库题材的清单行。monitor 自持一份窄结构，
// 不依赖 themelibrary 包——数据由 HTTP 层适配注入。
type ThemeSummary struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	ChangePercent float64 `json:"change_percent"`
	StockCount    int     `json:"stock_count"`
	LimitUpCount  int     `json:"limit_up_count"`
}

// ThemeCatalogProvider 提供题材库题材清单与成员股代码（裸 6 位）。
type ThemeCatalogProvider interface {
	Themes(ctx context.Context) ([]ThemeSummary, error)
	ThemeMembers(ctx context.Context, id int) ([]string, error)
}

// isThemeCode 判断板块代码是否指向题材库题材（TH+非零数字，如 TH5）。
func isThemeCode(code string) bool {
	_, ok := themeID(code)
	return ok
}

// themeID 从 TH 代码里解析题材 id。
func themeID(code string) (int, bool) {
	if !strings.HasPrefix(code, "TH") || len(code) < 3 {
		return 0, false
	}
	var id int
	if _, err := fmt.Sscanf(code, "TH%d", &id); err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// SectorMover 是板块事件消息里附带的领涨/领跌成员。数据来自快照，
// 不额外发请求——采集零成本，信息价值高（提醒到手就知道追什么）。
type SectorMover struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	ChangePercent float64 `json:"change_percent"`
}

// SectorStats 是一个板块的观测快照。每个维度独立 Have 标记：
// 榜单挂了不该连累分时信号，成分目录没对上也不该把普涨当成 0%。
type SectorStats struct {
	Have          bool          `json:"have"`
	Name          string        `json:"name,omitempty"`
	ChangePercent float64       `json:"change_percent,omitempty"`
	HaveFlow      bool          `json:"have_flow,omitempty"`
	MainNetInflow float64       `json:"main_net_inflow,omitempty"` // 元
	HaveFlowDelta bool          `json:"have_flow_delta,omitempty"`
	FlowDelta     float64       `json:"flow_delta,omitempty"` // 元，近 30 分钟
	HaveSpeed     bool          `json:"have_speed,omitempty"`
	Speed         float64       `json:"speed,omitempty"` // 百分点，近 5 分钟
	HaveBreadth   bool          `json:"have_breadth,omitempty"`
	Breadth       float64       `json:"breadth,omitempty"` // 0~100，成分上涨占比
	Members       int           `json:"members,omitempty"`
	MemberChange  float64       `json:"member_change,omitempty"` // 成员股平均涨跌幅（题材维度的涨跌幅来源）
	LimitUp       int           `json:"limit_up,omitempty"`
	LimitDown     int           `json:"limit_down,omitempty"`
	TopMovers     []SectorMover `json:"top_movers,omitempty"` // 涨幅前三的成员（SectorBreadth 顺带收集）
}

// SectorRankSource 暴露板块榜单数据源给 HTTP 层：板块选择器需要同一份
// 榜单来列候选。未配置返回 nil。
func (e *Engine) SectorRankSource() SectorRankProvider {
	return e.sectorRank
}

// sectorRankIndex 返回 代码→榜单行 的索引（industry + concept 合并），
// 60 秒缓存。榜单行同时是板块名的权威来源。
func (e *Engine) sectorRankIndex(ctx context.Context) map[string]foundation.SectorFlowRank {
	now := e.now()
	e.mu.Lock()
	fresh := now.Sub(e.sectorRankAt) < sectorRankTTL
	cached := e.sectorRanks
	e.mu.Unlock()
	if fresh && len(cached) > 0 {
		return cached
	}
	if e.sectorRank == nil {
		return cached
	}
	merged := map[string]foundation.SectorFlowRank{}
	for _, dimension := range []string{"industry", "concept"} {
		rows, _, err := e.sectorRank.SectorFlowRanking(ctx, dimension, 300)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("level=warn event=monitor_sector_rank_failed dimension=%s error=%q", dimension, runtimelog.Redact(err.Error()))
			}
			continue
		}
		for _, row := range rows {
			if row.Code == "" {
				continue
			}
			merged[row.Code] = row
		}
	}
	if len(merged) == 0 {
		return cached
	}
	e.mu.Lock()
	e.sectorRanks = merged
	e.sectorRankAt = now
	e.mu.Unlock()
	return merged
}

// sectorCatalog 取股票归属目录，6 小时缓存。失败返回 nil：广度维度
// 保持沉默，不阻塞涨跌幅与资金流两条主链路。
func (e *Engine) sectorCatalog(ctx context.Context) []foundation.StockCatalogEntry {
	now := e.now()
	e.mu.Lock()
	fresh := now.Sub(e.catalogAt) < sectorCatalogTTL
	cached := e.catalog
	e.mu.Unlock()
	if fresh && len(cached) > 0 {
		return cached
	}
	if e.catalogSrc == nil {
		return cached
	}
	entries, err := e.catalogSrc.StockCatalog(ctx)
	if err != nil || len(entries) == 0 {
		if err != nil && e.logger != nil {
			e.logger.Printf("level=warn event=monitor_sector_catalog_failed error=%q", runtimelog.Redact(err.Error()))
		}
		return cached
	}
	e.mu.Lock()
	e.catalog = entries
	e.catalogAt = now
	e.mu.Unlock()
	return entries
}

// sectorMembersIndex 把目录翻成 板块名 → 成分代码集合。行业精确匹配、
// 概念精确匹配；对不上的名字安静跳过——目录与榜单的口径差交给
// HaveBreadth 暴露，而不是硬凑一个错的广度数。
func sectorMembersIndex(entries []foundation.StockCatalogEntry) map[string][]string {
	members := map[string][]string{}
	for _, entry := range entries {
		if entry.Symbol == "" {
			continue
		}
		if industry := strings.TrimSpace(entry.Industry); industry != "" {
			members[industry] = append(members[industry], entry.Symbol)
		}
		for _, concept := range entry.Concepts {
			concept = strings.TrimSpace(concept)
			if concept != "" {
				members[concept] = append(members[concept], entry.Symbol)
			}
		}
	}
	return members
}

// themeIndex 取题材库清单，30 分钟缓存。题材与成员一天更新不了几次，
// 但热榜排序会变，留个短缓存兼顾新鲜与省调用。
func (e *Engine) themeIndex(ctx context.Context) map[int]ThemeSummary {
	now := e.now()
	e.mu.Lock()
	fresh := now.Sub(e.themesAt) < 30*time.Minute
	cached := e.themes
	e.mu.Unlock()
	if fresh && len(cached) > 0 {
		return cached
	}
	if e.themeCatalog == nil {
		return cached
	}
	themes, err := e.themeCatalog.Themes(ctx)
	if err != nil || len(themes) == 0 {
		if err != nil && e.logger != nil {
			e.logger.Printf("level=warn event=monitor_sector_themes_failed error=%q", runtimelog.Redact(err.Error()))
		}
		return cached
	}
	index := make(map[int]ThemeSummary, len(themes))
	for _, theme := range themes {
		if theme.ID > 0 {
			index[theme.ID] = theme
		}
	}
	e.mu.Lock()
	e.themes = index
	e.themesAt = now
	e.mu.Unlock()
	return index
}

// themeMembers 取题材成员股代码（裸 6 位），与题材清单同寿命缓存。
// 成员表按题材逐个取，失败返回 nil 由调用方沉默处理。
func (e *Engine) themeMembers(ctx context.Context, id int) []string {
	now := e.now()
	e.mu.Lock()
	cached, ok := e.themeMembersCache[id]
	fresh := now.Sub(e.themeMembersAt) < 30*time.Minute
	e.mu.Unlock()
	if ok && fresh {
		return cached
	}
	if e.themeCatalog == nil {
		return cached
	}
	members, err := e.themeCatalog.ThemeMembers(ctx, id)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("level=warn event=monitor_sector_theme_members_failed id=%d error=%q", id, runtimelog.Redact(err.Error()))
		}
		return cached
	}
	e.mu.Lock()
	if e.themeMembersCache == nil {
		e.themeMembersCache = map[int][]string{}
	}
	e.themeMembersCache[id] = members
	e.themeMembersAt = now
	e.mu.Unlock()
	return members
}

// sectorStats 装一批板块的观测。codes 是 BK 代码；ranks 缺某个代码时
// 该板块只剩广度维度可用（名字回落到代码本身）。
func (e *Engine) sectorStats(ctx context.Context, codes []string, ranks map[string]foundation.SectorFlowRank, now time.Time) map[string]SectorStats {
	stats := make(map[string]SectorStats, len(codes))
	// 账户广度两份底料：成分索引 + 快照行。
	var members map[string][]string
	if entries := e.sectorCatalog(ctx); len(entries) > 0 {
		members = sectorMembersIndex(entries)
	}
	rows, _ := e.snapshotRows(ctx)
	// 快照行的 Symbol 是裸 6 位（301660），成分目录给的是带后缀的
	// （301660.SZ）：两侧口径不一，统一再塞一份纯数字键对齐。
	snapBySymbol := make(map[string]foundation.MarketSnapshotRow, len(rows)*2)
	for _, row := range rows {
		snapBySymbol[row.Symbol] = row
		if code := sixDigitCode(row.Symbol); code != "" {
			snapBySymbol[code] = row
		}
	}

	// 题材库维度（TH）没有 BK 代码，分时与主力净额天然拿不到，
	// 不把它们塞进批量取数白白打一轮失败请求。
	var bkCodes []string
	for _, code := range codes {
		if !isThemeCode(code) {
			bkCodes = append(bkCodes, code)
		}
	}

	// 分时序列一次批量取：价格（拉升/回落）与资金流（净额变化）。
	priceByCode := map[string]foundation.SectorPriceSeries{}
	flowByCode := map[string]foundation.SectorFlowSeries{}
	if e.sectorIntraday != nil && len(bkCodes) > 0 {
		if series, _, err := e.sectorIntraday.SectorPriceIntraday(ctx, bkCodes); err == nil {
			for _, item := range series {
				priceByCode[item.Code] = item
			}
		}
		if series, _, err := e.sectorIntraday.SectorFlowIntraday(ctx, bkCodes); err == nil {
			for _, item := range series {
				flowByCode[item.Code] = item
			}
		}
	}
	var themes map[int]ThemeSummary
	if e.themeCatalog != nil {
		themes = e.themeIndex(ctx)
	}

	for _, code := range codes {
		stat := SectorStats{Have: true}
		if id, ok := themeID(code); ok {
			// 题材维度：名字回落题材库，涨跌幅优先用成员股快照的
			// 实时均值（库里的值是快照时点的），广度照走成员×快照。
			// 题材成员自己拉取，不依赖 BK 成分目录是否就绪。
			if theme, found := themes[id]; found && theme.Name != "" {
				stat.Name = theme.Name
				stat.ChangePercent = theme.ChangePercent
			} else {
				stat.Name = code
			}
			stat.SectorBreadth(snapBySymbol, e.themeMembers(ctx, id))
			if stat.HaveBreadth {
				stat.ChangePercent = stat.MemberChange
			}
			stats[code] = stat
			continue
		}
		if rank, ok := ranks[code]; ok && rank.Name != "" {
			stat.Name = rank.Name
			stat.ChangePercent = rank.ChangePercent
			stat.HaveFlow = true
			stat.MainNetInflow = rank.MainNetInflow
		} else {
			stat.Name = code
		}
		if series, ok := priceByCode[code]; ok && len(series.Points) > 0 {
			if speed, ok := seriesSpeed(series.Points, sectorSpeedMinutes); ok {
				stat.HaveSpeed = true
				stat.Speed = speed
			}
		}
		if series, ok := flowByCode[code]; ok && len(series.Points) > 0 {
			if delta, ok := seriesDelta(series.Points, series.LatestMain, sectorFlowDeltaMinutes); ok {
				stat.HaveFlowDelta = true
				stat.FlowDelta = delta
			}
		}
		if members != nil {
			stat.SectorBreadth(snapBySymbol, members[stat.Name])
		}
		stats[code] = stat
	}
	return stats
}

// SectorBreadth 用成分股快照填充广度维度。没有成分或成分全部停牌时
// HaveBreadth 保持 false——"不知道"不能伪装成"没人上涨"。
// 同一轮循环顺带记涨幅前三的成员：事件消息附带领涨股，不用二次查询。
func (s *SectorStats) SectorBreadth(snap map[string]foundation.MarketSnapshotRow, symbols []string) {
	if len(symbols) == 0 {
		return
	}
	alive, up, limitUp, limitDown, changeSum := 0, 0, 0, 0, 0.0
	movers := make([]SectorMover, 0, 3)
	for _, symbol := range symbols {
		row, ok := snap[symbol]
		if !ok {
			if code := sixDigitCode(symbol); code != "" {
				row, ok = snap[code]
			}
		}
		if !ok || row.Suspended || row.Price <= 0 {
			continue
		}
		alive++
		changeSum += row.ChangePercent
		if row.ChangePercent > 0 {
			up++
		}
		switch limitState(row) {
		case 1:
			limitUp++
		}
		if IsLimitDown(row) {
			limitDown++
		}
		// 只记上涨的：领跌信息由涨跌幅与广度自己会说话。
		if row.ChangePercent > 0 {
			movers = append(movers, SectorMover{Symbol: sixDigitCode(row.Symbol), Name: row.Name, ChangePercent: row.ChangePercent})
			if len(movers) > 3 {
				sort.Slice(movers, func(i, j int) bool { return movers[i].ChangePercent > movers[j].ChangePercent })
				movers = movers[:3]
			}
		}
	}
	if alive == 0 {
		return
	}
	if len(movers) > 1 {
		sort.Slice(movers, func(i, j int) bool { return movers[i].ChangePercent > movers[j].ChangePercent })
	}
	s.HaveBreadth = true
	s.Members = alive
	s.Breadth = float64(up) / float64(alive) * 100
	s.MemberChange = changeSum / float64(alive)
	s.LimitUp = limitUp
	s.LimitDown = limitDown
	s.TopMovers = movers
}

// seriesSpeed 计算分时价格序列最近 minutes 分钟的涨跌幅变化（百分点）。
// 找不到足够早的采样点（刚开盘）时返回 false。
func seriesSpeed(points []foundation.SectorPricePoint, minutes int) (float64, bool) {
	last := points[len(points)-1]
	base, ok := sectorPointBefore(points, last.Time, minutes)
	if !ok {
		return 0, false
	}
	return last.ChangePercent - base, true
}

// seriesDelta 计算分时资金流序列最近 minutes 分钟的净额变化（元）。
func seriesDelta(points []foundation.SectorFlowPoint, latest float64, minutes int) (float64, bool) {
	if latest == 0 {
		return 0, false
	}
	last := points[len(points)-1]
	base, ok := sectorFlowBefore(points, last.Time, minutes)
	if !ok {
		return 0, false
	}
	return latest - base, true
}

// sectorPointBefore 找时刻 boundary（time 往回 minutes 分钟）之后、
// 且严格早于 time 的第一个点，返回它的涨跌幅。把最后一个点自己
// 当基点会把"没有历史"伪装成"没有变化"，必须排除。
func sectorPointBefore(points []foundation.SectorPricePoint, time string, minutes int) (float64, bool) {
	boundary := shiftClock(time, -(minutes + 1))
	if boundary == "" {
		return 0, false
	}
	for _, point := range points {
		if point.Time >= boundary && point.Time < time {
			return point.ChangePercent, true
		}
	}
	return 0, false
}

// sectorFlowBefore 与上同，作用于资金流序列。
func sectorFlowBefore(points []foundation.SectorFlowPoint, time string, minutes int) (float64, bool) {
	boundary := shiftClock(time, -(minutes + 1))
	if boundary == "" {
		return 0, false
	}
	for _, point := range points {
		if point.Time >= boundary && point.Time <= time {
			return point.Main, true
		}
	}
	return 0, false
}

// shiftClock 把 "HH:MM" 平移 offset 分钟。解析失败返回空串。
func shiftClock(hhmm string, offset int) string {
	if len(hhmm) != 5 || hhmm[2] != ':' {
		return ""
	}
	var hour, minute int
	if _, err := fmt.Sscanf(hhmm, "%02d:%02d", &hour, &minute); err != nil {
		return ""
	}
	total := hour*60 + minute + offset
	if total < 0 {
		total = 0
	}
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

// runSectorTask：60 秒档的板块评估。规则没选板块、榜单不可用时安静
// 返回；单条规则单轮最多评估它自己选的板块。
func (e *Engine) runSectorTask(ctx context.Context, now time.Time, tradeDate string) {
	startedAt := now
	rules, err := e.store.ListRules(ctx, RuleTypeSector, true)
	if err != nil {
		e.recordRun(ctx, "sector", startedAt, taskResult{err: err}, 0)
		return
	}
	if len(rules) == 0 {
		return
	}
	codes := map[string]bool{}
	for _, rule := range rules {
		for _, code := range rule.Symbols {
			codes[code] = true
		}
	}
	scanning := keys(codes)
	sort.Strings(scanning)
	if len(scanning) > sectorScanLimit {
		scanning = scanning[:sectorScanLimit]
	}
	hasBK := false
	for _, code := range scanning {
		if !isThemeCode(code) {
			hasBK = true
			break
		}
	}
	ranks := e.sectorRankIndex(ctx)
	if len(ranks) == 0 && hasBK {
		// 纯题材规则不依赖榜单，榜单挂了只挡 BK 维度。
		e.recordRun(ctx, "sector", startedAt, taskResult{err: fmt.Errorf("板块榜单不可用")}, 0)
		return
	}
	if ranks == nil {
		ranks = map[string]foundation.SectorFlowRank{}
	}
	stats := e.sectorStats(ctx, scanning, ranks, now)
	triggered, evaluated := 0, 0
	for _, rule := range rules {
		for _, code := range rule.Symbols {
			stat, ok := stats[code]
			if !ok {
				continue
			}
			evaluated++
			triggered += e.evaluateRule(ctx, rule, Observation{Sector: stat}, code, stat.Name, now, tradeDate)
		}
	}
	e.recordRun(ctx, "sector", startedAt, taskResult{evaluated: evaluated}, triggered)
}
