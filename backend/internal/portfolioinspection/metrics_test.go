package portfolioinspection

import (
	"fmt"
	"testing"
	"time"

	"easy-stock/backend/internal/stockanalysis"
)

func TestCalculateMetricsIncludesCashConcentrationAndCorrelation(t *testing.T) {
	request := Request{TraderProfile: ProfileBalanced, Holdings: []Holding{
		{Symbol: "600519.SH", Weight: 40},
		{Symbol: "000858.SZ", Weight: 30},
	}}
	results := []HoldingResult{
		successfulHolding(request.Holdings[0], "贵州茅台", 78, 35, "白酒消费", 5, 100),
		successfulHolding(request.Holdings[1], "五粮液", 68, 72, "白酒消费", 4, 80),
	}
	rules, _ := RulesFor(ProfileBalanced)
	metrics := CalculateMetrics(request, results, rules)
	if metrics.TotalPositionPercent != 70 || metrics.CashPercent != 30 || metrics.MaxSinglePercent != 40 || metrics.TopThreePercent != 70 {
		t.Fatalf("unexpected allocation metrics: %+v", metrics)
	}
	if metrics.CoveragePercent != 100 || len(metrics.ThemeExposures) != 1 || metrics.ThemeExposures[0].Weight != 70 {
		t.Fatalf("unexpected coverage or theme exposure: %+v", metrics)
	}
	if len(metrics.HighCorrelations) != 1 || metrics.HighCorrelations[0].Correlation < .99 {
		t.Fatalf("expected highly correlated pair, got %+v", metrics.HighCorrelations)
	}
	if metrics.DiversificationScore >= 100 || metrics.StyleMatchScore != 100 || len(metrics.StyleBreaches) == 0 {
		t.Fatalf("V2 should separate concentration from behavior fit: %+v", metrics)
	}
	if !metrics.HealthScoreAvailable || metrics.HealthScore < 75 || metrics.HealthScore > 90 {
		t.Fatalf("V2 health score should remain useful without ignoring risk: %+v", metrics)
	}
}

func TestPortfolioHealthV2IsGentlerForConcentratedQualityHolding(t *testing.T) {
	holding := Holding{Symbol: "600519.SH", Weight: 100}
	request := Request{TraderProfile: ProfileBalanced, Holdings: []Holding{holding}}
	result := successfulHolding(holding, "贵州茅台", 70, 45, "白酒消费", 3, 100)
	rules, _ := RulesFor(ProfileBalanced)
	metrics := CalculateMetrics(request, []HoldingResult{result}, rules)
	if metrics.HealthScore < 70 || metrics.HealthScore > 80 {
		t.Fatalf("concentrated quality portfolio health = %d, metrics=%+v", metrics.HealthScore, metrics)
	}
	if metrics.DiversificationScore >= 70 || metrics.StyleMatchScore < 90 {
		t.Fatalf("concentration and style components are not separated: %+v", metrics)
	}
}

func TestPortfolioHealthV2KeepsExtremeRiskCap(t *testing.T) {
	holding := Holding{Symbol: "300001.SZ", Weight: 100}
	request := Request{TraderProfile: ProfileBalanced, Holdings: []Holding{holding}}
	result := successfulHolding(holding, "高风险样本", 82, 90, "题材样本", 9, 100)
	result.Analysis.RiskControl.StopPrice = 70
	rules, _ := RulesFor(ProfileBalanced)
	metrics := CalculateMetrics(request, []HoldingResult{result}, rules)
	if metrics.HealthScore > 50 || !extremePortfolioRisk(metrics, rules) {
		t.Fatalf("extreme risk cap was not applied: %+v", metrics)
	}
}

func TestPortfolioHealthV2WithholdsScoreBelowCoverageThreshold(t *testing.T) {
	request := Request{TraderProfile: ProfileBalanced, Holdings: []Holding{{Symbol: "600519.SH", Weight: 60}, {Symbol: "000858.SZ", Weight: 40}}}
	results := []HoldingResult{
		successfulHolding(request.Holdings[0], "贵州茅台", 72, 40, "白酒消费", 3, 100),
		{Holding: request.Holdings[1], Status: "failed", Error: "数据不可用"},
	}
	rules, _ := RulesFor(ProfileBalanced)
	metrics := CalculateMetrics(request, results, rules)
	if metrics.CoveragePercent != 60 || metrics.HealthScoreAvailable || metrics.HealthScore != 0 {
		t.Fatalf("low coverage should not produce final health score: %+v", metrics)
	}
}

func TestNormalizeRequestRejectsDuplicateAndOverAllocation(t *testing.T) {
	_, err := normalizeRequest(Request{TraderProfile: ProfileBalanced, Holdings: []Holding{{Symbol: "600519", Weight: 60}, {Symbol: "600519.SH", Weight: 30}}})
	if err == nil {
		t.Fatal("duplicate holding should fail")
	}
	_, err = normalizeRequest(Request{TraderProfile: ProfileBalanced, Holdings: []Holding{{Symbol: "600519", Weight: 60}, {Symbol: "000858", Weight: 50}}})
	if err == nil {
		t.Fatal("allocation above 100% should fail")
	}
}

func successfulHolding(holding Holding, name string, score, risk int, theme string, volatility float64, start float64) HoldingResult {
	points := make([]stockanalysis.TrendPoint, 0, 40)
	for index := 0; index < 40; index++ {
		points = append(points, stockanalysis.TrendPoint{Date: fmt.Sprintf("2026-07-%02d", index+1), Close: start + float64(index)*1.2})
	}
	analysis := stockanalysis.Analysis{
		Symbol: holding.Symbol, Name: name, Scorecard: stockanalysis.Scorecard{Overall: score},
		RiskControl: stockanalysis.RiskControl{Score: risk, StopPrice: start * .9},
		Trend:       stockanalysis.TrendAnalysis{LatestClose: start, ATR14Percent: volatility},
		Theme:       stockanalysis.ThemeAnalysis{Primary: theme}, Chart: points,
	}
	return HoldingResult{Holding: holding, Status: "succeeded", CompletedAt: time.Now(), Analysis: &analysis}
}
