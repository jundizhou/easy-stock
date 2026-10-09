package notification

import (
	"context"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
)

func TestDispatcherRespectsChannelsAndEventFilters(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		event                               Event
		stock, portfolio, failures, enabled bool
		want                                int
	}{
		{"both channels", Event{Kind: "stock_research"}, true, true, true, true, 2},
		{"portfolio", Event{Kind: "portfolio_inspection"}, false, true, true, true, 2},
		{"channels disabled", Event{Kind: "stock_research"}, true, true, true, false, 0},
		{"stock disabled", Event{Kind: "stock_research"}, false, true, true, true, 0},
		{"failure disabled", Event{Kind: "stock_research", Failed: true}, true, true, false, true, 0},
		{"unknown event", Event{Kind: "other"}, true, true, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sent := make(chan string, 2)
			cfg := appsettings.Notifications{Events: appsettings.NotificationEvents{StockResearch: tc.stock, PortfolioInspection: tc.portfolio, TaskFailed: tc.failures}, Feishu: appsettings.NotificationChannel{Enabled: tc.enabled, Webhook: "feishu"}, Dingtalk: appsettings.NotificationChannel{Enabled: tc.enabled, Webhook: "dingtalk"}}
			d := NewDispatcher(func(_ context.Context, channel string, _ appsettings.NotificationChannel, _ Message) error {
				sent <- channel
				return nil
			}, func() appsettings.Notifications { return cfg }, nil)
			defer d.Close()
			d.Publish(tc.event)
			for i := 0; i < tc.want; i++ {
				select {
				case <-sent:
				case <-time.After(time.Second):
					t.Fatal("enabled event not delivered")
				}
			}
			d.Close()
			if len(sent) != 0 {
				t.Fatal("disabled event delivered")
			}
		})
	}
}

func TestDispatcherDoesNotBlockCompletionAndCancelsOnClose(t *testing.T) {
	started := make(chan struct{})
	d := NewDispatcher(func(ctx context.Context, _ string, _ appsettings.NotificationChannel, _ Message) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, func() appsettings.Notifications {
		return appsettings.Notifications{Feishu: appsettings.NotificationChannel{Enabled: true, Webhook: "configured"}, Events: appsettings.NotificationEvents{StockResearch: true}}
	}, nil)
	defer d.Close()
	finished := make(chan struct{})
	go func() { d.Publish(Event{Kind: "stock_research"}); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("notification blocks research completion")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sender did not start")
	}
	d.Close()
	d.Publish(Event{Kind: "stock_research"})
}

func TestQueuedNotificationsUseCurrentRobotCredentials(t *testing.T) {
	store, err := appsettings.Open("")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Update(func(values *appsettings.Values) error {
		values.Notifications.Feishu = appsettings.NotificationChannel{Enabled: true, Webhook: "original-robot"}
		return nil
	})
	started := make(chan struct{})
	release := make(chan struct{})
	sent := make(chan string, 1)
	d := NewDispatcher(func(ctx context.Context, _ string, cfg appsettings.NotificationChannel, message Message) error {
		if message.Text == "first" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
		} else {
			sent <- cfg.Webhook
		}
		return nil
	}, func() appsettings.Notifications { return store.Snapshot().Notifications }, nil)
	defer d.Close()
	d.Publish(Event{Kind: "stock_research", Message: Message{Text: "first"}})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first delivery not started")
	}
	d.Publish(Event{Kind: "stock_research", Message: Message{Text: "second"}})
	_, _ = store.Update(func(values *appsettings.Values) error {
		values.Notifications.Feishu.Webhook = "replacement-robot"
		return nil
	})
	close(release)
	select {
	case webhook := <-sent:
		if webhook != "replacement-robot" {
			t.Fatal("queued notification sent to stale robot")
		}
	case <-time.After(time.Second):
		t.Fatal("queued notification not delivered")
	}
}
