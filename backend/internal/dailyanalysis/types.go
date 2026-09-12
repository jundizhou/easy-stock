package dailyanalysis

import (
	"time"
)

const (
	MaxSymbols       = 20
	PromptVersion    = "daily-analysis-v1"
	AlgorithmVersion = "watchlist-decision-v1"
	configKey        = "daily-analysis-config"
)

// Config 自选股日报的全局配置，保存在模块数据库中。
type Config struct {
	Watchlist []string   `json:"watchlist"`
	AutoRun   bool       `json:"auto_run"`
	RunHour   int        `json:"run_hour"`
	RunMinute int        `json:"run_minute"`
	AIEnhance bool       `json:"ai_enhance"`
	Push      PushConfig `json:"push"`
}

type PushConfig struct {
	Enabled       bool   `json:"enabled"`
	WeComWebhook  string `json:"wecom_webhook,omitempty"`
	FeishuWebhook string `json:"feishu_webhook,omitempty"`
}

type Request struct {
	Symbols   []string `json:"symbols"`
	AIEnhance bool     `json:"ai_enhance"`
}

// Indicators 单只股票的技术指标快照，全部基于日 K 计算。
type Indicators struct {
	MA5           float64 `json:"ma5"`
	MA10          float64 `json:"ma10"`
	MA20          float64 `json:"ma20"`
	MA60          float64 `json:"ma60"`
	MACDDif       float64 `json:"macd_dif"`
	MACDDea       float64 `json:"macd_dea"`
	MACDHist      float64 `json:"macd_hist"`
	KDJk          float64 `json:"kdj_k"`
	KDJd          float64 `json:"kdj_d"`
	KDJj          float64 `json:"kdj_j"`
	Rsi6          float64 `json:"rsi6"`
	Rsi14         float64 `json:"rsi14"`
	BollUpper     float64 `json:"boll_upper"`
	BollMid       float64 `json:"boll_mid"`
	BollLower     float64 `json:"boll_lower"`
	VolumeRatio   float64 `json:"volume_ratio"`
	Support       float64 `json:"support"`
	Resistance    float64 `json:"resistance"`
	TwentyDayLow  float64 `json:"twenty_day_low"`
	TwentyDayHigh float64 `json:"twenty_day_high"`
}

type StockReport struct {
	Symbol        string     `json:"symbol"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	Error         string     `json:"error,omitempty"`
	TradeDate     string     `json:"trade_date,omitempty"`
	Price         float64    `json:"price"`
	ChangePercent float64    `json:"change_percent"`
	Score         int        `json:"score"`
	Action        string     `json:"action"`
	Trend         string     `json:"trend"`
	TrendDetail   string     `json:"trend_detail"`
	Indicators    Indicators `json:"indicators"`
	Signals       []string   `json:"signals"`
	Risks         []string   `json:"risks"`
	Checklist     []string   `json:"checklist"`
	Commentary    string     `json:"commentary,omitempty"`
}

type MarketSummary struct {
	BullCount    int      `json:"bull_count"`
	NeutralCount int      `json:"neutral_count"`
	BearCount    int      `json:"bear_count"`
	FailedCount  int      `json:"failed_count"`
	AverageScore float64  `json:"average_score"`
	Bias         string   `json:"bias"`
	Highlights   []string `json:"highlights"`
}

type PushResult struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type Report struct {
	ID            string        `json:"id"`
	TradeDate     string        `json:"trade_date"`
	Trigger       string        `json:"trigger"`
	PromptVersion string        `json:"prompt_version"`
	AIEnhanced    bool          `json:"ai_enhanced"`
	Stocks        []StockReport `json:"stocks"`
	Summary       MarketSummary `json:"summary"`
	GeneratedAt   time.Time     `json:"generated_at"`
}

type Job struct {
	ID              string       `json:"id"`
	Status          string       `json:"status"`
	Stage           string       `json:"stage"`
	Trigger         string       `json:"trigger"`
	Request         Request      `json:"request"`
	TotalStocks     int          `json:"total_stocks"`
	CompletedStocks int          `json:"completed_stocks"`
	CurrentSymbols  []string     `json:"current_symbols"`
	Message         string       `json:"message"`
	Error           string       `json:"error,omitempty"`
	StartedAt       time.Time    `json:"started_at,omitempty"`
	UpdatedAt       time.Time    `json:"updated_at,omitempty"`
	CompletedAt     time.Time    `json:"completed_at,omitempty"`
	ReportAvailable bool         `json:"report_available"`
	Report          *Report      `json:"report,omitempty"`
	PushResults     []PushResult `json:"push_results,omitempty"`
}

// CorrelationPair 自选股两两收益相关性（Vibe-Trading 风格的相关性矩阵单元）。
type CorrelationPair struct {
	LeftSymbol  string  `json:"left_symbol"`
	RightSymbol string  `json:"right_symbol"`
	Correlation float64 `json:"correlation"`
	SampleDays  int     `json:"sample_days"`
}

type CorrelationMatrix struct {
	Symbols []string          `json:"symbols"`
	Days    int               `json:"days"`
	Pairs   []CorrelationPair `json:"pairs"`
	Errors  map[string]string `json:"errors,omitempty"`
}
