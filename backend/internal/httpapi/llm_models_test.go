package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/hermes"
)

func TestSettingsLLMModelsUsesSavedKeyAndNormalizesResults(t *testing.T) {
	var gotPath, gotAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"z-model","owned_by":"vendor"},{"id":"a-model"},{"id":"a-model"},{"id":""}]}`)
	}))
	defer upstream.Close()

	store, _ := appsettings.Open("")
	gateway := &fakeHermesGateway{status: hermes.Status{Available: true, APIKeyConfigured: true}, modelAPIKey: "saved-private-key"}
	server := NewServer(Config{SettingsStore: store, HermesGateway: gateway})
	body := `{"provider":"custom","base_url":"` + upstream.URL + `/v1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/llm/models", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/models" || gotAuthorization != "Bearer saved-private-key" {
		t.Fatalf("upstream request path=%q authorization=%q", gotPath, gotAuthorization)
	}
	if strings.Index(rec.Body.String(), "a-model") > strings.Index(rec.Body.String(), "z-model") || strings.Count(rec.Body.String(), "a-model") != 1 {
		t.Fatalf("models were not sorted and deduplicated: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "saved-private-key") {
		t.Fatalf("response leaked API key: %s", rec.Body.String())
	}
}

func TestSettingsLLMModelsUsesUnsavedKeyAndAnthropicHeaders(t *testing.T) {
	var gotPath, gotKey, gotBearer, gotVersion string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotBearer = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("anthropic-version")
		_, _ = io.WriteString(w, `{"data":[{"id":"claude-test","display_name":"Claude Test"}]}`)
	}))
	defer upstream.Close()

	store, _ := appsettings.Open("")
	server := NewServer(Config{SettingsStore: store})
	body := `{"provider":"anthropic","base_url":"` + upstream.URL + `","api_key":"new-private-key"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/llm/models", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/models" || gotKey != "new-private-key" || gotBearer != "" || gotVersion == "" {
		t.Fatalf("anthropic request path=%q key=%q bearer=%q version=%q", gotPath, gotKey, gotBearer, gotVersion)
	}
	if strings.Contains(rec.Body.String(), "new-private-key") {
		t.Fatalf("response leaked API key: %s", rec.Body.String())
	}
}

func TestSettingsLLMModelsDoesNotExposeUpstreamErrorBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"secret-provider-debug-body"}`)
	}))
	defer upstream.Close()

	store, _ := appsettings.Open("")
	server := NewServer(Config{SettingsStore: store})
	body := `{"provider":"custom","base_url":"` + upstream.URL + `","api_key":"private-key"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/llm/models", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "secret-provider-debug-body") || strings.Contains(rec.Body.String(), "private-key") {
		t.Fatalf("unsafe upstream error: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSettingsLLMModelsValidatesURLAndRequiresOfficialProviderKey(t *testing.T) {
	store, _ := appsettings.Open("")
	server := NewServer(Config{SettingsStore: store})

	badURL := httptest.NewRequest(http.MethodPost, "/api/v1/settings/llm/models", strings.NewReader(`{"provider":"custom","base_url":"file:///tmp/models"}`))
	badURLRec := httptest.NewRecorder()
	server.ServeHTTP(badURLRec, badURL)
	if badURLRec.Code != http.StatusBadRequest {
		t.Fatalf("bad URL status=%d body=%s", badURLRec.Code, badURLRec.Body.String())
	}

	missingKey := httptest.NewRequest(http.MethodPost, "/api/v1/settings/llm/models", strings.NewReader(`{"provider":"openai","base_url":"https://api.openai.com/v1","api_key":""}`))
	missingKeyRec := httptest.NewRecorder()
	server.ServeHTTP(missingKeyRec, missingKey)
	if missingKeyRec.Code != http.StatusPreconditionFailed {
		t.Fatalf("missing key status=%d body=%s", missingKeyRec.Code, missingKeyRec.Body.String())
	}
}

func TestSupportedLLMProvidersBuildTheirOfficialModelsURLs(t *testing.T) {
	tests := []struct {
		provider string
		baseURL  string
		wantURL  string
	}{
		{provider: "moonshot", baseURL: "https://api.moonshot.cn/v1", wantURL: "https://api.moonshot.cn/v1/models"},
		{provider: "minimax", baseURL: "https://api.minimaxi.com/v1", wantURL: "https://api.minimaxi.com/v1/models"},
		{provider: "zhipu", baseURL: "https://open.bigmodel.cn/api/paas/v4", wantURL: "https://open.bigmodel.cn/api/paas/v4/models"},
		{provider: "siliconflow", baseURL: "https://api.siliconflow.cn/v1", wantURL: "https://api.siliconflow.cn/v1/models"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			if !supportedLLMProvider(tt.provider) {
				t.Fatalf("provider %q is not supported", tt.provider)
			}
			got, err := buildModelsURL(tt.provider, tt.baseURL)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.wantURL {
				t.Fatalf("models URL = %q, want %q", got, tt.wantURL)
			}
		})
	}
}
