package portfolioinspection

import (
	"context"
	"path/filepath"
	"strings"
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
	for _, changedModel := range []bool{false, true} {
		t.Run(map[bool]string{false: "same_configuration", true: "changed_configuration"}[changedModel], func(t *testing.T) {
			req, results, _, _, _ := oversizedResearchFixture(1)
			results[0].Analysis.ResearchReport.Counter[0].Text = strings.Repeat("必须检查风险。", 20000)
			store, err := OpenStore("")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			binding, update := aggregationTestAgent(t, 300, "medium")
			g := &boundCompactGateway{compactTestGateway: &compactTestGateway{scoreGateway: &scoreGateway{content: scoreContent(t, scoreJSON(t))}}, binding: binding}
			g.fail.Store(true) // Fail scoring after successful persisted compaction.
			s := NewService(store, g, nil, nil)
			defer s.Close()
			researchCalls := 0
			s.ConfigureResearch(func(context.Context, Holding, Request, time.Time, bool, string, func(HoldingResult)) (HoldingResult, error) {
				researchCalls++
				return results[0], nil
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
			if changedModel {
				update(300, "high")
			}
			g.fail.Store(false)
			next, err := s.Resume(context.Background(), job.ID)
			if err != nil {
				t.Fatal(err)
			}
			s.wg.Wait()
			done, err = s.Get(context.Background(), next.ID)
			if err != nil || done.Status != "succeeded" || researchCalls != 1 {
				t.Fatalf("resume failed or repeated research: %s %s %v", done.Status, done.Error, err)
			}
			after := countCompaction()
			if changedModel && after <= before || !changedModel && after != before {
				t.Fatalf("wrong checkpoint/model reuse: before=%d after=%d", before, after)
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
