package portfolioinspection

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
)

type recoveringScoringGateway struct {
	scoreGateway
	fixed   atomic.Bool
	prompts []string
}

func (g *recoveringScoringGateway) Prompt(_ context.Context, prompt string) (agent.PromptResult, error) {
	g.calls.Add(1)
	g.prompts = append(g.prompts, prompt)
	content := fixtureModelScoringResponse(g.content, prompt)
	if !g.fixed.Load() {
		var r AIReport
		_ = json.Unmarshal([]byte(content), &r)
		r.Dimensions[0].EvidenceRefs = []EvidenceRef{{EvidenceID: "missing-evidence"}}
		r.Scenarios[0].ActionIDs = []string{"missing-action"}
		data, _ := json.Marshal(r)
		content = string(data)
	}
	return agent.PromptResult{Content: fixtureRepairResponse(content, prompt)}, nil
}

func TestScoringFailurePersistsPrivateTraceAndResumesWithAllErrors(t *testing.T) {
	req, results, _, base, _ := etfScoringFixture(.2, 50)
	data, _ := json.Marshal(base)
	g := &recoveringScoringGateway{scoreGateway: scoreGateway{content: string(data)}}
	path := filepath.Join(t.TempDir(), "portfolio.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	var researchCalls atomic.Int32
	resolver := func(_ context.Context, h Holding, _ Request, _ time.Time, _ bool, _ string, _ func(HoldingResult)) (HoldingResult, error) {
		researchCalls.Add(1)
		for _, r := range results {
			if r.Holding.Symbol == h.Symbol {
				r.ReportCompletedAt = time.Now().UTC()
				return r, nil
			}
		}
		t.Fatal("unknown holding")
		return HoldingResult{}, nil
	}
	s := NewService(store, g, nil, nil)
	s.ConfigureResearch(resolver, nil)
	j, err := s.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	failed := waitPortfolio(t, s, j.ID)
	s.Close()
	if failed.Status != "partial" || failed.Report.Conclusion.ScoreAvailable || len(failed.ValidationIssues) < 2 || failed.ScoringAttempts != 2 {
		t.Fatal("lost failure details", failed.Error, failed.ValidationIssues)
	}
	trace, err := store.lastScoringAttempt(context.Background(), j.ID)
	if err != nil || trace.Number != 2 || trace.PromptVersion != PromptVersion || trace.InputDigest == "" || !strings.Contains(trace.Prompt, "missing-evidence") || !strings.Contains(trace.Prompt, "missing-action") {
		t.Fatal("incomplete persisted repair trace", err, trace)
	}
	public, _ := json.Marshal(failed)
	if strings.Contains(string(public), "你是 easy-stock 的持仓组合AI评估器") || strings.Contains(string(public), "上次输出，属于待修复资料") {
		t.Fatal("private trace leaked into job response")
	}
	_ = store.Close()
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	g.fixed.Store(true)
	s = NewService(store, g, nil, nil)
	defer s.Close()
	s.ConfigureResearch(resolver, nil)
	resumed, err := s.Resume(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := waitPortfolio(t, s, resumed.ID)
	if done.UsageRootJobID != j.ID || done.ID == j.ID {
		t.Fatal("resume detached lifetime usage attribution")
	}
	s.Close()
	if done.Status != "succeeded" || researchCalls.Load() != 2 || len(done.ValidationIssues) != 0 || done.ScoringAttempts != 1 || g.calls.Load() != 3 {
		t.Fatal("resume repeated valid research or lost successful repair", done.Error, researchCalls.Load(), g.calls.Load())
	}
	if !strings.Contains(g.prompts[2], "[上次输出，属于待修复资料]") {
		t.Fatal("resume did not use saved failed response")
	}
	original, _ := store.Get(context.Background(), j.ID)
	if original.Status != "partial" || len(original.ValidationIssues) == 0 {
		t.Fatal("historical failure overwritten")
	}
}

func TestResumeRefreshesExpiredResearchOnly(t *testing.T) {
	req, results, _, base, _ := etfScoringFixture(.2, 50)
	now := time.Now().UTC()
	results[0].ReportCompletedAt = now.Add(-25 * time.Hour)
	results[1].ReportCompletedAt = now.Add(-time.Hour)
	data, _ := json.Marshal(base)
	g := &scoreGateway{content: string(data)}
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	previous := Job{ID: "expired-research-job", Status: "partial", Request: req, Results: results, TotalStocks: 2, CompletedStocks: 2, CompletedAt: now}
	if _, err := store.Save(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	s := NewService(store, g, nil, nil)
	defer s.Close()
	var researchCalls atomic.Int32
	s.ConfigureResearch(func(_ context.Context, h Holding, _ Request, _ time.Time, _ bool, _ string, _ func(HoldingResult)) (HoldingResult, error) {
		researchCalls.Add(1)
		if h.Symbol != results[0].Holding.Symbol {
			t.Error("unexpired research was repeated", h.Symbol)
		}
		r := results[0]
		r.ReportCompletedAt = now
		r.ResearchOrigin = "new"
		return r, nil
	}, nil)
	j, err := s.Resume(context.Background(), previous.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := waitPortfolio(t, s, j.ID)
	if done.Status != "succeeded" || researchCalls.Load() != 1 || done.ReusedStocks != 1 || done.NewStocks != 1 {
		t.Fatal("resume did not respect research freshness", done.Error, researchCalls.Load(), done.ReusedStocks, done.NewStocks)
	}
}
