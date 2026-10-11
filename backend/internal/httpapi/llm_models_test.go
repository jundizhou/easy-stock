package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
)

// Shape returned by https://open.bigmodel.cn/api/v1/models. Its inference
// endpoint works even though the directory is not an OpenAI data[].id list.
const glmResponsesCatalog = `{"models":[
	{"slug":"glm-5.3","display_name":"glm-5.3","supported_in_api":true,"context_window":1048576,"default_reasoning_level":"max","supported_reasoning_levels":[{"effort":"low"},{"effort":"high"},{"effort":"max"}]},
	{"slug":"glm-5.3-flash","display_name":"glm-5.3-flash","supported_in_api":true,"input_modalities":["text"],"default_reasoning_level":"max","supported_reasoning_levels":[{"effort":"low"},{"effort":"high"},{"effort":"max"}]},
	{"slug":"glm-5-turbo","display_name":"glm-5-turbo","supported_in_api":true,"supported_reasoning_levels":[]}
]}`

func TestSettingsLLMModelsSupportsGLMResponsesCatalogAndDiscoveryModes(t *testing.T) {
	for _, mode := range []string{"codex_responses", "responses", "auto"} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" || r.Header.Get("Authorization") != "Bearer glm-private-key" {
					t.Error("discovery did not use the configured endpoint and saved credential")
				}
				_, _ = io.WriteString(w, glmResponsesCatalog)
			}))
			defer upstream.Close()
			store, _ := appsettings.Open("")
			before := store.Snapshot()
			gateway := &fakeAgentGateway{modelAPIKey: "glm-private-key"}
			server := NewServer(Config{SettingsStore: store, AgentGateway: gateway})
			body := `{"provider":"zhipu","base_url":"` + upstream.URL + `/api/v1","api_mode":"` + mode + `"}`
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/settings/llm/models", strings.NewReader(body)))
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var result struct {
				Data llmModelsResult `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, model := range result.Data.Models {
				ids = append(ids, model.ID)
				if mode != "auto" && model.ID != "glm-5-turbo" {
					if model.Reasoning.Source != "model_api" || model.Reasoning.Default != "max" || !model.Reasoning.Allows("low") || !model.Reasoning.Allows("high") || model.Reasoning.Allows("medium") {
						t.Fatalf("Responses reasoning metadata lost: %+v", model.Reasoning)
					}
				}
			}
			if !reflect.DeepEqual(ids, []string{"glm-5-turbo", "glm-5.3", "glm-5.3-flash"}) {
				t.Fatalf("unexpected catalog: %v", ids)
			}
			if requests != 1 || result.Data.SourceURL != upstream.URL+"/api/v1/models" || !reflect.DeepEqual(before, store.Snapshot()) {
				t.Fatal("discovery must not probe inference, rewrite the URL, or change saved settings")
			}
			if strings.Contains(response.Body.String(), "glm-private-key") || strings.Contains(response.Body.String(), "context_window") {
				t.Fatal("response exposed credentials or internal catalog metadata")
			}
		})
	}
}

func TestDecodeNativeModelCatalogNormalizesIDsAndPreservesMetadata(t *testing.T) {
	models, err := decodeModelList([]byte(`{"models":[
		{"slug":" z-model ","display_name":" Z model ","context_window":1048576},
		{"slug":"a-model","supported_in_api":true},
		{"slug":"a-model"},{"slug":""},{"id":"not-a-slug"},
		{"slug":"unavailable","supported_in_api":false}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "a-model" || models[1].ID != "z-model" || models[1].DisplayName != "Z model" {
		t.Fatalf("unexpected models: %+v", models)
	}
	if !strings.Contains(string(models[1].Metadata), `"context_window":1048576`) {
		t.Fatal("native metadata lost before runtime capability resolution")
	}
}

func TestSettingsLLMModelsRejectsHTTP200ProviderErrorsWithoutLeakingBody(t *testing.T) {
	for _, body := range []string{
		`{"code":1001,"msg":"private-key debug","success":false}`,
		`{"error":{"code":"invalid_api_key","message":"private-key debug"}}`,
		`{"success":false,"models":[{"slug":"must-not-be-used"}]}`,
	} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		store, _ := appsettings.Open("")
		server := NewServer(Config{SettingsStore: store})
		request := `{"provider":"zhipu","base_url":"` + upstream.URL + `/api/v1","api_key":"private-key","api_mode":"codex_responses"}`
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/settings/llm/models", strings.NewReader(request)))
		upstream.Close()
		if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "模型列表接口返回错误") {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		for _, secret := range []string{"private-key", "debug", "must-not-be-used"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatal("provider error body leaked into the response")
			}
		}
	}
}

func TestDecodeModelListEmptyDirectorySuggestsManualModel(t *testing.T) {
	for _, body := range []string{`{"models":[]}`, `{"data":[]}`, `{}`} {
		if _, err := decodeModelList([]byte(body)); err == nil || !strings.Contains(err.Error(), "手动输入模型 ID 并测试连接") {
			t.Fatalf("empty directory was treated as a model invocation failure: %v", err)
		}
	}
}

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
	gateway := &fakeAgentGateway{status: agent.Status{Available: true, APIKeyConfigured: true}, modelAPIKey: "saved-private-key"}
	server := NewServer(Config{SettingsStore: store, AgentGateway: gateway})
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
	// 用解析后的模型 ID 列表断言「去重 + 排序」，而不是对整段 JSON 做子串计数：
	// 每个模型条目还带有 effective_model 等元数据字段，子串计数会被重复命中。
	var catalog struct {
		Data llmModelsResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	ids := make([]string, 0, len(catalog.Data.Models))
	for _, model := range catalog.Data.Models {
		ids = append(ids, model.ID)
	}
	if !reflect.DeepEqual(ids, []string{"a-model", "z-model"}) {
		t.Fatalf("models were not sorted and deduplicated: ids=%v body=%s", ids, rec.Body.String())
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

func TestModelSyncFeedsReasoningControlsAndRejectsUnsupportedValues(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"claude-discovered","capabilities":{"effort":{"supported":true,"low":{"supported":true},"high":{"supported":true},"max":{"supported":false}},"thinking":{"supported":true,"types":{"adaptive":{"supported":true}}}}}]}`)
	}))
	defer upstream.Close()
	store, _ := appsettings.Open("")
	runtime := agent.NewHermesRuntime(agent.HermesConfig{Home: t.TempDir()})
	server := NewServer(Config{SettingsStore: store, AgentGateway: runtime})
	if err := runtime.SyncLLM(appsettings.LLM{Provider: "custom", BaseURL: upstream.URL, Model: "claude-discovered", APIMode: "anthropic_messages"}, nil); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/settings/llm/models", strings.NewReader(`{"provider":"custom","base_url":"`+upstream.URL+`","api_mode":"anthropic_messages"}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"source":"model_api"`) {
		t.Fatalf("sync: %d %s", response.Code, response.Body.String())
	}
	settings, err := runtime.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ReasoningEffort != "high" || settings.Reasoning.Allows("max") {
		t.Fatalf("capabilities not used: %+v", settings)
	}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"reasoning_effort":"max"}`, http.StatusBadRequest},
		{`{"reasoning_effort":"low","reasoning_context":"old-model"}`, http.StatusConflict},
		{`{"reasoning_effort":"low","reasoning_context":"` + settings.ReasoningContext + `"}`, http.StatusOK},
	} {
		response = httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/settings/agent", strings.NewReader(tc.body)))
		if response.Code != tc.status {
			t.Fatalf("body=%s got %d: %s", tc.body, response.Code, response.Body.String())
		}
	}
}
