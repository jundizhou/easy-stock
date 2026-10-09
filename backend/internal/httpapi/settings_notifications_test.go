package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/notification"
)

type fakeNotificationSender struct {
	calls   int
	channel string
	config  appsettings.NotificationChannel
}

func (f *fakeNotificationSender) Send(_ context.Context, channel string, cfg appsettings.NotificationChannel, _ notification.Message) error {
	f.calls++
	f.channel, f.config = channel, cfg
	return nil
}

func notificationRequest(s *Server, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestNotificationSettingsPersistRedactPreserveAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store, err := appsettings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{SettingsStore: store})
	defer s.Close()
	input := `{"feishu":{"enabled":true,"webhook":"https://open.feishu.cn/open-apis/bot/v2/hook/private-bot-id","secret":"private-signing-secret","keyword":"股票"},"dingtalk":{"enabled":true,"webhook":"https://oapi.dingtalk.com/robot/send?access_token=private-token"}}`
	rec := notificationRequest(s, http.MethodPut, "/api/v1/settings/notifications", input)
	if rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	for _, endpoint := range []string{"/api/v1/settings/notifications", "/api/v1/settings"} {
		rec := notificationRequest(s, http.MethodGet, endpoint, "")
		if rec.Code != 200 || strings.Contains(rec.Body.String(), "private-") || strings.Contains(rec.Body.String(), "/hook/") {
			t.Fatalf("leaked webhook: %s", rec.Body.String())
		}
	}
	rec = notificationRequest(s, http.MethodPut, "/api/v1/settings/notifications", `{"feishu":{"webhook":"","secret":"","keyword":"研究"}}`)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	reopened, err := appsettings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := reopened.Snapshot().Notifications
	if cfg.Feishu.Secret != "private-signing-secret" || !strings.Contains(cfg.Feishu.Webhook, "private-bot-id") || cfg.Feishu.Keyword != "研究" || !cfg.Events.StockResearch {
		t.Fatalf("saved configuration not preserved: %+v", buildNotificationsView(cfg))
	}
	rec = notificationRequest(s, http.MethodPut, "/api/v1/settings/notifications", `{"feishu":{"enabled":false,"clear_webhook":true,"clear_secret":true}}`)
	if rec.Code != 200 || store.Snapshot().Notifications.Feishu.Webhook != "" || store.Snapshot().Notifications.Feishu.Secret != "" || !store.Snapshot().Notifications.Dingtalk.Enabled {
		t.Fatalf("clear failed: %s", rec.Body.String())
	}
}

func TestNotificationSettingsValidateAtomicallyAndTestUnsavedDraft(t *testing.T) {
	s := NewServer(Config{})
	defer s.Close()
	for _, body := range []string{
		`{"feishu":{"enabled":true}}`,
		`{"feishu":{"webhook":"https://127.0.0.1/private-token"}}`,
		`{"feishu":{"keyword":"valid"},"dingtalk":{"enabled":true}}`,
		`{"feishu":{"keyword":"line\nbreak"}}`,
		`{"unexpected":"private-token"}`,
		`{} {}`,
	} {
		rec := notificationRequest(s, http.MethodPut, "/api/v1/settings/notifications", body)
		if rec.Code != 400 || strings.Contains(rec.Body.String(), "private-token") || s.settingsStore.Snapshot().Notifications.Feishu.Keyword != "" {
			t.Fatalf("invalid update applied or leaked: %s", rec.Body.String())
		}
	}
	fake := &fakeNotificationSender{}
	s.notificationSender = fake
	rec := notificationRequest(s, http.MethodPost, "/api/v1/settings/notifications/test", `{"channel":"dingtalk","config":{"webhook":"https://oapi.dingtalk.com/robot/send?access_token=private-token","secret":"SEC-private"}}`)
	if rec.Code != 200 || fake.calls != 1 || fake.config.Secret != "SEC-private" || s.settingsStore.Snapshot().Notifications.Dingtalk.Webhook != "" {
		t.Fatalf("draft test failed or persisted: %s", rec.Body.String())
	}
	rec = notificationRequest(s, http.MethodPost, "/api/v1/settings/notifications/test", `{"channel":"other"}`)
	if rec.Code != 400 || fake.calls != 1 {
		t.Fatalf("invalid channel sent: %s", rec.Body.String())
	}
	rec = notificationRequest(s, http.MethodPut, "/api/v1/settings/notifications", `{"events":{"task_failed":false}}`)
	events := s.settingsStore.Snapshot().Notifications.Events
	if rec.Code != 200 || events.TaskFailed || !events.StockResearch || !events.PortfolioInspection {
		t.Fatalf("partial event update discarded other events: %s", rec.Body.String())
	}
}

func TestNotificationEndpointsRequireLocalAPIToken(t *testing.T) {
	s := NewServer(Config{Token: "local-api-token"})
	defer s.Close()
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/settings/notifications"},
		{http.MethodPut, "/api/v1/settings/notifications"},
		{http.MethodPost, "/api/v1/settings/notifications/test"},
	} {
		rec := notificationRequest(s, endpoint.method, endpoint.path, `{}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("notification endpoint bypassed token auth: %s %d", endpoint.path, rec.Code)
		}
	}
}
