package portfolioinspection

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/stockanalysis"

	"gopkg.in/yaml.v3"
)

func TestAggregationBudgetAllowsSlowReasoningAndConfiguredWait(t *testing.T) {
	for _, tc := range []struct {
		name, effort string
		wait, stage  time.Duration
	}{
		{"unbound_default", "", 0, 12 * time.Minute},
		{"none", "none", 5 * time.Minute, 12 * time.Minute},
		{"low", "low", 5 * time.Minute, 12 * time.Minute},
		{"medium", "medium", 5 * time.Minute, 12 * time.Minute},
		{"high", "high", 5 * time.Minute, 15 * time.Minute},
		{"max", "max", 5 * time.Minute, 20 * time.Minute},
		{"xhigh", " xHIGH ", 5 * time.Minute, 20 * time.Minute},
		{"long_wait", "low", 30 * time.Minute, 31 * time.Minute},
		{"longest_configured_wait", "max", time.Hour, 61 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := aggregationBudgetFor(tc.wait, tc.effort)
			wait := tc.wait
			if wait == 0 {
				wait = 5 * time.Minute
			}
			if b.StageTimeout != tc.stage || b.ResponseTimeout != wait || b.TotalTimeout() != 2*tc.stage {
				t.Fatalf("unexpected finite scoring/repair budget: %+v total=%s", b, b.TotalTimeout())
			}
		})
	}
}

// Use the real task binding, but all files and credentials are test fixtures;
// no runtime or model process is started.
func aggregationTestAgent(t *testing.T, responseSeconds int, effort string) (*agent.Service, func(int, string)) {
	t.Helper()
	hermesHome := t.TempDir()
	s := agent.NewService(agent.ServiceConfig{
		Hermes: agent.HermesConfig{Home: hermesHome, WorkDir: t.TempDir()},
		Codex:  agent.CodexConfig{Home: t.TempDir()},
	})
	update := func(wait int, effort string) {
		t.Helper()
		key := "test-only-key"
		if err := s.SyncLLM(appsettings.LLM{Provider: "custom", BaseURL: "https://example.invalid/v1", Model: "fixture", APIMode: "responses", ResponseTimeoutSeconds: wait}, &key); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(hermesHome, "config.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		if err := yaml.Unmarshal(data, &config); err != nil {
			t.Fatal(err)
		}
		agentConfig, _ := config["agent"].(map[string]any)
		if agentConfig == nil {
			agentConfig = map[string]any{}
		}
		agentConfig["easy_stock_reasoning_effort"] = effort
		config["agent"] = agentConfig
		data, err = yaml.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	update(responseSeconds, effort)
	return s, update
}

func TestAggregationBudgetUsesFrozenModelSettings(t *testing.T) {
	gateway, update := aggregationTestAgent(t, 300, "max")
	ctx, release, err := gateway.BindTask(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	update(1800, "low")
	b := aggregationBudgetFromContext(ctx)
	if b.ResponseTimeout != 5*time.Minute || b.StageTimeout != 20*time.Minute || b.ReasoningEffort != "max" {
		t.Fatal("running scoring inherited changed settings", b)
	}
	next, releaseNext, err := gateway.BindTask(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseNext()
	b = aggregationBudgetFromContext(next)
	if b.ResponseTimeout != 30*time.Minute || b.StageTimeout != 31*time.Minute || b.ReasoningEffort != "low" {
		t.Fatal("new task ignored model wait/effort", b)
	}
}

func TestInspectionDeadlineReservesFullAggregationAndRepair(t *testing.T) {
	req := Request{ResearchLevel: stockanalysis.ResearchLevelStandard}
	b := aggregationBudgetFor(5*time.Minute, "max")
	// A standard research batch has a 47-minute max-effort allowance.
	for _, tc := range []struct {
		name                  string
		holdings, concurrency int
		want                  time.Duration
	}{
		{"one_batch", 2, 2, 89 * time.Minute},
		{"two_batches", 4, 2, 136 * time.Minute},
		{"single_worker", 2, 1, 136 * time.Minute},
		{"aggregation_only", 0, 2, 42 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inspectionTimeout(req, tc.holdings, tc.concurrency, b); got != tc.want {
				t.Fatalf("outer job deadline cuts into scoring/repair: got=%s want=%s", got, tc.want)
			}
		})
	}
}

type aggregationDeadlineGateway struct {
	*scoreGateway
	deadlines []time.Time
	options   []agent.PromptOptions
	prompts   []string
	respond   func(int) (agent.PromptResult, error)
}

func (g *aggregationDeadlineGateway) PromptWithOptions(ctx context.Context, prompt string, options agent.PromptOptions) (agent.PromptResult, error) {
	deadline, _ := ctx.Deadline()
	g.deadlines = append(g.deadlines, deadline)
	g.options = append(g.options, options)
	g.prompts = append(g.prompts, prompt)
	result, err := g.respond(len(g.prompts))
	result.Content = fixtureModelScoringResponse(result.Content, prompt)
	return result, err
}

func TestScoringCallDeadlineAndWaitFollowFrozenConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, effort string
		waitSeconds  int
		wantStage    time.Duration
	}{
		{"default", "", 300, 12 * time.Minute},
		{"high", "high", 300, 15 * time.Minute},
		{"max", "max", 300, 20 * time.Minute},
		{"long_wait", "low", 1800, 31 * time.Minute},
		{"invalid_wait_normalized", "max", 1, 20 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binding, _ := aggregationTestAgent(t, tc.waitSeconds, tc.effort)
			ctx, release, err := binding.BindTask(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			valid := scoreContent(t, scoreJSON(t))
			g := &aggregationDeadlineGateway{scoreGateway: &scoreGateway{}, respond: func(int) (agent.PromptResult, error) {
				return agent.PromptResult{Content: valid}, nil
			}}
			s := NewService(nil, g, nil, nil)
			req, results, metrics, _ := scoreFixture()
			rules, _ := RulesFor(req.TraderProfile)
			before := time.Now()
			report, err := s.scorePortfolio(ctx, req, results, metrics, rules, before.UTC())
			if err != nil || !report.ScoreAvailable || len(g.deadlines) != 1 {
				t.Fatal("scoring failed", err, g.deadlines)
			}
			got := g.deadlines[0].Sub(before)
			if got < tc.wantStage || got > tc.wantStage+time.Second {
				t.Fatalf("call still uses old deadline: got=%s want=%s", got, tc.wantStage)
			}
			wantWait := time.Duration(appsettings.NormalizeLLMResponseTimeoutSeconds(tc.waitSeconds)) * time.Second
			opts := g.options[0]
			if opts.FirstResponseTimeout != wantWait || opts.IdleTimeout != wantWait || opts.ReasoningEffortCap != "" || opts.MaxAttempts != 1 || !opts.Sandbox || !opts.AutoApprove || !opts.DisableTools {
				t.Fatal("changed effort/retries or cut configured wait short", opts)
			}
		})
	}
}

func TestScoringRepairSharesParentDeadlineAndStopsAfterOne(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(map[bool]string{true: "repaired", false: "still_invalid"}[success], func(t *testing.T) {
			valid := scoreContent(t, scoreJSON(t))
			g := &aggregationDeadlineGateway{scoreGateway: &scoreGateway{}, respond: func(call int) (agent.PromptResult, error) {
				if call == 2 && success {
					return agent.PromptResult{Content: valid}, nil
				}
				return agent.PromptResult{Content: "invalid JSON"}, nil
			}}
			s := NewService(nil, g, nil, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			deadline, _ := ctx.Deadline()
			req, results, metrics, _ := scoreFixture()
			rules, _ := RulesFor(req.TraderProfile)
			_, err := s.scorePortfolio(ctx, req, results, metrics, rules, time.Now().UTC())
			if (err == nil) != success || len(g.prompts) != 2 {
				t.Fatal("repair count changed", err, len(g.prompts))
			}
			if !g.deadlines[0].Equal(deadline) || !g.deadlines[1].Equal(deadline) || !strings.HasPrefix(g.prompts[1], g.prompts[0]) || !strings.Contains(g.prompts[1], "invalid JSON") {
				t.Fatal("repair reset deadline or lost frozen input", g.deadlines)
			}
		})
	}
}

func TestScoringProviderFailureIsNotRetried(t *testing.T) {
	g := &aggregationDeadlineGateway{scoreGateway: &scoreGateway{}, respond: func(int) (agent.PromptResult, error) {
		return agent.PromptResult{}, context.DeadlineExceeded
	}}
	s := NewService(nil, g, nil, nil)
	req, results, metrics, _ := scoreFixture()
	rules, _ := RulesFor(req.TraderProfile)
	_, err := s.scorePortfolio(context.Background(), req, results, metrics, rules, time.Now().UTC())
	if !errors.Is(err, context.DeadlineExceeded) || len(g.prompts) != 1 {
		t.Fatal("provider failure restarted scoring", err, len(g.prompts))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.scorePortfolio(ctx, req, results, metrics, rules, time.Now().UTC())
	if !errors.Is(err, context.Canceled) || len(g.prompts) != 1 {
		t.Fatal("canceled workflow sent a model request", err, len(g.prompts))
	}
}

type boundAggregationGateway struct {
	*aggregationDeadlineGateway
	binding *agent.Service
}

func (g *boundAggregationGateway) BindTask(ctx context.Context) (context.Context, func(), error) {
	return g.binding.BindTask(ctx)
}

func TestInspectionReservesAndLogsFrozenAggregationBudget(t *testing.T) {
	req, results, _, _ := scoreFixture()
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var logs bytes.Buffer
	binding, update := aggregationTestAgent(t, 300, "max")
	valid := scoreContent(t, scoreJSON(t))
	g := &boundAggregationGateway{binding: binding, aggregationDeadlineGateway: &aggregationDeadlineGateway{
		scoreGateway: &scoreGateway{}, respond: func(int) (agent.PromptResult, error) {
			return agent.PromptResult{Content: valid}, nil
		},
	}}
	s := NewService(store, g, nil, log.New(&logs, "", 0))
	defer s.Close()
	outerBudget := make(chan time.Duration, 1)
	s.ConfigureResearch(func(ctx context.Context, _ Holding, _ Request, _ time.Time, _ bool, _ string, _ func(HoldingResult)) (HoldingResult, error) {
		deadline, _ := ctx.Deadline()
		outerBudget <- time.Until(deadline)
		// Settings can change while research runs; both aggregation reserves and
		// model waits must continue using the task's initial settings.
		update(1800, "low")
		return results[0], nil
	}, nil)
	j, err := s.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if done := waitPortfolio(t, s, j.ID); done.Status != "succeeded" {
		t.Fatal("inspection failed", done.Error)
	}
	// Wait for completion logging before reading the buffer.
	s.Close()
	if got := <-outerBudget; got > 89*time.Minute || got < 89*time.Minute-time.Second {
		t.Fatal("workflow kept the old outer deadline", got)
	}
	if len(g.deadlines) != 1 {
		t.Fatal("unexpected aggregation calls", g.deadlines)
	}
	remaining := time.Until(g.deadlines[0])
	if remaining > 20*time.Minute || remaining < 20*time.Minute-time.Second || g.options[0].FirstResponseTimeout != 5*time.Minute || g.options[0].IdleTimeout != 5*time.Minute {
		t.Fatal("workflow did not use frozen aggregation budget", remaining, g.options[0])
	}
	if !strings.Contains(logs.String(), "reasoning_effort=\"max\" response_timeout_seconds=300 stage_timeout_seconds=1200 total_timeout_seconds=2400") {
		t.Fatal("resolved budget missing from aggregation log", logs.String())
	}
}
