package stockanalysis

import (
	"context"
	"time"

	"easy-stock/backend/internal/foundation"
)

const ResearchPromptVersion = "stock-research-v2"

type ResearchRequest struct {
	Symbol    string   `json:"symbol"`
	Purpose   string   `json:"purpose"`
	Horizon   string   `json:"horizon"`
	CostPrice *float64 `json:"cost_price,omitempty"`
}

// Source text is evidence, not an instruction or a verified interpretation.
type ResearchSource struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Provider    string    `json:"provider"`
	URL         string    `json:"url,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	CapturedAt  time.Time `json:"captured_at"`
	ReportDate  string    `json:"report_date,omitempty"`
	TimeStatus  string    `json:"time_status"`
}

type PriceAnchor struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Price    float64 `json:"price"`
	SourceID string  `json:"source_id"`
	AsOf     string  `json:"as_of"`
}

type ResearchSnapshot struct {
	ID          string           `json:"id"`
	Version     int              `json:"version"`
	Symbol      string           `json:"symbol"`
	Name        string           `json:"name"`
	CapturedAt  time.Time        `json:"captured_at"`
	CutoffAt    time.Time        `json:"cutoff_at"`
	Quote       foundation.Quote `json:"quote"`
	Sources     []ResearchSource `json:"sources"`
	Anchors     []PriceAnchor    `json:"anchors"`
	Limitations []string         `json:"limitations"`
	DailyBars   []AIDailyBar     `json:"daily_bars"`
	Baseline    Scorecard        `json:"rule_baseline"`
}

type ResearchClaim struct {
	Text      string   `json:"text"`
	Kind      string   `json:"kind"`
	SourceIDs []string `json:"source_ids"`
	Quote     string   `json:"quote,omitempty"`
}

type ResearchQuestion struct {
	Question string `json:"question"`
	Why      string `json:"why"`
	Tool     string `json:"tool"`
	Query    string `json:"query"`
	SourceID string `json:"source_id,omitempty"`
	Status   string `json:"status"`
	Outcome  string `json:"outcome,omitempty"`
}

type ResearchOutline struct {
	Questions    []ResearchQuestion `json:"questions"`
	Hypotheses   []ResearchClaim    `json:"hypotheses"`
	MissingFacts []string           `json:"missing_facts"`
}

type ResearchCondition struct {
	ID        string   `json:"id"`
	Text      string   `json:"text"`
	Metric    string   `json:"metric"`
	Operator  string   `json:"operator"`
	AnchorID  string   `json:"anchor_id,omitempty"`
	Threshold *float64 `json:"threshold,omitempty"`
	Window    string   `json:"window"`
	SourceIDs []string `json:"source_ids"`
	Status    string   `json:"status"`
}

type ResearchScenario struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ConditionIDs []string `json:"condition_ids"`
	Response     string   `json:"response"`
}

type AnchoredPricePlan struct {
	EntryAnchor  string   `json:"entry_anchor"`
	StopAnchor   string   `json:"stop_anchor"`
	TargetAnchor string   `json:"target_anchor,omitempty"`
	Reason       string   `json:"reason"`
	SourceIDs    []string `json:"source_ids"`
}

type ResearchDecision struct {
	Status           string             `json:"status"`
	Mode             string             `json:"mode"`
	Horizon          string             `json:"horizon"`
	NewPosition      string             `json:"new_position"`
	ExistingPosition string             `json:"existing_position"`
	Reason           string             `json:"reason"`
	PricePlan        *AnchoredPricePlan `json:"price_plan,omitempty"`
}

type ResearchSynthesis struct {
	Headline         string              `json:"headline"`
	Thesis           ResearchClaim       `json:"thesis"`
	Support          []ResearchClaim     `json:"support"`
	Counter          []ResearchClaim     `json:"counter"`
	Alternatives     []ResearchClaim     `json:"alternatives"`
	MainConflict     string              `json:"main_conflict"`
	EvidenceLevel    string              `json:"evidence_level"`
	Limitations      []string            `json:"limitations"`
	Conditions       []ResearchCondition `json:"conditions"`
	InvalidationIDs  []string            `json:"invalidation_ids"`
	Scenarios        []ResearchScenario  `json:"scenarios"`
	Decision         ResearchDecision    `json:"decision"`
	BaselineRelation string              `json:"baseline_relation"`
	BaselineReason   string              `json:"baseline_reason"`
}

type ResearchAttempt struct {
	Stage         string `json:"stage"`
	DurationMS    int64  `json:"duration_ms"`
	PromptBytes   int    `json:"prompt_bytes"`
	ResponseBytes int    `json:"response_bytes"`
	Error         string `json:"error,omitempty"`
}

type ResearchPromptCompression struct {
	Version              string `json:"version"`
	OriginalSourceCount  int    `json:"original_source_count"`
	SelectedSourceCount  int    `json:"selected_source_count"`
	OriginalContentBytes int    `json:"original_content_bytes"`
	SelectedContentBytes int    `json:"selected_content_bytes"`
}

type ResearchReport struct {
	ResearchSynthesis
	SnapshotID      string                    `json:"snapshot_id"`
	SnapshotVersion int                       `json:"snapshot_version"`
	PromptVersion   string                    `json:"prompt_version"`
	Request         ResearchRequest           `json:"request"`
	Model           string                    `json:"model"`
	GeneratedAt     time.Time                 `json:"generated_at"`
	CutoffAt        time.Time                 `json:"cutoff_at"`
	Sources         []ResearchSource          `json:"sources"`
	Anchors         []PriceAnchor             `json:"anchors"`
	Questions       []ResearchQuestion        `json:"questions"`
	Attempts        []ResearchAttempt         `json:"attempts"`
	Compression     ResearchPromptCompression `json:"compression"`
	Validation      string                    `json:"validation"`
	ValidationNotes []string                  `json:"validation_notes"`
}

type SupplementFunc func(context.Context, ResearchSnapshot, ResearchQuestion) ([]ResearchSource, error)

type ConditionCheck struct {
	ConditionID string   `json:"condition_id"`
	Status      string   `json:"status"`
	Observed    *float64 `json:"observed,omitempty"`
	AsOf        string   `json:"as_of,omitempty"`
	Detail      string   `json:"detail"`
}

type ResearchVerification struct {
	CheckedAt  time.Time        `json:"checked_at"`
	BaselineAt time.Time        `json:"baseline_at"`
	Source     string           `json:"source"`
	Checks     []ConditionCheck `json:"checks"`
	Summary    string           `json:"summary"`
}
