package notification

import (
	"context"
	"log"
	"sync"

	"easy-stock/backend/internal/appsettings"
)

type SendFunc func(context.Context, string, appsettings.NotificationChannel, Message) error

type Event struct {
	Kind    string
	Failed  bool
	Message Message
}

type delivery struct {
	channel string
	event   Event
}

// Dispatcher keeps webhook latency out of task completion and uses a bounded
// queue. The worker starts only when an enabled channel receives an event.
type Dispatcher struct {
	mu       sync.Mutex
	closed   bool
	started  bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	queue    chan delivery
	send     SendFunc
	settings func() appsettings.Notifications
	logger   *log.Logger
}

func NewDispatcher(send SendFunc, settings func() appsettings.Notifications, logger *log.Logger) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &Dispatcher{ctx: ctx, cancel: cancel, queue: make(chan delivery, 32), send: send, settings: settings, logger: logger}
}

func (d *Dispatcher) Publish(event Event) {
	cfg := d.settings()
	if !eventEnabled(cfg.Events, event) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	for _, channel := range []struct {
		name   string
		config appsettings.NotificationChannel
	}{{"feishu", cfg.Feishu}, {"dingtalk", cfg.Dingtalk}} {
		if !channel.config.Enabled || channel.config.Webhook == "" {
			continue
		}
		if !d.started {
			d.started = true
			d.wg.Add(1)
			go d.run()
		}
		select {
		case d.queue <- delivery{channel.name, event}:
		default:
			if d.logger != nil {
				d.logger.Printf("level=warn event=notification_queue_full channel=%s", channel.name)
			}
		}
	}
}

func (d *Dispatcher) run() {
	defer d.wg.Done()
	for {
		select {
		case <-d.ctx.Done():
			return
		case item := <-d.queue:
			// Resolve credentials at send time so disabling a channel or changing
			// its robot also applies to notifications still waiting in the queue.
			cfg := d.settings()
			channel := cfg.Feishu
			if item.channel == "dingtalk" {
				channel = cfg.Dingtalk
			}
			if !channel.Enabled || channel.Webhook == "" || !eventEnabled(cfg.Events, item.event) {
				continue
			}
			ctx, cancel := context.WithTimeout(d.ctx, Timeout)
			err := d.send(ctx, item.channel, channel, item.event.Message)
			cancel()
			if err != nil && d.logger != nil {
				// Sender errors are credential-free; do not log message content.
				d.logger.Printf("level=warn event=notification_send_failed channel=%s error=%q", item.channel, err)
			}
		}
	}
}

func eventEnabled(events appsettings.NotificationEvents, event Event) bool {
	if event.Failed && !events.TaskFailed {
		return false
	}
	switch event.Kind {
	case "stock_research":
		return events.StockResearch
	case "portfolio_inspection":
		return events.PortfolioInspection
	default:
		return false
	}
}

func (d *Dispatcher) Close() {
	d.mu.Lock()
	d.closed = true
	d.cancel()
	d.mu.Unlock()
	d.wg.Wait()
}
