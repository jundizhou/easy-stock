package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	appsettings "easy-stock/backend/internal/appsettings"
)

// Run the real bundled Hermes gateway against a local model endpoint.
func TestHermesNativePrompt(t *testing.T) {
	python := os.Getenv("EASY_STOCK_HERMES_TEST_PYTHON")
	if python == "" {
		t.Skip("set EASY_STOCK_HERMES_TEST_PYTHON to the bundled interpreter")
	}
	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			http.NotFound(w, r)
			return
		}
		select {
		case requests <- struct{}{}:
		default:
		}
		nativeReply(w, nativeMessage())
	}))
	defer server.Close()
	runtime := NewHermesRuntime(HermesConfig{Home: t.TempDir(), WorkDir: t.TempDir(), PythonPath: python})
	key := "fake-local-key"
	if err := runtime.SyncLLM(appsettings.LLM{Provider: "custom", BaseURL: server.URL + "/v1", Model: "gpt-5.5", APIMode: "responses"}, &key); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := runtime.PromptWithOptions(ctx, "Reply EASY_STOCK_CODEX_OK", PromptOptions{Sandbox: true, DisableTools: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "EASY_STOCK_CODEX_OK" {
		t.Fatalf("unexpected response: %+v", result)
	}
	select {
	case <-requests:
	default:
		t.Fatal("model endpoint was not called")
	}
}
