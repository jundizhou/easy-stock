package sector

import (
	"easy-stock/backend/internal/foundation"
)

// industryBreadth carries the advancing/declining counts of one industry board.
type industryBreadth struct {
	Rising  int
	Falling int
}

// industryBreadthByBoard maps normalized industry board names to rising/falling
// counts taken from the EastMoney industry list.
//
// The radar's industry momentum source is Tencent's ranking, which only carries
// change percent and the leading stock — no breadth at all — so EastMoney's
// board list (f104/f105) is read separately and merged into those rows.
func industryBreadthByBoard(index *boardIndex) map[string]industryBreadth {
	if index == nil || len(index.boards) == 0 {
		return nil
	}
	result := make(map[string]industryBreadth, len(index.boards))
	for _, board := range index.boards {
		if board.RisingCount == 0 && board.FallingCount == 0 {
			continue
		}
		// 与 lookupByIndustryName 的查询侧保持一致，使用同一套归一化。
		key := normalizeIndustryKey(board.Name)
		if key == "" {
			continue
		}
		current := result[key]
		current.Rising += board.RisingCount
		current.Falling += board.FallingCount
		result[key] = current
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// industryNameAliases lists the names an industry row may be known by across
// sources: its own board name plus the mapped EastMoney board name.
func industryNameAliases(item foundation.MarketIndustryMomentum) []string {
	names := []string{item.Name}
	if mapping, ok := lookupRadarIndustryMapping(item.Code, item.Name); ok && mapping.EastMoneyBoardName != "" {
		names = append(names, mapping.EastMoneyBoardName)
	}
	return names
}

// applyIndustryBreadth returns a copy of the momentum rows with rising/falling
// counts filled from the EastMoney board list wherever the primary source left
// them empty, so breadth feeds both the display and the radar score.
func applyIndustryBreadth(items []foundation.MarketIndustryMomentum, breadth map[string]industryBreadth) []foundation.MarketIndustryMomentum {
	if len(items) == 0 || len(breadth) == 0 {
		return items
	}
	enriched := make([]foundation.MarketIndustryMomentum, len(items))
	copy(enriched, items)
	filled := false
	for index := range enriched {
		if enriched[index].RisingCount != 0 || enriched[index].FallingCount != 0 {
			continue
		}
		value, ok := lookupByIndustryName(breadth, industryNameAliases(enriched[index])...)
		if !ok {
			continue
		}
		enriched[index].RisingCount = value.Rising
		enriched[index].FallingCount = value.Falling
		filled = true
	}
	if !filled {
		return items
	}
	return enriched
}
