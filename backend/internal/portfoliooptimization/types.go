package portfoliooptimization

import (
	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/foundation"
	pi "easy-stock/backend/internal/portfolioinspection"
	"time"
)

const Version = "portfolio-optimization-v23"
const TargetPortfolioScore = 70
const MinimumPortfolioScore = 65
const MinimumFallbackImprovement = 5
const MinimumFallbackDimension = 50
const MaxRevisionRounds = 1
const MaxQualityPlans = 3
const MaxCandidates = 8
const MaxCandidateResearch = 6
const MaxScreeningStocks = 80
const MaxScreeningIndustries = 16
const ScreeningTimeout = 3 * time.Minute
const TotalTimeout = 24 * time.Minute
const ModelTimeout = 8 * time.Minute

type Request struct {
	RestartFrom      string   `json:"restart_from,omitempty"`
	CandidateSymbols []string `json:"candidate_symbols,omitempty"`
}
type Candidate struct {
	Symbol               string              `json:"symbol"`
	Name                 string              `json:"name"`
	Source               string              `json:"source"`
	Industry             string              `json:"industry,omitempty"`
	IndustryGroup        string              `json:"industry_group,omitempty"`
	CatalogIndustryGroup string              `json:"catalog_industry_group,omitempty"`
	Reason               string              `json:"reason"`
	Selected             bool                `json:"selected"`
	Screening            *CandidateScreening `json:"screening,omitempty"`
	FitBonus             int                 `json:"portfolio_fit_bonus,omitempty"`
	FitReason            string              `json:"portfolio_fit_reason,omitempty"`
}
type Eligibility struct {
	QuoteTradeDate     string   `json:"quote_trade_date,omitempty"`
	LiquiditySource    string   `json:"liquidity_source,omitempty"`
	LiquidityTradeDate string   `json:"liquidity_trade_date,omitempty"`
	MissingFields      []string `json:"missing_fields,omitempty"`
	Symbol             string   `json:"symbol"`
	CanIncrease        bool     `json:"can_increase"`
	Locked             bool     `json:"locked"`
	Conditional        bool     `json:"conditional"`
	Reason             string   `json:"reason"`
	TradeDate          string   `json:"trade_date"`
	Volume             float64  `json:"volume"`
	Amount             float64  `json:"amount"`
}
type Universe struct {
	ScreeningAudit *ScreeningAudit
	Candidates     []Candidate
	Catalog        []foundation.StockCatalogEntry
	Limitations    []string
	Quotes         []foundation.Quote
}

// The bounded audit is persisted for explanation, never sent to the model.
type ScreeningAudit struct {
	Checked    int               `json:"checked"`
	Qualified  int               `json:"qualified"`
	Retained   int               `json:"retained"`
	Diverse    int               `json:"diverse"`
	CheckLimit int               `json:"check_limit"`
	BudgetMS   int64             `json:"budget_ms"`
	DurationMS int64             `json:"duration_ms"`
	StopReason string            `json:"stop_reason"`
	Records    []ScreeningRecord `json:"records,omitempty"`
}
type ScreeningRecord struct {
	Symbol    string `json:"symbol"`
	Name      string `json:"name"`
	Industry  string `json:"industry"`
	Qualified bool   `json:"qualified"`
	Reason    string `json:"reason"`
}
type Allocation struct {
	Symbol            string                `json:"symbol"`
	Minimum           int                   `json:"min_weight"`
	Maximum           int                   `json:"max_weight"`
	Preferred         int                   `json:"preferred_weight"`
	Reason            string                `json:"reason"`
	Funding           string                `json:"funding_reason"`
	Suitable          bool                  `json:"suitable_for_increase"`
	SuitabilityReason string                `json:"suitability_reason"`
	ConfirmationIDs   []string              `json:"confirmation_ids"`
	InvalidationIDs   []string              `json:"invalidation_ids"`
	EvidenceRefs      []pi.EvidenceRef      `json:"evidence_refs"`
	Investment        *InvestmentJudgment   `json:"investment,omitempty"`
	Conditions        []AllocationCondition `json:"allocation_conditions,omitempty"`
}

type InvestmentJudgment struct {
	Role              string `json:"role"`
	Action            string `json:"action"`
	Horizon           string `json:"horizon"`
	Business          string `json:"business"`
	Growth            string `json:"growth"`
	Valuation         string `json:"valuation"`
	Timing            string `json:"timing"`
	PortfolioFit      string `json:"portfolio_fit"`
	Risk              string `json:"risk"`
	Exit              string `json:"exit"`
	OpportunityCost   string `json:"opportunity_cost"`
	PriorOpinion      string `json:"prior_opinion"`
	PeriodSuitability string `json:"period_suitability"`
}

// New portfolio conditions are research instructions, never claims of execution.
// Numeric prices must reference an actual frozen research anchor.
type AllocationCondition struct {
	Kind         string           `json:"kind"`
	Text         string           `json:"text"`
	Verification string           `json:"verification"`
	Status       string           `json:"status"`
	AnchorID     string           `json:"anchor_id,omitempty"`
	Operator     string           `json:"operator,omitempty"`
	Threshold    *float64         `json:"threshold,omitempty"`
	EvidenceRefs []pi.EvidenceRef `json:"evidence_refs"`
}

type InvestmentComparison struct {
	FromSymbol   string           `json:"from_symbol"`
	ToSymbol     string           `json:"to_symbol"`
	Dimension    string           `json:"dimension"`
	Reason       string           `json:"reason"`
	Tradeoff     string           `json:"tradeoff"`
	EvidenceRefs []pi.EvidenceRef `json:"evidence_refs"`
}

// The reviewer compares A/B independently, without optimizer explanations.
type ReviewedInvestmentComparison struct {
	PreferredSymbol string           `json:"preferred_symbol"`
	OtherSymbol     string           `json:"other_symbol"`
	Dimension       string           `json:"dimension"`
	Reason          string           `json:"reason"`
	Tradeoff        string           `json:"tradeoff"`
	EvidenceRefs    []pi.EvidenceRef `json:"evidence_refs"`
}
type Alternative struct {
	Name        string       `json:"name"`
	Allocations []Allocation `json:"allocations"`
}
type Proposal struct {
	IssueDetails          []IssueDetail          `json:"issue_details,omitempty"`
	RangeAdjustments      []rangeAdjustment      `json:"range_adjustments,omitempty"`
	RangeBoundCorrections []string               `json:"range_bound_corrections,omitempty"`
	Issues                []string               `json:"issues"`
	RiskGroups            []pi.RiskGroup         `json:"risk_groups"`
	Alternatives          []Alternative          `json:"alternatives"`
	KeepReason            string                 `json:"keep_reason"`
	InvestmentComparisons []InvestmentComparison `json:"investment_comparisons,omitempty"`
}
type Checks struct {
	Valid       bool     `json:"valid"`
	Total       int      `json:"total_position_percent"`
	Cash        int      `json:"cash_percent"`
	Sold        int      `json:"sold_percent"`
	Bought      int      `json:"bought_percent"`
	Retained    int      `json:"retained_percent"`
	Replacement float64  `json:"replacement_ratio_percent"`
	Maximum     int      `json:"maximum_replacement_ratio_percent"`
	Mode        string   `json:"change_budget_mode"`
	Errors      []string `json:"errors"`
}
type Improvement struct {
	Issue        string           `json:"issue"`
	Metric       string           `json:"metric"`
	Before       float64          `json:"before"`
	After        float64          `json:"after"`
	Kind         string           `json:"kind,omitempty"`
	FromSymbol   string           `json:"from_symbol,omitempty"`
	ToSymbol     string           `json:"to_symbol,omitempty"`
	Weight       int              `json:"weight_percent,omitempty"`
	Reason       string           `json:"reason,omitempty"`
	Tradeoff     string           `json:"tradeoff,omitempty"`
	EvidenceRefs []pi.EvidenceRef `json:"evidence_refs,omitempty"`
}
type Assessment struct {
	Preferred             string                         `json:"preferred_configuration,omitempty"`
	Accepted              bool                           `json:"accepted"`
	Reason                string                         `json:"reason"`
	Tradeoffs             []string                       `json:"tradeoffs"`
	ResidualRisks         []string                       `json:"residual_risks"`
	Issue                 string                         `json:"original_issue"`
	EvidenceRefs          []pi.EvidenceRef               `json:"evidence_refs"`
	InvestmentComparisons []ReviewedInvestmentComparison `json:"investment_comparisons,omitempty"`
}
type FundingTransfer struct {
	FromSymbol string `json:"from_symbol"`
	ToSymbol   string `json:"to_symbol"`
	Weight     int    `json:"weight_percent"`
}
type Plan struct {
	ReviewCheckpoint *ReviewCheckpoint `json:"review_checkpoint,omitempty"`
	RejectionReasons []string          `json:"rejection_reasons,omitempty"`
	RiskChecks       []RiskCheck       `json:"risk_checks,omitempty"`
	Search           *AllocationSearch `json:"allocation_search,omitempty"`
	TradeSold        int               `json:"trade_sold_percent"`
	TradeBought      int               `json:"trade_bought_percent"`
	Funding          []FundingTransfer `json:"funding"`
	Name             string            `json:"name"`
	Status           string            `json:"status"`
	Target           []pi.Holding      `json:"target"`
	Checks           Checks            `json:"checks"`
	Allocations      []Allocation      `json:"allocations"`
	Improvements     []Improvement     `json:"improvements"`
	Original         pi.Report         `json:"original_comparison"`
	Proposed         pi.Report         `json:"target_comparison"`
	Assessment       *Assessment       `json:"assessment,omitempty"`
	AssessmentOrder  string            `json:"assessment_order"`
	Error            string            `json:"error,omitempty"`
}

// A narrative grouping remains visible to the reviewer; only measured strong
// linkage can turn its combined weight into a hard concentration constraint.
type RiskCheck struct {
	Name    string   `json:"name"`
	Symbols []string `json:"symbols,omitempty"`
	Before  int      `json:"before_percent"`
	After   int      `json:"after_percent"`
	Limit   int      `json:"limit_percent,omitempty"`
	Hard    bool     `json:"hard"`
	Passed  bool     `json:"passed"`
	Basis   string   `json:"basis"`
}
type Job struct {
	ExecutionDurationMS int64 `json:"execution_duration_ms"`
	executionTick       time.Time
	ModelLoops          map[string]*ModelLoopState `json:"model_loops,omitempty"`
	ProposalCheckpoint  *ProposalCheckpoint        `json:"proposal_checkpoint,omitempty"`
	CheckpointProgress  *CheckpointProgress        `json:"checkpoint_progress,omitempty"`

	RangeRepairUsed      bool                 `json:"range_repair_used,omitempty"`
	FallbackPlan         *Plan                `json:"fallback_plan,omitempty"`
	InvestmentBaseline   *Proposal            `json:"investment_baseline,omitempty"`
	ID                   string               `json:"id"`
	SourceID             string               `json:"source_id"`
	RootSourceID         string               `json:"root_source_id"`
	Baseline             []pi.Holding         `json:"baseline"`
	BaselineFingerprint  string               `json:"baseline_fingerprint"`
	Fingerprint          string               `json:"fingerprint"`
	Version              string               `json:"version"`
	Source               pi.Report            `json:"source_report"`
	Request              Request              `json:"request"`
	Status               string               `json:"status"`
	Stage                string               `json:"stage"`
	Message              string               `json:"message"`
	Error                string               `json:"error,omitempty"`
	ResumeAvailable      bool                 `json:"resume_available"`
	StartedAt            time.Time            `json:"started_at"`
	UpdatedAt            time.Time            `json:"updated_at"`
	CompletedAt          time.Time            `json:"completed_at,omitempty"`
	AsOf                 time.Time            `json:"as_of"`
	UnionFacts           map[string]pi.Fact   `json:"union_facts,omitempty"`
	SnapshotAt           time.Time            `json:"snapshot_at,omitempty"`
	LatestTradeDate      string               `json:"latest_trade_date,omitempty"`
	Candidates           []Candidate          `json:"candidates"`
	ScreeningAudit       *ScreeningAudit      `json:"screening_audit,omitempty"`
	Eligibility          []Eligibility        `json:"eligibility"`
	Results              []pi.HoldingResult   `json:"results"`
	Proposal             *Proposal            `json:"proposal,omitempty"`
	Plans                []Plan               `json:"plans"`
	SelectedPlan         *int                 `json:"selected_plan,omitempty"`
	RevisionCount        int                  `json:"revision_count"`
	RevisionHistory      []RevisionRound      `json:"revision_history,omitempty"`
	Outcome              string               `json:"outcome,omitempty"`
	OutcomeReason        string               `json:"outcome_reason,omitempty"`
	Limitations          []string             `json:"limitations"`
	Model                string               `json:"model,omitempty"`
	ModelStartedAt       time.Time            `json:"model_started_at,omitempty"`
	ModelDurationMS      int64                `json:"model_duration_ms"`
	ModelStageDurationMS map[string]int64     `json:"model_stage_duration_ms,omitempty"`
	ModelPromptVersion   string               `json:"model_prompt_version,omitempty"`
	ModelPromptBytes     int                  `json:"model_prompt_bytes,omitempty"`
	ModelProgress        agent.PromptProgress `json:"model_progress,omitempty"`
	ModelAttempts        []ModelAttempt       `json:"model_attempts,omitempty"`
	ReusedStocks         int                  `json:"reused_stocks"`
	NewStocks            int                  `json:"new_stocks"`
	Needs                []string             `json:"portfolio_needs,omitempty"`
}

// Rejected rounds keep their actual independent judgments without duplicating
// the frozen research/fact dossier. They are never selectable or applicable.
type RevisionRound struct {
	Round        int               `json:"round"`
	Name         string            `json:"name"`
	Target       []pi.Holding      `json:"target"`
	Conclusion   pi.AIReport       `json:"conclusion"`
	Assessment   *Assessment       `json:"assessment"`
	Checks       Checks            `json:"checks"`
	Funding      []FundingTransfer `json:"funding"`
	Improvements []Improvement     `json:"improvements"`
	RiskGroups   []pi.RiskGroup    `json:"risk_groups"`
	Error        string            `json:"error"`
}

type ModelAttempt struct {
	Round         int                  `json:"round,omitempty"`
	BudgetMS      int64                `json:"budget_ms"`
	PromptVersion string               `json:"prompt_version,omitempty"`
	Stage         string               `json:"stage"`
	DurationMS    int64                `json:"duration_ms"`
	PromptBytes   int                  `json:"prompt_bytes"`
	ResponseBytes int                  `json:"response_bytes"`
	Progress      agent.PromptProgress `json:"progress"`
	Error         string               `json:"error,omitempty"`
}
