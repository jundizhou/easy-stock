package portfolioinspection

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
)

// Opt-in real-model aggregation of ten distinct saved research reports. All
// runtime writes, model artifacts and checkpoints stay in isolated directories.
// This exercises production compaction, scoring, repair and validation without
// launching research or changing the user's holdings and live database.
func TestLiveTenHoldingCompaction(t *testing.T) {
	if os.Getenv("EASY_STOCK_LIVE_TEN_HOLDINGS") != "1" {
		t.Skip("real model test is opt-in")
	}
	data, err := os.ReadFile(os.Getenv("EASY_STOCK_TEN_HOLDINGS_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture Report
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	request, err := normalizeRequest(fixture.Request)
	if err != nil || len(request.Holdings) != 10 || len(fixture.Holdings) != 10 {
		t.Fatal("exactly ten valid distinct holdings required", err)
	}
	seen := map[string]bool{}
	for i, r := range fixture.Holdings {
		if !validHoldingResearch(r) || r.Analysis.Symbol != r.Holding.Symbol || r.Holding.Symbol != request.Holdings[i].Symbol || r.Holding.Weight != request.Holdings[i].Weight || seen[r.Holding.Symbol] {
			t.Fatalf("invalid saved holding %d", i)
		}
		seen[r.Holding.Symbol] = true
	}
	out := os.Getenv("EASY_STOCK_TEN_HOLDINGS_OUTPUT")
	if out == "" {
		t.Fatal("private output directory required")
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(out, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, value any) {
		t.Helper()
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err == nil {
			err = os.WriteFile(filepath.Join(out, name), encoded, 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	rules, _ := RulesFor(request.TraderProfile)
	metrics := metricsForReport(request, fixture.Holdings, rules)
	at := fixture.EvaluatedAt
	resuming := os.Getenv("EASY_STOCK_TEN_HOLDINGS_RESUME") == "1"
	if resuming {
		prior, err := os.ReadFile(filepath.Join(out, "preflight.json"))
		var saved struct {
			EvaluatedAt time.Time `json:"evaluated_at"`
		}
		if err != nil || json.Unmarshal(prior, &saved) != nil || saved.EvaluatedAt.IsZero() {
			t.Fatal("resume requires the prior frozen evaluation time", err)
		}
		at = saved.EvaluatedAt
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	instructions, input, mode, err := buildScoringPromptParts(request, fixture.Holdings, metrics, rules, at)
	if err != nil || mode == "legacy" || mode == "shadow" {
		t.Fatal("production decision-card input required", err)
	}
	prompt, err := renderCompactScoringPrompt(instructions, input)
	if err != nil {
		t.Fatal(err)
	}
	write("preflight.json", map[string]any{"holdings": request.Holdings, "evaluated_at": at, "initial_bytes": len(prompt), "target_bytes": targetScoringPromptBytes, "hard_limit_bytes": maxScoringPromptBytes})
	t.Logf("holdings=10 initial_bytes=%d target_bytes=%d hard_limit_bytes=%d", len(prompt), targetScoringPromptBytes, maxScoringPromptBytes)
	if os.Getenv("EASY_STOCK_TEN_HOLDINGS_PREFLIGHT") == "1" {
		return
	}
	settings, err := os.ReadFile(os.Getenv("EASY_STOCK_REPLAY_SETTINGS"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		LLM     appsettings.LLM `json:"llm"`
		Runtime string          `json:"agent_runtime"`
	}
	if err := json.Unmarshal(settings, &cfg); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	for _, name := range []string{"config.yaml", ".env", "model-capabilities.json"} {
		content, readErr := os.ReadFile(filepath.Join(os.Getenv("EASY_STOCK_REPLAY_HERMES_HOME"), name))
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := os.WriteFile(filepath.Join(home, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	runtime := agent.NewService(agent.ServiceConfig{
		// A stable isolated work directory keeps the rendered model config
		// identity stable across restarts; credentials still use a temp home.
		Hermes: agent.HermesConfig{RuntimeRoot: os.Getenv("EASY_STOCK_REPLAY_HERMES_ROOT"), Home: home, WorkDir: out},
		Codex:  agent.CodexConfig{RuntimeRoot: os.Getenv("EASY_STOCK_REPLAY_CODEX_ROOT"), Home: filepath.Join(home, "codex"), WorkDir: out},
	})
	if err := runtime.SyncLLM(cfg.LLM, nil); err != nil {
		t.Fatal(err)
	}
	runtime.RestoreSelection(cfg.Runtime)
	status := runtime.Status()
	if !status.Available || !status.Configured {
		t.Fatalf("isolated runtime unavailable: %s", status.Message)
	}
	t.Logf("runtime=%s model=%s", runtime.ActiveRuntime(), cfg.LLM.Model)
	ctx, release, err := runtime.BindTask(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	store, err := OpenStore(filepath.Join(out, "checkpoints.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	previousID := fmt.Sprintf("live-ten-%x", sha256.Sum256(data))
	jobID := fmt.Sprintf("%s-%d", previousID, time.Now().UnixNano())
	var seed *scoringAttempt
	if resuming {
		var prior struct {
			JobID string `json:"job_id"`
		}
		if data, readErr := os.ReadFile(filepath.Join(out, "run.json")); readErr == nil {
			if json.Unmarshal(data, &prior) != nil || prior.JobID == "" {
				t.Fatal("invalid prior run identity")
			}
			previousID = prior.JobID
		} else if !os.IsNotExist(readErr) {
			t.Fatal(readErr)
		}
		seed, err = store.lastScoringAttempt(ctx, previousID)
		if err != nil || seed == nil {
			t.Fatal("resume requires a saved scoring attempt", err)
		}
		if err := store.copyCompactionCheckpoints(ctx, previousID, jobID); err != nil {
			t.Fatal(err)
		}
	}
	write("run.json", map[string]any{"job_id": jobID, "resumed_from": previousID})
	ctx = context.WithValue(ctx, compactionHooksKey{}, store.compactionHooks(jobID, func(message string) { t.Log(message) }))
	gateway := &tenHoldingLiveGateway{Service: runtime, dir: out, log: t.Logf}
	s := NewService(store, gateway, nil, log.New(os.Stdout, "", 0))
	defer s.Close()
	started := time.Now()
	result, scoreErr := s.scorePortfolioWithAudit(ctx, request, fixture.Holdings, metrics, rules, at, seed, func(attempt scoringAttempt) error {
		t.Logf("scoring_attempt=%d validation_issues=%d", attempt.Number, len(attempt.Issues))
		return store.saveScoringAttempt(context.Background(), jobID, attempt)
	})
	summary := map[string]any{"model": cfg.LLM.Model, "runtime": runtime.ActiveRuntime(), "resumed": resuming, "initial_bytes": len(prompt), "duration_ms": time.Since(started).Milliseconds(), "calls": gateway.calls, "holdings": len(result.Holdings), "score": result.TotalScore, "score_available": result.ScoreAvailable, "compaction_notice": strings.Contains(strings.Join(result.DataLimitations, "\n"), compactionNotice)}
	if scoreErr != nil {
		summary["error"] = scoreErr.Error()
	}
	write("summary.json", summary)
	write("result.json", result)
	if scoreErr != nil {
		t.Fatal(scoreErr)
	}
	if len(result.Holdings) != 10 || !result.ScoreAvailable {
		t.Fatal("validated report did not cover ten holdings")
	}
	if len(prompt) > targetScoringPromptBytes && !summary["compaction_notice"].(bool) {
		t.Fatal("missing automatic compaction notice")
	}
	// Also exercise checkpoint reuse with the exact bound configuration and
	// assert it needs no model calls after a successful real aggregation.
	beforeReuse := len(gateway.calls)
	if _, _, err := s.prepareScoringPrompt(ctx, request, fixture.Holdings, metrics, rules, at, aggregationBudgetFromContext(ctx)); err != nil {
		t.Fatal(err)
	}
	if len(gateway.calls) != beforeReuse {
		t.Fatal("unchanged successful summaries were regenerated")
	}
	summary["checkpoint_reuse_without_calls"] = true
	write("summary.json", summary)
	t.Logf("completed holdings=%d score=%d calls=%d elapsed=%s", len(result.Holdings), result.HealthScore, len(gateway.calls), time.Since(started).Round(time.Second))
}

type tenHoldingLiveCall struct {
	Node              string           `json:"node"`
	InputBytes        int              `json:"input_bytes"`
	OutputBytes       int              `json:"output_bytes"`
	DurationMS        int64            `json:"duration_ms"`
	CompactedHoldings int              `json:"compacted_holdings,omitempty"`
	Usage             agent.TokenUsage `json:"usage"`
	Error             string           `json:"error,omitempty"`
}

type tenHoldingLiveGateway struct {
	*agent.Service
	dir   string
	log   func(string, ...any)
	calls []tenHoldingLiveCall
}

func (g *tenHoldingLiveGateway) PromptWithOptions(ctx context.Context, prompt string, opts agent.PromptOptions) (agent.PromptResult, error) {
	n := len(g.calls) + 1
	call := tenHoldingLiveCall{Node: agent.CallTraceFromContext(ctx).NodeID, InputBytes: len(prompt)}
	if payload := strings.SplitN(prompt, "[组合证据JSON]\n", 2); len(payload) == 2 {
		var input compactScoringInput
		if json.NewDecoder(strings.NewReader(payload[1])).Decode(&input) == nil {
			for _, card := range input.Holdings {
				if card.CompactedResearch != "" {
					call.CompactedHoldings++
				}
			}
		}
	}
	if err := os.WriteFile(filepath.Join(g.dir, fmt.Sprintf("call-%02d-input.txt", n)), []byte(prompt), 0600); err != nil {
		return agent.PromptResult{}, err
	}
	g.log("real call=%d node=%s input_bytes=%d compacted_holdings=%d", n, call.Node, len(prompt), call.CompactedHoldings)
	started, lastLog := time.Now(), time.Time{}
	original := opts.OnProgress
	opts.OnProgress = func(p agent.PromptProgress) {
		if original != nil {
			original(p)
		}
		if lastLog.IsZero() || time.Since(lastLog) >= 30*time.Second {
			g.log("call=%d elapsed_ms=%d text_bytes=%d reasoning_bytes=%d", n, p.ElapsedMS, p.TextBytes, p.ReasoningBytes)
			lastLog = time.Now()
		}
	}
	result, err := g.Service.PromptWithOptions(ctx, prompt, opts)
	call.DurationMS, call.OutputBytes, call.Usage = time.Since(started).Milliseconds(), len(result.Content), result.Usage
	if err != nil {
		call.Error = err.Error()
	}
	g.calls = append(g.calls, call)
	data, saveErr := json.MarshalIndent(map[string]any{"call": call, "result": result}, "", "  ")
	if saveErr == nil {
		saveErr = os.WriteFile(filepath.Join(g.dir, fmt.Sprintf("call-%02d-output.json", n)), data, 0600)
	}
	if saveErr != nil {
		return result, saveErr
	}
	g.log("call=%d finished elapsed_ms=%d output_bytes=%d tokens=%d", n, call.DurationMS, call.OutputBytes, call.Usage.TotalTokens)
	return result, err
}
