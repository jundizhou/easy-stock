package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	po "easy-stock/backend/internal/portfoliooptimization"
)

type evaluationGateway struct {
	*agent.Service
	dir      string
	sequence atomic.Int32
	replay   string
}

func (g *evaluationGateway) PromptWithOptions(ctx context.Context, prompt string, options agent.PromptOptions) (agent.PromptResult, error) {
	n := g.sequence.Add(1)
	input := filepath.Join(g.dir, fmt.Sprintf("call-%02d-input.txt", n))
	if err := os.WriteFile(input, []byte(prompt), 0600); err != nil {
		return agent.PromptResult{}, err
	}
	var result agent.PromptResult
	var err error
	replayed := n == 1 && g.replay != ""
	if replayed {
		var saved struct {
			Result agent.PromptResult `json:"result"`
		}
		var data []byte
		data, err = os.ReadFile(g.replay)
		if err == nil {
			err = json.Unmarshal(data, &saved)
		}
		result = saved.Result
		// This is the exact recorded model output, not a newly charged call.
		result.Usage = agent.TokenUsage{}
	} else {
		result, err = g.Service.PromptWithOptions(ctx, prompt, options)
	}
	record := struct {
		Result   agent.PromptResult `json:"result"`
		Error    string             `json:"error,omitempty"`
		Replayed bool               `json:"replayed,omitempty"`
	}{Result: result, Replayed: replayed}
	if err != nil {
		record.Error = err.Error()
	}
	data, writeErr := json.MarshalIndent(record, "", "  ")
	if writeErr == nil {
		writeErr = os.WriteFile(filepath.Join(g.dir, fmt.Sprintf("call-%02d-output.json", n)), data, 0600)
	}
	if writeErr != nil {
		return result, writeErr
	}
	return result, err
}

// Opt-in end-to-end evaluation with real providers and the configured model.
// Input databases, settings and credentials must be isolated copies. It never
// applies a target portfolio or changes the original user's inspection history.
func TestLivePortfolioOptimizationEvaluation(t *testing.T) {
	if os.Getenv("EASY_STOCK_LIVE_PORTFOLIO_EVALUATION") != "1" {
		t.Skip("explicit live optimization opt-in required")
	}
	dir := os.Getenv("EASY_STOCK_EVALUATION_DIR")
	sourceID := os.Getenv("EASY_STOCK_EVALUATION_SOURCE")
	if dir == "" || sourceID == "" {
		t.Fatal("isolated evaluation directory and source ID required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"portfolio.db", "research.db", "settings.json", "hermes/.env"} {
		if _, err := os.Stat(filepath.Join(abs, name)); err != nil {
			t.Fatal(err)
		}
	}
	g := agent.NewService(agent.ServiceConfig{
		Hermes: agent.HermesConfig{RuntimeRoot: os.Getenv("EASY_STOCK_REPLAY_HERMES_ROOT"), Home: filepath.Join(abs, "hermes"), WorkDir: abs},
		Codex:  agent.CodexConfig{RuntimeRoot: os.Getenv("EASY_STOCK_REPLAY_CODEX_ROOT"), Home: filepath.Join(abs, "codex"), WorkDir: abs},
	})
	settings, err := os.ReadFile(filepath.Join(abs, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var selection struct {
		Runtime string `json:"agent_runtime"`
	}
	if err = json.Unmarshal(settings, &selection); err != nil {
		t.Fatal(err)
	}
	g.RestoreSelection(selection.Runtime)
	diagnosticDir := os.Getenv("EASY_STOCK_EVALUATION_CALLS")
	if diagnosticDir == "" {
		diagnosticDir = filepath.Join(abs, "calls")
	}
	if err = os.MkdirAll(diagnosticDir, 0700); err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{PortfolioDBPath: filepath.Join(abs, "portfolio.db"), StockResearchDBPath: filepath.Join(abs, "research.db"), SettingsPath: filepath.Join(abs, "settings.json"), AgentGateway: &evaluationGateway{Service: g, dir: diagnosticDir, replay: os.Getenv("EASY_STOCK_EVALUATION_REPLAY_FIRST")}, StrictPersistence: true})
	defer s.Close()
	if err := s.StartupError(); err != nil {
		t.Fatal(err)
	}
	job, err := s.portfolioOptimization.Start(context.Background(), sourceID, po.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if job.ResumeAvailable && os.Getenv("EASY_STOCK_EVALUATION_REPLAY_FIRST") != "" {
		job, err = s.portfolioOptimization.Resume(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("resuming with the exact saved proposal response; later calls use the real model")
	}
	t.Logf("evaluation job=%s source=%s version=%s", job.ID, sourceID, job.Version)
	output := os.Getenv("EASY_STOCK_EVALUATION_OUTPUT")
	if output == "" {
		output = filepath.Join(abs, "result.json")
	}
	save := func(j po.Job) {
		data, err := json.MarshalIndent(j, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.NewTimer(po.TotalTimeout + time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		job, err = s.portfolioOptimization.Get(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		save(job)
		t.Logf("stage=%s status=%s stocks=%d reused=%d new=%d prompt=%d elapsed_model_ms=%d text=%d reasoning=%d message=%s", job.Stage, job.Status, len(job.Results), job.ReusedStocks, job.NewStocks, job.ModelPromptBytes, job.ModelDurationMS, job.ModelProgress.TextBytes, job.ModelProgress.ReasoningBytes, job.Message)
		if job.Status != "running" {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("evaluation exceeded total runtime")
		}
	}
	for _, attempt := range job.ModelAttempts {
		t.Logf("attempt stage=%s input=%d output=%d duration_ms=%d error=%s", attempt.Stage, attempt.PromptBytes, attempt.ResponseBytes, attempt.DurationMS, attempt.Error)
	}
	if job.Status != "succeeded" {
		t.Fatalf("optimization incomplete: %s", job.Error)
	}
	selected, groups := 0, map[string]bool{}
	if len(job.Candidates) > po.MaxCandidates {
		t.Fatal("candidate pool exceeded limit")
	}
	for _, c := range job.Candidates {
		if c.Selected {
			if c.IndustryGroup == "" || groups[c.IndustryGroup] || c.Screening == nil || !c.Screening.Qualified {
				t.Fatal("selected candidate did not meet screening/diversification rules", c.Symbol)
			}
			groups[c.IndustryGroup] = true
			selected++
		}
	}
	if selected > po.MaxCandidateResearch {
		t.Fatal("too many candidate studies")
	}
	if job.SelectedPlan != nil {
		p := job.Plans[*job.SelectedPlan]
		if !p.Checks.Valid || p.Assessment == nil || !p.Assessment.Accepted || len(p.Improvements) == 0 {
			t.Fatal("invalid adopted plan")
		}
		check := po.Check(job.Baseline, p.Target)
		if !check.Valid {
			t.Fatal("adopted plan violates conservation/change budget", check.Errors)
		}
	}
	t.Logf("completed outcome=%s reason=%s selected=%v", job.Outcome, job.OutcomeReason, job.SelectedPlan != nil)
}
