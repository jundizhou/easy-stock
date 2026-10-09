package portfoliooptimization

import (
	"context"
	"easy-stock/backend/internal/agent"
	"errors"
	"strings"
	"testing"
	"time"
)

type deadlineGateway struct {
	*testGateway
	options  agent.PromptOptions
	deadline time.Duration
}

func (g *deadlineGateway) PromptWithOptions(ctx context.Context, _ string, options agent.PromptOptions) (agent.PromptResult, error) {
	g.calls.Add(1)
	if deadline, ok := ctx.Deadline(); ok {
		g.deadline = time.Until(deadline)
	}
	g.options = options
	options.OnProgress(agent.PromptProgress{ElapsedMS: 1000, ReasoningBytes: 100})
	return agent.PromptResult{Progress: agent.PromptProgress{ElapsedMS: 2000, TextBytes: 12, ReasoningBytes: 200}}, context.DeadlineExceeded
}

func TestReviewBudgetIsIndependentAndRespectsOverallDeadline(t *testing.T) {
	for _, tc := range []struct {
		name          string
		overall, want time.Duration
		spent         int64
	}{
		{"full_review_after_long_proposal", TotalTimeout, ModelTimeout, 0},
		{"repair_uses_same_review_budget", TotalTimeout, ModelTimeout - time.Minute, 60000},
		{"overall_deadline_still_limits_review", 90 * time.Second, 90 * time.Second, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &testGateway{}
			s, _, _ := setupService(t, g)
			d := &deadlineGateway{testGateway: g}
			s.gateway = d
			j := fixtureJob()
			j.ID = "budget"
			j.Fingerprint = "budget"
			j.Stage = "assessing"
			j.ModelDurationMS = 285000
			j.ModelStageDurationMS = map[string]int64{"proposing": 285000, "assessing": tc.spent}
			ctx, cancel := context.WithTimeout(context.Background(), tc.overall)
			defer cancel()
			_ = s.model(ctx, &j, "review", func(string) error { return nil })
			if d.deadline > tc.want || d.deadline < tc.want-time.Second {
				t.Fatalf("review budget=%s want=%s", d.deadline, tc.want)
			}
			if d.options.ReasoningEffortCap != "medium" || !d.options.Sandbox {
				t.Fatal("review lost its isolated effort cap", d.options.ReasoningEffortCap)
			}
		})
	}
}

func TestOversizedRequestIsRejectedBeforeCallingModel(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	j := fixtureJob()
	j.Stage = "proposing"
	err := s.model(context.Background(), &j, strings.Repeat("x", MaxModelPromptBytes+1), func(string) error { return nil })
	if err == nil || g.calls.Load() != 0 {
		t.Fatal("oversized input sent to model", err, g.calls.Load())
	}
}
func TestModelDeadlinePreservesProgressAndIdentifiesStage(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	d := &deadlineGateway{testGateway: g}
	s.gateway = d
	j := fixtureJob()
	j.ID = "model-diagnostic"
	j.Fingerprint = "model-diagnostic"
	j.Stage = "proposing"
	err := s.model(context.Background(), &j, "test prompt", func(string) error { t.Fatal("failed output must not be validated"); return nil })
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "生成持仓方案超时") {
		t.Fatal(err)
	}
	if len(j.ModelAttempts) != 1 || j.ModelAttempts[0].PromptBytes != len("test prompt") || j.ModelProgress.ReasoningBytes != 200 || j.ModelProgress.TextBytes != 12 || d.options.MaxAttempts != 1 || !d.options.DisableTools || d.options.ReasoningEffortCap != "low" || !d.options.Sandbox {
		t.Fatal("missing diagnostics or changed execution policy", j.ModelAttempts)
	}
	stored, err := s.Get(context.Background(), j.ID)
	if err != nil || len(stored.ModelAttempts) != 1 || stored.ModelProgress.TextBytes != 12 {
		t.Fatal("diagnostics not persisted", err)
	}
}

type repairGateway struct {
	*testGateway
	output  string
	prompts []string
}

func (g *repairGateway) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	g.calls.Add(1)
	g.prompts = append(g.prompts, prompt)
	select {
	case <-time.After(10 * time.Millisecond):
	case <-ctx.Done():
		return agent.PromptResult{}, ctx.Err()
	}
	return agent.PromptResult{Content: g.output}, nil
}
func TestRepairReservesSpaceAndCannotBypassInputLimit(t *testing.T) {
	for _, tc := range []struct {
		name        string
		outputBytes int
		wantCalls   int
		wantSuccess bool
	}{{"fits_reserved_space", 6000, 2, true}, {"observed_response_fits_repair", 9941, 2, true}, {"oversized_repair_is_blocked", 11000, 1, false}} {
		t.Run(tc.name, func(t *testing.T) {
			g := &testGateway{}
			s, _, _ := setupService(t, g)
			r := &repairGateway{testGateway: g, output: strings.Repeat("r", tc.outputBytes)}
			s.gateway = r
			j := fixtureJob()
			j.ID = tc.name
			j.Stage = "assessing"
			validations := 0
			err := s.model(context.Background(), &j, strings.Repeat("x", MaxInitialModelPromptBytes), func(string) error {
				validations++
				if validations == 1 {
					return errors.New("test validation failure")
				}
				return nil
			})
			if (err == nil) != tc.wantSuccess || int(g.calls.Load()) != tc.wantCalls {
				t.Fatal(err, g.calls.Load())
			}
			if !tc.wantSuccess && jobRepairUsed(&j) {
				t.Fatal("unsent oversized request consumed repair allowance")
			}
			for _, prompt := range r.prompts {
				if len(prompt) > MaxModelPromptBytes {
					t.Fatal("repair bypassed hard limit")
				}
			}
			if tc.wantSuccess && j.ModelAttempts[1].BudgetMS >= j.ModelAttempts[0].BudgetMS {
				t.Fatal("repair received a new stage budget", j.ModelAttempts)
			}
		})
	}
}

func TestRepairCanRemoveDuplicateSchemaWithoutChangingFrozenDataOrRules(t *testing.T) {
	prompt, err := proposalPrompt(fixtureJob())
	if err != nil {
		t.Fatal(err)
	}
	// Add a duplicate output example; compact contracts no longer carry one.
	prompt = strings.Replace(prompt, "[资料JSON]\n", `{"issues":[],"example":"`+strings.Repeat("s", 1000)+`"}`+"\n[资料JSON]\n", 1)
	prompt = strings.Repeat("x", MaxInitialModelPromptBytes-len(prompt)) + prompt
	output := strings.Repeat("r", 10351)
	repair := modelRepairPrompt(prompt, output, errors.New("invalid character '.' after object key:value pair"))
	_, originalData, _ := strings.Cut(prompt, "[资料JSON]\n")
	if len(repair) > MaxModelPromptBytes || !strings.Contains(repair, "[资料JSON]\n"+originalData) || !strings.HasSuffix(repair, output) {
		t.Fatal("repair lost frozen data/output or exceeded budget", len(repair))
	}
	if !strings.Contains(repair, "固定required_stock_total及required_cash") || !strings.Contains(repair, "报告评级/observe/no_plan") || strings.Contains(repair, `{"issues":[]`) {
		t.Fatal("changed investment rules instead of duplicated schema")
	}
}

type transportGateway struct {
	*testGateway
	prompts  []string
	options  []agent.PromptOptions
	budgets  []time.Duration
	failures int
	err      error
}

func (g *transportGateway) PromptWithOptions(ctx context.Context, prompt string, opts agent.PromptOptions) (agent.PromptResult, error) {
	g.prompts = append(g.prompts, prompt)
	g.options = append(g.options, opts)
	deadline, _ := ctx.Deadline()
	g.budgets = append(g.budgets, time.Until(deadline))
	if len(g.prompts) <= g.failures {
		return agent.PromptResult{Content: "transport failed"}, g.err
	}
	return agent.PromptResult{Content: "valid"}, nil
}
func TestTransientTransportRetryIsBoundedAndKeepsInputAndBudget(t *testing.T) {
	for _, tc := range []struct {
		name            string
		failures, calls int
		err             error
		ok              bool
	}{
		{"recovers", 1, 2, agent.ErrModelTransport, true},
		{"stops_after_two", 3, 2, agent.ErrModelTransport, false},
		{"auth_not_retried", 1, 1, errors.New("model auth failed"), false},
		{"deadline_not_retried", 1, 1, context.DeadlineExceeded, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &testGateway{}
			s, _, _ := setupService(t, g)
			transport := &transportGateway{testGateway: g, failures: tc.failures, err: tc.err}
			s.gateway = transport
			j := fixtureJob()
			j.ID = tc.name
			j.Stage = "assessing"
			validated := 0
			err := s.model(context.Background(), &j, "frozen same input", func(content string) error {
				validated++
				if content != "valid" {
					t.Fatal("failed response became investment content")
				}
				return nil
			})
			if (err == nil) != tc.ok || len(transport.prompts) != tc.calls || jobRepairUsed(&j) {
				t.Fatal("incorrect retry/repair limits", err, len(transport.prompts))
			}
			if validated != map[bool]int{true: 1, false: 0}[tc.ok] {
				t.Fatal("failed transport response validated")
			}
			for _, p := range transport.prompts {
				if p != "frozen same input" {
					t.Fatal("transport retry changed opinion input")
				}
			}
			if len(transport.budgets) > 1 && transport.budgets[1] >= transport.budgets[0]-500*time.Millisecond {
				t.Fatal("transport retry reset stage budget")
			}
			for _, o := range transport.options {
				if o.MaxAttempts != 1 {
					t.Fatal("nested unlimited retry")
				}
			}
		})
	}
}

func TestTransportRetryDoesNotConsumeOnlyFormatRepair(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	transport := &transportGateway{testGateway: g, failures: 1, err: agent.ErrModelTransport}
	s.gateway = transport
	j := fixtureJob()
	j.ID = "transport-then-format"
	j.Stage = "assessing"
	validations := 0
	err := s.model(context.Background(), &j, "same frozen input", func(string) error {
		validations++
		if validations == 1 {
			return errors.New("format mismatch")
		}
		return nil
	})
	if err != nil || len(transport.prompts) != 3 || validations != 2 || !jobRepairUsed(&j) {
		t.Fatal("transport incorrectly consumed format repair", err, transport.prompts)
	}
	if transport.prompts[0] != transport.prompts[1] || transport.prompts[2] == transport.prompts[1] {
		t.Fatal("wrong prompt retried or repaired")
	}
}

func TestRevisionPhaseBudgetSharesOriginalOverallDeadline(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	d := &deadlineGateway{testGateway: g}
	s.gateway = d
	j := fixtureJob()
	j.ID = "revision-budget"
	j.Stage = "assessing"
	j.RevisionCount = 1
	j.ModelStageDurationMS = map[string]int64{"proposing": 480000, "assessing": 480000, "assessing.round_2": 60000}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_ = s.model(ctx, &j, "revision review", func(string) error { return nil })
	if d.deadline > 90*time.Second || d.deadline < 89*time.Second {
		t.Fatal("revision bypassed overall deadline", d.deadline)
	}
	if j.ModelStageDurationMS["assessing"] != 480000 || j.ModelAttempts[0].Round != 2 {
		t.Fatal("overwrote first round budget")
	}
}
