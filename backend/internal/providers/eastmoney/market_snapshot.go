package eastmoney

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// marketSnapshotPageSize 是一次 clist 请求能拿到的行数。
// 上游 2026-10 起把 pz 静默截到 100：pz 传 1000 也只回 100 行，
// 按大页翻页会拿到 100 行就当"最后一页"提前收工，全市场快照只剩 100 只票。
// 所以这里按 100 翻页，靠 total 与"短页"两个信号判断何时停止。
const marketSnapshotPageSize = 100

// A 股约 5400-6000 只，60 页上下；62 页封顶防新上市超预期。
const marketSnapshotMaxPages = 62

// 并发 6 路：串行 60 页要十几秒，引擎快照缓存只有 10 秒；
// 6 路实测 2 秒出头拉完全市场。
const marketSnapshotWorkers = 6

// marketSnapshotFields 的字段号与 push2 clist 的约定一一对应：
// f12 代码 f13 市场 f14 名称 f2 现价 f3 涨跌幅 f15/f16/f17/f18 高低开昨收，
// f5 成交量 f6 成交额 f8 换手 f10 量比 f20/f21 总市值/流通市值。
const marketSnapshotFields = "f12,f13,f14,f2,f3,f15,f16,f17,f18,f5,f6,f8,f10,f20,f21"

// marketSnapshotFS 覆盖沪深主板、创业板、科创板与北交所全部 A 股。
const marketSnapshotFS = "m:0+t:6,m:0+t:80,m:1+t:2,m:1+t:23,m:0+t:81+s:2048"

// MarketSnapshot 拉取当前全市场 A 股快照。
//
// 翻页按代码排序（fid=f12）：涨跌幅排序盘中每一页都在变，
// 按它翻页会把同一只票重复取到、把另一只漏掉。
//
// 监控引擎每十几秒调用一次，因此这里不做任何缓存：缓存属于引擎，
// 引擎知道自己上一轮拿到的是什么、这次要不要重试。
func (c *Client) MarketSnapshot(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	start := time.Now()
	total := -1
	firstURL := ""

	// 第一页串行取：既要拿到 total 决定后面翻几页，也是对源可用性的探针。
	first, payloadTotal, firstURL, err := c.snapshotPage(ctx, 1)
	if err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	if payloadTotal > 0 {
		total = payloadTotal
	}
	rows := make([]foundation.MarketSnapshotRow, 0, 6000)
	rows = append(rows, first...)
	lastPage := marketSnapshotMaxPages
	if total > 0 {
		if need := (total + marketSnapshotPageSize - 1) / marketSnapshotPageSize; need < lastPage {
			lastPage = need
		}
	}

	// 剩余页 6 路并发：单页 ~40KB，串行 60 页要十几秒，引擎缓存只有 10 秒。
	stale := false
	var staleReason string
	results := make([][]foundation.MarketSnapshotRow, lastPage)
	errs := make([]error, lastPage)
	for batchStart := 2; batchStart <= lastPage; batchStart += marketSnapshotWorkers {
		batchEnd := batchStart + marketSnapshotWorkers - 1
		if batchEnd > lastPage {
			batchEnd = lastPage
		}
		var wg sync.WaitGroup
		for pageNo := batchStart; pageNo <= batchEnd; pageNo++ {
			wg.Add(1)
			go func(pageNo int) {
				defer wg.Done()
				pageRows, _, _, pageErr := c.snapshotPage(ctx, pageNo)
				results[pageNo-1] = pageRows
				errs[pageNo-1] = pageErr
			}(pageNo)
		}
		wg.Wait()
	}
	for pageNo := 2; pageNo <= lastPage; pageNo++ {
		if errs[pageNo-1] != nil {
			// 少几页只损覆盖面，不值得让整轮扫描作废；
			// meta 上打 Stale 让调用方知道缺了东西。
			stale = true
			staleReason = errs[pageNo-1].Error()
			continue
		}
		rows = append(rows, results[pageNo-1]...)
	}
	if stale {
		// 中途整页失败时上下页之间可能漏票：去重不做排序补齐，
		// 宁可少一只也不返回重复行污染统计。
		rows = dedupeSnapshotRows(rows)
	}

	if len(rows) == 0 {
		if staleReason != "" {
			return nil, foundation.SourceMeta{}, fmt.Errorf("eastmoney market snapshot unavailable: %s", staleReason)
		}
		return nil, foundation.SourceMeta{}, fmt.Errorf("eastmoney market snapshot unavailable")
	}
	tradeDate := time.Now().In(foundation.AStockLocation).Format("2006-01-02")
	for index := range rows {
		rows[index].TradeDate = tradeDate
	}
	meta := foundation.SourceMeta{
		Source:         "eastmoney:clist",
		SourceURL:      firstURL,
		FetchedAt:      time.Now(),
		LatencyMS:      time.Since(start).Milliseconds(),
		TradeDate:      tradeDate,
		Stale:          stale,
		FallbackReason: staleReason,
	}
	// 覆盖面明显缺斤短两（比如只拿到前几页）也算 Stale：
	// 调用方拿着 300 行去评「全市场异动」会得出错误结论。
	if total > 0 && len(rows) < total/2 {
		meta.Stale = true
	}
	return rows, meta, nil
}

// snapshotPage 取一页快照。返回值依次为：行、上游 total、请求 URL、错误。
func (c *Client) snapshotPage(ctx context.Context, page int) ([]foundation.MarketSnapshotRow, int, string, error) {
	endpoint := c.quoteBaseURL + "/api/qt/clist/get"
	params := standardListParams(marketSnapshotPageSize)
	params.Set("pn", strconv.Itoa(page))
	params.Set("fs", marketSnapshotFS)
	params.Set("fields", marketSnapshotFields)
	// fid=f12 按代码升序翻页：盘中排序值一直在变，按涨跌幅翻页必漏必重。
	params.Set("fid", "f12")
	params.Set("po", "1")
	requestURL := endpoint + "?" + params.Encode()
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			Total int              `json:"total"`
			Diff  []map[string]any `json:"diff"`
		} `json:"data"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, 0, requestURL, err
	}
	if payload.RC != 0 {
		return nil, 0, requestURL, fmt.Errorf("eastmoney clist rc=%d (page %d)", payload.RC, page)
	}
	rows := make([]foundation.MarketSnapshotRow, 0, len(payload.Data.Diff))
	for _, item := range payload.Data.Diff {
		if row, ok := parseMarketSnapshotRow(item); ok {
			rows = append(rows, row)
		}
	}
	return rows, payload.Data.Total, requestURL, nil
}

func dedupeSnapshotRows(rows []foundation.MarketSnapshotRow) []foundation.MarketSnapshotRow {
	seen := make(map[string]bool, len(rows))
	unique := rows[:0]
	for _, row := range rows {
		if seen[row.Symbol] {
			continue
		}
		seen[row.Symbol] = true
		unique = append(unique, row)
	}
	return unique
}

// 上游对缺失字段会给出 "-" 或 null，asFloat 对两者都返回 0——
// 0 价格的行（还没开盘数据的一条错误行）在这里被丢弃。
func parseMarketSnapshotRow(item map[string]any) (foundation.MarketSnapshotRow, bool) {
	symbol := strings.TrimSpace(asString(item["f12"]))
	name := strings.TrimSpace(asString(item["f14"]))
	if symbol == "" || name == "" {
		return foundation.MarketSnapshotRow{}, false
	}
	price := asFloat(item["f2"])
	volume := asFloat(item["f5"])
	row := foundation.MarketSnapshotRow{
		Symbol:        symbol,
		Name:          name,
		Board:         foundation.DetectBoard(symbol, name),
		Price:         price,
		ChangePercent: asFloat(item["f3"]),
		Open:          asFloat(item["f17"]),
		High:          asFloat(item["f15"]),
		Low:           asFloat(item["f16"]),
		PreviousClose: asFloat(item["f18"]),
		Volume:        volume,
		Amount:        asFloat(item["f6"]),
		TurnoverRate:  asFloat(item["f8"]),
		VolumeRatio:   asFloat(item["f10"]),
		MarketCap:     asFloat(item["f20"]),
		FloatCap:      asFloat(item["f21"]),
		IsST:          foundation.IsST(name),
		// 停牌的表现是一整天没有成交且没有价格；盘中 9:15 前 volume 也是 0，
		// 但那时价格同样未生成，两个条件叠加才足以判定停牌。
		Suspended: price <= 0 && asFloat(item["f17"]) <= 0 && volume <= 0,
	}
	return row, true
}
