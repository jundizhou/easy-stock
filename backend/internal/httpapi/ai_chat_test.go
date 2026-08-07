package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/methodology"
	"github.com/gorilla/websocket"
)

func TestAIChatRelaysHermesJSONRPCOverWebSocket(t *testing.T) {
	gateway := &fakeHermesGateway{status: hermes.Status{Available: true, Configured: true, APIKeyConfigured: true}}
	httpServer := httptest.NewServer(NewServer(Config{HermesGateway: gateway}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/v1/ai/ws"
	connection, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	readFrame := func() map[string]any {
		_, payload, readErr := connection.ReadMessage()
		if readErr != nil {
			t.Fatalf("read websocket frame: %v", readErr)
		}
		var frame map[string]any
		if json.Unmarshal(payload, &frame) != nil {
			t.Fatalf("invalid frame: %s", payload)
		}
		return frame
	}
	if frame := readFrame(); frame["method"] != "event" {
		t.Fatalf("first frame = %+v, want gateway.ready event", frame)
	}
	if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": "1", "method": "session.create", "params": map[string]any{"client": "test"}}); err != nil {
		t.Fatal(err)
	}
	if frame := readFrame(); frame["id"] != "1" {
		t.Fatalf("session response = %+v", frame)
	}
	if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": "2", "method": "prompt.submit", "params": map[string]any{"session_id": "live-1", "text": "你好"}}); err != nil {
		t.Fatal(err)
	}
	_ = readFrame()
	complete := readFrame()
	params, _ := complete["params"].(map[string]any)
	if params["type"] != "message.complete" {
		t.Fatalf("complete frame = %+v", complete)
	}
}

func TestEnrichHermesPromptInjectsMatchingMasteryContext(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tree" {
			fmt.Fprint(w, `{"sha":"commit-ai","truncated":false,"tree":[{"path":"游资心法/92科比/深度研读报告.md","type":"blob"}]}`)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/raw/") {
			fmt.Fprint(w, "# 92科比深度研读报告\n\n## 首板\n首板交易需要结合赚钱效应和次日预期。")
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	library := methodology.NewLibrary(methodology.Config{
		CacheDir:        filepath.Join(t.TempDir(), "cache"),
		HermesHome:      filepath.Join(t.TempDir(), "hermes"),
		TreeURL:         upstream.URL + "/tree",
		RawBaseURL:      upstream.URL + "/raw/",
		RefreshInterval: time.Hour,
	})
	if _, err := library.Snapshot(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	server := &Server{masteryLibrary: library}
	payload := []byte(`{"jsonrpc":"2.0","id":"2","method":"prompt.submit","params":{"session_id":"s1","text":"92科比怎么看首板？"}}`)
	enriched := server.enrichHermesPrompt(context.Background(), payload)
	if !strings.Contains(string(enriched), "本地游资心法知识库") || !strings.Contains(string(enriched), "首板交易需要结合赚钱效应") || !strings.Contains(string(enriched), "92科比怎么看首板") {
		t.Fatalf("prompt was not enriched: %s", enriched)
	}
}

func TestAIChatRequiresAvailableHermesRuntime(t *testing.T) {
	server := NewServer(Config{HermesGateway: &fakeHermesGateway{status: hermes.Status{Message: "Hermes 运行时不可用"}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/ws", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "Hermes") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
