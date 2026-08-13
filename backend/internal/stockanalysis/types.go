package stockanalysis

import (
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
)

type Input struct {
	Symbol          string
	Quote           foundation.Quote
	KLines          []foundation.KLine
	BenchmarkSymbol string
	BenchmarkName   string
	BenchmarkKLines []foundation.KLine
	LimitUps        []foundation.LimitUpEvent
	Concepts        []string
	Industry        string
	Business        string
	BusinessDetail  string
	BusinessSource  string
	Fundamentals    *foundation.StockFundamentals
	Reports         []foundation.MarketResearchItem
	CachedThemes    []foundation.StockThemeAttribution
	Themes          []foundation.ThemeOverview
	MarketEmotion   *marketemotion.Snapshot
	News            []foundation.NewsItem
	CollectionGaps  []string
}

type Analysis struct {
	Symbol      string               `json:"symbol"`
	Name        string               `json:"name"`
	GeneratedAt time.Time            `json:"generated_at"`
	Quote       foundation.Quote     `json:"quote"`
	Profile     Profile              `json:"profile"`
	Conclusion  Conclusion           `json:"conclusion"`
	Trend       TrendAnalysis        `json:"trend"`
	ShortTerm   ShortTermAnalysis    `json:"short_term"`
	Theme       ThemeAnalysis        `json:"theme"`
	Fundamental *FundamentalAnalysis `json:"fundamental,omitempty"`
	Research    *ResearchAnalysis    `json:"research,omitempty"`
	Market      *MarketContext       `json:"market,omitempty"`
	Scorecard   Scorecard            `json:"scorecard"`
	Timeframes  []TimeframeAnalysis  `json:"timeframes"`
	Relative    RelativeStrength     `json:"relative_strength"`
	Signals     []Signal             `json:"signals"`
	NextDay     NextDayPlan          `json:"next_day"`
	RiskControl RiskControl          `json:"risk_control"`
	ActionPlan  ActionPlan           `json:"action_plan"`
	Risks       []string             `json:"risks"`
	Evidence    []Evidence           `json:"evidence"`
	DataQuality []DataQuality        `json:"data_quality"`
	Chart       []TrendPoint         `json:"chart"`
	AI          AISynthesisStatus    `json:"ai"`
}

type Profile struct {
	PrimaryType string   `json:"primary_type"`
	TypeLabel   string   `json:"type_label"`
	PricePhase  string   `json:"price_phase"`
	MarketRole  string   `json:"market_role"`
	Tags        []string `json:"tags"`
	Confidence  float64  `json:"confidence"`
}

type Conclusion struct {
	Headline string `json:"headline"`
	Summary  string `json:"summary"`
	Action   string `json:"action"`
	BestPath string `json:"best_path"`
	MainRisk string `json:"main_risk"`
	Source   string `json:"source"`
}

type TrendAnalysis struct {
	Score               int      `json:"score"`
	Strength            string   `json:"strength"`
	Phase               string   `json:"phase"`
	Setup               string   `json:"setup"`
	LatestClose         float64  `json:"latest_close"`
	MA20                float64  `json:"ma20"`
	MA60                float64  `json:"ma60"`
	MA120               float64  `json:"ma120"`
	Return20            float64  `json:"return_20d"`
	Return60            float64  `json:"return_60d"`
	Return120           float64  `json:"return_120d"`
	RangePosition60     float64  `json:"range_position_60d"`
	DrawdownFromHigh120 float64  `json:"drawdown_from_high_120d"`
	VolumeRatio         float64  `json:"volume_ratio_5d_20d"`
	ATR14Percent        float64  `json:"atr_14_percent"`
	Support             float64  `json:"support"`
	Resistance          float64  `json:"resistance"`
	Invalidation        string   `json:"invalidation"`
	Reasons             []string `json:"reasons"`
}

type Scorecard struct {
	Overall         int              `json:"overall"`
	Grade           string           `json:"grade"`
	Direction       string           `json:"direction"`
	Conviction      string           `json:"conviction"`
	Dimensions      []DimensionScore `json:"dimensions"`
	PositiveSignals []string         `json:"positive_signals"`
	NegativeSignals []string         `json:"negative_signals"`
}

type DimensionScore struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	Score  int     `json:"score"`
	Weight float64 `json:"weight"`
	Status string  `json:"status"`
	Detail string  `json:"detail"`
}

type TimeframeAnalysis struct {
	Key        string  `json:"key"`
	Label      string  `json:"label"`
	Window     int     `json:"window"`
	Score      int     `json:"score"`
	State      string  `json:"state"`
	Return     float64 `json:"return_percent"`
	MA         float64 `json:"moving_average"`
	Slope      float64 `json:"slope_percent"`
	AboveMA    bool    `json:"above_moving_average"`
	Support    float64 `json:"support"`
	Resistance float64 `json:"resistance"`
}

type RelativeStrength struct {
	Available         bool    `json:"available"`
	BenchmarkSymbol   string  `json:"benchmark_symbol,omitempty"`
	BenchmarkName     string  `json:"benchmark_name,omitempty"`
	StockReturn20     float64 `json:"stock_return_20d"`
	StockReturn60     float64 `json:"stock_return_60d"`
	BenchmarkReturn20 float64 `json:"benchmark_return_20d"`
	BenchmarkReturn60 float64 `json:"benchmark_return_60d"`
	ExcessReturn20    float64 `json:"excess_return_20d"`
	ExcessReturn60    float64 `json:"excess_return_60d"`
	Score             int     `json:"score"`
	State             string  `json:"state"`
	Detail            string  `json:"detail"`
}

type Signal struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Tone     string `json:"tone"`
	Strength int    `json:"strength"`
	Detail   string `json:"detail"`
}

type PriceLevel struct {
	Label  string  `json:"label"`
	Price  float64 `json:"price"`
	Detail string  `json:"detail"`
}

type NextDayScenario struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Priority     string `json:"priority"`
	Trigger      string `json:"trigger"`
	Confirmation string `json:"confirmation"`
	Action       string `json:"action"`
	Invalidation string `json:"invalidation"`
}

type NextDayPlan struct {
	Bias           string            `json:"bias"`
	Score          int               `json:"score"`
	Expectation    string            `json:"expectation"`
	ExpectedLow    float64           `json:"expected_low"`
	ExpectedHigh   float64           `json:"expected_high"`
	Levels         []PriceLevel      `json:"levels"`
	Scenarios      []NextDayScenario `json:"scenarios"`
	PreOpenChecks  []string          `json:"pre_open_checks"`
	OpeningChecks  []string          `json:"opening_checks"`
	IntradayChecks []string          `json:"intraday_checks"`
	CloseChecks    []string          `json:"close_checks"`
}

type RiskControl struct {
	Level                string   `json:"level"`
	Score                int      `json:"score"`
	EntryReference       float64  `json:"entry_reference"`
	StopPrice            float64  `json:"stop_price"`
	StopPercent          float64  `json:"stop_percent"`
	TakeProfitFirst      float64  `json:"take_profit_first"`
	TakeProfitSecond     float64  `json:"take_profit_second"`
	RiskReward           float64  `json:"risk_reward"`
	SuggestedPositionMin int      `json:"suggested_position_min_percent"`
	SuggestedPositionMax int      `json:"suggested_position_max_percent"`
	SingleTradeRisk      float64  `json:"single_trade_risk_percent"`
	PositionFormula      string   `json:"position_formula"`
	Rules                []string `json:"rules"`
}

type ShortTermAnalysis struct {
	State            string   `json:"state"`
	LimitUpCount20   int      `json:"limit_up_count_20d"`
	MaxLimitStreak20 int      `json:"max_limit_streak_20d"`
	ExactLimitUpData bool     `json:"exact_limit_up_data"`
	LatestStreak     int      `json:"latest_streak"`
	LatestOpenCount  int      `json:"latest_open_count"`
	LatestTurnover   float64  `json:"latest_turnover_rate"`
	AverageAmount20  float64  `json:"average_amount_20d"`
	RecentReturn5    float64  `json:"return_5d"`
	RecentReturn10   float64  `json:"return_10d"`
	Tradability      string   `json:"tradability"`
	Reasons          []string `json:"reasons"`
}

type ThemeAnalysis struct {
	Primary     string   `json:"primary"`
	Business    string   `json:"business"`
	HotTheme    string   `json:"hot_theme,omitempty"`
	IsHot       bool     `json:"is_hot"`
	Concepts    []string `json:"concepts"`
	Source      string   `json:"source"`
	AsOf        string   `json:"as_of,omitempty"`
	Evidence    []string `json:"evidence,omitempty"`
	Route       string   `json:"route"`
	TrendScore  int      `json:"trend_score"`
	TrendStage  string   `json:"trend_stage"`
	ActiveDays  int      `json:"active_days"`
	MaxStreak   int      `json:"max_streak"`
	Role        string   `json:"role"`
	Description string   `json:"description"`
}

type FundamentalAnalysis struct {
	Available                 bool    `json:"available"`
	Score                     int     `json:"score"`
	Quality                   string  `json:"quality"`
	ReportDate                string  `json:"report_date"`
	ReportName                string  `json:"report_name"`
	Revenue                   float64 `json:"revenue"`
	RevenueYearOverYear       float64 `json:"revenue_yoy"`
	NetProfit                 float64 `json:"net_profit"`
	NetProfitYearOverYear     float64 `json:"net_profit_yoy"`
	EPS                       float64 `json:"eps"`
	ROE                       float64 `json:"roe"`
	GrossMargin               float64 `json:"gross_margin"`
	DebtRatio                 float64 `json:"debt_ratio"`
	OperatingCashFlowPerShare float64 `json:"operating_cash_flow_per_share"`
	Summary                   string  `json:"summary"`
	Source                    string  `json:"source"`
}

type ResearchAnalysis struct {
	Available         bool                            `json:"available"`
	Score             int                             `json:"score"`
	Coverage          string                          `json:"coverage"`
	ReportCount       int                             `json:"report_count"`
	OrganizationCount int                             `json:"organization_count"`
	LatestRating      string                          `json:"latest_rating,omitempty"`
	RatingChanges     []string                        `json:"rating_changes,omitempty"`
	Summary           string                          `json:"summary"`
	Reports           []foundation.MarketResearchItem `json:"reports"`
}

type MarketContext struct {
	TradeDate  string  `json:"trade_date"`
	Phase      string  `json:"phase"`
	Score      float64 `json:"score"`
	Confidence string  `json:"confidence"`
	Source     string  `json:"source"`
}

type ActionPlan struct {
	CurrentAction   string   `json:"current_action"`
	EntryConditions []string `json:"entry_conditions"`
	HoldConditions  []string `json:"hold_conditions"`
	AvoidConditions []string `json:"avoid_conditions"`
	Invalidation    string   `json:"invalidation"`
	PositionHint    string   `json:"position_hint"`
}

type Evidence struct {
	Category string `json:"category"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Source   string `json:"source"`
	AsOf     string `json:"as_of,omitempty"`
}

type DataQuality struct {
	Key     string `json:"key"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type TrendPoint struct {
	Date  string   `json:"date"`
	Close float64  `json:"close"`
	MA20  *float64 `json:"ma20,omitempty"`
	MA60  *float64 `json:"ma60,omitempty"`
	MA120 *float64 `json:"ma120,omitempty"`
}

type AISynthesisStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Model   string `json:"model,omitempty"`
}
