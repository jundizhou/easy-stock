package stockanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResearchCheckpointResumesAfterReopenWithoutRepeatingEvidenceOrCore(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	path := filepath.Join(t.TempDir(), "research.db")
	store, err := OpenResearchStore(path)
	if err != nil {
		t.Fatal(err)
	}
	modelCalls, supplements := 0, 0
	value, _ := json.Marshal(validResearch())
	prompter := &researchTestPrompter{respond: func(_ int, prompt string) (string, error) {
		modelCalls++
		if strings.Contains(prompt, "任务是独立提出") {
			return `{"questions":[{"question":"核实披露","why":"影响判断","tool":"announcements","query":"披露"}]}`, nil
		}
		if strings.HasPrefix(prompt, "你是A股交易条件整理器") && modelCalls == 3 {
			return "", errors.New("connection interrupted")
		}
		return string(value), nil
	}}
	runner := func(ctx context.Context, request ResearchRequest, publish ResearchPublisher) (Analysis, *ResearchSnapshot, error) {
		a, s := analysis, snapshot
		if previous := ResearchResumeData(ctx); previous != nil {
			a, s = *previous.Analysis, *previous.Snapshot
		}
		if err := publish("baseline", "quantitative", &a, &s); err != nil {
			return a, &s, err
		}
		ctx = WithResearchModelIdentity(ctx, "frozen-model-high")
		err := RunResearch(ctx, prompter, &s, &a, request, "fixture", func(context.Context, ResearchSnapshot, ResearchQuestion) ([]ResearchSource, error) {
			supplements++
			return []ResearchSource{NewResearchSource("announcement", "披露", "披露内容", "fixture", "", s.CutoffAt, s.CutoffAt)}, nil
		}, nil)
		return a, &s, err
	}
	service := NewResearchService(store, runner)
	first, err := service.Start(context.Background(), ResearchRequest{Symbol: analysis.Symbol, AnalysisLevel: ResearchLevelDeep})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, err = service.Wait(ctx, first.ID)
	if err != nil || first.Status != "failed" || !first.Public().ResumeAvailable || first.Public().Checkpoint != nil || first.Analysis.ResearchReport != nil || modelCalls != 3 {
		t.Fatalf("failed state incorrect: %+v %v", first, err)
	}
	service.Close()
	store.Close()
	store, err = OpenResearchStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = NewResearchService(store, runner)
	defer service.Close()
	second, err := service.Resume(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err = service.Wait(ctx, second.ID)
	if err != nil || second.Status != "succeeded" || second.ResumedFrom != first.ID || second.ID == first.ID || modelCalls != 4 || supplements != 1 || second.Analysis.ResearchReport == nil {
		t.Fatalf("resume reran completed work: status=%s calls=%d supplements=%d err=%v", second.Status, modelCalls, supplements, err)
	}
	if second.Snapshot.ID != first.Snapshot.ID || !second.Snapshot.CutoffAt.Equal(first.Snapshot.CutoffAt) || len(second.Analysis.ResearchReport.Attempts) != 4 {
		t.Fatal("resume changed evidence time or lost attempts")
	}
	previous, _ := store.Get(ctx, first.ID)
	if previous.Status != "failed" {
		t.Fatal("original task overwritten")
	}
	if err := store.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	var versions int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM stock_research_snapshots WHERE id=?`, second.Snapshot.ID).Scan(&versions); err != nil || versions == 0 {
		t.Fatalf("deleting the original removed the resumed task's evidence: %d %v", versions, err)
	}
	if err := store.Delete(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM stock_research_snapshots WHERE id=?`, second.Snapshot.ID).Scan(&versions); err != nil || versions != 0 {
		t.Fatalf("unreferenced evidence not cleaned up: %d %v", versions, err)
	}
}

func TestResearchCheckpointRejectsChangedModelRequestAndSnapshot(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	request, _ := NormalizeResearchRequest(ResearchRequest{Symbol: analysis.Symbol})
	cp := &ResearchCheckpoint{PromptVersion: ResearchPromptVersion, CompressionVersion: ResearchCompressionVersion, ModelIdentity: "model-high", Request: request, SnapshotHash: researchHash(&snapshot), Outline: &ResearchOutline{}}
	for _, kind := range []string{"model", "request", "snapshot", "version"} {
		t.Run(kind, func(t *testing.T) {
			copyCP, copySnapshot, copyRequest, identity := *cp, snapshot, request, "model-high"
			switch kind {
			case "model":
				identity = "model-max"
			case "request":
				copyRequest.Horizon = "short"
			case "snapshot":
				copySnapshot.Quote.Price++
			case "version":
				copyCP.PromptVersion = "old"
			}
			ctx := context.WithValue(context.Background(), researchExecutionKey{}, &researchExecution{checkpoint: &copyCP, identity: identity})
			p := &researchTestPrompter{respond: func(int, string) (string, error) { t.Fatal("mismatched checkpoint reached model"); return "", nil }}
			if err := RunResearch(ctx, p, &copySnapshot, &analysis, copyRequest, "fixture", nil, nil); err == nil {
				t.Fatal("mixed incompatible checkpoint")
			}
		})
	}
}

func TestResearchCheckpointSaveFailureStopsBeforeModel(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	ctx := context.WithValue(context.Background(), researchExecutionKey{}, &researchExecution{save: func(*ResearchCheckpoint) error { return errors.New("disk full") }})
	p := &researchTestPrompter{}
	if err := RunResearch(ctx, p, &snapshot, &analysis, ResearchRequest{}, "fixture", nil, nil); err == nil || p.calls != 0 {
		t.Fatal("persistence failure ignored")
	}
}
