package stockanalysis

import (
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
)

type Input struct {
	Symbol             string
	Quote              foundation.Quote
	KLines             []foundation.KLine
	BenchmarkSymbol    string
	BenchmarkName      string
	BenchmarkKLines    []foundation.KLine
	LimitUps           []foundation.LimitUpEvent
	Catalog            []foundation.StockCatalogEntry
	Concepts           []string
	Industry           string
	Business           string
	BusinessDetail     string
	BusinessSource     string
	Fundamentals       *foundation.StockFundamentals
	Reports            []foundation.MarketResearchItem
	Announcements      []foundation.MarketResearchItem
	ModelThemeEvidence []ThemeEvidence
	CachedThemes       []foundation.StockThemeAttribution
	Themes             []foundation.ThemeOverview
	MarketEmotion      *marketemotion.Snapshot
	News               []foundation.NewsItem
	CollectionGaps     []string
}

type Analysis struct {
	Symbol                string               `json:"symbol"`
	Name                  string               `json:"name"`
	GeneratedAt           time.Time            `json:"generated_at"`
	Quote                 foundation.Quote     `json:"quote"`
	Profile               Profile              `json:"profile"`
	Conclusion            Conclusion           `json:"conclusion"`
	Trend                 TrendAnalysis        `json:"trend"`
	ShortTerm             ShortTermAnalysis    `json:"short_term"`
	Theme                 ThemeAnalysis        `json:"theme"`
	Fundamental           *FundamentalAnalysis `json:"fundamental,omitempty"`
	Research              *ResearchAnalysis    `json:"research,omitempty"`
	StockNews             *NewsAnalysis        `json:"stock_news,omitempty"`
	ThemeNews             *NewsAnalysis        `json:"theme_news,omitempty"`
	Market                *MarketContext       `json:"market,omitempty"`
	Scorecard             Scorecard            `json:"scorecard"`
	Timeframes            []TimeframeAnalysis  `json:"timeframes"`
	Relative              RelativeStrength     `json:"relative_strength"`
	Signals               []Signal             `json:"signals"`
	NextDay               NextDayPlan          `json:"next_day"`
	RiskControl           RiskControl          `json:"risk_control"`
	ActionPlan            ActionPlan           `json:"action_plan"`
	Risks                 []string             `json:"risks"`
	Evidence              []Evidence           `json:"evidence"`
	DataQuality           []DataQuality        `json:"data_quality"`
	Chart                 []TrendPoint         `json:"chart"`
	AI                    AISynthesisStatus    `json:"ai"`
	shortTermQuantitative *ShortTermQuantitativePlan
	dailyBars             []AIDailyBar
}

// AIDailyBar is the compact daily OHLCV representation sent to Hermes.
// Keeping only the fields used for price planning avoids leaking provider
// metadata and keeps the prompt bounded while preserving the recent price
// structure, volume and turnover context.
type AIDailyBar struct {
	Date          string  `json:"date"`
	Open          float64 `json:"open"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Close         float64 `json:"close"`
	Volume        float64 `json:"volume"`
	Amount        float64 `json:"amount"`
	TurnoverRate  float64 `json:"turnover_rate,omitempty"`
	ChangePercent float64 `json:"change_percent,omitempty"`
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
	Primary           string          `json:"primary"`
	Business          string          `json:"business"`
	HotTheme          string          `json:"hot_theme,omitempty"`
	IsHot             bool            `json:"is_hot"`
	Concepts          []string        `json:"concepts"`
	Source            string          `json:"source"`
	AsOf              string          `json:"as_of,omitempty"`
	Evidence          []string        `json:"evidence,omitempty"`
	Route             string          `json:"route"`
	TrendScore        int             `json:"trend_score"`
	TrendStage        string          `json:"trend_stage"`
	ActiveDays        int             `json:"active_days"`
	MaxStreak         int             `json:"max_streak"`
	Role              string          `json:"role"`
	Description       string          `json:"description"`
	HotScore          int             `json:"hot_score"`
	Confidence        string          `json:"confidence"`
	BusinessTheme     string          `json:"business_theme,omitempty"`
	ConfirmedThemes   []ThemeTag      `json:"confirmed_themes,omitempty"`
	SpeculativeThemes []ThemeTag      `json:"speculative_themes,omitempty"`
	EvidenceItems     []ThemeEvidence `json:"evidence_items,omitempty"`
	Resonance         ThemeResonance  `json:"resonance"`
}

// ThemeEvidence is a traceable fact used to attribute a stock to a theme.
// Evidence is deliberately separated from the final label so a user can
// distinguish company facts, market attribution and model inference.
type ThemeEvidence struct {
	Theme       string    `json:"theme"`
	Type        string    `json:"type"`
	Source      string    `json:"source"`
	Title       string    `json:"title"`
	URL         string    `json:"url,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	Snippet     string    `json:"snippet,omitempty"`
	Strength    float64   `json:"strength"`
	Freshness   float64   `json:"freshness"`
}

type ThemeTag struct {
	Name          string `json:"name"`
	Layer         string `json:"layer"`
	Confidence    string `json:"confidence"`
	Score         int    `json:"score"`
	EvidenceCount int    `json:"evidence_count"`
	Detail        string `json:"detail"`
}

type ThemeResonance struct {
	Available        bool   `json:"available"`
	Score            int    `json:"score"`
	State            string `json:"state"`
	Detail           string `json:"detail"`
	StockMomentum    int    `json:"stock_momentum"`
	RelativeStrength int    `json:"relative_strength"`
	Breadth          int    `json:"breadth"`
	LimitUpEnergy    int    `json:"limit_up_energy"`
	Persistence      int    `json:"persistence"`
	LeaderPosition   int    `json:"leader_position"`
	EvidenceQuality  int    `json:"evidence_quality"`
	CapitalDiffusion int    `json:"capital_diffusion"`
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

type NewsAnalysis struct {
	Available      bool                  `json:"available"`
	WindowDays     int                   `json:"window_days"`
	ArticleCount   int                   `json:"article_count"`
	SourceCount    int                   `json:"source_count"`
	LatestAt       string                `json:"latest_at,omitempty"`
	Tone           string                `json:"tone"`
	Summary        string                `json:"summary"`
	Catalysts      []string              `json:"catalysts"`
	Risks          []string              `json:"risks"`
	Keywords       []string              `json:"keywords"`
	Articles       []foundation.NewsItem `json:"articles"`
	AnalysisSource string                `json:"analysis_source"`
}

type MarketContext struct {
	TradeDate  string  `json:"trade_date"`
	Phase      string  `json:"phase"`
	Score      float64 `json:"score"`
	Confidence string  `json:"confidence"`
	Source     string  `json:"source"`
}

type ActionPriceZone struct {
	Label     string  `json:"label"`
	PriceLow  float64 `json:"price_low"`
	PriceHigh float64 `json:"price_high"`
	PriceText string  `json:"price_text"`
	Reason    string  `json:"reason"`
	Action    string  `json:"action"`
}

type ShortTermDecisionStage struct {
	Label    string   `json:"label"`
	Status   string   `json:"status"`
	Summary  string   `json:"summary"`
	Required []string `json:"required"`
	Avoid    []string `json:"avoid"`
}

type ShortTermDecisionScenario struct {
	Name      string `json:"name"`
	Tone      string `json:"tone"`
	Condition string `json:"condition"`
	Action    string `json:"action"`
}

type ShortTermStockThresholds struct {
	ReferenceClose     float64 `json:"reference_close"`
	LimitUpPercent     float64 `json:"limit_up_percent"`
	AuctionChangeMin   float64 `json:"auction_change_min"`
	AuctionChangeMax   float64 `json:"auction_change_max"`
	AuctionPriceMin    float64 `json:"auction_price_min"`
	AuctionPriceMax    float64 `json:"auction_price_max"`
	AuctionAmountMin   float64 `json:"auction_amount_min"`
	AuctionAmountMax   float64 `json:"auction_amount_max"`
	OpeningDrawdownMax float64 `json:"opening_drawdown_max"`
	OpeningAmountMin   float64 `json:"opening_amount_min"`
	RelativeIndexMin   float64 `json:"relative_index_min"`
}

type ShortTermBenchmarkThresholds struct {
	Symbol           string  `json:"symbol"`
	Name             string  `json:"name"`
	ReferenceClose   float64 `json:"reference_close"`
	AuctionChangeMin float64 `json:"auction_change_min"`
	OpeningChangeMin float64 `json:"opening_change_min"`
	FailureChange    float64 `json:"failure_change"`
}

type ShortTermThemeThresholds struct {
	Name                 string  `json:"name"`
	LimitUpCount         int     `json:"limit_up_count"`
	BoardCount           int     `json:"board_count"`
	MaxStreak            int     `json:"max_streak"`
	ActiveDays           int     `json:"active_days"`
	MinimumPositivePeers int     `json:"minimum_positive_peers"`
	MaximumWeakPeers     int     `json:"maximum_weak_peers"`
	PositiveThreshold    float64 `json:"positive_threshold"`
	WeakThreshold        float64 `json:"weak_threshold"`
	Source               string  `json:"source"`
}

type ShortTermPeerReference struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	Role          string  `json:"role"`
	Streak        int     `json:"streak"`
	ChangePercent float64 `json:"change_percent"`
	HasQuote      bool    `json:"has_quote"`
}

type ShortTermQuantitativePlan struct {
	BaselineDate string                       `json:"baseline_date"`
	Stock        ShortTermStockThresholds     `json:"stock"`
	Benchmark    ShortTermBenchmarkThresholds `json:"benchmark"`
	Theme        ShortTermThemeThresholds     `json:"theme"`
	Peers        []ShortTermPeerReference     `json:"peers"`
	Missing      []string                     `json:"missing,omitempty"`
}

// ShortTermPlaybook deliberately models an execution state machine instead of
// pretending that an after-hours static price is a reliable entry instruction.
// Auction and opening data are not available in an after-hours analysis, so the
// generated result describes the evidence that must be observed before acting.
type ShortTermPlaybook struct {
	Positioning             string                      `json:"positioning"`
	SentimentCycle          string                      `json:"sentiment_cycle"`
	ExpectedPattern         string                      `json:"expected_pattern"`
	OvernightConclusion     string                      `json:"overnight_conclusion"`
	DataStatus              string                      `json:"data_status"`
	Quantitative            ShortTermQuantitativePlan   `json:"quantitative"`
	Auction                 ShortTermDecisionStage      `json:"auction"`
	Opening                 ShortTermDecisionStage      `json:"opening"`
	ParticipationConditions []string                    `json:"participation_conditions"`
	HoldConditions          []string                    `json:"hold_conditions"`
	ExitConditions          []string                    `json:"exit_conditions"`
	VetoConditions          []string                    `json:"veto_conditions"`
	Scenarios               []ShortTermDecisionScenario `json:"scenarios"`
}

type ActionPlan struct {
	DecisionMode       string             `json:"decision_mode"`
	DecisionLabel      string             `json:"decision_label"`
	DecisionConfidence float64            `json:"decision_confidence"`
	Horizon            string             `json:"horizon"`
	Rationale          string             `json:"rationale"`
	PricingSource      string             `json:"pricing_source"`
	CurrentAction      string             `json:"current_action"`
	Entry              ActionPriceZone    `json:"entry"`
	Hold               ActionPriceZone    `json:"hold"`
	TakeProfit         ActionPriceZone    `json:"take_profit"`
	StopLoss           ActionPriceZone    `json:"stop_loss"`
	ShortTerm          *ShortTermPlaybook `json:"short_term_playbook,omitempty"`
	EntryConditions    []string           `json:"entry_conditions"`
	HoldConditions     []string           `json:"hold_conditions"`
	AvoidConditions    []string           `json:"avoid_conditions"`
	Invalidation       string             `json:"invalidation"`
	PositionHint       string             `json:"position_hint"`
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
