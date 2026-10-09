package httpapi

import (
	"fmt"
	"strings"

	"easy-stock/backend/internal/notification"
	"easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
)

func (s *Server) notifyStockResearch(job stockanalysis.ResearchJob) {
	if job.Status == "cancelled" || job.Request.AnalysisLevel == stockanalysis.ResearchLevelQuantitative {
		return
	}
	failed := job.Status != "succeeded"
	name := job.Request.Symbol
	summary := "研究未完成，已保留可用数据。请在个股 AI 分析中查看详情或继续研究。"
	status := "未完成"
	if !failed {
		status = "完成"
		if job.Analysis != nil {
			name = strings.TrimSpace(job.Analysis.Name + " " + job.Request.Symbol)
			summary = job.Analysis.Conclusion.Summary
			if job.Analysis.ResearchReport != nil {
				summary = job.Analysis.ResearchReport.Headline + "\n\n" + job.Analysis.ResearchReport.Thesis.Text
			}
		}
	}
	s.notifications.Publish(notification.Event{Kind: "stock_research", Failed: failed, Message: notification.Message{
		Title: "easy-stock 个股研究" + status,
		Text:  fmt.Sprintf("**%s**\n\n%s\n\n任务：%s\n请在 easy-stock 个股 AI 分析中查看完整报告。", name, truncateRunes(summary, 1000), job.ID),
	}})
}

func (s *Server) notifyPortfolioInspection(job portfolioinspection.Job) {
	if job.Status == "cancelled" {
		return
	}
	failed := job.Status != "succeeded"
	status := "未完成"
	summary := "已有个股报告已保留，请在持仓 AI 巡检中继续研究或重试组合评估。"
	if !failed {
		status = "完成"
		if job.Report != nil {
			summary = job.Report.Conclusion.ExecutiveSummary
		}
	}
	s.notifications.Publish(notification.Event{Kind: "portfolio_inspection", Failed: failed, Message: notification.Message{
		Title: "easy-stock 持仓巡检" + status,
		Text:  fmt.Sprintf("完成 %d/%d 只个股研究\n\n%s\n\n任务：%s\n请在 easy-stock 持仓 AI 巡检中查看完整报告。", job.CompletedStocks, job.TotalStocks, truncateRunes(summary, 1000), job.ID),
	}})
}
