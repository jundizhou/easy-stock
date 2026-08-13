package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"easy-stock/backend/internal/hermes"
)

var (
	mcpNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type agentSettingsView struct {
	ReasoningEffort string             `json:"reasoning_effort"`
	Skills          []hermes.SkillInfo `json:"skills"`
	MCPServers      []mcpServerView    `json:"mcp_servers"`
}

type mcpServerView struct {
	Name                     string                         `json:"name"`
	Enabled                  bool                           `json:"enabled"`
	Transport                string                         `json:"transport"`
	Command                  string                         `json:"command,omitempty"`
	Args                     []string                       `json:"args,omitempty"`
	Env                      map[string]secretSettingStatus `json:"env,omitempty"`
	URL                      string                         `json:"url,omitempty"`
	Headers                  map[string]secretSettingStatus `json:"headers,omitempty"`
	Timeout                  int                            `json:"timeout,omitempty"`
	ConnectTimeout           int                            `json:"connect_timeout,omitempty"`
	SupportsParallelToolCall bool                           `json:"supports_parallel_tool_calls,omitempty"`
}

type agentSettingsUpdateRequest struct {
	ReasoningEffort *string            `json:"reasoning_effort"`
	Skills          *[]skillUpdate     `json:"skills"`
	MCPServers      *[]mcpServerUpdate `json:"mcp_servers"`
}

type skillUpdate struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type mcpServerUpdate struct {
	Name                     string             `json:"name"`
	OriginalName             string             `json:"original_name"`
	Enabled                  bool               `json:"enabled"`
	Transport                string             `json:"transport"`
	Command                  string             `json:"command"`
	Args                     []string           `json:"args"`
	Env                      map[string]*string `json:"env"`
	ClearEnv                 []string           `json:"clear_env"`
	URL                      string             `json:"url"`
	Headers                  map[string]*string `json:"headers"`
	ClearHeaders             []string           `json:"clear_headers"`
	Timeout                  int                `json:"timeout"`
	ConnectTimeout           int                `json:"connect_timeout"`
	SupportsParallelToolCall bool               `json:"supports_parallel_tool_calls"`
}

func (s *Server) settingsAgentGateway() (hermes.SettingsGateway, bool) {
	gateway, ok := s.hermesGateway.(hermes.SettingsGateway)
	return gateway, ok
}

func (s *Server) settingsAgentGet(w http.ResponseWriter, r *http.Request) {
	gateway, ok := s.settingsAgentGateway()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Hermes Skill/MCP 配置服务不可用")
		return
	}
	settings, err := gateway.AgentSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 Hermes Skill/MCP 设置: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": buildAgentSettingsView(settings)})
}

func (s *Server) settingsAgentUpdate(w http.ResponseWriter, r *http.Request) {
	gateway, ok := s.settingsAgentGateway()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Hermes Skill/MCP 配置服务不可用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request agentSettingsUpdateRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent settings request: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateAgentSettingsUpdate(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := gateway.AgentSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取现有 Hermes 设置: "+err.Error())
		return
	}
	settings := mergeAgentSettings(current, request)
	if err := gateway.SyncAgentSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, "保存 Hermes Skill/MCP 设置: "+err.Error())
		return
	}
	updated, err := gateway.AgentSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "重新读取 Hermes 设置: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": buildAgentSettingsView(updated)})
}

func buildAgentSettingsView(settings hermes.AgentSettings) agentSettingsView {
	view := agentSettingsView{ReasoningEffort: settings.ReasoningEffort, Skills: settings.Skills, MCPServers: make([]mcpServerView, 0, len(settings.MCPServers))}
	for _, server := range settings.MCPServers {
		item := mcpServerView{
			Name: server.Name, Enabled: server.Enabled, Transport: server.Transport,
			Command: server.Command, Args: server.Args, URL: server.URL,
			Timeout: server.Timeout, ConnectTimeout: server.ConnectTimeout,
			SupportsParallelToolCall: server.SupportsParallelToolCall,
		}
		if len(server.Env) > 0 {
			item.Env = map[string]secretSettingStatus{}
			for key, value := range server.Env {
				item.Env[key] = secretStatus(value)
			}
		}
		if len(server.Headers) > 0 {
			item.Headers = map[string]secretSettingStatus{}
			for key, value := range server.Headers {
				item.Headers[key] = secretStatus(value)
			}
		}
		view.MCPServers = append(view.MCPServers, item)
	}
	return view
}

func mergeAgentSettings(current hermes.AgentSettings, request agentSettingsUpdateRequest) hermes.AgentSettings {
	if request.ReasoningEffort != nil {
		current.ReasoningEffort = strings.ToLower(strings.TrimSpace(*request.ReasoningEffort))
	}
	if request.Skills != nil {
		requestedSkills := map[string]bool{}
		for _, skill := range *request.Skills {
			requestedSkills[strings.TrimSpace(skill.Name)] = skill.Enabled
		}
		for index := range current.Skills {
			if enabled, ok := requestedSkills[current.Skills[index].Name]; ok {
				current.Skills[index].Enabled = enabled
			}
		}
	}
	if request.MCPServers == nil {
		return current
	}
	existingServers := map[string]hermes.MCPServerInfo{}
	for _, server := range current.MCPServers {
		existingServers[server.Name] = server
	}
	current.MCPServers = make([]hermes.MCPServerInfo, 0, len(*request.MCPServers))
	for _, input := range *request.MCPServers {
		name := strings.TrimSpace(input.Name)
		originalName := strings.TrimSpace(input.OriginalName)
		if originalName == "" {
			originalName = name
		}
		server := existingServers[originalName]
		server.Name = name
		server.Enabled = input.Enabled
		server.Transport = strings.TrimSpace(input.Transport)
		server.Command = strings.TrimSpace(input.Command)
		server.Args = append([]string(nil), input.Args...)
		server.URL = strings.TrimSpace(input.URL)
		server.Timeout = input.Timeout
		server.ConnectTimeout = input.ConnectTimeout
		server.SupportsParallelToolCall = input.SupportsParallelToolCall
		server.Env = mergeProtectedMap(server.Env, input.Env, input.ClearEnv)
		server.Headers = mergeProtectedMap(server.Headers, input.Headers, input.ClearHeaders)
		current.MCPServers = append(current.MCPServers, server)
	}
	return current
}

func mergeProtectedMap(existing map[string]string, updates map[string]*string, clear []string) map[string]string {
	result := map[string]string{}
	for key, value := range existing {
		result[key] = value
	}
	for _, key := range clear {
		delete(result, strings.TrimSpace(key))
	}
	for key, value := range updates {
		if value != nil && strings.TrimSpace(*value) != "" {
			result[strings.TrimSpace(key)] = strings.TrimSpace(*value)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func validateAgentSettingsUpdate(request agentSettingsUpdateRequest) error {
	if request.ReasoningEffort != nil {
		effort := strings.ToLower(strings.TrimSpace(*request.ReasoningEffort))
		if !hermes.IsValidReasoningEffort(effort) {
			return fmt.Errorf("无效的思考等级: %s", *request.ReasoningEffort)
		}
	}
	if request.Skills != nil && len(*request.Skills) > 500 || request.MCPServers != nil && len(*request.MCPServers) > 100 {
		return fmt.Errorf("Skill 或 MCP Server 数量过多")
	}
	seenSkills := map[string]bool{}
	if request.Skills != nil {
		for _, skill := range *request.Skills {
			name := strings.TrimSpace(skill.Name)
			if name == "" || len(name) > 160 || seenSkills[name] {
				return fmt.Errorf("Skill 名称无效或重复")
			}
			seenSkills[name] = true
		}
	}
	seenServers := map[string]bool{}
	if request.MCPServers == nil {
		return nil
	}
	for _, server := range *request.MCPServers {
		name := strings.TrimSpace(server.Name)
		if !mcpNamePattern.MatchString(name) || seenServers[name] {
			return fmt.Errorf("MCP Server 名称必须为 1-64 位字母、数字、点、下划线或短横线，且不能重复")
		}
		seenServers[name] = true
		if originalName := strings.TrimSpace(server.OriginalName); originalName != "" && !mcpNamePattern.MatchString(originalName) {
			return fmt.Errorf("MCP Server %s 的原名称无效", name)
		}
		transport := strings.TrimSpace(server.Transport)
		if transport != "stdio" && transport != "http" && transport != "sse" {
			return fmt.Errorf("MCP Server %s 的传输方式无效", name)
		}
		if transport == "stdio" {
			command := strings.TrimSpace(server.Command)
			if command == "" || len(command) > 1024 || strings.ContainsAny(command, "\r\n") {
				return fmt.Errorf("MCP Server %s 需要有效的启动命令", name)
			}
			if len(server.Args) > 100 {
				return fmt.Errorf("MCP Server %s 的参数过多", name)
			}
			for _, arg := range server.Args {
				if len(arg) > 4096 || strings.ContainsAny(arg, "\r\n") {
					return fmt.Errorf("MCP Server %s 包含无效参数", name)
				}
			}
		} else {
			endpoint := strings.TrimSpace(server.URL)
			parsed, err := url.Parse(endpoint)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || len(endpoint) > 2048 {
				return fmt.Errorf("MCP Server %s 需要有效的 HTTP/HTTPS URL", name)
			}
		}
		if server.Timeout < 0 || server.Timeout > 3600 || server.ConnectTimeout < 0 || server.ConnectTimeout > 600 {
			return fmt.Errorf("MCP Server %s 的超时时间无效", name)
		}
		if err := validateProtectedMap(name, "环境变量", server.Env, server.ClearEnv, true); err != nil {
			return err
		}
		if err := validateProtectedMap(name, "请求头", server.Headers, server.ClearHeaders, false); err != nil {
			return err
		}
	}
	return nil
}

func validateProtectedMap(serverName, label string, values map[string]*string, clear []string, env bool) error {
	if len(values)+len(clear) > 100 {
		return fmt.Errorf("MCP Server %s 的%s过多", serverName, label)
	}
	keys := map[string]bool{}
	for key, value := range values {
		key = strings.TrimSpace(key)
		if keys[key] {
			return fmt.Errorf("MCP Server %s 的%s键重复", serverName, label)
		}
		keys[key] = true
		if key == "" || (env && !envNamePattern.MatchString(key)) || strings.ContainsAny(key, "\r\n:") || (value != nil && (len(*value) > 32<<10 || strings.ContainsAny(*value, "\r\n"))) {
			return fmt.Errorf("MCP Server %s 包含无效%s", serverName, label)
		}
	}
	for _, key := range clear {
		key = strings.TrimSpace(key)
		if key == "" || (env && !envNamePattern.MatchString(key)) || strings.ContainsAny(key, "\r\n:") {
			return fmt.Errorf("MCP Server %s 包含无效%s", serverName, label)
		}
		if keys[key] {
			return fmt.Errorf("MCP Server %s 的%s键重复", serverName, label)
		}
		keys[key] = true
	}
	return nil
}
