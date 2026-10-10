package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"easy-stock/backend/internal/portfolioinspection"
)

func (s *Server) portfolioScheduleGet(w http.ResponseWriter, r *http.Request) {
	if s.portfolioStore == nil {
		writeError(w, http.StatusServiceUnavailable, "定时巡检服务不可用")
		return
	}
	value, err := s.portfolioStore.GetSchedule(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"data": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取定时巡检失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": value})
}

func (s *Server) portfolioScheduleSave(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var value portfolioinspection.ScheduleConfig
	if err := decoder.Decode(&value); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if value.Enabled {
		settings := s.settingsStore.Snapshot().Notifications
		for _, channel := range value.Channels {
			cfg := settings.Feishu
			label := "飞书"
			if channel == "dingtalk" {
				cfg = settings.Dingtalk
				label = "钉钉"
			}
			if !cfg.Enabled || cfg.Webhook == "" {
				writeError(w, http.StatusBadRequest, "请先在系统设置中配置并启用"+label+"机器人")
				return
			}
		}
	}
	saved, err := s.portfolioScheduler.Save(r.Context(), r.PathValue("id"), value, time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": saved})
}

func (s *Server) portfolioScheduleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.portfolioScheduler.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "删除定时巡检失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": nil})
}

func (s *Server) RunPortfolioScheduler(ctx context.Context) {
	if s.portfolioScheduler == nil {
		return
	}
	s.logSchedulerLifecycle(ctx, "portfolio-inspection", "scheduled_inspection", func() {
		tick := func() {
			if err := s.portfolioScheduler.Tick(ctx, time.Now()); err != nil && s.logger != nil {
				s.logger.Printf("level=warn event=scheduler_error feature=portfolio-inspection task=scheduled_inspection")
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
	})
}
