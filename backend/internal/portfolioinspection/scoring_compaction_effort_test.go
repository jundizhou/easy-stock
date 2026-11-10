package portfolioinspection

import (
	"context"
	"testing"

	"easy-stock/backend/internal/agent"
)

func TestCompactionCapsReasoningWithoutChangingBoundScoringEffort(t *testing.T) {
	binding, _ := aggregationTestAgent(t, 300, "max")
	ctx, release, err := binding.BindTask(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	g := &aggregationDeadlineGateway{scoreGateway: &scoreGateway{}, respond: func(int) (agent.PromptResult, error) {
		return agent.PromptResult{Content: `{"summary":"原报告风险与执行前提仍需核验，来源e-execution"}`}, nil
	}}
	r := compactionRunner{service: NewService(nil, g, nil, nil), ctx: ctx, budget: aggregationBudgetFromContext(ctx)}
	defer r.service.Close()
	if _, err := r.summarizeChunk(compactionChunk{Symbol: "510300.SH"}); err != nil {
		t.Fatal(err)
	}
	if len(g.options) != 1 || g.options[0].ReasoningEffortCap != "low" || !g.options[0].DisableTools {
		t.Fatal("mechanical summary inherited expensive reasoning", g.options)
	}
	if effort, _ := agent.BoundReasoningEffort(ctx); effort != "max" {
		t.Fatal("summary cap changed the frozen scoring effort", effort)
	}
}
