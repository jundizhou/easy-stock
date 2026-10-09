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

const invalidTradeJSON = `{"conditions":[{"id":"c1","threshold":"not-a-number"}],"decision":{}}`

func TestDecodeResearchJSONPreservesMainObjectFieldError(t *testing.T) {
	trade := ResearchTradeConditions{InvalidationIDs: []string{"original"}}
	err := decodeJSONObject(invalidTradeJSON, &trade)
	var fieldErr *json.UnmarshalTypeError
	if !errors.As(err, &fieldErr) || !strings.Contains(fieldErr.Field, "threshold") {
		t.Fatalf("nested decision hid the threshold type error: %v", err)
	}
	if len(trade.InvalidationIDs) != 1 || trade.InvalidationIDs[0] != "original" || len(trade.Conditions) != 0 {
		t.Fatal("invalid main object changed the decode target")
	}
}

func TestResearchRepairsTradeJSONOnceWithOriginalError(t *testing.T) {
	for _, level := range []ResearchLevel{ResearchLevelDeep, ResearchLevelStandard} {
		t.Run(string(level), func(t *testing.T) {
			analysis, snapshot := researchFixture(t)
			final, _ := json.Marshal(validResearch())
			execution := &researchExecution{}
			ctx := context.WithValue(context.Background(), researchExecutionKey{}, execution)
			expectedCalls := 3
			if level == ResearchLevelDeep {
				expectedCalls = 4
			}
			p := &researchTestPrompter{respond: func(call int, prompt string) (string, error) {
				if level == ResearchLevelDeep && call == 1 {
					return `{"questions":[]}`, nil
				}
				if call == expectedCalls-1 {
					return invalidTradeJSON, nil
				}
				if call == expectedCalls {
					if !strings.Contains(prompt, "具体解析错误：") || !strings.Contains(prompt, "threshold") || !strings.Contains(prompt, invalidTradeJSON) {
						t.Fatal("repair did not receive the field error and original final output")
					}
					failure, ok := execution.checkpoint.Failures["交易条件"]
					if !ok || failure.Content != invalidTradeJSON || !strings.Contains(failure.Error, "threshold") {
						t.Fatal("invalid trade output was not captured before repair")
					}
				}
				if call > expectedCalls {
					t.Fatal("exceeded the shared repair budget")
				}
				return string(final), nil
			}}
			request := ResearchRequest{Symbol: analysis.Symbol, Horizon: "swing", AnalysisLevel: level}
			if err := RunResearch(ctx, p, &snapshot, &analysis, request, "fixture", nil, nil); err != nil {
				t.Fatal(err)
			}
			if p.calls != expectedCalls || analysis.ResearchReport == nil || analysis.ResearchReport.Validation != "references_checked" || len(analysis.ResearchReport.Attempts) != expectedCalls {
				t.Fatalf("trade repair failed: calls=%d report=%+v", p.calls, analysis.ResearchReport)
			}
			cp := execution.checkpoint
			if !cp.RepairUsed || len(cp.Failures) != 0 || len(cp.Outputs["交易条件"].Value) == 0 {
				t.Fatal("repaired trade was not cached under its original stage")
			}
			for _, options := range p.options {
				if !options.DisableTools || !options.Sandbox {
					t.Fatal("format repair enabled model tools")
				}
			}
		})
	}
}

func TestDeepResearchSharesJSONAndValidationRepairBudget(t *testing.T) {
	// [ES_PARTIAL_CORE_SAVE_PATCH] 预期行为差异（本地有意保留，非回归）：
	//
	// 上游 v1.5.0 语义为「全有或全无」——交易段修复预算耗尽 → 整次研究 failed，
	// core 段已通过的结论一并丢弃。
	//
	// 本地 §4 受控分段保存刻意改掉了这一点：只要 core 段自身通过完整语义校验，
	// 就保存 core-only 报告并返回 PartialResearchError（job 标记 partial），
	// 避免「交易段 JSON 坏一次」就把已可信的核心判断全部作废。
	//
	// 因此上游断言的两点在本项目不成立：
	//   - analysis.ResearchReport == nil（本地会保留 core-only 报告）
	//   - err 为整次失败且 Conclusion 未变（本地应用了 core-only 结论）
	// 但「修复预算上限」「不得触发第二次修复」仍必须守护 —— 这两点由下方
	// partialSave 分支继续断言，故不整体跳过。
	partialSave := true
	for _, failure := range []string{"trade_json_after_core_repair", "validation_after_trade_repair"} {
		t.Run(failure, func(t *testing.T) {
			analysis, snapshot := researchFixture(t)
			before := analysis.Conclusion
			final, _ := json.Marshal(validResearch())
			invalidReferences := validResearch()
			invalidReferences.Conditions[0].SourceIDs = []string{"unknown-source"}
			invalidFinal, _ := json.Marshal(invalidReferences)
			p := &researchTestPrompter{respond: func(call int, prompt string) (string, error) {
				if call > 4 {
					t.Fatal("a second repair was attempted")
				}
				if call == 1 {
					return `{"questions":[]}`, nil
				}
				if failure == "trade_json_after_core_repair" {
					if call == 2 {
						return `{"headline":["invalid"]}`, nil
					}
					if call == 3 && strings.Contains(prompt, "交易条件conditions") {
						t.Fatal("core repair was given trade-stage instructions")
					}
					if call == 4 {
						return invalidTradeJSON, nil
					}
				} else {
					if call == 3 {
						return invalidTradeJSON, nil
					}
					if call == 4 {
						return string(invalidFinal), nil
					}
				}
				return string(final), nil
			}}
			request := ResearchRequest{Symbol: analysis.Symbol, Horizon: "swing", AnalysisLevel: ResearchLevelDeep}
			err := RunResearch(context.Background(), p, &snapshot, &analysis, request, "fixture", nil, nil)
			// 预算与「不得二次修复」是硬约束，任何语义下都必须成立。
			if p.calls != 4 {
				t.Fatalf("exceeded shared repair budget: calls=%d", p.calls)
			}
			if err == nil {
				t.Fatalf("invalid trade JSON was accepted as success: calls=%d", p.calls)
			}
			if partialSave {
				// [ES_PARTIAL_CORE_SAVE_PATCH] 本地预期：core 段校验通过 → 保存
				// core-only 报告，job 以 PartialResearchError 标记 partial。
				var partial *PartialResearchError
				if !errors.As(err, &partial) {
					t.Fatalf("expected PartialResearchError under local partial-save semantics, got %T: %v", err, err)
				}
				if analysis.ResearchReport == nil {
					t.Fatal("core-only report was not preserved under local partial-save semantics")
				}
				return
			}
			if analysis.ResearchReport != nil || analysis.Conclusion != before {
				t.Fatalf("failed stage became AI success or exceeded budget: calls=%d err=%v", p.calls, err)
			}
		})
	}
}

func TestDeepResearchDoesNotRepairTransportErrorsOrCancellation(t *testing.T) {
	for _, failure := range []string{"transport", "cancelled_with_invalid_json"} {
		t.Run(failure, func(t *testing.T) {
			analysis, snapshot := researchFixture(t)
			final, _ := json.Marshal(validResearch())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &researchTestPrompter{respond: func(call int, _ string) (string, error) {
				switch call {
				case 1:
					return `{"questions":[]}`, nil
				case 2:
					return string(final), nil
				case 3:
					if failure == "transport" {
						return "", errors.New("connection interrupted")
					}
					cancel()
					return invalidTradeJSON, nil
				default:
					t.Fatal("transport or cancellation triggered format repair")
					return "", nil
				}
			}}
			err := RunResearch(ctx, p, &snapshot, &analysis, ResearchRequest{Symbol: analysis.Symbol, Horizon: "swing", AnalysisLevel: ResearchLevelDeep}, "fixture", nil, nil)
			if err == nil || p.calls != 3 || analysis.ResearchReport != nil {
				t.Fatalf("failure was accepted: calls=%d err=%v", p.calls, err)
			}
		})
	}
}

func TestResearchFailedFinalOutputIsBoundedRedactedAndPrivate(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	path := filepath.Join(t.TempDir(), "research.db")
	store, err := OpenResearchStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cp := &ResearchCheckpoint{PromptVersion: ResearchPromptVersion, CompressionVersion: ResearchCompressionVersion, Outputs: map[string]ResearchStageOutput{}}
	job := ResearchJob{ID: "invalid-json", Status: "failed", Analysis: &analysis, Snapshot: &snapshot, Checkpoint: cp}
	ctx := context.WithValue(context.Background(), researchExecutionKey{}, &researchExecution{save: func(*ResearchCheckpoint) error {
		return store.Save(context.Background(), job)
	}})
	content := `{"conditions":[{"threshold":"wrong"}],"decision":{},"debug":"PRIVATE_FINAL_OUTPUT Bearer fake-test-secret ` + strings.Repeat("x", 9000) + ` END_OF_RESPONSE"}`
	p := &researchTestPrompter{respond: func(int, string) (string, error) { return content, nil }}
	_, err = promptResearchJSON[ResearchTradeConditions](ctx, p, "trade prompt", "交易条件", promptJSONObjectOptions{maxAttempts: 1, disableTools: true}, cp, &snapshot)
	if !isInvalidModelJSON(err) {
		t.Fatalf("expected typed invalid JSON error: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenResearchStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	saved, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	failure := saved.Checkpoint.Failures["交易条件"]
	if failure.PromptHash != researchHash("trade prompt") || !strings.Contains(failure.Error, "threshold") || !strings.Contains(failure.Content, "PRIVATE_FINAL_OUTPUT") || !strings.Contains(failure.Content, "<redacted>") || len([]rune(failure.Content)) > 8000 || strings.Contains(failure.Content, "fake-test-secret") || strings.Contains(failure.Content, "END_OF_RESPONSE") {
		t.Fatal("failed final output was not stored with bounded, redacted diagnostics")
	}
	if len(saved.Checkpoint.Outputs) != 0 || saved.Analysis.ResearchReport != nil {
		t.Fatal("invalid JSON was saved as a successful stage")
	}
	public, err := json.Marshal(saved.Public())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), "PRIVATE_FINAL_OUTPUT") || strings.Contains(string(public), "checkpoint") || strings.Contains(string(public), "failures") {
		t.Fatal("private failed output escaped through the public task")
	}
}

func TestResearchResumeCachesRepairedCoreAndPreservesBudget(t *testing.T) {
	// [ES_PARTIAL_CORE_SAVE_PATCH] 预期行为差异（本地有意保留，非回归）：
	//
	// 上游断言第一次运行 status=="failed"、resume 后仍 status=="failed" 且
	// second.Analysis.ResearchReport == nil（全有或全无）。
	//
	// 本地 §4 分段保存的意图正是本测试的场景：第一次运行 core 段已修复成功、
	// 交易段因传输错误中断 → 保存 core-only 报告并把 job 标为 partial；resume 后
	// 交易段被 repair 预算耗尽拒绝 → 再次以 partial + PartialResearchError 收尾。
	//
	// 因此下方两处 status/ResearchReport 断言按本地语义放宽，但**预算不重置**
	// （p.calls==5）、**resume 不得重跑已完成阶段**（prompter 的 default 分支）、
	// 以及快照/任务延续等守护全部保留。
	partialSave := true
	analysis, snapshot := researchFixture(t)
	path := filepath.Join(t.TempDir(), "research.db")
	store, err := OpenResearchStore(path)
	if err != nil {
		t.Fatal(err)
	}
	final, _ := json.Marshal(validResearch())
	p := &researchTestPrompter{respond: func(call int, prompt string) (string, error) {
		switch call {
		case 1:
			return `{"questions":[]}`, nil
		case 2:
			return `{"headline":["invalid"]}`, nil
		case 3:
			return string(final), nil
		case 4:
			return "", errors.New("connection interrupted")
		case 5:
			if !strings.HasPrefix(prompt, "你是A股交易条件整理器") || strings.Contains(prompt, "[结构修复要求]") {
				t.Fatal("resume reran a completed stage")
			}
			return invalidTradeJSON, nil
		default:
			t.Fatal("resume repeated a completed stage or reset its repair budget")
			return "", nil
		}
	}}
	runner := func(ctx context.Context, request ResearchRequest, publish ResearchPublisher) (Analysis, *ResearchSnapshot, error) {
		a, s := analysis, snapshot
		if previous := ResearchResumeData(ctx); previous != nil {
			a, s = *previous.Analysis, *previous.Snapshot
		}
		if err := publish("baseline", "quantitative", &a, &s); err != nil {
			return a, &s, err
		}
		err := RunResearch(ctx, p, &s, &a, request, "fixture", nil, nil)
		return a, &s, err
	}
	service := NewResearchService(store, runner)
	first, err := service.Start(context.Background(), ResearchRequest{Symbol: analysis.Symbol, Horizon: "swing", AnalysisLevel: ResearchLevelDeep})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, err = service.Wait(ctx, first.ID)
	firstFailed := first.Status == "failed"
	firstPartial := first.Status == "partial"
	if err != nil || (!firstFailed && !(partialSave && firstPartial)) || p.calls != 4 || first.Checkpoint == nil || !first.Checkpoint.RepairUsed || len(first.Checkpoint.Outputs["核心判断"].Value) == 0 || len(first.Checkpoint.Failures) != 0 {
		t.Fatalf("repaired core was not checkpointed: status=%s calls=%d err=%v", first.Status, p.calls, err)
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
	secondOK := second.Status == "failed"
	if partialSave {
		// 本地预期：第二次仍以 partial 收尾，且必须**保留** core-only 报告，
		// 错误信息须仍指向交易条件的 threshold 解析失败。
		secondOK = second.Status == "partial" && second.Analysis.ResearchReport != nil
	}
	if err != nil || !secondOK || p.calls != 5 || !strings.Contains(second.Error, "threshold") {
		t.Fatalf("resume reset repair budget or accepted invalid trade: status=%s calls=%d err=%v", second.Status, p.calls, err)
	}
	if second.ResumedFrom != first.ID || second.ID == first.ID || second.Snapshot.ID != first.Snapshot.ID || !second.Snapshot.CutoffAt.Equal(first.Snapshot.CutoffAt) {
		t.Fatal("resume lost the original task or evidence time")
	}
}
