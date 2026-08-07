package sector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

func (p *RadarProvider) fallbackOverviews(ctx context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	if p.fallback == nil {
		return nil, foundation.SourceMeta{}, fmt.Errorf("radar fallback provider is unavailable")
	}
	p.fallbackMu.Lock()
	defer p.fallbackMu.Unlock()
	now := time.Now()
	if len(p.fallbackList) > 0 && now.Sub(p.fallbackAt) < p.fallbackTTL {
		return cloneThemeOverviews(p.fallbackList), p.fallbackMeta, nil
	}
	items, meta, err := p.fallback.Overviews(ctx)
	if err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	p.fallbackList = cloneThemeOverviews(items)
	p.fallbackMeta = meta
	p.fallbackAt = now
	return cloneThemeOverviews(items), meta, nil
}

func cloneThemeOverviews(items []foundation.ThemeOverview) []foundation.ThemeOverview {
	result := make([]foundation.ThemeOverview, len(items))
	for index, item := range items {
		result[index] = item
		result[index].Leaders = append([]string(nil), item.Leaders...)
	}
	return result
}

func hasThemeOverview(items []foundation.ThemeOverview, candidate foundation.ThemeOverview) bool {
	for _, item := range items {
		if themeOverviewsEquivalent(item, candidate) {
			return true
		}
	}
	return false
}

func themeOverviewsEquivalent(left foundation.ThemeOverview, right foundation.ThemeOverview) bool {
	if normalizeThemeName(left.Name) == normalizeThemeName(right.Name) && normalizeThemeName(left.Name) != "" {
		return true
	}
	if strings.HasPrefix(left.Theme, "kpl:") {
		return kaipanlaOverviewMatchesLocal(left, right)
	}
	if strings.HasPrefix(right.Theme, "kpl:") {
		return kaipanlaOverviewMatchesLocal(right, left)
	}
	return radarThemeNamesOverlap(left.Name, right.Name)
}

func kaipanlaOverviewMatchesLocal(kaipanla foundation.ThemeOverview, local foundation.ThemeOverview) bool {
	return kaipanlaThemeMatchesOverview(duanxianxia.Theme{
		Code: strings.TrimPrefix(kaipanla.Theme, "kpl:"),
		Name: kaipanla.Name,
	}, local)
}

func kaipanlaThemeMatchesOverview(theme duanxianxia.Theme, overview foundation.ThemeOverview) bool {
	if normalizeThemeName(theme.Name) == normalizeThemeName(overview.Name) && normalizeThemeName(theme.Name) != "" {
		return true
	}
	mapping, exists := lookupRadarThemeMapping(theme.Code, theme.Name)
	if !exists {
		return radarThemeNamesOverlap(theme.Name, overview.Name)
	}
	if mapping.StaticThemeID != "" && strings.TrimSpace(overview.Theme) == mapping.StaticThemeID {
		return true
	}
	if strings.TrimSpace(overview.Theme) == radarMappedThemeID(mapping) {
		return true
	}
	return normalizeThemeName(mapping.EastMoneyName) == normalizeThemeName(overview.Name)
}

func radarThemeNamesOverlap(left string, right string) bool {
	a := normalizeThemeName(left)
	b := normalizeThemeName(right)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	shorter := a
	longer := b
	if len([]rune(shorter)) > len([]rune(longer)) {
		shorter, longer = longer, shorter
	}
	return len([]rune(shorter)) >= 2 && strings.Contains(longer, shorter)
}

func (p *RadarProvider) mergeFallbackStocks(ctx context.Context, theme duanxianxia.Theme, result *foundation.SectorMap) {
	if p.fallback == nil || result == nil {
		return
	}
	fallbackThemeID, mappedName := mappedFallbackThemeID(theme.Code, theme.Name)
	fallbackMap, err := p.fallback.Build(ctx, fallbackThemeID)
	if err != nil {
		appendKaipanlaWarning(result, fmt.Sprintf("开盘啦题材“%s”映射东财题材失败：%s", theme.Name, err.Error()))
		return
	}
	groups, stockCount := mappedFallbackGroups(theme.Name, fallbackMap)
	if stockCount == 0 {
		appendKaipanlaWarning(result, fmt.Sprintf("已映射东财题材“%s”，但暂未获得补充个股。", mappedName))
		return
	}
	result.Groups = append(result.Groups, groups...)
	if !radarContainsString(result.Tabs, "东财映射补充") {
		result.Tabs = append(result.Tabs, "东财映射补充")
	}
	appendKaipanlaWarning(result, fmt.Sprintf("已按题材映射从东财“%s”补充 %d 只候选股。", fallbackMap.Name, stockCount))
}

func radarContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func mappedFallbackGroups(kaipanlaName string, fallbackMap foundation.SectorMap) ([]foundation.SectorMapGroup, int) {
	groups := make([]foundation.SectorMapGroup, 0, len(fallbackMap.Groups))
	stocks := map[string]struct{}{}
	for groupIndex, group := range fallbackMap.Groups {
		outGroup := group
		outGroup.ID = fmt.Sprintf("fallback_%d_%s", groupIndex, group.ID)
		outGroup.Name = "东财映射 · " + group.Name
		outGroup.Nodes = make([]foundation.SectorMapNode, 0, len(group.Nodes))
		for nodeIndex, node := range group.Nodes {
			outNode := node
			outNode.ID = fmt.Sprintf("fallback_%d_%d_%s", groupIndex, nodeIndex, node.ID)
			outNode.Description = strings.TrimSpace("开盘啦“" + kaipanlaName + "”映射至东财题材“" + fallbackMap.Name + "”。 " + node.Description)
			outNode.MatchedBy = append(outNode.MatchedBy, "theme-map:"+kaipanlaName+"→"+fallbackMap.Name)
			if outNode.StockSource == "" {
				outNode.StockSource = "eastmoney:theme-mapping"
			} else if !strings.Contains(outNode.StockSource, "theme-mapping") {
				outNode.StockSource += "+theme-mapping"
			}
			for _, stock := range outNode.Stocks {
				stocks[stock.Symbol] = struct{}{}
			}
			outGroup.Nodes = append(outGroup.Nodes, outNode)
		}
		groups = append(groups, outGroup)
	}
	return groups, len(stocks)
}

func appendKaipanlaWarning(result *foundation.SectorMap, warning string) {
	if result == nil || len(result.Groups) == 0 || len(result.Groups[0].Nodes) == 0 || strings.TrimSpace(warning) == "" {
		return
	}
	result.Groups[0].Nodes[0].Warnings = append(result.Groups[0].Nodes[0].Warnings, warning)
}
