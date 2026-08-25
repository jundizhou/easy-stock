package sector

import (
	"context"
	"fmt"
	"strings"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

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

func mergeFusionIndustryStocks(industryName string, industryMap foundation.SectorMap, result *foundation.SectorMap) {
	if result == nil {
		return
	}
	groups := make([]foundation.SectorMapGroup, 0, len(industryMap.Groups))
	stockCount := 0
	for groupIndex, group := range industryMap.Groups {
		outGroup := group
		outGroup.ID = fmt.Sprintf("fusion_industry_%d_%s", groupIndex, group.ID)
		outGroup.Nodes = make([]foundation.SectorMapNode, 0, len(group.Nodes))
		for nodeIndex, node := range group.Nodes {
			outNode := node
			outNode.ID = fmt.Sprintf("fusion_industry_%d_%d_%s", groupIndex, nodeIndex, node.ID)
			stockCount += len(outNode.Stocks)
			outGroup.Nodes = append(outGroup.Nodes, outNode)
		}
		groups = append(groups, outGroup)
	}
	if stockCount == 0 {
		appendKaipanlaWarning(result, fmt.Sprintf("行业“%s”暂未获得成分股，当前仅展示题材匹配个股。", industryName))
		return
	}
	result.Groups = append(result.Groups, groups...)
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
