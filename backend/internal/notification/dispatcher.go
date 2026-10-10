package notification

import (
	"context"
	"log"
	"sync"
	"time"

	"easy-stock/backend/internal/appsettings"
)

type SendFunc func(context.Context, string, appsettings.NotificationChannel, Message) error

type Event struct {
	// Nil follows global event settings; a non-nil list selects scheduled delivery channels.
	Channels []string
	Kind     string
	Failed   bool
	Message  Message
}

type delivery struct {
	channel string
	event   Event
}

// Dispatcher keeps webhook latency out of task completion and uses a bounded
// queue. The worker starts only when an enabled channel receives an event.
type Dispatcher struct {
	mu           sync.Mutex
	closed       bool
	started      bool
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	queue        chan delivery
	send         SendFunc
	settings     func() appsettings.Notifications
	logger       *log.Logger
	partInterval time.Duration
}

func NewDispatcher(send SendFunc, settings func() appsettings.Notifications, logger *log.Logger) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &Dispatcher{ctx: ctx, cancel: cancel, queue: make(chan delivery, 32), send: send, settings: settings, logger: logger, partInterval: 3 * time.Second}
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
		if !selectedChannel(event, channel.name) || !channel.config.Enabled || channel.config.Webhook == "" {
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
			d.deliver(item)
		}
	}
}

func eventEnabled(events appsettings.NotificationEvents, event Event) bool {
	if event.Failed && !events.TaskFailed {
		return false
	}
	if (event.Kind == "portfolio_inspection" || event.Kind == "daily_review") && event.Channels != nil {
		return true
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

func selectedChannel(event Event, channel string) bool {
	if event.Channels == nil {
		return true
	}
	for _, selected := range event.Channels {
		if selected == channel {
			return true
		}
	}
	return false
}

func (d *Dispatcher) deliver(item delivery) {
	parts := SplitMessage(item.event.Message)
	for i, message := range parts {
		if i > 0 && d.partInterval > 0 {
			timer := time.NewTimer(d.partInterval)
			select {
			case <-d.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if d.ctx.Err() != nil {
			return
		}
		// Recheck each part: disabling a robot also stops a multipart delivery.
		cfg := d.settings()
		channel := cfg.Feishu
		if item.channel == "dingtalk" {
			channel = cfg.Dingtalk
		}
		if !channel.Enabled || channel.Webhook == "" || !eventEnabled(cfg.Events, item.event) {
			return
		}
		ctx, cancel := context.WithTimeout(d.ctx, Timeout)
		err := d.send(ctx, item.channel, channel, message)
		cancel()
		if err != nil {
			if d.logger != nil {
				d.logger.Printf("level=warn event=notification_send_failed channel=%s part=%d total=%d error=%q", item.channel, i+1, len(parts), err)
			}
			return // Do not send later sections after a failed part or retry ambiguously.
		}
	}
}
