package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/hermes"
)

func TestAgentSettingsAPIUpdatesSkillsAndMCPWithoutLeakingSecrets(t *testing.T) {
	store, _ := appsettings.Open("")
	gateway := &fakeHermesGateway{agentSettings: hermes.AgentSettings{
		Skills:     []hermes.SkillInfo{{Name: "test-skill", Description: "test", Category: "trading", Enabled: true}},
		MCPServers: []hermes.MCPServerInfo{{Name: "github", Enabled: true, Transport: "stdio", Command: "npx", Env: map[string]string{"TOKEN": "old-secret"}}},
	}}
	server := NewServer(Config{SettingsStore: store, HermesGateway: gateway})
	body := `{"skills":[{"name":"test-skill","enabled":false}],"mcp_servers":[{"name":"github-renamed","original_name":"github","enabled":true,"transport":"stdio","command":"npx","args":["-y","server"],"env":{},"clear_env":[],"headers":{},"clear_headers":[],"timeout":120,"connect_timeout":30,"supports_parallel_tool_calls":true}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/agent", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "old-secret") || !strings.Contains(rec.Body.String(), `"configured":true`) {
		t.Fatalf("agent settings leaked or omitted secret status: %s", rec.Body.String())
	}
	if gateway.agentSettings.Skills[0].Enabled || gateway.agentSettings.MCPServers[0].Name != "github-renamed" || gateway.agentSettings.MCPServers[0].Env["TOKEN"] != "old-secret" {
		t.Fatalf("settings not applied: %+v", gateway.agentSettings)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings/agent", nil)
	getRec := httptest.NewRecorder()
	server.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK || strings.Contains(getRec.Body.String(), "old-secret") {
		t.Fatalf("GET leaked or failed: status=%d body=%s", getRec.Code, getRec.Body.String())
	}
}

func TestAgentSettingsAPIRejectsInvalidMCP(t *testing.T) {
	store, _ := appsettings.Open("")
	server := NewServer(Config{SettingsStore: store, HermesGateway: &fakeHermesGateway{}})
	for _, body := range []string{
		`{"mcp_servers":[{"name":"bad name","enabled":true,"transport":"stdio","command":"npx"}]}`,
		`{"mcp_servers":[{"name":"remote","enabled":true,"transport":"http","url":"file:///tmp/server"}]}`,
		`{"mcp_servers":[{"name":"local","enabled":true,"transport":"stdio","command":"bad\ncommand"}]}`,
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/agent", strings.NewReader(body))
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want 400 body=%s", rec.Code, rec.Body.String())
		}
	}
}
