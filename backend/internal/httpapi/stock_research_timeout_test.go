package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/stockanalysis"
)

func TestResearchPreparedBudgetControlsBothStageAndActivityWait(t *testing.T) {
	store, err := appsettings.Open("")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(func(values *appsettings.Values) error {
		values.LLM.ResponseTimeoutSeconds = 300
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	gateway := &fakeAgentGateway{agentSettings: agent.AgentSettings{ReasoningEffort: "max"}}
	server := &Server{settingsStore: store, agentGateway: gateway}
	request := stockanalysis.ResearchRequest{AnalysisLevel: stockanalysis.ResearchLevelStandard}
	ctx, budget, release, err := server.prepareStockResearch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(stockanalysis.WithResearchBudget(ctx, budget), budget.TotalTimeout())
	defer cancel()
	// A later setting change cannot restore the old 180-second deadline.
	gateway.agentSettings.ReasoningEffort = "low"
	gateway.promptFunc = func(callCtx context.Context, _ string) (agent.PromptResult, error) {
		deadline, ok := callCtx.Deadline()
		if !ok || time.Until(deadline) < 15*time.Minute-time.Second || time.Until(deadline) > 15*time.Minute {
			t.Fatal("maximum reasoning is still truncated by the standard stage")
		}
		return agent.PromptResult{Content: "answer"}, nil
	}
	p := researchPrompter{prompter: gateway, request: request, consistent: func() bool { return true }}
	if _, err := p.Prompt(ctx, "core"); err != nil {
		t.Fatal(err)
	}
	options := gateway.promptOptions[0]
	if options.FirstResponseTimeout != 300*time.Second || options.IdleTimeout != 300*time.Second || budget.TotalTimeout() != 47*time.Minute {
		t.Fatalf("stage, wait and total budgets diverged: budget=%+v options=%+v", budget, options)
	}
}

func TestResearchPrompterSeparatesActivityWaitFromStageDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	gateway := &fakeAgentGateway{promptFunc: func(callCtx context.Context, _ string) (agent.PromptResult, error) {
		deadline, ok := callCtx.Deadline()
		if !ok || time.Until(deadline) < 8*time.Minute-time.Second || time.Until(deadline) > 8*time.Minute {
			t.Fatal("stage can consume the entire research budget")
		}
		return agent.PromptResult{Content: `{"ok":true}`}, nil
	}}
	p := researchPrompter{prompter: gateway, request: stockanalysis.ResearchRequest{AnalysisLevel: stockanalysis.ResearchLevelDeep}, waitTimeout: 300 * time.Second, consistent: func() bool { return true }}
	if _, err := p.Prompt(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	options := gateway.promptOptions[0]
	if options.FirstResponseTimeout != 300*time.Second || options.IdleTimeout != 300*time.Second || options.OnProgress == nil || options.MaxAttempts != 2 {
		t.Fatal("activity budget missing")
	}
}

func TestResearchPrompterRetriesWatchdogOnceAndRespectsCancellation(t *testing.T) {
	for _, kind := range []string{"idle", "first_response", "cancelled", "short_budget", "auth"} {
		t.Run(kind, func(t *testing.T) {
			budget := time.Minute
			if kind == "short_budget" {
				budget = time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			calls := 0
			gateway := &fakeAgentGateway{promptFunc: func(context.Context, string) (agent.PromptResult, error) {
				calls++
				if kind == "cancelled" {
					cancel()
					return agent.PromptResult{}, context.Canceled
				}
				if kind == "auth" {
					return agent.PromptResult{}, errors.New("authentication failed")
				}
				timeoutKind := "idle"
				if kind == "first_response" {
					timeoutKind = kind
				}
				return agent.PromptResult{Content: `{"part":`}, &agent.PromptTimeoutError{Kind: timeoutKind, Wait: time.Millisecond}
			}}
			p := researchPrompter{prompter: gateway, waitTimeout: time.Millisecond, consistent: func() bool { return true }}
			result, err := p.Prompt(ctx, "test")
			want := 1
			if kind == "idle" {
				want = 2
			}
			if err == nil || calls != want || result.Progress.RetryCount != want-1 {
				t.Fatalf("unbounded or inappropriate retry: kind=%s calls=%d result=%+v err=%v", kind, calls, result, err)
			}
		})
	}
}

func TestResearchPrompterDoesNotRetryAfterRuntimeRetries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var gateway fakeAgentGateway
	var observed []int
	calls := 0
	gateway.promptFunc = func(context.Context, string) (agent.PromptResult, error) {
		calls++
		progress := agent.PromptProgress{RetryCount: calls + 1}
		gateway.promptOptions[len(gateway.promptOptions)-1].OnProgress(progress)
		return agent.PromptResult{Progress: progress}, &agent.PromptTimeoutError{Kind: "idle", Wait: time.Millisecond, Progress: progress}
	}
	p := researchPrompter{prompter: &gateway, waitTimeout: time.Millisecond, consistent: func() bool { return true }, onProgress: func(_ string, progress agent.PromptProgress) {
		observed = append(observed, progress.RetryCount)
	}}
	result, err := p.Prompt(ctx, "test")
	var timeout *agent.PromptTimeoutError
	if calls != 1 || result.Progress.RetryCount != 2 || !errors.As(err, &timeout) || timeout.Progress.RetryCount != 2 {
		t.Fatalf("lost retries between attempts: calls=%d progress=%+v err=%v", calls, result.Progress, err)
	}
	if len(observed) != 1 || observed[0] != 2 {
		t.Fatalf("inconsistent progress retry counts: %v", observed)
	}
}

func TestResearchPrompterStageDeadlineStopsActiveCallWithoutCancellingJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	gateway := &fakeAgentGateway{promptFunc: func(callCtx context.Context, _ string) (agent.PromptResult, error) {
		calls++
		<-callCtx.Done()
		return agent.PromptResult{Content: "draft", Progress: agent.PromptProgress{ReasoningBytes: 128}}, callCtx.Err()
	}}
	p := researchPrompter{prompter: gateway, stageTimeout: 40 * time.Millisecond, waitTimeout: time.Millisecond, consistent: func() bool { return true }}
	result, err := p.Prompt(ctx, "test")
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "单阶段") || calls != 1 || ctx.Err() != nil || result.Content != "draft" {
		t.Fatalf("stage deadline lost or retried: calls=%d result=%+v err=%v", calls, result, err)
	}
}

func TestResearchPrompterSharesStageDeadlineAndRetryAllowance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var deadlines []time.Time
	var gateway fakeAgentGateway
	gateway.promptFunc = func(callCtx context.Context, _ string) (agent.PromptResult, error) {
		deadline, _ := callCtx.Deadline()
		deadlines = append(deadlines, deadline)
		if len(deadlines) == 1 {
			return agent.PromptResult{}, &agent.PromptTimeoutError{Kind: "idle", Wait: time.Millisecond}
		}
		return agent.PromptResult{Progress: agent.PromptProgress{RetryCount: 1}}, &agent.PromptRetryLimitError{MaxAttempts: 1, Progress: agent.PromptProgress{RetryCount: 1}}
	}
	p := researchPrompter{prompter: &gateway, waitTimeout: time.Millisecond, consistent: func() bool { return true }}
	result, err := p.Prompt(ctx, "test")
	var limit *agent.PromptRetryLimitError
	if len(deadlines) != 2 || deadlines[0] != deadlines[1] || gateway.promptOptions[0].MaxAttempts != 2 || gateway.promptOptions[1].MaxAttempts != 1 || result.Progress.RetryCount != 2 || !errors.As(err, &limit) || limit.MaxAttempts != 2 || limit.Progress.RetryCount != 2 {
		t.Fatalf("retry reset the budget or allowance: deadlines=%v options=%+v result=%+v err=%v", deadlines, gateway.promptOptions, result, err)
	}
}

func TestResearchResumeAPIUsesSavedStagesAndRejectsLegacyReports(t *testing.T) {
	calls := 0
	gateway := &fakeAgentGateway{status: agent.Status{Available: true, Configured: true}, promptFunc: func(_ context.Context, prompt string) (agent.PromptResult, error) {
		calls++
		if strings.Contains(prompt, "任务是独立提出") {
			return agent.PromptResult{Content: `{"questions":[]}`}, nil
		}
		if strings.HasPrefix(prompt, "你是A股交易条件整理器") && calls == 3 {
			return agent.PromptResult{}, errors.New("model disconnected")
		}
		return agent.PromptResult{Content: validHTTPResearchJSON}, nil
	}}
	server := NewServer(Config{Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{}, StockBusiness: stockAnalysisBusiness{}, MarketOverview: &fakeMarketOverviewProvider{}, ReviewDBPath: ":memory:", AgentGateway: gateway})
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	job, err := server.stockResearch.Start(ctx, stockanalysis.ResearchRequest{Symbol: "600519.SH", AnalysisLevel: stockanalysis.ResearchLevelDeep})
	if err != nil {
		t.Fatal(err)
	}
	job, err = server.stockResearch.Wait(ctx, job.ID)
	if err != nil || !job.Public().ResumeAvailable || job.Analysis.ResearchReport != nil {
		t.Fatalf("failed research lost its checkpoint: %s %v", job.Status, err)
	}
	r := httptest.NewRequest("POST", "/api/v1/stocks/research/"+job.ID+"/resume", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 202 || strings.Contains(w.Body.String(), `"checkpoint"`) {
		t.Fatalf("resume failed or leaked draft: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Data stockanalysis.ResearchJob `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	resumed, err := server.stockResearch.Wait(ctx, result.Data.ID)
	if err != nil || resumed.Status != "succeeded" || calls != 4 || resumed.Snapshot.ID != job.Snapshot.ID {
		t.Fatalf("resume repeated model calls: %s calls=%d err=%v", resumed.Status, calls, err)
	}
	legacy := job
	legacy.ID = "legacy"
	legacy.Checkpoint = nil
	if err := server.stockResearchStore.Save(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/stocks/research/legacy/resume", nil))
	if w.Code != 409 {
		t.Fatal("legacy report falsely advertised resumability")
	}
}
