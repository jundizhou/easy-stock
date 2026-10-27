package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
)

type stubGateway struct {
	result agent.PromptResult
	err    error
}

func (s *stubGateway) Prompt(context.Context, string) (agent.PromptResult, error) {
	return s.result, s.err
}

type usageOptionsGateway struct{ stubGateway }

func (s *usageOptionsGateway) PromptWithOptions(ctx context.Context, prompt string, _ agent.PromptOptions) (agent.PromptResult, error) {
	return s.Prompt(ctx, prompt)
}

func (s *usageOptionsGateway) PromptWithBrowserState(ctx context.Context, prompt, _ string) (agent.PromptResult, error) {
	return s.Prompt(ctx, prompt)
}
func (s *stubGateway) Status() agent.Status { return agent.Status{} }
func (s *stubGateway) ModelAPIKey() (string, error) {
	return "", nil
}
func (s *stubGateway) SyncLLM(appsettings.LLM, *string) error { return nil }
func (s *stubGateway) Start(context.Context) (agent.Process, error) {
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
	store.add(tokenUsageRequest{Module: "stock-analysis", Model: "deepseek-v4-pro", Prompt: 4, Completion: 5, Total: 9})

	if len(store.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(store.Entries))
	}
}

func TestTokenUsageGatewayMarksMissingUsageAsEstimated(t *testing.T) {
	wrapped := newTokenUsageGateway(&stubGateway{result: agent.PromptResult{Content: "hello world"}}, newTokenUsageStore(filepath.Join(t.TempDir(), "settings.json")))
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
	wrapped := newTokenUsageGateway(&stubGateway{result: agent.PromptResult{
		Content: "hello world",
		Usage:   agent.TokenUsage{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15, Model: "claude-sonnet-4"},
	}}, store)
	wrapped.Prompt(context.Background(), "分析这只股票")

	if len(store.Entries) != 1 || store.Entries[0].Model != "claude-sonnet-4" || store.Entries[0].Total != 15 {
		t.Fatalf("model usage was not recorded: %+v", store.Entries)
	}
}

func TestTokenUsageGatewayAccountsForFailedRequests(t *testing.T) {
	for _, method := range []string{"prompt", "options", "browser"} {
		for _, tc := range []struct {
			name         string
			result       agent.PromptResult
			err          error
			wantReal     int
			wantEstimate bool
		}{
			{name: "session failed before model call", err: errors.New("session.create failed")},
			{name: "canceled", err: context.Canceled},
			{name: "partial output without usage", result: agent.PromptResult{Content: "partial"}, err: context.DeadlineExceeded},
			{name: "failed with real usage", result: agent.PromptResult{Usage: agent.TokenUsage{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15, Model: "test-model"}}, err: context.DeadlineExceeded, wantReal: 15},
			{name: "failed with input usage only", result: agent.PromptResult{Usage: agent.TokenUsage{PromptTokens: 12, Model: "test-model"}}, err: context.DeadlineExceeded, wantReal: 12},
			{name: "successful without usage", result: agent.PromptResult{Content: "response", Usage: agent.TokenUsage{Model: "test-model"}}, wantEstimate: true},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				store := newTokenUsageStore("")
				gateway := newTokenUsageGateway(&usageOptionsGateway{stubGateway{result: tc.result, err: tc.err}}, store).(*tokenUsageGateway)
				ctx := agent.WithUsageModule(context.Background(), "review-analysis")
				var err error
				switch method {
				case "prompt":
					_, err = gateway.Prompt(ctx, "test input")
				case "options":
					_, err = gateway.PromptWithOptions(ctx, "test input", agent.PromptOptions{Sandbox: true})
				case "browser":
					_, err = gateway.PromptWithBrowserState(ctx, "test input", "test-state")
				}
				if !errors.Is(err, tc.err) {
					t.Fatalf("error = %v, want %v", err, tc.err)
				}
				if tc.wantReal == 0 && !tc.wantEstimate {
					if len(store.Entries) != 0 {
						t.Fatalf("failed request created usage: %+v", store.Entries)
					}
					return
				}
				if len(store.Entries) != 1 {
					t.Fatalf("entries = %+v", store.Entries)
				}
				entry := store.Entries[0]
				if entry.Total != tc.wantReal || (entry.EstimatedTotal > 0) != tc.wantEstimate || entry.Module != "review-analysis" || entry.Model != "test-model" {
					t.Fatalf("incorrect usage: %+v", entry)
				}
			})
		}
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

func TestLegacyTokenUsageInferencePreservesAmountsAndOriginalModule(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	usagePath := filepath.Join(filepath.Dir(settingsPath), "token-usage.json")
	original := []tokenUsageEntry{
		{Date: "2026-09-10", Module: "other", Prompt: 90, Completion: 10, Total: 100},
		{Date: "2026-09-11", Module: "other", Model: "test-model", EstimatedPrompt: 20, EstimatedCompletion: 5, EstimatedTotal: 25},
		{Date: "2026-09-11", Module: "stock-analysis", Total: 200},
	}
	payload, err := json.Marshal(map[string]any{"entries": original})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(usagePath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	store := newTokenUsageStore(settingsPath)
	if store.Version != 1 || len(store.Entries) != len(original) {
		t.Fatalf("legacy store = %+v", store)
	}
	for i, entry := range store.Entries {
		want := original[i]
		if want.Module == "other" {
			want.Module, want.OriginalModule = "review-legacy", "other"
		}
		if entry != want {
			t.Fatalf("legacy entry changed unexpectedly: got=%+v want=%+v", entry, want)
		}
	}
	// Reading statistics should not overwrite the original file. The versioned
	// representation is persisted together with the next newly recorded usage.
	after, err := os.ReadFile(usagePath)
	if err != nil || string(after) != string(payload) {
		t.Fatalf("loading overwrote historical data: %v", err)
	}
	server := &Server{tokenUsage: store}
	w := httptest.NewRecorder()
	server.tokenUsageSummary(w, httptest.NewRequest(http.MethodGet, "/api/v1/settings/token-usage?module=review-legacy&period=month", nil))
	var summary struct {
		Data struct {
			Rows  []tokenUsageEntry `json:"rows"`
			Total tokenUsageEntry   `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Data.Rows) != 2 || summary.Data.Total.Total != 100 || summary.Data.Total.EstimatedTotal != 25 {
		t.Fatalf("historical filtering changed totals: %s", w.Body.String())
	}
	for _, entry := range summary.Data.Rows {
		if entry.OriginalModule != "other" || entry.Module != "review-legacy" {
			t.Fatalf("summary lost attribution provenance: %+v", entry)
		}
	}
	store.add(tokenUsageRequest{Module: "other", Prompt: 3, Completion: 2})
	reloaded := newTokenUsageStore(settingsPath)
	if !reflect.DeepEqual(store.Entries, reloaded.Entries) {
		t.Fatalf("reload reclassified new unknown usage: before=%+v after=%+v", store.Entries, reloaded.Entries)
	}
	last := reloaded.Entries[len(reloaded.Entries)-1]
	if last.Module != "other" || last.OriginalModule != "" {
		t.Fatalf("new unknown usage must not be inferred as a review: %+v", last)
	}
}

// usageTotal 读取指定模块在当天记录里的总量。
func usageTotal(store *tokenUsageStore, module string) int {
	total := 0
	for _, entry := range store.Entries {
		if entry.Module == module {
			total += entry.Total
		}
	}
	return total
}

// 两个实例并存时，后写的那份不能把先写的用量抹掉。
//
// 2026-10-21 上午 47 万输入 token 就是这么消失的：11:20 还能读到 module=other
// 的记录，11:43 文件被另一次写入覆盖后只剩下 cockpit-patrol。原因是 persist
// 把内存里的整份快照覆盖上去，而旧进程的内存里根本没有新进程记的账。
func TestTokenUsagePersistMergesStaleInstance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	stale := newTokenUsageStore(path)
	stale.add(tokenUsageRequest{Module: "monitor", Prompt: 1000, Completion: 100, Total: 1100})

	fresh := newTokenUsageStore(path)
	fresh.add(tokenUsageRequest{Module: "monitor", Prompt: 900, Completion: 90, Total: 990})
	if got := usageTotal(newTokenUsageStore(path), "monitor"); got != 2090 {
		t.Fatalf("两笔应累加为 2090，实际 %d", got)
	}

	// 陈旧实例继续记账：它内存里只有 1100，直接覆盖会退化成 1320。
	stale.add(tokenUsageRequest{Module: "monitor", Prompt: 200, Completion: 20, Total: 220})
	if got := usageTotal(newTokenUsageStore(path), "monitor"); got != 2090 {
		t.Fatalf("陈旧实例写入后总量应保持 2090，实际 %d", got)
	}
}

// 文件被写坏时不能静默当成空库：那样下一次写入会把整份历史清掉。
func TestTokenUsageCorruptFileIsBackedUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token-usage.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"entries":[`), 0o600); err != nil {
		t.Fatalf("准备损坏文件失败：%v", err)
	}
	store := newTokenUsageStore(filepath.Join(dir, "settings.json"))
	if len(store.Entries) != 0 {
		t.Fatalf("损坏文件不应解析出条目，实际 %d", len(store.Entries))
	}
	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatalf("查找备份失败：%v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("损坏文件应留一份备份，实际 %d 份", len(matches))
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("原子写完成后不应残留临时文件")
	}
}
