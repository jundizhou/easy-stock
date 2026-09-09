package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/hermes"
)

type tokenUsageEntry struct {
	Date       string `json:"date"`
	Module     string `json:"module"`
	Prompt     int    `json:"prompt_tokens"`
	Completion int    `json:"completion_tokens"`
	Total      int    `json:"total_tokens"`
}
type tokenUsageStore struct {
	mu      sync.Mutex
	path    string
	Entries []tokenUsageEntry `json:"entries"`
}
type tokenUsageRequest struct {
	Module     string `json:"module"`
	Prompt     int    `json:"prompt_tokens"`
	Completion int    `json:"completion_tokens"`
	Total      int    `json:"total_tokens"`
}

func newTokenUsageStore(settingsPath string) *tokenUsageStore {
	path := ""
	if strings.TrimSpace(settingsPath) != "" {
		path = filepath.Join(filepath.Dir(settingsPath), "token-usage.json")
	}
	s := &tokenUsageStore{path: path}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, s)
		}
	}
	return s
}
func (s *tokenUsageStore) add(req tokenUsageRequest) {
	if req.Total <= 0 {
		req.Total = req.Prompt + req.Completion
	}
	if req.Total <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	date := time.Now().Format("2006-01-02")
	for i := range s.Entries {
		if s.Entries[i].Date == date && s.Entries[i].Module == req.Module {
			s.Entries[i].Prompt += req.Prompt
			s.Entries[i].Completion += req.Completion
			s.Entries[i].Total += req.Total
			s.persist()
			return
		}
	}
	s.Entries = append(s.Entries, tokenUsageEntry{Date: date, Module: req.Module, Prompt: req.Prompt, Completion: req.Completion, Total: req.Total})
	s.persist()
}
func (s *tokenUsageStore) persist() {
	if strings.TrimSpace(s.path) == "" {
		return
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	_ = os.MkdirAll(filepath.Dir(s.path), 0o700)
	_ = os.WriteFile(s.path, data, 0o600)
}

type tokenUsageGateway struct {
	hermes.Gateway
	store *tokenUsageStore
}

func newTokenUsageGateway(gateway hermes.Gateway, store *tokenUsageStore) hermes.Gateway {
	if gateway == nil {
		return nil
	}
	return &tokenUsageGateway{Gateway: gateway, store: store}
}

func (g *tokenUsageGateway) Prompt(ctx context.Context, prompt string) (hermes.PromptResult, error) {
	result, err := g.Gateway.Prompt(ctx, prompt)
	g.record(ctx, prompt, result)
	return result, err
}

func (g *tokenUsageGateway) PromptWithOptions(ctx context.Context, prompt string, options hermes.PromptOptions) (hermes.PromptResult, error) {
	prompter, ok := g.Gateway.(interface {
		PromptWithOptions(context.Context, string, hermes.PromptOptions) (hermes.PromptResult, error)
	})
	if !ok {
		return g.Prompt(ctx, prompt)
	}
	result, err := prompter.PromptWithOptions(ctx, prompt, options)
	g.record(ctx, prompt, result)
	return result, err
}

func (g *tokenUsageGateway) PromptWithBrowserState(ctx context.Context, prompt, statePath string) (hermes.PromptResult, error) {
	prompter, ok := g.Gateway.(hermes.BrowserStatePrompter)
	if !ok {
		return hermes.PromptResult{}, fmt.Errorf("Hermes 不支持浏览器登录态提示词")
	}
	result, err := prompter.PromptWithBrowserState(ctx, prompt, statePath)
	g.record(ctx, prompt, result)
	return result, err
}

func (g *tokenUsageGateway) AgentSettings() (hermes.AgentSettings, error) {
	gateway, ok := g.Gateway.(hermes.SettingsGateway)
	if !ok {
		return hermes.AgentSettings{}, fmt.Errorf("Hermes 设置接口不可用")
	}
	return gateway.AgentSettings()
}

func (g *tokenUsageGateway) SyncAgentSettings(settings hermes.AgentSettings) error {
	gateway, ok := g.Gateway.(hermes.SettingsGateway)
	if !ok {
		return fmt.Errorf("Hermes 设置接口不可用")
	}
	return gateway.SyncAgentSettings(settings)
}

func (g *tokenUsageGateway) SyncLLMProfile(cfg appsettings.LLM, profileID string, keyUpdate *string) error {
	gateway, ok := g.Gateway.(hermes.ProfileGateway)
	if !ok {
		return fmt.Errorf("Hermes 模型配置接口不可用")
	}
	return gateway.SyncLLMProfile(cfg, profileID, keyUpdate)
}

func (g *tokenUsageGateway) StoreLLMProfileKey(profileID string, keyUpdate *string) error {
	gateway, ok := g.Gateway.(hermes.ProfileGateway)
	if !ok {
		return fmt.Errorf("Hermes 模型密钥接口不可用")
	}
	return gateway.StoreLLMProfileKey(profileID, keyUpdate)
}

func (g *tokenUsageGateway) ModelAPIKeyForProfile(profileID string) (string, error) {
	gateway, ok := g.Gateway.(hermes.ProfileGateway)
	if !ok {
		return "", fmt.Errorf("Hermes 模型密钥接口不可用")
	}
	return gateway.ModelAPIKeyForProfile(profileID)
}

func (g *tokenUsageGateway) record(ctx context.Context, prompt string, result hermes.PromptResult) {
	if g.store == nil {
		return
	}
	usage := result.Usage
	if usage.TotalTokens <= 0 {
		usage = estimateTokenUsage(prompt, result.Content)
	}
	if usage.TotalTokens <= 0 {
		return
	}
	module := hermes.UsageModule(ctx)
	if module == "" {
		module = "other"
	}
	g.store.add(tokenUsageRequest{Module: module, Prompt: usage.PromptTokens, Completion: usage.CompletionTokens, Total: usage.TotalTokens})
}

// Some compatible providers omit usage from their response. Keep the statistic
// useful with a conservative text estimate until provider usage is available.
func estimateTokenUsage(prompt, content string) hermes.TokenUsage {
	promptTokens := estimateTextTokens(prompt)
	completionTokens := estimateTextTokens(content)
	return hermes.TokenUsage{PromptTokens: promptTokens, CompletionTokens: completionTokens, TotalTokens: promptTokens + completionTokens}
}

func estimateTextTokens(text string) int {
	runes := len([]rune(strings.TrimSpace(text)))
	if runes == 0 {
		return 0
	}
	return max(1, (runes+1)/2)
}
func (s *Server) tokenUsageRecord(w http.ResponseWriter, r *http.Request) {
	var req tokenUsageRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.Module) == "" {
		writeError(w, http.StatusBadRequest, "invalid token usage")
		return
	}
	s.tokenUsage.add(req)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) tokenUsageSummary(w http.ResponseWriter, r *http.Request) {
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	module := r.URL.Query().Get("module")
	period := r.URL.Query().Get("period")
	s.tokenUsage.mu.Lock()
	defer s.tokenUsage.mu.Unlock()
	type row struct {
		Date       string `json:"date"`
		Module     string `json:"module"`
		Prompt     int    `json:"prompt_tokens"`
		Completion int    `json:"completion_tokens"`
		Total      int    `json:"total_tokens"`
	}
	rows := []row{}
	index := map[string]int{}
	modules := map[string]bool{}
	total := row{}
	for _, e := range s.tokenUsage.Entries {
		if from != "" && e.Date < from || to != "" && e.Date > to || module != "" && module != e.Module {
			continue
		}
		date := e.Date
		if period == "month" && len(date) >= 7 {
			date = date[:7]
		}
		key := date + "\x00" + e.Module
		if existing, ok := index[key]; ok {
			rows[existing].Prompt += e.Prompt
			rows[existing].Completion += e.Completion
			rows[existing].Total += e.Total
		} else {
			index[key] = len(rows)
			rows = append(rows, row{date, e.Module, e.Prompt, e.Completion, e.Total})
		}
		modules[e.Module] = true
		total.Prompt += e.Prompt
		total.Completion += e.Completion
		total.Total += e.Total
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Date < rows[j].Date })
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"rows": rows, "modules": sortedKeys(modules), "total": total}})
}
func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for k := range values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
