package stockanalysis

import (
	"fmt"
	"sort"
	"strings"
)

// MinuteMicrostructure 是基于最近可用交易日1分钟K的确定性微观结构摘要。
//
// 全部指标由本地代码计算，供证据链（Evidence）与 Hermes prompt（price_context.minute_structure）
// 引用。数据未同步到最新交易日时 TradeDate 会如实落后，消费方必须按日期标注，
// 不得冒充当日分时。1分钟K标签为左端点（该分钟起始时刻），volume 单位为股。
type MinuteMicrostructure struct {
	Available          bool     `json:"available"`
	TradeDate          string   `json:"trade_date"`
	BarCount           int      `json:"bar_count"`
	Open               float64  `json:"open"`
	High               float64  `json:"high"`
	Low                float64  `json:"low"`
	Close              float64  `json:"close"`
	VWAP               float64  `json:"vwap"`
	CloseVsVWAPPercent float64  `json:"close_vs_vwap_percent"`
	AmplitudePercent   float64  `json:"amplitude_percent"`
	First30Return      float64  `json:"first30_return_percent"`
	First30VolumeShare float64  `json:"first30_volume_share"`
	Last30Return       float64  `json:"last30_return_percent"`
	Last30VolumeShare  float64  `json:"last30_volume_share"`
	Last60Return       float64  `json:"last60_return_percent"`
	HighTime           string   `json:"high_time"`
	LowTime            string   `json:"low_time"`
	State              string   `json:"state"`
	Tags               []string `json:"tags"`
	Summary            string   `json:"summary"`
}

// LatestMinuteDay 从全量分钟K中取出最近一个有成交的交易日样本（按日期过滤 +
// 剔除零成交占位行 + 时间升序）。stockdb 子键不支持范围语义，调用方拿到的
// 是整表数据，必须先经过本函数再交给 analyzeMinuteMicro。
func LatestMinuteDay(bars []MinuteBar) []MinuteBar {
	latest := ""
	for _, bar := range bars {
		if len(bar.Stamp) != 14 {
			continue
		}
		if day := bar.Stamp[:8]; day > latest {
			latest = day
		}
	}
	if latest == "" {
		return nil
	}
	out := make([]MinuteBar, 0, 240)
	for _, bar := range bars {
		if strings.HasPrefix(bar.Stamp, latest) && bar.Volume > 0 {
			out = append(out, bar)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stamp < out[j].Stamp })
	return out
}

// analyzeMinuteMicro 计算1分钟K微观结构。样本不足（少于60根，正常全日约240根）
// 或字段异常时返回 nil，由调用方在 DataQuality 中标注缺失。
func analyzeMinuteMicro(bars []MinuteBar) *MinuteMicrostructure {
	if len(bars) < 60 {
		return nil
	}
	m := &MinuteMicrostructure{
		Available: true,
		TradeDate: minuteDate(bars[0].Stamp),
		BarCount:  len(bars),
		Open:      bars[0].Open,
		Close:     bars[len(bars)-1].Close,
		HighTime:  minuteClock(bars[0].Stamp),
		LowTime:   minuteClock(bars[0].Stamp),
	}
	totalVolume := 0.0
	totalAmount := 0.0
	for _, bar := range bars {
		totalVolume += bar.Volume
		totalAmount += bar.Amount
		if bar.High > m.High {
			m.High = bar.High
			m.HighTime = minuteClock(bar.Stamp)
		}
		if m.Low == 0 || bar.Low < m.Low {
			m.Low = bar.Low
			m.LowTime = minuteClock(bar.Stamp)
		}
	}
	if totalVolume <= 0 || m.Open <= 0 || m.Low <= 0 {
		return nil
	}
	m.VWAP = totalAmount / totalVolume
	if m.VWAP > 0 {
		m.CloseVsVWAPPercent = (m.Close - m.VWAP) / m.VWAP * 100
	}
	if m.Open > 0 {
		m.AmplitudePercent = (m.High - m.Low) / m.Open * 100
	}

	// 时间窗（左端点标签，时间部分为 stamp[8:14] 的6位 HHMMSS）：
	//   首30分钟 = 09:30:00-09:59:59；末60分钟 = >=14:00:00；末30分钟 = >=14:30:00
	first30 := minuteWindow(bars, "", "100000")
	last60 := minuteWindow(bars, "140000", "")
	last30 := minuteWindow(bars, "143000", "")
	m.First30Return = minuteWindowReturn(first30, m.Open)
	m.First30VolumeShare = minuteVolumeShare(first30, totalVolume)
	m.Last60Return = minuteWindowReturn(last60, windowBaseClose(bars, "140000"))
	m.Last30Return = minuteWindowReturn(last30, windowBaseClose(bars, "143000"))
	m.Last30VolumeShare = minuteVolumeShare(last30, totalVolume)

	m.State = minuteTailState(m)
	m.Tags = minuteTags(m, bars, last30)
	m.Summary = minuteSummary(m)
	return m
}

// minuteWindow 取时间部分落在 [from, to) 的K线；from/to 为空表示不设界。
func minuteWindow(bars []MinuteBar, from, to string) []MinuteBar {
	out := make([]MinuteBar, 0, len(bars))
	for _, bar := range bars {
		if len(bar.Stamp) != 14 {
			continue
		}
		clock := bar.Stamp[8:14]
		if from != "" && clock < from {
			continue
		}
		if to != "" && clock >= to {
			continue
		}
		out = append(out, bar)
	}
	return out
}

// minuteWindowReturn 窗口收益：窗口末根收盘相对窗口首根开盘。
func minuteWindowReturn(window []MinuteBar, base float64) float64 {
	if len(window) == 0 || base <= 0 {
		return 0
	}
	return (window[len(window)-1].Close - base) / base * 100
}

// minuteVolumeShare 窗口成交量占全日比例。
func minuteVolumeShare(window []MinuteBar, totalVolume float64) float64 {
	if totalVolume <= 0 {
		return 0
	}
	volume := 0.0
	for _, bar := range window {
		volume += bar.Volume
	}
	return volume / totalVolume * 100
}

// windowBaseClose 取窗口起点前最后一根的收盘价（尾盘收益基准）。
func windowBaseClose(bars []MinuteBar, from string) float64 {
	base := 0.0
	for _, bar := range bars {
		if len(bar.Stamp) != 14 || bar.Stamp[8:14] >= from {
			continue
		}
		if bar.Close > 0 {
			base = bar.Close
		}
	}
	return base
}

func minuteTailState(m *MinuteMicrostructure) string {
	score := 0
	switch {
	case m.Last30Return >= 0.5:
		score++
	case m.Last30Return <= -0.5:
		score--
	}
	switch {
	case m.CloseVsVWAPPercent >= 0.2:
		score++
	case m.CloseVsVWAPPercent <= -0.2:
		score--
	}
	switch score {
	case 2:
		return "尾盘走强"
	case 1:
		return "尾盘偏强"
	case -1:
		return "尾盘偏弱"
	case -2:
		return "尾盘走弱"
	default:
		return "尾盘震荡"
	}
}

func minuteTags(m *MinuteMicrostructure, bars, last30 []MinuteBar) []string {
	tags := make([]string, 0, 4)
	add := func(format string, args ...any) {
		tags = append(tags, fmt.Sprintf(format, args...))
	}
	if m.HighTime < "14:00" && m.High > 0 && (m.High-m.Close)/m.High >= 0.02 {
		add("冲高回落（高点%s，收盘距高点%.1f%%）", m.HighTime, (m.High-m.Close)/m.High*100)
	}
	last30Low := 0.0
	for _, bar := range last30 {
		if last30Low == 0 || bar.Low < last30Low {
			last30Low = bar.Low
		}
	}
	if m.Last30Return >= 0 && last30Low > m.Low {
		add("尾盘企稳（末30分钟未创日内新低）")
	}
	switch {
	case m.Last30Return >= 1:
		add("尾盘拉升（末30分钟%+.1f%%）", m.Last30Return)
	case m.Last30Return <= -1:
		add("尾盘跳水（末30分钟%+.1f%%）", m.Last30Return)
	}
	if m.Last30Return > 0 && m.Last30VolumeShare < 5 {
		add("尾盘缩量上行（末30分钟量占%.0f%%）", m.Last30VolumeShare)
	} else if m.Last30VolumeShare >= 15 {
		add("尾盘放量（末30分钟量占%.0f%%）", m.Last30VolumeShare)
	}
	switch {
	case m.First30Return >= 1.5:
		add("早盘抢筹（首30分钟%+.1f%%，量占%.0f%%）", m.First30Return, m.First30VolumeShare)
	case m.First30Return <= -1.5:
		add("早盘杀跌（首30分钟%+.1f%%，量占%.0f%%）", m.First30Return, m.First30VolumeShare)
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

func minuteSummary(m *MinuteMicrostructure) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s分时结构：%s；收盘价%sVWAP（%+.2f%%）", m.TradeDate, m.State, vwapSide(m), m.CloseVsVWAPPercent)
	fmt.Fprintf(&builder, "；首30分钟%+.2f%%（量占%.0f%%），末30分钟%+.2f%%（量占%.0f%%）", m.First30Return, m.First30VolumeShare, m.Last30Return, m.Last30VolumeShare)
	fmt.Fprintf(&builder, "；日内高点%s/低点%s，全天振幅%.1f%%", m.HighTime, m.LowTime, m.AmplitudePercent)
	return builder.String()
}

func vwapSide(m *MinuteMicrostructure) string {
	if m.CloseVsVWAPPercent >= 0 {
		return "高于"
	}
	return "低于"
}

// minuteClock 把14位时间戳转成 HH:MM（用于高低点时点展示与比较）。
func minuteClock(stamp string) string {
	if len(stamp) != 14 {
		return ""
	}
	return stamp[8:10] + ":" + stamp[10:12]
}

// minuteDate 把14位时间戳的日期部分转成 YYYY-MM-DD（与其他证据 AsOf 格式一致）。
func minuteDate(stamp string) string {
	if len(stamp) != 14 {
		return stamp
	}
	return stamp[0:4] + "-" + stamp[4:6] + "-" + stamp[6:8]
}

// minuteMicroEvidence 把分钟结构结论并入证据链（Category=分钟结构）。
func minuteMicroEvidence(m *MinuteMicrostructure) []Evidence {
	details := make([]string, 0, 4)
	details = append(details, m.Summary)
	if len(m.Tags) > 0 {
		details = append(details, "形态："+strings.Join(m.Tags, "；"))
	}
	details = append(details, fmt.Sprintf("VWAP %.2f（收盘偏离%+.2f%%）", m.VWAP, m.CloseVsVWAPPercent))
	return []Evidence{{
		Category: "分钟结构",
		Title:    fmt.Sprintf("%s · %s · 尾盘30分钟%+.2f%%", m.TradeDate, m.State, m.Last30Return),
		Detail:   strings.Join(details, "；"),
		Source:   "free-stockdb:分钟k",
		AsOf:     m.TradeDate,
	}}
}
