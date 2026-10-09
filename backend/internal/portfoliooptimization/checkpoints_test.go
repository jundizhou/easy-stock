package portfoliooptimization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	pi "easy-stock/backend/internal/portfolioinspection"
)

type checkpointGateway struct {
	*testGateway
	outputs []string
	prompts []string
	stop    bool
}

func (g *checkpointGateway) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	g.prompts = append(g.prompts, prompt)
	if len(g.outputs) > 0 {
		content := g.outputs[0]
		g.outputs = g.outputs[1:]
		return agent.PromptResult{Content: content}, nil
	}
	if g.stop {
		return agent.PromptResult{}, context.Canceled
	}
	return g.testGateway.Prompt(ctx, prompt)
}
func jsonText(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPartialProposalSurvivesRestartAndResumesOnlyMissingRow(t *testing.T) {
	j, good, bad := partsFixture(t)
	j.ID, j.Version, j.Status = "partial-restart", Version, "running"
	j.LatestTradeDate, j.SnapshotAt = LatestSession(time.Now()), time.Now().UTC()
	j.ModelStageDurationMS = map[string]int64{"proposing": 180000}
	j.ExecutionDurationMS = 180000
	g := &checkpointGateway{testGateway: &testGateway{}, stop: true, outputs: []string{
		programProposalJSON(t, bad),
		partsPatch(t, map[string]any{"allocation:600519.SH": programAllocation(good.Alternatives[0].Allocations[0])}),
	}}
	s, store, research := setupService(t, g.testGateway)
	s.gateway = g
	s.run(context.Background(), j)
	saved, err := s.Get(context.Background(), j.ID)
	if err != nil || saved.ProposalCheckpoint == nil || len(saved.ProposalCheckpoint.Pending) != 1 || saved.ProposalCheckpoint.Pending[0].Key != "allocation:300185.SZ" {
		t.Fatal("partial success lost", err)
	}
	if saved.CheckpointProgress.SavedStocks != 2 || !saved.ResumeAvailable || saved.ModelStageDurationMS["proposing"] < 180000 {
		t.Fatal("checkpoint or budget reset", saved.CheckpointProgress)
	}
	before := jsonText(t, saved.ProposalCheckpoint.Draft.Alternatives[0].Allocations[0])
	s.Close()
	next := &checkpointGateway{testGateway: g.testGateway, outputs: []string{partsPatch(t, map[string]any{"allocation:300185.SZ": programAllocation(good.Alternatives[0].Allocations[2])})}}
	fresh := NewService(store, next, s.deps)
	defer fresh.Close()
	if _, err := fresh.Resume(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	done := waitDone(t, fresh, j.ID)
	if done.Status != "succeeded" || done.SelectedPlan == nil || research.Load() != 0 || len(next.prompts) != 2 {
		t.Fatal("resume repeated completed work", done.Status, done.Error, len(next.prompts), research.Load())
	}
	pending := strings.Split(next.prompts[0], "[待补项]\n")
	if len(pending) != 2 || strings.Contains(pending[1], "allocation:600519.SH") || !strings.Contains(pending[1], "allocation:300185.SZ") {
		t.Fatal("resumed wrong repair scope")
	}
	after := done.Proposal.Alternatives[0].Allocations[0]
	after.Preferred = saved.ProposalCheckpoint.Draft.Alternatives[0].Allocations[0].Preferred
	if before != jsonText(t, after) {
		t.Fatal("valid judgment changed after restart")
	}
	if done.ModelStageDurationMS["proposing"] < saved.ModelStageDurationMS["proposing"] || done.ExecutionDurationMS < saved.ExecutionDurationMS {
		t.Fatal("resume reset budget")
	}
}

func checkpointReviewFixture() (Job, Plan, pi.AIReport, Assessment) {
	j := fixtureJob()
	p := fixtureProposal()
	j.Proposal = &p
	target := holds(45, 35)
	req := j.Source.Request
	req.Holdings = target
	plan := Plan{Target: target, AssessmentOrder: "original_first", Original: j.Source, Proposed: pi.OptimizationReport(req, j.Results)}
	score := fixtureScore(nil)
	score.Holdings, score.Scenarios = nil, nil
	review := Assessment{Preferred: "b", Reason: "集中有所改善", Issue: "原集中问题", EvidenceRefs: []pi.EvidenceRef{{Fact: "max_single_percent"}}}
	return j, plan, score, review
}
func TestReviewKeepsValidScoreAndComparisonWhileRepairingOnlyFailures(t *testing.T) {
	j, plan, good, review := checkpointReviewFixture()
	comparison := ReviewedInvestmentComparison{PreferredSymbol: "000858.SZ", OtherSymbol: "600519.SH", Dimension: "business", Reason: "盈利用途更匹配", Tradeoff: "保留增长不确定性", EvidenceRefs: []pi.EvidenceRef{{ReportID: "report-600519.SH", SourceID: "s1"}, {ReportID: "report-000858.SZ", SourceID: "s1"}}}
	badComparison := comparison
	badComparison.Dimension = "portfolio_fit"
	badComparison.EvidenceRefs = []pi.EvidenceRef{{Fact: "invented"}}
	review.InvestmentComparisons = []ReviewedInvestmentComparison{comparison, badComparison}
	var bad pi.AIReport
	_ = json.Unmarshal([]byte(jsonText(t, good)), &bad)
	bad.Dimensions[0].EvidenceRefs = []pi.EvidenceRef{{Fact: "invented"}}
	c := &ReviewCheckpoint{}
	_, _, _, err := c.validate(j, plan, jsonText(t, map[string]any{"a": good, "b": bad, "assessment": review}))
	var parts *reviewPartsError
	if !errors.As(err, &parts) || len(c.Pending) != 2 || len(c.Blocks) != 3 {
		t.Fatal("valid blocks discarded", err, len(c.Blocks))
	}
	originalA := string(c.Blocks["a"])
	originalComparison := string(c.Blocks["comparison:0"])
	// A single successful block is committed even when the other requested patch is wrong.
	_, _, _, err = c.validate(j, plan, partsPatch(t, map[string]any{"b": good, "comparison:1": badComparison}))
	if !errors.As(err, &parts) || len(c.Pending) != 1 || c.Pending[0].Key != "comparison:1" {
		t.Fatal("successful score was lost", err)
	}
	persisted := jsonText(t, c)
	c = &ReviewCheckpoint{}
	_ = json.Unmarshal([]byte(persisted), c)
	comparison.Dimension = "portfolio_fit"
	a, b, r, err := c.validate(j, plan, partsPatch(t, map[string]any{"comparison:1": comparison}))
	if err != nil || !r.Accepted || len(r.InvestmentComparisons) != 2 || a.TotalScore == nil || b.TotalScore == nil {
		t.Fatal("partial review did not complete", err)
	}
	if string(c.Blocks["a"]) != originalA || string(c.Blocks["comparison:0"]) != originalComparison {
		t.Fatal("valid block rewritten")
	}
}
func TestMissingAssessmentDoesNotDiscardValidScores(t *testing.T) {
	j, plan, good, review := checkpointReviewFixture()
	c := &ReviewCheckpoint{}
	_, _, _, err := c.validate(j, plan, jsonText(t, map[string]any{"a": good, "b": good}))
	if err == nil || len(c.Blocks) != 2 || len(c.Pending) != 1 || c.Pending[0].Key != "assessment" {
		t.Fatal("missing assessment discarded scores", err)
	}
	if _, _, _, err = c.validate(j, plan, partsPatch(t, map[string]any{"assessment": review})); err != nil {
		t.Fatal(err)
	}
}
func TestModelLoopBoundsAndCompletedReplay(t *testing.T) {
	for _, changing := range []bool{false, true} {
		t.Run(fmt.Sprint(changing), func(t *testing.T) {
			g := &repairGateway{testGateway: &testGateway{}, output: `{}`}
			s, _, _ := setupService(t, g.testGateway)
			s.gateway = g
			j := fixtureJob()
			j.ID = "bounded"
			j.Stage = "proposing"
			calls := 0
			err := s.model(context.Background(), &j, "bounded operation", func(string) error {
				calls++
				if changing {
					return fmt.Errorf("failure %d", calls)
				}
				return errors.New("same failure")
			})
			want := 3
			if changing {
				want = 1 + MaxOperationRepairs
			}
			if err == nil || len(g.prompts) != want || checkpointCanResume(j) {
				t.Fatal("unbounded loop", err, len(g.prompts))
			}
			stored, e := s.Get(context.Background(), j.ID)
			if e != nil {
				t.Fatal(e)
			}
			_ = s.model(context.Background(), &stored, "bounded operation", func(string) error { return errors.New("same failure") })
			if len(g.prompts) != want {
				t.Fatal("resume reset loop bounds")
			}
		})
	}
	g := &repairGateway{testGateway: &testGateway{}, output: `{"value":7}`}
	s, _, _ := setupService(t, g.testGateway)
	s.gateway = g
	j := fixtureJob()
	j.ID = "completed-loop"
	j.Stage = "proposing"
	var value struct{ Value int }
	validate := func(s string) error { return json.Unmarshal([]byte(s), &value) }
	if err := s.model(context.Background(), &j, "complete", validate); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Get(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	value.Value = 0
	stored.ModelStageDurationMS["proposing"] = ModelTimeout.Milliseconds()
	if err := s.model(context.Background(), &stored, "complete", validate); err != nil || value.Value != 7 || len(g.prompts) != 1 {
		t.Fatal("cached result failed to restore callback", err, value)
	}
}
func TestExhaustedTaskCannotResumeButCanExplicitlyRestart(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	j := fixtureJob()
	j.ID, j.Version, j.Status, j.Stage = "exhausted", Version, "incomplete", "proposing"
	j.LatestTradeDate = LatestSession(time.Now())
	j.ResumeAvailable = true
	j.ExecutionDurationMS = TotalTimeout.Milliseconds()
	if err := s.save(&j); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resume(context.Background(), j.ID); err == nil {
		t.Fatal("resume reset total budget")
	}
	j.ResumeAvailable = false
	if err := s.save(&j); err != nil {
		t.Fatal(err)
	}
	newJob, err := s.Start(context.Background(), "source", Request{RestartFrom: j.ID})
	if err != nil || newJob.ID == j.ID {
		t.Fatal("no explicit restart path", err)
	}
	done := waitDone(t, s, newJob.ID)
	if done.Status != "succeeded" {
		t.Fatal(done.Error)
	}
	old, _ := s.Get(context.Background(), j.ID)
	if old.ExecutionDurationMS != TotalTimeout.Milliseconds() {
		t.Fatal("restart changed original history")
	}
}
func TestPreselectionReviewsOneFeasibleConfiguration(t *testing.T) {
	j, alt := searchFixture(t)
	p := Proposal{Alternatives: []Alternative{alt}}
	j.Proposal = &p
	solutions, err := SearchAllocations(context.Background(), j, alt)
	if err != nil || len(solutions) < 2 {
		t.Fatal("fixture lacks alternatives", err)
	}
	for _, solution := range solutions {
		req := j.Source.Request
		req.Holdings = solution.Target
		j.Plans = append(j.Plans, Plan{Status: "pending_review", Target: solution.Target, Allocations: alt.Allocations, Checks: Check(j.Baseline, solution.Target), Improvements: measureImprovements(j, solution.Target), Original: j.Source, Proposed: pi.OptimizationReport(req, j.Results)})
	}
	selectReviewPlan(&j)
	selected := ""
	count := 0
	for _, plan := range j.Plans {
		if plan.Status == "pending_review" {
			count++
			selected = baselineHash(plan.Target)
		}
		if plan.Assessment != nil || plan.Proposed.Conclusion.ScoreAvailable {
			t.Fatal("program invented AI score")
		}
	}
	if count != 1 {
		t.Fatal("more than one pending review", count)
	}
	for i := range j.Plans {
		j.Plans[i].Status = "pending_review"
	}
	for i, k := 0, len(j.Plans)-1; i < k; i, k = i+1, k-1 {
		j.Plans[i], j.Plans[k] = j.Plans[k], j.Plans[i]
	}
	selectReviewPlan(&j)
	for _, plan := range j.Plans {
		if plan.Status == "pending_review" && baselineHash(plan.Target) != selected {
			t.Fatal("selection depends on solver enumeration order")
		}
	}
}

func TestUncleanRestartChargesOnlyUnaccountedIssuedBudget(t *testing.T) {
	now := time.Now()
	j := fixtureJob()
	j.ExecutionDurationMS = 90000
	j.ModelDurationMS = 60000
	j.ModelStageDurationMS = map[string]int64{"assessing": 60000}
	j.ModelLoops = map[string]*ModelLoopState{"review": {InFlight: &ModelFlight{StartedAt: now.Add(-time.Hour), BudgetMS: 480000, AccountedMS: 60000, BudgetKey: "assessing"}}}
	settleInterruptedUsage(&j, now)
	if j.ExecutionDurationMS != 510000 || j.ModelDurationMS != 480000 || j.ModelStageDurationMS["assessing"] != 480000 {
		t.Fatal("crashed request reset or double charged budget")
	}
	settleInterruptedUsage(&j, now.Add(time.Hour))
	if j.ExecutionDurationMS != 510000 {
		t.Fatal("paused time charged")
	}
}

func TestNarrativeRepairCannotResampleDimensionScores(t *testing.T) {
	j, plan, good, review := checkpointReviewFixture()
	bad := jsonText(t, good)
	bad = strings.Replace(bad, `"fact":"concentration_hhi"`, `"fact":"invented"`, 1)
	c := &ReviewCheckpoint{}
	_, _, _, err := c.validate(j, plan, `{"a":`+jsonText(t, good)+`,"b":`+bad+`,"assessment":`+jsonText(t, review)+`}`)
	if err == nil {
		t.Fatal("fixture did not fail")
	}
	changed := jsonText(t, good)
	changed = strings.Replace(changed, `"score":70`, `"score":90`, 1)
	_, _, _, err = c.validate(j, plan, partsPatch(t, map[string]any{"b": json.RawMessage(changed)}))
	if err == nil || !strings.Contains(err.Error(), "不得改写") {
		t.Fatal("score resampling accepted", err)
	}
	if _, _, _, err = c.validate(j, plan, partsPatch(t, map[string]any{"b": good})); err != nil {
		t.Fatal(err)
	}
}

// Optional offline recovery of a real saved journal. Never contacts a model or
// production storage; verifies that cached initial/patch/review responses alone
// reconstruct exactly the same approved target and score.
func TestSavedOptimizationJournalRecovery(t *testing.T) {
	path := os.Getenv("EASY_STOCK_SAVED_OPTIMIZATION_JOURNAL")
	if path == "" {
		t.Skip("set a private saved v23 job to verify offline recovery")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err = json.Unmarshal(raw, &j); err != nil {
		t.Fatal(err)
	}
	if j.SelectedPlan == nil || len(j.ModelLoops) == 0 || j.RevisionCount != 0 {
		t.Fatal("requires a completed first-round journal")
	}
	expected := j.Plans[*j.SelectedPlan]
	attempts := len(j.ModelAttempts)
	j.Proposal, j.ProposalCheckpoint, j.Plans, j.SelectedPlan = nil, nil, nil, nil
	j.Status = "running"
	g := &testGateway{}
	s, _, research := setupService(t, g)
	if err = s.executeFrozen(context.Background(), &j); err != nil {
		t.Fatal(err)
	}
	if g.calls.Load() != 0 || research.Load() != 0 || len(j.ModelAttempts) != attempts || j.SelectedPlan == nil {
		t.Fatal("journal called model or lost selection", g.calls.Load(), j.Outcome)
	}
	actual := j.Plans[*j.SelectedPlan]
	if !equalWeights(actual.Target, expected.Target) || jsonText(t, actual.Original.Conclusion) != jsonText(t, expected.Original.Conclusion) || jsonText(t, actual.Proposed.Conclusion) != jsonText(t, expected.Proposed.Conclusion) {
		t.Fatal("recovery changed target or score")
	}
}
