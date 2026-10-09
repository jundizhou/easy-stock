package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/notification"
	"easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
)

func TestTaskNotificationsKeepReportReferenceAndSkipCancellation(t *testing.T) {
	s := NewServer(Config{})
	defer s.Close()
	_, err := s.settingsStore.Update(func(values *appsettings.Values) error {
		values.Notifications.Feishu = appsettings.NotificationChannel{Enabled: true, Webhook: "https://open.feishu.cn/open-apis/bot/v2/hook/test-only"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sent := make(chan notification.Message, 4)
	s.notifications.Close()
	s.notifications = notification.NewDispatcher(func(_ context.Context, _ string, _ appsettings.NotificationChannel, message notification.Message) error {
		sent <- message
		return nil
	}, func() appsettings.Notifications { return s.settingsStore.Snapshot().Notifications }, nil)
	s.notifyStockResearch(stockanalysis.ResearchJob{ID: "stock-task-reference", Status: "succeeded", Request: stockanalysis.ResearchRequest{Symbol: "600519.SH"}, Analysis: &stockanalysis.Analysis{ResearchReport: &stockanalysis.ResearchReport{ResearchSynthesis: stockanalysis.ResearchSynthesis{Headline: "摘要", Thesis: stockanalysis.ResearchClaim{Text: strings.Repeat("研究结论", 3000)}}}}})
	select {
	case message := <-sent:
		if !strings.Contains(message.Text, "stock-task-reference") || len([]rune(message.Text)) > 1200 {
			t.Fatal("long summary lost task reference or exceeded budget")
		}
	case <-time.After(time.Second):
		t.Fatal("stock completion notification missing")
	}
	s.notifyPortfolioInspection(portfolioinspection.Job{ID: "portfolio-task-reference", Status: "partial", Error: "private-provider-key", Message: "private-provider-key"})
	select {
	case message := <-sent:
		if !strings.Contains(message.Text, "portfolio-task-reference") || strings.Contains(message.Text, "private-provider-key") {
			t.Fatal("incomplete portfolio notification leaked provider error or lost task reference")
		}
	case <-time.After(time.Second):
		t.Fatal("incomplete portfolio notification missing")
	}
	s.notifyStockResearch(stockanalysis.ResearchJob{Status: "cancelled"})
	s.notifyStockResearch(stockanalysis.ResearchJob{Status: "succeeded", Request: stockanalysis.ResearchRequest{AnalysisLevel: stockanalysis.ResearchLevelQuantitative}})
	s.notifyPortfolioInspection(portfolioinspection.Job{Status: "cancelled"})
	s.notifications.Close()
	if len(sent) != 0 {
		t.Fatal("cancelled or quantitative task produced a notification")
	}
}
