package portfolioinspection

import (
	"context"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/review"
)

func TestExpectationStoreFindsMatchingCachedJob(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	job := ExpectationJob{
		ID: "expectation-1", Status: "succeeded", PromptVersion: ExpectationPromptVersion,
		PortfolioHash: "portfolio-hash", SummaryHash: "summary-hash",
		Request:   ExpectationRequest{SummaryDate: "2026-08-27", TraderProfile: ProfileBalanced},
		UpdatedAt: time.Now().UTC(), ReportAvailable: true,
	}
	if _, err := store.SaveExpectation(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	cached, err := store.FindExpectation(context.Background(), "2026-08-27", "portfolio-hash", "summary-hash", ExpectationPromptVersion)
	if err != nil {
		t.Fatal(err)
	}
	if cached.ID != job.ID || !cached.ReportAvailable {
		t.Fatalf("unexpected cached job: %+v", cached)
	}
	latest, err := store.LatestExpectation(context.Background(), "2026-08-27")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != job.ID {
		t.Fatalf("latest id=%q", latest.ID)
	}
}

func TestExpectationStoreMarksRunningJobsInterrupted(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	job := ExpectationJob{ID: "running-1", Status: "running", Stage: "analyzing_stocks", PromptVersion: ExpectationPromptVersion, Request: ExpectationRequest{SummaryDate: "2026-08-27"}, UpdatedAt: time.Now().UTC()}
	if _, err := store.SaveExpectation(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkExpectationsInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetExpectation(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "interrupted" || loaded.Stage != "interrupted" {
		t.Fatalf("job not interrupted: %+v", loaded)
	}
}

func TestBuildExpectationPromptPinsEvidenceRules(t *testing.T) {
	rules, _ := RulesFor(ProfileBalanced)
	prompt, err := buildExpectationPrompt(
		review.DailySummary{TradeDate: "2026-08-27", MarketRegime: "震荡分化", TomorrowOutlook: "观察主线承接"},
		Request{TraderProfile: ProfileBalanced, Holdings: []Holding{{Symbol: "600519.SH", Weight: 50}}},
		[]HoldingResult{{Holding: Holding{Symbol: "600519.SH", Weight: 50}, Status: "failed", Error: "missing"}},
		Metrics{TotalPositionPercent: 50, CashPercent: 50, HealthScore: 70, StyleMatchScore: 90}, rules,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"portfolio-tomorrow-expectation-v1", "作者观点只能作为情景证据", "strong、base、weak三个情景必须齐全", `"market_regime":"震荡分化"`} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q", expected)
		}
	}
}

func TestNormalizeExpectationConclusionRejectsMissingHolding(t *testing.T) {
	report := ExpectationConclusion{Headline: "明日按条件验证持仓", PortfolioBias: "中性", Confidence: .8}
	err := normalizeExpectationConclusion(&report, Request{Holdings: []Holding{{Symbol: "600519.SH", Weight: 50}}})
	if err == nil || !strings.Contains(err.Error(), "未覆盖全部持仓") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalExpectationReportKeepsThreeScenariosAndEveryHolding(t *testing.T) {
	request := Request{TraderProfile: ProfileBalanced, Holdings: []Holding{
		{Symbol: "600519.SH", Weight: 40},
		{Symbol: "000001.SZ", Weight: 20},
	}}
	report := localExpectationReport(
		review.DailySummary{TradeDate: "2026-08-27", MarketRegime: "震荡分化"},
		request,
		[]HoldingResult{
			{Holding: request.Holdings[0], Status: "failed"},
			{Holding: request.Holdings[1], Status: "failed"},
		},
		Metrics{TotalPositionPercent: 60, CashPercent: 40},
	)
	if len(report.Scenarios) != 3 {
		t.Fatalf("scenario count=%d", len(report.Scenarios))
	}
	if err := normalizeExpectationConclusion(&report, request); err != nil {
		t.Fatalf("fallback report should satisfy the complete report contract: %v", err)
	}
}
