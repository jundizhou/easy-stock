package portfoliooptimization

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const MaxOperationRepairs = 4
const MaxRepeatedFailure = 3
const MaxSavedResponseBytes = 128 * 1024

// Bounded response journal reconstructs validator state after a process restart,
// including partial successes and numeric repairs, without another model call.
// The scope includes the frozen prompt, stage and substantive revision.
type ModelFlight struct {
	StartedAt   time.Time `json:"started_at"`
	BudgetMS    int64     `json:"budget_ms"`
	AccountedMS int64     `json:"accounted_ms"`
	BudgetKey   string    `json:"budget_key"`
}
type ModelLoopState struct {
	InFlight         *ModelFlight `json:"in_flight,omitempty"`
	RepairKind       string       `json:"repair_kind,omitempty"`
	ProcessedOutputs int          `json:"processed_outputs"`
	Outputs          []string     `json:"outputs,omitempty"`
	NextPrompt       string       `json:"next_prompt,omitempty"`
	ScopePrompt      string       `json:"scope_prompt,omitempty"`
	RepairCalls      int          `json:"repair_calls"`
	TransportRetries int          `json:"transport_retries"`
	LastError        string       `json:"last_error,omitempty"`
	RepeatedFailure  int          `json:"repeated_failure"`
	Completed        bool         `json:"completed"`
	Stopped          string       `json:"stopped,omitempty"`
}
type ProposalCheckpoint struct {
	Draft   Proposal       `json:"draft"`
	Pending []proposalPart `json:"pending,omitempty"`
}
type CheckpointProgress struct {
	SavedStocks            int `json:"saved_stocks"`
	TotalStocks            int `json:"total_stocks"`
	PendingParts           int `json:"pending_parts"`
	SavedReviewBlocks      int `json:"saved_review_blocks"`
	ReviewedConfigurations int `json:"reviewed_configurations"`
}

func updateCheckpointProgress(job *Job) {
	p := &CheckpointProgress{TotalStocks: len(job.Results)}
	if job.Proposal != nil {
		p.SavedStocks = len(job.Results)
	} else if cp := job.ProposalCheckpoint; cp != nil {
		failed := map[string]bool{}
		for _, part := range cp.Pending {
			if part.kind == "allocation" {
				failed[part.symbol] = true
			}
		}
		if len(cp.Draft.Alternatives) > 0 {
			for _, a := range cp.Draft.Alternatives[0].Allocations {
				if !failed[a.Symbol] {
					p.SavedStocks++
				}
			}
		}
		p.PendingParts = len(cp.Pending)
	}
	for _, plan := range job.Plans {
		if cp := plan.ReviewCheckpoint; cp != nil {
			p.SavedReviewBlocks += len(cp.Blocks)
			p.PendingParts += len(cp.Pending)
		}
		if plan.Assessment != nil {
			p.ReviewedConfigurations++
		}
	}
	for _, r := range job.RevisionHistory {
		if r.Assessment != nil {
			p.ReviewedConfigurations++
		}
	}
	job.CheckpointProgress = p
}

func nextRepairPrompt(ctx context.Context, job *Job, state *ModelLoopState, original, output string, err error) (string, error) {
	var parts *proposalPartsError
	if errors.As(err, &parts) {
		job.ProposalCheckpoint = &ProposalCheckpoint{Draft: parts.proposal, Pending: parts.parts}
		job.Message = fmt.Sprintf("已保存通过的股票判断，正在补齐剩余%d项", len(parts.parts))
		state.RepairKind = "parts"
		state.ScopePrompt = parts.prompt(original)
		return state.ScopePrompt, nil
	}
	var review *reviewPartsError
	if errors.As(err, &review) {
		job.Message = fmt.Sprintf("已保存通过的评分与比较，正在补齐剩余%d项", len(review.state.Pending))
		state.RepairKind = "review_parts"
		state.ScopePrompt = review.prompt(original)
		return state.ScopePrompt, nil
	}
	var ranges *programRangeRepairError
	if job.Stage == "proposing" && job.RevisionCount == 0 && errors.As(err, &ranges) {
		job.ProposalCheckpoint = &ProposalCheckpoint{Draft: ranges.proposal}
		prompt, e := feasibilityRepairPrompt(ctx, *job, ranges.proposal, ranges.cause)
		if e != nil {
			return "", e
		}
		state.RepairKind = "ranges"
		state.ScopePrompt = prompt
		job.Message = "投资判断已保存，正在修复可行仓位范围"
		return prompt, nil
	}
	var evidence *comparisonEvidenceError
	var weight *preferredWeightTotalError
	if errors.As(err, &evidence) || errors.As(err, &weight) {
		state.ScopePrompt = modelRepairPrompt(original, output, err)
		return state.ScopePrompt, nil
	}
	base := original
	if state.ScopePrompt != "" {
		base = state.ScopePrompt
	}
	return modelRepairPrompt(base, output, err), nil
}

func remainingExecution(job Job) time.Duration {
	remaining := TotalTimeout - time.Duration(job.ExecutionDurationMS)*time.Millisecond
	if !job.executionTick.IsZero() {
		remaining -= time.Since(job.executionTick)
	}
	return remaining
}
func checkpointCanResume(job Job) bool {
	if remainingExecution(job) <= 0 {
		return false
	}
	if time.Duration(job.ModelStageDurationMS[modelBudgetKey(job)])*time.Millisecond >= ModelTimeout {
		return false
	}
	for _, loop := range job.ModelLoops {
		if loop.Stopped != "" {
			return false
		}
	}
	return true
}

// After an unclean exit, an issued request may have run until its deadline.
// Charge its unaccounted time conservatively, capped by the issued budget.
// Normal cancellation clears this lease, so idle/paused time is never charged.
func settleInterruptedUsage(job *Job, now time.Time) {
	if job.ModelStageDurationMS == nil {
		job.ModelStageDurationMS = map[string]int64{}
	}
	for _, state := range job.ModelLoops {
		flight := state.InFlight
		if flight == nil {
			continue
		}
		elapsed := min(flight.BudgetMS, max(int64(0), now.Sub(flight.StartedAt).Milliseconds()))
		delta := max(int64(0), elapsed-flight.AccountedMS)
		job.ModelDurationMS += delta
		job.ModelStageDurationMS[flight.BudgetKey] += delta
		job.ExecutionDurationMS += delta
		state.InFlight = nil
	}
}
