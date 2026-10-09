package portfolioinspection

import (
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/stockanalysis"
)

const (
	MaxHoldings        = 10
	PromptVersion      = "portfolio-inspection-v4"
	AlgorithmVersion   = "portfolio-ai-score-v4"
	MinimumAICoverage  = 70
	DefaultConcurrency = 2
)

type TraderProfile string

const (
	ProfileAggressive TraderProfile = "aggressive"
	ProfileBalanced   TraderProfile = "balanced"
	ProfileSteady     TraderProfile = "steady"
)

type ProfileRules struct {
	ID                    TraderProfile `json:"id"`
	Label                 string        `json:"label"`
	Description           string        `json:"description"`
	MaxSinglePercent      int           `json:"max_single_percent"`
	MaxTopThreePercent    int           `json:"max_top_three_percent"`
	MinimumCashPercent    int           `json:"minimum_cash_percent"`
	MaxHighRiskPercent    int           `json:"max_high_risk_percent"`
	MaxStopLossRisk       float64       `json:"max_stop_loss_risk_percent"`
	PreferredShortTermMax int           `json:"preferred_short_term_max_percent"`
}

type Holding struct {
	Symbol    string   `json:"symbol"`
	Name      string   `json:"name,omitempty"`
	Weight    int      `json:"weight_percent"`
	CostPrice *float64 `json:"cost_price,omitempty"`
}

type Request struct {
	SourceOptimizationID string                      `json:"source_optimization_id,omitempty"`
	TraderProfile        TraderProfile               `json:"trader_profile"`
	Holdings             []Holding                   `json:"holdings"`
	Horizon              string                      `json:"horizon,omitempty"`
	ResearchLevel        stockanalysis.ResearchLevel `json:"research_level,omitempty"`
	ForceSymbols         []string                    `json:"force_symbols,omitempty"`
}

type HoldingResult struct {
	Holding            Holding                 `json:"holding"`
	Status             string                  `json:"status"`
	Error              string                  `json:"error,omitempty"`
	CompletedAt        time.Time               `json:"completed_at,omitempty"`
	Analysis           *stockanalysis.Analysis `json:"analysis,omitempty"`
	AnalysisID         string                  `json:"analysis_id,omitempty"`
	ResearchOrigin     string                  `json:"research_origin,omitempty"`
	ReportCompletedAt  time.Time               `json:"report_completed_at,omitempty"`
	ResearchCutoffAt   time.Time               `json:"research_cutoff_at,omitempty"`
	ResearchStartedAt  time.Time               `json:"research_started_at,omitempty"`
	ResearchDurationMS int64                   `json:"research_duration_ms,omitempty"`
	CurrentQuote       *foundation.Quote       `json:"current_quote,omitempty"`
	QuoteStatus        string                  `json:"quote_status,omitempty"`
	QuoteMessage       string                  `json:"quote_message,omitempty"`
}

type ThemeExposure struct {
	Theme   string `json:"theme"`
	Weight  int    `json:"weight_percent"`
	Symbols int    `json:"stock_count"`
}

type CorrelationPair struct {
	LeftSymbol  string  `json:"left_symbol"`
	RightSymbol string  `json:"right_symbol"`
	Correlation float64 `json:"correlation"`
}

type RiskContribution struct {
	Symbol  string  `json:"symbol"`
	Name    string  `json:"name"`
	Weight  int     `json:"weight_percent"`
	Score   float64 `json:"score"`
	Percent float64 `json:"contribution_percent"`
}

type Metrics struct {
	TotalPositionPercent      int                `json:"total_position_percent"`
	CashPercent               int                `json:"cash_percent"`
	CoveragePercent           float64            `json:"coverage_percent"`
	MaxSinglePercent          int                `json:"max_single_percent"`
	TopThreePercent           int                `json:"top_three_percent"`
	HHI                       float64            `json:"concentration_hhi"`
	WeightedScore             float64            `json:"weighted_stock_score"`
	WeightedRisk              float64            `json:"weighted_risk_score"`
	StopLossRiskPercent       float64            `json:"stop_loss_risk_percent"`
	StopLossCoveragePercent   float64            `json:"stop_loss_coverage_percent"`
	AIResearchCoveragePercent float64            `json:"ai_research_coverage_percent"`
	ShortTermPercent          int                `json:"short_term_percent"`
	NewListingPercent         int                `json:"new_listing_percent"`
	HighRiskPercent           int                `json:"high_risk_percent"`
	HealthScore               int                `json:"health_score"`
	HealthScoreAvailable      bool               `json:"health_score_available"`
	RiskResilienceScore       int                `json:"risk_resilience_score"`
	DiversificationScore      int                `json:"diversification_score"`
	StyleMatchScore           int                `json:"style_match_score"`
	StyleBreaches             []string           `json:"style_breaches"`
	ThemeExposures            []ThemeExposure    `json:"theme_exposures"`
	HighCorrelations          []CorrelationPair  `json:"high_correlations"`
	RiskContributions         []RiskContribution `json:"risk_contributions"`
}

type HoldingConclusion struct {
	Symbol           string  `json:"symbol"`
	PortfolioRole    string  `json:"portfolio_role"`
	RiskContribution float64 `json:"risk_contribution"`
	Conclusion       string  `json:"conclusion"`
	ActionPriority   string  `json:"action_priority"`
	Action           string  `json:"action"`
	Confirmation     string  `json:"confirmation"`
	Invalidation     string  `json:"invalidation"`
}

type Scenario struct {
	Name            string `json:"name"`
	Condition       string `json:"condition"`
	PortfolioAction string `json:"portfolio_action"`
}

type AIReport struct {
	TotalScore           *int                         `json:"total_score,omitempty"`
	ScoreAvailable       bool                         `json:"score_available"`
	Dimensions           []ScoreDimension             `json:"dimensions,omitempty"`
	ConfidenceLevel      string                       `json:"confidence_level,omitempty"`
	ConfidenceReason     string                       `json:"confidence_reason,omitempty"`
	RiskReason           string                       `json:"risk_reason,omitempty"`
	RiskGroups           []RiskGroup                  `json:"risk_groups,omitempty"`
	HealthScore          int                          `json:"health_score"`
	RiskLevel            string                       `json:"risk_level"`
	StyleMatch           string                       `json:"style_match"`
	ExecutiveSummary     string                       `json:"executive_summary"`
	PrimaryRisks         []string                     `json:"primary_risks"`
	ConcentrationFinding []string                     `json:"concentration_findings"`
	Holdings             []HoldingConclusion          `json:"holdings"`
	AdjustmentOrder      []string                     `json:"adjustment_order"`
	Scenarios            []Scenario                   `json:"scenarios"`
	NextChecklist        []string                     `json:"next_checklist"`
	DataLimitations      []string                     `json:"data_limitations"`
	ExplanationDetails   map[string]ExplanationDetail `json:"explanation_details,omitempty"`
	Confidence           float64                      `json:"confidence"`
	Source               string                       `json:"source"`
}

type Report struct {
	ID               string          `json:"id"`
	PromptVersion    string          `json:"prompt_version"`
	AlgorithmVersion string          `json:"algorithm_version,omitempty"`
	Profile          ProfileRules    `json:"profile"`
	Holdings         []HoldingResult `json:"holdings"`
	Metrics          Metrics         `json:"metrics"`
	Conclusion       AIReport        `json:"conclusion"`
	GeneratedAt      time.Time       `json:"generated_at"`
	Request          Request         `json:"request,omitempty"`
	Facts            map[string]Fact `json:"facts,omitempty"`
	Model            string          `json:"model,omitempty"`
}

type Job struct {
	ID                    string          `json:"id"`
	Status                string          `json:"status"`
	Stage                 string          `json:"stage"`
	Request               Request         `json:"request"`
	Results               []HoldingResult `json:"results"`
	CompletedStocks       int             `json:"completed_stocks"`
	TotalStocks           int             `json:"total_stocks"`
	CoveragePercent       float64         `json:"coverage_percent"`
	CurrentSymbols        []string        `json:"current_symbols"`
	Message               string          `json:"message"`
	Error                 string          `json:"error,omitempty"`
	StartedAt             time.Time       `json:"started_at,omitempty"`
	UpdatedAt             time.Time       `json:"updated_at,omitempty"`
	CompletedAt           time.Time       `json:"completed_at,omitempty"`
	ReportAvailable       bool            `json:"report_available"`
	Report                *Report         `json:"report,omitempty"`
	ResumedFrom           string          `json:"resumed_from,omitempty"`
	ResumeAvailable       bool            `json:"resume_available,omitempty"`
	ReusedStocks          int             `json:"reused_stocks"`
	NewStocks             int             `json:"new_stocks"`
	SharedStocks          int             `json:"shared_stocks"`
	AggregationStartedAt  time.Time       `json:"aggregation_started_at,omitempty"`
	AggregationDurationMS int64           `json:"aggregation_duration_ms,omitempty"`
}

type EvidenceRef struct {
	ReportID string `json:"report_id,omitempty"`
	SourceID string `json:"source_id,omitempty"`
	Fact     string `json:"fact,omitempty"`
}

// ExplanationDetail preserves evidence from structured model list items while
// keeping the public explanation lists compatible with existing string reports.
// Keys are stable paths such as primary_risks[0] or dimensions.holding_logic.limitations[0].
type ExplanationDetail struct {
	EvidenceRefs []EvidenceRef `json:"evidence_refs,omitempty"`
	Symbols      []string      `json:"symbols,omitempty"`
}

type ScoreAdjustment struct {
	RiskID string `json:"risk_id"`
	Reason string `json:"reason"`
	Points int    `json:"points"`
}

type ScoreDimension struct {
	Key          string            `json:"key"`
	Label        string            `json:"label"`
	Score        *int              `json:"score"`
	Weight       int               `json:"weight"`
	Reason       string            `json:"reason"`
	Adjustments  []ScoreAdjustment `json:"adjustments"`
	EvidenceRefs []EvidenceRef     `json:"evidence_refs"`
	Limitations  []string          `json:"limitations"`
}

type RiskGroup struct {
	Name         string        `json:"name"`
	Symbols      []string      `json:"symbols"`
	Weight       int           `json:"weight_percent"`
	Reason       string        `json:"reason"`
	EvidenceRefs []EvidenceRef `json:"evidence_refs"`
}

type Fact struct {
	Value      any       `json:"value"`
	Available  bool      `json:"available"`
	Method     string    `json:"method"`
	AsOf       time.Time `json:"as_of,omitempty"`
	Limitation string    `json:"limitation,omitempty"`
}

func RulesFor(profile TraderProfile) (ProfileRules, bool) {
	switch profile {
	case ProfileAggressive:
		return ProfileRules{ID: profile, Label: "激进", Description: "短线机会与弹性优先，可接受较高波动", MaxSinglePercent: 45, MaxTopThreePercent: 90, MinimumCashPercent: 0, MaxHighRiskPercent: 75, MaxStopLossRisk: 8, PreferredShortTermMax: 80}, true
	case ProfileBalanced:
		return ProfileRules{ID: profile, Label: "均衡", Description: "兼顾收益与回撤，控制单票和同题材集中", MaxSinglePercent: 35, MaxTopThreePercent: 75, MinimumCashPercent: 5, MaxHighRiskPercent: 55, MaxStopLossRisk: 6, PreferredShortTermMax: 60}, true
	case ProfileSteady:
		return ProfileRules{ID: profile, Label: "稳重", Description: "本金保护与趋势确认优先，保留现金缓冲", MaxSinglePercent: 25, MaxTopThreePercent: 60, MinimumCashPercent: 10, MaxHighRiskPercent: 35, MaxStopLossRisk: 4, PreferredShortTermMax: 35}, true
	default:
		return ProfileRules{}, false
	}
}
