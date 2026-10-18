package foundation

import "strings"

// NormalizeSectorFlowCode 归一化板块代码，接受 BK0917 / bk0917 / 90.BK0917 / 0917 等写法。
//
// 返回规范化后的东财板块代码；无法识别时第二个返回值为 false，
// HTTP 层据此把非法代码判定为请求错误而不是上游故障。
func NormalizeSectorFlowCode(raw string) (string, bool) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	code = strings.TrimPrefix(code, "90.")
	if code == "" {
		return "", false
	}
	if !strings.HasPrefix(code, "BK") {
		digits := strings.TrimLeft(code, "0")
		if digits == "" {
			return "", false
		}
		code = "BK" + digits
	}
	if len(code) < 5 || len(code) > 8 {
		return "", false
	}
	for _, char := range code[2:] {
		if char < '0' || char > '9' {
			return "", false
		}
	}
	return code, true
}

// SectorFlowRank 是板块资金流榜单的一行（概念板块或行业板块）。
//
// 口径来自东方财富板块资金流（data.eastmoney.com/bkzj）：
// 主力净流入 = 超大单净流入 + 大单净流入；净占比 = 净流入 / 成交额，单位为百分比数值。
// 5 日、10 日字段是最近 N 个交易日的累计净流入，不是单日数据。
type SectorFlowRank struct {
	Code               string     `json:"code"`
	Name               string     `json:"name"`
	ChangePercent      float64    `json:"change_percent"`
	MainNetInflow      float64    `json:"main_net_inflow"`
	MainNetInflowRatio float64    `json:"main_net_inflow_ratio"`
	SuperLargeNet      float64    `json:"super_large_net_inflow"`
	LargeNet           float64    `json:"large_net_inflow"`
	MediumNet          float64    `json:"medium_net_inflow"`
	SmallNet           float64    `json:"small_net_inflow"`
	FiveDayMainNet     float64    `json:"five_day_main_net_inflow"`
	TenDayMainNet      float64    `json:"ten_day_main_net_inflow"`
	Meta               SourceMeta `json:"meta"`
}

// SectorFlowPoint 是板块分时资金流的一个采样点，数值为当日累计净流入（元）。
type SectorFlowPoint struct {
	Time       string  `json:"time"` // 交易时间，形如 09:31
	Main       float64 `json:"main"`
	SuperLarge float64 `json:"super_large"`
	Large      float64 `json:"large"`
	Medium     float64 `json:"medium"`
	Small      float64 `json:"small"`
}

// SectorFlowSeries 是单个板块的当日分时资金流序列，供多板块曲线对比使用。
type SectorFlowSeries struct {
	Code       string            `json:"code"`
	Name       string            `json:"name"`
	TradeDate  string            `json:"trade_date"`
	Points     []SectorFlowPoint `json:"points"`
	LatestMain float64           `json:"latest_main"`
	Meta       SourceMeta        `json:"meta"`
}
