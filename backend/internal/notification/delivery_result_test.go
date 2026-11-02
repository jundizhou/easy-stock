package notification

import (
	"context"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
)

func TestDispatcherReportsDisabledAndFullChannels(t *testing.T) {
	started := make(chan struct{})
	d := NewDispatcher(func(ctx context.Context, _ string, _ appsettings.NotificationChannel, _ Message) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}, func() appsettings.Notifications {
		return appsettings.Notifications{Feishu: appsettings.NotificationChannel{Enabled: true, Webhook: "fixture"}}
	}, nil)
	defer d.Close()
	d.Publish(Event{Kind: "monitor_alert", Channels: []string{"feishu"}})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sender did not start")
	}
	for i := 0; i < 32; i++ {
		d.Publish(Event{Kind: "monitor_alert", Channels: []string{"feishu"}})
	}
	results := make(chan DeliveryResult, 2)
	d.Publish(Event{Kind: "monitor_alert", Channels: []string{"feishu", "dingtalk"}, OnDelivery: func(result DeliveryResult) { results <- result }})
	for _, want := range []string{"failed", "skipped"} {
		select {
		case result := <-results:
			if result.Status != want {
				t.Fatalf("result=%+v want=%s", result, want)
			}
		case <-time.After(time.Second):
			t.Fatal("missing final queue result")
		}
	}
}

func TestDispatcherCloseReportsUnsentQueuedMessages(t *testing.T) {
	started := make(chan struct{})
	d := NewDispatcher(func(ctx context.Context, _ string, _ appsettings.NotificationChannel, _ Message) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, func() appsettings.Notifications {
		return appsettings.Notifications{Feishu: appsettings.NotificationChannel{Enabled: true, Webhook: "fixture"}}
	}, nil)
	defer d.Close()
	d.Publish(Event{Kind: "monitor_alert", Channels: []string{"feishu"}})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sender did not start")
	}
	result := make(chan DeliveryResult, 1)
	d.Publish(Event{Kind: "monitor_alert", Channels: []string{"feishu"}, OnDelivery: func(value DeliveryResult) { result <- value }})
	d.Close()
	select {
	case value := <-result:
		if value.Status != "failed" {
			t.Fatalf("shutdown result=%+v", value)
		}
	case <-time.After(time.Second):
		t.Fatal("queued event left permanently pending")
	}
}
