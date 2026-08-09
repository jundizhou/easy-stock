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
	Hermes hermes.Status `json:"hermes"`
	LLM    struct {
		Provider string              `json:"provider"`
		BaseURL  string              `json:"base_url"`
		Model    string              `json:"model"`
		APIMode  string              `json:"api_mode"`
		APIKey   secretSettingStatus `json:"api_key"`
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
	LLM struct {
		Provider *string `json:"provider"`
		BaseURL  *string `json:"base_url"`
		Model    *string `json:"model"`
		APIMode  *string `json:"api_mode"`
		APIKey   *string `json:"api_key"`
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
	if llmAPIKeyUpdate != nil && s.hermesGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "Hermes 配置服务不可用")
		return
	}
	values, err := s.settingsStore.Update(func(values *appsettings.Values) error {
		applyOptionalString(&values.LLM.Provider, request.LLM.Provider)
		applyOptionalString(&values.LLM.BaseURL, request.LLM.BaseURL)
		applyOptionalString(&values.LLM.Model, request.LLM.Model)
		applyOptionalString(&values.LLM.APIMode, request.LLM.APIMode)
		// LLM secrets are owned by Hermes' .env and must never be persisted in
		// the application's general settings file.
		values.LLM.APIKey = ""
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
		if err := s.hermesGateway.SyncLLM(values.LLM, llmAPIKeyUpdate); err != nil {
			writeError(w, http.StatusInternalServerError, "同步 Hermes 设置: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.buildSettingsView(values)})
}

func validateSettingsUpdate(request settingsUpdateRequest) error {
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
	view.LLM.APIKey.Configured = view.Hermes.APIKeyConfigured
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
