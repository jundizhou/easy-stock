package dailyanalysis

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func syntheticLines(closes []float64) []foundation.KLine {
	lines := make([]foundation.KLine, len(closes))
	base := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	for i, close := range closes {
		lines[i] = foundation.KLine{
			Time: base.AddDate(0, 0, i), Open: close, High: close * 1.01, Low: close * 0.99, Close: close,
			Volume: 1_000_000, PreviousClose: closes[max(0, i-1)],
		}
	}
	return lines
}

func TestComputeIndicatorsRisingMarket(t *testing.T) {
	closes := make([]float64, 0, 160)
	for i := 0; i < 160; i++ {
		closes = append(closes, 10+float64(i)*0.05)
	}
	indicators := ComputeIndicators(syntheticLines(closes))
	if indicators.MA20 == 0 || indicators.MA60 == 0 {
		t.Fatalf("MA20/MA60 should be computed, got %+v", indicators)
	}
	if indicators.MACDHist <= 0 {
		t.Fatalf("rising market should produce positive MACD hist, got %.4f", indicators.MACDHist)
	}
	if indicators.Rsi14 < 70 {
		t.Fatalf("monotonic rise should push RSI14 above 70, got %.1f", indicators.Rsi14)
	}
	if indicators.KDJk <= 50 {
		t.Fatalf("monotonic rise should keep KDJ K above 50, got %.1f", indicators.KDJk)
	}
	if indicators.BollUpper <= indicators.BollMid || indicators.BollMid <= indicators.BollLower {
		t.Fatalf("boll band order invalid: %.2f/%.2f/%.2f", indicators.BollUpper, indicators.BollMid, indicators.BollLower)
	}
	if indicators.Support <= 0 || indicators.Resistance <= 0 {
		t.Fatalf("support/resistance should be positive: %+v", indicators)
	}
}

func TestComputeIndicatorsFallingMarket(t *testing.T) {
	closes := make([]float64, 0, 160)
	for i := 0; i < 160; i++ {
		closes = append(closes, 50-float64(i)*0.08)
	}
	indicators := ComputeIndicators(syntheticLines(closes))
	if indicators.MACDHist >= 0 {
		t.Fatalf("falling market should produce negative MACD hist, got %.4f", indicators.MACDHist)
	}
	if indicators.Rsi14 > 45 {
		t.Fatalf("monotonic fall should push RSI14 low, got %.1f", indicators.Rsi14)
	}
}

func TestScoreAndAction(t *testing.T) {
	rising := make([]float64, 0, 160)
	for i := 0; i < 160; i++ {
		rising = append(rising, 10+float64(i)*0.06)
	}
	report := buildStockReport(syntheticLines(rising), nil, "600519.SH")
	if report.Status != "succeeded" {
		t.Fatalf("report should succeed, got %s %s", report.Status, report.Error)
	}
	if report.Score < 60 {
		t.Fatalf("strong uptrend should score >= 60, got %d (%+v)", report.Score, report.Indicators)
	}
	if report.Action == "回避" {
		t.Fatalf("strong uptrend should not be 回避, action=%s", report.Action)
	}
	if len(report.Signals) == 0 || len(report.Risks) == 0 || len(report.Checklist) == 0 {
		t.Fatalf("signals/risks/checklist should not be empty")
	}

	falling := make([]float64, 0, 160)
	for i := 0; i < 160; i++ {
		falling = append(falling, 60-float64(i)*0.1)
	}
	bear := buildStockReport(syntheticLines(falling), nil, "000001.SZ")
	if bear.Score > 40 {
		t.Fatalf("downtrend should score low, got %d", bear.Score)
	}
}

func TestEmptyLines(t *testing.T) {
	report := buildStockReport(nil, nil, "600519.SH")
	if report.Status != "failed" {
		t.Fatalf("empty kline should fail, got %s", report.Status)
	}
	if ComputeIndicators(nil).MA20 != 0 {
		t.Fatalf("empty input should give zero indicators")
	}
}

func TestSummarize(t *testing.T) {
	summary := summarize([]StockReport{
		{Status: "succeeded", Score: 80},
		{Status: "succeeded", Score: 65},
		{Status: "succeeded", Score: 30},
		{Status: "failed"},
	})
	if summary.BullCount != 2 || summary.BearCount != 1 || summary.FailedCount != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if math.Abs(summary.AverageScore-58.3) > 0.2 {
		t.Fatalf("average score should be ~58.3, got %.1f", summary.AverageScore)
	}
	if summary.Bias != "整体偏多" && summary.Bias != "略偏多" {
		t.Fatalf("bias should lean bullish, got %s", summary.Bias)
	}
}

func TestNormalizeSymbols(t *testing.T) {
	symbols, err := normalizeSymbols([]string{"600519", "000001"})
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if len(symbols) != 2 || symbols[0] != "600519.SH" {
		t.Fatalf("unexpected symbols: %v", symbols)
	}
	if _, err := normalizeSymbols([]string{"600519", "600519"}); err == nil {
		t.Fatalf("duplicates should be rejected")
	}
	if _, err := normalizeSymbols(make([]string, MaxSymbols+1)); err == nil {
		t.Fatalf("overflow should be rejected")
	}
}

func TestValidateWebhookURL(t *testing.T) {
	valid := []string{
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc123",
		"https://open.feishu.cn/open-apis/bot/v2/hook/abc123",
	}
	for _, raw := range valid {
		host := wecomWebhookHost
		if strings.Contains(raw, "feishu") {
			host = feishuWebhookHost
		}
		if err := validateWebhookURL(raw, host); err != nil {
			t.Fatalf("expected %q to pass, got %v", raw, err)
		}
	}
	invalid := []struct {
		raw  string
		host string
	}{
		{"http://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x", wecomWebhookHost}, // 非 https
		{"https://127.0.0.1/cgi-bin/webhook/send", wecomWebhookHost},                // 内网 IP
		{"https://evil.example.com/collect", wecomWebhookHost},                      // 任意域名
		{"https://qyapi.weixin.qq.com.evil.com/send", wecomWebhookHost},             // 仿冒子域
		{"https://qyapi.weixin.qq.com:8443/send", wecomWebhookHost},                 // 自定义端口
		{"https://user:pass@qyapi.weixin.qq.com/send", wecomWebhookHost},            // userinfo
		{"https://open.feishu.cn/open-apis/bot/v2/hook/x", wecomWebhookHost},        // 域名与渠道不匹配
	}
	for _, item := range invalid {
		if err := validateWebhookURL(item.raw, item.host); err == nil {
			t.Fatalf("expected %q to be rejected", item.raw)
		}
	}
}

func TestUpdateConfigRejectsBadWebhook(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	service := NewService(store, nil, nil, nil, nil)
	_, err = service.UpdateConfig(context.Background(), Config{
		Watchlist: []string{"600519"},
		Push:      PushConfig{Enabled: true, WeComWebhook: "https://169.254.169.254/latest/meta-data"},
	})
	if err == nil || !strings.Contains(err.Error(), "企业微信 Webhook 无效") {
		t.Fatalf("internal webhook target should be rejected, got %v", err)
	}
}
