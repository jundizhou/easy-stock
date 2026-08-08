package duanxianxia

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"easy-stock/backend/internal/foundation"
)

const kaipanlaThemeLeaderSource = "duanxianxia:kaipanla-theme-leader"

type RecentLimitUpProvider interface {
	RecentLimitUps(ctx context.Context, lookbackDays int) ([]foundation.LimitUpEvent, error)
}

// LimitUpProvider keeps retained Kaipanla pools authoritative for every
// available trading day, while EastMoney supplies missing days, stocks, and
// quote fields that are absent from Kaipanla's compact pool payload.
type LimitUpProvider struct {
	primary  *Service
	fallback RecentLimitUpProvider
}

func NewLimitUpProvider(primary *Service, fallback RecentLimitUpProvider) *LimitUpProvider {
	return &LimitUpProvider{primary: primary, fallback: fallback}
}

// StockThemes returns retained Kaipanla per-stock attributions without
// requiring the stock to appear in the current trading day's limit-up pool.
// It deliberately returns both short-term pool concepts and trend-theme leader
// labels so the analysis engine can choose the right source for each route.
func (p *LimitUpProvider) StockThemes(ctx context.Context, symbol string, lookbackDays int) ([]foundation.StockThemeAttribution, error) {
	if p.primary == nil {
		return nil, fmt.Errorf("kaipanla theme cache is unavailable")
	}
	limit := max(lookbackDays, 2)
	pools, _, poolErr := p.primary.LimitUpPools(ctx, limit)
	snapshots, _, snapshotErr := p.primary.Snapshots(ctx, limit)
	if poolErr != nil && snapshotErr != nil {
		return nil, fmt.Errorf("kaipanla theme cache failed: pools: %v; themes: %w", poolErr, snapshotErr)
	}

	items := make([]foundation.StockThemeAttribution, 0, 8)
	for _, pool := range pools {
		for _, event := range pool.Events {
			if event.Symbol != symbol {
				continue
			}
			theme := firstCachedConcept(event.Concepts)
			if theme == "" {
				continue
			}
			source := strings.TrimSpace(event.Meta.Source)
			if source == "" {
				source = "duanxianxia:kaipanla-limit-up"
			}
			items = append(items, foundation.StockThemeAttribution{
				Symbol: symbol, Theme: theme, Concepts: append([]string(nil), event.Concepts...),
				Source: source, TradeDate: pool.TradeDate,
			})
		}
	}
	for _, snapshot := range snapshots {
		for _, theme := range snapshot.Themes {
			name := strings.TrimSpace(theme.Name)
			if name == "" || !theme.LeadersLoaded {
				continue
			}
			for _, leader := range theme.Leaders {
				if leader.Symbol != symbol {
					continue
				}
				items = append(items, foundation.StockThemeAttribution{
					Symbol: symbol, Theme: name, Concepts: []string{name},
					Source: kaipanlaThemeLeaderSource, TradeDate: snapshot.TradeDate,
					Rank: theme.Rank, Role: leader.Role,
				})
			}
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].TradeDate != items[j].TradeDate {
			return items[i].TradeDate > items[j].TradeDate
		}
		leftPriority := cachedThemeSourcePriority(items[i].Source)
		rightPriority := cachedThemeSourcePriority(items[j].Source)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return items[i].Rank < items[j].Rank
	})
	seen := map[string]struct{}{}
	result := make([]foundation.StockThemeAttribution, 0, len(items))
	for _, item := range items {
		themeKey := compactCachedTheme(item.Theme)
		if themeKey == "" {
			continue
		}
		key := item.Source + "|" + themeKey
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result, nil
}

func (p *LimitUpProvider) RecentLimitUps(ctx context.Context, lookbackDays int) ([]foundation.LimitUpEvent, error) {
	var primaryEvents []foundation.LimitUpEvent
	var themeSnapshots []Snapshot
	var primaryErr error
	if p.primary != nil {
		pools, _, err := p.primary.LimitUpPools(ctx, max(lookbackDays, 2))
		if err != nil {
			primaryErr = err
		} else {
			for _, pool := range pools {
				primaryEvents = append(primaryEvents, cloneLimitUpEvents(pool.Events)...)
			}
		}
		if snapshots, _, err := p.primary.Snapshots(ctx, max(lookbackDays, 2)); err == nil {
			themeSnapshots = snapshots
		}
	}

	var fallbackEvents []foundation.LimitUpEvent
	var fallbackErr error
	if p.fallback != nil {
		fallbackEvents, fallbackErr = p.fallback.RecentLimitUps(ctx, lookbackDays)
	}

	switch {
	case len(primaryEvents) > 0 && len(fallbackEvents) > 0:
		return applyKaipanlaThemeLeaders(mergeLimitUpEvents(primaryEvents, fallbackEvents, primaryErr), themeSnapshots), nil
	case len(primaryEvents) > 0:
		return applyKaipanlaThemeLeaders(primaryEvents, themeSnapshots), nil
	case len(fallbackEvents) > 0:
		reason := "开盘啦涨停池暂无可用快照"
		if primaryErr != nil {
			reason = "开盘啦涨停池不可用：" + primaryErr.Error()
		}
		return applyKaipanlaThemeLeaders(markLimitUpFallback(fallbackEvents, reason), themeSnapshots), nil
	case primaryErr != nil && fallbackErr != nil:
		return nil, fmt.Errorf("kaipanla limit-up pool failed: %v; eastmoney fallback failed: %w", primaryErr, fallbackErr)
	case primaryErr != nil:
		return nil, primaryErr
	case fallbackErr != nil:
		return nil, fallbackErr
	default:
		return nil, fmt.Errorf("no limit-up data is available")
	}
}

type kaipanlaThemeLeaderAttribution struct {
	theme string
	rank  int
	role  string
}

func applyKaipanlaThemeLeaders(events []foundation.LimitUpEvent, snapshots []Snapshot) []foundation.LimitUpEvent {
	if len(events) == 0 || len(snapshots) == 0 {
		return events
	}
	byStockDate := map[string]kaipanlaThemeLeaderAttribution{}
	for _, snapshot := range snapshots {
		for _, theme := range snapshot.Themes {
			name := strings.TrimSpace(theme.Name)
			if name == "" || !theme.LeadersLoaded {
				continue
			}
			for _, leader := range theme.Leaders {
				key := snapshot.TradeDate + "|" + leader.Symbol
				candidate := kaipanlaThemeLeaderAttribution{theme: name, rank: theme.Rank, role: leader.Role}
				previous, exists := byStockDate[key]
				if !exists || candidate.rank < previous.rank || (candidate.rank == previous.rank && leader.Rank < leaderRoleRank(previous.role)) {
					byStockDate[key] = candidate
				}
			}
		}
	}
	for index := range events {
		date := ""
		if !events[index].Date.IsZero() {
			date = events[index].Date.Format("2006-01-02")
		}
		attribution, exists := byStockDate[date+"|"+events[index].Symbol]
		if !exists {
			continue
		}
		events[index].PrimaryTheme = attribution.theme
		events[index].ThemeSource = kaipanlaThemeLeaderSource
		events[index].ThemeRank = attribution.rank
		events[index].ThemeLeaderRole = attribution.role
		if !containsString(events[index].Concepts, attribution.theme) {
			events[index].Concepts = append([]string{attribution.theme}, events[index].Concepts...)
		}
	}
	return events
}

func leaderRoleRank(role string) int {
	for index, candidate := range []string{"龙一", "龙二", "龙三", "龙四", "龙五"} {
		if role == candidate {
			return index + 1
		}
	}
	return 99
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstCachedConcept(values []string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func cachedThemeSourcePriority(source string) int {
	if strings.Contains(source, "kaipanla-theme-leader") {
		return 0
	}
	if strings.Contains(source, "kaipanla-limit-up") {
		return 1
	}
	return 2
}

func compactCachedTheme(value string) string {
	replacer := strings.NewReplacer("概念", "", "板块", "", "产业链", "", " ", "", "-", "", "_", "")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(value)))
}

func mergeLimitUpEvents(primary []foundation.LimitUpEvent, fallback []foundation.LimitUpEvent, primaryErr error) []foundation.LimitUpEvent {
	result := cloneLimitUpEvents(primary)
	index := make(map[string]int, len(primary)+len(fallback))
	primaryDate := latestLimitUpDate(primary)
	for position, event := range result {
		index[limitUpEventKey(event)] = position
	}
	for _, candidate := range fallback {
		key := limitUpEventKey(candidate)
		if position, exists := index[key]; exists {
			result[position] = fillLimitUpEvent(result[position], candidate)
			continue
		}
		copyEvent := cloneLimitUpEvent(candidate)
		if primaryDate != "" && candidate.Date.Format("2006-01-02") > primaryDate {
			copyEvent.Meta.FallbackReason = "开盘啦涨停池尚未更新到当日，当前交易日使用东方财富补位"
			copyEvent.Meta.CarryForward = true
		} else if primaryErr != nil && strings.TrimSpace(copyEvent.Meta.FallbackReason) == "" {
			copyEvent.Meta.FallbackReason = "开盘啦涨停池刷新失败，使用东方财富补位"
		}
		index[key] = len(result)
		result = append(result, copyEvent)
	}
	return result
}

func fillLimitUpEvent(primary foundation.LimitUpEvent, fallback foundation.LimitUpEvent) foundation.LimitUpEvent {
	if primary.Name == "" {
		primary.Name = fallback.Name
	}
	if primary.Price == 0 {
		primary.Price = fallback.Price
	}
	if primary.ChangePercent == 0 {
		primary.ChangePercent = fallback.ChangePercent
	}
	if primary.Amount == 0 {
		primary.Amount = fallback.Amount
	}
	if primary.FloatMarketCap == 0 {
		primary.FloatMarketCap = fallback.FloatMarketCap
	}
	if primary.TurnoverRate == 0 {
		primary.TurnoverRate = fallback.TurnoverRate
	}
	if primary.Streak == 0 {
		primary.Streak = fallback.Streak
	}
	if primary.FirstLimitTime == "" {
		primary.FirstLimitTime = fallback.FirstLimitTime
	}
	if primary.LastLimitTime == "" {
		primary.LastLimitTime = fallback.LastLimitTime
	}
	if primary.Industry == "" {
		primary.Industry = fallback.Industry
	}
	if primary.Days == 0 {
		primary.Days = fallback.Days
	}
	if primary.Count == 0 {
		primary.Count = fallback.Count
	}
	return primary
}

func markLimitUpFallback(events []foundation.LimitUpEvent, reason string) []foundation.LimitUpEvent {
	result := cloneLimitUpEvents(events)
	for index := range result {
		if strings.TrimSpace(result[index].Meta.FallbackReason) == "" {
			result[index].Meta.FallbackReason = reason
		}
	}
	return result
}

func cloneLimitUpEvents(events []foundation.LimitUpEvent) []foundation.LimitUpEvent {
	result := make([]foundation.LimitUpEvent, len(events))
	for index, event := range events {
		result[index] = cloneLimitUpEvent(event)
	}
	return result
}

func cloneLimitUpEvent(event foundation.LimitUpEvent) foundation.LimitUpEvent {
	event.Concepts = append([]string(nil), event.Concepts...)
	return event
}

func latestLimitUpDate(events []foundation.LimitUpEvent) string {
	latest := ""
	for _, event := range events {
		if event.Date.IsZero() {
			continue
		}
		date := event.Date.Format("2006-01-02")
		if date > latest {
			latest = date
		}
	}
	return latest
}

func limitUpEventKey(event foundation.LimitUpEvent) string {
	date := ""
	if !event.Date.IsZero() {
		date = event.Date.Format("2006-01-02")
	}
	return date + "|" + event.Symbol
}

var _ RecentLimitUpProvider = (*LimitUpProvider)(nil)
