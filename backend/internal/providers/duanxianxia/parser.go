package duanxianxia

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"golang.org/x/net/html"
)

var numericText = regexp.MustCompile("[-+]?\\d+(?:\\.\\d+)?")

func ParseRotationHTML(fragment string) (string, []Theme, error) {
	document, err := html.Parse(strings.NewReader("<html><body><table><tbody>" + fragment + "</tbody></table></body></html>"))
	if err != nil {
		return "", nil, fmt.Errorf("parse rotation html: %w", err)
	}
	rows := findElements(document, "tr")
	if len(rows) < 2 {
		return "", nil, fmt.Errorf("rotation html has no ranked rows")
	}
	headerCells := directElementChildren(rows[0], "td")
	if len(headerCells) < 2 {
		return "", nil, fmt.Errorf("rotation html has no trade dates")
	}
	dates := make([]string, 0, len(headerCells)-1)
	for _, cell := range headerCells[1:] {
		value := strings.TrimSpace(textContent(cell))
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return "", nil, fmt.Errorf("invalid rotation trade date %q", value)
		}
		dates = append(dates, value)
	}

	type pointMap map[string]RankPoint
	history := map[string]pointMap{}
	current := make([]Theme, 0, 10)
	for _, row := range rows[1:] {
		cells := directElementChildren(row, "td")
		if len(cells) < 2 {
			continue
		}
		rank, err := strconv.Atoi(strings.TrimSpace(textContent(cells[0])))
		if err != nil || rank <= 0 {
			continue
		}
		for column := 1; column < len(cells) && column <= len(dates); column++ {
			cell := cells[column]
			code := strings.TrimSpace(attr(cell, "code"))
			name := strings.TrimSpace(attr(cell, "name"))
			if code == "" || name == "" {
				continue
			}
			strength := lastNumber(textContent(cell))
			if history[code] == nil {
				history[code] = pointMap{}
			}
			history[code][dates[column-1]] = RankPoint{TradeDate: dates[column-1], Rank: rank, Strength: strength}
			if column == 1 {
				current = append(current, Theme{Code: code, Name: name, Rank: rank, Strength: strength})
			}
		}
	}
	if len(current) == 0 {
		return "", nil, fmt.Errorf("rotation html has no current themes")
	}
	sort.SliceStable(current, func(i, j int) bool { return current[i].Rank < current[j].Rank })
	for index := range current {
		points := make([]RankPoint, 0, len(dates))
		for _, date := range dates {
			if point, ok := history[current[index].Code][date]; ok {
				points = append(points, point)
			}
		}
		current[index].History = points
	}
	return dates[0], current, nil
}

func ParseLeadersHTML(fragment string) ([]Leader, bool, error) {
	document, err := html.Parse(strings.NewReader("<html><body><table><tbody><tr>" + fragment + "</tr></tbody></table></body></html>"))
	if err != nil {
		return nil, false, fmt.Errorf("parse leader html: %w", err)
	}
	rows := findElements(document, "tr")
	if len(rows) == 0 {
		return nil, false, fmt.Errorf("leader html has no row")
	}
	cells := directElementChildren(rows[0], "td")
	if len(cells) == 0 {
		return nil, false, fmt.Errorf("leader html has no cells")
	}
	currentCell := cells[0]
	if len(cells) > 1 {
		currentCell = cells[1]
	}
	items := findElementsWithClass(currentCell, "div", "kline")
	leaders := make([]Leader, 0, len(items))
	for index, item := range items {
		rawCode := strings.TrimSpace(attr(item, "code"))
		normalized, err := foundation.NormalizeSymbol(rawCode)
		if err != nil {
			return nil, false, fmt.Errorf("normalize leader symbol %q: %w", rawCode, err)
		}
		role := fmt.Sprintf("龙%d", index+1)
		for _, span := range findElements(item, "span") {
			if value := strings.TrimSpace(textContent(span)); value != "" {
				role = value
				break
			}
		}
		name := strings.TrimSpace(textContent(item))
		name = strings.TrimSpace(strings.TrimPrefix(name, role))
		if name == "" {
			continue
		}
		leaders = append(leaders, Leader{Rank: index + 1, Role: role, Symbol: normalized.Canonical, Name: name})
	}
	if len(leaders) > 0 {
		return leaders, false, nil
	}
	noLeaders := strings.Contains(strings.TrimSpace(textContent(currentCell)), "当日无领涨")
	return []Leader{}, noLeaders, nil
}

func lastNumber(value string) float64 {
	matches := numericText.FindAllString(value, -1)
	if len(matches) == 0 {
		return 0
	}
	number, _ := strconv.ParseFloat(matches[len(matches)-1], 64)
	return number
}

func findElements(node *html.Node, tag string) []*html.Node {
	items := []*html.Node{}
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.ElementNode && current.Data == tag {
			items = append(items, current)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return items
}

func findElementsWithClass(node *html.Node, tag string, className string) []*html.Node {
	items := []*html.Node{}
	for _, item := range findElements(node, tag) {
		for _, candidate := range strings.Fields(attr(item, "class")) {
			if candidate == className {
				items = append(items, item)
				break
			}
		}
	}
	return items
}

func directElementChildren(node *html.Node, tag string) []*html.Node {
	items := []*html.Node{}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == tag {
			items = append(items, child)
		}
	}
	return items
}

func attr(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

func textContent(node *html.Node) string {
	var builder strings.Builder
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
			builder.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return strings.Join(strings.Fields(builder.String()), " ")
}
