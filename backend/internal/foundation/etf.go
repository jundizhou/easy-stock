package foundation

import "strings"

// IsListedFundCandidate is a routing guard, not proof that a security is an ETF.
func IsListedFundCandidate(symbol string) bool {
	normalized, err := NormalizeSymbol(symbol)
	if err != nil {
		return false
	}
	// 港股是 5 位代码，个别代码段会撞上 A 股 ETF 的前缀规则；
	// 港股没有场外/场内基金代码体系，直接判定不是基金。
	if normalized.Market == "HK" {
		return false
	}
	code := normalized.RawCode
	return strings.HasPrefix(code, "5") || strings.HasPrefix(code, "15") || strings.HasPrefix(code, "16")
}

// Instrument carries identity separately from a provider's quote symbol.
type Instrument struct {
	Symbol         string `json:"symbol"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	InstrumentType string `json:"instrument_type"`
	FundType       string `json:"fund_type,omitempty"`
}

type ETFProfile struct {
	Instrument
	FullName        string     `json:"full_name"`
	AssetClass      string     `json:"asset_class"`
	CrossBorder     bool       `json:"cross_border"`
	Manager         string     `json:"manager"`
	Benchmark       string     `json:"benchmark"`
	TrackingTarget  string     `json:"tracking_target"`
	InvestmentScope string     `json:"investment_scope"`
	Strategy        string     `json:"strategy"`
	RiskDescription string     `json:"risk_description"`
	Distribution    string     `json:"distribution"`
	ManagementFee   string     `json:"management_fee"`
	CustodyFee      string     `json:"custody_fee"`
	Size            string     `json:"size"`
	Shares          string     `json:"shares"`
	Inception       string     `json:"inception"`
	PricePrecision  int        `json:"price_precision"`
	Meta            SourceMeta `json:"meta"`
}

// Dates are valuation dates, not publication dates or live indicative values.
type ETFNAVPoint struct {
	Date         string  `json:"date"`
	Value        float64 `json:"value"`
	Distribution string  `json:"distribution,omitempty"`
}

type ETFNetValues struct {
	Symbol               string        `json:"symbol"`
	Points               []ETFNAVPoint `json:"points"`
	IncomePerTenThousand []ETFNAVPoint `json:"income_per_ten_thousand,omitempty"`
	SevenDayAnnualized   []ETFNAVPoint `json:"seven_day_annualized,omitempty"`
	Meta                 SourceMeta    `json:"meta"`
}

// ETFMarketSeries retains the provider's exact index identity and price bars.
// A sector reference has different constituents from the tracking index.
type ETFMarketSeries struct {
	Symbol       string     `json:"symbol"`
	Name         string     `json:"name"`
	Relationship string     `json:"relationship"`
	Bars         []KLine    `json:"bars"`
	Meta         SourceMeta `json:"meta"`
}
