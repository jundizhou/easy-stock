package portfolioinspection

import (
	"encoding/json"
	"testing"
	"time"

	"easy-stock/backend/internal/stockanalysis"
)

func TestInvestmentFinancialFactsUseOnlyAvailableFrozenSourceFields(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		mutate      func(map[string]any, map[string]any, *stockanalysis.ResearchSource)
		available   bool
		value       any
	}{
		{"real revenue", "revenue", nil, true, float64(1000)},
		{"missing not zero", "eps", nil, false, nil},
		{"serialized zero unknown", "roe", nil, false, nil},
		{"explicit zero", "roe", func(d, p map[string]any, s *stockanalysis.ResearchSource) {
			d["meta"] = map[string]any{"source": "test", "available_fields": []string{"roe"}}
		}, true, float64(0)},
		{"deducted actual zero", "deducted_net_profit", func(d, p map[string]any, s *stockanalysis.ResearchSource) { d["deducted_net_profit"] = 0 }, true, float64(0)},
		{"deducted missing flag", "deducted_net_profit", func(d, p map[string]any, s *stockanalysis.ResearchSource) { d["deducted_net_profit_available"] = false }, false, nil},
		{"deducted zero growth unknown", "deducted_net_profit_yoy", func(d, p map[string]any, s *stockanalysis.ResearchSource) { d["deducted_net_profit_yoy"] = 0 }, false, nil},
		{"conflicting field", "revenue", func(d, p map[string]any, s *stockanalysis.ResearchSource) {
			p["cross_checks"] = []any{map[string]any{"report_date": "2026-06-30 00:00:00", "conflicting_fields": []string{"revenue"}}}
		}, false, nil},
		{"future disclosure", "revenue", func(d, p map[string]any, s *stockanalysis.ResearchSource) {
			s.PublishedAt = s.CapturedAt.Add(time.Hour)
		}, false, nil},
		{"future period", "report_date", func(d, p map[string]any, s *stockanalysis.ResearchSource) { d["report_date"] = "2026-12-31" }, false, nil},
		{"old period", "report_date", func(d, p map[string]any, s *stockanalysis.ResearchSource) { d["report_date"] = "2024-12-31" }, false, nil},
		{"stale source", "revenue", func(d, p map[string]any, s *stockanalysis.ResearchSource) { d["meta"] = map[string]any{"stale": true} }, false, nil},
		{"null", "net_profit", func(d, p map[string]any, s *stockanalysis.ResearchSource) { d["net_profit"] = nil }, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, rs, _, _ := scoreFixture()
			r := rs[0]
			r.Analysis.Fundamental = &stockanalysis.FundamentalAnalysis{Summary: "营收虚构99999", Revenue: 99999}
			cutoff := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
			r.Analysis.ResearchReport.CutoffAt = cutoff
			d := map[string]any{"report_date": "2026-06-30 00:00:00", "revenue": 1000, "net_profit": 100, "deducted_net_profit": 80, "deducted_net_profit_available": true, "roe": 0, "meta": map[string]any{"source": "test"}}
			p := map[string]any{"data": d}
			src := stockanalysis.ResearchSource{ID: "f-financial", CapturedAt: cutoff, PublishedAt: cutoff.Add(-time.Hour)}
			if tc.mutate != nil {
				tc.mutate(d, p, &src)
			}
			b, _ := json.Marshal(p)
			src.Content = string(b)
			r.Analysis.ResearchReport.Sources = append(r.Analysis.ResearchReport.Sources, src)
			facts := InvestmentFinancialFacts(r)
			f, ok := facts[r.Holding.Symbol+".financial."+tc.field]
			if !ok || f.Available != tc.available || f.Value != tc.value {
				t.Fatalf("%s: %+v", tc.field, f)
			}
			if tc.name == "conflicting field" && !facts[r.Holding.Symbol+".financial.net_profit"].Available {
				t.Fatal("unrelated facts suppressed")
			}
		})
	}
}
