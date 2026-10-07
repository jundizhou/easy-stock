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

// NewClient 创建 stockdb 客户端。
func NewClient(options ...Option) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range options {
		opt(c)
	}
	return c
}

// RawQuery 执行 ?cmd=get&t=<t>&json=1，返回未解析的原始 JSON 字节。
// 适用于响应可能是对象、数组或标量的场景（透传最稳妥）。
func (c *Client) RawQuery(ctx context.Context, t string) ([]byte, error) {
	if strings.TrimSpace(t) == "" {
		return nil, fmt.Errorf("stockdb: empty query key")
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
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stockdb: request %q: %w", t, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("stockdb: read %q: %w", t, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stockdb: %q -> http %d: %s", t, resp.StatusCode, strings.TrimSpace(string(body)))
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

// KLineWeekly 周 K：表 周k:code:date。
func (c *Client) KLineWeekly(ctx context.Context, code, date string) (map[string]any, error) {
	return c.Query(ctx, joinKey("周k", code, date))
}

// KLineMonthly 月 K：表 月k:code:date。
func (c *Client) KLineMonthly(ctx context.Context, code, date string) (map[string]any, error) {
	return c.Query(ctx, joinKey("月k", code, date))
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
