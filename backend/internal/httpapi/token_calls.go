package httpapi

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/agent"
)

// Append-only lifecycle journal. A start without a finish is unknown, not free.
// Shared research is charged at the worker gateway, never at each waiting user.
type tokenCall struct {
	agent.CallTrace
	ID              string               `json:"id"`
	Kind            string               `json:"kind"`
	ResearchOrigin  string               `json:"research_origin,omitempty"`
	Module          string               `json:"module"`
	StartedAt       time.Time            `json:"started_at"`
	FinishedAt      *time.Time           `json:"finished_at,omitempty"`
	DurationMS      int64                `json:"duration_ms"`
	Status          string               `json:"status"`
	UsageStatus     string               `json:"usage_status"`
	Runtime         string               `json:"runtime,omitempty"`
	ModelIdentity   string               `json:"model_identity,omitempty"`
	EffortCap       string               `json:"requested_effort_cap,omitempty"`
	RequestedEffort string               `json:"requested_effort,omitempty"`
	InputDigest     string               `json:"input_digest"`
	PromptBytes     int                  `json:"prompt_bytes"`
	ResponseBytes   int                  `json:"response_bytes"`
	Usage           *agent.TokenUsage    `json:"usage,omitempty"`
	Estimate        *agent.TokenUsage    `json:"estimate,omitempty"`
	Progress        agent.PromptProgress `json:"progress"`
	ErrorKind       string               `json:"error_kind,omitempty"`
}

func (s *tokenUsageStore) callsPath() string {
	if s.path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.path), "token-calls.jsonl")
}

func (s *tokenUsageStore) loadCalls() {
	s.calls = map[string]tokenCall{}
	f, err := os.Open(s.callsPath())
	if err != nil {
		return
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1024*1024)
	for scan.Scan() {
		var call tokenCall
		if json.Unmarshal(scan.Bytes(), &call) == nil && call.ID != "" {
			s.calls[call.ID] = call
		}
	}
	if err := scan.Err(); err != nil {
		log.Printf("token call ledger read failed: %v", err)
	}
}

func (s *tokenUsageStore) saveCall(call tokenCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == nil {
		s.calls = map[string]tokenCall{}
	}
	s.calls[call.ID] = call
	path := s.callsPath()
	if path == "" {
		return
	}
	err := func() error {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		data, err := json.Marshal(call)
		if err != nil {
			return err
		}
		// A separator also recovers from a torn trailing line after a crash;
		// otherwise the next start event would be glued to invalid JSON.
		data = append(append([]byte{'\n'}, data...), '\n')
		if _, err := f.Write(data); err != nil {
			return err
		}
		return f.Sync()
	}()
	if err != nil {
		log.Printf("token call ledger write failed: %v", err)
	}
}

func (g *tokenUsageGateway) beginCall(ctx context.Context, prompt, cap string) tokenCall {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	hash := sha256.Sum256([]byte(prompt))
	effort, _ := agent.BoundReasoningEffort(ctx)
	call := tokenCall{CallTrace: agent.CallTraceFromContext(ctx), ID: hex.EncodeToString(id), Kind: "model_call", Module: agent.UsageModule(ctx), StartedAt: time.Now().UTC(), Status: "started", UsageStatus: "unknown", InputDigest: hex.EncodeToString(hash[:]), PromptBytes: len(prompt), EffortCap: cap, RequestedEffort: effort, ModelIdentity: agent.BoundModel(ctx), Runtime: agent.BoundRuntime(ctx)}
	if call.Module == "" {
		call.Module = "other"
	}
	if g.store != nil {
		g.store.saveCall(call)
	}
	return call
}

func (g *tokenUsageGateway) finishCall(call tokenCall, prompt string, result agent.PromptResult, err error) {
	if g.store == nil {
		return
	}
	now := time.Now().UTC()
	call.FinishedAt = &now
	call.DurationMS = now.Sub(call.StartedAt).Milliseconds()
	call.Status = "succeeded"
	if err != nil {
		call.Status = "failed"
		call.ErrorKind = fmt.Sprintf("%T", err)
	}
	if result.Runtime != "" {
		call.Runtime = result.Runtime
	}
	if result.ModelIdentity != "" {
		call.ModelIdentity = result.ModelIdentity
	}
	call.Progress, call.ResponseBytes = result.Progress, len(result.Content)
	u := result.Usage
	if u.TotalTokens <= 0 {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	if u.TotalTokens > 0 {
		call.UsageStatus, call.Usage = "actual", &u
	} else if err == nil {
		e := estimateTokenUsage(prompt, result.Content)
		call.UsageStatus, call.Estimate = "estimated", &e
	}
	g.store.saveCall(call)
}

// Consumer edges never add tokens: join/cached cost is attached to the worker
// through SharedTaskID. This also records waits canceled before completion.
func (s *tokenUsageStore) researchDependency(ctx context.Context, taskID, origin string) func(error) {
	if s == nil {
		return func(error) {}
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	trace := agent.CallTraceFromContext(ctx)
	trace.SharedTaskID = taskID
	trace.NodeID = "research_wait"
	trace.AttemptID = ""
	call := tokenCall{CallTrace: trace, ID: hex.EncodeToString(id), Kind: "research_dependency", ResearchOrigin: origin, Module: agent.UsageModule(ctx), StartedAt: time.Now().UTC(), Status: "started", UsageStatus: "attributed_to_shared_task"}
	s.saveCall(call)
	return func(err error) {
		now := time.Now().UTC()
		call.FinishedAt = &now
		call.DurationMS = now.Sub(call.StartedAt).Milliseconds()
		call.Status = "succeeded"
		if err != nil {
			call.Status = "failed"
			call.ErrorKind = fmt.Sprintf("%T", err)
		}
		s.saveCall(call)
	}
}

func (s *Server) tokenUsageCalls(w http.ResponseWriter, r *http.Request) {
	root, shared := r.URL.Query().Get("root_job_id"), r.URL.Query().Get("shared_task_id")
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		limit = n
	}
	var before time.Time
	var beforeID string
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		parts := strings.SplitN(cursor, "|", 2)
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		var err error
		before, err = time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		beforeID = parts[1]
	}
	s.tokenUsage.mu.Lock()
	calls := []tokenCall{}
	for _, call := range s.tokenUsage.calls {
		if root != "" && root != call.RootJobID || shared != "" && shared != call.SharedTaskID {
			continue
		}
		if !before.IsZero() && (call.StartedAt.After(before) || call.StartedAt.Equal(before) && call.ID >= beforeID) {
			continue
		}
		calls = append(calls, call)
	}
	s.tokenUsage.mu.Unlock()
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].StartedAt.Equal(calls[j].StartedAt) {
			return calls[i].ID > calls[j].ID
		}
		return calls[i].StartedAt.After(calls[j].StartedAt)
	})
	// Pagination keeps long-lived installations bounded at the API boundary.
	next := ""
	if len(calls) > limit {
		calls = calls[:limit]
		last := calls[len(calls)-1]
		next = last.StartedAt.Format(time.RFC3339Nano) + "|" + last.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": calls, "next_cursor": next})
}
