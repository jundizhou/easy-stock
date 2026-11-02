package httpapi

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/monitor"
	"easy-stock/backend/internal/notification"
)

type monitorTestSender struct {
	send func(string, notification.Message) error
}

func (f monitorTestSender) Send(_ context.Context, channel string, _ appsettings.NotificationChannel, message notification.Message) error {
	return f.send(channel, message)
}

func monitorNotificationServer(t *testing.T) *Server {
	t.Helper()
	s := NewServer(Config{})
	t.Cleanup(func() { _ = s.Close() })
	_, err := s.settingsStore.Update(func(values *appsettings.Values) error {
		values.Notifications.Feishu = appsettings.NotificationChannel{Enabled: true, Webhook: "https://open.feishu.cn/open-apis/bot/v2/hook/fixture"}
		values.Notifications.Dingtalk = appsettings.NotificationChannel{Enabled: true, Webhook: "https://oapi.dingtalk.com/robot/send?access_token=fixture"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.monitorPreferences.Update(context.Background(), func(prefs *monitor.Preferences) { prefs.NotifyEnabled = true })
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func waitMonitorDelivery(t *testing.T, s *Server, id int64, want string) monitor.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		items, err := s.monitorStore.ListEvents(context.Background(), monitor.EventFilter{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.ID == id && item.NotifyStatus == want {
				return item
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("event %d did not finish as %s", id, want)
	return monitor.Event{}
}

func TestMonitorDeliveryPersistsConfirmedResultsAndHonorsChannels(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		channels     []string
		fail         string
		want         int
	}{
		{"both", monitor.NotifySent, nil, "", 2},
		{"feishu only", monitor.NotifySent, []string{"feishu"}, "", 1},
		{"dingtalk only", monitor.NotifySent, []string{"dingtalk"}, "", 1},
		{"partial", monitor.NotifyPartial, nil, "dingtalk", 2},
		{"failed", monitor.NotifyFailed, nil, "all", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := monitorNotificationServer(t)
			var mu sync.Mutex
			sent := []string{}
			s.notificationSender = monitorTestSender{send: func(channel string, message notification.Message) error {
				mu.Lock()
				sent = append(sent, channel)
				mu.Unlock()
				if !strings.Contains(message.Text, "样本阈值") || !strings.Contains(message.Text, "北京时间") {
					t.Error("monitor message lost trigger context")
				}
				if tc.fail == channel || tc.fail == "all" {
					return errors.New("机器人拒绝请求")
				}
				return nil
			}}
			event := monitor.Event{RuleID: "fixture", RuleName: "阈值规则", Message: "样本阈值已触发", Severity: monitor.SeverityCritical, CreatedAt: time.Now(), NotifyStatus: monitor.NotifyQueued, NotifyChannels: tc.channels, Detail: map[string]any{"field": "last"}}
			id, err := s.monitorStore.AppendEvent(context.Background(), event)
			if err != nil {
				t.Fatal(err)
			}
			event.ID = id
			status, err := s.NotifyMonitorAlert(context.Background(), event)
			if err != nil || status != monitor.NotifyQueued {
				t.Fatalf("enqueue=%s err=%v", status, err)
			}
			stored := waitMonitorDelivery(t, s, id, tc.status)
			if stored.Detail["field"] != "last" || len(stored.Detail["notification_deliveries"].([]any)) != tc.want {
				t.Fatalf("lost delivery/evidence details: %+v", stored)
			}
			if (tc.status == monitor.NotifySent || tc.status == monitor.NotifyPartial) != (stored.NotifiedAt != nil) {
				t.Fatalf("incorrect notified time: %+v", stored)
			}
			// A fast webhook response must not be overwritten by late queue bookkeeping.
			if err := s.monitorStore.UpdateEventNotify(context.Background(), id, monitor.NotifyQueued, "", time.Now()); err != nil {
				t.Fatal(err)
			}
			waitMonitorDelivery(t, s, id, tc.status)
			mu.Lock()
			count := len(sent)
			mu.Unlock()
			if count != tc.want {
				t.Fatalf("sent to %d channels, want %d", count, tc.want)
			}
		})
	}
}

func TestMonitorDigestDeliveryUpdatesEverySourceEvent(t *testing.T) {
	s := monitorNotificationServer(t)
	s.notificationSender = monitorTestSender{send: func(string, notification.Message) error { return nil }}
	ids := []int64{}
	for i := 0; i < 2; i++ {
		id, err := s.monitorStore.AppendEvent(context.Background(), monitor.Event{RuleID: "digest-source", NotifyStatus: monitor.NotifyPending})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	_, err := s.NotifyMonitorAlert(context.Background(), monitor.Event{RuleType: "digest", Message: "两条汇总", NotificationEventIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		waitMonitorDelivery(t, s, id, monitor.NotifySent)
		_ = s.monitorStore.UpdateEventNotify(context.Background(), id, monitor.NotifyMerged, "", time.Now())
		waitMonitorDelivery(t, s, id, monitor.NotifySent)
	}
}

func TestMonitorNoEnabledRobotIsSkipped(t *testing.T) {
	s := monitorNotificationServer(t)
	_, _ = s.settingsStore.Update(func(values *appsettings.Values) error {
		values.Notifications.Feishu.Enabled = false
		values.Notifications.Dingtalk.Enabled = false
		return nil
	})
	s.notificationSender = monitorTestSender{send: func(string, notification.Message) error {
		t.Error("disabled robot received a notification")
		return nil
	}}
	status, err := s.NotifyMonitorAlert(context.Background(), monitor.Event{Message: "fixture"})
	if err != nil || status != monitor.NotifySkipped {
		t.Fatalf("status=%s err=%v", status, err)
	}
}
