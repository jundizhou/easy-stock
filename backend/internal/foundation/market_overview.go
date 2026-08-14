package foundation

import "time"

type MarketIndexSnapshot struct {
	ID            string     `json:"id"`
	SecID         string     `json:"secid"`
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	Region        string     `json:"region"`
	Market        string     `json:"market"`
	Currency      string     `json:"currency"`
	Price         float64    `json:"price"`
	Change        float64    `json:"change"`
	ChangePercent float64    `json:"change_percent"`
	TradeTime     time.Time  `json:"trade_time,omitempty"`
	Status        string     `json:"status"`
	Meta          SourceMeta `json:"meta"`
}

type MarketIndexSeries struct {
	Index MarketIndexSnapshot `json:"index"`
	Lines []KLine             `json:"lines"`
	Meta  SourceMeta          `json:"meta"`
}

type MarketIndustryMomentum struct {
	Code                 string     `json:"code"`
	Name                 string     `json:"name"`
	ChangePercent        float64    `json:"change_percent"`
	FiveDayChangePercent float64    `json:"five_day_change_percent"`
	TwentyDayChange      float64    `json:"twenty_day_change_percent"`
	TurnoverRate         float64    `json:"turnover_rate"`
	RisingCount          int        `json:"rising_count"`
	FallingCount         int        `json:"falling_count"`
	MainNetInflow        float64    `json:"main_net_inflow"`
	LeaderName           string     `json:"leader_name,omitempty"`
	LeaderChangePercent  float64    `json:"leader_change_percent"`
	Score                float64    `json:"score"`
	Meta                 SourceMeta `json:"meta"`
}

type MarketFundFlow struct {
	Dimension          string     `json:"dimension"`
	Code               string     `json:"code"`
	Symbol             string     `json:"symbol,omitempty"`
	Name               string     `json:"name"`
	Price              float64    `json:"price"`
	ChangePercent      float64    `json:"change_percent"`
	Inflow             float64    `json:"inflow"`
	Outflow            float64    `json:"outflow"`
	NetInflow          float64    `json:"net_inflow"`
	NetInflowRatio     float64    `json:"net_inflow_ratio"`
	MainInflow         float64    `json:"main_inflow"`
	MainOutflow        float64    `json:"main_outflow"`
	MainNetInflow      float64    `json:"main_net_inflow"`
	MainNetInflowRatio float64    `json:"main_net_inflow_ratio"`
	RetailInflow       float64    `json:"retail_inflow"`
	RetailOutflow      float64    `json:"retail_outflow"`
	RetailNetInflow    float64    `json:"retail_net_inflow"`
	RetailNetRatio     float64    `json:"retail_net_inflow_ratio"`
	SuperLargeNet      float64    `json:"super_large_net_inflow"`
	SuperLargeRatio    float64    `json:"super_large_net_inflow_ratio"`
	LargeNet           float64    `json:"large_net_inflow"`
	LargeRatio         float64    `json:"large_net_inflow_ratio"`
	MediumNet          float64    `json:"medium_net_inflow"`
	MediumRatio        float64    `json:"medium_net_inflow_ratio"`
	SmallNet           float64    `json:"small_net_inflow"`
	SmallRatio         float64    `json:"small_net_inflow_ratio"`
	LeaderSymbol       string     `json:"leader_symbol,omitempty"`
	LeaderName         string     `json:"leader_name,omitempty"`
	LeaderPrice        float64    `json:"leader_price"`
	LeaderChange       float64    `json:"leader_change_percent"`
	LeaderNetRatio     float64    `json:"leader_net_inflow_ratio"`
	Meta               SourceMeta `json:"meta"`
}

type MarketBillboardItem struct {
	TradeDate         string     `json:"trade_date"`
	Symbol            string     `json:"symbol"`
	Name              string     `json:"name"`
	ClosePrice        float64    `json:"close_price"`
	ChangePercent     float64    `json:"change_percent"`
	TurnoverRate      float64    `json:"turnover_rate"`
	Reason            string     `json:"reason"`
	Summary           string     `json:"summary,omitempty"`
	BuyAmount         float64    `json:"buy_amount"`
	SellAmount        float64    `json:"sell_amount"`
	NetAmount         float64    `json:"net_amount"`
	InstitutionBuyers int        `json:"institution_buyers"`
	BuySeats          int        `json:"buy_seats"`
	SellSeats         int        `json:"sell_seats"`
	Meta              SourceMeta `json:"meta"`
}

type MarketBillboardSeat struct {
	Direction   string  `json:"direction"`
	Rank        int     `json:"rank"`
	Name        string  `json:"name"`
	BuyAmount   float64 `json:"buy_amount"`
	BuyRatio    float64 `json:"buy_ratio"`
	SellAmount  float64 `json:"sell_amount"`
	SellRatio   float64 `json:"sell_ratio"`
	NetAmount   float64 `json:"net_amount"`
	Institution bool    `json:"institution"`
	// Source labels are optional enrichment from a third-party provider such as
	// 同花顺. They describe that provider's classification, not an official
	// identity confirmation.
	SourceLabel     string `json:"source_label,omitempty"`
	Source          string `json:"source,omitempty"`
	LabelConfidence string `json:"label_confidence,omitempty"`
	LabelNote       string `json:"label_note,omitempty"`
}

type MarketBillboardDetail struct {
	TradeDate string                `json:"trade_date"`
	Symbol    string                `json:"symbol"`
	Reason    string                `json:"reason"`
	BuySeats  []MarketBillboardSeat `json:"buy_seats"`
	SellSeats []MarketBillboardSeat `json:"sell_seats"`
	Meta      SourceMeta            `json:"meta"`
}

type MarketResearchItem struct {
	Kind           string     `json:"kind"`
	ID             string     `json:"id"`
	Symbol         string     `json:"symbol,omitempty"`
	StockName      string     `json:"stock_name,omitempty"`
	IndustryCode   string     `json:"industry_code,omitempty"`
	IndustryName   string     `json:"industry_name,omitempty"`
	Title          string     `json:"title"`
	Content        string     `json:"content,omitempty"`
	Organization   string     `json:"organization,omitempty"`
	Researchers    string     `json:"researchers,omitempty"`
	Rating         string     `json:"rating,omitempty"`
	PreviousRating string     `json:"previous_rating,omitempty"`
	RatingChange   string     `json:"rating_change,omitempty"`
	TargetLow      float64    `json:"target_low,omitempty"`
	TargetHigh     float64    `json:"target_high,omitempty"`
	EPS            float64    `json:"eps,omitempty"`
	PE             float64    `json:"pe,omitempty"`
	Category       string     `json:"category,omitempty"`
	PublishedAt    time.Time  `json:"published_at"`
	URL            string     `json:"url"`
	Meta           SourceMeta `json:"meta"`
}
