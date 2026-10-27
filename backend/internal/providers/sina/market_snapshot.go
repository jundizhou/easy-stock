package sina

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// 新浪全市场列表（Market_Center.getHQNodeData）每页上限 100 行，
// 用它兜底东财 clist：push2.eastmoney.com 在部分网络整段不可达时，
// 监控引擎若拿不到全市场快照，异动页就只能扫持仓/关注仓，
// 「异动名单」永远空着——那不是没有异动，而是根本没扫市场。
const (
	sinaSnapshotPageSize = 100
	// 沪深京 A 股约 5400 只，55 页封顶；留一点余量防止新上市超预期。
	sinaSnapshotMaxPages = 62
	// 并发 6 路：单页 ~40KB，串行取 55 页要十几秒，引擎缓存只有 10 秒。
	sinaSnapshotWorkers = 6
)

// MarketSnapshot 拉取全市场 A 股快照（新浪口径）。
//
// 翻页按代码升序而不是按涨跌幅：盘中涨跌幅排序每次请求都在变，
// 按它翻页会重复取到同一只、同时漏掉另一只。
func (c *Client) MarketSnapshot(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	start := time.Now()
	rows := make([]foundation.MarketSnapshotRow, 0, 6000)
	meta := foundation.SourceMeta{Source: "sina:market_center"}
	firstURL := ""

	page := 1
	for page <= sinaSnapshotMaxPages {
		batch := make([]int, 0, sinaSnapshotWorkers)
		for len(batch) < sinaSnapshotWorkers && page <= sinaSnapshotMaxPages {
			batch = append(batch, page)
			page++
		}
		results := make([][]foundation.MarketSnapshotRow, len(batch))
		errs := make([]error, len(batch))
		var wg sync.WaitGroup
		for index, pageNo := range batch {
			wg.Add(1)
			go func(index, pageNo int) {
				defer wg.Done()
				results[index], errs[index] = c.snapshotPage(ctx, pageNo)
			}(index, pageNo)
		}
		wg.Wait()

		short := false
		for index := range batch {
			if errs[index] != nil {
				if len(rows) == 0 && index == 0 {
					return nil, foundation.SourceMeta{}, errs[index]
				}
				// 已经拿到一部分就先用一部分：少几页会让覆盖面变小，
				// 但比整页报「快照不可用」好；meta 上标 Stale 让调用方知道。
				meta.Stale = true
				meta.FallbackReason = errs[index].Error()
				short = true
				continue
			}
			if firstURL == "" {
				firstURL = c.snapshotPageURL(batch[index])
			}
			rows = append(rows, results[index]...)
			if len(results[index]) < sinaSnapshotPageSize {
				short = true
			}
		}
		if short {
			break
		}
	}

	if len(rows) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("sina market snapshot unavailable")
	}

	// 同一只票理论上只会出现一次；真出现重复时保留第一条，
	// 后面的页可能是翻页边界上的补位行。
	seen := make(map[string]bool, len(rows))
	unique := rows[:0]
	for _, row := range rows {
		if seen[row.Symbol] {
			continue
		}
		seen[row.Symbol] = true
		unique = append(unique, row)
	}
	rows = unique

	tradeDate := time.Now().In(foundation.AStockLocation).Format("2006-01-02")
	for index := range rows {
		rows[index].TradeDate = tradeDate
	}
	meta.TradeDate = tradeDate
	meta.SourceURL = firstURL
	if meta.FetchedAt.IsZero() {
		meta.FetchedAt = time.Now()
	}
	meta.LatencyMS = time.Since(start).Milliseconds()
	return rows, meta, nil
}

func (c *Client) snapshotPageURL(page int) string {
	values := url.Values{}
	values.Set("page", strconv.Itoa(page))
	values.Set("num", strconv.Itoa(sinaSnapshotPageSize))
	values.Set("sort", "symbol")
	values.Set("asc", "1")
	values.Set("node", "hs_a")
	return c.listBaseURL + "?" + values.Encode()
}

func (c *Client) snapshotPage(ctx context.Context, page int) ([]foundation.MarketSnapshotRow, error) {
	requestURL := c.snapshotPageURL(page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sina market snapshot http status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(decodeSinaBody(body, resp.Header.Get("Content-Type"))))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return nil, nil
	}
	var items []sinaListNode
	if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
		return nil, fmt.Errorf("decode sina market snapshot page %d: %w", page, err)
	}
	rows := make([]foundation.MarketSnapshotRow, 0, len(items))
	for _, item := range items {
		if row, ok := parseSinaSnapshotRow(item); ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// sinaListNode 用 json.Number 接住数值：新浪同一字段在不同板块
// 有时给数字、有时给字符串，直接声明成 float64 会在改版时整页解不出来。
type sinaListNode map[string]any

func sinaNumber(value any) float64 {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0
		}
		return parsed
	case float64:
		return typed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func parseSinaSnapshotRow(item sinaListNode) (foundation.MarketSnapshotRow, bool) {
	raw := sinaString(item["symbol"])
	name := sinaString(item["name"])
	if raw == "" || name == "" {
		return foundation.MarketSnapshotRow{}, false
	}
	symbol, err := foundation.NormalizeSymbol(raw)
	if err != nil {
		return foundation.MarketSnapshotRow{}, false
	}
	price := sinaNumber(item["trade"])
	open := sinaNumber(item["open"])
	volume := sinaNumber(item["volume"])
	return foundation.MarketSnapshotRow{
		Symbol:        symbol.Canonical,
		Name:          name,
		Board:         foundation.DetectBoard(symbol.Canonical, name),
		Price:         price,
		ChangePercent: sinaNumber(item["changepercent"]),
		Open:          open,
		High:          sinaNumber(item["high"]),
		Low:           sinaNumber(item["low"]),
		PreviousClose: sinaNumber(item["settlement"]),
		// 新浪 volume 单位是股，与东财 f5 一致；amount 单位是元。
		Volume:       volume,
		Amount:       sinaNumber(item["amount"]),
		TurnoverRate: sinaNumber(item["turnoverratio"]),
		// 该接口不提供量比，留 0（规则里的量比条件在兜底源下不生效，
		// 但不会因此误判——0 不会越过任何「量比 ≥ x」的条件）。
		VolumeRatio: 0,
		// mktcap/nmc 单位是万元，统一换成元。
		MarketCap: sinaNumber(item["mktcap"]) * 10000,
		FloatCap:  sinaNumber(item["nmc"]) * 10000,
		IsST:      foundation.IsST(name),
		Suspended: price <= 0 && open <= 0 && volume <= 0,
	}, true
}
