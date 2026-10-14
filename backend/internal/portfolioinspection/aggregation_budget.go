package portfolioinspection

import (
	"context"
	"strings"
	"time"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/stockanalysis"
)

const aggregationModelCalls = 2 // Initial scoring and at most one validation repair.

type aggregationBudget struct {
	ResponseTimeout time.Duration
	StageTimeout    time.Duration
	ReasoningEffort string
}

func (b aggregationBudget) TotalTimeout() time.Duration {
	return aggregationModelCalls * b.StageTimeout
}

// Keep a full response allowance and time to finish the answer in each call.
// Slower reasoning settings get a larger finite ceiling without changing the
// user's effort. A validation repair has its own call allowance, but shares the
// aggregation deadline; provider failures do not trigger another scoring call.
func aggregationBudgetFor(responseWait time.Duration, effort string) aggregationBudget {
	if responseWait <= 0 {
		responseWait = appsettings.DefaultLLMResponseTimeoutSeconds * time.Second
	}
	stage := 12 * time.Minute
	effort = strings.ToLower(strings.TrimSpace(effort))
	switch effort {
	case "high":
		stage = 15 * time.Minute
	case "xhigh", "max":
		stage = 20 * time.Minute
	}
	stage = max(stage, responseWait+time.Minute)
	return aggregationBudget{ResponseTimeout: responseWait, StageTimeout: stage, ReasoningEffort: effort}
}

// Task bindings freeze settings before any research starts, so the outer job
// reserve and scoring calls cannot inherit a mid-task model/settings change.
func aggregationBudgetFromContext(ctx context.Context) aggregationBudget {
	responseWait := time.Duration(0)
	if llm, ok := agent.BoundLLM(ctx); ok {
		responseWait = time.Duration(appsettings.NormalizeLLMResponseTimeoutSeconds(llm.ResponseTimeoutSeconds)) * time.Second
	}
	effort, _ := agent.BoundReasoningEffort(ctx)
	return aggregationBudgetFor(responseWait, effort)
}

func inspectionTimeout(request Request, holdings, concurrency int, aggregation aggregationBudget) time.Duration {
	research := stockanalysis.ResearchBudgetFor(stockanalysis.ResearchRequest{AnalysisLevel: request.ResearchLevel}, aggregation.ResponseTimeout, aggregation.ReasoningEffort)
	concurrency = max(1, concurrency)
	batches := (holdings + concurrency - 1) / concurrency
	return time.Duration(batches)*research.TotalTimeout() + aggregation.TotalTimeout() + 2*time.Minute
}
