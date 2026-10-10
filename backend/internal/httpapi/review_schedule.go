package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"easy-stock/backend/internal/sqliteutil"

	"easy-stock/backend/internal/notification"
	"easy-stock/backend/internal/review"
)

func (s *Server) reviewScheduleGet(w http.ResponseWriter, r *http.Request) {
	value, err := s.reviewStore.GetDailySchedule(r.Context())
	if err != nil {
		writeError(w, 500, "读取定时复盘失败")
		return
	}
	writeJSON(w, 200, map[string]any{"data": value})
}
func (s *Server) reviewScheduleSave(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	var c review.DailyScheduleConfig
	if err := decoder.Decode(&c); err != nil {
		writeError(w, 400, "无效的定时复盘设置")
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, 400, "无效的定时复盘设置")
		return
	}
	if c.Enabled {
		n := s.settingsStore.Snapshot().Notifications
		for _, ch := range c.Channels {
			cfg := n.Feishu
			if ch == "dingtalk" {
				cfg = n.Dingtalk
			}
			if !cfg.Enabled || cfg.Webhook == "" {
				writeError(w, 400, "请先配置并启用所选通知机器人")
				return
			}
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	value, err := s.reviewScheduler.Save(ctx, c, time.Now())
	if err != nil {
		if sqliteutil.IsBusy(err) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "数据库正在处理其他任务，定时设置尚未保存，请稍后重试")
			return
		}

		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"data": value})
}
func (s *Server) notifyDailySummary(job review.DailySummaryJob, summary *review.DailySummary) {
	title := "easy-stock 大V每日复盘完成"
	if job.Status != "succeeded" {
		title = "easy-stock 大V每日复盘未完成"
	}
	s.notifications.Publish(notification.Event{Kind: "daily_review", Failed: job.Status != "succeeded", Channels: append([]string{}, job.NotificationChannels...), Message: notification.Message{Title: title, Text: review.DailySummaryMarkdown(job, summary)}})
}
func (s *Server) RunDailySummaryScheduler(ctx context.Context) {
	if s.reviewScheduler == nil {
		return
	}
	tick := func() {
		if err := s.reviewScheduler.Tick(ctx, time.Now()); err != nil && s.logger != nil {
			s.logger.Printf("level=warn event=daily_review_schedule_failed")
		}
	}
	tick()
	timer := time.NewTicker(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			tick()
		}
	}
}
