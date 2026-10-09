package portfoliooptimization

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"easy-stock/backend/internal/agent"
	pi "easy-stock/backend/internal/portfolioinspection"
)

// Replays a saved, validated review without a provider or any network access.
// These scores may only be reused for exactly the same A/B configurations.
type savedAcceptanceReview struct {
	*testGateway
	plan  Plan
	calls int
}

func (g *savedAcceptanceReview) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	if err := ctx.Err(); err != nil {
		return agent.PromptResult{}, err
	}
	g.calls++
	if g.calls != 1 || !strings.Contains(prompt, "独立组合复评员") {
		return agent.PromptResult{}, fmt.Errorf("saved acceptance replay allows only one frozen review")
	}
	var input struct {
		A, B struct {
			Weights map[string]float64 `json:"weights"`
		}
	}
	_, payload, ok := strings.Cut(prompt, "[资料JSON]\n")
	if !ok {
		return agent.PromptResult{}, fmt.Errorf("missing review payload")
	}
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		return agent.PromptResult{}, err
	}
	a, b := g.plan.Original, g.plan.Proposed
	if a.AlgorithmVersion != pi.AlgorithmVersion || b.AlgorithmVersion != pi.AlgorithmVersion {
		return agent.PromptResult{}, fmt.Errorf("cannot replay scores under a different rubric")
	}
	if g.plan.AssessmentOrder == "target_first" {
		a, b = b, a
	}
	for _, pair := range []struct {
		report  pi.Report
		weights map[string]float64
	}{{a, input.A.Weights}, {b, input.B.Weights}} {
		w, total, err := weights(pair.report.Request.Holdings, false)
		if err != nil {
			return agent.PromptResult{}, err
		}
		want := map[string]float64{}
		for symbol, weight := range w {
			want[symbol] = float64(weight) * 100 / float64(total)
		}
		if !reflect.DeepEqual(want, pair.weights) {
			return agent.PromptResult{}, fmt.Errorf("frozen A/B weights changed")
		}
	}
	// These are program projections added after decoding, not model outputs.
	a.Conclusion.RiskGroups, b.Conclusion.RiskGroups = nil, nil
	a.Conclusion.Holdings, b.Conclusion.Holdings = nil, nil
	a.Conclusion.Scenarios, b.Conclusion.Scenarios = nil, nil
	review := *g.plan.Assessment
	review.EvidenceRefs = append([]pi.EvidenceRef(nil), review.EvidenceRefs...)
	for i := range review.EvidenceRefs {
		review.EvidenceRefs[i].Fact = strings.TrimPrefix(review.EvidenceRefs[i].Fact, "comparison.")
	}
	data, err := json.Marshal(map[string]any{"a": a.Conclusion, "b": b.Conclusion, "assessment": review})
	return agent.PromptResult{Content: string(data)}, err
}

func TestSavedHighScoreRejectionPassesCorrectedAcceptance(t *testing.T) {
	path := os.Getenv("EASY_STOCK_ACCEPTANCE_AUDIT")
	if path == "" {
		t.Skip("explicit private saved optimization required; no live calls")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err = json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	if len(j.Plans) != 1 || j.Proposal == nil || j.Plans[0].Assessment == nil || j.SelectedPlan != nil {
		t.Fatal("expected original rejected review")
	}
	p := j.Plans[0]
	if *p.Original.Conclusion.TotalScore != 54 || *p.Proposed.Conclusion.TotalScore != 72 || p.Checks.Sold != 59 {
		t.Fatal("unexpected private regression fixture")
	}
	classifyReviewedPlan(j, &j.Plans[0])
	if j.Plans[0].Status != "conditional" {
		t.Fatal("corrected acceptance still rejected saved review", j.Plans[0].Error)
	}
	soft := false
	for _, check := range j.Plans[0].RiskChecks {
		if check.Name == "中报盈利兑现依赖" {
			soft = !check.Hard && check.After == 84
		}
	}
	if !soft {
		t.Fatal("shared reporting label remained a hard cap")
	}

	// Start at the frozen review checkpoint using a fresh in-memory database.
	// The original file, live database, research, allocations and scores stay intact.
	j.Version, j.ModelPromptVersion = Version, ModelPromptVersion
	j.Plans[0].Status, j.Plans[0].Error = "pending_review", ""
	j.Plans[0].Assessment = nil
	j.Status, j.Stage, j.Outcome, j.OutcomeReason = "running", "assessing", "", ""
	store, err := pi.OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	g := &savedAcceptanceReview{testGateway: &testGateway{}, plan: p}
	s := NewService(store, g, Dependencies{})
	defer s.Close()
	if err = s.executeFrozen(context.Background(), &j); err != nil {
		t.Fatal(err)
	}
	if g.calls != 1 || j.RevisionCount != 0 || j.SelectedPlan == nil || j.Outcome != "conditional" || j.Status != "succeeded" {
		t.Fatal("saved review not accepted", g.calls, j.Outcome, j.OutcomeReason)
	}
	next := j.Plans[*j.SelectedPlan]
	if !equalWeights(next.Target, p.Target) || *next.Original.Conclusion.TotalScore != 54 || *next.Proposed.Conclusion.TotalScore != 72 || !reflect.DeepEqual(next.Checks, p.Checks) {
		t.Fatal("acceptance changed weights, scores or constraints")
	}
	if _, err = ApplyRequest(j); err != nil {
		t.Fatal("accepted target cannot be used", err)
	}
	if err = s.ValidateApplication(context.Background(), pi.Request{SourceOptimizationID: j.ID, Holdings: next.Target, TraderProfile: j.Source.Request.TraderProfile, Horizon: j.Source.Request.Horizon, ResearchLevel: j.Source.Request.ResearchLevel}); err != nil {
		t.Fatal("application validation rejected saved replay", err)
	}
	t.Logf("offline production review replay: %d -> %d; replacement %.0f%%; retained %d%%; one recorded response, zero live model calls", *next.Original.Conclusion.TotalScore, *next.Proposed.Conclusion.TotalScore, next.Checks.Replacement, next.Checks.Retained)
}
