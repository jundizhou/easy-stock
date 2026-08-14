package review

import (
	"testing"
	"time"
)

func TestBuildDailyValidationScoresMarketThemeAndStockEvidence(t *testing.T) {
	summary := DailySummary{
		TradeDate:             "2026-08-13",
		GeneratedAt:           time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC),
		MarketRegime:          "修复",
		TomorrowOutlook:       "偏强情景优先，关注主线延续",
		Directions:            []DailyDirectionView{{Name: "机器人", Trigger: "核心股保持强势"}},
		TomorrowFocus:         []DailyStockView{{Name: "样本科技", Symbol: "600001.SH", Logic: "核心承接"}},
		VerificationChecklist: []string{"核心股不能出现连续负反馈"},
	}
	snapshot := DailyValidationSnapshot{
		TradeDate: "2026-08-14",
		Emotion:   &DailyValidationEmotion{Phase: "启动/修复", EmotionScore: 72, LimitUpCount: 45, BrokenCount: 8, MaxStreak: 6, PreviousLimitUpRet: 2.4},
		Indexes:   []DailyValidationIndex{{Name: "上证指数", ChangePercent: 1.1}},
		Themes:    []DailyValidationTheme{{Name: "机器人", ChangePercent: 3.4, RisingCount: 28, LimitUpCount: 5, Stage: "发酵"}},
		Stocks:    []DailyValidationStock{{Name: "样本科技", Symbol: "600001.SH", Open: 10.2, High: 11.2, Low: 10, Price: 11, PreviousClose: 10, ChangePercent: 10, Matched: true}},
	}
	validation := BuildDailyValidation(summary, snapshot)
	if validation.Market.Verdict != "correct" {
		t.Fatalf("market verdict = %s, want correct", validation.Market.Verdict)
	}
	if validation.Scenario.ActualKey != "strong" || validation.Scenario.Verdict != "correct" {
		t.Fatalf("scenario = %+v", validation.Scenario)
	}
	if len(validation.Directions) != 1 || validation.Directions[0].Verdict != "correct" {
		t.Fatalf("directions = %+v", validation.Directions)
	}
	if len(validation.Stocks) != 1 || validation.Stocks[0].Verdict != "correct" {
		t.Fatalf("stocks = %+v", validation.Stocks)
	}
	if validation.Coverage <= 0 || validation.Score <= 0 || validation.SummaryHash == "" {
		t.Fatalf("score metadata = %+v", validation)
	}
}

func TestBuildDailyValidationDoesNotScoreMissingDataAsWrong(t *testing.T) {
	validation := BuildDailyValidation(DailySummary{TradeDate: "2026-08-13", MarketRegime: "修复", TomorrowFocus: []DailyStockView{{Name: "未知股", Logic: "待观察"}}}, DailyValidationSnapshot{TradeDate: "2026-08-14"})
	if validation.Market.Verdict != "unverified" || validation.Stocks[0].Verdict != "unverified" {
		t.Fatalf("missing data should be unverified: %+v", validation)
	}
	if validation.Coverage != 0 || validation.Score != 0 {
		t.Fatalf("unverified data should not affect score: score=%v coverage=%v", validation.Score, validation.Coverage)
	}
}
