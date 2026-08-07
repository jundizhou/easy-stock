package inflection

import "time"

type Scope string

const (
	ScopeMarket Scope = "market"
	ScopeSector Scope = "sector"
	ScopeStock  Scope = "stock"
)

type AnchorKind string

const (
	AnchorOldProfit   AnchorKind = "old_profit"
	AnchorOldNegative AnchorKind = "old_negative"
	AnchorNewCarrier  AnchorKind = "new_carrier"
)

type InflectionType string

const (
	InflectionNone  InflectionType = "none"
	InflectionSmall InflectionType = "small"
	InflectionBig   InflectionType = "big"
)

type SignalStatus string

const (
	StatusNone      SignalStatus = "none"
	StatusCandidate SignalStatus = "candidate"
	StatusConfirmed SignalStatus = "confirmed"
)

type SmallSetup string

const (
	SmallSetupNone               SmallSetup = "none"
	SmallSetupHighLowSwitch      SmallSetup = "high_low_switch"
	SmallSetupIndividualReversal SmallSetup = "individual_reversal"
)

// RecognitionFeatures are normalized 0-100 inputs. They deliberately mirror
// the meeting's qualitative definition of recognition: height, attention,
// persistence, influence, and resilience. Upstream collectors are responsible
// for converting their raw market data into comparable percentiles.
type RecognitionFeatures struct {
	Height      float64 `json:"height"`
	Attention   float64 `json:"attention"`
	Persistence float64 `json:"persistence"`
	Influence   float64 `json:"influence"`
	Resilience  float64 `json:"resilience"`
}

// CandidateSnapshot describes either an old consensus anchor or a possible new
// carrier at one point in time. An anchor is an observation reference and does
// not imply that the security itself is a trade target.
type CandidateSnapshot struct {
	Symbol    string     `json:"symbol"`
	Name      string     `json:"name"`
	Kind      AnchorKind `json:"kind"`
	Scope     Scope      `json:"scope"`
	ScopeName string     `json:"scope_name,omitempty"`

	Recognition RecognitionFeatures `json:"recognition"`

	ChangePercent       float64 `json:"change_percent"`
	ScopeChangePercent  float64 `json:"scope_change_percent"`
	MarketChangePercent float64 `json:"market_change_percent"`
	VWAPDistancePercent float64 `json:"vwap_distance_percent"`
	ExpectationGap      float64 `json:"expectation_gap_percent"`
	AmountRatio         float64 `json:"amount_ratio"`
	SectorFollowers     int     `json:"sector_followers"`

	LimitUp              bool    `json:"limit_up"`
	LimitDown            bool    `json:"limit_down"`
	BoardBroken          bool    `json:"board_broken"`
	LimitDownOpened      bool    `json:"limit_down_opened"`
	ConsecutiveLimitDown int     `json:"consecutive_limit_down"`
	Drawdown3DPercent    float64 `json:"drawdown_3d_percent"`
	PreviousPassiveWeak  bool    `json:"previous_passive_weak"`
}

// MarketSnapshot is the environment anchor. BreadthImprovement is expressed in
// percentage points and LimitDownReleaseRatio is a 0-1 fraction.
type MarketSnapshot struct {
	Time      time.Time `json:"time,omitempty"`
	Scope     Scope     `json:"scope"`
	ScopeName string    `json:"scope_name,omitempty"`

	MarketChangePercent   float64 `json:"market_change_percent"`
	ScopeChangePercent    float64 `json:"scope_change_percent"`
	Advancers             int     `json:"advancers"`
	Decliners             int     `json:"decliners"`
	LimitUps              int     `json:"limit_ups"`
	LimitDowns            int     `json:"limit_downs"`
	BrokenBoards          int     `json:"broken_boards"`
	PreviousLimitPremium  float64 `json:"previous_limit_up_premium"`
	StressDays            int     `json:"stress_days"`
	PriorStressScore      float64 `json:"prior_stress_score"`
	BreadthImprovement    float64 `json:"breadth_improvement"`
	IndexAboveVWAP        bool    `json:"index_above_vwap"`
	LimitDownReleaseRatio float64 `json:"limit_down_release_ratio"`
}

type EvaluationRequest struct {
	Market     MarketSnapshot      `json:"market"`
	Candidates []CandidateSnapshot `json:"candidates"`
}

type FactorScore struct {
	Name   string  `json:"name"`
	Score  float64 `json:"score"`
	Weight float64 `json:"weight"`
	Detail string  `json:"detail,omitempty"`
}

type CandidateEvaluation struct {
	Candidate           CandidateSnapshot `json:"candidate"`
	RecognitionScore    float64           `json:"recognition_score"`
	ActiveWeaknessScore float64           `json:"active_weakness_score"`
	ActiveStrengthScore float64           `json:"active_strength_score"`
	ClearanceScore      float64           `json:"clearance_score"`
	Evidence            []string          `json:"evidence,omitempty"`
}

type AnchorSelection struct {
	Kind     AnchorKind           `json:"kind"`
	Selected *CandidateEvaluation `json:"selected,omitempty"`
	RunnerUp *CandidateEvaluation `json:"runner_up,omitempty"`
	Clear    bool                 `json:"clear"`
	ScoreGap float64              `json:"score_gap"`
	Evidence []string             `json:"evidence,omitempty"`
}

type SignalEvaluation struct {
	Type             InflectionType `json:"type"`
	Status           SignalStatus   `json:"status"`
	Setup            SmallSetup     `json:"setup,omitempty"`
	Scope            Scope          `json:"scope"`
	Score            float64        `json:"score"`
	OldAnchorSymbol  string         `json:"old_anchor_symbol,omitempty"`
	NewCarrierSymbol string         `json:"new_carrier_symbol,omitempty"`
	Factors          []FactorScore  `json:"factors"`
	Evidence         []string       `json:"evidence,omitempty"`
	Risks            []string       `json:"risks,omitempty"`
}

type Evaluation struct {
	EvaluatedAt          time.Time             `json:"evaluated_at"`
	MarketStressScore    float64               `json:"market_stress_score"`
	EnvironmentTurnScore float64               `json:"environment_turn_score"`
	Candidates           []CandidateEvaluation `json:"candidates"`
	Anchors              []AnchorSelection     `json:"anchors"`
	Big                  SignalEvaluation      `json:"big"`
	Small                SignalEvaluation      `json:"small"`
	PrimarySignal        InflectionType        `json:"primary_signal"`
	Warnings             []string              `json:"warnings,omitempty"`
}
