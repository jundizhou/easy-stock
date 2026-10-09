// Package stockdb 封装 free-stockdb 本地行情引擎（默认 http://127.0.0.1:7899）。
//
// free-stockdb 采用 K-V 存储结构：table:keyn:keyn_1 -> value。
// 通过命令式 HTTP 接口访问：
//
//	GET /?cmd=get&t=<table>:<key>[:<subkey>]&json=1
//
// 常用表名（中文，需 URL 编码）：
//   - 日k / 周k / 月k / 分钟k : 行情（分钟 k 的 key 为 14 位 YYYYMMDDHHMMSS）
//   - 复权                    : 分红送转（后复权因子）
//   - 股票代码                : 全市场代码索引（按首字符分组）
//   - 板块 / 指标( zb)        : 板块映射 / 技术指标
//
// 该客户端仅依赖标准库，无第三方依赖。
package stockdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "http://127.0.0.1:7899"
	defaultTimeout = 10 * time.Second
)

// Client 是 free-stockdb 的轻量 HTTP 客户端。
type Client struct {
	baseURL    string
	httpClient *http.Client
	cache      *queryCache   // 可选；启用 WithCache 后用于缓存取数结果
	stats      *Stats        // 调用统计与滚动延迟（镜像 quant-stock 延迟采集）
	remotes   []DailySource  // 可选；远程备用日线源（多接口共用 / 负载均衡 / 故障转移）
	multi     *MultiSource   // 由本地源 + remotes 聚合而成；非空时 DailyBars 走多源池
}

// Option 用于构造 Client 的可选参数。
type Option func(*Client)

// WithBaseURL 设置 stockdb 基址（含协议与端口）。
func WithBaseURL(value string) Option {
	return func(c *Client) {
		if strings.TrimSpace(value) != "" {
			c.baseURL = strings.TrimRight(value, "/")
		}
	}
}

// WithHTTPClient 注入自定义 *http.Client（用于测试或调优超时）。
func WithHTTPClient(value *http.Client) Option {
	return func(c *Client) {
		if value != nil {
			c.httpClient = value
		}
	}
}

// NewClient 创建 stockdb 客户端。若有远程备用源（WithDailySource），则在选项之后
// 自动构建多源池（本地 7899 为主，远程源为备，主源降级时负载均衡 + 故障转移）。
func NewClient(options ...Option) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
		stats:      newStats(),
	}
	for _, opt := range options {
		opt(c)
	}
	if len(c.remotes) > 0 {
		c.multi = NewMultiSource(localDailySource{c}, c.remotes...)
	}
	return c
}

// WithDailySource 追加一个或多个远程日线备用源（多接口共用）。可多次调用以堆叠多个
// 上游，本地源降级时它们按健康 + 轮询分摊流量，任一失败自动切换到下一个。
func WithDailySource(srcs ...DailySource) Option {
	return func(c *Client) {
		for _, src := range srcs {
			if src != nil {
				c.remotes = append(c.remotes, src)
			}
		}
	}
}

// RawQuery 执行 ?cmd=get&t=<t>&json=1，返回未解析的原始 JSON 字节。
// 适用于响应可能是对象、数组或标量的场景（透传最稳妥）。
func (c *Client) RawQuery(ctx context.Context, t string) ([]byte, error) {
	if strings.TrimSpace(t) == "" {
		return nil, fmt.Errorf("stockdb: empty query key")
	}
	if c.cache != nil {
		if b, hit := c.cache.get(t); hit {
			return b, nil
		}
	}
	u := c.baseURL + "/?" + url.Values{
		"cmd":  {"get"},
		"t":    {t},
		"json": {"1"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.stats.record(0, false, err.Error())
		return nil, fmt.Errorf("stockdb: request %q: %w", t, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	elapsed := time.Since(started)
	if err != nil {
		c.stats.record(elapsed.Microseconds(), false, err.Error())
		return nil, fmt.Errorf("stockdb: read %q: %w", t, err)
	}
	if resp.StatusCode != http.StatusOK {
		c.stats.record(elapsed.Microseconds(), false, fmt.Sprintf("http %d", resp.StatusCode))
		return nil, fmt.Errorf("stockdb: %q -> http %d: %s", t, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	c.stats.record(elapsed.Microseconds(), true, "")
	if c.cache != nil && len(body) > 0 {
		c.cache.put(t, body)
	}
	return body, nil
}

// Query 同 RawQuery，但将响应解析为 map[string]any。
// 当 stockdb 返回数组或标量时请改用 RawQuery。
func (c *Client) Query(ctx context.Context, t string) (map[string]any, error) {
	raw, err := c.RawQuery(ctx, t)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("stockdb: decode %q: %w", t, err)
	}
	return out, nil
}

// KLineDaily 日 K：表 日k:code:date。
func (c *Client) KLineDaily(ctx context.Context, code, date string) (map[string]any, error) {
	return c.Query(ctx, joinKey("日k", code, date))
}

// KLineWeekly 周 K：由日线重采样得到（stockdb 的 周k 表未预存历史，实测返回空）。
// 返回最近 count 根周线（前复权口径，与 DailyBars 一致）。
func (c *Client) KLineWeekly(ctx context.Context, code string, count int) ([]Bar, error) {
	return c.WeeklyBars(ctx, code, count)
}

// KLineMonthly 月 K：由日线重采样得到（stockdb 的 月k 表未预存历史，实测返回空）。
// 返回最近 count 根月线（前复权口径，与 DailyBars 一致）。
func (c *Client) KLineMonthly(ctx context.Context, code string, count int) ([]Bar, error) {
	return c.MonthlyBars(ctx, code, count)
}

// KLineMinute 分钟 K：表 分钟k:code:YYYYMMDDHHMMSS。
func (c *Client) KLineMinute(ctx context.Context, code, datetime string) (map[string]any, error) {
	return c.Query(ctx, joinKey("分钟k", code, datetime))
}

// MinuteKLine 单根分钟K。free-stockdb 分钟k 表 volume 单位为股，
// Stamp 为该分钟起始时刻的 14 位时间戳 YYYYMMDDHHMMSS（左端点标签）。
type MinuteKLine struct {
	Stamp  string  `json:"stamp"`
	Open   float64 `json:"open"`
	Close  float64 `json:"close"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Volume float64 `json:"volume"`
	Amount float64 `json:"amount"`
}

type minuteKLineValue struct {
	Date   int64   `json:"date"`
	Open   float64 `json:"open"`
	Close  float64 `json:"close"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Volume float64 `json:"volume"`
	Amount float64 `json:"amount"`
}

// MinuteKLines 拉取一只股票的分钟K全表。
//
// free-stockdb 的子键不支持范围语义（t=分钟k:000001:20250901000000 返回空），
// 因此只能整表返回后由调用方按日期过滤；单只股票全量约十万行，属本地回环请求。
// 缺失字段或时间戳不完整的行会被跳过。
func (c *Client) MinuteKLines(ctx context.Context, code string) ([]MinuteKLine, error) {
	raw, err := c.RawQuery(ctx, joinKey("分钟k", code, ""))
	if err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("stockdb: decode 分钟k %q: %w", code, err)
	}
	out := make([]MinuteKLine, 0, len(rows))
	for _, row := range rows {
		var pair [2]json.RawMessage
		if err := json.Unmarshal(row, &pair); err != nil {
			continue
		}
		var value minuteKLineValue
		if err := json.Unmarshal(pair[1], &value); err != nil {
			continue
		}
		item := MinuteKLine{
			Open:   value.Open,
			Close:  value.Close,
			High:   value.High,
			Low:    value.Low,
			Volume: value.Volume,
			Amount: value.Amount,
		}
		if value.Date > 0 {
			item.Stamp = strconv.FormatInt(value.Date, 10)
		} else {
			var keyText string
			_ = json.Unmarshal(pair[0], &keyText)
			if idx := strings.LastIndex(keyText, ":"); idx >= 0 {
				item.Stamp = keyText[idx+1:]
			}
		}
		if len(item.Stamp) != 14 || item.Open <= 0 || item.Close <= 0 {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

// Adjustment 复权因子：表 复权:code。
func (c *Client) Adjustment(ctx context.Context, code string) (map[string]any, error) {
	return c.Query(ctx, joinKey("复权", code, ""))
}

// Codes 全市场代码索引：表 股票代码。
func (c *Client) Codes(ctx context.Context) (map[string]any, error) {
	return c.Query(ctx, "股票代码")
}

func joinKey(table, key, sub string) string {
	if strings.TrimSpace(sub) == "" {
		return table + ":" + key
	}
	return table + ":" + key + ":" + sub
}

// Probe 主动探测本地源连通性与延迟（镜像 quant-stock 的 sys_probe/net_probe），
// 用于探活与「健康冷却」维护。返回探测耗时（毫秒）与错误。
func (c *Client) Probe(ctx context.Context) (int64, error) {
	started := time.Now()
	_, err := c.RawQuery(ctx, "复权:000001")
	elapsed := time.Since(started)
	if err != nil {
		c.stats.markProbe(false, err.Error())
		return elapsed.Milliseconds(), err
	}
	c.stats.markProbe(true, "")
	return elapsed.Milliseconds(), nil
}

// Stats 返回当前调用统计快照（含滚动 p50/p95 延迟）。
func (c *Client) Stats() StatsSnapshot {
	return c.stats.snapshot()
}

// Healthy 返回本地源是否处于健康（未降级）状态。
func (c *Client) Healthy() bool {
	return c.stats.Healthy()
}

// DailySources 返回日线多源池各源的健康快照（含本地主源与远程备用源）。
// 未配置远程源时仅返回本地源。供 /api/v1/stockdb/status 暴露，便于观察「被封/降级/恢复」。
func (c *Client) DailySources() []SourceStatus {
	if c.multi == nil {
		return []SourceStatus{{Name: "stockdb-local", OK: c.stats.Healthy(), QFQ: true, LatencyMs: c.stats.snapshot().P50Ms}}
	}
	return c.multi.Status()
}
