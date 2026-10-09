package stockanalysis

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

const ResearchPeerLimit = 24
const ResearchPeerDailyBars = 121

type ResearchPeerInput struct {
	Symbol string
	Name   string
	KLines []foundation.KLine
}

type ResearchPeerGroup struct {
	Basis          string
	SelectionScope string
	CandidateCount int
	Members        []ResearchPeerInput
}

// Select one business cohort before looking at price performance. Code-spaced
// sampling includes weak stocks as well as winners; it is not a sector index.
func SelectResearchPeers(input Input, limit int) ResearchPeerGroup {
	groups := map[string][]ResearchPeerInput{}
	seen := map[string]bool{}
	conceptKeys := map[string]string{}
	for _, concept := range uniqueStrings(input.Concepts, 40) {
		if validBusinessThemeLabel(concept) {
			label := researchCohortLabel(concept)
			conceptKeys[strings.ToLower(label)] = label
		}
	}
	for _, item := range input.Catalog {
		symbol, err := foundation.NormalizeSymbol(item.Symbol)
		if err != nil || symbol.Canonical == input.Symbol || seen[symbol.Canonical] {
			continue
		}
		seen[symbol.Canonical] = true
		peer := ResearchPeerInput{Symbol: symbol.Canonical, Name: item.Name}
		if input.Industry != "" && strings.TrimSpace(item.Industry) == strings.TrimSpace(input.Industry) {
			groups["行业："+strings.TrimSpace(input.Industry)] = append(groups["行业："+strings.TrimSpace(input.Industry)], peer)
		}
		matchedConcepts := map[string]bool{}
		for _, other := range item.Concepts {
			key := strings.ToLower(researchCohortLabel(other))
			if concept, ok := conceptKeys[key]; ok && !matchedConcepts[key] {
				groupKey := "概念目录：" + concept
				groups[groupKey] = append(groups[groupKey], peer)
				if researchBroadIndustry(input.Industry) && strings.TrimSpace(item.Industry) == strings.TrimSpace(input.Industry) {
					intersection := "行业概念：" + strings.TrimSpace(input.Industry) + " / " + concept
					groups[intersection] = append(groups[intersection], peer)
				}
				matchedConcepts[key] = true
			}
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := len(groups[keys[i]]), len(groups[keys[j]])
		if (left >= 2) != (right >= 2) {
			return left >= 2
		}
		leftScore, _ := researchCohortPriority(input, keys[i])
		rightScore, _ := researchCohortPriority(input, keys[j])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if left != right {
			return left < right
		}
		return keys[i] < keys[j]
	})
	if len(keys) == 0 {
		return ResearchPeerGroup{}
	}
	basis := keys[0]
	pool := groups[basis]
	if len(pool) == 0 || limit <= 0 {
		return ResearchPeerGroup{}
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].Symbol < pool[j].Symbol })
	count := min(limit, len(pool))
	_, scope := researchCohortPriority(input, basis)
	result := ResearchPeerGroup{Basis: basis, SelectionScope: scope, CandidateCount: len(pool), Members: make([]ResearchPeerInput, 0, count)}
	for i := 0; i < count; i++ {
		index := 0
		if count > 1 {
			index = i * (len(pool) - 1) / (count - 1)
		}
		result.Members = append(result.Members, pool[index])
	}
	return result
}

func researchCohortLabel(label string) string {
	label = strings.TrimSuffix(strings.TrimSpace(label), "概念")
	switch label {
	case "锂离子电池", "动力电池":
		return "锂电池"
	default:
		return label
	}
}

func researchCohortPriority(input Input, basis string) (int, string) {
	if strings.HasPrefix(basis, "行业概念：") {
		parts := strings.SplitN(basis, " / ", 2)
		if len(parts) == 2 {
			score, scope := researchCohortPriority(input, "概念目录："+parts[1])
			if scope == "business_concept_directory" {
				return score + 100, "industry_business_concept_intersection"
			}
		}
		return 15, "industry_concept_fallback"
	}
	if strings.HasPrefix(basis, "行业：") {
		industry := strings.TrimPrefix(basis, "行业：")
		if researchBroadIndustry(industry) {
			return 25, "broad_industry_fallback"
		}
		return 300, "industry_directory"
	}
	label := strings.TrimPrefix(basis, "概念目录：")
	business := input.Business + "\n" + input.BusinessDetail
	if label == "锂电池" && containsAnyFold(business, "锂电池", "锂离子电池", "磷酸铁锂", "三元材料", "动力电池", "电芯") {
		return 250, "business_concept_directory"
	}
	if businessTextSupportsTheme(business, label) {
		return 200, "business_concept_directory"
	}
	return 10, "concept_directory_fallback"
}

func researchBroadIndustry(industry string) bool {
	switch strings.TrimSpace(industry) {
	case "电源设备", "电力设备", "电气设备", "电子元件", "化学制品", "专用设备", "通用设备", "机械设备", "电子", "基础化工", "有色金属":
		return true
	default:
		return false
	}
}

type researchPeerPerformance struct {
	Symbol        string             `json:"symbol"`
	Name          string             `json:"name"`
	EndDate       string             `json:"end_date"`
	SampleDays    int                `json:"sample_days"`
	Aligned       bool               `json:"date_aligned"`
	Returns       map[string]float64 `json:"returns_percent"`
	PriorDrawdown *float64           `json:"max_drawdown_before_last_5_sessions_percent,omitempty"`
	DataSource    string             `json:"data_source,omitempty"`
	SourceURL     string             `json:"source_url,omitempty"`
	Bars          []AIDailyBar       `json:"daily_bars,omitempty"`
}

type researchPeerWindow struct {
	BaseDate    string  `json:"base_date"`
	StartDate   string  `json:"start_date"`
	EndDate     string  `json:"end_date"`
	SampleSize  int     `json:"sample_size"`
	RisingCount int     `json:"rising_count"`
	MeanReturn  float64 `json:"equal_weight_mean_return_percent"`
	StockReturn float64 `json:"stock_return_percent"`
	StockExcess float64 `json:"stock_excess_percentage_points"`
}

func researchMarketBars(lines []foundation.KLine, cutoff time.Time) []AIDailyBar {
	filtered := make([]foundation.KLine, 0, len(lines))
	for _, line := range lines {
		if !line.Time.IsZero() && !line.Time.After(cutoff) && line.Close > 0 && finite(line.Close) {
			filtered = append(filtered, line)
		}
	}
	return compactDailyBars(normalizeKLines(filtered), ResearchPeerDailyBars)
}

// Returns require every target trading date, including the base close. A
// suspension or stale peer must not silently shift the comparison window.
func researchAlignedReturns(bars, target []AIDailyBar) map[string]float64 {
	closes := map[string]float64{}
	for _, bar := range bars {
		closes[bar.Date] = bar.Close
	}
	result := map[string]float64{}
	for _, days := range []int{1, 2, 3, 4, 5, 20, 60, 120} {
		if len(target) <= days {
			continue
		}
		window := target[len(target)-days-1:]
		complete := true
		for _, bar := range window {
			if closes[bar.Date] <= 0 {
				complete = false
				break
			}
		}
		if complete {
			result[fmt.Sprintf("%dd", days)] = round2(percentChange(closes[window[0].Date], closes[window[len(window)-1].Date]))
		}
	}
	return result
}

func researchPriorDrawdown(bars []AIDailyBar) *float64 {
	if len(bars) < 25 {
		return nil
	}
	peak, drawdown := bars[0].Close, 0.0
	for _, bar := range bars[:len(bars)-5] {
		peak = math.Max(peak, bar.Close)
		drawdown = math.Min(drawdown, percentChange(peak, bar.Close))
	}
	value := round2(drawdown)
	return &value
}

func researchRelatedThemes(input Input) []foundation.ThemeOverview {
	terms := append([]string{input.Industry}, input.Concepts...)
	result := []foundation.ThemeOverview{}
	scores := map[string]int{}
	for _, theme := range input.Themes {
		score := 0
		for _, term := range terms {
			if !validBusinessThemeLabel(term) {
				continue
			}
			for _, name := range append([]string{theme.Name}, theme.Aliases...) {
				if strings.TrimSpace(term) == strings.TrimSpace(name) {
					weight := 100
					if term == input.Industry {
						weight = 200
					}
					score = max(score, weight)
				} else if canonicalTheme(term) == canonicalTheme(name) {
					score = max(score, 80)
				} else if themeMatches(term, foundation.ThemeOverview{Name: name}) {
					score = max(score, 40)
				}
			}
		}
		if score > 0 {
			result = append(result, theme)
			scores[theme.Theme+"|"+theme.Name] = score
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := scores[result[i].Theme+"|"+result[i].Name], scores[result[j].Theme+"|"+result[j].Name]
		if left != right {
			return left > right
		}
		if result[i].TrendScore != result[j].TrendScore {
			return result[i].TrendScore > result[j].TrendScore
		}
		return result[i].Name < result[j].Name
	})
	return result[:min(4, len(result))]
}

// These are provider theme-node statistics, not a constituent-weighted index.
// Carry-forward can keep an old node's move under a newer trade-date label.
func researchThemeMetric(theme foundation.ThemeOverview, date string) map[string]any {
	aligned := date != "" && theme.TradeDate == date
	status := "current_nodes"
	switch {
	case theme.CarryForward:
		status = "carried_forward_nodes"
	case theme.Provisional:
		status = "provisional_nodes"
	case !aligned:
		status = "date_mismatch"
	}
	return map[string]any{
		"name": theme.Name, "metric_scope": "theme_nodes_not_sector_index",
		"node_change_percent": theme.ChangePercent, "trade_date": theme.TradeDate,
		"date_aligned": aligned, "data_status": status,
		"usable_for_current_move": aligned && !theme.CarryForward && !theme.Provisional,
		"rising_nodes":            theme.RisingNodes, "falling_nodes": theme.FallingNodes,
		"matched_nodes": theme.MatchedNodes, "total_nodes": theme.TotalNodes,
		"limit_up_count": theme.LimitUpCount, "data_source": theme.Source,
		"carry_forward": theme.CarryForward, "provisional": theme.Provisional,
	}
}

type researchMoveWindow struct {
	Window    string `json:"window"`
	Direction string `json:"direction"`
	Sessions  int    `json:"sessions"`
	BaseDate  string `json:"base_date"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

func researchRecentMoveWindow(target []AIDailyBar) researchMoveWindow {
	result := researchMoveWindow{}
	for i := len(target) - 1; i > 0 && result.Sessions < 5; i-- {
		direction := "up"
		if target[i].Close < target[i-1].Close {
			direction = "down"
		} else if target[i].Close == target[i-1].Close {
			break
		}
		if result.Direction != "" && result.Direction != direction {
			break
		}
		result.Direction = direction
		result.Sessions++
		result.BaseDate = target[i-1].Date
		result.StartDate = target[i].Date
	}
	if result.Sessions > 0 {
		result.Window = fmt.Sprintf("%dd", result.Sessions)
		result.EndDate = target[len(target)-1].Date
	}
	return result
}

func researchMarketContext(input Input, cutoff time.Time) map[string]any {
	target := researchMarketBars(input.KLines, cutoff)
	date := ""
	if len(target) > 0 {
		date = target[len(target)-1].Date
	}
	peers := []researchPeerPerformance{}
	windows := map[string]researchPeerWindow{}
	stockReturns := researchAlignedReturns(target, target)
	reboundCount, reboundSample, twoDayRebounds := 0, 0, 0
	for _, member := range input.ResearchPeers.Members {
		bars := researchMarketBars(member.KLines, cutoff)
		if len(bars) == 0 {
			continue
		}
		peer := researchPeerPerformance{Symbol: member.Symbol, Name: member.Name, EndDate: bars[len(bars)-1].Date, SampleDays: len(bars), Bars: bars, Returns: map[string]float64{}}
		peer.Aligned = peer.EndDate == date
		if peer.Aligned {
			peer.Returns = researchAlignedReturns(bars, target)
			peer.PriorDrawdown = researchPriorDrawdown(bars)
		}
		if len(member.KLines) > 0 {
			peer.DataSource = member.KLines[len(member.KLines)-1].Meta.Source
			peer.SourceURL = member.KLines[len(member.KLines)-1].Meta.SourceURL
		}
		peers = append(peers, peer)
		for key, value := range peer.Returns {
			item := windows[key]
			item.SampleSize++
			item.MeanReturn += value
			if value > 0 {
				item.RisingCount++
			}
			windows[key] = item
		}
		if fiveDay, ok := peer.Returns["5d"]; ok && peer.PriorDrawdown != nil {
			reboundSample++
			if fiveDay > 0 && *peer.PriorDrawdown <= -20 {
				reboundCount++
			}
			if twoDay, ok := peer.Returns["2d"]; ok && twoDay > 0 && *peer.PriorDrawdown <= -20 {
				twoDayRebounds++
			}
		}
	}
	for key, item := range windows {
		var days int
		_, _ = fmt.Sscanf(key, "%dd", &days)
		item.BaseDate = target[len(target)-days-1].Date
		item.StartDate = target[len(target)-days].Date
		item.EndDate = date
		item.MeanReturn = round2(item.MeanReturn / float64(item.SampleSize))
		item.StockReturn = stockReturns[key]
		item.StockExcess = round2(item.StockReturn - item.MeanReturn)
		windows[key] = item
	}
	themes := []map[string]any{}
	for _, theme := range researchRelatedThemes(input) {
		if parsed, err := time.Parse("2006-01-02", theme.TradeDate); err == nil && parsed.After(cutoff) {
			continue
		}
		themes = append(themes, researchThemeMetric(theme, date))
	}
	recent := researchRecentMoveWindow(target)
	windowNote := "归因必须匹配所解释异动的起止日期；最近连续涨跌窗口仅描述量价，不证明事件起点或因果。"
	if short, ok := windows[recent.Window]; ok && recent.Sessions < 5 {
		if five, ok := windows["5d"]; ok {
			windowNote += fmt.Sprintf("最近%d日%d/%d上涨，近5日%d/%d上涨；5日累计下跌不能否定最近%d日共同回升，也不能仅凭个股超额证明公告是主要原因。", recent.Sessions, short.RisingCount, short.SampleSize, five.RisingCount, five.SampleSize, recent.Sessions)
		}
	}
	return map[string]any{
		"as_of": date, "industry": input.Industry, "related_themes": themes,
		"stock_returns_percent": stockReturns, "stock_max_drawdown_before_last_5_sessions_percent": researchPriorDrawdown(target),
		"peer_group": input.ResearchPeers.Basis, "candidate_count": input.ResearchPeers.CandidateCount,
		"selection_scope":      input.ResearchPeers.SelectionScope,
		"requested_peer_count": len(input.ResearchPeers.Members), "available_peer_count": len(peers), "peers": peers, "windows": windows,
		"prior_drawdown_20pct_and_5d_rebound_count": reboundCount, "prior_drawdown_sample_size": reboundSample,
		"prior_drawdown_20pct_and_2d_rebound_count": twoDayRebounds,
		"recent_move_window":                        recent, "window_interpretation": windowNote,
		"return_definition": "N日收益=末日收盘/此前第N个交易日收盘-1，需要N+1个收盘；base_date是基准收盘日，start_date至end_date是N个收益交易日。",
		"scope":             "优先目标主营匹配的细分目录，按代码分散取样，未按涨幅挑选；概念成员的业务纯度未逐一核实，宽行业回退不能代表细分板块；样本均值不是板块指数，题材节点不是全板块上涨家数；收益为百分比，超额为百分点。",
		"caution":           "概念归属不证明业务受益，量价同步不证明因果或资金净流入；日期不齐不合并，盘中日线未完成；前期回撤与最近5日回升仅支持反弹假设，非固定超跌定义。",
	}
}

func compactResearchMarketSource(source ResearchSource, policy researchLevelPolicy) ResearchSource {
	var value map[string]any
	if json.Unmarshal([]byte(source.Content), &value) != nil {
		return source
	}
	peers, _ := value["peers"].([]any)
	for _, item := range peers {
		if peer, ok := item.(map[string]any); ok {
			delete(peer, "daily_bars")
			delete(peer, "source_url")
		}
	}
	limit := policy.MaxEvidenceBytes / 6
	if limit == 0 {
		limit = 6000
	}
	if policy.DailyBars == 60 {
		focus := ""
		if recent, ok := value["recent_move_window"].(map[string]any); ok {
			focus, _ = recent["window"].(string)
		}
		windows := map[string]any{}
		if original, ok := value["windows"].(map[string]any); ok {
			for key, item := range original {
				if key != "2d" && key != "5d" && key != focus {
					continue
				}
				if window, ok := item.(map[string]any); ok {
					windows[key] = map[string]any{
						"base_date": window["base_date"], "start_date": window["start_date"],
						"end_date":    window["end_date"],
						"sample_size": window["sample_size"], "rising_count": window["rising_count"],
						"mean_percent":  window["equal_weight_mean_return_percent"],
						"stock_percent": window["stock_return_percent"], "excess_pp": window["stock_excess_percentage_points"],
					}
				}
			}
		}
		themes := []any{}
		if original, ok := value["related_themes"].([]any); ok && len(original) > 0 {
			if theme, ok := original[0].(map[string]any); ok {
				compact := map[string]any{}
				for _, key := range []string{"name", "trade_date", "node_change_percent", "metric_scope", "data_status", "usable_for_current_move", "carry_forward", "provisional"} {
					compact[key] = theme[key]
				}
				themes = append(themes, compact)
			}
		}
		value = map[string]any{
			"as_of": value["as_of"], "peer_group": value["peer_group"], "selection_scope": value["selection_scope"],
			"candidate_count": value["candidate_count"], "available_peer_count": value["available_peer_count"],
			"recent_move_window": map[string]any{"window": focus}, "windows": windows, "related_themes": themes,
			"scope": "目录样本非全板块；均值与题材节点非指数；沿用节点不确认当期行情。",
		}
		// Quick research retains aggregate event/5d comparisons before names;
		// the complete member list and bars remain in the frozen snapshot.
		peers = nil
	}

	for {
		value["peers"] = peers
		value["displayed_peer_count"] = len(peers)
		encoded, _ := json.Marshal(value)
		if len(encoded) <= limit || len(peers) == 0 {
			if len(encoded) > limit && policy.DailyBars != 60 {
				// Keep event/5d/swing windows and their original dates and
				// aggregates. Extra horizons must not crowd out company facts.
				limitResearchMarketWindows(value)
				encoded, _ = json.Marshal(value)
			}
			source.Content = string(encoded)
			return source
		}
		peers = peers[:len(peers)-1]
	}
}

func limitResearchMarketWindows(value map[string]any) {
	focus := ""
	if recent, ok := value["recent_move_window"].(map[string]any); ok {
		focus, _ = recent["window"].(string)
	}
	if windows, ok := value["windows"].(map[string]any); ok {
		for key := range windows {
			if key != focus && key != "2d" && key != "5d" && key != "20d" {
				delete(windows, key)
			}
		}
	}
	value["window_note"] = "只展示异动窗口及2/5/20日对照，未展示的窗口不是缺失或无共振"
}
