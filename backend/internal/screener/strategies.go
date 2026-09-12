package screener

import (
	"fmt"
	"strings"

	"easy-stock/backend/internal/foundation"
)

// SnapshotRow 是全市场快照里一只股票参与筛选所需的全部字段。
type SnapshotRow = foundation.MarketQuoteRow

// rowIsST 判断是否 ST / *ST / 退市整理股票。
func rowIsST(r SnapshotRow) bool {
	name := strings.ToUpper(r.Name)
	return strings.Contains(name, "ST") || strings.HasPrefix(name, "退")
}

// rowIsSuspended 判断是否停牌/无行情（价格与涨跌幅同时缺失）。
func rowIsSuspended(r SnapshotRow) bool {
	return r.Close <= 0 && r.ChangePercent == 0
}

// Params 是策略参数集合（键与 Strategy.Params 对应，取不到时用默认值）。
type Params map[string]float64

func (p Params) get(key string, def float64) float64 {
	if v, ok := p[key]; ok && v == v { // 非 NaN
		return v
	}
	return def
}

// snapshotPredicate 是快照策略的判断函数：返回是否命中与命中说明。
type snapshotPredicate func(row SnapshotRow, params Params) (bool, string)

// klinePredicate 是 K 线策略的判断函数：bars 为时间升序日 K（最后一根为最新）。
type klinePredicate func(bars []foundation.KLine, params Params) (bool, string, map[string]float64)

// strategyDef 是内部策略定义（含判断函数）。
type strategyDef struct {
	meta              Strategy
	snapshot          snapshotPredicate
	kline             klinePredicate
	universeMinAmount float64 // K 线策略进入计算池的最低成交额偏好（亿），0 表示无偏好
}

// registry 是全部内置策略，ID 稳定不可变更（前端按 ID 勾选）。
var registry = []*strategyDef{
	// ── 快照类：量价 ──
	{
		meta: Strategy{
			ID: "vol_surge_up", Name: "放量上涨", Category: "量价", Kind: KindSnapshot,
			Description: "量比显著放大且当日上涨，短线资金关注的直接信号",
			Params: []ParamSpec{
				{Key: "min_vr", Label: "最小量比", Default: 2, Min: 1, Max: 10, Step: 0.5},
				{Key: "min_pct", Label: "最小涨幅%", Default: 2, Min: 0, Max: 9.5, Step: 0.5},
				{Key: "max_pct", Label: "最大涨幅%", Default: 7, Min: 1, Max: 20, Step: 0.5},
			},
		},
		snapshot: func(r SnapshotRow, p Params) (bool, string) {
			if r.VolumeRatio < p.get("min_vr", 2) || r.ChangePercent < p.get("min_pct", 2) || r.ChangePercent > p.get("max_pct", 7) {
				return false, ""
			}
			return true, fmt.Sprintf("量比 %.1f · 涨 %.1f%%", r.VolumeRatio, r.ChangePercent)
		},
	},
	{
		meta: Strategy{
			ID: "steady_rise", Name: "温和放量上行", Category: "量价", Kind: KindSnapshot,
			Description: "量能与换手温和配合的稳步上涨，趋势启动早期形态",
			Params: []ParamSpec{
				{Key: "min_vr", Label: "量比下限", Default: 1.5, Min: 1, Max: 5, Step: 0.1},
				{Key: "max_vr", Label: "量比上限", Default: 3, Min: 1, Max: 8, Step: 0.1},
				{Key: "min_tr", Label: "换手下限%", Default: 3, Min: 1, Max: 20, Step: 0.5},
			},
		},
		snapshot: func(r SnapshotRow, p Params) (bool, string) {
			vr := p.get("min_vr", 1.5)
			if r.VolumeRatio < vr || r.VolumeRatio > p.get("max_vr", 3) || r.TurnoverRate < p.get("min_tr", 3) {
				return false, ""
			}
			if r.ChangePercent <= 0 || r.ChangePercent > 6 {
				return false, ""
			}
			return true, fmt.Sprintf("量比 %.1f · 换手 %.1f%% · 涨 %.1f%%", r.VolumeRatio, r.TurnoverRate, r.ChangePercent)
		},
	},
	{
		meta: Strategy{
			ID: "active_high_turnover", Name: "高换手活跃股", Category: "量价", Kind: KindSnapshot,
			Description: "换手率显著放大且未大幅拉升的活跃筹码交换区",
			Params: []ParamSpec{
				{Key: "min_tr", Label: "换手下限%", Default: 10, Min: 5, Max: 40, Step: 1},
				{Key: "max_tr", Label: "换手上限%", Default: 25, Min: 8, Max: 60, Step: 1},
				{Key: "max_pct", Label: "最大涨幅%", Default: 5, Min: 1, Max: 15, Step: 0.5},
			},
		},
		snapshot: func(r SnapshotRow, p Params) (bool, string) {
			if r.TurnoverRate < p.get("min_tr", 10) || r.TurnoverRate > p.get("max_tr", 25) || r.ChangePercent > p.get("max_pct", 5) {
				return false, ""
			}
			return true, fmt.Sprintf("换手 %.1f%% · 涨 %.1f%%", r.TurnoverRate, r.ChangePercent)
		},
	},
	// ── 快照类：资金 ──
	{
		meta: Strategy{
			ID: "main_inflow", Name: "主力净流入", Category: "资金", Kind: KindSnapshot,
			Description: "主力资金净流入且股价收涨，量价资金共振",
			Params: []ParamSpec{
				{Key: "min_inflow_yi", Label: "最小净流入(亿)", Default: 0.5, Min: 0.1, Max: 10, Step: 0.1},
				{Key: "min_pct", Label: "最小涨幅%", Default: 1, Min: 0, Max: 9, Step: 0.5},
			},
		},
		snapshot: func(r SnapshotRow, p Params) (bool, string) {
			inflowYi := r.MainInflow / 1e8
			if inflowYi < p.get("min_inflow_yi", 0.5) || r.ChangePercent < p.get("min_pct", 1) {
				return false, ""
			}
			return true, fmt.Sprintf("主力净流入 %.2f 亿 · 涨 %.1f%%", inflowYi, r.ChangePercent)
		},
	},
	// ── 快照类：动量/超跌 ──
	{
		meta: Strategy{
			ID: "oversold_rebound", Name: "超跌反弹", Category: "动量", Kind: KindSnapshot,
			Description: "近 5 日显著下跌后当日转涨的反弹候选",
			Params: []ParamSpec{
				{Key: "min_5d_drop", Label: "5日最小跌幅%", Default: 10, Min: 3, Max: 30, Step: 1},
				{Key: "min_pct", Label: "当日最小涨幅%", Default: 1, Min: 0, Max: 6, Step: 0.5},
			},
		},
		snapshot: func(r SnapshotRow, p Params) (bool, string) {
			if r.FiveDayPct > -p.get("min_5d_drop", 10) || r.ChangePercent < p.get("min_pct", 1) {
				return false, ""
			}
			return true, fmt.Sprintf("5日 %.1f%% · 今日涨 %.1f%%", r.FiveDayPct, r.ChangePercent)
		},
	},
	{
		meta: Strategy{
			ID: "momentum_60d", Name: "中期强势股", Category: "动量", Kind: KindSnapshot,
			Description: "60 日累计涨幅可观且当日仍收涨，中期趋势延续",
			Params: []ParamSpec{
				{Key: "min_60d", Label: "60日最小涨幅%", Default: 30, Min: 5, Max: 200, Step: 5},
				{Key: "max_5d", Label: "5日最大涨幅%", Default: 25, Min: 5, Max: 80, Step: 1},
			},
		},
		snapshot: func(r SnapshotRow, p Params) (bool, string) {
			if r.SixtyDayPct < p.get("min_60d", 30) || r.FiveDayPct > p.get("max_5d", 25) || r.ChangePercent <= 0 {
				return false, ""
			}
			return true, fmt.Sprintf("60日 %.0f%% · 5日 %.1f%%", r.SixtyDayPct, r.FiveDayPct)
		},
	},
	// ── 快照类：规模/估值 ──
	{
		meta: Strategy{
			ID: "small_cap_active", Name: "小市值活跃", Category: "规模", Kind: KindSnapshot,
			Description: "小流通盘 + 活跃换手 + 收涨，弹性偏好组合",
			Params: []ParamSpec{
				{Key: "max_cap_yi", Label: "流通市值上限(亿)", Default: 80, Min: 10, Max: 300, Step: 10},
				{Key: "min_tr", Label: "换手下限%", Default: 5, Min: 1, Max: 20, Step: 0.5},
			},
		},
		snapshot: func(r SnapshotRow, p Params) (bool, string) {
			if r.FloatCapYi <= 0 || r.FloatCapYi > p.get("max_cap_yi", 80) || r.TurnoverRate < p.get("min_tr", 5) || r.ChangePercent <= 0 {
				return false, ""
			}
			return true, fmt.Sprintf("流通 %.0f 亿 · 换手 %.1f%%", r.FloatCapYi, r.TurnoverRate)
		},
	},
	{
		meta: Strategy{
			ID: "value_low_pb", Name: "低估破净修复", Category: "规模", Kind: KindSnapshot,
			Description: "市净率低于阈值且当日收涨的低估值修复观察",
			Params: []ParamSpec{
				{Key: "max_pb", Label: "市净率上限", Default: 0.9, Min: 0.2, Max: 2, Step: 0.05},
				{Key: "min_tr", Label: "换手下限%", Default: 1, Min: 0.5, Max: 10, Step: 0.5},
			},
		},
		snapshot: func(r SnapshotRow, p Params) (bool, string) {
			if r.PB <= 0 || r.PB > p.get("max_pb", 0.9) || r.TurnoverRate < p.get("min_tr", 1) || r.ChangePercent <= 0 {
				return false, ""
			}
			return true, fmt.Sprintf("PB %.2f · 涨 %.1f%%", r.PB, r.ChangePercent)
		},
	},
	// ── K 线类：趋势 ──
	{
		meta: Strategy{
			ID: "ma_bull_align", Name: "均线多头排列", Category: "趋势K线", Kind: KindKline,
			Description: "MA5 > MA10 > MA20 > MA60 且收盘站上 MA5，标准多头结构",
		},
		kline: func(bars []foundation.KLine, p Params) (bool, string, map[string]float64) {
			closes := closeSeries(bars)
			ma5 := SMA(closes, 5)
			ma10 := SMA(closes, 10)
			ma20 := SMA(closes, 20)
			ma60 := SMA(closes, 60)
			if ma60 == nil {
				return false, "", nil
			}
			i := len(closes) - 1
			if !(ma5[i] > ma10[i] && ma10[i] > ma20[i] && ma20[i] > ma60[i] && closes[i] > ma5[i]) {
				return false, "", nil
			}
			return true, "MA5>MA10>MA20>MA60 且收于 MA5 上", indMap(bars, ma5, ma10, ma20, ma60)
		},
	},
	{
		meta: Strategy{
			ID: "ma_bond_break", Name: "放量突破20日高", Category: "趋势K线", Kind: KindKline,
			Description: "成交量放大至 20 日均量 2 倍以上且收盘突破前 20 日高点",
			Params: []ParamSpec{
				{Key: "vol_mult", Label: "量能倍数", Default: 2, Min: 1.2, Max: 5, Step: 0.1},
			},
		},
		kline: func(bars []foundation.KLine, p Params) (bool, string, map[string]float64) {
			if len(bars) < 25 {
				return false, "", nil
			}
			i := len(bars) - 1
			var volSum float64
			for m := i - 20; m < i; m++ {
				volSum += bars[m].Volume
			}
			avgVol := volSum / 20
			if avgVol <= 0 {
				return false, "", nil
			}
			mult := p.get("vol_mult", 2)
			prevHigh := highest(highSeries(bars), i, 20, false)
			volRatio := bars[i].Volume / avgVol
			if volRatio < mult || bars[i].Close <= prevHigh {
				return false, "", nil
			}
			return true, fmt.Sprintf("量能 %.1f 倍 · 突破 20 日高 %.2f", volRatio, prevHigh),
				map[string]float64{"vol_ratio": round2(volRatio), "break_high": round2(prevHigh)}
		},
	},
	// ── K 线类：指标 ──
	{
		meta: Strategy{
			ID: "macd_golden", Name: "MACD 金叉", Category: "指标K线", Kind: KindKline,
			Description: "DIF 最近 3 根K线内上穿 DEA（零轴下金叉更佳，另行说明）",
			Params: []ParamSpec{
				{Key: "lookback", Label: "回看K线数", Default: 3, Min: 1, Max: 10, Step: 1},
			},
		},
		kline: func(bars []foundation.KLine, p Params) (bool, string, map[string]float64) {
			dif, dea, hist := MACD(closeSeries(bars), 12, 26, 9)
			ok, ago := crossedUp(dif, dea, int(p.get("lookback", 3)))
			if !ok {
				return false, "", nil
			}
			i := len(bars) - 1
			zone := "零上"
			if dif[i] < 0 {
				zone = "零下"
			}
			return true, fmt.Sprintf("%s金叉（%d 根K线前）· HIST %.3f", zone, ago, hist[i]),
				map[string]float64{"dif": round3(dif[i]), "dea": round3(dea[i]), "hist": round3(hist[i])}
		},
	},
	{
		meta: Strategy{
			ID: "macd_bull", Name: "MACD 多头", Category: "指标K线", Kind: KindKline,
			Description: "DIF 与 DEA 均在零轴上方且 DIF > DEA，趋势动能健康",
		},
		kline: func(bars []foundation.KLine, p Params) (bool, string, map[string]float64) {
			dif, dea, hist := MACD(closeSeries(bars), 12, 26, 9)
			i := len(bars) - 1
			if dif[i] <= dea[i] || dif[i] <= 0 || dea[i] <= 0 {
				return false, "", nil
			}
			return true, fmt.Sprintf("DIF %.3f > DEA %.3f（零上多头）", dif[i], dea[i]),
				map[string]float64{"dif": round3(dif[i]), "dea": round3(dea[i]), "hist": round3(hist[i])}
		},
	},
	{
		meta: Strategy{
			ID: "rsi_recover", Name: "RSI 超卖回升", Category: "指标K线", Kind: KindKline,
			Description: "RSI14 前期跌破 30 后回升收复，超跌修复信号",
			Params: []ParamSpec{
				{Key: "oversold", Label: "超卖阈值", Default: 30, Min: 15, Max: 45, Step: 1},
			},
		},
		kline: func(bars []foundation.KLine, p Params) (bool, string, map[string]float64) {
			closes := closeSeries(bars)
			rsi := RSI(closes, 14)
			if rsi == nil {
				return false, "", nil
			}
			threshold := p.get("oversold", 30)
			i := len(closes) - 1
			if rsi[i] < threshold {
				return false, "", nil
			}
			wasOversold := false
			for m := i - 1; m >= i-5 && m > 0; m-- {
				if rsi[m] < threshold {
					wasOversold = true
					break
				}
			}
			if !wasOversold {
				return false, "", nil
			}
			return true, fmt.Sprintf("RSI14 %.1f（5 日内曾低于 %.0f）", rsi[i], threshold),
				map[string]float64{"rsi14": round2(rsi[i])}
		},
	},
	{
		meta: Strategy{
			ID: "kdj_golden", Name: "KDJ 金叉", Category: "指标K线", Kind: KindKline,
			Description: "K 线最近 3 根内上穿 D 线且 J 值从低位起步",
			Params: []ParamSpec{
				{Key: "lookback", Label: "回看K线数", Default: 3, Min: 1, Max: 10, Step: 1},
				{Key: "max_j", Label: "J 值上限", Default: 60, Min: 10, Max: 100, Step: 5},
			},
		},
		kline: func(bars []foundation.KLine, p Params) (bool, string, map[string]float64) {
			k, d, j := KDJ(highSeries(bars), lowSeries(bars), closeSeries(bars), 9, 3, 3)
			ok, ago := crossedUp(k, d, int(p.get("lookback", 3)))
			if !ok {
				return false, "", nil
			}
			i := len(bars) - 1
			if j[i] > p.get("max_j", 60) {
				return false, "", nil
			}
			return true, fmt.Sprintf("KDJ 金叉（%d 根K线前）· J %.1f", ago, j[i]),
				map[string]float64{"k": round2(k[i]), "d": round2(d[i]), "j": round2(j[i])}
		},
	},
	{
		meta: Strategy{
			ID: "boll_mid_reclaim", Name: "收复布林中轨", Category: "指标K线", Kind: KindKline,
			Description: "收盘价自下而上收复 20 日布林中轨，震荡转强",
		},
		kline: func(bars []foundation.KLine, p Params) (bool, string, map[string]float64) {
			closes := closeSeries(bars)
			mid, upper, lower := BOLL(closes, 20, 2)
			if mid == nil {
				return false, "", nil
			}
			i := len(closes) - 1
			if mid[i-1] <= 0 {
				return false, "", nil
			}
			if !(closes[i-1] < mid[i-1] && closes[i] > mid[i]) {
				return false, "", nil
			}
			return true, fmt.Sprintf("收复中轨 %.2f（上轨 %.2f）", mid[i], upper[i]),
				map[string]float64{"boll_mid": round2(mid[i]), "boll_upper": round2(upper[i]), "boll_lower": round2(lower[i])}
		},
	},
	// ── K 线类：形态 ──
	{
		meta: Strategy{
			ID: "bull_streak", Name: "五连阳", Category: "形态K线", Kind: KindKline,
			Description: "最近 5 根K线连续收阳且累计涨幅温和，蓄势形态",
			Params: []ParamSpec{
				{Key: "min_total", Label: "累计涨幅下限%", Default: 1, Min: 0, Max: 10, Step: 0.5},
				{Key: "max_total", Label: "累计涨幅上限%", Default: 15, Min: 3, Max: 30, Step: 0.5},
			},
		},
		kline: func(bars []foundation.KLine, p Params) (bool, string, map[string]float64) {
			if len(bars) < 6 {
				return false, "", nil
			}
			i := len(bars) - 1
			for m := i - 4; m <= i; m++ {
				if bars[m].Close <= bars[m].Open {
					return false, "", nil
				}
			}
			total := (bars[i].Close/bars[i-5].Close - 1) * 100
			if total < p.get("min_total", 1) || total > p.get("max_total", 15) {
				return false, "", nil
			}
			return true, fmt.Sprintf("五连阳 · 累计 %.1f%%", total),
				map[string]float64{"streak_gain": round2(total)}
		},
	},
	{
		meta: Strategy{
			ID: "limit_pullback", Name: "涨停回踩", Category: "形态K线", Kind: KindKline,
			Description: "近 5 日出现过涨停，今日缩量回踩但守住 10 日线",
			Params: []ParamSpec{
				{Key: "min_pullback", Label: "回踩幅度%", Default: 1.5, Min: 0.5, Max: 8, Step: 0.5},
			},
		},
		kline: func(bars []foundation.KLine, p Params) (bool, string, map[string]float64) {
			if len(bars) < 15 {
				return false, "", nil
			}
			i := len(bars) - 1
			closes := closeSeries(bars)
			dayPct := func(m int) float64 {
				if m <= 0 || closes[m-1] <= 0 {
					return 0
				}
				return (closes[m]/closes[m-1] - 1) * 100
			}
			hadLimit := false
			for m := i - 4; m <= i; m++ {
				if dayPct(m) >= 9.8 {
					hadLimit = true
					break
				}
			}
			if !hadLimit {
				return false, "", nil
			}
			pullback := p.get("min_pullback", 1.5)
			if dayPct(i) > -pullback {
				return false, "", nil
			}
			ma10 := SMA(closeSeries(bars), 10)
			if ma10 == nil || bars[i].Close < ma10[i] {
				return false, "", nil
			}
			var volSum float64
			for m := i - 5; m < i; m++ {
				volSum += bars[m].Volume
			}
			shrink := 0.0
			if volSum > 0 {
				shrink = bars[i].Volume / (volSum / 5)
			}
			return true, fmt.Sprintf("回踩 %.1f%% 守住 10 日线 · 量能 %.1f 倍", -dayPct(i), shrink),
				map[string]float64{"pullback": round2(bars[i].ChangePercent), "vol_ratio5": round2(shrink)}
		},
	},
}

// Strategies 返回策略目录（前端勾选用）。
func Strategies() []Strategy {
	out := make([]Strategy, 0, len(registry))
	for _, def := range registry {
		out = append(out, def.meta)
	}
	return out
}

func strategyByID(id string) *strategyDef {
	for _, def := range registry {
		if def.meta.ID == id {
			return def
		}
	}
	return nil
}

// ── 序列与工具 ──

func closeSeries(bars []foundation.KLine) []float64 {
	out := make([]float64, len(bars))
	for i, bar := range bars {
		out[i] = bar.Close
	}
	return out
}

func highSeries(bars []foundation.KLine) []float64 {
	out := make([]float64, len(bars))
	for i, bar := range bars {
		out[i] = bar.High
	}
	return out
}

func lowSeries(bars []foundation.KLine) []float64 {
	out := make([]float64, len(bars))
	for i, bar := range bars {
		out[i] = bar.Low
	}
	return out
}

// indMap 汇总展示用均线指标。
func indMap(bars []foundation.KLine, ma5, ma10, ma20, ma60 []float64) map[string]float64 {
	i := len(bars) - 1
	out := map[string]float64{"close": round2(bars[i].Close)}
	if ma5 != nil {
		out["ma5"] = round2(ma5[i])
	}
	if ma10 != nil {
		out["ma10"] = round2(ma10[i])
	}
	if ma20 != nil {
		out["ma20"] = round2(ma20[i])
	}
	if ma60 != nil {
		out["ma60"] = round2(ma60[i])
	}
	return out
}

func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }
func round3(v float64) float64 { return float64(int(v*1000+0.5)) / 1000 }
