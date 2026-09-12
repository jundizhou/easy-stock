package eastmoney

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// marketQuoteRow 是全市场快照里单只股票所需的字段。
type marketQuoteRow struct {
	symbol       string
	name         string
	close        float64
	changePct    float64
	amount       float64
	turnoverRate float64
	volumeRatio  float64
	totalCapYi   float64
	floatCapYi   float64
	pe           float64
	pb           float64
	mainInflow   float64
	fiveDayPct   float64
	sixtyDayPct  float64
}

// snapshotFields 是全市场快照请求的字段清单：
// 代码/名称/现价/涨跌幅/成交额/换手率/量比/总市值/流通市值/PE/PB/主力净流入/5日涨幅/60日涨幅。
const snapshotFields = "f12,f14,f2,f3,f6,f8,f10,f20,f21,f9,f23,f62,f109,f24"

// MarketSnapshot 拉取沪深 A 股全市场快照并聚合为看板所需的广度统计。
//
// 东财 clist 单页上限 100 条，全市场约 5500 只需要约 56 页；以小并发池 +
// 页间限速在 3 秒左右取完，调用方（httpapi）再做 45 秒级缓存，避免高频轮询
// 打到上游。fltt=2 让涨跌幅/价格直接以浮点返回（无需再除以 100 的精度换算）。
func (c *Client) MarketSnapshot(ctx context.Context) (foundation.MarketBreadth, error) {
	const pageSize = 100
	const workers = 6
	start := time.Now()

	firstURL := c.snapshotPageURL(1, pageSize)
	var firstPayload struct {
		RC   int `json:"rc"`
		Data struct {
			Total int              `json:"total"`
			Diff  []map[string]any `json:"diff"`
		} `json:"data"`
	}
	if err := c.getJSONWithRetry(ctx, firstURL, &firstPayload); err != nil {
		return foundation.MarketBreadth{}, fmt.Errorf("eastmoney market snapshot unavailable: %w", err)
	}
	if firstPayload.RC != 0 || len(firstPayload.Data.Diff) == 0 {
		return foundation.MarketBreadth{}, fmt.Errorf("eastmoney market snapshot unavailable: rc=%d", firstPayload.RC)
	}
	total := firstPayload.Data.Total
	if total <= 0 {
		return foundation.MarketBreadth{}, fmt.Errorf("eastmoney market snapshot: empty market")
	}
	pageCount := (total + pageSize - 1) / pageSize
	if pageCount > 80 {
		pageCount = 80 // 上游 total 异常放大时的保护上限
	}

	rows := make([]marketQuoteRow, 0, total)
	var mu sync.Mutex
	rows = append(rows, parseSnapshotRows(firstPayload.Data.Diff)...)

	// 页号 2..pageCount 交给并发池；每页之间由 worker 内部限速，整体速率
	// 约 workers/interval，远低于东财 clist 的风控阈值。
	type job struct{ page int }
	jobs := make(chan job)
	var wg sync.WaitGroup
	fetchErr := error(nil)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				select {
				case <-ctx.Done():
					return
				case <-time.After(120 * time.Millisecond):
				}
				var payload struct {
					RC   int `json:"rc"`
					Data struct {
						Diff []map[string]any `json:"diff"`
					} `json:"data"`
				}
				if err := c.getJSONWithRetry(ctx, c.snapshotPageURL(j.page, pageSize), &payload); err != nil {
					mu.Lock()
					if fetchErr == nil {
						fetchErr = err
					}
					mu.Unlock()
					continue
				}
				batch := parseSnapshotRows(payload.Data.Diff)
				mu.Lock()
				rows = append(rows, batch...)
				mu.Unlock()
			}
		}()
	}
	for page := 2; page <= pageCount; page++ {
		jobs <- job{page: page}
	}
	close(jobs)
	wg.Wait()
	if len(rows) == 0 {
		if fetchErr != nil {
			return foundation.MarketBreadth{}, fmt.Errorf("eastmoney market snapshot pages failed: %w", fetchErr)
		}
		return foundation.MarketBreadth{}, fmt.Errorf("eastmoney market snapshot: no rows")
	}

	meta := foundation.SourceMeta{
		Source:    "eastmoney:market-snapshot",
		SourceURL: firstURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	return aggregateSnapshot(rows, meta), nil
}

func (c *Client) snapshotPageURL(page, pageSize int) string {
	params := standardListParams(pageSize)
	params.Set("pn", strconv.Itoa(page))
	params.Set("po", "1")
	params.Set("fid", "f3")
	params.Set("fs", "m:0+t:6,m:0+t:80,m:1+t:2,m:1+t:23")
	params.Set("fields", snapshotFields)
	return c.quoteBaseURL + "/api/qt/clist/get?" + params.Encode()
}

func parseSnapshotRows(diff []map[string]any) []marketQuoteRow {
	rows := make([]marketQuoteRow, 0, len(diff))
	for _, raw := range diff {
		symbol := asString(raw["f12"])
		if symbol == "" {
			continue
		}
		change := asFloat(raw["f3"])
		// 东财用 "-" 表示停牌/无数据，asFloat 已归零；按涨跌幅为 0 会误入"平"档，
		// 价格也为 0 时直接丢弃该行。
		if asFloat(raw["f2"]) == 0 && change == 0 {
			continue
		}
		rows = append(rows, marketQuoteRow{
			symbol:       symbol,
			name:         asString(raw["f14"]),
			close:        asFloat(raw["f2"]),
			changePct:    change,
			amount:       asFloat(raw["f6"]),
			turnoverRate: asFloat(raw["f8"]),
			volumeRatio:  asFloat(raw["f10"]),
			totalCapYi:   asFloat(raw["f20"]) / 1e8,
			floatCapYi:   asFloat(raw["f21"]) / 1e8,
			pe:           asFloat(raw["f9"]),
			pb:           asFloat(raw["f23"]),
			mainInflow:   asFloat(raw["f62"]),
			fiveDayPct:   asFloat(raw["f109"]),
			sixtyDayPct:  asFloat(raw["f24"]),
		})
	}
	return rows
}

// aggregateSnapshot 把全市场行情聚合为看板统计：广度、分布、成交、换手与四个榜单。
func aggregateSnapshot(rows []marketQuoteRow, meta foundation.SourceMeta) foundation.MarketBreadth {
	buckets := []struct {
		label    string
		positive bool
		match    func(pct float64) bool
	}{
		{"≤-7", false, func(p float64) bool { return p <= -7 }},
		{"-7~-5", false, func(p float64) bool { return p > -7 && p <= -5 }},
		{"-5~-3", false, func(p float64) bool { return p > -5 && p <= -3 }},
		{"-3~-1", false, func(p float64) bool { return p > -3 && p <= -1 }},
		{"-1~0", false, func(p float64) bool { return p > -1 && p < 0 }},
		{"0~1", true, func(p float64) bool { return p >= 0 && p < 1 }},
		{"1~3", true, func(p float64) bool { return p >= 1 && p < 3 }},
		{"3~5", true, func(p float64) bool { return p >= 3 && p < 5 }},
		{"5~7", true, func(p float64) bool { return p >= 5 && p < 7 }},
		{">7", true, func(p float64) bool { return p >= 7 }},
	}

	breadth := foundation.MarketBreadth{Meta: meta, AsOf: time.Now()}
	breadth.Distribution = make([]foundation.MarketDistributionBucket, len(buckets))
	for i, bucket := range buckets {
		breadth.Distribution[i] = foundation.MarketDistributionBucket{Label: bucket.label, Positive: bucket.positive}
	}

	percents := make([]float64, 0, len(rows))
	sumPct, sumAmount, sumTurnover := 0.0, 0.0, 0.0
	for _, row := range rows {
		breadth.Total++
		pct := row.changePct
		percents = append(percents, pct)
		sumPct += pct
		sumAmount += row.amount
		sumTurnover += row.turnoverRate
		switch {
		case pct > 0:
			breadth.Up++
		case pct < 0:
			breadth.Down++
		default:
			breadth.Flat++
		}
		if pct >= 3 {
			breadth.StrongUp++
		}
		if pct <= -3 {
			breadth.StrongDown++
		}
		if row.turnoverRate >= 10 {
			breadth.HighTurnover++
		}
		if row.turnoverRate >= 5 && row.close > 0 {
			breadth.HighVolCount++
		}
		for i, bucket := range buckets {
			if bucket.match(pct) {
				breadth.Distribution[i].Count++
				break
			}
		}
	}
	if breadth.Total > 0 {
		breadth.UpRatio = float64(breadth.Up) / float64(breadth.Total) * 100
		breadth.AvgPct = sumPct / float64(breadth.Total)
		breadth.AvgAmount = sumAmount / float64(breadth.Total)
		breadth.AvgTurnover = sumTurnover / float64(breadth.Total)
	}
	sort.Float64s(percents)
	if len(percents) > 0 {
		breadth.MedianPct = percents[len(percents)/2]
	}
	breadth.TotalAmount = sumAmount

	sorted := make([]marketQuoteRow, len(rows))
	// 涨幅榜
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].changePct > sorted[j].changePct })
	breadth.TopGainers = snapshotTop(sorted, 8)
	// 跌幅榜
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].changePct < sorted[j].changePct })
	breadth.TopLosers = snapshotTop(sorted, 8)
	// 成交额榜
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].amount > sorted[j].amount })
	breadth.TurnoverLeaders = snapshotTop(sorted, 8)
	// 活跃换手榜
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].turnoverRate > sorted[j].turnoverRate })
	breadth.ActiveLeaders = snapshotTop(sorted, 8)
	breadth.Rows = toQuoteRows(rows)
	return breadth
}

// toQuoteRows 把内部行转换为对外的全量筛选行。
func toQuoteRows(rows []marketQuoteRow) []foundation.MarketQuoteRow {
	out := make([]foundation.MarketQuoteRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, foundation.MarketQuoteRow{
			Symbol:        row.symbol,
			Name:          row.name,
			Close:         row.close,
			ChangePercent: row.changePct,
			Amount:        row.amount,
			TurnoverRate:  row.turnoverRate,
			VolumeRatio:   row.volumeRatio,
			TotalCapYi:    row.totalCapYi,
			FloatCapYi:    row.floatCapYi,
			PE:            row.pe,
			PB:            row.pb,
			MainInflow:    row.mainInflow,
			FiveDayPct:    row.fiveDayPct,
			SixtyDayPct:   row.sixtyDayPct,
		})
	}
	return out
}

// MarketSnapshotRows 返回全市场快照的完整筛选字段行，供策略选股引擎使用。
// 行数据与 MarketSnapshot 同源（一次拉取同时供聚合与筛选）。
func (c *Client) MarketSnapshotRows(ctx context.Context) ([]foundation.MarketQuoteRow, error) {
	breadth, err := c.MarketSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return breadth.Rows, nil
}

func snapshotTop(rows []marketQuoteRow, limit int) []foundation.MarketSnapshotStock {
	if len(rows) > limit {
		rows = rows[:limit]
	}
	items := make([]foundation.MarketSnapshotStock, 0, len(rows))
	for _, row := range rows {
		if row.close == 0 {
			continue
		}
		items = append(items, foundation.MarketSnapshotStock{
			Symbol: row.symbol, Name: row.name, Close: row.close,
			ChangePercent: row.changePct, Amount: row.amount, TurnoverRate: row.turnoverRate,
		})
	}
	return items
}
