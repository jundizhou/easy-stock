// Package stockdb 的 K 线取数层：日/周/月线 + 前复权，作为 easy-stock 的
// 单一正确本地获取方法（free-stockdb 引擎，127.0.0.1:7899）。
//
// 设计要点：
//   - stockdb 引擎的 周k/月k 表未预存历史（实测返回 []），因此周/月线一律由日线
//     在本地重采样（ResamplePeriod），与股票 SDK 的内部逻辑同构；
//   - 前复权由 复权:code 表的累计因子 cum 还原：qfq[t] = raw[t] * cum[t] / cum[latest]，
//     参考点=最新价，与 Python get_bars(fq="qfq") 同口径；
//   - 所有方法都建立在 RawQuery + 本地解析之上，避免重复造轮子，且可被 shannon
//     策略、个股分析、行情接口等任意消费方复用。
package stockdb

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Bar 是一根标准 K 线（与 Python core/stockdb.py 的取数口径一致）。
// 它被定义为 stockdb 包的共享类型，shannon 包以类型别名复用，避免各自维护一份。
type Bar struct {
	Date   int     `json:"date"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
	Name   string  `json:"name,omitempty"`
}

// DefaultDailyBars 是日线取数的默认根数：L3 多周期对齐需要周线/月线各至少
// 2*SWING_K+3 根才能给出有效状态。stockdb 的 日k:code 一次性返回全部历史，
// count 仅做本地裁剪、不增加传输量，故默认拉 2000 根（覆盖 ~1900 交易日）再在
// 本地重采样出周/月线。
const DefaultDailyBars = 2000

// dailyCacheTTL 是本地取数缓存的默认过期时间（仅对启用了 WithCache 的客户端生效）。
const dailyCacheTTL = 30 * time.Second

type rawBar struct {
	Date   int     `json:"date"`
	Open   float64 `json:"open"`
	Close  float64 `json:"close"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Volume float64 `json:"volume"`
	Name   string  `json:"name"`
}

// WithCache 启用本地取数结果的短时效缓存（默认 30s），用于减少单只或多只代码
// 在相同时间窗内重复拉取相同表的回环请求（如多次扫描、开发热重载）。
// stockdb 是只读的日线快照数据源，30s 内的陈旧对日级分析无影响。
func WithCache(ttl time.Duration) Option {
	return func(c *Client) {
		if ttl > 0 {
			c.cache = newQueryCache(ttl)
		}
	}
}

// RawBars 拉取指定周期的 K 线，升序返回最近 count 根（已剔除停牌与无效行）。
func (c *Client) RawBars(ctx context.Context, table, code string, count int) ([]Bar, error) {
	raw, err := c.RawQuery(ctx, table+":"+code)
	if err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("stockdb: decode %s %q: %w", table, code, err)
	}
	out := make([]Bar, 0, len(rows))
	for _, row := range rows {
		var pair [2]json.RawMessage
		if err := json.Unmarshal(row, &pair); err != nil {
			continue
		}
		var value rawBar
		if err := json.Unmarshal(pair[1], &value); err != nil {
			continue
		}
		if !validBar(value) {
			continue
		}
		out = append(out, Bar{
			Date:   value.Date,
			Open:   value.Open,
			High:   value.High,
			Low:    value.Low,
			Close:  value.Close,
			Volume: value.Volume,
			Name:   value.Name,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	if count > 0 && len(out) > count {
		out = out[len(out)-count:]
	}
	return out, nil
}

// DailyBars 日线（已按 复权 表的累计因子还原为前复权价，与 Python get_bars(fq="qfq") 同口径）。
// 若已构建多源池（WithDailySource 注入了远程源），则走 MultiSource 做健康感知的
// 负载均衡 + 故障转移；否则仅取本地 7899。
func (c *Client) DailyBars(ctx context.Context, code string, count int) ([]Bar, error) {
	if c.multi != nil {
		return c.multi.DailyBars(ctx, code, count)
	}
	return c.localDailyBars(ctx, code, count)
}

// localDailyBars 仅从本地 7899 取日线（前复权）。是多源池主源的取数实现，也是未
// 配置远程源时的回落路径。
func (c *Client) localDailyBars(ctx context.Context, code string, count int) ([]Bar, error) {
	bars, err := c.RawBars(ctx, "日k", code, count)
	if err != nil {
		return nil, err
	}
	recs, _ := c.adjFactors(ctx, code) // 取数失败则视为无分红，raw 即 qfq
	return applyQFQ(bars, recs), nil
}

// WeeklyBars 周线（由日线重采样，避免依赖 stockdb 未预存的 周k 表）。
func (c *Client) WeeklyBars(ctx context.Context, code string, count int) ([]Bar, error) {
	d, err := c.DailyBars(ctx, code, count)
	if err != nil {
		return nil, err
	}
	return c.WeeklyFrom(d), nil
}

// MonthlyBars 月线（由日线重采样）。
func (c *Client) MonthlyBars(ctx context.Context, code string, count int) ([]Bar, error) {
	d, err := c.DailyBars(ctx, code, count)
	if err != nil {
		return nil, err
	}
	return c.MonthlyFrom(d), nil
}

// WeeklyFrom 用日线重采样出周线（ISO 周聚合）。
func (c *Client) WeeklyFrom(d []Bar) []Bar { return ResamplePeriod(d, "week") }

// MonthlyFrom 用日线重采样出月线（自然月聚合）。
func (c *Client) MonthlyFrom(d []Bar) []Bar { return ResamplePeriod(d, "month") }

// ResamplePeriod 把日线聚合成周线（period="week"）或月线（period="month"）。
// 聚合规则：每周期首根的开盘、末根的收盘、区间最高/最低、成交量求和。
// 返回升序排列的聚合 K 线，与 Python 股票 SDK 的周/月线重采样同构。
func ResamplePeriod(daily []Bar, period string) []Bar {
	if len(daily) == 0 {
		return nil
	}
	loc := time.UTC
	out := make([]Bar, 0, len(daily)/5)
	var (
		open, high, low, closeL, vol float64
		date                         int
		name                         string
		curKey                       int
		has                          bool
	)
	for _, b := range daily {
		t := parseDate(b.Date, loc)
		var key int
		if period == "week" {
			y, w := t.ISOWeek()
			key = y*100 + w
		} else {
			key = t.Year()*100 + int(t.Month())
		}
		if !has || key != curKey {
			if has {
				out = append(out, Bar{
					Date: date, Open: open, High: high, Low: low,
					Close: closeL, Volume: vol, Name: name,
				})
			}
			open, high, low, closeL, vol = b.Open, b.High, b.Low, b.Close, b.Volume
			date, name, curKey, has = b.Date, b.Name, key, true
		} else {
			if b.High > high {
				high = b.High
			}
			if b.Low < low {
				low = b.Low
			}
			closeL, date, name = b.Close, b.Date, b.Name
			vol += b.Volume
		}
	}
	if has {
		out = append(out, Bar{
			Date: date, Open: open, High: high, Low: low,
			Close: closeL, Volume: vol, Name: name,
		})
	}
	return out
}

// factorRec 是一条分红/转增的累计复权因子记录。
type factorRec struct {
	Date int     // 除权除息日（YYYYMMDD）
	Cum  float64 // 累计复权因子（相对于上市日，单调递增）
}

// adjFactors 读取 stockdb 的 复权:code 表，提取每条记录的除权日与累计因子 cum，升序返回。
// 无分红记录的标的返回空切片（此时 raw 价即前复权价）。
func (c *Client) adjFactors(ctx context.Context, code string) ([]factorRec, error) {
	raw, err := c.RawQuery(ctx, "复权:"+code)
	if err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, nil
	}
	recs := make([]factorRec, 0, len(rows))
	for _, row := range rows {
		var pair [2]json.RawMessage
		if err := json.Unmarshal(row, &pair); err != nil {
			continue
		}
		var key string
		if err := json.Unmarshal(pair[0], &key); err != nil {
			continue
		}
		var val struct {
			Cum float64 `json:"cum"`
		}
		if err := json.Unmarshal(pair[1], &val); err != nil {
			continue
		}
		d, ok := factorDateOf(key)
		if !ok {
			continue
		}
		recs = append(recs, factorRec{Date: d, Cum: val.Cum})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Date < recs[j].Date })
	return recs, nil
}

// factorDateOf 从形如 "复权:600519:20020725" 的 key 中提取末段 8 位日期。
func factorDateOf(key string) (int, bool) {
	idx := strings.LastIndex(key, ":")
	if idx < 0 || len(key)-idx-1 < 8 {
		return 0, false
	}
	v, err := strconv.Atoi(key[idx+1 : idx+9])
	if err != nil {
		return 0, false
	}
	return v, true
}

// applyQFQ 把 raw 价按累计因子还原为前复权价：qfq[t] = raw[t] * cum[t] / cum[latest]。
// 最新一根 K 线 cum == cum[latest]，故其 qfq == raw（前复权参考点=最新，与 Python 一致）。
// 无分红记录（recs 为空）时原样返回。
func applyQFQ(bars []Bar, recs []factorRec) []Bar {
	if len(recs) == 0 {
		return bars
	}
	cumLatest := recs[len(recs)-1].Cum
	out := make([]Bar, len(bars))
	rf := 0 // 指向下一个 Date > 当前 bar.Date 的因子
	for i, b := range bars {
		for rf < len(recs) && recs[rf].Date <= b.Date {
			rf++
		}
		cum := cumLatest
		if rf > 0 {
			cum = recs[rf-1].Cum
		}
		k := cum / cumLatest
		out[i] = Bar{
			Date:   b.Date,
			Name:   b.Name,
			Open:   b.Open * k,
			High:   b.High * k,
			Low:    b.Low * k,
			Close:  b.Close * k,
			Volume: b.Volume,
		}
	}
	return out
}

// parseDate 把 YYYYMMDD 整数解析为 time.Time（UTC，仅用于周期分组）。
func parseDate(v int, loc *time.Location) time.Time {
	s := fmt.Sprintf("%08d", v)
	y, _ := strconv.Atoi(s[0:4])
	mo, _ := strconv.Atoi(s[4:6])
	d, _ := strconv.Atoi(s[6:8])
	return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, loc)
}

// validBar 判定一根 K 线是否有效（排除停牌/无价格）。
// 停牌常见特征：四价相等且成交量为 0。
func validBar(v rawBar) bool {
	if v.Open <= 0 || v.High <= 0 || v.Low <= 0 || v.Close <= 0 {
		return false
	}
	if v.Open == v.High && v.High == v.Low && v.Low == v.Close && v.Volume == 0 {
		return false
	}
	return true
}

// queryCache 是 RawQuery 结果的短时效缓存（仅对启用了 WithCache 的客户端生效）。
type queryCache struct {
	ttl  time.Duration
	mu   sync.Mutex
	data map[string]cacheEntry
}

type cacheEntry struct {
	body  []byte
	expAt time.Time
}

func newQueryCache(ttl time.Duration) *queryCache {
	if ttl <= 0 {
		ttl = dailyCacheTTL
	}
	return &queryCache{ttl: ttl, data: map[string]cacheEntry{}}
}

// get 命中且未过期时返回缓存体。
func (q *queryCache) get(key string) ([]byte, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.data[key]
	if !ok || time.Now().After(e.expAt) {
		return nil, false
	}
	return e.body, true
}

// put 写入缓存（仅缓存成功响应）。
func (q *queryCache) put(key string, body []byte) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.data[key] = cacheEntry{body: body, expAt: time.Now().Add(q.ttl)}
}
