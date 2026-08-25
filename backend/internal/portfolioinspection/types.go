package portfolioinspection

import (
	"time"

	"easy-stock/backend/internal/stockanalysis"
)

const (
	MaxHoldings        = 10
	PromptVersion      = "portfolio-inspection-v1"
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
	TraderProfile TraderProfile `json:"trader_profile"`
	Holdings      []Holding     `json:"holdings"`
}

type HoldingResult struct {
	Holding     Holding                 `json:"holding"`
	Status      string                  `json:"status"`
	Error       string                  `json:"error,omitempty"`
	CompletedAt time.Time               `json:"completed_at,omitempty"`
	Analysis    *stockanalysis.Analysis `json:"analysis,omitempty"`
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
	TotalPositionPercent int                `json:"total_position_percent"`
	CashPercent          int                `json:"cash_percent"`
	CoveragePercent      float64            `json:"coverage_percent"`
	MaxSinglePercent     int                `json:"max_single_percent"`
	TopThreePercent      int                `json:"top_three_percent"`
	HHI                  float64            `json:"concentration_hhi"`
	WeightedScore        float64            `json:"weighted_stock_score"`
	WeightedRisk         float64            `json:"weighted_risk_score"`
	StopLossRiskPercent  float64            `json:"stop_loss_risk_percent"`
	ShortTermPercent     int                `json:"short_term_percent"`
	NewListingPercent    int                `json:"new_listing_percent"`
	HighRiskPercent      int                `json:"high_risk_percent"`
	StyleMatchScore      int                `json:"style_match_score"`
	StyleBreaches        []string           `json:"style_breaches"`
	ThemeExposures       []ThemeExposure    `json:"theme_exposures"`
	HighCorrelations     []CorrelationPair  `json:"high_correlations"`
	RiskContributions    []RiskContribution `json:"risk_contributions"`
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
	HealthScore          int                 `json:"health_score"`
	RiskLevel            string              `json:"risk_level"`
	StyleMatch           string              `json:"style_match"`
	ExecutiveSummary     string              `json:"executive_summary"`
	PrimaryRisks         []string            `json:"primary_risks"`
	ConcentrationFinding []string            `json:"concentration_findings"`
	Holdings             []HoldingConclusion `json:"holdings"`
	AdjustmentOrder      []string            `json:"adjustment_order"`
	Scenarios            []Scenario          `json:"scenarios"`
	NextChecklist        []string            `json:"next_checklist"`
	DataLimitations      []string            `json:"data_limitations"`
	Confidence           float64             `json:"confidence"`
	Source               string              `json:"source"`
}

type Report struct {
	ID            string          `json:"id"`
	PromptVersion string          `json:"prompt_version"`
	Profile       ProfileRules    `json:"profile"`
	Holdings      []HoldingResult `json:"holdings"`
	Metrics       Metrics         `json:"metrics"`
	Conclusion    AIReport        `json:"conclusion"`
	GeneratedAt   time.Time       `json:"generated_at"`
}

type Job struct {
	ID              string          `json:"id"`
	Status          string          `json:"status"`
	Stage           string          `json:"stage"`
	Request         Request         `json:"request"`
	Results         []HoldingResult `json:"results"`
	CompletedStocks int             `json:"completed_stocks"`
	TotalStocks     int             `json:"total_stocks"`
	CoveragePercent float64         `json:"coverage_percent"`
	CurrentSymbols  []string        `json:"current_symbols"`
	Message         string          `json:"message"`
	Error           string          `json:"error,omitempty"`
	StartedAt       time.Time       `json:"started_at,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at,omitempty"`
	CompletedAt     time.Time       `json:"completed_at,omitempty"`
	ReportAvailable bool            `json:"report_available"`
	Report          *Report         `json:"report,omitempty"`
}

func RulesFor(profile TraderProfile) (ProfileRules, bool) {
	switch profile {
	case ProfileAggressive:
		return ProfileRules{ID: profile, Label: "激进", Description: "短线机会与弹性优先，可接受较高波动", MaxSinglePercent: 35, MaxTopThreePercent: 80, MinimumCashPercent: 0, MaxHighRiskPercent: 65, MaxStopLossRisk: 6, PreferredShortTermMax: 70}, true
	case ProfileBalanced:
		return ProfileRules{ID: profile, Label: "均衡", Description: "兼顾收益与回撤，控制单票和同题材集中", MaxSinglePercent: 25, MaxTopThreePercent: 65, MinimumCashPercent: 10, MaxHighRiskPercent: 40, MaxStopLossRisk: 4, PreferredShortTermMax: 45}, true
	case ProfileSteady:
		return ProfileRules{ID: profile, Label: "稳重", Description: "本金保护与趋势确认优先，保留现金缓冲", MaxSinglePercent: 15, MaxTopThreePercent: 45, MinimumCashPercent: 20, MaxHighRiskPercent: 20, MaxStopLossRisk: 2.5, PreferredShortTermMax: 20}, true
	default:
		return ProfileRules{}, false
	}
}
