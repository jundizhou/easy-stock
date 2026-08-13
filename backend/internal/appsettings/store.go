package appsettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type LLM struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	APIMode  string `json:"api_mode"`
	APIKey   string `json:"api_key"`
}

// LLMProfile is a named model connection. API keys are deliberately not
// persisted in the general settings file; Hermes owns the encrypted-at-rest
// (local .env) secret for each profile.
type LLMProfile struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Provider         string `json:"provider"`
	BaseURL          string `json:"base_url"`
	Model            string `json:"model"`
	APIMode          string `json:"api_mode"`
	APIKeyConfigured bool   `json:"api_key_configured,omitempty"`
}

type Credentials struct {
	TushareToken    string `json:"tushare_token"`
	THSCookie       string `json:"ths_cookie"`
	XueqiuCookie    string `json:"xueqiu_cookie"`
	EastMoneyCookie string `json:"eastmoney_cookie"`
	WeChatAPIToken  string `json:"wechat_api_token"`
}

type ReviewAutomation struct {
	Profiles     []ReviewSourceProfile `json:"profiles"`
	WeChatAPIURL string                `json:"wechat_api_url,omitempty"`
	SyncHour     int                   `json:"sync_hour,omitempty"`
	AutoAnalyze  bool                  `json:"auto_analyze,omitempty"`
}

type ReviewSourceProfile struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	Credential  string `json:"credential"`
	SyncHour    int    `json:"sync_hour"`
	AutoAnalyze bool   `json:"auto_analyze"`
	Enabled     bool   `json:"enabled"`
}

type Values struct {
	LLM                LLM              `json:"llm"`
	LLMProfiles        []LLMProfile     `json:"llm_profiles,omitempty"`
	ActiveLLMProfileID string           `json:"active_llm_profile_id,omitempty"`
	Credentials        Credentials      `json:"credentials"`
	ReviewAutomation   ReviewAutomation `json:"review_automation"`
	UpdatedAt          time.Time        `json:"updated_at,omitempty"`
}

type Store struct {
	mu     sync.RWMutex
	path   string
	values Values
}

func Open(path string) (*Store, error) {
	store := &Store{path: path, values: defaultValues()}
	if path == "" {
		store.normalizeLLMProfiles()
		return store, nil
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	store.normalizeLLMProfiles()
	return store, nil
}

func (s *Store) Snapshot() Values {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.values
}

func (s *Store) Update(update func(*Values) error) (Values, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.values
	if err := update(&next); err != nil {
		return Values{}, err
	}
	next.UpdatedAt = time.Now()
	if err := s.persist(next); err != nil {
		return Values{}, err
	}
	s.values = next
	return next, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &s.values); err != nil {
		return fmt.Errorf("decode settings: %w", err)
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) == nil {
		if _, exists := raw["review_automation"]; !exists {
			s.values.ReviewAutomation = defaultValues().ReviewAutomation
		}
	}
	s.normalizeReviewProfiles()
	s.normalizeLLMProfiles()
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("secure settings permissions: %w", err)
	}
	return nil
}

// normalizeLLMProfiles migrates the old single llm object to the profile
// format without changing its connection details.
func (s *Store) normalizeLLMProfiles() {
	if len(s.values.LLMProfiles) == 0 {
		current := s.values.LLM
		active := LLMProfile{ID: "llm-default", Name: profileName(current), Provider: current.Provider, BaseURL: current.BaseURL, Model: current.Model, APIMode: current.APIMode}
		s.values.LLMProfiles = []LLMProfile{active}
	}
	if s.values.ActiveLLMProfileID == "" {
		s.values.ActiveLLMProfileID = profileIDForLLM(s.values.LLMProfiles, s.values.LLM)
	}
	if active, ok := findLLMProfile(s.values.LLMProfiles, s.values.ActiveLLMProfileID); ok {
		apiKey := s.values.LLM.APIKey
		s.values.LLM = llmFromProfile(active)
		s.values.LLM.APIKey = apiKey
	}
}

func profileName(cfg LLM) string {
	if strings.TrimSpace(cfg.Model) != "" {
		return strings.TrimSpace(cfg.Model)
	}
	if strings.TrimSpace(cfg.Provider) != "" {
		return strings.TrimSpace(cfg.Provider)
	}
	return "默认模型"
}

func profileIDForLLM(profiles []LLMProfile, cfg LLM) string {
	for _, profile := range profiles {
		if profile.Provider == cfg.Provider && profile.BaseURL == cfg.BaseURL && profile.Model == cfg.Model && profile.APIMode == cfg.APIMode {
			return profile.ID
		}
	}
	if len(profiles) > 0 {
		return profiles[len(profiles)-1].ID
	}
	return ""
}

func findLLMProfile(profiles []LLMProfile, id string) (LLMProfile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return LLMProfile{}, false
}

func llmFromProfile(profile LLMProfile) LLM {
	return LLM{Provider: profile.Provider, BaseURL: profile.BaseURL, Model: profile.Model, APIMode: profile.APIMode}
}

func defaultValues() Values {
	return Values{ReviewAutomation: ReviewAutomation{Profiles: []ReviewSourceProfile{
		{ID: "wechat-default", Source: "wechat", Name: "微信公众号默认配置", SyncHour: 7, AutoAnalyze: true, Enabled: true},
		{ID: "xueqiu-default", Source: "xueqiu", Name: "雪球默认配置", BaseURL: "https://xueqiu.com", SyncHour: 7, AutoAnalyze: true, Enabled: true},
		{ID: "taoguba-default", Source: "taoguba", Name: "淘股吧默认配置", BaseURL: "https://www.tgb.cn", SyncHour: 7, AutoAnalyze: true, Enabled: true},
	}}}
}

func (s *Store) normalizeReviewProfiles() {
	if len(s.values.ReviewAutomation.Profiles) == 0 {
		defaults := defaultValues().ReviewAutomation.Profiles
		if strings.TrimSpace(s.values.ReviewAutomation.WeChatAPIURL) != "" {
			defaults[0].BaseURL = s.values.ReviewAutomation.WeChatAPIURL
			defaults[0].Credential = s.values.Credentials.WeChatAPIToken
		}
		if strings.TrimSpace(s.values.Credentials.XueqiuCookie) != "" {
			defaults[1].Credential = s.values.Credentials.XueqiuCookie
		}
		if s.values.ReviewAutomation.SyncHour >= 0 {
			for index := range defaults {
				defaults[index].SyncHour = s.values.ReviewAutomation.SyncHour
			}
		}
		for index := range defaults {
			defaults[index].AutoAnalyze = s.values.ReviewAutomation.AutoAnalyze || s.values.UpdatedAt.IsZero()
		}
		s.values.ReviewAutomation.Profiles = defaults
	}
}

func (s *Store) persist(values Values) error {
	if s.path == "" {
		return nil
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create settings temp file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure settings temp file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write settings: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync settings: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close settings: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace settings: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("secure settings file: %w", err)
	}
	return nil
}
