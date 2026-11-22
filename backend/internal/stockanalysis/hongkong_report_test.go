package stockanalysis

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
)

func TestHongKongReportUsesDisclosedFieldsAndApplicableEvidence(t *testing.T) {
	symbol := "00005.HK"
	for _, days := range []int{5, 60} {
		input := Input{
			Symbol: symbol, KLines: syntheticTrendLines(symbol, days, 100, .1, 500_000_000),
			Fundamentals: &foundation.StockFundamentals{Symbol: symbol, Currency: "USD", ReportDate: "2026-09-30",
				Revenue: 100, NetProfit: 10, ROE: 11.02, GrossMargin: 95,
				Meta: foundation.SourceMeta{AvailableFields: []string{"revenue", "net_profit", "roe", "net_profit_yoy"}}},
			MarketEmotion: &marketemotion.Snapshot{Phase: "退潮"}, Industry: "A股行业",
		}
		for i := range input.KLines {
			input.KLines[i].Meta.Source = "tencent:hk-qfq-kline"
		}
		report, err := Analyze(input)
		if err != nil {
			t.Fatal(err)
		}
		fin := report.Fundamental
		if fin.GrossMargin != nil || fin.RecurringNetProfit != nil || fin.RecurringNetProfitYearOverYear != nil || fin.NetProfitYearOverYear == nil || *fin.NetProfitYearOverYear != 0 || fin.Score != 56 {
			t.Fatalf("%d days: missing or unreported fields affected report: %+v", days, fin)
		}
		if !strings.Contains(fin.Summary, "归母净利同比+0.0%") || strings.Contains(fin.Summary, "毛利率") || strings.Contains(fin.Summary, "扣非") || len(fin.SustainabilityFlags) != 0 {
			t.Fatalf("%d days: unexpected financial summary: %+v", days, fin)
		}
		if report.Market != nil || report.Theme.Source != "" || report.Theme.Primary != "" {
			t.Fatalf("%d days: A-share context attributed to HK report", days)
		}
		for _, risk := range report.Risks {
			if strings.Contains(risk, "题材") || strings.Contains(risk, "市场情绪") {
				t.Fatalf("%d days: inapplicable risk: %s", days, risk)
			}
		}
		var source, status string
		for _, row := range report.Evidence {
			if row.Category == "短线" {
				source = row.Source
			}
		}
		for _, row := range report.DataQuality {
			if row.Key == "fundamental" {
				status = row.Status
			}
		}
		if source != "tencent:hk-qfq-kline" || status != "ready" {
			t.Fatalf("%d days: incorrect source %q or financial status %q", days, source, status)
		}
	}
}

func TestHongKongFinancialEvidenceDistinguishesZeroFromMissing(t *testing.T) {
	item := foundation.StockFundamentals{Symbol: "00700.HK", ReportDate: "2026-09-30", Currency: "CNY",
		Meta: foundation.SourceMeta{AvailableFields: []string{"gross_margin"}}}
	analysis := analyzeFundamentals(item.Symbol, &item)
	if analysis.GrossMargin == nil || *analysis.GrossMargin != 0 || !strings.Contains(analysis.Summary, "毛利率0.0%") {
		t.Fatalf("explicit zero lost: %+v", analysis)
	}
	row := financialModelRows([]foundation.StockFundamentals{item}, nil)[0].(map[string]any)
	if row["currency"] != "CNY" || row["gross_margin"] != 0.0 {
		t.Fatalf("reported fields lost: %+v", row)
	}
	for _, key := range []string{"revenue_yoy", "roe", "operating_cash_flow_per_share", "deducted_net_profit"} {
		if _, ok := row[key]; ok {
			t.Fatalf("missing %s exposed as model evidence: %+v", key, row)
		}
	}
}

func TestHongKongQuickResearchKeepsFinancialDefinitions(t *testing.T) {
	item := foundation.StockFundamentals{Symbol: "00005.HK", ReportDate: "2026-09-30", Currency: "USD",
		Revenue: 100, NetProfit: 10, Meta: foundation.SourceMeta{Source: "eastmoney:hkf10-financials", AvailableFields: []string{"revenue", "net_profit"}}}
	input := Input{Symbol: item.Symbol, Fundamentals: &item, KLines: syntheticTrendLines(item.Symbol, 25, 10, .1, 500_000_000)}
	analysis, err := Analyze(input)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := BuildResearchSnapshot(input, analysis, time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC))
	for _, source := range snapshot.Sources {
		if source.ID != "f-financial" {
			continue
		}
		card := researchSourceForLevel(source, snapshot, researchLevelPolicy{DailyBars: 60})
		var payload struct {
			Data        map[string]any
			Definitions map[string]string
		}
		if err := json.Unmarshal([]byte(card.Content), &payload); err != nil {
			t.Fatal(err)
		}
		_, hasMargin := payload.Data["gross_margin"]
		if hasMargin || payload.Data["currency"] != "USD" || payload.Definitions["net_profit"] != "股东应占溢利" {
			t.Fatalf("quick research changed HK reporting basis: %s", card.Content)
		}
		return
	}
	t.Fatal("missing financial evidence")
}
