package portfoliooptimization

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	pi "easy-stock/backend/internal/portfolioinspection"
)

func qualityScore(score int) pi.AIReport {
	r := fixtureScore(nil)
	r.ScoreAvailable = true
	r.TotalScore = &score
	for i := range r.Dimensions {
		value := score
		r.Dimensions[i].Score = &value
	}
	return r
}

func TestScoreAdmissionBoundaryAndPersistedScoreIntegrity(t *testing.T) {
	for _, score := range []int{0, 69, 70, 100} {
		p := Plan{Proposed: pi.Report{Conclusion: qualityScore(score)}}
		if meetsScoreMinimum(p) != (score >= 70) {
			t.Fatal("incorrect floor", score)
		}
	}
	r := qualityScore(69)
	fake := 100
	r.TotalScore = &fake
	if meetsScoreMinimum(Plan{Proposed: pi.Report{Conclusion: r}}) {
		t.Fatal("fabricated total bypassed rubric")
	}
	r = qualityScore(70)
	r.ScoreAvailable = false
	if meetsScoreMinimum(Plan{Proposed: pi.Report{Conclusion: r}}) {
		t.Fatal("unavailable score admitted")
	}
	r = qualityScore(70)
	r.Dimensions[0].Key = "unknown"
	if meetsScoreMinimum(Plan{Proposed: pi.Report{Conclusion: r}}) {
		t.Fatal("unknown dimension admitted")
	}
	r = qualityScore(70)
	r.Dimensions[0].Score = nil
	if meetsScoreMinimum(Plan{Proposed: pi.Report{Conclusion: r}}) {
		t.Fatal("missing dimension admitted")
	}
}

func TestNearTargetRequiresVerifiedImprovementAndAllDimensions(t *testing.T) {
	for _, tc := range []struct {
		before, after int
		want          bool
	}{{60, 65, true}, {64, 69, true}, {65, 69, false}, {50, 64, false}, {69, 65, false}} {
		p := Plan{Original: pi.Report{Conclusion: qualityScore(tc.before)}, Proposed: pi.Report{Conclusion: qualityScore(tc.after)}}
		if meetsScoreMinimum(p) != tc.want || meetsScoreTarget(p) {
			t.Fatal("incorrect relaxed admission", tc)
		}
	}
	p := Plan{Original: pi.Report{Conclusion: qualityScore(50)}, Proposed: pi.Report{Conclusion: qualityScore(67)}}
	for i, v := range []int{49, 79, 79, 68} {
		p.Proposed.Conclusion.Dimensions[i].Score = &v
	}
	if n, ok := pi.VerifiedOptimizationScore(p.Proposed.Conclusion); !ok || n != 67 {
		t.Fatal("fixture rubric", n, ok)
	}
	if meetsScoreMinimum(p) {
		t.Fatal("weak dimension accepted by relaxed rule")
	}
	p.Proposed.Conclusion = qualityScore(69)
	*p.Original.Conclusion.TotalScore = 40
	if meetsScoreMinimum(p) {
		t.Fatal("fabricated original score manufactured improvement")
	}
}

type qualityGateway struct {
	*testGateway
	scores             []int
	proposals, reviews int
	prompts            []string
	unchanged          bool
	repair             bool
	rejectFinal        bool
	invalidReviews     int
	fullMatch          bool
	proposal           *Proposal
	refinementError    error
}

func (g *qualityGateway) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	g.calls.Add(1)
	g.prompts = append(g.prompts, prompt)
	var output any
	if strings.Contains(prompt, "独立组合复评员") {
		var input struct {
			A, B struct {
				Weights map[string]float64 `json:"weights"`
			}
		}
		if err := json.NewDecoder(strings.NewReader(strings.Split(prompt, "[资料JSON]\n")[1])).Decode(&input); err != nil {
			return agent.PromptResult{}, err
		}
		score := g.scores[g.reviews]
		g.reviews++
		a, b := qualityScore(60), qualityScore(60)
		if g.fullMatch {
			a.StyleMatch, b.StyleMatch = "匹配", "匹配"
		}
		preferred := "b"
		if input.A.Weights["600519.SH"] < input.B.Weights["600519.SH"] {
			a = qualityScore(score)
			preferred = "a"
		} else {
			b = qualityScore(score)
		}
		if g.fullMatch {
			a.StyleMatch, b.StyleMatch = "匹配", "匹配"
		}
		if g.reviews <= g.invalidReviews {
			for _, r := range []*pi.AIReport{&a, &b} {
				for i := 0; i < 2; i++ {
					r.Dimensions[i].Adjustments = []pi.ScoreAdjustment{{RiskID: "重复共振", Reason: "同一风险", Points: -5}}
				}
			}
		}
		// A model-supplied fake total must be ignored by the shared decoder.
		fake := 100
		a.TotalScore = &fake
		b.TotalScore = &fake
		a.Holdings, b.Holdings, a.Scenarios, b.Scenarios = nil, nil, nil, nil
		if g.rejectFinal && g.reviews == len(g.scores) {
			preferred = "neither"
		}
		output = map[string]any{"a": a, "b": b, "assessment": Assessment{Preferred: preferred, Reason: "降低集中但仍需改善风险", Issue: "原集中缺陷", ResidualRisks: []string{"单票集中仍在"}, EvidenceRefs: []pi.EvidenceRef{{Fact: "max_single_percent"}}}}
	} else {
		if g.repair {
			return agent.PromptResult{Content: "malformed JSON"}, nil
		}
		p := fixtureProposal()
		g.proposals++
		if g.proposals > 1 && g.refinementError != nil {
			return agent.PromptResult{}, g.refinementError
		}
		if g.proposal != nil {
			data, _ := json.Marshal(g.proposal)
			return agent.PromptResult{Content: string(data)}, nil
		}
		first := g.proposals == 1 || g.unchanged
		if first {
			p.Alternatives[0].Allocations[0].Minimum = 50
			p.Alternatives[0].Allocations[0].Maximum = 50
			p.Alternatives[0].Allocations[0].Preferred = 50
			p.Alternatives[0].Allocations[1].Minimum = 30
			p.Alternatives[0].Allocations[1].Maximum = 30
			p.Alternatives[0].Allocations[1].Preferred = 30
		}
		if strings.Contains(prompt, "allocation_ranges") {
			rows := []rangeAdjustment{}
			for _, a := range p.Alternatives[0].Allocations {
				rows = append(rows, rangeAdjustment{Symbol: a.Symbol, Minimum: a.Minimum, Maximum: a.Maximum, Reason: "降低原单票集中"})
			}
			output = map[string]any{"allocation_ranges": rows, "keep_reason": ""}
		} else {
			output = p
		}
	}
	data, _ := json.Marshal(output)
	return agent.PromptResult{Content: string(data)}, nil
}

func TestOneInvocationSearchesBeyondAIReferenceAndStopsOnQualifiedReview(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	j, alt := searchFixture(t)
	j.ID = "quality-search"
	j.Version = Version
	j.Proposal = nil
	j.SnapshotAt = time.Now()
	j.Fingerprint = "ab"
	p := Proposal{Alternatives: []Alternative{alt}}
	q := &qualityGateway{testGateway: g, proposal: &p, scores: []int{73}}
	s.gateway = q
	if err := s.execute(context.Background(), &j); err != nil {
		t.Fatal(err)
	}
	if j.SelectedPlan == nil || !meetsScoreMinimum(j.Plans[*j.SelectedPlan]) || q.proposals != 1 || q.reviews != 1 || j.RevisionCount != 0 {
		t.Fatal("goal was only gated, or extra calls ran", j.Outcome, j.Error, q.proposals, q.reviews)
	}
	selected := j.Plans[*j.SelectedPlan]
	if selected.Search == nil || selected.Search.LossPercent >= 20 || equalWeights(j.Baseline, selected.Target) {
		t.Fatal("reference weights preserved", selected.Target)
	}
	for _, plan := range j.Plans {
		if plan.Status == "pending_review" {
			t.Fatal("review continued after reaching the goal")
		}
	}
}

func TestInvalidReviewDoesNotSampleOtherConfigurations(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	j, alt := searchFixture(t)
	j.ID, j.Version, j.Fingerprint = "stop-invalid", Version, "ab"
	j.Proposal, j.SnapshotAt = nil, time.Now()
	p := Proposal{Alternatives: []Alternative{alt}}
	q := &qualityGateway{testGateway: g, proposal: &p, scores: []int{60, 60, 74}, invalidReviews: 2}
	s.gateway = q
	if err := s.execute(context.Background(), &j); err != nil {
		t.Fatal(err)
	}
	if j.SelectedPlan != nil || q.proposals != 1 || q.reviews != 3 || j.RevisionCount != 0 || j.Outcome != "review_invalid" {
		t.Fatal("invalid review triggered another configuration", j.Outcome, q.reviews)
	}
	invalid := 0
	for _, p := range j.Plans {
		if p.Status == "invalid_review" {
			invalid++
		}
		if p.Assessment != nil {
			t.Fatal("unrepaired score was adopted")
		}
	}
	if invalid != 1 {
		t.Fatal("more than one configuration reviewed", invalid)
	}
}

func TestZeroCashPreservesMatchAndDoesNotCallAgain(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	j, alt := searchFixture(t)
	for i, w := range []int{40, 30, 20, 10} {
		j.Source.Request.Holdings[i].Weight = w
		j.Results[i].Holding.Weight = w
		alt.Allocations[i].Preferred = w
	}
	j.Baseline = append([]pi.Holding(nil), j.Source.Request.Holdings...)
	j.ID, j.Version, j.Fingerprint = "cash-label", Version, "ab"
	j.Proposal, j.SnapshotAt = nil, time.Now()
	p := Proposal{Alternatives: []Alternative{alt}}
	q := &qualityGateway{testGateway: g, proposal: &p, scores: []int{75}, fullMatch: true}
	s.gateway = q
	if err := s.execute(context.Background(), &j); err != nil {
		t.Fatal(err)
	}
	if j.SelectedPlan == nil || q.reviews != 1 || q.proposals != 1 || jobRepairUsed(&j) {
		t.Fatal("cash label caused a new score sample", j.Outcome, q.reviews)
	}
	r := j.Plans[*j.SelectedPlan].Proposed
	if r.Metrics.CashPercent != 0 || r.Conclusion.StyleMatch != "匹配" || *r.Conclusion.TotalScore != 75 {
		t.Fatal("cash correction changed scores", r.Conclusion)
	}
	for _, d := range r.Conclusion.Dimensions {
		if *d.Score != 75 {
			t.Fatal("dimension changed", d)
		}
	}
}

func TestLowScoreRevisionIsBoundedAndReusesAllResearch(t *testing.T) {
	for _, tc := range []struct {
		name         string
		scores       []int
		unchanged    bool
		wantSelected bool
		calls        int
	}{
		{"69_then_70", []int{69, 70}, false, true, 4},
		{"two_rejected_rounds", []int{59, 64}, false, false, 4},
		{"65_after_full_search", []int{59, 65}, false, true, 4},
		{"keep_better_prior_alternative", []int{69, 65}, false, true, 4},
		{"same_target_not_regraded", []int{59}, true, false, 3},
		{"70_no_revision", []int{70}, false, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &testGateway{}
			s, store, researchCalls := setupService(t, g)
			q := &qualityGateway{testGateway: g, scores: tc.scores, unchanged: tc.unchanged}
			s.gateway = q
			collectCalls := 0
			collect := s.deps.Collect
			s.deps.Collect = func(ctx context.Context, r pi.Report, req Request, at time.Time) (Universe, error) {
				collectCalls++
				return collect(ctx, r, req, at)
			}
			start, err := s.Start(context.Background(), "source", Request{})
			if err != nil {
				t.Fatal(err)
			}
			j := waitDone(t, s, start.ID)
			if j.Status != "succeeded" || (j.SelectedPlan != nil) != tc.wantSelected || int(g.calls.Load()) != tc.calls {
				t.Fatal(j.Status, j.Outcome, j.Error, j.SelectedPlan, g.calls.Load())
			}
			if researchCalls.Load() != 0 || collectCalls != 1 || j.NewStocks != 0 {
				t.Fatal("screening/research repeated", collectCalls, researchCalls.Load())
			}
			if tc.scores[0] < 70 {
				if j.RevisionCount != 1 || len(j.RevisionHistory) != 1 || *j.RevisionHistory[0].Conclusion.TotalScore != tc.scores[0] {
					t.Fatal("missing rejected round", j.RevisionHistory)
				}
				revisionPrompt := ""
				for _, prompt := range q.prompts {
					if strings.Contains(prompt, "previous_review") {
						revisionPrompt = prompt
					}
					if strings.Contains(prompt, "独立组合复评员") && (strings.Contains(prompt, "previous_review") || strings.Contains(prompt, "minimum_portfolio_score")) {
						t.Fatal("desired score leaked to reviewer")
					}
				}
				if revisionPrompt == "" || !strings.Contains(revisionPrompt, "单票集中仍在") || len(revisionPrompt) > MaxRevisionModelPromptBytes {
					t.Fatal("missing/bloated revision feedback")
				}
			} else if j.RevisionCount != 0 || len(j.RevisionHistory) != 0 {
				t.Fatal("unnecessary revision")
			}
			for _, p := range j.Plans {
				if p.Checks.Valid && (p.Checks.Total != 80 || p.Checks.Cash != 20 || p.Checks.Replacement > 70) {
					t.Fatal("changed original constraints", p.Checks)
				}
			}
			if !equalWeights(j.Baseline, fixtureJob().Baseline) {
				t.Fatal("revision reset chain baseline")
			}
			if !tc.wantSelected {
				if j.Outcome != "below_target_score" || !strings.Contains(j.OutcomeReason, "未达标") || j.ResumeAvailable {
					t.Fatal("rejected target advertised as success", j.Outcome, j.OutcomeReason)
				}
				if _, err := ApplyRequest(j); err == nil {
					t.Fatal("unqualified result applied")
				}
			} else if !meetsScoreMinimum(j.Plans[*j.SelectedPlan]) {
				t.Fatal("selected score below floor")
			}
			if tc.name == "keep_better_prior_alternative" {
				p := j.Plans[*j.SelectedPlan]
				if *p.Proposed.Conclusion.TotalScore != 69 || !equalWeights(p.Target, j.RevisionHistory[0].Target) || !strings.Contains(j.OutcomeReason, "未达到70分") || j.FallbackPlan != nil {
					t.Fatal("prior independently reviewed alternative lost or misrepresented", j.OutcomeReason)
				}
				if _, err := ApplyRequest(j); err != nil {
					t.Fatal("valid relaxed plan cannot be used", err)
				}
			}
			// Reopening the same input returns the saved task, without further sampling.
			again, err := s.Start(context.Background(), "source", Request{})
			if err != nil || again.ID != j.ID || int(g.calls.Load()) != tc.calls {
				t.Fatal("completed result resampled", err)
			}
			if _, err := s.Resume(context.Background(), j.ID); err == nil {
				t.Fatal("completed result resumed")
			}
			src, _ := store.Get(context.Background(), "source")
			if src.Request.Holdings[0].Weight != 60 {
				t.Fatal("source mutated")
			}
		})
	}
}

func TestLaterFailureKeepsReviewedAlternativeButCancellationStops(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failure  error
		selected bool
		status   string
	}{
		{"provider_failure", errors.New("revision provider failure"), true, "succeeded"},
		{"user_cancelled", context.Canceled, false, "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &testGateway{}
			s, _, _ := setupService(t, g)
			q := &qualityGateway{testGateway: g, scores: []int{69}, refinementError: tc.failure}
			s.gateway = q
			start, err := s.Start(context.Background(), "source", Request{})
			if err != nil {
				t.Fatal(err)
			}
			j := waitDone(t, s, start.ID)
			if j.Status != tc.status || (j.SelectedPlan != nil) != tc.selected || q.reviews != 1 || q.proposals != 2 {
				t.Fatal("lost valid fallback or ignored cancellation", j.Status, j.Outcome, j.Error)
			}
			if tc.selected {
				if *j.Plans[*j.SelectedPlan].Proposed.Conclusion.TotalScore != 69 || !strings.Contains(j.OutcomeReason, "后续搜索未完成") {
					t.Fatal("failed output altered reviewed score", j.OutcomeReason)
				}
				if _, err := ApplyRequest(j); err != nil {
					t.Fatal(err)
				}
			} else if _, err := ApplyRequest(j); err == nil {
				t.Fatal("cancelled task became applicable")
			}
		})
	}
}

func TestBelowMinimumSavedTargetCannotBypassApplication(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	start, err := s.Start(context.Background(), "source", Request{})
	if err != nil {
		t.Fatal(err)
	}
	j := waitDone(t, s, start.ID)
	request, err := ApplyRequest(j)
	if err != nil {
		t.Fatal(err)
	}
	j.Plans[*j.SelectedPlan].Proposed.Conclusion = qualityScore(69)
	if err := s.save(&j); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRequest(j); err == nil {
		t.Fatal("low selected target applied")
	}
	if err := s.ValidateApplication(context.Background(), request); err == nil {
		t.Fatal("low selected target bypassed API validation")
	}
}

func TestRevisionHasOwnBoundedRepairButCannotRewriteRiskGroups(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	q := &qualityGateway{testGateway: g, repair: true}
	s.gateway = q
	j := fixtureJob()
	j.ID = "revision-repair"
	j.Stage = "proposing"
	j.RevisionCount = 1
	j.RevisionHistory = []RevisionRound{{RiskGroups: nil, Assessment: &Assessment{}}}
	j.Limitations = append(j.Limitations, "已使用一次模型格式/一致性修复")
	if err := s.model(context.Background(), &j, "invalid revision", func(string) error { return json.Unmarshal([]byte("invalid"), new(any)) }); err == nil || g.calls.Load() != 3 {
		t.Fatal("operation repair bound changed", err, g.calls.Load())
	}
	p := fixtureProposal()
	p.RiskGroups = []pi.RiskGroup{{Name: "rewritten"}}
	if validateProposal(j, p) == nil {
		t.Fatal("revision rewrote frozen risk groups")
	}
}

func TestAboveFloorStillRequiresIndependentAcceptance(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	q := &qualityGateway{testGateway: g, scores: []int{64, 75}, rejectFinal: true}
	s.gateway = q
	start, err := s.Start(context.Background(), "source", Request{})
	if err != nil {
		t.Fatal(err)
	}
	j := waitDone(t, s, start.ID)
	if j.Status != "succeeded" || j.SelectedPlan != nil || j.Outcome != "no_feasible_plan" || j.RevisionCount != 1 {
		t.Fatal("score used instead of investment acceptance", j.Status, j.Outcome, j.Error)
	}
	if strings.Contains(j.OutcomeReason, "最高目标评分为69") {
		t.Fatal("above-floor rejection misrepresented as low score")
	}
}
