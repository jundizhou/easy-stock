package stockanalysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResearchStageDecoderRejectsPartialObjectsAndFindsCompleteWrappedCore(t *testing.T) {
	_, snapshot := researchFixture(t)
	valid, _ := json.Marshal(validResearch())
	for _, tc := range []struct {
		name, content string
		valid         bool
	}{
		{"only-grade", `{"evidence_level":"limited"}`, false},
		{"nested-grade", `{"metadata":{"evidence_level":"limited"}}`, false},
		{"malformed-main-with-nested-grade", `{"headline":["wrong"],"metadata":{"evidence_level":"limited"}}`, false},
		{"complete-wrapped-core", `{"evidence_level":"limited","result":` + string(valid) + `}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core := ResearchCoreSynthesis{Headline: "original"}
			err := decodeJSONObjectWithValidation(tc.content, &core, func(value any) error {
				return validateResearchStage(value, snapshot, "核心判断")
			})
			if tc.valid {
				if err != nil || core.Headline != validResearch().Headline || core.Thesis.Text == "" {
					t.Fatalf("complete core was not selected: %+v, %v", core, err)
				}
			} else if err == nil || core.Headline != "original" || core.EvidenceLevel != "" {
				t.Fatalf("partial object was accepted or changed the target: %+v, %v", core, err)
			}
		})
	}
}

func TestResearchCoreStructureFailureRepairsBeforeGeneratingTrade(t *testing.T) {
	unknown := validResearch()
	unknown.Thesis.SourceIDs = []string{"invented"}
	unknownJSON, _ := json.Marshal(unknown)
	for _, level := range []ResearchLevel{ResearchLevelStandard, ResearchLevelDeep} {
		for _, tc := range []struct{ name, content string }{
			{"only-grade", `{"evidence_level":"limited"}`},
			{"missing-thesis", `{"headline":"missing thesis"}`},
			{"unknown-source", string(unknownJSON)},
		} {
			t.Run(string(level)+"/"+tc.name, func(t *testing.T) {
				invalid := tc.content
				analysis, snapshot := researchFixture(t)
				valid, _ := json.Marshal(validResearch())
				execution := &researchExecution{}
				ctx := context.WithValue(context.Background(), researchExecutionKey{}, execution)
				coreCall := 1
				if level == ResearchLevelDeep {
					coreCall = 2
				}
				p := &researchTestPrompter{respond: func(call int, prompt string) (string, error) {
					switch call {
					case coreCall:
						return invalid, nil
					case coreCall + 1:
						if !strings.Contains(prompt, "[结构修复要求]") || !strings.Contains(prompt, invalid) {
							t.Fatal("invalid core was sent to trade instead of repair")
						}
						cp := execution.checkpoint
						if cp.Failures["核心判断"].Content != invalid || len(cp.Outputs["核心判断"].Value) != 0 {
							t.Fatal("incomplete core was cached or original output was lost")
						}
						return string(valid), nil
					case coreCall + 2:
						if !strings.HasPrefix(prompt, "你是A股交易条件整理器") || !strings.Contains(prompt, validResearch().Headline) {
							t.Fatal("trade did not receive the repaired core")
						}
						return string(valid), nil
					default:
						if level == ResearchLevelDeep && call == 1 {
							return `{"questions":[]}`, nil
						}
						t.Fatal("exceeded the shared repair budget")
						return "", nil
					}
				}}
				request := ResearchRequest{Symbol: analysis.Symbol, Horizon: "swing", AnalysisLevel: level}
				if err := RunResearch(ctx, p, &snapshot, &analysis, request, "fixture", nil, nil); err != nil {
					t.Fatal(err)
				}
				if p.calls != coreCall+2 || analysis.ResearchReport == nil || !execution.checkpoint.RepairUsed || len(execution.checkpoint.Failures) != 0 {
					t.Fatal("repair was not completed or checkpointed")
				}
				if execution.checkpoint.Attempts[coreCall-1].Error == "" {
					t.Fatal("structural failure was not recorded in the attempt")
				}
			})
		}
	}
}

func TestStandardFinalValidationUsesOnlyRemainingSharedRepair(t *testing.T) {
	for _, coreRepair := range []bool{false, true} {
		name := "repair-available"
		if coreRepair {
			name = "repair-already-used"
		}
		t.Run(name, func(t *testing.T) {
			analysis, snapshot := researchFixture(t)
			valid, _ := json.Marshal(validResearch())
			bad := validResearch()
			bad.Conditions[0].AnchorID = "invented-anchor"
			invalidTrade, _ := json.Marshal(bad)
			p := &researchTestPrompter{respond: func(call int, prompt string) (string, error) {
				if call > 3 {
					t.Fatal("more than one shared repair was used")
				}
				if coreRepair {
					if call == 1 {
						return `{"evidence_level":"limited"}`, nil
					}
					if call == 3 {
						return string(invalidTrade), nil
					}
				} else {
					if call == 2 {
						return string(invalidTrade), nil
					}
					if call == 3 && (!strings.Contains(prompt, "[结构修复要求]") || !strings.Contains(prompt, "未知anchor_id") || !strings.Contains(prompt, "[上次拆分结果") || !strings.Contains(prompt, validResearch().Headline)) {
						t.Fatal("final validation error was not sent to repair")
					}
				}
				return string(valid), nil
			}}
			err := RunResearch(context.Background(), p, &snapshot, &analysis, ResearchRequest{Symbol: analysis.Symbol, Horizon: "swing", AnalysisLevel: ResearchLevelStandard}, "fixture", nil, nil)
			if p.calls != 3 || (coreRepair && (err == nil || analysis.ResearchReport != nil)) || (!coreRepair && (err != nil || analysis.ResearchReport == nil)) {
				t.Fatalf("invalid repair accounting: calls=%d, err=%v", p.calls, err)
			}
		})
	}
}

func TestStageValidationPreservesConservativeModelJudgments(t *testing.T) {
	for _, evidence := range []string{"limited", "insufficient"} {
		t.Run(evidence, func(t *testing.T) {
			analysis, snapshot := researchFixture(t)
			result := validResearch()
			result.EvidenceLevel = evidence
			result.Decision.Status, result.Decision.PricePlan = "no_plan", nil
			encoded, _ := json.Marshal(result)
			p := &researchTestPrompter{respond: func(int, string) (string, error) { return string(encoded), nil }}
			if err := RunResearch(context.Background(), p, &snapshot, &analysis, ResearchRequest{Symbol: analysis.Symbol, Horizon: "swing", AnalysisLevel: ResearchLevelStandard}, "fixture", nil, nil); err != nil {
				t.Fatal(err)
			}
			if p.calls != 2 || analysis.ResearchReport.EvidenceLevel != evidence || analysis.ResearchReport.Decision.Status != "no_plan" {
				t.Fatal("conservative research was unnecessarily repaired or upgraded")
			}
		})
	}
}

func TestInvalidCachedCoreDoesNotResetUsedRepairBudget(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	request := ResearchRequest{Symbol: analysis.Symbol, Horizon: "swing", AnalysisLevel: ResearchLevelStandard}
	prompt := researchCorePromptWithPack(snapshot, request, ResearchOutline{}, buildResearchCoreEvidencePack(snapshot, request, ResearchOutline{}))
	cp := &ResearchCheckpoint{PromptVersion: ResearchPromptVersion, CompressionVersion: ResearchCompressionVersion, ModelIdentity: "fixture", Request: request, SnapshotHash: researchHash(&snapshot), RepairUsed: true, Outputs: map[string]ResearchStageOutput{}}
	cacheResearchValue(cp, "核心判断", prompt, ResearchCoreSynthesis{EvidenceLevel: "limited"})
	p := &researchTestPrompter{respond: func(int, string) (string, error) { t.Fatal("resume reset the repair budget"); return "", nil }}
	ctx := context.WithValue(context.Background(), researchExecutionKey{}, &researchExecution{checkpoint: cp})
	err := RunResearch(ctx, p, &snapshot, &analysis, request, "fixture", nil, nil)
	if !isInvalidModelJSON(err) || p.calls != 0 || !cp.RepairUsed || len(cp.Outputs) != 0 || analysis.ResearchReport != nil {
		t.Fatalf("invalid cache was reused or repair budget reset: %v", err)
	}
}

// Optional replay uses a read-only saved checkpoint and mock model responses.
// It neither calls a model/provider nor persists the simulated report.
func TestSavedMalformedCoreCheckpointRecovery(t *testing.T) {
	path, id := os.Getenv("EASY_STOCK_FAILED_RESEARCH_DB"), os.Getenv("EASY_STOCK_FAILED_RESEARCH_ID")
	if path == "" || id == "" {
		t.Skip("read-only replay requires EASY_STOCK_FAILED_RESEARCH_DB and EASY_STOCK_FAILED_RESEARCH_ID")
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw string
	if err := db.QueryRow("SELECT content_json FROM stock_research_jobs WHERE id=?", id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var job ResearchJob
	if err := json.Unmarshal([]byte(raw), &job); err != nil || job.Snapshot == nil || job.Analysis == nil || job.Checkpoint == nil || job.Checkpoint.RepairUsed {
		t.Fatalf("not the expected recoverable checkpoint: %v", err)
	}
	analysis := QuantitativeOnly(*job.Analysis)
	result := validResearch()
	result.Decision.Status, result.Decision.PricePlan = "observe", nil
	valid, _ := json.Marshal(result)
	p := &researchTestPrompter{respond: func(call int, prompt string) (string, error) {
		if call == 1 {
			if !strings.Contains(prompt, "已保存核心判断无效") || !strings.Contains(prompt, "缺少headline") || !strings.Contains(prompt, "[结构修复要求]") {
				t.Fatal("saved empty core did not trigger repair")
			}
		} else if call != 2 || !strings.HasPrefix(prompt, "你是A股交易条件整理器") {
			t.Fatal("dependent trade was reused or model-call budget exceeded")
		}
		return string(valid), nil
	}}
	execution := &researchExecution{checkpoint: job.Checkpoint, identity: job.Checkpoint.ModelIdentity}
	ctx := context.WithValue(context.Background(), researchExecutionKey{}, execution)
	if err := RunResearch(ctx, p, job.Snapshot, &analysis, job.Request, "mock-model", nil, nil); err != nil {
		t.Fatal(err)
	}
	if p.calls != 2 || analysis.ResearchReport == nil || analysis.ResearchReport.EvidenceLevel != "limited" {
		t.Fatal("saved checkpoint failed mock recovery")
	}
	var after string
	if err := db.QueryRow("SELECT content_json FROM stock_research_jobs WHERE id=?", id).Scan(&after); err != nil || after != raw {
		t.Fatal("saved history changed during read-only replay")
	}
	t.Log("saved empty core rejected; one mock core repair and one new mock trade call; database unchanged")
}

func TestInvalidCoreRepairStopsWithoutTradeOrAISuccess(t *testing.T) {
	for _, level := range []ResearchLevel{ResearchLevelStandard, ResearchLevelDeep} {
		t.Run(string(level), func(t *testing.T) {
			analysis, snapshot := researchFixture(t)
			before := analysis.Conclusion
			execution := &researchExecution{}
			ctx := context.WithValue(context.Background(), researchExecutionKey{}, execution)
			p := &researchTestPrompter{respond: func(call int, prompt string) (string, error) {
				if strings.HasPrefix(prompt, "你是A股交易条件整理器") {
					t.Fatal("empty core reached the trade stage")
				}
				if level == ResearchLevelDeep && call == 1 {
					return `{"questions":[]}`, nil
				}
				return `{"evidence_level":"limited"}`, nil
			}}
			err := RunResearch(ctx, p, &snapshot, &analysis, ResearchRequest{Symbol: analysis.Symbol, Horizon: "swing", AnalysisLevel: level}, "fixture", nil, nil)
			wantCalls := 2
			if level == ResearchLevelDeep {
				wantCalls = 3
			}
			if !isInvalidModelJSON(err) || !strings.Contains(err.Error(), "缺少headline") || p.calls != wantCalls || analysis.ResearchReport != nil || analysis.Conclusion != before {
				t.Fatalf("failed repair exceeded budget or became success: calls=%d, err=%v", p.calls, err)
			}
			if len(execution.checkpoint.Outputs["核心判断"].Value) > 0 || len(execution.checkpoint.Outputs["核心判断修复"].Value) > 0 {
				t.Fatal("failed core was cached as a successful stage")
			}
		})
	}
}

func TestResumeInvalidCoreRebuildsDependentTradeAndPreservesOriginalJob(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	request, _ := NormalizeResearchRequest(ResearchRequest{Symbol: analysis.Symbol, Purpose: "holding", Horizon: "swing", AnalysisLevel: ResearchLevelStandard})
	outline := ResearchOutline{}
	core := ResearchCoreSynthesis{EvidenceLevel: "limited"}
	corePrompt := researchCorePromptWithPack(snapshot, request, outline, buildResearchCoreEvidencePack(snapshot, request, outline))
	tradePrompt := researchTradePromptWithPack(snapshot, request, outline, buildResearchTradeEvidencePack(snapshot, request, outline, core), core)
	cp := &ResearchCheckpoint{PromptVersion: ResearchPromptVersion, CompressionVersion: ResearchCompressionVersion, ModelIdentity: "fixture", Request: request, SnapshotHash: researchHash(&snapshot), Outputs: map[string]ResearchStageOutput{}}
	cacheResearchValue(cp, "核心判断", corePrompt, core)
	cacheResearchValue(cp, "交易条件", tradePrompt, ResearchTradeConditions{Decision: ResearchDecision{Status: "no_plan", NewPosition: "old", ExistingPosition: "old", Reason: "核心判断为空"}})
	job := ResearchJob{ID: "legacy-empty-core", Request: request, Status: "degraded", Analysis: &analysis, Snapshot: &snapshot, Checkpoint: cp, StartedAt: time.Now().UTC()}
	store, err := OpenResearchStore(filepath.Join(t.TempDir(), "research.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Save(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	valid, _ := json.Marshal(validResearch())
	p := &researchTestPrompter{respond: func(call int, prompt string) (string, error) {
		if call == 1 {
			if !strings.Contains(prompt, "已保存核心判断无效") || !strings.Contains(prompt, "缺少headline") || !strings.Contains(prompt, "[结构修复要求]") {
				t.Fatal("resume reused the empty core or omitted its repair reason")
			}
		} else if call != 2 || !strings.HasPrefix(prompt, "你是A股交易条件整理器") || strings.Contains(prompt, "核心判断为空") {
			t.Fatal("resume reused the dependent trade or exceeded the repair budget")
		}
		return string(valid), nil
	}}
	runner := func(ctx context.Context, request ResearchRequest, publish ResearchPublisher) (Analysis, *ResearchSnapshot, error) {
		previous := ResearchResumeData(ctx)
		a, s := QuantitativeOnly(*previous.Analysis), *previous.Snapshot
		if err := publish("baseline", "quantitative", &a, &s); err != nil {
			return a, &s, err
		}
		err := RunResearch(ctx, p, &s, &a, request, "fixture", nil, nil)
		return a, &s, err
	}
	service := NewResearchService(store, runner)
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resumed, err := service.Resume(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err = service.Wait(ctx, resumed.ID)
	if err != nil || resumed.Status != "succeeded" || p.calls != 2 || resumed.Analysis.ResearchReport == nil || resumed.Analysis.ResearchReport.EvidenceLevel != "limited" {
		t.Fatalf("resume did not recover conservatively: status=%s, calls=%d, err=%v", resumed.Status, p.calls, err)
	}
	if resumed.ResumedFrom != job.ID || resumed.Snapshot.ID != snapshot.ID || !resumed.Snapshot.CutoffAt.Equal(snapshot.CutoffAt) || !resumed.Checkpoint.RepairUsed {
		t.Fatal("resume changed history, snapshot or repair accounting")
	}
	original, err := store.Get(ctx, job.ID)
	if err != nil || original.Status != "degraded" || !reflect.DeepEqual(original.Checkpoint.Outputs, cp.Outputs) {
		t.Fatal("resuming mutated the original job")
	}
}
