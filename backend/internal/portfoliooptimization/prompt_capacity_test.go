package portfoliooptimization

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	pi "easy-stock/backend/internal/portfolioinspection"
)

func TestPromptCompressionTargetAllowsBoundedEvidenceFallback(t *testing.T) {
	for _, target := range []int{MaxInitialModelPromptBytes, MaxLargeInitialModelPromptBytes, MaxReviewModelPromptBytes, MaxRevisionModelPromptBytes} {
		t.Run(fmt.Sprint(target), func(t *testing.T) {
			calls := 0
			want := strings.Repeat("证", (target+1024)/3)
			got, err := boundedModelPromptWithLimit(target, func(detail int) (string, error) {
				calls++
				return want + strings.Repeat("字", detail), nil
			})
			if err != nil || got != want || calls != 5 {
				t.Fatal("compression target rejected required evidence or skipped compaction", len(got), calls, err)
			}
			if _, err := boundedModelPromptWithLimit(target, func(int) (string, error) {
				return strings.Repeat("x", MaxEvidenceModelPromptBytes+1), nil
			}); err == nil {
				t.Fatal("unbounded evidence bypassed request capacity")
			}
		})
	}
}

func TestOversizedProposalResumeCompletesReviewAndPersistsPlan(t *testing.T) {
	j := richCapacityBaseJob()
	p := fixtureProposal()
	// Two held stocks plus six fully researched candidates. Their rich evidence
	// pushes the initial request beyond the former 22 KiB stop condition.
	seed, _ := json.Marshal(j.Results[1])
	for n := 0; n < MaxCandidateResearch; n++ {
		var r pi.HoldingResult
		symbol := fmt.Sprintf("600%03d.SH", n+200)
		if err := json.Unmarshal([]byte(strings.ReplaceAll(string(seed), "000858.SZ", symbol)), &r); err != nil {
			t.Fatal(err)
		}
		r.Holding.Weight = 0
		j.Results = append(j.Results, r)
		j.Eligibility = append(j.Eligibility, Eligibility{Symbol: symbol, CanIncrease: true})
		j.Candidates = append(j.Candidates, Candidate{Symbol: symbol, Selected: true})
		a := p.Alternatives[0].Allocations[1]
		a.Symbol, a.Minimum, a.Maximum, a.Preferred = symbol, 0, 0, 0
		a.EvidenceRefs = []pi.EvidenceRef{{ReportID: r.AnalysisID, SourceID: "s1"}}
		p.Alternatives[0].Allocations = append(p.Alternatives[0].Allocations, a)
	}
	j.ID, j.Version, j.Status, j.Stage = "oversized-resume", Version, "incomplete", "proposing"
	j.Error, j.ResumeAvailable = "优化必要证据超过22 KiB本阶段初次输入上限", true
	j.LatestTradeDate, j.SnapshotAt = LatestSession(time.Now()), time.Now().UTC()
	j.ExecutionDurationMS = int64((14 * time.Minute) / time.Millisecond)
	prompt, err := proposalPrompt(j)
	if err != nil || len(prompt) <= MaxInitialModelPromptBytes {
		t.Fatal("fixture must reproduce old size failure", len(prompt), err)
	}
	g := &checkpointGateway{testGateway: &testGateway{}, outputs: []string{jsonText(t, p)}}
	s, _, research := setupService(t, g.testGateway)
	s.gateway = g
	if err := s.save(&j); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resume(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	done := waitDone(t, s, j.ID)
	if done.Status != "succeeded" || done.Stage != "completed" || done.SelectedPlan == nil || done.ResumeAvailable || done.Error != "" {
		t.Fatal("optimization did not finish with a reviewed plan", done.Status, done.Outcome, done.Error)
	}
	if research.Load() != 0 || len(g.prompts) != 2 || done.ExecutionDurationMS < j.ExecutionDurationMS {
		t.Fatal("resume discarded research/budgets or skipped a stage", research.Load(), len(g.prompts))
	}
	if done.Plans[*done.SelectedPlan].Assessment == nil || len(done.ModelAttempts) != 2 {
		t.Fatal("finished without independent review and saved diagnostics")
	}
	for _, prompt := range g.prompts {
		if len(prompt) > MaxModelPromptBytes || !strings.Contains(prompt, "news_columns") || !strings.Contains(prompt, "earnings_disclosure") {
			t.Fatal("request lost news/disclosure evidence or exceeded capacity")
		}
	}
}
