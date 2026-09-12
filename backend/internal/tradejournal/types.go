package tradejournal

import "time"

const (
	MaxTrades        = 5000
	AlgorithmVersion = "trade-journal-v1"
)

// RawTrade 券商交割单/流水的一行原始记录。字段名兼容同花顺/东财/富途/通用 CSV 的中文表头。
type RawTrade struct {
	OccurDate string  `json:"occur_date"`
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name,omitempty"`
	Operation string  `json:"operation"`
	Price     float64 `json:"price"`
	Volume    float64 `json:"volume"`
	Amount    float64 `json:"amount,omitempty"`
	Fee       float64 `json:"fee,omitempty"`
}

// RoundTripFIFO 配对成交的一笔完整交易（开仓→清仓），Vibe-Trading Trade Journal 的核心口径。
type RoundTrip struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name,omitempty"`
	OpenDate      string  `json:"open_date"`
	CloseDate     string  `json:"close_date"`
	HoldingDays   float64 `json:"holding_days"`
	Direction     string  `json:"direction"`
	Volume        float64 `json:"volume"`
	OpenPrice     float64 `json:"open_price"`
	ClosePrice    float64 `json:"close_price"`
	NetProfit     float64 `json:"net_profit"`
	ProfitRate    float64 `json:"profit_rate"`
	Fees          float64 `json:"fees"`
	MaxAdversePct float64 `json:"max_adverse_pct"`
	Outcome       string  `json:"outcome"`
}

// Statistics 整体交易画像。
type Statistics struct {
	TotalTrades    int          `json:"total_trades"`
	WinCount       int          `json:"win_count"`
	LossCount      int          `json:"loss_count"`
	WinRate        float64      `json:"win_rate"`
	AvgWin         float64      `json:"avg_win"`
	AvgLoss        float64      `json:"avg_loss"`
	ProfitFactor   float64      `json:"profit_factor"`
	PayoffRatio    float64      `json:"payoff_ratio"`
	Expectancy     float64      `json:"expectancy"`
	TotalNetProfit float64      `json:"total_net_profit"`
	MaxDrawdown    float64      `json:"max_drawdown"`
	MaxDrawdownPct float64      `json:"max_drawdown_pct"`
	AvgHoldingDays float64      `json:"avg_holding_days"`
	MaxHoldingDays float64      `json:"max_holding_days"`
	TradePerWeek   float64      `json:"trade_per_week"`
	ActiveDays     int          `json:"active_days"`
	BestTrade      *RoundTrip   `json:"best_trade,omitempty"`
	WorstTrade     *RoundTrip   `json:"worst_trade,omitempty"`
	ProfitCurve    []CurvePoint `json:"profit_curve"`
}

type CurvePoint struct {
	Index      int     `json:"index"`
	CloseDate  string  `json:"close_date"`
	Cumulative float64 `json:"cumulative"`
}

// Bias 行为偏差诊断结果（处置效应/追涨/过度交易等四类，沿用 Vibe-Trading 的诊断框架）。
type Bias struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	Severity   string  `json:"severity"`
	Score      float64 `json:"score"`
	Evidence   string  `json:"evidence"`
	Suggestion string  `json:"suggestion"`
}

type AnalyzeRequest struct {
	// CSV 文本内容直接上传（前端读取文件后提交），UTF-8。
	CSVContent string `json:"csv_content"`
	Filename   string `json:"filename,omitempty"`
}

type AnalyzeResult struct {
	AlgorithmVersion string         `json:"algorithm_version"`
	Filename         string         `json:"filename,omitempty"`
	Imported         int            `json:"imported"`
	Skipped          int            `json:"skipped"`
	ParsedRange      [2]string      `json:"parsed_range"`
	Trades           []RoundTrip    `json:"trades"`
	Statistics       Statistics     `json:"statistics"`
	Biases           []Bias         `json:"biases"`
	OpenPositions    []OpenPosition `json:"open_positions"`
	AnalyzedAt       time.Time      `json:"analyzed_at"`
}

type OpenPosition struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name,omitempty"`
	Volume    float64 `json:"volume"`
	OpenPrice float64 `json:"open_price"`
	OpenDate  string  `json:"open_date"`
}

type HistoryEntry struct {
	ID             string         `json:"id"`
	Filename       string         `json:"filename,omitempty"`
	Imported       int            `json:"imported"`
	Trades         int            `json:"trades"`
	WinRate        float64        `json:"win_rate"`
	TotalProfit    float64        `json:"total_profit"`
	MaxDrawdownPct float64        `json:"max_drawdown_pct"`
	BiasCount      int            `json:"bias_count"`
	AnalyzedAt     time.Time      `json:"analyzed_at"`
	Result         *AnalyzeResult `json:"result,omitempty"`
}
