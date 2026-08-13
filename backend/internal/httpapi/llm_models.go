package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/hermes"
)

const maxModelListResponseBytes = 2 << 20

type llmModelsRequest struct {
	Provider  string  `json:"provider"`
	BaseURL   string  `json:"base_url"`
	APIKey    *string `json:"api_key"`
	ProfileID string  `json:"profile_id"`
}

type llmModelOption struct {
	ID          string `json:"id"`
	OwnedBy     string `json:"owned_by,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type llmModelsResult struct {
	Models    []llmModelOption `json:"models"`
	SourceURL string           `json:"source_url"`
}

func (s *Server) settingsLLMModels(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input llmModelsRequest
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid model list request: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	saved := s.settingsStore.Snapshot().LLM
	provider := strings.ToLower(firstNonEmpty(strings.TrimSpace(input.Provider), strings.TrimSpace(saved.Provider), "openai"))
	if !supportedLLMProvider(provider) {
		writeError(w, http.StatusBadRequest, "unsupported llm provider: "+provider)
		return
	}
	baseURL := firstNonEmpty(strings.TrimSpace(input.BaseURL), strings.TrimSpace(saved.BaseURL))
	modelsURL, err := buildModelsURL(provider, baseURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	apiKey := ""
	if input.APIKey != nil {
		apiKey = strings.TrimSpace(*input.APIKey)
	} else if s.hermesGateway != nil {
		if gateway, ok := s.hermesGateway.(hermes.ProfileGateway); ok && strings.TrimSpace(input.ProfileID) != "" {
			apiKey, err = gateway.ModelAPIKeyForProfile(strings.TrimSpace(input.ProfileID))
		} else {
			apiKey, err = s.hermesGateway.ModelAPIKey()
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "读取已保存模型密钥失败")
			return
		}
	}
	if apiKey == "" && provider != "custom" {
		writeError(w, http.StatusPreconditionFailed, "请先输入或保存模型 API Key")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "创建模型列表请求失败")
		return
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "easy-stock/model-discovery")
	if apiKey != "" {
		if provider == "anthropic" {
			request.Header.Set("x-api-key", apiKey)
			request.Header.Set("anthropic-version", "2023-06-01")
		} else {
			request.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "获取模型列表超时")
			return
		}
		writeError(w, http.StatusBadGateway, "无法连接模型服务的模型列表接口")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("模型服务返回 %s，请检查 Base URL 和 API Key", response.Status))
		return
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxModelListResponseBytes+1))
	if err != nil {
		writeError(w, http.StatusBadGateway, "读取模型列表失败")
		return
	}
	if len(body) > maxModelListResponseBytes {
		writeError(w, http.StatusBadGateway, "模型列表响应过大")
		return
	}
	models, err := decodeModelList(body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": llmModelsResult{Models: models, SourceURL: modelsURL}})
}

func supportedLLMProvider(provider string) bool {
	switch provider {
	case "openai", "deepseek", "qwen", "moonshot", "minimax", "zhipu", "siliconflow", "anthropic", "custom":
		return true
	default:
		return false
	}
}

func buildModelsURL(provider, rawBaseURL string) (string, error) {
	if rawBaseURL == "" {
		return "", errors.New("llm base_url is required")
	}
	if len(rawBaseURL) > 512 {
		return "", errors.New("llm base_url is too long")
	}
	parsed, err := url.Parse(rawBaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("llm base_url must be an http or https URL")
	}
	if parsed.User != nil {
		return "", errors.New("llm base_url must not contain user information")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("llm base_url must not contain a query or fragment")
	}

	path := strings.TrimRight(parsed.Path, "/")
	if provider == "anthropic" && path == "" {
		path = "/v1"
	}
	if !strings.HasSuffix(path, "/models") {
		path += "/models"
	}
	if path == "models" {
		path = "/models"
	}
	parsed.Path = path
	parsed.RawPath = ""
	return parsed.String(), nil
}

func decodeModelList(body []byte) ([]llmModelOption, error) {
	var payload struct {
		Data []struct {
			ID          string `json:"id"`
			OwnedBy     string `json:"owned_by"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("模型列表响应不是有效 JSON")
	}
	seen := make(map[string]struct{}, len(payload.Data))
	models := make([]llmModelOption, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || len(id) > 256 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, llmModelOption{ID: id, OwnedBy: strings.TrimSpace(item.OwnedBy), DisplayName: strings.TrimSpace(item.DisplayName)})
	}
	if len(models) == 0 {
		return nil, errors.New("模型服务没有返回可用模型")
	}
	sort.Slice(models, func(i, j int) bool {
		return strings.ToLower(models[i].ID) < strings.ToLower(models[j].ID)
	})
	return models, nil
}
