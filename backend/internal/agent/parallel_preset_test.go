package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
)

// The UI and this native caller test consume the same executable preset.
func parallelPreset(t *testing.T) MCPServerInfo {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "frontend", "src", "lib", "mcp-presets.json"))
	if err != nil {
		t.Fatal(err)
	}
	var presets map[string]MCPServerInfo
	if err := json.Unmarshal(data, &presets); err != nil {
		t.Fatal(err)
	}
	preset := presets["parallel-search"]
	if preset.URL != "https://search.parallel.ai/mcp" || preset.Transport != "http" || preset.Headers["User-Agent"] != "easy-stock/1.7.0" {
		t.Fatalf("unexpected executable preset: %+v", preset)
	}
	return preset
}

func TestParallelPresetPreservesPolicy(t *testing.T) {
	h := NewHermesRuntime(HermesConfig{Home: t.TempDir()})
	key := "fixture-model-key"
	if err := h.SyncLLM(appsettings.LLM{Provider: "custom", BaseURL: "https://example.invalid/v1", Model: "gpt-5.5", APIMode: "responses"}, &key); err != nil {
		t.Fatal(err)
	}
	settings, err := h.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.MCPServers) != 0 {
		t.Fatal("preset was enabled without user selection")
	}
	preset := parallelPreset(t)
	settings.MCPServers = []MCPServerInfo{preset}
	if err := h.SyncAgentSettings(settings); err != nil {
		t.Fatal(err)
	}
	c := NewCodexRuntime(CodexConfig{Home: t.TempDir()}, h)
	for _, options := range []PromptOptions{{DisableTools: true}, {Sandbox: true}} {
		text, _, err := c.renderConfiguration(options)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(text, preset.Name) {
			t.Fatal("user MCP escaped task tool policy")
		}
	}
	preset.Enabled = false
	settings.MCPServers = []MCPServerInfo{preset}
	if err := h.SyncAgentSettings(settings); err != nil {
		t.Fatal(err)
	}
	text, _, err := c.renderConfiguration(PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, preset.Name) {
		t.Fatal("disabled preset loaded")
	}
	if RuntimeID("") != Hermes {
		t.Fatal("default runtime changed")
	}
}

// Use exactly the tool namespace/name advertised to the model by native Codex.
func presetToolName(value any, wanted string) (string, string) {
	var visit func(any, string) (string, string)
	visit = func(value any, namespace string) (string, string) {
		switch value := value.(type) {
		case []any:
			for _, item := range value {
				if name, ns := visit(item, namespace); name != "" {
					return name, ns
				}
			}
		case map[string]any:
			name := stringValue(value["name"])
			if value["type"] == "namespace" {
				namespace = name
			}
			if value["type"] == "function" && (name == wanted || strings.HasSuffix(name, "__"+wanted)) {
				return name, namespace
			}
			for _, key := range []string{"tools", "function"} {
				if name, ns := visit(value[key], namespace); name != "" {
					return name, ns
				}
			}
		}
		return "", ""
	}
	return visit(value, "")
}

// Drive the normal interactive gateway, answering only explicit approval frames.
// This is the same JSON-RPC boundary used by the settings/chat UI.
func presetConversation(ctx context.Context, c *CodexRuntime, choice string) (PromptResult, int, error) {
	process, err := c.Start(ctx)
	if err != nil {
		return PromptResult{}, 0, err
	}
	defer func() { process.Stop(); process.Wait() }()
	go io.Copy(io.Discard, process.Errors())
	lines := make(chan []byte, 1)
	go func() {
		scanner := bufio.NewScanner(process.Output())
		scanner.Buffer(make([]byte, 65536), 4<<20)
		for scanner.Scan() {
			data := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- data:
			case <-ctx.Done():
				return
			}
		}
		close(lines)
	}()
	encoder := json.NewEncoder(process.Input())
	send := func(id, method string, params map[string]any) error {
		return encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	}
	approvals := 0
	for {
		select {
		case <-ctx.Done():
			return PromptResult{}, approvals, ctx.Err()
		case line, ok := <-lines:
			if !ok {
				return PromptResult{}, approvals, fmt.Errorf("native gateway closed")
			}
			var frame rpcFrame
			if err := json.Unmarshal(line, &frame); err != nil {
				return PromptResult{}, approvals, err
			}
			if frame.Error != nil {
				return PromptResult{}, approvals, fmt.Errorf("native gateway: %s", frame.Error.Message)
			}
			if eventType(frame) == "gateway.ready" {
				if err := send("1", "session.create", map[string]any{}); err != nil {
					return PromptResult{}, approvals, err
				}
			} else if frame.ID == "1" {
				if err := send("2", "prompt.submit", map[string]any{"session_id": frame.Result["session_id"], "text": "Search and read the source evidence"}); err != nil {
					return PromptResult{}, approvals, err
				}
			} else if eventType(frame) == "approval" {
				approvals++
				if !strings.Contains(stringValue(frame.Params["description"]), "parallel-search") {
					return PromptResult{}, approvals, fmt.Errorf("approval omitted server identity")
				}
				if err := encoder.Encode(map[string]any{"id": frame.ID, "result": map[string]any{"choice": choice}}); err != nil {
					return PromptResult{}, approvals, err
				}
			} else if eventType(frame) == "clarify" {
				return PromptResult{}, approvals, fmt.Errorf("tool approval was misrouted to clarification")
			} else if eventType(frame) == "message.complete" {
				return PromptResult{Content: eventText(frame, "content")}, approvals, nil
			}
		}
	}
}

// Fail the fixture on malformed required endpoint arguments, rather than
// allowing a permissive schema to manufacture successful native-call evidence.
func validateParallelFixtureArgs(name string, args map[string]any) error {
	key := "urls"
	if name == "web_search" {
		objective, ok := args["objective"].(string)
		if !ok || strings.TrimSpace(objective) == "" {
			return fmt.Errorf("web_search requires objective:string")
		}
		key = "search_queries"
	} else if name != "web_fetch" {
		return fmt.Errorf("unknown tool")
	}
	values, ok := args[key].([]any)
	if !ok || len(values) == 0 || name == "web_fetch" && len(values) > 20 {
		return fmt.Errorf("%s requires nonempty %s:string[]", name, key)
	}
	for _, value := range values {
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return fmt.Errorf("%s must contain nonempty strings", key)
		}
		if key == "urls" && !(strings.HasPrefix(text, "https://") || strings.HasPrefix(text, "http://")) {
			return fmt.Errorf("urls must contain HTTP(S) URLs")
		}
	}
	if _, exists := args["url"]; exists {
		return fmt.Errorf("web_fetch uses plural urls")
	}
	return nil
}

// A scripted Responses model drives the real pinned Codex App Server, its MCP
// HTTP client, and the production shared-settings projection. No live LLM is used.
func TestParallelPresetNativeCaller(t *testing.T) {
	bin := os.Getenv("EASY_STOCK_CODEX_TEST_BINARY")
	if bin == "" {
		t.Skip("set EASY_STOCK_CODEX_TEST_BINARY to verified pinned Codex")
	}
	for _, mode := range []string{"success", "session", "decline", "tool_error", "timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			preset := parallelPreset(t)
			var mu sync.Mutex
			var observed []map[string]string
			modelCalls := 0
			searchName, searchNamespace, fetchName, fetchNamespace := "", "", "", ""
			searchStarted := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/mcp" {
					if r.Method != http.MethodPost {
						w.WriteHeader(http.StatusMethodNotAllowed)
						return
					}
					var frame map[string]any
					if err := json.NewDecoder(r.Body).Decode(&frame); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					method := stringValue(frame["method"])
					mu.Lock()
					observed = append(observed, map[string]string{"path": r.URL.Path, "method": method, "userAgent": r.UserAgent(), "authorization": r.Header.Get("Authorization")})
					mu.Unlock()
					if r.UserAgent() != preset.Headers["User-Agent"] || r.Header.Get("Authorization") != "" {
						t.Error("anonymous project User-Agent missing or credentials leaked")
					}
					if frame["id"] == nil {
						w.WriteHeader(202)
						return
					}
					var result any = map[string]any{}
					switch method {
					case "initialize":
						params, _ := stringMap(frame["params"])
						result = map[string]any{"protocolVersion": params["protocolVersion"], "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "parallel-fixture", "version": "1"}}
					case "tools/list":
						// Match the documented Parallel schemas: objective + queries for
						// search, plural urls for fetch. Optional conversation fields remain
						// host-owned and are not filled by this configuration preset.
						str := map[string]any{"type": "string"}
						stringsSchema := map[string]any{"type": "array", "items": str, "minItems": 1}
						result = map[string]any{"tools": []any{
							map[string]any{"name": "web_search", "description": "Search public source evidence", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"objective": str, "search_queries": stringsSchema, "session_id": str, "model_name": str}, "required": []string{"objective", "search_queries"}}},
							map[string]any{"name": "web_fetch", "description": "Read public source evidence", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"urls": map[string]any{"type": "array", "items": str, "minItems": 1, "maxItems": 20}, "objective": map[string]any{"type": "string", "maxLength": 200}, "search_queries": stringsSchema, "full_content": map[string]any{"type": "boolean", "default": false}, "session_id": str, "model_name": str}, "required": []string{"urls"}}},
						}}
					case "tools/call":
						params, _ := stringMap(frame["params"])
						name := stringValue(params["name"])
						args, _ := stringMap(params["arguments"])
						if err := validateParallelFixtureArgs(name, args); err != nil {
							t.Errorf("invalid documented tool arguments: %v", err)
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						if name == "web_fetch" && args["urls"].([]any)[0] != "https://example.com/source" {
							t.Error("source URLs were not passed to fetch")
						}
						mu.Lock()
						observed[len(observed)-1]["tool"] = name
						arguments, _ := json.Marshal(args)
						observed[len(observed)-1]["arguments"] = string(arguments)
						mu.Unlock()
						content := "SEARCH_RESULT https://example.com/source"
						if name == "web_fetch" {
							content = "FETCH_RESULT verified source text https://example.com/source"
						}
						if name == "web_search" && (mode == "timeout" || mode == "cancel") {
							select {
							case searchStarted <- struct{}{}:
							default:
							}
							select {
							case <-r.Context().Done():
								return
							case <-time.After(5 * time.Second):
							}
						}
						isError := mode == "tool_error"
						if isError {
							content = "FIXTURE_TOOL_ERROR: unavailable source"
						}
						result = map[string]any{"content": []any{map[string]any{"type": "text", "text": content}}, "isError": isError}
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": frame["id"], "result": result})
					return
				}
				if !strings.HasSuffix(r.URL.Path, "/responses") {
					http.NotFound(w, r)
					return
				}
				var frame map[string]any
				if err := json.NewDecoder(r.Body).Decode(&frame); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				modelCalls++
				input, _ := json.Marshal(frame["input"])
				if modelCalls == 1 {
					nativeReply(w, []any{map[string]any{"type": "tool_search_call", "id": "ts_discover", "call_id": "discover", "arguments": map[string]any{"query": "parallel-search web_search web_fetch", "limit": 2}, "execution": "client"}})
					return
				}
				if modelCalls == 2 {
					searchName, searchNamespace = presetToolName([]any{frame["tools"], frame["input"]}, "web_search")
					fetchName, fetchNamespace = presetToolName([]any{frame["tools"], frame["input"]}, "web_fetch")
					tools, _ := json.Marshal([]any{frame["tools"], frame["input"]})
					if !strings.Contains(string(tools), "web_search") || !strings.Contains(string(tools), "web_fetch") {
						t.Errorf("MCP tools not discovered: %s", tools)
					}
					nativeReply(w, []any{map[string]any{"type": "function_call", "id": "fc_search", "call_id": "search", "namespace": searchNamespace, "name": searchName, "arguments": `{"objective":"Find source evidence","search_queries":["example source evidence"]}`}})
					return
				}
				if (mode == "success" || mode == "session") && modelCalls == 3 {
					if !strings.Contains(string(input), "SEARCH_RESULT https://example.com/source") {
						t.Errorf("search result not consumed: %.1200s", string(input)[max(0, len(input)-1200):])
					}
					nativeReply(w, []any{map[string]any{"type": "function_call", "id": "fc_fetch", "call_id": "fetch", "namespace": fetchNamespace, "name": fetchName, "arguments": `{"urls":["https://example.com/source"],"objective":"Read source evidence","search_queries":["example source evidence"]}`}})
					return
				}
				if mode == "session" && modelCalls == 4 {
					nativeReply(w, []any{map[string]any{"type": "function_call", "id": "fc_followup", "call_id": "followup", "namespace": searchNamespace, "name": searchName, "arguments": `{"objective":"Find followup source evidence","search_queries":["followup source evidence"]}`}})
					return
				}
				switch mode {
				case "success", "session":
					if !strings.Contains(string(input), "FETCH_RESULT verified source text") {
						t.Errorf("fetch result not consumed: %.1200s", string(input)[max(0, len(input)-1200):])
					}
				case "decline":
					if !strings.Contains(string(input), "user rejected MCP tool call") {
						t.Error("declined call was not surfaced")
					}
				case "tool_error":
					if !strings.Contains(string(input), "FIXTURE_TOOL_ERROR") {
						t.Errorf("tool error not surfaced: %.1200s", string(input)[max(0, len(input)-1200):])
					}
				case "timeout":
					if !strings.Contains(strings.ToLower(string(input)), "timed out") {
						t.Errorf("timeout not surfaced: %.1200s", string(input)[max(0, len(input)-1200):])
					}
				}
				nativeReply(w, nativeMessage())
			}))
			defer server.Close()
			h := NewHermesRuntime(HermesConfig{Home: t.TempDir()})
			key := "fixture-model-key"
			if err := h.SyncLLM(appsettings.LLM{Provider: "custom", BaseURL: server.URL + "/v1", Model: "gpt-5.5", APIMode: "responses"}, &key); err != nil {
				t.Fatal(err)
			}
			settings, err := h.AgentSettings()
			if err != nil {
				t.Fatal(err)
			}
			// Redirect only the validated production URL to a deterministic HTTP fixture.
			preset.URL = server.URL + "/mcp"
			if mode == "timeout" {
				preset.Timeout = 1
			}
			settings.MCPServers = []MCPServerInfo{preset}
			if err := h.SyncAgentSettings(settings); err != nil {
				t.Fatal(err)
			}
			c := NewCodexRuntime(CodexConfig{Executable: bin, Home: t.TempDir()}, h)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if mode == "cancel" {
				go func() {
					select {
					case <-searchStarted:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			start := time.Now()
			choice := "once"
			if mode == "session" {
				choice = "session"
			}
			if mode == "decline" {
				choice = "deny"
			}
			result, approvals, err := presetConversation(ctx, c, choice)
			if mode == "cancel" {
				if err == nil || time.Since(start) > 10*time.Second {
					t.Fatalf("cancellation was not propagated promptly: %v", err)
				}
			} else if err != nil || result.Content != "EASY_STOCK_CODEX_OK" {
				t.Fatalf("native caller failed: %v; result=%+v", err, result)
			}
			mu.Lock()
			defer mu.Unlock()
			if mode == "decline" {
				for _, request := range observed {
					if request["method"] == "tools/call" {
						t.Fatal("declined tool executed")
					}
				}
			}
			if mode == "session" && approvals != 2 {
				t.Fatalf("session choice was not scoped to each tool: approvals=%d", approvals)
			}
			if approvals == 0 {
				t.Fatal("MCP call bypassed explicit approval")
			}
			if len(observed) == 0 {
				t.Fatal("no native MCP requests observed")
			}
			if mode == "success" {
				data, _ := json.Marshal(observed)
				t.Logf("PARALLEL_PRESET_CALLER=%s", data)
			}
			t.Logf("mode=%s modelCalls=%d approvals=%d elapsed=%s", mode, modelCalls, approvals, time.Since(start))
		})
	}
}

func TestMCPToolApprovalClassification(t *testing.T) {
	base := map[string]any{"mode": "form", "_meta": map[string]any{"codex_approval_kind": "mcp_tool_call"}, "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}}}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		want   bool
	}{
		{"marked empty approval", func(map[string]any) {}, true},
		{"ordinary empty form", func(p map[string]any) { delete(p, "_meta") }, false},
		{"URL auth", func(p map[string]any) { p["mode"] = "url" }, false},
		{"form with required input", func(p map[string]any) {
			p["requestedSchema"] = map[string]any{"type": "object", "properties": map[string]any{"token": map[string]any{"type": "string"}}}
		}, false},
		{"missing schema", func(p map[string]any) { delete(p, "requestedSchema") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := json.Marshal(base)
			var params map[string]any
			json.Unmarshal(data, &params)
			tc.change(params)
			if got := isMCPToolApproval(codexFrame{Method: "mcpServer/elicitation/request", Params: params}); got != tc.want {
				t.Fatalf("classification=%v, want=%v", got, tc.want)
			}
		})
	}
}
