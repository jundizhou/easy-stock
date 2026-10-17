package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
)

func TestTokenCallLedgerKeepsFailuresResumesAndUnknownUsage(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	s := newTokenUsageStore(settings)
	provider := &usageOptionsGateway{}
	g := newTokenUsageGateway(provider, s).(*tokenUsageGateway)
	ctx := agent.WithCallTrace(agent.WithUsageModule(context.Background(), "portfolio-inspection"), agent.CallTrace{RootJobID: "synthetic-job", NodeID: "assess", AttemptID: "1", SharedTaskID: "shared-study", ProtocolVersion: "v1"})
	// Same logical node on resume gets a different call ID; real costs accumulate.
	for _, state := range []string{"failed_known", "failed_unknown", "estimated", "actual"} {
		provider.result, provider.err = agent.PromptResult{Runtime: "test", ModelIdentity: "test/model"}, nil
		if strings.HasPrefix(state, "failed") {
			provider.err = errors.New("private provider detail")
		}
		if state == "actual" || state == "failed_known" {
			provider.result.Usage = agent.TokenUsage{PromptTokens: 11, CompletionTokens: 3, TotalTokens: 14}
		}
		_, _ = g.PromptWithOptions(ctx, "private prompt", agent.PromptOptions{})
	}
	if len(s.calls) != 4 || len(s.Entries) != 1 || s.Entries[0].Total != 28 {
		t.Fatal("duplicated or lost usage", s.Entries)
	}
	unknown, actual, estimated := 0, 0, 0
	for _, call := range s.calls {
		if call.RootJobID != "synthetic-job" || call.SharedTaskID != "shared-study" || call.FinishedAt == nil || call.InputDigest == "" {
			t.Fatal("attribution missing", call)
		}
		switch call.UsageStatus {
		case "unknown":
			unknown++
			if call.Usage != nil || call.Estimate != nil {
				t.Fatal("failed unknown invented tokens")
			}
		case "actual":
			actual++
		case "estimated":
			estimated++
		}
	}
	if unknown != 1 || actual != 2 || estimated != 1 {
		t.Fatal(unknown, actual, estimated)
	}
	// An interrupted call is durably present with unknown usage after restart.
	g.beginCall(ctx, "private unfinished prompt", "")
	reloaded := newTokenUsageStore(settings)
	if len(reloaded.calls) != 5 {
		t.Fatal("restart lost calls")
	}
	data, err := os.ReadFile(s.callsPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private prompt") || strings.Contains(string(data), "private provider detail") {
		t.Fatal("private evidence logged")
	}
	f, err := os.OpenFile(s.callsPath(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"torn":`)
	_ = f.Close()
	// The next start must still be recoverable even if it never finishes.
	interrupted := g.beginCall(ctx, "after torn write", "")
	if got := newTokenUsageStore(settings).calls[interrupted.ID]; got.Status != "started" || got.UsageStatus != "unknown" {
		t.Fatal("torn tail swallowed the next start event")
	}
	// Two consumers of one worker create two zero-cost dependency edges, while
	// the actual model requests above remain charged exactly once each.
	s.researchDependency(ctx, "shared-study", "shared_running")(nil)
	s.researchDependency(ctx, "shared-study", "reused")(context.Canceled)
	if len(s.calls) != 8 || s.Entries[0].Total != 28 {
		t.Fatal("shared wait was billed again")
	}
	for _, call := range s.calls {
		if call.Kind == "research_dependency" && (call.Usage != nil || call.Estimate != nil || call.UsageStatus != "attributed_to_shared_task") {
			t.Fatal("dependency acquired usage")
		}
	}
	if len(newTokenUsageStore(settings).calls) != 8 {
		t.Fatal("torn journal tail swallowed the next event")
	}
}

func TestTokenCallLedgerFiltersAndPaginates(t *testing.T) {
	store := newTokenUsageStore("")
	at := time.Now().UTC()
	for _, id := range []string{"a", "b", "c"} {
		store.saveCall(tokenCall{ID: id, StartedAt: at, CallTrace: agent.CallTrace{RootJobID: "job"}})
	}
	store.saveCall(tokenCall{ID: "unrelated", StartedAt: at, CallTrace: agent.CallTrace{RootJobID: "other"}})
	s := &Server{tokenUsage: store}
	var seen []string
	cursor := ""
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/settings/token-usage/calls?root_job_id=job&limit=1&cursor="+url.QueryEscape(cursor), nil)
		s.tokenUsageCalls(w, r)
		var response struct {
			Data []tokenCall `json:"data"`
			Next string      `json:"next_cursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Data) != 1 {
			t.Fatal(w.Body.String())
		}
		seen = append(seen, response.Data[0].ID)
		cursor = response.Next
	}
	if strings.Join(seen, ",") != "c,b,a" || cursor != "" {
		t.Fatal("pagination skipped or duplicated calls", seen, cursor)
	}
}
