package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/monitor"
	"easy-stock/backend/internal/notification"
	"easy-stock/backend/internal/runtimelog"
)

// The engine owns quiet hours and digest timing. Delivery remains asynchronous,
// and only confirmed robot responses mark the source events as sent.
func (s *Server) NotifyMonitorAlert(ctx context.Context, event monitor.Event) (string, error) {
	if s == nil || s.monitorNotifications == nil || !s.monitorCanNotify(event) {
		return monitor.NotifySkipped, nil
	}
	if err := ctx.Err(); err != nil {
		return monitor.NotifyFailed, err
	}
	channels := monitor.NotificationChannels(event.NotifyChannels)
	ids := append([]int64(nil), event.NotificationEventIDs...)
	if event.ID > 0 {
		ids = append(ids, event.ID)
	}
	var mu sync.Mutex
	results := map[string]notification.DeliveryResult{}
	complete := func(result notification.DeliveryResult) {
		mu.Lock()
		defer mu.Unlock()
		result.Error = runtimelog.Redact(result.Error)
		results[result.Channel] = result
		if len(results) != len(channels) || s.monitorStore == nil || len(ids) == 0 {
			return
		}
		sent, failed := 0, 0
		deliveries := []notification.DeliveryResult{}
		messages := []string{}
		for _, channel := range channels {
			item := results[channel]
			deliveries = append(deliveries, item)
			if item.Status == "sent" {
				sent++
			} else if item.Status == "failed" {
				failed++
				label := "飞书"
				if channel == "dingtalk" {
					label = "钉钉"
				}
				messages = append(messages, label+"："+item.Error)
			}
		}
		status := monitor.NotifySkipped
		switch {
		case sent > 0 && failed > 0:
			status = monitor.NotifyPartial
		case sent > 0:
			status = monitor.NotifySent
		case failed > 0:
			status = monitor.NotifyFailed
		}
		payload, _ := json.Marshal(deliveries)
		saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.monitorStore.CompleteEventNotify(saveCtx, ids, status, strings.Join(messages, "；"), string(payload), time.Now()); err != nil && s.logger != nil {
			s.logger.Printf("level=error event=monitor_notification_result_failed error=%q", runtimelog.Redact(err.Error()))
		}
	}
	s.monitorNotifications.Publish(notification.Event{
		Kind: "monitor_alert", Channels: channels, OnDelivery: complete,
		Message: monitorNotificationMessage(event),
	})
	return monitor.NotifyQueued, nil
}

func (s *Server) monitorCanNotify(event monitor.Event) bool {
	if s == nil || s.monitorPreferences == nil || s.settingsStore == nil || !s.monitorPreferences.Snapshot().NotifyEnabled {
		return false
	}
	cfg := s.settingsStore.Snapshot().Notifications
	for _, channel := range monitor.NotificationChannels(event.NotifyChannels) {
		robot := cfg.Feishu
		if channel == "dingtalk" {
			robot = cfg.Dingtalk
		}
		if robot.Enabled && robot.Webhook != "" {
			return true
		}
	}
	return false
}

func monitorNotificationMessage(event monitor.Event) notification.Message {
	level := map[string]string{monitor.SeverityCritical: "紧急", monitor.SeverityWarning: "提醒", monitor.SeverityInfo: "记录"}[event.Severity]
	text := []string{}
	if event.RuleName != "" {
		text = append(text, "**规则**："+event.RuleName)
	}
	text = append(text, event.Message)
	if event.Price > 0 {
		text = append(text, fmt.Sprintf("现价 %.2f · 涨跌幅 %+.2f%%", event.Price, event.ChangePercent))
	}
	if !event.CreatedAt.IsZero() {
		text = append(text, "触发时间："+event.CreatedAt.In(foundation.AStockLocation).Format("2006-01-02 15:04:05")+"（北京时间）")
	}
	return notification.Message{Title: strings.TrimSpace("easy-stock 监控" + level), Text: strings.Join(text, "\n\n")}
}
