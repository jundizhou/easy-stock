package portfolioinspection

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
)

// Opt-in isolated model replay: read a saved inspection, never modify the live
// store/settings/runtime home or the user's running optimization job.
func TestLiveSavedEarningsInspection(t *testing.T) {
	if os.Getenv("EASY_STOCK_LIVE_EARNINGS_INSPECTION") != "1" {
		t.Skip("live replay is opt-in")
	}
	data, err := os.ReadFile(os.Getenv("EASY_STOCK_EARNINGS_PORTFOLIO_AUDIT"))
	if err != nil {
		t.Fatal(err)
	}
	var job Job
	if err = json.Unmarshal(data, &job); err != nil || job.Report == nil {
		t.Fatal("missing saved report", err)
	}
	settings, err := os.ReadFile(os.Getenv("EASY_STOCK_REPLAY_SETTINGS"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		LLM     appsettings.LLM `json:"llm"`
		Runtime string          `json:"agent_runtime"`
	}
	if err = json.Unmarshal(settings, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime != "hermes" {
		t.Skip("this replay requires the configured Hermes runtime")
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
		if err = os.WriteFile(filepath.Join(home, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	runtime := agent.NewHermesRuntime(agent.HermesConfig{RuntimeRoot: os.Getenv("EASY_STOCK_REPLAY_HERMES_ROOT"), Home: home, WorkDir: home})
	if err = runtime.SyncLLM(cfg.LLM, nil); err != nil {
		t.Fatal(err)
	}
	report := job.Report
	prompt, err := buildScoringPrompt(report.Request, report.Holdings, report.Metrics, report.Profile)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	response, err := agent.PromptUsingOptions(ctx, runtime, prompt, agent.PromptOptions{Sandbox: true, AutoApprove: true, DisableTools: true, MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	result, err := decodeScoringReport(response.Content)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateScoringReport(&result, report.Request, report.Holdings, report.Metrics); err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("EASY_STOCK_EARNINGS_REPLAY_OUTPUT"); path != "" {
		encoded, _ := json.MarshalIndent(result, "", "  ")
		if err = os.WriteFile(path, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("model=%s summary=%s risks=%v adjustments=%v", cfg.LLM.Model, result.ExecutiveSummary, result.PrimaryRisks, result.AdjustmentOrder)
}
