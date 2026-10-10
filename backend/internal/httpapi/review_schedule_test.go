package httpapi

import (
	"context"
	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/notification"
	"easy-stock/backend/internal/review"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type reviewTestCalendar struct{}

func (reviewTestCalendar) IsTradingDay(_ context.Context, t time.Time) (bool, error) {
	return t.Weekday() != time.Saturday && t.Weekday() != time.Sunday, nil
}
func TestReviewScheduleAPIAndNotificationRouting(t *testing.T) {
	s := NewServer(Config{ReviewCalendar: reviewTestCalendar{}, Token: "test-token"})
	defer s.Close()
	call := func(method, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/reviews/daily-summary/schedule", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if r := call("GET", "", ""); r.Code != 401 {
		t.Fatalf("auth bypass: %d", r.Code)
	}
	if r := call("GET", "", "test-token"); r.Code != 200 || !strings.Contains(r.Body.String(), "22:00") {
		t.Fatal("missing default", r.Body.String())
	}
	if r := call("PUT", `{"enabled":true,"time":"22:00","channels":["dingtalk"]}`, "test-token"); r.Code != 400 {
		t.Fatal("unconfigured robot allowed")
	}
	_, err := s.settingsStore.Update(func(v *appsettings.Values) error {
		v.Notifications.Dingtalk = appsettings.NotificationChannel{Enabled: true, Webhook: "fixture"}
		v.Notifications.Feishu = appsettings.NotificationChannel{Enabled: true, Webhook: "fixture"}
		v.Notifications.Events.TaskFailed = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := call("PUT", `{"enabled":true,"time":"21:45","channels":["dingtalk"]}`, "test-token")
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var payload struct {
		Data review.DailySchedule `json:"data"`
	}
	if err = json.Unmarshal(r.Body.Bytes(), &payload); err != nil || payload.Data.Time != "21:45" || payload.Data.NextRunAt.IsZero() {
		t.Fatal("schedule not saved", err)
	}
	sent := make(chan string, 4)
	s.notifications.Close()
	s.notifications = notification.NewDispatcher(func(_ context.Context, ch string, _ appsettings.NotificationChannel, m notification.Message) error {
		sent <- ch
		return nil
	}, func() appsettings.Notifications { return s.settingsStore.Snapshot().Notifications }, nil)
	for _, channels := range [][]string{nil, {}, {"dingtalk"}} {
		s.notifyDailySummary(review.DailySummaryJob{Status: "partial", NotificationChannels: channels}, &review.DailySummary{ExecutiveSummary: "完整报告"})
		if len(channels) > 0 {
			select {
			case ch := <-sent:
				if ch != "dingtalk" {
					t.Fatal("wrong channel")
				}
			case <-time.After(time.Second):
				t.Fatal("missing report")
			}
		}
	}
	s.notifications.Close()
	if len(sent) != 0 {
		t.Fatal("unchecked channel received report")
	}
	if r = call(http.MethodPut, `{"enabled":false,"time":"22:00","channels":[]}`, "test-token"); r.Code != 200 {
		t.Fatal("disable failed")
	}
}
