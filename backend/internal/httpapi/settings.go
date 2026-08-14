package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/hermes"
)

type secretSettingStatus struct {
	Configured bool   `json:"configured"`
	Masked     string `json:"masked,omitempty"`
}

type settingsView struct {
	Hermes             hermes.Status    `json:"hermes"`
	ActiveLLMProfileID string           `json:"active_llm_profile_id"`
	LLMProfiles        []llmProfileView `json:"llm_profiles"`
	LLM                struct {
		Provider               string              `json:"provider"`
		BaseURL                string              `json:"base_url"`
		Model                  string              `json:"model"`
		APIMode                string              `json:"api_mode"`
		ResponseTimeoutSeconds int                 `json:"response_timeout_seconds"`
		APIKey                 secretSettingStatus `json:"api_key"`
	} `json:"llm"`
	Credentials struct {
		TushareToken    secretSettingStatus `json:"tushare_token"`
		THSCookie       secretSettingStatus `json:"ths_cookie"`
		XueqiuCookie    secretSettingStatus `json:"xueqiu_cookie"`
		EastMoneyCookie secretSettingStatus `json:"eastmoney_cookie"`
		WeChatAPIToken  secretSettingStatus `json:"wechat_api_token"`
	} `json:"credentials"`
	ReviewAutomation struct {
		Profiles []reviewSourceProfileView `json:"profiles"`
	} `json:"review_automation"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type llmProfileView struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Provider string              `json:"provider"`
	BaseURL  string              `json:"base_url"`
	Model    string              `json:"model"`
	APIMode  string              `json:"api_mode"`
	APIKey   secretSettingStatus `json:"api_key"`
}

type llmProfileUpdate struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Provider    string  `json:"provider"`
	BaseURL     string  `json:"base_url"`
	Model       string  `json:"model"`
	APIMode     string  `json:"api_mode"`
	APIKey      *string `json:"api_key"`
	ClearAPIKey bool    `json:"clear_api_key"`
}

type reviewSourceProfileView struct {
	ID          string              `json:"id"`
	Source      string              `json:"source"`
	Name        string              `json:"name"`
	BaseURL     string              `json:"base_url"`
	Credential  secretSettingStatus `json:"credential"`
	SyncHour    int                 `json:"sync_hour"`
	AutoAnalyze bool                `json:"auto_analyze"`
	Enabled     bool                `json:"enabled"`
}

type reviewSourceProfileUpdate struct {
	ID              string  `json:"id"`
	Source          string  `json:"source"`
	Name            string  `json:"name"`
	BaseURL         string  `json:"base_url"`
	Credential      *string `json:"credential"`
	ClearCredential bool    `json:"clear_credential"`
	SyncHour        int     `json:"sync_hour"`
	AutoAnalyze     bool    `json:"auto_analyze"`
	Enabled         bool    `json:"enabled"`
}

type settingsUpdateRequest struct {
	LLMProfiles        *[]llmProfileUpdate `json:"llm_profiles"`
	ActiveLLMProfileID *string             `json:"active_llm_profile_id"`
	LLM                struct {
		Provider               *string `json:"provider"`
		BaseURL                *string `json:"base_url"`
		Model                  *string `json:"model"`
		APIMode                *string `json:"api_mode"`
		ResponseTimeoutSeconds *int    `json:"response_timeout_seconds"`
		APIKey                 *string `json:"api_key"`
	} `json:"llm"`
	Credentials struct {
		TushareToken    *string `json:"tushare_token"`
		THSCookie       *string `json:"ths_cookie"`
		XueqiuCookie    *string `json:"xueqiu_cookie"`
		EastMoneyCookie *string `json:"eastmoney_cookie"`
		WeChatAPIToken  *string `json:"wechat_api_token"`
	} `json:"credentials"`
	ClearSecrets     []string `json:"clear_secrets"`
	ReviewAutomation struct {
		Profiles *[]reviewSourceProfileUpdate `json:"profiles"`
	} `json:"review_automation"`
}

func (s *Server) settingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": s.buildSettingsView(s.settingsStore.Snapshot())})
}

func (s *Server) settingsUpdate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request settingsUpdateRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid settings request: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateSettingsUpdate(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var llmAPIKeyUpdate *string
	profileKeyUpdates := map[string]*string{}
	if request.LLMProfiles != nil {
		for _, profile := range *request.LLMProfiles {
			id := strings.TrimSpace(profile.ID)
			if profile.ClearAPIKey {
				value := ""
				profileKeyUpdates[id] = &value
			} else if profile.APIKey != nil && strings.TrimSpace(*profile.APIKey) != "" {
				value := strings.TrimSpace(*profile.APIKey)
				profileKeyUpdates[id] = &value
			}
		}
	}
	if request.LLM.APIKey != nil && strings.TrimSpace(*request.LLM.APIKey) != "" {
		value := strings.TrimSpace(*request.LLM.APIKey)
		llmAPIKeyUpdate = &value
	}
	for _, key := range request.ClearSecrets {
		if key == "llm_api_key" {
			value := ""
			llmAPIKeyUpdate = &value
			break
		}
	}
	if (llmAPIKeyUpdate != nil || len(profileKeyUpdates) > 0) && s.hermesGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "Hermes 配置服务不可用")
		return
	}
	values, err := s.settingsStore.Update(func(values *appsettings.Values) error {
		if request.LLMProfiles != nil {
			existingConfigured := map[string]bool{}
			for _, profile := range values.LLMProfiles {
				existingConfigured[profile.ID] = profile.APIKeyConfigured
			}
			profiles := make([]appsettings.LLMProfile, 0, len(*request.LLMProfiles))
			for _, input := range *request.LLMProfiles {
				id := strings.TrimSpace(input.ID)
				configured := existingConfigured[id]
				if update, ok := profileKeyUpdates[id]; ok {
					configured = strings.TrimSpace(*update) != ""
				}
				profiles = append(profiles, appsettings.LLMProfile{ID: id, Name: strings.TrimSpace(input.Name), Provider: strings.TrimSpace(input.Provider), BaseURL: strings.TrimSpace(input.BaseURL), Model: strings.TrimSpace(input.Model), APIMode: normalizeAPIMode(input.APIMode, input.Provider), APIKeyConfigured: configured})
			}
			values.LLMProfiles = profiles
		}
		if request.ActiveLLMProfileID != nil {
			values.ActiveLLMProfileID = strings.TrimSpace(*request.ActiveLLMProfileID)
		}
		if active, ok := findLLMProfile(values.LLMProfiles, values.ActiveLLMProfileID); ok {
			values.LLM = appsettings.LLM{Provider: active.Provider, BaseURL: active.BaseURL, Model: active.Model, APIMode: active.APIMode, ResponseTimeoutSeconds: appsettings.NormalizeLLMResponseTimeoutSeconds(values.LLM.ResponseTimeoutSeconds)}
		}
		applyOptionalString(&values.LLM.Provider, request.LLM.Provider)
		applyOptionalString(&values.LLM.BaseURL, request.LLM.BaseURL)
		applyOptionalString(&values.LLM.Model, request.LLM.Model)
		applyOptionalString(&values.LLM.APIMode, request.LLM.APIMode)
		if request.LLM.ResponseTimeoutSeconds != nil {
			values.LLM.ResponseTimeoutSeconds = *request.LLM.ResponseTimeoutSeconds
		}
		values.LLM.ResponseTimeoutSeconds = appsettings.NormalizeLLMResponseTimeoutSeconds(values.LLM.ResponseTimeoutSeconds)
		// LLM secrets are owned by Hermes' .env and must never be persisted in
		// the application's general settings file.
		values.LLM.APIKey = ""
		if request.LLMProfiles == nil && hasLLMUpdate(request) {
			upsertActiveLLMProfile(values)
		}
		applyOptionalSecret(&values.Credentials.TushareToken, request.Credentials.TushareToken)
		applyOptionalSecret(&values.Credentials.THSCookie, request.Credentials.THSCookie)
		// Snowball authentication is now owned by Electron's isolated browser
		// partitions. Remove the legacy shared cookie whenever settings are saved.
		values.Credentials.XueqiuCookie = ""
		applyOptionalSecret(&values.Credentials.EastMoneyCookie, request.Credentials.EastMoneyCookie)
		applyOptionalSecret(&values.Credentials.WeChatAPIToken, request.Credentials.WeChatAPIToken)
		for _, key := range request.ClearSecrets {
			switch key {
			case "llm_api_key":
				values.LLM.APIKey = ""
			case "tushare_token":
				values.Credentials.TushareToken = ""
			case "ths_cookie":
				values.Credentials.THSCookie = ""
			case "xueqiu_cookie":
				values.Credentials.XueqiuCookie = ""
			case "eastmoney_cookie":
				values.Credentials.EastMoneyCookie = ""
			case "wechat_api_token":
				values.Credentials.WeChatAPIToken = ""
			}
		}
		if request.ReviewAutomation.Profiles != nil {
			existing := map[string]appsettings.ReviewSourceProfile{}
			for _, profile := range values.ReviewAutomation.Profiles {
				existing[profile.ID] = profile
			}
			profiles := make([]appsettings.ReviewSourceProfile, 0, len(*request.ReviewAutomation.Profiles))
			for index, input := range *request.ReviewAutomation.Profiles {
				id := strings.TrimSpace(input.ID)
				if id == "" {
					id = fmt.Sprintf("%s-%d-%d", strings.TrimSpace(input.Source), time.Now().UnixNano(), index)
				}
				credential := existing[id].Credential
				if input.Source == "xueqiu" {
					credential = ""
				} else if input.ClearCredential {
					credential = ""
				} else if input.Credential != nil && strings.TrimSpace(*input.Credential) != "" {
					credential = strings.TrimSpace(*input.Credential)
				}
				profiles = append(profiles, appsettings.ReviewSourceProfile{ID: id, Source: strings.TrimSpace(input.Source), Name: strings.TrimSpace(input.Name), BaseURL: strings.TrimRight(strings.TrimSpace(input.BaseURL), "/"), Credential: credential, SyncHour: input.SyncHour, AutoAnalyze: input.AutoAnalyze, Enabled: input.Enabled})
			}
			values.ReviewAutomation.Profiles = profiles
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save settings: "+err.Error())
		return
	}
	if s.hermesGateway != nil {
		profileGateway, supportsProfiles := s.hermesGateway.(hermes.ProfileGateway)
		if supportsProfiles {
			for id, update := range profileKeyUpdates {
				if id != values.ActiveLLMProfileID {
					if err := profileGateway.StoreLLMProfileKey(id, update); err != nil {
						writeError(w, http.StatusInternalServerError, "保存模型配置密钥: "+err.Error())
						return
					}
				}
			}
			activeUpdate := profileKeyUpdates[values.ActiveLLMProfileID]
			if activeUpdate == nil {
				activeUpdate = llmAPIKeyUpdate
			}
			if err := profileGateway.SyncLLMProfile(values.LLM, values.ActiveLLMProfileID, activeUpdate); err != nil {
				writeError(w, http.StatusInternalServerError, "同步 Hermes 设置: "+err.Error())
				return
			}
		} else if err := s.hermesGateway.SyncLLM(values.LLM, llmAPIKeyUpdate); err != nil {
			writeError(w, http.StatusInternalServerError, "同步 Hermes 设置: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.buildSettingsView(values)})
}

func validateSettingsUpdate(request settingsUpdateRequest) error {
	if request.LLMProfiles != nil {
		if len(*request.LLMProfiles) == 0 || len(*request.LLMProfiles) > 20 {
			return fmt.Errorf("模型配置数量必须在 1 到 20 之间")
		}
		seen := map[string]bool{}
		for _, profile := range *request.LLMProfiles {
			id := strings.TrimSpace(profile.ID)
			if id == "" || len(id) > 100 || seen[id] {
				return fmt.Errorf("模型配置 ID 无效或重复")
			}
			seen[id] = true
			if strings.TrimSpace(profile.Name) == "" || len([]rune(profile.Name)) > 80 {
				return fmt.Errorf("模型配置名称不能为空且最多 80 个字符")
			}
			provider := profile.Provider
			baseURL := profile.BaseURL
			model := profile.Model
			apiMode := profile.APIMode
			copyRequest := settingsUpdateRequest{}
			copyRequest.LLM.Provider = &provider
			copyRequest.LLM.BaseURL = &baseURL
			copyRequest.LLM.Model = &model
			copyRequest.LLM.APIMode = &apiMode
			if err := validateSingleLLM(copyRequest); err != nil {
				return err
			}
			if profile.APIKey != nil && len(*profile.APIKey) > 16<<10 {
				return fmt.Errorf("a secret value is too long")
			}
		}
		if request.ActiveLLMProfileID != nil && !seen[strings.TrimSpace(*request.ActiveLLMProfileID)] {
			return fmt.Errorf("当前模型配置不存在")
		}
	}
	return validateSingleLLM(request)
}

func validateSingleLLM(request settingsUpdateRequest) error {
	if request.LLM.Provider != nil {
		provider := strings.TrimSpace(*request.LLM.Provider)
		allowed := map[string]bool{"": true, "openai": true, "deepseek": true, "qwen": true, "moonshot": true, "minimax": true, "zhipu": true, "siliconflow": true, "anthropic": true, "custom": true}
		if !allowed[provider] {
			return fmt.Errorf("unsupported llm provider: %s", provider)
		}
	}
	if request.LLM.BaseURL != nil {
		baseURL := strings.TrimSpace(*request.LLM.BaseURL)
		if len(baseURL) > 512 {
			return fmt.Errorf("llm base_url is too long")
		}
		if baseURL != "" {
			parsed, err := url.Parse(baseURL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				return fmt.Errorf("llm base_url must be an http or https URL")
			}
		}
	}
	if request.LLM.Model != nil && len(strings.TrimSpace(*request.LLM.Model)) > 160 {
		return fmt.Errorf("llm model is too long")
	}
	if request.LLM.APIMode != nil {
		apiMode := strings.TrimSpace(*request.LLM.APIMode)
		allowed := map[string]bool{"": true, "chat_completions": true, "responses": true, "codex_responses": true, "anthropic_messages": true}
		if !allowed[apiMode] {
			return fmt.Errorf("unsupported llm api_mode: %s", apiMode)
		}
	}
	if request.LLM.ResponseTimeoutSeconds != nil {
		if *request.LLM.ResponseTimeoutSeconds < appsettings.MinLLMResponseTimeoutSeconds || *request.LLM.ResponseTimeoutSeconds > appsettings.MaxLLMResponseTimeoutSeconds {
			return fmt.Errorf("模型响应等待时间必须在 %d 到 %d 秒之间", appsettings.MinLLMResponseTimeoutSeconds, appsettings.MaxLLMResponseTimeoutSeconds)
		}
	}
	if request.ReviewAutomation.Profiles != nil {
		if len(*request.ReviewAutomation.Profiles) > 100 {
			return fmt.Errorf("too many review automation profiles")
		}
		for _, profile := range *request.ReviewAutomation.Profiles {
			if profile.Source != "wechat" && profile.Source != "xueqiu" && profile.Source != "taoguba" {
				return fmt.Errorf("unsupported review source: %s", profile.Source)
			}
			if strings.TrimSpace(profile.Name) == "" || len([]rune(profile.Name)) > 80 {
				return fmt.Errorf("review profile name is required and must be at most 80 characters")
			}
			if profile.SyncHour < 0 || profile.SyncHour > 23 {
				return fmt.Errorf("sync_hour must be between 0 and 23")
			}
			if baseURL := strings.TrimSpace(profile.BaseURL); baseURL != "" {
				parsed, err := url.Parse(baseURL)
				if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
					return fmt.Errorf("review profile base_url must be an http or https URL")
				}
			}
			if profile.Credential != nil && len(*profile.Credential) > 16<<10 {
				return fmt.Errorf("a review profile credential is too long")
			}
		}
	}
	secrets := []*string{
		request.LLM.APIKey,
		request.Credentials.TushareToken,
		request.Credentials.THSCookie,
		request.Credentials.XueqiuCookie,
		request.Credentials.EastMoneyCookie,
		request.Credentials.WeChatAPIToken,
	}
	for _, secret := range secrets {
		if secret != nil && len(*secret) > 16<<10 {
			return fmt.Errorf("a secret value is too long")
		}
	}
	allowedClear := map[string]bool{
		"llm_api_key": true, "tushare_token": true, "ths_cookie": true,
		"xueqiu_cookie": true, "eastmoney_cookie": true, "wechat_api_token": true,
	}
	for _, key := range request.ClearSecrets {
		if !allowedClear[key] {
			return fmt.Errorf("unsupported clear_secrets key: %s", key)
		}
	}
	return nil
}

func (s *Server) buildSettingsView(values appsettings.Values) settingsView {
	view := settingsView{}
	if s.hermesGateway != nil {
		view.Hermes = s.hermesGateway.Status()
	} else {
		view.Hermes.Message = "Hermes 配置服务不可用"
	}
	if !values.UpdatedAt.IsZero() {
		updatedAt := values.UpdatedAt
		view.UpdatedAt = &updatedAt
	}
	view.LLM.Provider = values.LLM.Provider
	if view.LLM.Provider == "" {
		view.LLM.Provider = "openai"
	}
	view.LLM.BaseURL = values.LLM.BaseURL
	if view.LLM.BaseURL == "" && values.LLM.Provider == "" {
		view.LLM.BaseURL = "https://api.openai.com/v1"
	}
	view.LLM.Model = values.LLM.Model
	view.LLM.APIMode = strings.TrimSpace(values.LLM.APIMode)
	if view.LLM.APIMode == "" {
		if view.LLM.Provider == "anthropic" {
			view.LLM.APIMode = "anthropic_messages"
		} else {
			view.LLM.APIMode = "chat_completions"
		}
	}
	view.LLM.ResponseTimeoutSeconds = appsettings.NormalizeLLMResponseTimeoutSeconds(values.LLM.ResponseTimeoutSeconds)
	view.LLM.APIKey.Configured = view.Hermes.APIKeyConfigured
	view.ActiveLLMProfileID = values.ActiveLLMProfileID
	view.LLMProfiles = make([]llmProfileView, 0, len(values.LLMProfiles))
	profileGateway, _ := s.hermesGateway.(hermes.ProfileGateway)
	for _, profile := range values.LLMProfiles {
		configured := profile.APIKeyConfigured
		if profile.ID == values.ActiveLLMProfileID {
			configured = view.Hermes.APIKeyConfigured
		} else if profileGateway != nil {
			if key, err := profileGateway.ModelAPIKeyForProfile(profile.ID); err == nil {
				configured = strings.TrimSpace(key) != ""
			}
		}
		view.LLMProfiles = append(view.LLMProfiles, llmProfileView{ID: profile.ID, Name: profile.Name, Provider: profile.Provider, BaseURL: profile.BaseURL, Model: profile.Model, APIMode: normalizeAPIMode(profile.APIMode, profile.Provider), APIKey: secretSettingStatus{Configured: configured}})
	}
	view.Credentials.TushareToken = secretStatus(values.Credentials.TushareToken)
	view.Credentials.THSCookie = secretStatus(values.Credentials.THSCookie)
	view.Credentials.XueqiuCookie = secretStatus(values.Credentials.XueqiuCookie)
	view.Credentials.EastMoneyCookie = secretStatus(values.Credentials.EastMoneyCookie)
	view.Credentials.WeChatAPIToken = secretStatus(values.Credentials.WeChatAPIToken)
	view.ReviewAutomation.Profiles = make([]reviewSourceProfileView, 0, len(values.ReviewAutomation.Profiles))
	for _, profile := range values.ReviewAutomation.Profiles {
		view.ReviewAutomation.Profiles = append(view.ReviewAutomation.Profiles, reviewSourceProfileView{ID: profile.ID, Source: profile.Source, Name: profile.Name, BaseURL: profile.BaseURL, Credential: secretStatus(profile.Credential), SyncHour: profile.SyncHour, AutoAnalyze: profile.AutoAnalyze, Enabled: profile.Enabled})
	}
	return view
}

func normalizeAPIMode(mode, provider string) string {
	mode = strings.TrimSpace(mode)
	if mode == "responses" {
		return "codex_responses"
	}
	if mode != "" {
		return mode
	}
	if strings.TrimSpace(provider) == "anthropic" {
		return "anthropic_messages"
	}
	return "chat_completions"
}

func findLLMProfile(profiles []appsettings.LLMProfile, id string) (appsettings.LLMProfile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return appsettings.LLMProfile{}, false
}

func hasLLMUpdate(request settingsUpdateRequest) bool {
	return request.LLM.Provider != nil || request.LLM.BaseURL != nil || request.LLM.Model != nil || request.LLM.APIMode != nil
}

func upsertActiveLLMProfile(values *appsettings.Values) {
	for i := range values.LLMProfiles {
		if values.LLMProfiles[i].ID == values.ActiveLLMProfileID {
			values.LLMProfiles[i].Provider = values.LLM.Provider
			values.LLMProfiles[i].BaseURL = values.LLM.BaseURL
			values.LLMProfiles[i].Model = values.LLM.Model
			values.LLMProfiles[i].APIMode = values.LLM.APIMode
			return
		}
	}
}

func secretStatus(secret string) secretSettingStatus {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return secretSettingStatus{}
	}
	runes := []rune(secret)
	if len(runes) <= 4 {
		return secretSettingStatus{Configured: true, Masked: "••••"}
	}
	return secretSettingStatus{Configured: true, Masked: "••••••" + string(runes[len(runes)-4:])}
}

func applyOptionalString(target *string, value *string) {
	if value != nil {
		*target = strings.TrimSpace(*value)
	}
}

func applyOptionalSecret(target *string, value *string) {
	if value != nil && strings.TrimSpace(*value) != "" {
		*target = strings.TrimSpace(*value)
	}
}
