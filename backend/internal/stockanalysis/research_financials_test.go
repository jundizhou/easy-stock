package stockanalysis

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestFinancialSourcesPreserveConflictsAndPriorYearPeriodsInModelInput(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	cutoff, _ := time.Parse(time.RFC3339, "2026-10-05T19:00:00+08:00")
	primary := []foundation.StockFundamentals{}
	secondary := []foundation.StockFinancialEvidence{}
	for _, date := range []string{"2026-06-30", "2026-03-31", "2025-12-31", "2025-09-30", "2025-06-30", "2025-03-31"} {
		day, _ := time.Parse("2006-01-02", date)
		primary = append(primary, foundation.StockFundamentals{Symbol: analysis.Symbol, ReportDate: date, PublishedAt: day.AddDate(0, 1, 0), Revenue: 1000000000, NetProfit: 100000000, DeductedNetProfit: 80000000, DeductedNetProfitAvailable: true, Meta: foundation.SourceMeta{Source: "eastmoney:f10-financials"}})
		secondary = append(secondary, foundation.StockFinancialEvidence{Symbol: analysis.Symbol, ReportDate: date, PublishedAt: day.AddDate(0, 1, 0), Fields: map[string]float64{"revenue": 1000000000, "net_profit": 100000000, "deducted_net_profit": 90000000, "operating_cash_flow": 50000000}, Meta: foundation.SourceMeta{Source: "sina:financial-indicators"}})
	}
	input := Input{FinancialHistory: primary, FinancialSupplement: secondary}
	actual := BuildResearchSnapshot(input, analysis, cutoff)
	var source ResearchSource
	for _, s := range actual.Sources {
		if s.ID == "f-financial" {
			source = s
		}
	}
	if !strings.Contains(source.Content, "conflicting_fields") || !strings.Contains(source.Content, "sina:financial-indicators") || financialConflictReason(source) == "" {
		t.Fatal("financial conflict lost")
	}
	card := researchSourceForLevel(source, snapshot, researchLevelPolicyFor(ResearchLevelStandard))
	for _, period := range []string{"2025-06-30", "2025-03-31", "2026-03-31"} {
		if !strings.Contains(card.Content, period) {
			t.Fatalf("lost single-quarter YoY base %s", period)
		}
	}
	result := validResearch()
	result.EvidenceLevel = "sufficient"
	result.Thesis.SourceIDs = []string{"f-financial"}
	snapshot.Sources = append(snapshot.Sources, source)
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if result.EvidenceLevel != "limited" || !strings.Contains(strings.Join(result.EvidenceReasons, " "), "冲突") {
		t.Fatal("conflicting financials accepted as sufficient")
	}
	var values map[string]any
	if json.Unmarshal([]byte(card.Content), &values) != nil {
		t.Fatal("financial card invalid JSON")
	}
}

func TestFinancialFallbackDoesNotInventMissingFieldsOrUseFutureDisclosure(t *testing.T) {
	cutoff := time.Now()
	x := foundation.StockFinancialEvidence{Symbol: "600519.SH", ReportDate: "2026-06-30", PublishedAt: cutoff.Add(-time.Hour), Fields: map[string]float64{"revenue": 100, "net_profit": 0}}
	items := FinancialFallback([]foundation.StockFinancialEvidence{x}, x.Symbol, cutoff)
	if len(items) != 1 || items[0].DeductedNetProfitAvailable || items[0].Revenue != 100 {
		t.Fatalf("missing fields fabricated: %+v", items)
	}
	x.PublishedAt = cutoff.Add(time.Hour)
	if len(FinancialFallback([]foundation.StockFinancialEvidence{x}, x.Symbol, cutoff)) != 0 {
		t.Fatal("future report leaked into research")
	}
	x.PublishedAt = cutoff.Add(-time.Hour)
	delete(x.Fields, "revenue")
	if len(FinancialFallback([]foundation.StockFinancialEvidence{x}, x.Symbol, cutoff)) != 0 {
		t.Fatal("missing revenue became zero")
	}
}
