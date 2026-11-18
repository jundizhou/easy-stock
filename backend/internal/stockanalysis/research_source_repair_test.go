package stockanalysis

import (
	"strings"
	"testing"

	"easy-stock/backend/internal/etfanalysis"
	"easy-stock/backend/internal/foundation"
)

func TestETFJudgmentSourceIDsRepairAnchorsAndNearMisses(t *testing.T) {
	snapshot := ResearchSnapshot{
		Sources: []ResearchSource{
			{ID: "e-profile"},
			{ID: "e-direction"},
			{ID: "e-direction-technical"},
			{ID: "e-direction-news-coverage"},
			{ID: "m-price"},
			{ID: "m-quote"},
			{ID: "e-notice-AN1-fulltext"},
			{ID: "e-notice-AN2-excerpt"},
			{ID: "s-abc"},
		},
		Anchors: []PriceAnchor{
			{ID: "etf-close", SourceID: "m-price"},
			{ID: "etf-low", SourceID: "m-price"},
		},
		ETF: &etfanalysis.Analysis{
			Profile:   foundation.ETFProfile{AssetClass: "equity"},
			Direction: &etfanalysis.DirectionAnalysis{Technical: &etfanalysis.DirectionTechnical{}},
		},
	}
	judgments := &etfanalysis.Judgments{
		Technical:  &etfanalysis.Judgment{Status: "mixed", Text: "技术判断", EvidenceLevel: "limited", SourceIDs: []string{"e-direction-technical", "etf-close"}},
		News:       &etfanalysis.Judgment{Status: "insufficient", Text: "消息依据不足", EvidenceLevel: "limited", SourceIDs: []string{"e-direction-news-coverage", "e-notice-AN1-excerpt"}},
		Underlying: etfanalysis.Judgment{Status: "mixed", Text: "底层判断", EvidenceLevel: "limited", SourceIDs: []string{"e-direction-technical", "anchors"}},
		Product:    etfanalysis.Judgment{Status: "limited", Text: "产品判断", EvidenceLevel: "limited", SourceIDs: []string{"e-notice-AN2-fulltext"}},
		Entry:      etfanalysis.Judgment{Status: "wait", Text: "等待条件", EvidenceLevel: "limited", SourceIDs: []string{"m-price", "s-abd"}},
	}
	if err := validateETFJudgments(judgments, snapshot); err != nil {
		t.Fatalf("repaired judgments still rejected: %v", err)
	}
	want := map[string][]string{
		"technical":  {"e-direction-technical", "m-price"},
		"news":       {"e-direction-news-coverage", "e-notice-AN1-fulltext"},
		"underlying": {"e-direction-technical", "m-price"},
		"product":    {"e-notice-AN2-excerpt"},
		"entry":      {"m-price", "s-abc"},
	}
	got := map[string][]string{
		"technical":  judgments.Technical.SourceIDs,
		"news":       judgments.News.SourceIDs,
		"underlying": judgments.Underlying.SourceIDs,
		"product":    judgments.Product.SourceIDs,
		"entry":      judgments.Entry.SourceIDs,
	}
	for key, ids := range want {
		if strings.Join(got[key], ",") != strings.Join(ids, ",") {
			t.Fatalf("%s ids = %v, want %v", key, got[key], ids)
		}
	}
}

func TestETFJudgmentSourceIDsKeepInventedIDsRejected(t *testing.T) {
	snapshot := ResearchSnapshot{
		Sources: []ResearchSource{
			{ID: "e-direction-technical"},
			{ID: "e-direction-news-coverage"},
			{ID: "m-price"},
			{ID: "e-notice-AN3-excerpt"},
		},
		Anchors: []PriceAnchor{{ID: "etf-close", SourceID: "m-price"}},
		ETF: &etfanalysis.Analysis{
			Profile:   foundation.ETFProfile{AssetClass: "equity"},
			Direction: &etfanalysis.DirectionAnalysis{Technical: &etfanalysis.DirectionTechnical{}},
		},
	}
	judgments := &etfanalysis.Judgments{
		Technical:  &etfanalysis.Judgment{Status: "mixed", Text: "技术判断", EvidenceLevel: "limited", SourceIDs: []string{"e-direction-technical", "etf-unknown"}},
		News:       &etfanalysis.Judgment{Status: "insufficient", Text: "消息依据不足", EvidenceLevel: "limited", SourceIDs: []string{"e-direction-news-coverage"}},
		Underlying: etfanalysis.Judgment{Status: "mixed", Text: "底层判断", EvidenceLevel: "limited", SourceIDs: []string{"e-direction-technical"}},
		Product:    etfanalysis.Judgment{Status: "limited", Text: "产品判断", EvidenceLevel: "limited", SourceIDs: []string{"e-notice-AN3-excerpt"}},
		Entry:      etfanalysis.Judgment{Status: "wait", Text: "等待条件", EvidenceLevel: "limited", SourceIDs: []string{"m-price"}},
	}
	err := validateETFJudgments(judgments, snapshot)
	if err == nil || !strings.Contains(err.Error(), "etf-unknown") {
		t.Fatalf("invented id was not rejected: %v", err)
	}
}

func TestETFJudgmentSourceIDsKeepAmbiguousNoticeSuffixRejected(t *testing.T) {
	snapshot := ResearchSnapshot{
		Sources: []ResearchSource{
			{ID: "e-direction-technical"},
			{ID: "e-direction-news-coverage"},
			{ID: "m-price"},
			{ID: "e-notice-AN3-fulltext"},
			{ID: "e-notice-AN3-excerpt"},
		},
		Anchors: []PriceAnchor{{ID: "etf-close", SourceID: "m-price"}},
		ETF: &etfanalysis.Analysis{
			Profile:   foundation.ETFProfile{AssetClass: "equity"},
			Direction: &etfanalysis.DirectionAnalysis{Technical: &etfanalysis.DirectionTechnical{}},
		},
	}
	judgments := &etfanalysis.Judgments{
		Technical:  &etfanalysis.Judgment{Status: "mixed", Text: "技术判断", EvidenceLevel: "limited", SourceIDs: []string{"e-direction-technical"}},
		News:       &etfanalysis.Judgment{Status: "insufficient", Text: "消息依据不足", EvidenceLevel: "limited", SourceIDs: []string{"e-direction-news-coverage"}},
		Underlying: etfanalysis.Judgment{Status: "mixed", Text: "底层判断", EvidenceLevel: "limited", SourceIDs: []string{"e-direction-technical"}},
		Product:    etfanalysis.Judgment{Status: "limited", Text: "产品判断", EvidenceLevel: "limited", SourceIDs: []string{"e-notice-AN3-title_only"}},
		Entry:      etfanalysis.Judgment{Status: "wait", Text: "等待条件", EvidenceLevel: "limited", SourceIDs: []string{"m-price"}},
	}
	err := validateETFJudgments(judgments, snapshot)
	if err == nil || !strings.Contains(err.Error(), "e-notice-AN3-title_only") {
		t.Fatalf("ambiguous notice suffix was guessed: %v", err)
	}
}

func TestClaimSourceRepairAlsoHandlesAnchorsAndNoticeSuffixes(t *testing.T) {
	sources := map[string]ResearchSource{
		"m-price":               {},
		"e-notice-AN1-fulltext": {},
		"s-abc":                 {},
	}
	result := &ResearchSynthesis{
		Thesis:  ResearchClaim{SourceIDs: []string{"anchors", "e-notice-AN1-excerpt", "s-abd"}},
		Support: []ResearchClaim{{SourceIDs: []string{"etf-close"}}},
	}
	notes := repairResearchSourceIDs(result, sources, []PriceAnchor{{ID: "etf-close", SourceID: "m-price"}})
	want := []string{"m-price", "e-notice-AN1-fulltext", "s-abc"}
	if strings.Join(result.Thesis.SourceIDs, ",") != strings.Join(want, ",") {
		t.Fatalf("thesis ids = %v, want %v", result.Thesis.SourceIDs, want)
	}
	if len(result.Support[0].SourceIDs) != 1 || result.Support[0].SourceIDs[0] != "m-price" {
		t.Fatalf("support ids = %v", result.Support[0].SourceIDs)
	}
	if len(notes) != 4 {
		t.Fatalf("repair notes = %v", notes)
	}
}
