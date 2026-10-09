package portfolioinspection

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNamedDimensionsAreLosslessAndRejectAmbiguousKeys(t *testing.T) {
	req, results, metrics, _ := scoreFixture()
	raw := scoreJSON(t)
	array := raw["dimensions"].([]any)
	keyed := map[string]any{}
	for _, value := range array {
		d := value.(map[string]any)
		key := d["key"].(string)
		delete(d, "key")
		keyed[key] = d
	}
	raw["dimensions"] = keyed
	decoded, err := decodeScoringReport(scoreContent(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateScoringReport(&decoded, req, results, metrics); err != nil {
		t.Fatal(err)
	}
	expected, err := decodeScoringReport(scoreContent(t, scoreJSON(t)))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateScoringReport(&expected, req, results, metrics); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Dimensions, expected.Dimensions) || *decoded.TotalScore != *expected.TotalScore {
		t.Fatal("container normalization changed scores or references", decoded.Dimensions)
	}
	for _, mode := range []string{"missing", "unknown", "conflicting"} {
		t.Run(mode, func(t *testing.T) {
			var changed map[string]any
			_ = json.Unmarshal([]byte(scoreContent(t, raw)), &changed)
			dims := changed["dimensions"].(map[string]any)
			switch mode {
			case "missing":
				delete(dims, "holding_logic")
			case "unknown":
				dims["unknown"] = dims["holding_logic"]
				delete(dims, "holding_logic")
			case "conflicting":
				dims["holding_logic"].(map[string]any)["key"] = "risk_capacity"
			}
			if _, err := decodeScoringReport(scoreContent(t, changed)); err == nil {
				t.Fatal("ambiguous dimension keys accepted")
			}
		})
	}
}

func scoreJSON(t *testing.T) map[string]any {
	t.Helper()
	_, _, _, report := scoreFixture()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func scoreContent(t *testing.T, raw map[string]any) string {
	t.Helper()
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestScoringDecodesStructuredExplanationListsAndPreservesEvidence(t *testing.T) {
	req, results, metrics, _ := scoreFixture()
	raw := scoreJSON(t)
	for _, list := range explanationListLimits {
		raw[list.field] = []any{"纯文字", map[string]any{"title": "集中风险", "reason": "共同驱动尚待确认", "symbols": []string{"600519.SH"}, "evidence_refs": []EvidenceRef{{ReportID: "report1", SourceID: "s1"}}}}
	}
	raw["dimensions"].([]any)[0].(map[string]any)["limitations"] = []any{map[string]any{"text": "仍需核验", "evidence_refs": []EvidenceRef{{Fact: "cash_percent"}}}}
	report, err := decodeScoringReport("```json\n" + scoreContent(t, raw) + "\n```")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateScoringReport(&report, req, results, metrics); err != nil {
		t.Fatal(err)
	}
	if report.PrimaryRisks[1] != "集中风险；共同驱动尚待确认" || len(report.ExplanationDetails) != 6 || report.ExplanationDetails["primary_risks[1]"].EvidenceRefs[0].SourceID != "s1" || !report.ScoreAvailable {
		t.Fatalf("structured text or evidence lost: %+v", report)
	}
	// The canonical API and database shape remains readable without the model decoder.
	data, _ := json.Marshal(report)
	var stored AIReport
	if err := json.Unmarshal(data, &stored); err != nil || len(stored.ExplanationDetails) != 6 || stored.PrimaryRisks[1] != report.PrimaryRisks[1] {
		t.Fatalf("canonical round trip failed: %v %+v", err, stored)
	}
}

func TestScoringRejectsInvalidExplanationShapes(t *testing.T) {
	for name, value := range map[string]any{
		"no_text":           map[string]any{"evidence_refs": []EvidenceRef{{Fact: "cash_percent"}}},
		"empty":             map[string]any{"reason": " "},
		"number":            42,
		"null":              nil,
		"nested_text":       map[string]any{"text": map[string]any{"reason": "说明"}},
		"unknown_evidence":  map[string]any{"text": "说明", "sources": []string{"invented"}},
		"unknown_ref_field": map[string]any{"text": "说明", "evidence_refs": []any{map[string]any{"source_ids": []string{"s1"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			raw := scoreJSON(t)
			raw["primary_risks"] = []any{value}
			if _, err := decodeScoringReport(scoreContent(t, raw)); err == nil {
				t.Fatal("invalid object accepted")
			}
		})
	}
	raw := scoreJSON(t)
	raw["primary_risks"] = map[string]any{"text": "不是数组"}
	if _, err := decodeScoringReport(scoreContent(t, raw)); err == nil {
		t.Fatal("non-list explanation accepted")
	}
	raw = scoreJSON(t)
	raw["dimensions"].([]any)[0].(map[string]any)["score"] = "70"
	if _, err := decodeScoringReport(scoreContent(t, raw)); err == nil {
		t.Fatal("string score accepted")
	}
}

func TestSingleNarrativeLimitPreservesTextAndScore(t *testing.T) {
	req, results, metrics, _ := scoreFixture()
	raw := scoreJSON(t)
	raw["data_limitations"] = "原始资料限制"
	raw["dimensions"].([]any)[3].(map[string]any)["limitations"] = "现金约束仍在"
	r, err := decodeScoringReport(scoreContent(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if err = validateScoringReport(&r, req, results, metrics); err != nil {
		t.Fatal(err)
	}
	if len(r.Dimensions[3].Limitations) != 1 || r.Dimensions[3].Limitations[0] != "现金约束仍在" || r.DataLimitations[0] != "原始资料限制" || *r.TotalScore != 71 {
		t.Fatal("single narrative normalization changed content", r)
	}
}

func TestStructuredExplanationReferencesRemainStrict(t *testing.T) {
	req, results, metrics, _ := scoreFixture()
	for name, detail := range map[string]map[string]any{
		"invented_source": {"evidence_refs": []EvidenceRef{{ReportID: "report1", SourceID: "invented"}}},
		"unknown_fact":    {"evidence_refs": []EvidenceRef{{Fact: "known_stop_loss_risk_percent"}}},
		"foreign_stock":   {"symbols": []string{"000001.SZ"}},
		"mixed_ref":       {"evidence_refs": []EvidenceRef{{Fact: "cash_percent", ReportID: "report1", SourceID: "s1"}}},
	} {
		t.Run(name, func(t *testing.T) {
			for _, field := range []string{"primary_risks", "concentration_findings", "adjustment_order", "next_checklist", "data_limitations", "dimension_limitations"} {
				raw := scoreJSON(t)
				detail["text"] = "需要核验"
				if field == "dimension_limitations" {
					raw["dimensions"].([]any)[0].(map[string]any)["limitations"] = []any{detail}
				} else {
					raw[field] = []any{detail}
				}
				report, err := decodeScoringReport(scoreContent(t, raw))
				if err != nil {
					t.Fatal(err)
				}
				if err := validateScoringReport(&report, req, results, metrics); err == nil {
					t.Fatalf("invalid %s accepted in %s", name, field)
				}
			}
		})
	}
	raw := scoreJSON(t)
	raw["primary_risks"] = []any{map[string]any{"text": "说明", "evidence_refs": []EvidenceRef{{Fact: "cash_percent"}}}}
	raw["explanation_details"] = map[string]any{"primary_risks[0]": ExplanationDetail{EvidenceRefs: []EvidenceRef{{Fact: "invented"}}}}
	report, err := decodeScoringReport(scoreContent(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateScoringReport(&report, req, results, metrics); err == nil {
		t.Fatal("structured item overwrote invalid existing reference")
	}
	raw = scoreJSON(t)
	raw["explanation_details"] = map[string]any{"primary_risks[99]": ExplanationDetail{EvidenceRefs: []EvidenceRef{{Fact: "cash_percent"}}}}
	report, _ = decodeScoringReport(scoreContent(t, raw))
	if err := validateScoringReport(&report, req, results, metrics); err == nil {
		t.Fatal("nonexistent explanation path accepted")
	}
}

func TestResumeFourCompletedStocksWithStructuredRisksOnlyCallsAggregation(t *testing.T) {
	req, results, _, report := scoreFixture()
	base := results[0]
	conclusion := report.Holdings[0]
	req.Holdings, results, report.Holdings = nil, nil, nil
	for _, symbol := range []string{"301536.SZ", "688220.SH", "300209.SZ", "688512.SH"} {
		h := Holding{Symbol: symbol, Weight: 20}
		r := base
		r.Holding = h
		a := *base.Analysis
		a.Symbol, a.AnalysisID = symbol, "report-"+symbol
		r.Analysis, r.AnalysisID = &a, a.AnalysisID
		req.Holdings = append(req.Holdings, h)
		results = append(results, r)
		c := conclusion
		c.Symbol = symbol
		report.Holdings = append(report.Holdings, c)
	}
	rawData, _ := json.Marshal(report)
	var raw map[string]any
	_ = json.Unmarshal(rawData, &raw)
	raw["primary_risks"] = []any{map[string]any{"title": "主线集中", "reason": "共同驱动需核验", "evidence_refs": []EvidenceRef{{ReportID: results[0].AnalysisID, SourceID: "s1"}}}}
	gateway := &scoreGateway{content: scoreContent(t, raw)}
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	seed := Job{ID: "failed-format-job", Status: "partial", Request: req, Results: results, TotalStocks: 4, CompletedStocks: 4, CompletedAt: time.Now()}
	if _, err := store.Save(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, gateway, nil, nil)
	defer service.Close()
	var researchCalls atomic.Int32
	service.ConfigureResearch(func(context.Context, Holding, Request, time.Time, bool, string, func(HoldingResult)) (HoldingResult, error) {
		researchCalls.Add(1)
		return HoldingResult{}, nil
	}, nil)
	job, err := service.Resume(context.Background(), seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := waitPortfolio(t, service, job.ID)
	if done.Status != "succeeded" || researchCalls.Load() != 0 || gateway.calls.Load() != 1 || !done.Report.Conclusion.ScoreAvailable || len(done.Report.Conclusion.ExplanationDetails) != 1 || done.ReusedStocks != 4 || done.NewStocks != 0 {
		t.Fatalf("resume failed or repeated stocks: %+v research=%d aggregation=%d", done, researchCalls.Load(), gateway.calls.Load())
	}
}

func TestInvalidScoringKeepsDiagnosticOutOfProductSummary(t *testing.T) {
	req, results, metrics, _ := scoreFixture()
	raw := scoreJSON(t)
	raw["primary_risks"] = []any{map[string]any{"unexpected": true}}
	var diagnostics bytes.Buffer
	store, storeErr := OpenStore("")
	if storeErr != nil {
		t.Fatal(storeErr)
	}
	defer store.Close()
	service := NewService(store, &scoreGateway{content: scoreContent(t, raw)}, nil, log.New(&diagnostics, "", 0))
	rules, _ := RulesFor(req.TraderProfile)
	_, err := service.scorePortfolio(context.Background(), req, results, metrics, rules)
	if err == nil || !strings.Contains(err.Error(), "请重试组合评估") || strings.Contains(err.Error(), "unexpected") || !strings.Contains(diagnostics.String(), "unexpected") {
		t.Fatalf("bad diagnostic boundary: %v %s", err, diagnostics.String())
	}
	job := Job{ID: "failed-validation", Request: req, Results: results}
	service.finishPartial(&job, err)
	if strings.Contains(job.Report.Conclusion.ExecutiveSummary, "校验") || !strings.Contains(job.Report.Conclusion.ExecutiveSummary, "已有报告已保留") {
		t.Fatalf("duplicated technical error in summary: %+v", job.Report.Conclusion)
	}
}
