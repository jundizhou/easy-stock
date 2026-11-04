package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
)

func TestCodexRejectsUnsupportedModels(t *testing.T) {
	h := NewHermesRuntime(HermesConfig{Home: t.TempDir()})
	key := "test-key"
	if err := h.SyncLLM(appsettings.LLM{Provider: "custom", BaseURL: "https://example.com/v1", Model: "test", APIMode: "chat_completions"}, &key); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("test"), 0700); err != nil {
		t.Fatal(err)
	}
	c := NewCodexRuntime(CodexConfig{Executable: bin, Home: t.TempDir()}, h)
	status := c.Status()
	if !status.Available || status.Configured || !strings.Contains(status.Message, "MODEL_PROTOCOL_UNSUPPORTED") {
		t.Fatalf("unexpected status: %+v", status)
	}
	if _, err := c.Start(context.Background()); err == nil {
		t.Fatal("unsupported model was started")
	}
}

func TestCodexConfigurationSharesSettingsWithoutModelKey(t *testing.T) {
	h := NewHermesRuntime(HermesConfig{Home: t.TempDir()})
	key := "provider-secret-123"
	if err := h.SyncLLM(appsettings.LLM{Provider: "custom", BaseURL: "https://example.com/v1", Model: "test-model", APIMode: "responses"}, &key); err != nil {
		t.Fatal(err)
	}
	settings, err := h.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.MCPServers = []MCPServerInfo{{Name: "finance", Enabled: true, Transport: "http", URL: "https://mcp.example.com", Headers: map[string]string{"Authorization": "Bearer mcp-secret"}}}
	if err = h.SyncAgentSettings(settings); err != nil {
		t.Fatal(err)
	}
	c := NewCodexRuntime(CodexConfig{Home: t.TempDir()}, h)
	text, env, err := c.renderConfiguration(PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, key) || strings.Contains(text, "mcp-secret") {
		t.Fatal("secrets leaked into generated config")
	}
	if !strings.Contains(text, `wire_api = "responses"`) || !strings.Contains(text, "finance") || env["EASY_STOCK_MODEL_API_KEY"] != key {
		t.Fatal("shared model/MCP settings missing")
	}
	text, _, err = c.renderConfiguration(PromptOptions{Sandbox: true, DisableTools: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "finance") || !strings.Contains(text, "shell_tool = false") {
		t.Fatal("unattended policy loaded user tools")
	}
}

// This opt-in test runs a real isolated App Server against a local fake model,
// without an API key, external model traffic or modifying the user's Codex.
func TestCodexNativeAppServer(t *testing.T) {
	bin := os.Getenv("EASY_STOCK_CODEX_TEST_BINARY")
	if bin == "" {
		t.Skip("set EASY_STOCK_CODEX_TEST_BINARY to a verified native executable")
	}
	requests := make(chan map[string]any, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			http.Error(w, "not found", 404)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
			http.Error(w, "bad request", 400)
			return
		}
		select {
		case requests <- request:
		default:
		}
		w.Header().Set("Content-Type", "text/event-stream")
		message := map[string]any{"type": "message", "id": "msg_test", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "EASY_STOCK_CODEX_OK", "annotations": []any{}}}}
		event := func(name string, payload map[string]any) {
			payload["type"] = name
			data, _ := json.Marshal(payload)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
		}
		event("response.created", map[string]any{"response": map[string]any{"id": "resp_test", "object": "response", "status": "in_progress", "output": []any{}}})
		event("response.output_item.added", map[string]any{"output_index": 0, "item": map[string]any{"id": "msg_test", "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}})
		event("response.output_text.delta", map[string]any{"item_id": "msg_test", "output_index": 0, "content_index": 0, "delta": "EASY_STOCK_CODEX_OK"})
		event("response.output_item.done", map[string]any{"output_index": 0, "item": message})
		event("response.completed", map[string]any{"response": map[string]any{"id": "resp_test", "object": "response", "status": "completed", "output": []any{message}, "usage": map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}})
	}))
	defer server.Close()
	h := NewHermesRuntime(HermesConfig{Home: t.TempDir()})
	key := "fake-local-key"
	if err := h.SyncLLM(appsettings.LLM{Provider: "custom", BaseURL: server.URL + "/v1", Model: "gpt-5.5", APIMode: "responses"}, &key); err != nil {
		t.Fatal(err)
	}
	if err := h.SyncModelCapabilities(server.URL+"/v1", "codex_responses", map[string]ReasoningCapability{"gpt-5.5": reasoningCapability("model_api", "openai_responses", "test fixture", "high", "high")}); err != nil {
		t.Fatal(err)
	}
	settings, err := h.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.ReasoningEffort = "high"
	if err = h.SyncAgentSettings(settings); err != nil {
		t.Fatal(err)
	}
	c := NewCodexRuntime(CodexConfig{Executable: bin, Home: t.TempDir()}, h)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := c.PromptWithOptions(ctx, "Reply EASY_STOCK_CODEX_OK", PromptOptions{Sandbox: true, DisableTools: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "EASY_STOCK_CODEX_OK" {
		t.Fatalf("unexpected result: %+v", result)
	}
	select {
	case req := <-requests:
		reasoning, _ := req["reasoning"].(map[string]any)
		if reasoning["effort"] != "high" {
			t.Fatalf("shared reasoning not applied: %v", reasoning)
		}
		tools, _ := req["tools"].([]any)
		if len(tools) > 0 {
			data, _ := json.Marshal(tools)
			t.Fatalf("no-tools policy exposed tools: %s", data)
		}
	default:
		t.Fatal("model was not called")
	}
}

func nativeReply(w http.ResponseWriter, output []any) {
	w.Header().Set("Content-Type", "text/event-stream")
	emit := func(kind string, value map[string]any) {
		value["type"] = kind
		data, _ := json.Marshal(value)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
	}
	emit("response.created", map[string]any{"response": map[string]any{"id": "r", "object": "response", "status": "in_progress", "output": []any{}}})
	for i, item := range output {
		emit("response.output_item.done", map[string]any{"output_index": i, "item": item})
	}
	emit("response.completed", map[string]any{"response": map[string]any{"id": "r", "object": "response", "status": "completed", "output": output, "usage": map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}})
}

func nativeMessage() []any {
	return []any{map[string]any{"type": "message", "id": "msg", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "EASY_STOCK_CODEX_OK", "annotations": []any{}}}}}
}

// Run the real bundled Codex against a local model endpoint and inspect the
// actual HTTP body. This catches selectable efforts being silently omitted.
func TestCodexNativeProviderReasoningEfforts(t *testing.T) {
	bin := os.Getenv("EASY_STOCK_CODEX_TEST_BINARY")
	if bin == "" {
		t.Skip("native Codex binary required")
	}
	requests := make(chan map[string]any, 12)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		requests <- request
		nativeReply(w, nativeMessage())
	}))
	defer server.Close()
	for _, tc := range []struct{ model, base string }{
		{"glm-5.3-flash", "https://open.bigmodel.cn/api/v1"},
		{"deepseek-flash", "https://api.deepseek.com"},
		{"kimi-k3", "https://api.moonshot.cn/v1"},
		{"MiniMax-M3.1-Flash-Preview", "https://api.minimaxi.com/v1"},
		{"MiniMax-M3", "https://api.minimaxi.com/v1"},
		{"qwen3.8-max", "https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"},
		{"gpt-6.1-sol", "https://api.openai.com/v1"},
	} {
		h := NewHermesRuntime(HermesConfig{Home: t.TempDir()})
		key := "dummy-test-key"
		cfg := appsettings.LLM{Provider: "custom", BaseURL: server.URL + "/v1", Model: tc.model, APIMode: "codex_responses"}
		if err := h.SyncLLM(cfg, &key); err != nil {
			t.Fatal(err)
		}
		cap := OfficialReasoningCapability(appsettings.LLM{Model: cfg.Model, BaseURL: tc.base, APIMode: cfg.APIMode})
		if err := h.SyncModelCapabilities(cfg.BaseURL, cfg.APIMode, map[string]ReasoningCapability{cfg.Model: cap}); err != nil {
			t.Fatal(err)
		}
		c := NewCodexRuntime(CodexConfig{Executable: bin, Home: t.TempDir()}, h)
		for _, isolated := range []bool{false, true} {
			for _, effort := range optionValues(cap) {
				t.Run(fmt.Sprintf("%s/%s/isolated=%v", tc.model, effort, isolated), func(t *testing.T) {
					settings, err := h.AgentSettings()
					if err != nil {
						t.Fatal(err)
					}
					settings.ReasoningEffort = effort
					if err := h.SyncAgentSettings(settings); err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					result, err := c.PromptWithOptions(ctx, "Reply EASY_STOCK_CODEX_OK", PromptOptions{Sandbox: isolated, DisableTools: isolated})
					if err != nil || result.Content != "EASY_STOCK_CODEX_OK" {
						t.Fatalf("native response: %v %v", result, err)
					}
					select {
					case request := <-requests:
						reasoning, _ := request["reasoning"].(map[string]any)
						wireEffort := effort
						if effort == "enabled" {
							wireEffort = "medium"
						}
						if reasoning["effort"] != wireEffort || request["model"] != cfg.Model {
							t.Fatalf("effort not sent: model=%v reasoning=%v", request["model"], reasoning)
						}
						if request["reasoning_effort"] != nil || request["thinking"] != nil {
							t.Fatal("Chat Completions parameters sent to Responses")
						}
					default:
						t.Fatal("native model endpoint was never called")
					}
				})
			}
		}
	}
}

func TestCodexNativeSharedSkillsAndMCP(t *testing.T) {
	bin, python := os.Getenv("EASY_STOCK_CODEX_TEST_BINARY"), os.Getenv("EASY_STOCK_HERMES_TEST_PYTHON")
	if bin == "" || python == "" {
		t.Skip("native Codex and packaged Python required")
	}
	for _, transport := range []string{"stdio", "sse", "builtin"} {
		t.Run(transport, func(t *testing.T) {
			var requests []map[string]any
			var mu sync.Mutex
			messages := make(chan []byte, 8)
			listed := make(chan bool, 8)
			replyMCP := func(frame map[string]any) map[string]any {
				result := map[string]any{}
				if frame["method"] == "initialize" {
					result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fixture", "version": "1"}}
				}
				if frame["method"] == "tools/list" {
					result = map[string]any{"tools": []any{map[string]any{"name": "echo", "description": "test echo", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
					listed <- true
				}
				return map[string]any{"jsonrpc": "2.0", "id": frame["id"], "result": result}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/sse" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
					w.(http.Flusher).Flush()
					for {
						select {
						case <-r.Context().Done():
							return
						case data := <-messages:
							fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
							w.(http.Flusher).Flush()
						}
					}
				}
				var frame map[string]any
				if json.NewDecoder(r.Body).Decode(&frame) != nil {
					http.Error(w, "bad request", 400)
					return
				}
				if r.URL.Path == "/messages" {
					if frame["id"] != nil {
						data, _ := json.Marshal(replyMCP(frame))
						messages <- data
					}
					w.WriteHeader(202)
					return
				}
				if !strings.HasSuffix(r.URL.Path, "/responses") {
					http.NotFound(w, r)
					return
				}
				mu.Lock()
				requests = append(requests, frame)
				count := len(requests)
				mu.Unlock()
				if transport == "builtin" && count == 1 {
					nativeReply(w, []any{map[string]any{"type": "function_call", "id": "fc_1", "call_id": "call_1", "namespace": "mcp__easy_stock_research", "name": "execute_code", "arguments": `{"code":"print(2 + 2)"}`}})
					return
				}
				nativeReply(w, nativeMessage())
			}))
			defer server.Close()
			h := NewHermesRuntime(HermesConfig{Home: t.TempDir(), PythonPath: python})
			key := "fake-local-key"
			if err := h.SyncLLM(appsettings.LLM{Provider: "custom", BaseURL: server.URL + "/v1", Model: "gpt-5.5", APIMode: "responses"}, &key); err != nil {
				t.Fatal(err)
			}
			skillPath := filepath.Join(h.home, "skills", "native-test-skill", "SKILL.md")
			os.MkdirAll(filepath.Dir(skillPath), 0700)
			os.WriteFile(skillPath, []byte("---\nname: native-test-skill\ndescription: NATIVE_SKILL_DISCOVERY_MARKER\n---\nThis is a deterministic test skill."), 0600)
			settings, err := h.AgentSettings()
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "listed")
			if transport == "stdio" {
				script := `import sys,json,pathlib
for line in sys.stdin:
 f=json.loads(line)
 if 'id' not in f: continue
 result={}
 if f.get('method')=='initialize': result={'protocolVersion':'2024-11-05','capabilities':{'tools':{}},'serverInfo':{'name':'fixture','version':'1'}}
 if f.get('method')=='tools/list':
  pathlib.Path(sys.argv[1]).write_text('listed')
  result={'tools':[{'name':'echo','description':'echo','inputSchema':{'type':'object','properties':{}}}]}
 print(json.dumps({'jsonrpc':'2.0','id':f['id'],'result':result}),flush=True)
`
				settings.MCPServers = []MCPServerInfo{{Name: "fixture", Enabled: true, Transport: "stdio", Command: python, Args: []string{"-c", script, marker}, Env: map[string]string{"FIXTURE_SECRET": "server-secret"}}}
			} else if transport == "sse" {
				settings.MCPServers = []MCPServerInfo{{Name: "fixture", Enabled: true, Transport: "sse", URL: server.URL + "/sse"}}
			}
			if err = h.SyncAgentSettings(settings); err != nil {
				t.Fatal(err)
			}
			c := NewCodexRuntime(CodexConfig{Executable: bin, Home: t.TempDir()}, h)
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			options := PromptOptions{}
			if transport == "builtin" {
				options = PromptOptions{Sandbox: true, AutoApprove: true}
			}
			result, err := c.PromptWithOptions(ctx, "Reply EASY_STOCK_CODEX_OK without tools", options)
			if err != nil {
				t.Fatal(err)
			}
			if result.Content != "EASY_STOCK_CODEX_OK" {
				t.Fatalf("unexpected response: %s", result.Content)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) == 0 {
				t.Fatal("no Responses request")
			}
			body, _ := json.Marshal(requests[0])
			if transport != "builtin" && !strings.Contains(string(body), "NATIVE_SKILL_DISCOVERY_MARKER") {
				t.Fatal("shared Skill was not discovered by native Codex")
			}
			if transport == "stdio" {
				if _, err = os.Stat(marker); err != nil {
					t.Fatal("stdio MCP not initialized", err)
				}
			}
			if transport == "sse" {
				select {
				case <-listed:
				default:
					t.Fatal("SSE MCP not initialized")
				}
			}
			if transport == "builtin" {
				if len(requests) < 2 {
					tools, _ := json.Marshal(requests[0]["tools"])
					t.Fatalf("native Codex did not execute the research tool: %s", tools)
				}
				last, _ := json.Marshal(requests[len(requests)-1]["input"])
				if !strings.Contains(string(last), "4") || strings.Contains(string(last), "execute_code blocked") {
					t.Fatalf("compute tool failed: %s", last)
				}
			}
			if transport == "builtin" && !strings.Contains(string(body), "execute_code") {
				t.Fatalf("background research tools missing: %.1800s", string(body))
			}
			if strings.Contains(string(body), "server-secret") || strings.Contains(string(body), key) {
				t.Fatal("credentials reached model prompt")
			}
		})
	}
}

func TestBuiltinMCPHandshake(t *testing.T) {
	python := os.Getenv("EASY_STOCK_HERMES_TEST_PYTHON")
	if python == "" {
		t.Skip("packaged Python required")
	}
	h := NewHermesRuntime(HermesConfig{Home: t.TempDir(), PythonPath: python})
	key := "fake"
	if err := h.SyncLLM(appsettings.LLM{Provider: "custom", BaseURL: "https://example.invalid/v1", Model: "test", APIMode: "responses"}, &key); err != nil {
		t.Fatal(err)
	}
	sandbox, err := h.preparePromptSandbox(PromptOptions{Sandbox: true, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.close()
	spec, _ := json.Marshal(map[string]any{"transport": "builtin", "env": sandbox.process.env, "toolsets": []string{"web", "code_execution"}})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", mcpLauncher, "EASY_STOCK_MCP_TEST")
	cmd.Env = append(os.Environ(), "EASY_STOCK_MCP_TEST="+string(spec))
	cmd.Dir = sandbox.process.workDir
	cmd.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}` + "\n")
	data, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(data), `"protocolVersion"`) {
		t.Fatalf("MCP handshake: %v %s", err, data)
	}
}

func TestBuiltinMCPBrowserAdapterStartup(t *testing.T) {
	python := os.Getenv("EASY_STOCK_HERMES_TEST_PYTHON")
	if python == "" {
		t.Skip("packaged Python required")
	}
	h := NewHermesRuntime(HermesConfig{Home: t.TempDir(), PythonPath: python})
	key := "fake"
	if err := h.SyncLLM(appsettings.LLM{Provider: "custom", BaseURL: "https://example.invalid/v1", Model: "test", APIMode: "responses"}, &key); err != nil {
		t.Fatal(err)
	}
	sandbox, err := h.preparePromptSandbox(PromptOptions{Sandbox: true, AutoApprove: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.close()
	wrapperDir := filepath.Join(t.TempDir(), "packaged browser 中文")
	if err := os.MkdirAll(wrapperDir, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(wrapperDir, "adapter.json")
	// Probe the real builtin MCP startup boundary. The adapter implementation
	// itself is exercised against native browser fixtures by the Python tests.
	probe := "import json,os,sys\nfrom pathlib import Path\ndef install_hermes_adapter():\n    Path(os.environ['A_STOCK_TEST_ADAPTER_LOG']).write_text(json.dumps({'python':sys.executable,'state':os.getenv('AGENT_BROWSER_STATE'),'model_key':os.getenv('OPENAI_API_KEY')}), encoding='utf-8')\n"
	if err := os.WriteFile(filepath.Join(wrapperDir, "agent-browser.py"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(wrapperDir, "state.json")
	if err := os.WriteFile(state, []byte(`{"cookies":[],"origins":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox.process.env["A_STOCK_AGENT_BROWSER_WRAPPER_DIR"] = wrapperDir
	sandbox.process.env["A_STOCK_TEST_ADAPTER_LOG"] = logPath
	sandbox.process.env["AGENT_BROWSER_STATE"] = state
	spec, err := json.Marshal(map[string]any{"transport": "builtin", "env": sandbox.process.env, "toolsets": []string{"web"}, "browser_state": state})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", mcpLauncher, "EASY_STOCK_MCP_TEST")
	cmd.Env = append(os.Environ(), "EASY_STOCK_MCP_TEST="+string(spec), "OPENAI_API_KEY=fixture-model-secret")
	cmd.Dir = sandbox.process.workDir
	cmd.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}` + "\n")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil || !strings.Contains(string(output), `"protocolVersion"`) {
		t.Fatalf("MCP adapter startup: %v %s %s", err, output, stderr.String())
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil || record["state"] != state || record["model_key"] != nil || record["python"] == "" {
		t.Fatalf("adapter was not isolated or did not receive the selected state: %s, %v", data, err)
	}
}

func TestMCPStdioLauncherPreservesArgumentsAndPipes(t *testing.T) {
	python := os.Getenv("EASY_STOCK_HERMES_TEST_PYTHON")
	if python == "" {
		var err error
		python, err = exec.LookPath("python3")
		if err != nil {
			t.Skip("Python is unavailable")
		}
	}
	for _, subprocessLaunch := range []bool{false, true} {
		t.Run(fmt.Sprintf("subprocess=%v", subprocessLaunch), func(t *testing.T) {
			argument := "A path with spaces\nand a second line"
			child := "import json,os,sys\nprint(json.dumps({'arg':sys.argv[1], 'line':sys.stdin.readline().strip(), 'model_key':os.getenv('OPENAI_API_KEY'), 'tool_key':os.getenv('FIXTURE_SECRET')}),flush=True)\n"
			spec, _ := json.Marshal(map[string]any{"transport": "stdio", "command": python, "args": []string{"-c", child, argument}, "env": map[string]string{"FIXTURE_SECRET": "fixture-only"}})
			launcher := mcpLauncher
			if subprocessLaunch {
				launcher = strings.Replace(launcher, "if os.name == 'nt':", "if True:", 1)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, python, "-c", launcher, "EASY_STOCK_MCP_TEST")
			cmd.Env = append(os.Environ(), "EASY_STOCK_MCP_TEST="+string(spec), "OPENAI_API_KEY=fixture-model-key")
			cmd.Stdin = strings.NewReader("MCP input message\n")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("stdio launcher failed: %v: %s", err, output)
			}
			var result map[string]any
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatalf("stdio output is not JSON: %v: %s", err, output)
			}
			if result["arg"] != argument || result["line"] != "MCP input message" || result["model_key"] != nil || result["tool_key"] != "fixture-only" {
				t.Fatalf("stdio arguments, pipes or credential isolation failed: %v", result)
			}
		})
	}
}
