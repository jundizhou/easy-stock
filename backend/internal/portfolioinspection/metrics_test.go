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
	if metrics.StyleMatchScore >= 100 || len(metrics.StyleBreaches) == 0 {
		t.Fatalf("balanced profile should flag the 40%% single position: %+v", metrics)
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
