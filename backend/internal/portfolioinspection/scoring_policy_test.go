package portfolioinspection

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScoringInputIsIndependentOfAccountExposure(t *testing.T) {
	for _, profile := range []TraderProfile{ProfileAggressive, ProfileBalanced, ProfileSteady} {
		for _, withStop := range []bool{false, true} {
			var previous string
			for _, weight := range []int{20, 50, 100} {
				req, results, _, _ := scoreFixture()
				req.TraderProfile, req.Holdings[0].Weight = profile, weight
				results[0].Holding = req.Holdings[0]
				if withStop {
					results[0].Analysis.Quote.Price = 33.37
					results[0].Analysis.RiskControl.StopPrice = 30.11
				}
				rules, _ := RulesFor(profile)
				metrics := CalculateMetrics(req, results, rules)
				prompt, err := buildScoringPrompt(req, results, metrics, rules)
				if err != nil {
					t.Fatal(err)
				}
				if previous != "" && previous != prompt {
					t.Fatal("only account exposure changed, but scorer saw different input", profile, weight, withStop)
				}
				previous = prompt
				_, payload, _ := strings.Cut(prompt, "[组合证据JSON]\n")
				for _, forbidden := range []string{`"cash_percent"`, `"total_position_percent"`, `"weight_percent"`, `"minimum_cash_percent"`, `保留现金缓冲`} {
					if strings.Contains(payload, forbidden) {
						t.Fatal("account exposure leaked into scoring payload", forbidden)
					}
				}
				facts := portfolioFacts(req, results, metrics)
				if facts["cash_percent"].Value != 100-weight || facts["equity_max_single_percent"].Value != float64(100) {
					t.Fatal("report lost actual cash or normalized concentration", facts)
				}
			}
		}
	}
}

func TestFullInvestmentDoesNotLimitRiskOrStrategyScore(t *testing.T) {
	for _, weight := range []int{50, 100} {
		req, results, _, report := scoreFixture()
		req.Holdings[0].Weight = weight
		results[0].Holding = req.Holdings[0]
		rules, _ := RulesFor(req.TraderProfile)
		metrics := CalculateMetrics(req, results, rules)
		for i := range report.Dimensions {
			if report.Dimensions[i].Key == "risk_capacity" || report.Dimensions[i].Key == "strategy_fit" {
				score := 85
				report.Dimensions[i].Score = &score
			}
		}
		report.StyleMatch = "匹配"
		if err := validateScoringReport(&report, req, results, metrics); err != nil {
			t.Fatal(err)
		}
		if report.StyleMatch != "匹配" || *report.TotalScore != 76 {
			t.Fatal("account exposure capped score or match", report)
		}
	}
}

func TestCashAndAccountWeightsCannotSupportDimensionScores(t *testing.T) {
	for _, key := range []string{"cash_percent", "total_position_percent", "profile.minimum_cash_percent", "max_single_percent", "top_three_percent", "600519.SH.weight_percent"} {
		req, results, metrics, report := scoreFixture()
		report.Dimensions[0].EvidenceRefs = []EvidenceRef{{Fact: key}}
		if err := validateScoringReport(&report, req, results, metrics); err == nil || !strings.Contains(err.Error(), "不参与评分") {
			t.Fatal("excluded scoring evidence accepted", key, err)
		}
		data, _ := json.Marshal(report)
		if _, err := DecodeNamedOptimizationComparisonScore(data, OptimizationReport(req, results), "a"); err == nil {
			t.Fatal("paired scorer admitted account-based score", key)
		}
	}
}

func TestConcentrationGuardUsesStockRelativeWeights(t *testing.T) {
	req, results, _, report := scoreFixture()
	req.Holdings[0].Weight = 10
	results[0].Holding = req.Holdings[0]
	rules, _ := RulesFor(req.TraderProfile)
	metrics := CalculateMetrics(req, results, rules)
	score := 90
	report.Dimensions[1].Score = &score
	if err := validateScoringReport(&report, req, results, metrics); err == nil || !strings.Contains(err.Error(), "集中度") {
		t.Fatal("small total exposure hid single-stock concentration", err)
	}
}
