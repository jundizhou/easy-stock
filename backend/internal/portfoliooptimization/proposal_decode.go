package portfoliooptimization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The current model contract decides investments and their acceptable ranges.
// The program derives the redundant permissions from those actions and solves
// exact weights. Historical, fully named proposals keep their strict validator.
func decodeInitialProposal(ctx context.Context, job Job, content string) (Proposal, error) {
	var p Proposal
	if err := jsonContent(content, &p); err != nil {
		return p, err
	}
	var raw struct {
		WeightMode   string `json:"weight_mode"`
		Alternatives []struct {
			Allocations []map[string]json.RawMessage `json:"allocations"`
		} `json:"alternatives"`
	}
	if err := jsonContent(content, &raw); err != nil {
		return p, err
	}
	if raw.WeightMode == "" {
		return p, nil
	}
	if raw.WeightMode != "program" {
		return p, errors.New("未知配仓模式")
	}
	for i := range p.Alternatives {
		for k, a := range p.Alternatives[i].Allocations {
			fields := raw.Alternatives[i].Allocations[k]
			if fields["preferred_weight"] != nil || fields["suitable_for_increase"] != nil || fields["suitability_reason"] != nil {
				return p, fmt.Errorf("%s程序配仓模式不能重复输出参考权重或资金权限；仅由action及原约束推导", a.Symbol)
			}
		}
	}
	return solveProgramProposal(ctx, job, p)
}

func solveProgramProposal(ctx context.Context, job Job, p Proposal) (Proposal, error) {
	for i := range p.Alternatives {
		for k := range p.Alternatives[i].Allocations {
			deriveAllocation(job, &p.Alternatives[i].Allocations[k])
		}
	}

	// Invalid investment content is rejected. A missing comparison citation can
	// be repaired after computing reference weights: this prevents the sole
	// citation-only repair from encountering a second, artificial total error.
	// These provisional weights never authorize or publish a portfolio.
	if err := validateProposal(job, p); err != nil {
		if repair := collectProposalParts(job, p); repair != nil {
			return p, repair
		}
		var rangeErr *allocationRangeError
		if errors.As(err, &rangeErr) {
			return p, &programRangeRepairError{proposal: p, cause: err}
		}
		var totalErr *preferredWeightTotalError
		var citationErr *comparisonEvidenceError
		if !errors.As(err, &totalErr) && !errors.As(err, &citationErr) {
			return p, err
		}
	}
	searchJob := job
	searchJob.Proposal = &p
	for i := range p.Alternatives {
		solutions, err := SearchAllocations(ctx, searchJob, p.Alternatives[i])
		if err != nil {
			if ctx.Err() != nil {
				return p, ctx.Err()
			}
			// A citation-only failure cannot freeze an unvalidated investment.
			validationErr := validateProposal(job, p)
			var totalErr *preferredWeightTotalError
			if validationErr != nil && !errors.As(validationErr, &totalErr) {
				return p, validationErr
			}
			return p, &programRangeRepairError{proposal: p, cause: err}
		}
		selected, _, _ := weights(solutions[0].Target, false)
		for k := range p.Alternatives[i].Allocations {
			a := &p.Alternatives[i].Allocations[k]
			a.Preferred = selected[a.Symbol]
		}
	}
	return p, validateProposal(job, p)
}

func deriveAllocation(job Job, a *Allocation) {
	original, _, _ := weights(job.Source.Request.Holdings, false)
	if a.Investment != nil {
		a.Suitable = in(a.Investment.Action, "allocate", "wait") && a.Maximum > 0
		a.SuitabilityReason = strings.TrimSpace(a.Investment.PortfolioFit + "；" + a.Investment.Timing)
	}
	a.Preferred = max(a.Minimum, min(a.Maximum, original[a.Symbol]))
}
