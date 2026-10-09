package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/notification"
)

type notificationSender interface {
	Send(context.Context, string, appsettings.NotificationChannel, notification.Message) error
}

type notificationChannelView struct {
	Enabled bool                `json:"enabled"`
	Webhook secretSettingStatus `json:"webhook"`
	Secret  secretSettingStatus `json:"secret"`
	Keyword string              `json:"keyword"`
}

type notificationsView struct {
	Feishu   notificationChannelView        `json:"feishu"`
	Dingtalk notificationChannelView        `json:"dingtalk"`
	Events   appsettings.NotificationEvents `json:"events"`
}

type notificationChannelUpdate struct {
	Enabled      *bool   `json:"enabled"`
	Webhook      *string `json:"webhook"`
	Secret       *string `json:"secret"`
	Keyword      *string `json:"keyword"`
	ClearWebhook bool    `json:"clear_webhook"`
	ClearSecret  bool    `json:"clear_secret"`
}

type notificationsUpdate struct {
	Feishu   *notificationChannelUpdate `json:"feishu"`
	Dingtalk *notificationChannelUpdate `json:"dingtalk"`
	Events   *notificationEventsUpdate  `json:"events"`
}

type notificationEventsUpdate struct {
	StockResearch       *bool `json:"stock_research"`
	PortfolioInspection *bool `json:"portfolio_inspection"`
	TaskFailed          *bool `json:"task_failed"`
}

func buildNotificationsView(values appsettings.Notifications) notificationsView {
	channelView := func(cfg appsettings.NotificationChannel) notificationChannelView {
		return notificationChannelView{Enabled: cfg.Enabled, Webhook: secretSettingStatus{Configured: cfg.Webhook != ""}, Secret: secretSettingStatus{Configured: cfg.Secret != ""}, Keyword: cfg.Keyword}
	}
	return notificationsView{Feishu: channelView(values.Feishu), Dingtalk: channelView(values.Dingtalk), Events: values.Events}
}

func applyNotificationChannel(channel string, cfg appsettings.NotificationChannel, update *notificationChannelUpdate) (appsettings.NotificationChannel, error) {
	if update != nil {
		if update.Enabled != nil {
			cfg.Enabled = *update.Enabled
		}
		if update.Webhook != nil && len(*update.Webhook) > 2048 || update.Secret != nil && len(*update.Secret) > 1024 {
			return cfg, fmt.Errorf("通知 Webhook 或签名密钥过长")
		}
		applyOptionalSecret(&cfg.Webhook, update.Webhook)
		applyOptionalSecret(&cfg.Secret, update.Secret)
		applyOptionalString(&cfg.Keyword, update.Keyword)
		if update.ClearWebhook {
			cfg.Webhook = ""
		}
		if update.ClearSecret {
			cfg.Secret = ""
		}
	}
	if len([]rune(cfg.Keyword)) > 100 || strings.ContainsAny(cfg.Keyword, "\r\n") {
		return cfg, fmt.Errorf("通知关键词最多 100 个字符且不能换行")
	}
	if cfg.Webhook != "" {
		if err := notification.ValidateWebhook(channel, cfg.Webhook); err != nil {
			return cfg, err
		}
	} else if cfg.Enabled {
		return cfg, fmt.Errorf("启用通知前请配置 Webhook")
	}
	return cfg, nil
}

func decodeNotificationRequest(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		// Do not echo decoder text, which may contain part of a credential.
		return fmt.Errorf("通知配置请求格式无效")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return fmt.Errorf("通知配置请求只能包含一个 JSON 对象")
	}
	return nil
}

func (s *Server) settingsNotificationsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": buildNotificationsView(s.settingsStore.Snapshot().Notifications)})
}

func (s *Server) settingsNotificationsUpdate(w http.ResponseWriter, r *http.Request) {
	var request notificationsUpdate
	if err := decodeNotificationRequest(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	invalid := errors.New("invalid notifications")
	values, err := s.settingsStore.Update(func(values *appsettings.Values) error {
		var err error
		values.Notifications.Feishu, err = applyNotificationChannel("feishu", values.Notifications.Feishu, request.Feishu)
		if err != nil {
			return fmt.Errorf("%w: %s", invalid, err)
		}
		values.Notifications.Dingtalk, err = applyNotificationChannel("dingtalk", values.Notifications.Dingtalk, request.Dingtalk)
		if err != nil {
			return fmt.Errorf("%w: %s", invalid, err)
		}
		if request.Events != nil {
			if request.Events.StockResearch != nil {
				values.Notifications.Events.StockResearch = *request.Events.StockResearch
			}
			if request.Events.PortfolioInspection != nil {
				values.Notifications.Events.PortfolioInspection = *request.Events.PortfolioInspection
			}
			if request.Events.TaskFailed != nil {
				values.Notifications.Events.TaskFailed = *request.Events.TaskFailed
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, invalid) {
			writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), invalid.Error()+": "))
		} else {
			writeError(w, http.StatusInternalServerError, "保存通知配置失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": buildNotificationsView(values.Notifications)})
}

func (s *Server) settingsNotificationsTest(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Channel string                     `json:"channel"`
		Config  *notificationChannelUpdate `json:"config"`
	}
	if err := decodeNotificationRequest(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	values := s.settingsStore.Snapshot().Notifications
	cfg := values.Feishu
	if request.Channel == "dingtalk" {
		cfg = values.Dingtalk
	} else if request.Channel != "feishu" {
		writeError(w, http.StatusBadRequest, "不支持的通知渠道")
		return
	}
	cfg, err := applyNotificationChannel(request.Channel, cfg, request.Config)
	if err == nil {
		err = notification.ValidateWebhook(request.Channel, cfg.Webhook)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), notification.Timeout)
	defer cancel()
	started := time.Now()
	err = s.notificationSender.Send(ctx, request.Channel, cfg, notification.Message{Title: "easy-stock 通知测试", Text: "飞书 / 钉钉机器人连接成功。\n\n这是一条测试消息，请返回 easy-stock 保存通知配置。"})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"message": "测试通知已发送，请检查机器人所在群", "latency_ms": time.Since(started).Milliseconds()}})
}
