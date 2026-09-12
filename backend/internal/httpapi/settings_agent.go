package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

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

type skillImporter interface {
	ImportSkills([]hermes.SkillImportFile) ([]hermes.InstalledSkill, error)
}

type skillRemover interface {
	DeleteSkill(name string) error
}

type skillMarketEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Repository  string `json:"repository"`
	Path        string `json:"path"`
	Category    string `json:"category"`
}

type skillMarketSource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Region      string `json:"region"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

var curatedSkillMarket = []skillMarketEntry{
	{ID: "a-share-market-overview", Name: "A 股市场总览", Description: "分析大盘、市场宽度、行业动量、资金流向、涨跌停和市场情绪。", Repository: "https://github.com/jundizhou/easy-stock", Path: "https://github.com/jundizhou/easy-stock/tree/main/.agents/skills/a-share-market-overview", Category: "A 股"},
	{ID: "a-share-stock-analysis", Name: "A 股个股研究", Description: "结合行情、K 线、题材、主营业务和基本面研究 A 股个股。", Repository: "https://github.com/jundizhou/easy-stock", Path: "https://github.com/jundizhou/easy-stock/tree/main/.agents/skills/a-share-stock-analysis", Category: "A 股"},
	{ID: "a-share-review", Name: "A 股盘后复盘", Description: "整理题材演化、连板梯队、情绪周期和次日观察清单。", Repository: "https://github.com/jundizhou/easy-stock", Path: "https://github.com/jundizhou/easy-stock/tree/main/.agents/skills/a-share-review", Category: "A 股"},
}

var skillMarketSources = []skillMarketSource{
	{ID: "skillhub-cn", Name: "SkillHub", Region: "中国", URL: "https://skillhub.cn/", Description: "面向中国用户的 AI Skills 社区，适合中文技能发现。"},
	{ID: "skillsmp", Name: "SkillsMP", Region: "海外", URL: "https://skillsmp.com/", Description: "跨平台 Skill 搜索与发现目录。"},
	{ID: "skills-sh", Name: "skills.sh", Region: "海外", URL: "https://skills.sh/", Description: "Vercel 社区维护的 Agent Skills 目录。"},
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

func (s *Server) settingsAgentSkillImport(w http.ResponseWriter, r *http.Request) {
	importer, ok := s.hermesGateway.(skillImporter)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Hermes Skill 导入服务不可用")
		return
	}
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "读取 Skill 文件失败: "+err.Error())
		return
	}
	parts := r.MultipartForm.File["files"]
	paths := r.MultipartForm.Value["paths"]
	if len(parts) == 0 || len(parts) > 1000 {
		writeError(w, http.StatusBadRequest, "请至少选择一个 Skill 文件，且文件数量不超过 1000 个")
		return
	}
	files := make([]hermes.SkillImportFile, 0, len(parts))
	for index, header := range parts {
		limit := int64(8 << 20)
		if strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
			limit = 128 << 20
		}
		if header.Size > limit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("文件 %s 超过 %d MB 限制", header.Filename, limit/(1<<20)))
			return
		}
		file, err := header.Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, "打开 Skill 文件失败: "+err.Error())
			return
		}
		data, readErr := io.ReadAll(io.LimitReader(file, limit+1))
		_ = file.Close()
		if readErr != nil {
			writeError(w, http.StatusBadRequest, "读取 Skill 文件失败: "+readErr.Error())
			return
		}
		if int64(len(data)) > limit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("文件 %s 超过 %d MB 限制", header.Filename, limit/(1<<20)))
			return
		}
		name := header.Filename
		if index < len(paths) && strings.TrimSpace(paths[index]) != "" {
			name = paths[index]
		}
		files = append(files, hermes.SkillImportFile{Name: name, Data: data})
	}
	installed, err := importer.ImportSkills(files)
	if err != nil {
		writeError(w, http.StatusBadRequest, "导入 Skill 失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": installed})
}

func (s *Server) settingsAgentSkillMarket(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": curatedSkillMarket})
}

func (s *Server) settingsAgentSkillDelete(w http.ResponseWriter, r *http.Request) {
	remover, ok := s.hermesGateway.(skillRemover)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Hermes Skill 删除服务不可用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var request struct {
		Name string `json:"name"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "无效的 Skill 删除请求: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "请提供要删除的 Skill 名称")
		return
	}
	if err := remover.DeleteSkill(name); err != nil {
		writeError(w, http.StatusBadRequest, "删除 Skill 失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"name": name}})
}

func (s *Server) settingsAgentSkillMarketSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": skillMarketSources})
}

func (s *Server) settingsAgentSkillInstallGit(w http.ResponseWriter, r *http.Request) {
	importer, ok := s.hermesGateway.(skillImporter)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Hermes Skill 导入服务不可用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var request struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "无效的 Skill 仓库请求: "+err.Error())
		return
	}
	source, prefix, err := githubSkillArchiveURL(request.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	streamProgress := r.URL.Query().Get("progress") == "1"
	var emitProgress func(skillDownloadProgress)
	if streamProgress {
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "当前服务不支持下载进度")
			return
		}
		emitProgress = func(progress skillDownloadProgress) {
			_ = json.NewEncoder(w).Encode(progress)
			flusher.Flush()
		}
		// Keep the user-facing link on GitHub's repository/tree page. The
		// codeload URL above is an implementation detail and would immediately
		// download the ZIP when opened in a browser.
		emitProgress(skillDownloadProgress{Type: "started", URL: strings.TrimSpace(request.URL)})
	}
	fail := func(status int, message string) {
		if emitProgress != nil {
			emitProgress(skillDownloadProgress{Type: "error", Error: message})
			return
		}
		writeError(w, status, message)
	}
	// GitHub's codeload endpoint can take tens of seconds to start sending a
	// repository archive. Keep the request cancellable while giving the archive
	// enough time to arrive.
	client := &http.Client{Timeout: 2 * time.Minute}
	requestCtx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	downloadRequest, err := http.NewRequestWithContext(requestCtx, http.MethodGet, source, nil)
	if err != nil {
		fail(http.StatusBadGateway, "下载 Skill 仓库失败: "+err.Error())
		return
	}
	response, err := client.Do(downloadRequest)
	if err != nil {
		fail(http.StatusBadGateway, "下载 Skill 仓库失败（等待时间超过 120 秒或网络中断）: "+err.Error())
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fail(http.StatusBadGateway, fmt.Sprintf("下载 Skill 仓库失败: HTTP %d", response.StatusCode))
		return
	}
	data, err := readSkillArchive(response.Body, response.ContentLength, emitProgress)
	if err != nil || len(data) > 128<<20 {
		fail(http.StatusBadGateway, "Skill 仓库压缩包过大或读取失败")
		return
	}
	if prefix != "" {
		data, err = filterGitHubSkillArchive(data, prefix)
		if err != nil {
			fail(http.StatusBadRequest, "读取 GitHub Skill 目录失败: "+err.Error())
			return
		}
	}
	if emitProgress != nil {
		emitProgress(skillDownloadProgress{Type: "processing", Downloaded: int64(len(data)), Total: int64(len(data))})
	}
	installed, err := importer.ImportSkills([]hermes.SkillImportFile{{Name: "github-skill.zip", Data: data}})
	if err != nil {
		fail(http.StatusBadRequest, "安装 Skill 失败: "+err.Error())
		return
	}
	for i := range installed {
		installed[i].Source = request.URL
	}
	if emitProgress != nil {
		emitProgress(skillDownloadProgress{Type: "complete", Data: installed})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": installed})
}

type skillDownloadProgress struct {
	Type           string                  `json:"type"`
	URL            string                  `json:"url,omitempty"`
	Downloaded     int64                   `json:"downloaded,omitempty"`
	Total          int64                   `json:"total,omitempty"`
	BytesPerSecond int64                   `json:"bytes_per_second,omitempty"`
	Data           []hermes.InstalledSkill `json:"data,omitempty"`
	Error          string                  `json:"error,omitempty"`
}

func readSkillArchive(source io.Reader, total int64, emit func(skillDownloadProgress)) ([]byte, error) {
	const chunkSize = 32 << 10
	data := bytes.NewBuffer(nil)
	data.Grow(1 << 20)
	buffer := make([]byte, chunkSize)
	started := time.Now()
	var downloaded int64
	lastUpdate := time.Time{}
	for {
		n, err := source.Read(buffer)
		if n > 0 {
			downloading := int64(n)
			downloaded += downloading
			if downloaded > 128<<20 {
				return nil, errors.New("Skill 仓库压缩包超过 128 MB 限制")
			}
			_, _ = data.Write(buffer[:n])
			if emit != nil && (lastUpdate.IsZero() || time.Since(lastUpdate) >= 200*time.Millisecond) {
				elapsed := time.Since(started).Seconds()
				rate := int64(0)
				if elapsed > 0 {
					rate = int64(float64(downloaded) / elapsed)
				}
				emit(skillDownloadProgress{Type: "progress", Downloaded: downloaded, Total: total, BytesPerSecond: rate})
				lastUpdate = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if emit != nil {
		elapsed := time.Since(started).Seconds()
		rate := int64(0)
		if elapsed > 0 {
			rate = int64(float64(downloaded) / elapsed)
		}
		emit(skillDownloadProgress{Type: "progress", Downloaded: downloaded, Total: total, BytesPerSecond: rate})
	}
	return data.Bytes(), nil
}

func githubSkillArchiveURL(raw string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" {
		return "", "", errors.New("目前只支持 HTTPS GitHub 仓库地址")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("GitHub 地址应为 https://github.com/用户名/仓库")
	}
	prefix := ""
	if len(parts) >= 5 && parts[2] == "tree" {
		prefix = strings.Join(parts[4:], "/")
	}
	return "https://codeload.github.com/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(strings.TrimSuffix(parts[1], ".git")) + "/zip/HEAD", prefix, nil
}

func filterGitHubSkillArchive(data []byte, prefix string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	prefix = strings.Trim(prefix, "/") + "/"
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	matched := 0
	for _, entry := range reader.File {
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 || strings.HasSuffix(entry.Name, "/") {
			continue
		}
		parts := strings.SplitN(entry.Name, "/", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[1], prefix) {
			continue
		}
		name := strings.TrimPrefix(parts[1], prefix)
		if name == "" {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(rc, 8<<20+1))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		if len(content) > 8<<20 {
			return nil, errors.New("目录内文件超过 8 MB 限制")
		}
		file, err := writer.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := file.Write(content); err != nil {
			return nil, err
		}
		matched++
	}
	if matched == 0 {
		return nil, errors.New("目录中未找到文件")
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
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
