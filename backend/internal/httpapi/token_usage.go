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

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
)

type tokenUsageEntry struct {
	Runtime             string `json:"runtime,omitempty"`
	Date                string `json:"date"`
	Module              string `json:"module"`
	OriginalModule      string `json:"original_module,omitempty"`
	Model               string `json:"model,omitempty"`
	Prompt              int    `json:"prompt_tokens"`
	Completion          int    `json:"completion_tokens"`
	Total               int    `json:"total_tokens"`
	EstimatedPrompt     int    `json:"estimated_prompt_tokens"`
	EstimatedCompletion int    `json:"estimated_completion_tokens"`
	EstimatedTotal      int    `json:"estimated_total_tokens"`
}
type tokenUsageFile struct {
	Version int               `json:"version"`
	Entries []tokenUsageEntry `json:"entries"`
}

type tokenUsageStore struct {
	mu      sync.Mutex
	path    string
	Version int               `json:"version"`
	Entries []tokenUsageEntry `json:"entries"`
	calls   map[string]tokenCall
}
type tokenUsageRequest struct {
	Runtime    string `json:"runtime,omitempty"`
	Module     string `json:"module"`
	Model      string `json:"model,omitempty"`
	Prompt     int    `json:"prompt_tokens"`
	Completion int    `json:"completion_tokens"`
	Total      int    `json:"total_tokens"`
	Estimated  bool   `json:"estimated"`
}

func newTokenUsageStore(settingsPath string) *tokenUsageStore {
	path := ""
	if strings.TrimSpace(settingsPath) != "" {
		path = filepath.Join(filepath.Dir(settingsPath), "token-usage.json")
	}
	s := &tokenUsageStore{path: path, Version: 1}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			var stored tokenUsageStore
			if json.Unmarshal(data, &stored) == nil {
				s.Entries = stored.Entries
				s.Version = max(1, stored.Version)
				if stored.Version == 0 {
					// In the unversioned application, review automation was the
					// only built-in caller missing module tags. This is a broad
					// historical inference, not per-request attribution. Retain
					// the original module and never apply it to new unknown calls.
					for i := range s.Entries {
						if s.Entries[i].Module == "other" {
							s.Entries[i].OriginalModule = "other"
							s.Entries[i].Module = "review-legacy"
						}
					}
				}
			} else {
				// 解析失败时不要静默当成空库：那样下一次写入会把整份历史覆盖掉。
				// 先留一份带时间戳的副本，出问题时还有东西可查。
				backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
				if writeErr := os.WriteFile(backup, data, 0o600); writeErr != nil {
					fmt.Printf("token usage: 备份损坏文件失败 %v\n", writeErr)
				} else {
					fmt.Printf("token usage: 文件损坏，已备份到 %s\n", backup)
				}
			}
		}
	}
	s.loadCalls()
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
	model := strings.TrimSpace(req.Model)
	apply := func(entry *tokenUsageEntry) {
		if req.Estimated {
			entry.EstimatedPrompt += req.Prompt
			entry.EstimatedCompletion += req.Completion
			entry.EstimatedTotal += req.Total
			return
		}
		entry.Prompt += req.Prompt
		entry.Completion += req.Completion
		entry.Total += req.Total
	}
	for i := range s.Entries {
		if s.Entries[i].Date == date && s.Entries[i].Module == req.Module && s.Entries[i].Model == model && s.Entries[i].Runtime == req.Runtime {
			apply(&s.Entries[i])
			s.persist()
			return
		}
	}
	entry := tokenUsageEntry{Runtime: req.Runtime, Date: date, Module: req.Module, Model: model}
	apply(&entry)
	s.Entries = append(s.Entries, entry)
	s.persist()
}
func (s *tokenUsageStore) persist() {
	if strings.TrimSpace(s.path) == "" {
		return
	}
	// 写之前先合并磁盘上的最新版本。
	//
	// 之前这里是"把内存里的整份快照覆盖上去"，两次实例并存时（旧进程没退干净，
	// 或同一份数据被两个进程打开）后写的那份会把先写的用量整段抹掉：
	// 2026-10-21 上午 47 万输入 token 的"other"记录就是这么消失的，
	// 11:20 还能读到，11:43 文件被另一次写入覆盖后就只剩 cockpit-patrol。
	// 用量在同一 (日期,模块,模型,运行时) 下只增不减，所以逐字段取较大值：
	// 既不会重复累加，也不会丢掉别人记的账。
	if disk, err := s.readFile(); err == nil {
		for _, entry := range disk.Entries {
			if index := s.indexOf(entry); index >= 0 {
				s.Entries[index].keepMax(entry)
			} else if entry.Total > 0 || entry.EstimatedTotal > 0 {
				s.Entries = append(s.Entries, entry)
			}
		}
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	_ = os.MkdirAll(filepath.Dir(s.path), 0o700)
	// 原子写：WriteFile 会先截断再写，读端在这一瞬间拿到半截 JSON，
	// 解析失败就按空库处理，接着把自己那份写回去——历史就是这么丢的。
	// 先写临时文件再改名，读端要么看到旧文件，要么看到完整的新文件。
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
	}
}

// indexOf 定位同一统计维度（日期 + 模块 + 模型 + 运行时）的条目。
func (s *tokenUsageStore) indexOf(entry tokenUsageEntry) int {
	for i := range s.Entries {
		e := s.Entries[i]
		if e.Date == entry.Date && e.Module == entry.Module && e.Model == entry.Model && e.Runtime == entry.Runtime {
			return i
		}
	}
	return -1
}

// keepMax 逐字段取较大值，用于合并另一实例写下的同一维度用量。
func (e *tokenUsageEntry) keepMax(other tokenUsageEntry) {
	e.Prompt = max(e.Prompt, other.Prompt)
	e.Completion = max(e.Completion, other.Completion)
	e.Total = max(e.Total, other.Total)
	e.EstimatedPrompt = max(e.EstimatedPrompt, other.EstimatedPrompt)
	e.EstimatedCompletion = max(e.EstimatedCompletion, other.EstimatedCompletion)
	e.EstimatedTotal = max(e.EstimatedTotal, other.EstimatedTotal)
}

// readFile 读取并解析当前的用量文件，不触碰内存状态。
// 用独立的落盘结构而不是 tokenUsageStore：后者带互斥锁，按值返回会复制锁。
func (s *tokenUsageStore) readFile() (tokenUsageFile, error) {
	var stored tokenUsageFile
	data, err := os.ReadFile(s.path)
	if err != nil {
		return stored, err
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return stored, err
	}
	return stored, nil
}

type tokenUsageGateway struct {
	agent.Gateway
	store *tokenUsageStore
}

func (g *tokenUsageGateway) BindTask(ctx context.Context) (context.Context, func(), error) {
	return agent.BindTask(ctx, g.Gateway)
}

func newTokenUsageGateway(gateway agent.Gateway, store *tokenUsageStore) agent.Gateway {
	if gateway == nil {
		return nil
	}
	return &tokenUsageGateway{Gateway: gateway, store: store}
}

func (g *tokenUsageGateway) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	call := g.beginCall(ctx, prompt, "")
	result, err := g.Gateway.Prompt(ctx, prompt)
	g.finishCall(call, prompt, result, err)
	g.record(ctx, prompt, result, err)
	return result, err
}

func (g *tokenUsageGateway) PromptWithOptions(ctx context.Context, prompt string, options agent.PromptOptions) (agent.PromptResult, error) {
	prompter, ok := g.Gateway.(interface {
		PromptWithOptions(context.Context, string, agent.PromptOptions) (agent.PromptResult, error)
	})
	if !ok {
		return g.Prompt(ctx, prompt)
	}
	call := g.beginCall(ctx, prompt, options.ReasoningEffortCap)
	result, err := prompter.PromptWithOptions(ctx, prompt, options)
	g.finishCall(call, prompt, result, err)
	g.record(ctx, prompt, result, err)
	return result, err
}

func (g *tokenUsageGateway) PromptWithBrowserState(ctx context.Context, prompt, statePath string) (agent.PromptResult, error) {
	prompter, ok := g.Gateway.(agent.BrowserStatePrompter)
	if !ok {
		return agent.PromptResult{}, fmt.Errorf("Hermes 不支持浏览器登录态提示词")
	}
	call := g.beginCall(ctx, prompt, "")
	result, err := prompter.PromptWithBrowserState(ctx, prompt, statePath)
	g.finishCall(call, prompt, result, err)
	g.record(ctx, prompt, result, err)
	return result, err
}

func (g *tokenUsageGateway) AgentSettings() (agent.AgentSettings, error) {
	gateway, ok := g.Gateway.(agent.SettingsGateway)
	if !ok {
		return agent.AgentSettings{}, fmt.Errorf("Hermes 设置接口不可用")
	}
	return gateway.AgentSettings()
}

func (g *tokenUsageGateway) SyncAgentSettings(settings agent.AgentSettings) error {
	gateway, ok := g.Gateway.(agent.SettingsGateway)
	if !ok {
		return fmt.Errorf("Hermes 设置接口不可用")
	}
	return gateway.SyncAgentSettings(settings)
}

func (g *tokenUsageGateway) SyncLLMProfile(cfg appsettings.LLM, profileID string, keyUpdate *string) error {
	gateway, ok := g.Gateway.(agent.ProfileGateway)
	if !ok {
		return fmt.Errorf("Hermes 模型配置接口不可用")
	}
	return gateway.SyncLLMProfile(cfg, profileID, keyUpdate)
}

func (g *tokenUsageGateway) StoreLLMProfileKey(profileID string, keyUpdate *string) error {
	gateway, ok := g.Gateway.(agent.ProfileGateway)
	if !ok {
		return fmt.Errorf("Hermes 模型密钥接口不可用")
	}
	return gateway.StoreLLMProfileKey(profileID, keyUpdate)
}

func (g *tokenUsageGateway) ModelAPIKeyForProfile(profileID string) (string, error) {
	gateway, ok := g.Gateway.(agent.ProfileGateway)
	if !ok {
		return "", fmt.Errorf("Hermes 模型密钥接口不可用")
	}
	return gateway.ModelAPIKeyForProfile(profileID)
}

func (g *tokenUsageGateway) record(ctx context.Context, prompt string, result agent.PromptResult, callErr error) {
	if g.store == nil {
		return
	}
	usage := result.Usage
	if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	estimated := usage.TotalTokens <= 0
	if estimated {
		// A failed request may never have reached the model. Only provider
		// usage is evidence of consumption in that case; do not invent an estimate.
		if callErr != nil {
			return
		}
		usage = estimateTokenUsage(prompt, result.Content)
		usage.Model = result.Usage.Model
	}
	if usage.TotalTokens <= 0 {
		return
	}
	module := agent.UsageModule(ctx)
	if module == "" {
		module = "other"
	}
	g.store.add(tokenUsageRequest{Runtime: result.Runtime, Module: module, Model: usage.Model, Prompt: usage.PromptTokens, Completion: usage.CompletionTokens, Total: usage.TotalTokens, Estimated: estimated})
}

// Some compatible providers omit usage from their response. Estimates stay in
// dedicated fields so they never inflate the real provider-reported usage.
func estimateTokenUsage(prompt, content string) agent.TokenUsage {
	promptTokens := estimateTextTokens(prompt)
	completionTokens := estimateTextTokens(content)
	return agent.TokenUsage{PromptTokens: promptTokens, CompletionTokens: completionTokens, TotalTokens: promptTokens + completionTokens}
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
	model := r.URL.Query().Get("model")
	period := r.URL.Query().Get("period")
	s.tokenUsage.mu.Lock()
	defer s.tokenUsage.mu.Unlock()
	type row struct {
		Runtime             string `json:"runtime,omitempty"`
		Date                string `json:"date"`
		Module              string `json:"module"`
		OriginalModule      string `json:"original_module,omitempty"`
		Model               string `json:"model"`
		Prompt              int    `json:"prompt_tokens"`
		Completion          int    `json:"completion_tokens"`
		Total               int    `json:"total_tokens"`
		EstimatedPrompt     int    `json:"estimated_prompt_tokens"`
		EstimatedCompletion int    `json:"estimated_completion_tokens"`
		EstimatedTotal      int    `json:"estimated_total_tokens"`
	}
	rows := []row{}
	index := map[string]int{}
	modules := map[string]bool{}
	models := map[string]bool{}
	total := row{}
	for _, e := range s.tokenUsage.Entries {
		if from != "" && e.Date < from || to != "" && e.Date > to || module != "" && module != e.Module || model != "" && model != e.Model {
			continue
		}
		date := e.Date
		if period == "month" && len(date) >= 7 {
			date = date[:7]
		}
		key := date + "\x00" + e.Module + "\x00" + e.Model + "\x00" + e.Runtime
		if existing, ok := index[key]; ok {
			rows[existing].Prompt += e.Prompt
			rows[existing].Completion += e.Completion
			rows[existing].Total += e.Total
			rows[existing].EstimatedPrompt += e.EstimatedPrompt
			rows[existing].EstimatedCompletion += e.EstimatedCompletion
			rows[existing].EstimatedTotal += e.EstimatedTotal
		} else {
			index[key] = len(rows)
			rows = append(rows, row{
				Runtime: e.Runtime, Date: date, Module: e.Module, Model: e.Model, OriginalModule: e.OriginalModule,
				Prompt: e.Prompt, Completion: e.Completion, Total: e.Total,
				EstimatedPrompt: e.EstimatedPrompt, EstimatedCompletion: e.EstimatedCompletion, EstimatedTotal: e.EstimatedTotal,
			})
		}
		modules[e.Module] = true
		models[e.Model] = true
		total.Prompt += e.Prompt
		total.Completion += e.Completion
		total.Total += e.Total
		total.EstimatedPrompt += e.EstimatedPrompt
		total.EstimatedCompletion += e.EstimatedCompletion
		total.EstimatedTotal += e.EstimatedTotal
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Date < rows[j].Date })
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"rows": rows, "modules": sortedKeys(modules), "models": sortedKeys(models), "total": total}})
}
func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for k := range values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
