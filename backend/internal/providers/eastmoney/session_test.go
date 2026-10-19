package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserSessionProviderReadsEnvAndCaches(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/v1/eastmoney/session" {
			t.Fatalf("path = %s, want /v1/eastmoney/session", r.URL.Path)
		}
		if got := r.Header.Get("X-A-Stock-Browser-Token"); got != "bridge-token" {
			t.Fatalf("token header = %q, want bridge-token", got)
		}
		if r.URL.Query().Get("refresh") == "1" {
			_, _ = w.Write([]byte(`{"ok":true,"data":{"cookie":"nid18=fresh","expires_at":"2099-01-01T00:00:00Z"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"cookie":"nid18=first","expires_at":""}}`))
	}))
	defer server.Close()

	t.Setenv(eastmoneySessionBridgeEnvURL, server.URL+"/")
	t.Setenv(eastmoneySessionBridgeEnvToken, "bridge-token")
	provider := NewBrowserSessionProviderFromEnv()
	if provider == nil {
		t.Fatal("configured bridge should produce a provider")
	}

	ctx := context.Background()
	cookie, err := provider.SessionCookie(ctx, false)
	if err != nil || cookie != "nid18=first" {
		t.Fatalf("cookie = %q err = %v, want nid18=first", cookie, err)
	}
	// 未过期时沿用缓存，不再请求桥接。
	if cookie, err = provider.SessionCookie(ctx, false); err != nil || cookie != "nid18=first" {
		t.Fatalf("cached cookie = %q err = %v", cookie, err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("bridge calls = %d, want 1", got)
	}
	// refresh 跳过缓存。
	if cookie, err = provider.SessionCookie(ctx, true); err != nil || cookie != "nid18=fresh" {
		t.Fatalf("refreshed cookie = %q err = %v, want nid18=fresh", cookie, err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("bridge calls = %d, want 2", got)
	}
}

func TestNewBrowserSessionProviderWithoutConfig(t *testing.T) {
	t.Setenv(eastmoneySessionBridgeEnvURL, "")
	t.Setenv(eastmoneySessionBridgeEnvToken, "")
	if provider := NewBrowserSessionProviderFromEnv(); provider != nil {
		t.Fatalf("unset bridge url should produce no provider, got %+v", provider)
	}
	if provider := NewBrowserSessionProvider("   ", "token"); provider != nil {
		t.Fatalf("blank bridge url should produce no provider, got %+v", provider)
	}
}

func TestBrowserSessionProviderSurfacesBridgeFailures(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "http error", status: http.StatusBadGateway, body: `{"ok":false,"error":"等待会话超时"}`, wantErr: "返回 502"},
		{name: "invalid json", status: http.StatusOK, body: `not-json`, wantErr: "格式无效"},
		{name: "empty cookie", status: http.StatusOK, body: `{"ok":true,"data":{"cookie":"   "}}`, wantErr: "未返回会话 Cookie"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(item.status)
				_, _ = w.Write([]byte(item.body))
			}))
			defer server.Close()

			provider := NewBrowserSessionProvider(server.URL, "")
			if provider == nil {
				t.Fatal("provider should be created")
			}
			_, err := provider.SessionCookie(context.Background(), false)
			if err == nil || !strings.Contains(err.Error(), item.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, item.wantErr)
			}
		})
	}
}

func TestBrowserSessionProviderHonoursEarlierExpiry(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"ok":true,"data":{"cookie":"nid18=soon","expires_at":"` + time.Now().Add(50*time.Millisecond).UTC().Format(time.RFC3339Nano) + `"}}`))
	}))
	defer server.Close()

	provider := NewBrowserSessionProvider(server.URL, "")
	ctx := context.Background()
	if _, err := provider.SessionCookie(ctx, false); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := provider.SessionCookie(ctx, false); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("bridge calls = %d, want 2 (expired session must be refetched)", got)
	}
}
