package shannon

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"easy-stock/backend/internal/providers/stockdb"
)

// 日线取数根数：L3 多周期对齐需要周线/月线各至少 2*SWING_K+3（≥13）根才能
// 给出有效状态而非 UNKNOWN。Python 侧实际有效窗口约 ~1900 个交易日
// （周 160 ≈ 800 日 + 月 90 ≈ 1890 日）。stockdb 的 日k:code 一次性返回全部
// 历史，客户端 count 仅做裁剪、不增加传输量，故直接拉 2000 根，再在本地重采样
// 出周/月线（与股票 SDK 内部逻辑同构）。
const dailyBars = 2000

// Feed 封装行情取数（Free-StockDB，127.0.0.1:7899）。
// 底层取数（日/周/月线 + 前复权 + 周/月重采样）已统一在 providers/stockdb 包，
// 这里仅做策略层的薄封装与选股域方法（全市场代码、板块成分）。
type Feed struct {
	client *stockdb.Client
}

// NewFeed 创建取数器。
func NewFeed(client *stockdb.Client) *Feed {
	return &Feed{client: client}
}

// Daily 日线（前复权口径，来自 stockdb.DailyBars）。
func (f *Feed) Daily(ctx context.Context, code string) ([]Bar, error) {
	return f.client.DailyBars(ctx, code, dailyBars)
}

// Weekly 周线（由日线重采样，来自 stockdb.WeeklyBars）。
func (f *Feed) Weekly(ctx context.Context, code string) ([]Bar, error) {
	return f.client.WeeklyBars(ctx, code, dailyBars)
}

// Monthly 月线（由日线重采样，来自 stockdb.MonthlyBars）。
func (f *Feed) Monthly(ctx context.Context, code string) ([]Bar, error) {
	return f.client.MonthlyBars(ctx, code, dailyBars)
}

// WeeklyFrom 用日线重采样出周线（委托 stockdb.ResamplePeriod）。
func (f *Feed) WeeklyFrom(d []Bar) []Bar { return f.client.WeeklyFrom(d) }

// MonthlyFrom 用日线重采样出月线（委托 stockdb.ResamplePeriod）。
func (f *Feed) MonthlyFrom(d []Bar) []Bar { return f.client.MonthlyFrom(d) }

// Codes 返回全市场 6 位代码（取自 stockdb 股票代码 索引表）。
func (f *Feed) Codes(ctx context.Context) ([]string, error) {
	raw, err := f.client.RawQuery(ctx, "股票代码")
	if err != nil {
		return nil, err
	}
	var grouped map[string][]string
	if err := json.Unmarshal(raw, &grouped); err != nil {
		return nil, fmt.Errorf("shannon: decode 股票代码: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	keys := make([]string, 0, len(grouped))
	for k := range grouped {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, code := range grouped[k] {
			code = strings.TrimSpace(code)
			if len(code) != 6 || seen[code] {
				continue
			}
			seen[code] = true
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out, nil
}

type boardRow struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Symbols []string `json:"symbols"`
}

// BoardSymbols 按板块名取成分股（取自 stockdb 板块* 表，覆盖概念与申万行业）。
func (f *Feed) BoardSymbols(ctx context.Context, name string) ([]string, error) {
	raw, err := f.client.RawQuery(ctx, "板块*")
	if err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("shannon: decode 板块*: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, row := range rows {
		var pair [2]json.RawMessage
		if err := json.Unmarshal(row, &pair); err != nil {
			continue
		}
		var value boardRow
		if err := json.Unmarshal(pair[1], &value); err != nil {
			continue
		}
		if value.Name != name {
			continue
		}
		for _, code := range value.Symbols {
			code = strings.TrimSpace(code)
			if len(code) != 6 || seen[code] {
				continue
			}
			seen[code] = true
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ListBoards 返回 stockdb 板块* 表中的全部板块名称（概念 + 申万行业），升序去重。
// 供前端工作台下拉选择扫描范围使用。
func (f *Feed) ListBoards(ctx context.Context) ([]string, error) {
	raw, err := f.client.RawQuery(ctx, "板块*")
	if err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("shannon: decode 板块*: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, row := range rows {
		var pair [2]json.RawMessage
		if err := json.Unmarshal(row, &pair); err != nil {
			continue
		}
		var value boardRow
		if err := json.Unmarshal(pair[1], &value); err != nil {
			continue
		}
		name := strings.TrimSpace(value.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}
