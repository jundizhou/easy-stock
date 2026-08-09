package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/hermes"
)

func TestSettingsAPIStoresSecretsWithoutReturningThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store, err := appsettings.Open(path)
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}
	gateway := &fakeHermesGateway{status: hermes.Status{Available: true}}
	server := NewServer(Config{SettingsStore: store, HermesGateway: gateway})
	body := `{
		"llm":{"provider":"deepseek","base_url":"https://api.deepseek.com","model":"deepseek-chat","api_mode":"chat_completions","api_key":"sk-private-12345678"},
		"credentials":{"tushare_token":"tushare-private-87654321"}
	}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-private") || strings.Contains(rec.Body.String(), "tushare-private") {
		t.Fatalf("settings response leaked a secret: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"api_key":{"configured":true}`) || !strings.Contains(rec.Body.String(), "••••••4321") {
		t.Fatalf("settings response missing masked status: %s", rec.Body.String())
	}

	reopened, err := appsettings.Open(path)
	if err != nil {
		t.Fatalf("reopen settings: %v", err)
	}
	values := reopened.Snapshot()
	if values.LLM.APIKey != "" || values.LLM.APIMode != "chat_completions" || values.Credentials.TushareToken != "tushare-private-87654321" {
		t.Fatalf("stored settings mismatch: %+v", values)
	}
	if gateway.lastKey == nil || *gateway.lastKey != "sk-private-12345678" {
		t.Fatalf("Hermes did not receive model secret: %+v", gateway.lastKey)
	}
	storedFile, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(storedFile), "sk-private") {
		t.Fatalf("general settings file contains model secret: err=%v data=%s", err, storedFile)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	getRec := httptest.NewRecorder()
	server.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK || strings.Contains(getRec.Body.String(), "sk-private") {
		t.Fatalf("GET settings leaked or failed: status=%d body=%s", getRec.Code, getRec.Body.String())
	}
}

func TestSettingsLLMConnectionUsesSavedOpenAICompatibleConfig(t *testing.T) {
	store, err := appsettings.Open("")
	if err != nil {
		t.Fatalf("open memory settings: %v", err)
	}
	_, _ = store.Update(func(values *appsettings.Values) error {
		values.LLM = appsettings.LLM{
			Provider: "custom",
			BaseURL:  "https://model.example/v1",
			Model:    "gpt-test",
			APIMode:  "chat_completions",
		}
		return nil
	})
	gateway := &fakeHermesGateway{
		status:       hermes.Status{Available: true, Configured: true, APIKeyConfigured: true, Version: "0.18.2"},
		promptResult: hermes.PromptResult{Content: llmProbeMarker},
	}
	server := NewServer(Config{SettingsStore: store, HermesGateway: gateway})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/llm/test", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) || !strings.Contains(rec.Body.String(), llmProbeMarker) || !strings.Contains(rec.Body.String(), `"runtime":"hermes"`) {
		t.Fatalf("connection response missing success details: %s", rec.Body.String())
	}
}

func TestSettingsLLMConnectionHidesProviderErrorBody(t *testing.T) {
	store, _ := appsettings.Open("")
	_, _ = store.Update(func(values *appsettings.Values) error {
		values.LLM = appsettings.LLM{Provider: "custom", BaseURL: "https://model.example", Model: "gpt-test"}
		return nil
	})
	gateway := &fakeHermesGateway{status: hermes.Status{Available: true, Configured: true}, promptErr: errors.New("provider authentication failed")}
	server := NewServer(Config{SettingsStore: store, HermesGateway: gateway})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/llm/test", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-provider-detail") || !strings.Contains(rec.Body.String(), "Hermes") {
		t.Fatalf("provider error was not safely categorized: %s", rec.Body.String())
	}
}

func TestSettingsAPIValidatesAndClearsSecrets(t *testing.T) {
	store, err := appsettings.Open("")
	if err != nil {
		t.Fatalf("open memory settings: %v", err)
	}
	_, _ = store.Update(func(values *appsettings.Values) error {
		values.LLM.APIKey = "secret"
		return nil
	})
	gateway := &fakeHermesGateway{status: hermes.Status{Available: true, Configured: true, APIKeyConfigured: true}}
	server := NewServer(Config{SettingsStore: store, HermesGateway: gateway})

	badReq := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"llm":{"base_url":"file:///tmp/key"}}`))
	badRec := httptest.NewRecorder()
	server.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid base URL status = %d, want 400", badRec.Code)
	}

	clearReq := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"clear_secrets":["llm_api_key"]}`))
	clearRec := httptest.NewRecorder()
	server.ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear status = %d, want 200; body=%s", clearRec.Code, clearRec.Body.String())
	}
	if store.Snapshot().LLM.APIKey != "" {
		t.Fatal("llm api key was not cleared")
	}
	if gateway.lastKey == nil || *gateway.lastKey != "" {
		t.Fatal("Hermes model api key was not cleared")
	}
}

func TestSettingsAPISupportsMultipleReviewProfilesAndMasksCredentials(t *testing.T) {
	store, err := appsettings.Open("")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{SettingsStore: store})
	body := `{"review_automation":{"profiles":[
		{"id":"wx-1","source":"wechat","name":"微信实例一","base_url":"http://127.0.0.1:5000","credential":"wx-secret-1234","sync_hour":7,"auto_analyze":true,"enabled":true},
		{"id":"wx-2","source":"wechat","name":"微信实例二","base_url":"http://127.0.0.1:5001","sync_hour":9,"auto_analyze":false,"enabled":true},
		{"id":"xq-1","source":"xueqiu","name":"雪球账号一","base_url":"https://xueqiu.com","credential":"xq-cookie-5678","sync_hour":8,"auto_analyze":true,"enabled":true}
	]}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "wx-secret") || strings.Contains(rec.Body.String(), "xq-cookie") {
		t.Fatalf("profile secret leaked: %s", rec.Body.String())
	}
	values := store.Snapshot()
	if len(values.ReviewAutomation.Profiles) != 3 || values.ReviewAutomation.Profiles[0].Credential != "wx-secret-1234" {
		t.Fatalf("profiles=%+v", values.ReviewAutomation.Profiles)
	}
	if values.ReviewAutomation.Profiles[2].Credential != "" {
		t.Fatalf("legacy xueqiu cookie should be discarded: %+v", values.ReviewAutomation.Profiles[2])
	}
	update := `{"review_automation":{"profiles":[{"id":"wx-1","source":"wechat","name":"微信实例一改名","base_url":"http://127.0.0.1:5000","sync_hour":10,"auto_analyze":true,"enabled":true}]}}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(update))
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || store.Snapshot().ReviewAutomation.Profiles[0].Credential != "wx-secret-1234" {
		t.Fatalf("credential was not preserved: status=%d values=%+v", rec.Code, store.Snapshot().ReviewAutomation.Profiles)
	}
}

func TestSettingsAPIAcceptsAdditionalLLMProviders(t *testing.T) {
	store, err := appsettings.Open("")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{SettingsStore: store, HermesGateway: &fakeHermesGateway{status: hermes.Status{Available: true}}})
	providers := []struct {
		provider string
		baseURL  string
		model    string
	}{
		{provider: "moonshot", baseURL: "https://api.moonshot.cn/v1", model: "moonshot-v1-8k"},
		{provider: "minimax", baseURL: "https://api.minimaxi.com/v1", model: "MiniMax-Text-01"},
		{provider: "zhipu", baseURL: "https://open.bigmodel.cn/api/paas/v4", model: "glm-4-plus"},
		{provider: "siliconflow", baseURL: "https://api.siliconflow.cn/v1", model: "vendor/model"},
	}
	for _, tt := range providers {
		t.Run(tt.provider, func(t *testing.T) {
			body := fmt.Sprintf(`{"llm":{"provider":%q,"base_url":%q,"model":%q,"api_mode":"chat_completions"}}`, tt.provider, tt.baseURL, tt.model)
			req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if got := store.Snapshot().LLM.Provider; got != tt.provider {
				t.Fatalf("stored provider=%q, want %q", got, tt.provider)
			}
		})
	}
}
