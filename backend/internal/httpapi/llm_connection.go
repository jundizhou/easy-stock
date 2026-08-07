package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"
)

const llmProbeMarker = "A_STOCK_HERMES_OK"

type llmConnectionTestResult struct {
	OK        bool   `json:"ok"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	APIMode   string `json:"api_mode"`
	Runtime   string `json:"runtime"`
	LatencyMS int64  `json:"latency_ms"`
	Response  string `json:"response"`
}

func (s *Server) settingsLLMTest(w http.ResponseWriter, r *http.Request) {
	if s.hermesGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "Hermes 模型运行时不可用")
		return
	}
	status := s.hermesGateway.Status()
	if !status.Available {
		writeError(w, http.StatusServiceUnavailable, firstNonEmpty(status.Message, "Hermes 运行时不可用"))
		return
	}
	if !status.Configured {
		writeError(w, http.StatusPreconditionFailed, firstNonEmpty(status.Message, "请先配置 Hermes 模型"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	startedAt := time.Now()
	result, err := s.hermesGateway.Prompt(ctx, "这是模型连接探针。请仅回复 "+llmProbeMarker+"，不要添加任何其他文字。")
	if err != nil {
		writeError(w, http.StatusBadGateway, "Hermes 模型连接失败: "+err.Error())
		return
	}
	content := strings.TrimSpace(result.Content)
	if !strings.Contains(strings.ToUpper(content), llmProbeMarker) {
		writeError(w, http.StatusBadGateway, "Hermes 已启动，但模型未返回预期探针标记")
		return
	}

	cfg := s.settingsStore.Snapshot().LLM
	apiMode := strings.TrimSpace(cfg.APIMode)
	if apiMode == "responses" {
		apiMode = "codex_responses"
	}
	if apiMode == "" {
		if strings.TrimSpace(cfg.Provider) == "anthropic" {
			apiMode = "anthropic_messages"
		} else {
			apiMode = "chat_completions"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": llmConnectionTestResult{
		OK:        true,
		Provider:  firstNonEmpty(strings.TrimSpace(cfg.Provider), "openai"),
		Model:     strings.TrimSpace(cfg.Model),
		APIMode:   apiMode,
		Runtime:   "hermes",
		LatencyMS: time.Since(startedAt).Milliseconds(),
		Response:  truncateRunes(content, 200),
	}})
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}
