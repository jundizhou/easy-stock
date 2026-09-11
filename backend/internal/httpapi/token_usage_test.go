package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/hermes"
)

type stubGateway struct {
	result hermes.PromptResult
}

func (s *stubGateway) Prompt(context.Context, string) (hermes.PromptResult, error) {
	return s.result, nil
}
func (s *stubGateway) Status() hermes.Status { return hermes.Status{} }
func (s *stubGateway) ModelAPIKey() (string, error) {
	return "", nil
}
func (s *stubGateway) SyncLLM(appsettings.LLM, *string) error { return nil }
func (s *stubGateway) Start(context.Context) (hermes.Process, error) {
	return nil, nil
}

func TestTokenUsageStoreSeparatesEstimatedFromReal(t *testing.T) {
	store := newTokenUsageStore(filepath.Join(t.TempDir(), "settings.json"))
	store.add(tokenUsageRequest{Module: "stock-analysis", Model: "kimi-k3", Prompt: 900, Completion: 250, Total: 1150})
	store.add(tokenUsageRequest{Module: "stock-analysis", Model: "kimi-k3", Prompt: 200, Completion: 40, Estimated: true})

	if len(store.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(store.Entries))
	}
	entry := store.Entries[0]
	if entry.Prompt != 900 || entry.Completion != 250 || entry.Total != 1150 {
		t.Fatalf("real usage mixed with estimate: %+v", entry)
	}
	if entry.EstimatedPrompt != 200 || entry.EstimatedCompletion != 40 || entry.EstimatedTotal != 240 {
		t.Fatalf("estimated usage not tracked: %+v", entry)
	}
	if entry.Model != "kimi-k3" {
		t.Fatalf("model = %q, want kimi-k3", entry.Model)
	}
}

func TestTokenUsageStoreSeparatesModels(t *testing.T) {
	store := newTokenUsageStore(filepath.Join(t.TempDir(), "settings.json"))
	store.add(tokenUsageRequest{Module: "stock-analysis", Model: "kimi-k3", Prompt: 1, Completion: 2, Total: 3})
	store.add(tokenUsageRequest{Module: "stock-analysis", Model: "deepseek-chat", Prompt: 4, Completion: 5, Total: 9})

	if len(store.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(store.Entries))
	}
}

func TestTokenUsageGatewayMarksMissingUsageAsEstimated(t *testing.T) {
	wrapped := newTokenUsageGateway(&stubGateway{result: hermes.PromptResult{Content: "hello world"}}, newTokenUsageStore(filepath.Join(t.TempDir(), "settings.json")))
	gateway := wrapped.(*tokenUsageGateway)
	gateway.Prompt(context.Background(), "分析这只股票")

	if len(gateway.store.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(gateway.store.Entries))
	}
	entry := gateway.store.Entries[0]
	if entry.Total != 0 || entry.EstimatedTotal == 0 {
		t.Fatalf("missing usage should stay in estimated fields: %+v", entry)
	}
}

func TestTokenUsageGatewayRecordsProviderModel(t *testing.T) {
	store := newTokenUsageStore(filepath.Join(t.TempDir(), "settings.json"))
	wrapped := newTokenUsageGateway(&stubGateway{result: hermes.PromptResult{
		Content: "hello world",
		Usage:   hermes.TokenUsage{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15, Model: "claude-sonnet-4"},
	}}, store)
	wrapped.Prompt(context.Background(), "分析这只股票")

	if len(store.Entries) != 1 || store.Entries[0].Model != "claude-sonnet-4" || store.Entries[0].Total != 15 {
		t.Fatalf("model usage was not recorded: %+v", store.Entries)
	}
}

func TestTokenUsageSummarySplitsRealAndEstimated(t *testing.T) {
	server := &Server{tokenUsage: newTokenUsageStore(filepath.Join(t.TempDir(), "settings.json"))}
	server.tokenUsage.add(tokenUsageRequest{Module: "ai-chat", Model: "kimi-k3", Prompt: 100, Completion: 20, Total: 120})
	server.tokenUsage.add(tokenUsageRequest{Module: "ai-chat", Model: "kimi-k3", Prompt: 50, Completion: 10, Estimated: true})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/settings/token-usage", nil)
	server.tokenUsageSummary(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("summary status = %d", w.Code)
	}
	var payload struct {
		Data struct {
			Rows []struct {
				Model          string `json:"model"`
				Total          int    `json:"total_tokens"`
				EstimatedTotal int    `json:"estimated_total_tokens"`
			} `json:"rows"`
			Models []string `json:"models"`
			Total  struct {
				Total          int `json:"total_tokens"`
				EstimatedTotal int `json:"estimated_total_tokens"`
			} `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(payload.Data.Rows))
	}
	if payload.Data.Rows[0].Model != "kimi-k3" || payload.Data.Rows[0].Total != 120 || payload.Data.Rows[0].EstimatedTotal != 60 {
		t.Fatalf("row split wrong: %+v", payload.Data.Rows[0])
	}
	if len(payload.Data.Models) != 1 || payload.Data.Models[0] != "kimi-k3" {
		t.Fatalf("models = %+v", payload.Data.Models)
	}
	if payload.Data.Total.Total != 120 || payload.Data.Total.EstimatedTotal != 60 {
		t.Fatalf("total split wrong: %+v", payload.Data.Total)
	}
}
