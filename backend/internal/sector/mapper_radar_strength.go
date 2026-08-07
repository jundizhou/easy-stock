package sector

import (
	"context"
	"fmt"
	"strings"

	"easy-stock/backend/internal/foundation"
)

// radarThemeConstituents builds several mapped theme pools from one boards and
// stock-catalog snapshot, avoiding a full Mapper.Build network cycle per theme.
func (m *Mapper) radarThemeConstituents(ctx context.Context, themeIDs []string) (map[string][]foundation.BoardStock, error) {
	boards, boardErr, entries, catalogErr := m.loadBoardsAndCatalog(ctx)
	if boardErr != nil && len(entries) == 0 {
		return nil, fmt.Errorf("board list unavailable: %v; stock catalog unavailable: %w", boardErr, catalogErr)
	}
	catalog := newCatalogIndex(entries)
	result := make(map[string][]foundation.BoardStock, len(themeIDs))
	for _, themeID := range themeIDs {
		theme, ok := FindTheme(strings.TrimSpace(themeID))
		if !ok {
			continue
		}
		stocks := []foundation.BoardStock{}
		for _, group := range theme.Groups {
			for _, node := range group.Nodes {
				nodeStocks := catalogStocksForNode(catalog, node, 0)
				if len(nodeStocks) == 0 {
					if board, _, matched := matchBoard(node, boards); matched {
						if fallbackStocks, err := m.provider.BoardStocks(ctx, board.Code, 200); err == nil {
							nodeStocks = fallbackStocks
						}
					}
				}
				stocks = append(stocks, nodeStocks...)
			}
		}
		result[themeID] = uniqueBoardStocks(stocks)
	}
	return result, nil
}
