package duanxianxia

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

type rotationResponse struct {
	First string   `json:"first"`
	Dates []string `json:"dates"`
	Rows  []struct {
		Rank  int `json:"rank"`
		Cells []struct {
			Code     string      `json:"code"`
			Name     string      `json:"name"`
			Strength json.Number `json:"strong"`
		} `json:"cells"`
	} `json:"rows"`
}

func (rotation rotationResponse) themes() (string, []Theme, error) {
	if len(rotation.Dates) == 0 {
		return "", nil, fmt.Errorf("rotation has no trade dates")
	}
	var current []Theme
	history := make(map[string][]RankPoint)
	for column, date := range rotation.Dates {
		if _, err := time.Parse(time.DateOnly, date); err != nil {
			return "", nil, fmt.Errorf("invalid rotation trade date %q", date)
		}
		for _, row := range rotation.Rows {
			if row.Rank <= 0 || column >= len(row.Cells) {
				continue
			}
			cell := row.Cells[column]
			code, name := strings.TrimSpace(cell.Code), strings.TrimSpace(cell.Name)
			if code == "" || name == "" {
				continue
			}
			strength, err := cell.Strength.Float64()
			if err != nil {
				return "", nil, fmt.Errorf("invalid rotation strength for %s on %s: %w", code, date, err)
			}
			history[code] = append(history[code], RankPoint{TradeDate: date, Rank: row.Rank, Strength: strength})
			if column == 0 {
				current = append(current, Theme{Code: code, Name: name, Rank: row.Rank, Strength: strength})
			}
		}
	}
	if len(current) == 0 {
		return "", nil, fmt.Errorf("rotation has no current themes")
	}
	sort.SliceStable(current, func(i, j int) bool { return current[i].Rank < current[j].Rank })
	if rotation.First != "" && current[0].Code != rotation.First {
		return "", nil, fmt.Errorf("rotation first code %s does not match rank one %s", rotation.First, current[0].Code)
	}
	for i := range current {
		current[i].History = history[current[i].Code]
	}
	return rotation.Dates[0], current, nil
}

type leadersResponse struct {
	Result string `json:"result"`
	Data   []struct {
		Date   string `json:"date"`
		Stocks []struct {
			Code string `json:"code"`
			Name string `json:"name"`
			Role string `json:"long"`
		} `json:"stocks"`
	} `json:"data"`
}

func (response leadersResponse) leadersOn(date string) ([]Leader, error) {
	if response.Result != "success" {
		return nil, fmt.Errorf("unexpected leader result %q", response.Result)
	}
	for _, day := range response.Data {
		if day.Date != date {
			continue
		}
		if day.Stocks == nil {
			return nil, fmt.Errorf("missing leader stocks for %s", date)
		}
		leaders := make([]Leader, 0, len(day.Stocks))
		for i, stock := range day.Stocks {
			symbol, err := foundation.NormalizeSymbol(stock.Code)
			if err != nil {
				return nil, fmt.Errorf("normalize leader symbol %q: %w", stock.Code, err)
			}
			name := strings.TrimSpace(stock.Name)
			if name == "" {
				return nil, fmt.Errorf("missing leader name for %s", stock.Code)
			}
			role := strings.TrimSpace(stock.Role)
			if role == "" {
				role = fmt.Sprintf("龙%d", i+1)
			}
			leaders = append(leaders, Leader{Rank: i + 1, Role: role, Symbol: symbol.Canonical, Name: name})
		}
		return leaders, nil
	}
	return nil, fmt.Errorf("leader response has no entry for %s", date)
}
