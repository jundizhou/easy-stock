package stockanalysis

import (
	"context"
	"strings"
	"time"
)

const ResearchCollectionTimeout = 2 * time.Minute

// ResearchBudget is frozen with the model configuration at submission. The
// execution clock starts after a worker is acquired, never while queued.
type ResearchBudget struct {
	ResponseTimeoutSeconds int    `json:"response_timeout_seconds"`
	StageTimeoutSeconds    int    `json:"stage_timeout_seconds"`
	TotalTimeoutSeconds    int    `json:"total_timeout_seconds"`
	ReasoningEffort        string `json:"reasoning_effort,omitempty"`
}

func (b ResearchBudget) ResponseTimeout() time.Duration {
	return time.Duration(b.ResponseTimeoutSeconds) * time.Second
}
func (b ResearchBudget) StageTimeout() time.Duration {
	return time.Duration(b.StageTimeoutSeconds) * time.Second
}
func (b ResearchBudget) TotalTimeout() time.Duration {
	return time.Duration(b.TotalTimeoutSeconds) * time.Second
}

// ResearchBudgetFor keeps the first-response allowance inside the stage, with
// time left for the answer. Reasoning floors accommodate deliberately slower
// settings without silently reducing the user's selected effort. They are
// finite ceilings, not a promise that a provider will finish within them.
func ResearchBudgetFor(request ResearchRequest, responseWait time.Duration, effort string) ResearchBudget {
	level, _ := normalizeResearchLevel(request.AnalysisLevel)
	if level == ResearchLevelQuantitative {
		return ResearchBudget{TotalTimeoutSeconds: int(ResearchCollectionTimeout.Seconds())}
	}
	stage := ResearchStageTimeout(request)
	if responseWait > 0 {
		stage = max(stage, responseWait+time.Minute)
	} else {
		responseWait = stage
	}
	effort = strings.ToLower(strings.TrimSpace(effort))
	switch effort {
	case "default", "enabled", "medium":
		stage = max(stage, 8*time.Minute)
	case "high":
		stage = max(stage, 10*time.Minute)
	case "xhigh", "max":
		stage = max(stage, 15*time.Minute)
	}
	// Reserve every possible model call, including the shared JSON repair.
	calls := 1
	supplement := time.Duration(0)
	switch level {
	case ResearchLevelStandard:
		calls = 3
	case ResearchLevelDeep:
		calls = 4
		supplement = time.Minute // at most three 12-second evidence requests
	}
	total := ResearchCollectionTimeout + time.Duration(calls)*stage + supplement
	return ResearchBudget{ResponseTimeoutSeconds: int(responseWait.Seconds()), StageTimeoutSeconds: int(stage.Seconds()), TotalTimeoutSeconds: int(total.Seconds()), ReasoningEffort: effort}
}

type researchBudgetKey struct{}

func WithResearchBudget(ctx context.Context, budget ResearchBudget) context.Context {
	return context.WithValue(ctx, researchBudgetKey{}, budget)
}

func ResearchExecutionBudget(ctx context.Context) (ResearchBudget, bool) {
	budget, ok := ctx.Value(researchBudgetKey{}).(ResearchBudget)
	return budget, ok
}
