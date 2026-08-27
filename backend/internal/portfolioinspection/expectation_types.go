package portfolioinspection

import "time"

const ExpectationPromptVersion = "portfolio-tomorrow-expectation-v1"

type ExpectationRequest struct {
	SummaryDate   string        `json:"summary_date"`
	TraderProfile TraderProfile `json:"trader_profile"`
	Holdings      []Holding     `json:"holdings"`
	Force         bool          `json:"force,omitempty"`
}

type ReviewExposure struct {
	Symbol            string   `json:"symbol"`
	Alignment         string   `json:"alignment"`
	ReviewEvidence    []string `json:"review_evidence"`
	StructureEvidence []string `json:"structure_evidence"`
}

type ExpectationHoldingAction struct {
	Symbol           string `json:"symbol"`
	ExpectedBehavior string `json:"expected_behavior"`
	Action           string `json:"action"`
	Confirmation     string `json:"confirmation"`
	Invalidation     string `json:"invalidation"`
}

type ExpectationScenario struct {
	Key                   string                     `json:"key"`
	Name                  string                     `json:"name"`
	MarketTriggers        []string                   `json:"market_triggers"`
	PortfolioImpact       string                     `json:"portfolio_impact"`
	TotalPositionResponse string                     `json:"total_position_response"`
	HoldingResponses      []ExpectationHoldingAction `json:"holding_responses"`
}

type ExpectationHolding struct {
	Symbol              string `json:"symbol"`
	Priority            string `json:"priority"`
	OpeningExpectation  string `json:"opening_expectation"`
	IntradayExpectation string `json:"intraday_expectation"`
	CloseExpectation    string `json:"close_expectation"`
	CostContext         string `json:"cost_context"`
	BeforeTrigger       string `json:"before_trigger"`
	PositiveTrigger     string `json:"positive_trigger"`
	NegativeTrigger     string `json:"negative_trigger"`
	Invalidation        string `json:"invalidation"`
}

type ExpectationTimeline struct {
	PreOpen    []string `json:"pre_open"`
	Opening30M []string `json:"opening_30m"`
	Intraday   []string `json:"intraday"`
	Close      []string `json:"close"`
}

type ExpectationConclusion struct {
	Headline        string                `json:"headline"`
	PortfolioBias   string                `json:"portfolio_bias"`
	CoreConflict    string                `json:"core_conflict"`
	ReviewExposure  []ReviewExposure      `json:"review_exposure"`
	Scenarios       []ExpectationScenario `json:"scenarios"`
	Holdings        []ExpectationHolding  `json:"holdings"`
	Timeline        ExpectationTimeline   `json:"timeline"`
	RiskAlerts      []string              `json:"risk_alerts"`
	DataLimitations []string              `json:"data_limitations"`
	Confidence      float64               `json:"confidence"`
	Source          string                `json:"source"`
}

type ExpectationReport struct {
	ID            string                `json:"id"`
	TradeDate     string                `json:"trade_date"`
	PromptVersion string                `json:"prompt_version"`
	Profile       ProfileRules          `json:"profile"`
	Holdings      []HoldingResult       `json:"holdings"`
	Metrics       Metrics               `json:"metrics"`
	Conclusion    ExpectationConclusion `json:"conclusion"`
	GeneratedAt   time.Time             `json:"generated_at"`
}

type ExpectationJob struct {
	ID              string             `json:"id"`
	Status          string             `json:"status"`
	Stage           string             `json:"stage"`
	PromptVersion   string             `json:"prompt_version"`
	PortfolioHash   string             `json:"portfolio_hash"`
	SummaryHash     string             `json:"summary_hash"`
	Request         ExpectationRequest `json:"request"`
	Results         []HoldingResult    `json:"results"`
	CompletedStocks int                `json:"completed_stocks"`
	TotalStocks     int                `json:"total_stocks"`
	CoveragePercent float64            `json:"coverage_percent"`
	CurrentSymbols  []string           `json:"current_symbols"`
	Message         string             `json:"message"`
	Error           string             `json:"error,omitempty"`
	StartedAt       time.Time          `json:"started_at,omitempty"`
	UpdatedAt       time.Time          `json:"updated_at,omitempty"`
	CompletedAt     time.Time          `json:"completed_at,omitempty"`
	ReportAvailable bool               `json:"report_available"`
	Cached          bool               `json:"cached,omitempty"`
	Report          *ExpectationReport `json:"report,omitempty"`
}
