package portfolioinspection

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
)

func TestCompactionCheckpointSurvivesRestartAndInvalidatesChangedInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portfolio.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	card := PortfolioAssetCard{Symbol: "600519.SH", ReportID: "report1"}
	value := map[string]any{"counter": "现金流恶化", "source_ids": []string{"s1"}}
	run := func(identity string, value any, wantCalls int) {
		t.Helper()
		g := &compactTestGateway{scoreGateway: &scoreGateway{}}
		s := NewService(nil, g, nil, nil)
		defer s.Close()
		r := &compactionRunner{service: s, ctx: context.Background(), budget: aggregationBudgetFor(time.Minute, ""), hooks: store.compactionHooks("job1", nil), identity: identity}
		if _, err := r.summarize(card, value); err != nil {
			t.Fatal(err)
		}
		if len(g.prompts) != wantCalls {
			t.Fatalf("unexpected checkpoint reuse: %d vs %d", len(g.prompts), wantCalls)
		}
	}
	run("model-a", value, 1)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run("model-a", value, 0)
	run("model-b", value, 1)
	run("model-a", map[string]any{"counter": "现金流改善", "source_ids": []string{"s1"}}, 1)
	// Unknown bindings never reuse potentially foreign model output.
	run("", value, 1)
	run("", value, 1)
	if _, err := store.db.Exec(`UPDATE portfolio_compaction_checkpoints SET summary=''`); err != nil {
		t.Fatal(err)
	}
	run("model-a", value, 1)
}

func TestCompactionCheckpointRespectsChangedSummaryBudget(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	g := &compactTestGateway{scoreGateway: &scoreGateway{}, failure: "budget_summaries"}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	card := PortfolioAssetCard{Symbol: "600519.SH", ReportID: "report1"}
	value := map[string]any{"counter": "风险须结合来源核验", "source_ids": []string{"s1"}}
	for _, tc := range []struct{ limit, calls int }{{6144, 1}, {1024, 2}, {1024, 2}, {6144, 2}} {
		r := &compactionRunner{service: s, ctx: context.Background(), budget: aggregationBudgetFor(time.Minute, ""), hooks: store.compactionHooks("job1", nil), identity: "model-a", summaryBytes: tc.limit}
		summary, err := r.summarize(card, value)
		if err != nil || !validCompactionSummaryWithin(summary, tc.limit) || len(g.prompts) != tc.calls {
			t.Fatalf("wrong summary budget/checkpoint reuse: limit=%d calls=%d want=%d err=%v", tc.limit, len(g.prompts), tc.calls, err)
		}
	}
}

func TestCopiedCompactionCheckpointsAreDeletedOnlyWithTheirOwnJob(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, id := range []string{"old", "resumed"} {
		if _, err := store.Save(ctx, Job{ID: id, Status: "partial"}); err != nil {
			t.Fatal(err)
		}
	}
	hooks := store.compactionHooks("old", nil)
	if err := hooks.save(ctx, "digest", "已完成的摘要"); err != nil {
		t.Fatal(err)
	}
	if err := store.copyCompactionCheckpoints(ctx, "old", "resumed"); err != nil {
		t.Fatal(err)
	}
	if err := store.deleteJob(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := hooks.load(ctx, "digest"); err != nil || ok {
		t.Fatal("deleted job kept private summaries", err)
	}
	if summary, ok, err := store.compactionHooks("resumed", nil).load(ctx, "digest"); err != nil || !ok || summary != "已完成的摘要" {
		t.Fatal("deletion damaged resumed job", err)
	}
}

type boundCompactGateway struct {
	*compactTestGateway
	binding *agent.Service
}

func (g *boundCompactGateway) BindTask(ctx context.Context) (context.Context, func(), error) {
	return g.binding.BindTask(ctx)
}

func TestInspectionResumeReusesCompletedCompaction(t *testing.T) {
	for _, tc := range []struct {
		holdings     int
		changedModel bool
	}{{1, false}, {1, true}, {10, false}, {10, true}} {
		t.Run(fmt.Sprintf("holdings_%d_changed_model_%v", tc.holdings, tc.changedModel), func(t *testing.T) {
			req, results, _, _, _ := sourceRichResearchFixture(tc.holdings)
			if tc.holdings == 1 {
				results[0].Analysis.ResearchReport.Counter[0].Text = strings.Repeat("必须检查风险。", 20000)
			}
			_, _, _, answer := scoreFixture()
			conclusion := answer.Holdings[0]
			answer.Holdings = nil
			for _, result := range results {
				conclusion.Symbol = result.Holding.Symbol
				answer.Holdings = append(answer.Holdings, conclusion)
			}
			content, err := json.Marshal(answer)
			if err != nil {
				t.Fatal(err)
			}
			store, err := OpenStore("")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			binding, update := aggregationTestAgent(t, 300, "medium")
			g := &boundCompactGateway{compactTestGateway: &compactTestGateway{scoreGateway: &scoreGateway{content: string(content)}, failure: "budget_summaries"}, binding: binding}
			g.fail.Store(true) // Fail scoring after successful persisted compaction.
			s := NewService(store, g, nil, nil)
			defer s.Close()
			var researchCalls atomic.Int32
			s.ConfigureResearch(func(_ context.Context, holding Holding, _ Request, _ time.Time, _ bool, _ string, _ func(HoldingResult)) (HoldingResult, error) {
				researchCalls.Add(1)
				for _, result := range results {
					if result.Holding.Symbol == holding.Symbol {
						return result, nil
					}
				}
				return HoldingResult{}, fmt.Errorf("unexpected test holding: %s", holding.Symbol)
			}, nil)
			job, err := s.Start(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			s.wg.Wait()
			done, err := s.Get(context.Background(), job.ID)
			if err != nil || done.Status != "partial" {
				t.Fatalf("expected recoverable scoring failure: %+v %v", done, err)
			}
			countCompaction := func() int {
				n := 0
				for _, p := range g.prompts {
					if strings.Contains(p, "[研究片段JSON]") {
						n++
					}
				}
				return n
			}
			before := countCompaction()
			if before == 0 {
				t.Fatal("fixture did not compact")
			}
			if tc.changedModel {
				update(300, "high")
			}
			g.fail.Store(false)
			next, err := s.Resume(context.Background(), job.ID)
			if err != nil {
				t.Fatal(err)
			}
			s.wg.Wait()
			done, err = s.Get(context.Background(), next.ID)
			if err != nil || done.Status != "succeeded" || researchCalls.Load() != int32(tc.holdings) || done.Report == nil || !done.Report.Conclusion.ScoreAvailable || len(done.Report.Conclusion.Holdings) != tc.holdings {
				t.Fatalf("resume failed or repeated research: %s %s research_calls=%d %v", done.Status, done.Error, researchCalls.Load(), err)
			}
			if !strings.Contains(strings.Join(done.Report.Conclusion.DataLimitations, " "), compactionNotice) {
				t.Fatal("resumed score lost its compaction limitation")
			}
			after := countCompaction()
			if tc.changedModel && after <= before || !tc.changedModel && after != before {
				t.Fatalf("wrong checkpoint/model reuse: before=%d after=%d", before, after)
			}
			for _, prompt := range g.prompts {
				if len(prompt) > maxScoringPromptBytes {
					t.Fatalf("scoring or resume prompt exceeded input limit: %d", len(prompt))
				}
			}
		})
	}
}

type cancelAfterSummaryGateway struct {
	*compactTestGateway
	cancel context.CancelFunc
}

func (g *cancelAfterSummaryGateway) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	result, err := g.compactTestGateway.Prompt(ctx, prompt)
	g.cancel()
	return result, err
}

func TestCancelledCompactionPreservesCompletedChunkForResume(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	card := PortfolioAssetCard{Symbol: "600519.SH", ReportID: "report1"}
	value := map[string]any{"counter": strings.Repeat("经营风险。", 15000)}
	chunks, err := compactionChunks(card, value, false)
	if err != nil || len(chunks) < 2 {
		t.Fatal("bad multi-chunk fixture", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := &cancelAfterSummaryGateway{compactTestGateway: &compactTestGateway{scoreGateway: &scoreGateway{}}, cancel: cancel}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	r := &compactionRunner{service: s, ctx: ctx, budget: aggregationBudgetFor(time.Minute, ""), identity: "same-model", hooks: store.compactionHooks("old", nil)}
	if _, err := r.summarize(card, value); err != context.Canceled {
		t.Fatalf("compaction ignored cancellation: %v", err)
	}
	if len(g.prompts) != 1 {
		t.Fatal("cancelled run started another model call")
	}
	if err := store.copyCompactionCheckpoints(context.Background(), "old", "resumed"); err != nil {
		t.Fatal(err)
	}
	nextGateway := &compactTestGateway{scoreGateway: &scoreGateway{}}
	next := NewService(nil, nextGateway, nil, nil)
	defer next.Close()
	r = &compactionRunner{service: next, ctx: context.Background(), budget: aggregationBudgetFor(time.Minute, ""), identity: "same-model", hooks: store.compactionHooks("resumed", nil)}
	if _, err := r.summarize(card, value); err != nil {
		t.Fatal(err)
	}
	if r.reused != 1 || len(nextGateway.prompts) != len(chunks)-1 {
		t.Fatalf("resume repeated completed chunk: reused=%d calls=%d chunks=%d", r.reused, len(nextGateway.prompts), len(chunks))
	}
}
