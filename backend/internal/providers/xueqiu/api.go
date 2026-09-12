package xueqiu

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// hotStockTypeCN 是雪球热榜的 A 股市场类型码（global=10 us=11 cn=12 hk=13）。
const hotStockTypeCN = "12"

// HotStocks 返回雪球 A 股热榜（按社区讨论热度排名）。上游 5 分钟更新一次。
func (c *Client) HotStocks(ctx context.Context, size int) ([]HotStock, error) {
	if size <= 0 {
		size = 20
	}
	if size > 50 {
		size = 50
	}
	payload, err := c.getJSON(ctx, stockBase, "/v5/stock/hot_stock/list.json", map[string]string{
		"type": hotStockTypeCN,
		"size": strconv.Itoa(size),
	}, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	items := payloadItems(payload, "items")
	list := make([]HotStock, 0, len(items))
	for index, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		list = append(list, HotStock{
			Symbol:     text(entry["symbol"]),
			Name:       text(entry["name"]),
			Current:    num(entry["current"]),
			Percent:    num(entry["percent"]),
			Rank:       index + 1,
			RankChange: int(num(entry["rank_change"])),
			Increase:   num(entry["increment"]),
			Exchange:   text(entry["exchange"]),
		})
	}
	return list, nil
}

// HotUsers 返回讨论某只 A 股最热的雪球用户（KOL）。
// symbol 接受 easy-stock 规范形（600519.SH）或雪球形（SH600519）。
func (c *Client) HotUsers(ctx context.Context, symbol string, count int) ([]HotUser, error) {
	snowballSymbol, err := toSnowballSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if count <= 0 {
		count = 8
	}
	if count > 20 {
		count = 20
	}
	payload, err := c.getJSON(ctx, siteBase, "/recommend/user/stock_hot_user.json", map[string]string{
		"symbol": snowballSymbol,
		"start":  "0",
		"count":  strconv.Itoa(count),
	}, time.Hour)
	if err != nil {
		return nil, err
	}
	// 该端点返回裸数组（无包裹对象）
	items, _ := payload.([]any)
	list := make([]HotUser, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		list = append(list, HotUser{
			ID:          idNum(entry["id"]),
			ScreenName:  text(entry["screen_name"]),
			Description: text(entry["description"]),
			Followers:   int64(num(entry["followers_count"])),
			Statuses:    int64(num(entry["status_count"])),
			Verified:    boolVal(entry["verified"]),
		})
	}
	return list, nil
}

// News 返回雪球 7×24 快讯（A股/宏观）；Mark==1 表示重要。按时间降序。
func (c *Client) News(ctx context.Context, count int) ([]News, error) {
	if count <= 0 {
		count = 20
	}
	if count > 50 {
		count = 50
	}
	payload, err := c.getJSON(ctx, siteBase, "/statuses/livenews/list.json", map[string]string{
		"since_id": "-1",
		"max_id":   "-1",
		"count":    strconv.Itoa(count),
	}, 2*time.Minute)
	if err != nil {
		return nil, err
	}
	items := payloadItems(payload, "items")
	list := make([]News, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		list = append(list, News{
			ID:        idNum(entry["id"]),
			Text:      text(entry["text"]),
			Mark:      int(num(entry["mark"])),
			CreatedAt: int64(num(entry["created_at"])),
			Target:    text(entry["target"]),
		})
	}
	return list, nil
}

// toSnowballSymbol 把 easy-stock 规范代码（600519.SH）转成雪球代码（SH600519）。
// 已是雪球形（SH/SZ/BJ 前缀）则原样返回。
func toSnowballSymbol(symbol string) (string, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(symbol))
	if strings.HasPrefix(trimmed, "SH") || strings.HasPrefix(trimmed, "SZ") || strings.HasPrefix(trimmed, "BJ") {
		if len(trimmed) == 8 {
			return trimmed, nil
		}
	}
	if len(symbol) == 9 && symbol[6] == '.' {
		switch suffix := strings.ToUpper(symbol[7:]); suffix {
		case "SH", "SZ", "BJ":
			return suffix + symbol[:6], nil
		}
	}
	if len(symbol) == 6 {
		switch {
		case symbol[0] == '6':
			return "SH" + symbol, nil
		case symbol[0] == '8' || symbol[0] == '4':
			return "BJ" + symbol, nil
		default:
			return "SZ" + symbol, nil
		}
	}
	return "", fmt.Errorf("无效股票代码 %q", symbol)
}

// payloadItems 从响应载荷取出条目数组：兼容 {data:{items:[]}}、{items:[]} 与裸数组三种形态。
func payloadItems(payload any, path ...string) []any {
	switch typed := payload.(type) {
	case []any:
		return typed
	case map[string]any:
		for _, key := range path {
			if inner, ok := typed[key].([]any); ok {
				return inner
			}
		}
		if data, ok := typed["data"].(map[string]any); ok {
			for _, key := range path {
				if inner, ok := data[key].([]any); ok {
					return inner
				}
			}
		}
		if data, ok := typed["data"].([]any); ok {
			return data
		}
	}
	return nil
}

func text(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func num(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case json.Number:
		n, _ := v.Float64()
		return n
	case string:
		n, _ := strconv.ParseFloat(v, 64)
		return n
	default:
		return 0
	}
}

func idNum(value any) json.Number {
	switch v := value.(type) {
	case float64:
		return json.Number(strconv.FormatInt(int64(v), 10))
	case json.Number:
		return v
	case string:
		return json.Number(v)
	default:
		return json.Number("0")
	}
}

func boolVal(value any) bool {
	b, _ := value.(bool)
	return b
}

// Post 一条雪球个股讨论帖（正文已清 HTML 并截断）。
type Post struct {
	ID         json.Number `json:"id"`
	Text       string      `json:"text"`
	ReplyCount int         `json:"reply_count"`
	ViewCount  int64       `json:"view_count"`
	CreatedAt  int64       `json:"created_at"`
	Target     string      `json:"target,omitempty"`
	UserName   string      `json:"user_name,omitempty"`
	UserFollow int64       `json:"user_followers,omitempty"`
}

// StockPosts 按关键词（通常传股票名称）搜索雪球社区讨论帖，按时间相关度混合返回。
func (c *Client) StockPosts(ctx context.Context, query string, count int) ([]Post, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("搜索关键词为空")
	}
	if count <= 0 {
		count = 10
	}
	if count > 30 {
		count = 30
	}
	payload, err := c.getJSON(ctx, siteBase, "/query/v1/search/status.json", map[string]string{
		"q":     query,
		"count": strconv.Itoa(count),
	}, 2*time.Minute)
	if err != nil {
		return nil, err
	}
	items, _ := payload.([]any)
	list := make([]Post, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		user, _ := entry["user"].(map[string]any)
		list = append(list, Post{
			ID:         idNum(entry["id"]),
			Text:       stripTags(text(entry["description"]), text(entry["text"])),
			ReplyCount: int(num(entry["reply_count"])),
			ViewCount:  int64(num(entry["view_count"])),
			CreatedAt:  int64(num(entry["created_at"])),
			Target:     text(entry["target"]),
			UserName:   text(user["screen_name"]),
			UserFollow: int64(num(user["followers_count"])),
		})
	}
	return list, nil
}

// stripTags 清 HTML 标签并压缩空白，取非空的那个字段（description 优先于 text），截断 200 字。
func stripTags(fields ...string) string {
	content := ""
	for _, field := range fields {
		if strings.TrimSpace(field) != "" {
			content = field
			break
		}
	}
	if content == "" {
		return ""
	}
	var builder strings.Builder
	depth := false
	for _, char := range content {
		switch {
		case char == '<':
			depth = true
		case char == '>':
			depth = false
			builder.WriteRune(' ')
		case !depth:
			builder.WriteRune(char)
		}
	}
	cleaned := strings.Join(strings.Fields(builder.String()), " ")
	runes := []rune(cleaned)
	if len(runes) > 200 {
		return string(runes[:200]) + "…"
	}
	return cleaned
}
