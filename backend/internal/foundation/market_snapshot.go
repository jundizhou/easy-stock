package foundation

import "strings"

// MarketSnapshotRow 是全市场快照的一行。
//
// 它刻意比 Quote 多带换手、量比、市值等字段：监控规则需要在一轮扫描里
// 同时判断"涨了多少"和"放没放量"，如果每个条件都回源单独取一次，
// 一次全市场扫描会变成上万次请求。
type MarketSnapshotRow struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	Board         string  `json:"board"`
	Price         float64 `json:"price"`
	ChangePercent float64 `json:"change_percent"`
	Open          float64 `json:"open"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	PreviousClose float64 `json:"previous_close"`
	Volume        float64 `json:"volume"`
	Amount        float64 `json:"amount"`
	TurnoverRate  float64 `json:"turnover_rate"`
	VolumeRatio   float64 `json:"volume_ratio"`
	MarketCap     float64 `json:"market_cap"`
	FloatCap      float64 `json:"float_cap"`
	IsST          bool    `json:"is_st"`
	Suspended     bool    `json:"suspended"`
	TradeDate     string  `json:"trade_date,omitempty"`
}

// Board 常量与涨跌幅限制一一对应，避免在多个页面各写一份板块判断。
const (
	BoardMain    = "main"
	BoardChiNext = "chinext"
	BoardSTAR    = "star"
	BoardBSE     = "bse"
)

// DetectBoard 按代码段推断板块。名称只在 ST 判断里参与；板块本身只看
// 代码段，这条规则对所有现存上市代码稳定成立。
func DetectBoard(symbol, name string) string {
	switch {
	case strings.HasPrefix(symbol, "688"), strings.HasPrefix(symbol, "689"):
		return BoardSTAR
	case strings.HasPrefix(symbol, "300"), strings.HasPrefix(symbol, "301"):
		return BoardChiNext
	case strings.HasPrefix(symbol, "8"), strings.HasPrefix(symbol, "4"), strings.HasPrefix(symbol, "920"):
		return BoardBSE
	default:
		return BoardMain
	}
}

// IsST 按名称判断风险警示股。
func IsST(name string) bool {
	return strings.Contains(strings.ToUpper(name), "ST")
}
