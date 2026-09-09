package stockanalysis

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/runtimelog"
)

// Opt-in diagnostic: uses the explicitly configured local runtime, never CI credentials.
func TestResearchLiveToolFreeJSON(t *testing.T) {
	if os.Getenv("EASY_STOCK_LIVE_HERMES") != "1" {
		t.Skip("explicit live-model opt-in required")
	}
	runtime := hermes.NewRuntime(hermes.Config{RuntimeRoot: os.Getenv("A_STOCK_HERMES_RUNTIME_ROOT"), Home: os.Getenv("A_STOCK_HERMES_HOME"), WorkDir: t.TempDir()})
	data, err := os.ReadFile(os.Getenv("A_STOCK_SETTINGS_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		LLM appsettings.LLM `json:"llm"`
	}
	if err = json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if model := os.Getenv("EASY_STOCK_LIVE_MODEL"); model != "" {
		settings.LLM.Model = model
	}
	if err = runtime.SyncLLM(settings.LLM, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := hermes.PromptUsingOptions(ctx, runtime, `只输出JSON，不要解释，不要调用工具：{"status":"ok","language":"zh"}`, hermes.PromptOptions{Sandbox: true, AutoApprove: true, DisableTools: true})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = decodeJSONObject(result.Content, &decoded); err != nil {
		t.Fatalf("tool-free output was not JSON: %s", runtimelog.Redact(truncateExactText(result.Content, 800)))
	}
	if decoded["status"] != "ok" {
		t.Fatalf("unexpected response: %+v", decoded)
	}
}
