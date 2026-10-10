package httpapi

import (
	"easy-stock/backend/internal/notification"
	"easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
)

func (s *Server) notifyStockResearch(job stockanalysis.ResearchJob) {
	if job.Status == "cancelled" || job.Request.AnalysisLevel == stockanalysis.ResearchLevelQuantitative {
		return
	}
	failed := job.Status != "succeeded"
	status := "未完成"
	if !failed {
		status = "完成"
	}
	s.notifications.Publish(notification.Event{Kind: "stock_research", Failed: failed, Message: notification.Message{
		Title: "easy-stock 个股研究" + status,
		Text:  stockanalysis.NotificationMarkdown(job),
	}})
}

func (s *Server) notifyPortfolioInspection(job portfolioinspection.Job) {
	if job.Status == "cancelled" {
		return
	}
	failed := job.Status != "succeeded"
	status := "未完成"
	if !failed {
		status = "完成"
	}
	var channels []string
	if job.ScheduleID != "" {
		channels = append([]string{}, job.NotificationChannels...)
	}
	s.notifications.Publish(notification.Event{Channels: channels, Kind: "portfolio_inspection", Failed: failed, Message: notification.Message{
		Title: "easy-stock 持仓巡检" + status,
		Text:  portfolioinspection.NotificationMarkdown(job),
	}})
}
