package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
)

func combinedSettingsFixture(t *testing.T) (*Server, *agent.Service, string, []string) {
	t.Helper()
	root := t.TempDir()
	settingsPath := filepath.Join(root, "settings.json")
	store, err := appsettings.Open(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	home, codexHome := filepath.Join(root, "hermes"), filepath.Join(root, "codex")
	runtime := agent.NewService(agent.ServiceConfig{Hermes: agent.HermesConfig{Home: home}, Codex: agent.CodexConfig{Home: codexHome}})
	server := NewServer(Config{SettingsStore: store, AgentGateway: runtime})
	t.Cleanup(func() { server.Close() })
	rec := notificationRequest(server, http.MethodPut, "/api/v1/settings", `{"llm":{"provider":"custom","base_url":"https://example.invalid/v1","model":"old-model","api_mode":"chat_completions","api_key":"old-key"}}`)
	if rec.Code != 200 {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}
	_, err = runtime.ImportSkills([]agent.SkillImportFile{{Name: "test-skill/SKILL.md", Data: []byte("---\nname: test-skill\ndescription: fixture\n---\nTest")}})
	if err != nil {
		t.Fatal(err)
	}
	current, err := runtime.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	current.MCPServers = []agent.MCPServerInfo{{Name: "original", Enabled: true, Transport: "stdio", Command: "never-executed", Env: map[string]string{"TOKEN": "old-mcp-secret"}}}
	if err := runtime.SyncAgentSettings(current); err != nil {
		t.Fatal(err)
	}
	return server, runtime, settingsPath, []string{filepath.Join(home, "config.yaml"), filepath.Join(home, ".env"), filepath.Join(codexHome, "config.toml")}
}

const combinedSettingsBody = `{"llm":{"model":"changed-model","api_key":"new-model-secret","response_timeout_seconds":600},"notifications":{"feishu":{"enabled":false,"keyword":"changed"}},"agent_settings":{"skills":[{"name":"test-skill","enabled":false}],"mcp_servers":[{"name":"renamed","original_name":"original","enabled":false,"transport":"stdio","command":"never-executed","env":{},"timeout":90}]}}`

func TestCombinedSettingsPersistSkillsMCPAndSecrets(t *testing.T) {
	server, runtime, path, _ := combinedSettingsFixture(t)
	rec := notificationRequest(server, http.MethodPut, "/api/v1/settings", combinedSettingsBody)
	if rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "old-mcp-secret") || strings.Contains(rec.Body.String(), "new-model-secret") {
		t.Fatal("response exposed secrets")
	}
	var response struct {
		AgentSettings agentSettingsView `json:"agent_settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.AgentSettings.Skills) != 1 || response.AgentSettings.Skills[0].Enabled || !response.AgentSettings.MCPServers[0].Env["TOKEN"].Configured {
		t.Fatal("missing saved/redacted Agent state")
	}
	current, err := runtime.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	if current.Skills[0].Enabled || current.MCPServers[0].Enabled || current.MCPServers[0].Name != "renamed" || current.MCPServers[0].Env["TOKEN"] != "old-mcp-secret" {
		t.Fatalf("Agent changes lost: %+v", current)
	}
	reopened, err := appsettings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().LLM.Model != "changed-model" || reopened.Snapshot().LLM.ResponseTimeoutSeconds != 600 || reopened.Snapshot().Notifications.Feishu.Keyword != "changed" {
		t.Fatal("general settings not persisted")
	}
	// Omitting the child settings preserves them; an explicitly empty list deletes all MCP servers.
	for _, body := range []string{`{"llm":{"response_timeout_seconds":300}}`, `{"agent_settings":{"mcp_servers":[]}}`} {
		rec = notificationRequest(server, http.MethodPut, "/api/v1/settings", body)
		if rec.Code != 200 {
			t.Fatalf("partial update: %d %s", rec.Code, rec.Body)
		}
		next, _ := runtime.AgentSettings()
		if !strings.Contains(body, "mcp_servers") && !reflect.DeepEqual(next, current) {
			t.Fatal("omitted Agent state was changed")
		}
		if strings.Contains(body, "mcp_servers") && (len(next.MCPServers) != 0 || next.Skills[0].Enabled) {
			t.Fatal("explicit deletion failed or changed Skill switch")
		}
	}
}

func TestCombinedSettingsValidationAndPersistenceRollback(t *testing.T) {
	for _, failure := range []string{"invalid-mcp", "invalid-notifications", "invalid-reasoning-context", "settings-write"} {
		t.Run(failure, func(t *testing.T) {
			server, runtime, settingsPath, paths := combinedSettingsFixture(t)
			before := server.settingsStore.Snapshot()
			files := map[string]string{}
			for _, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				files[path] = string(data)
			}
			body := combinedSettingsBody
			expectedStatus := 400
			switch failure {
			case "invalid-mcp":
				body = strings.Replace(body, `"timeout":90`, `"timeout":-1`, 1)
			case "invalid-notifications":
				body = strings.Replace(body, `"enabled":false,"keyword":"changed"`, `"enabled":true,"keyword":"changed"`, 1)
			case "invalid-reasoning-context":
				body = strings.Replace(body, `"agent_settings":{`, `"agent_settings":{"reasoning_context":"outdated",`, 1)
				expectedStatus = 409
			case "settings-write":
				if err := os.Remove(settingsPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(settingsPath, 0700); err != nil {
					t.Fatal(err)
				}
				expectedStatus = 500
			}
			rec := notificationRequest(server, http.MethodPut, "/api/v1/settings", body)
			if rec.Code != expectedStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, expectedStatus, rec.Body)
			}
			if !reflect.DeepEqual(server.settingsStore.Snapshot(), before) {
				t.Fatal("failed save changed settings")
			}
			for _, path := range paths {
				data, _ := os.ReadFile(path)
				if string(data) != files[path] {
					t.Fatalf("failed save changed %s", path)
				}
			}
			if key, _ := runtime.ModelAPIKey(); key != "old-key" {
				t.Fatal("failed save changed model credential")
			}
		})
	}
}
