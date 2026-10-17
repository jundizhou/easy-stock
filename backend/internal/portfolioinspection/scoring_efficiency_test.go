package portfolioinspection

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/stockanalysis"
)

// Saved complete answers are test fixtures; adapt them to the new wire protocol.
// Production accepts only the requested patch, never a full replacement answer.
func fixtureRepairResponse(content, prompt string) string {
	_, payload, ok := strings.Cut(prompt, "[局部修复JSON]\n")
	if !ok {
		return content
	}
	var r scoringRepair
	if json.Unmarshal([]byte(payload), &r) != nil {
		return content
	}
	var answer map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &answer) != nil {
		return content
	}
	patch := map[string]json.RawMessage{}
	for _, key := range r.Allowed {
		v, ok := answer[key]
		if !ok || string(v) == "null" {
			if key == "actions" || key == "adjustment_order" {
				v = json.RawMessage(`[]`)
			} else {
				v = json.RawMessage(`""`)
			}
		}
		patch[key] = v
	}
	data, _ := json.Marshal(map[string]any{"base_digest": r.Digest, "replacements": patch})
	return string(data)
}

func TestLocalScoringRepairFreezesScoresAndRejectsForeignPatches(t *testing.T) {
	req, results, metrics, report, at := etfScoringFixture(12, 50)
	rules, _ := RulesFor(req.TraderProfile)
	base, err := buildScoringPromptAt(req, results, metrics, rules, at)
	if err != nil {
		t.Fatal(err)
	}
	report = fixtureContractReport(report, buildScoringCatalog(req, results, metrics, at))
	report.StyleMatch = "偏离"
	raw, _ := json.Marshal(report)
	_, issues := validateScoringOutput(string(raw), req, results, metrics, at)
	repair, prompt := makeScoringRepair(base, string(raw), issues)
	if repair == nil || !reflect.DeepEqual(repair.Allowed, []string{"style_match"}) {
		t.Fatal("repair escaped style field", repair, issues)
	}
	t.Logf("local_repair_bytes=%d initial_bytes=%d ratio=%.1f%%", len(prompt), len(base), 100*float64(len(prompt))/float64(len(base)))
	if len(prompt)*4 > len(base) {
		t.Fatalf("local repair too large: %d/%d", len(prompt), len(base))
	}
	for _, tc := range []string{"valid", "stale", "extra", "delete", "full"} {
		t.Run(tc, func(t *testing.T) {
			patch := map[string]any{"style_match": "明显偏离"}
			digest := repair.Digest
			if tc == "stale" {
				digest = "old"
			}
			if tc == "extra" {
				patch["dimensions"] = []any{}
			}
			if tc == "delete" {
				patch["style_match"] = nil
			}
			data, _ := json.Marshal(map[string]any{"base_digest": digest, "replacements": patch})
			if tc == "full" {
				data = raw
			}
			merged, err := repair.merge(string(data))
			if tc != "valid" {
				if err == nil {
					t.Fatal("unsafe patch accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var fixed AIReport
			_ = json.Unmarshal([]byte(merged), &fixed)
			if !reflect.DeepEqual(fixed.Dimensions, report.Dimensions) || !reflect.DeepEqual(fixed.RiskGroups, report.RiskGroups) {
				t.Fatal("frozen judgment changed")
			}
			validated, issues := validateScoringOutput(merged, req, results, metrics, at)
			if len(issues) > 0 || !validated.ScoreAvailable || validated.ETFScoring.PremiumPenalty <= 0 {
				t.Fatal("global checks or premium penalty lost", issues)
			}
		})
	}
	// Cross-block semantic edits must re-evaluate the dependent scores.
	r, _ := makeScoringRepair(base, string(raw), scoringIssues{{Code: "risk_group", Path: "risk_groups[0]", Message: "invalid group"}})
	if r == nil || !oneOf("dimensions", r.Allowed...) || !oneOf("risk_level", r.Allowed...) {
		t.Fatal("risk dependency not invalidated")
	}
}

func TestCompactInputFrozenCoverageMatrix(t *testing.T) {
	for _, premium := range []float64{.2, 1, 3, 5, 10, 12} {
		for _, size := range []int{2, 4, 10} {
			for _, stale := range []bool{false, true} {
				t.Run(fmt.Sprintf("premium_%g/size_%d/stale_%v", premium, size, stale), func(t *testing.T) {
					req, results, metrics, _, at := etfScoringFixture(premium, 50)
					for i := 2; i < size; i++ {
						copyRaw, _ := json.Marshal(results[0])
						var r HoldingResult
						_ = json.Unmarshal(copyRaw, &r)
						r.Holding.Symbol = fmt.Sprintf("600%03d.SH", i)
						r.Analysis.Symbol = r.Holding.Symbol
						r.AnalysisID = fmt.Sprintf("synthetic-%d", i)
						results = append(results, r)
						req.Holdings = append(req.Holdings, r.Holding)
					}
					for i := range results {
						rr := results[i].Analysis.ResearchReport
						for n := 0; n < 8; n++ {
							rr.Counter = append(rr.Counter, stockanalysis.ResearchClaim{Text: fmt.Sprintf("必须保留的反证%d", n), SourceIDs: []string{rr.Sources[0].ID}})
						}
						rr.Limitations = append(rr.Limitations, "事件源不可用，不能证明没有新事件")
						if stale {
							results[i].ResearchCutoffAt = at.Add(-25 * time.Hour)
						}
					}
					rules, _ := RulesFor(req.TraderProfile)
					before, _ := json.Marshal(results)
					input := buildCompactScoringInput(req, results, metrics, rules, at)
					if len(input.Holdings) != size {
						t.Fatal("holding lost")
					}
					for i, card := range input.Holdings {
						if !reflect.DeepEqual(card.Counter, results[i].Analysis.ResearchReport.Counter) || !reflect.DeepEqual(card.Conditions, results[i].Analysis.ResearchReport.Conditions) || !reflect.DeepEqual(card.Limitations, results[i].Analysis.ResearchReport.Limitations) {
							t.Fatal("mandatory evidence lost")
						}
					}
					facts := ScoringFacts(portfolioFactsAt(req, results, metrics, at))
					for k, f := range input.Facts {
						want := facts[k]
						if !reflect.DeepEqual(f.Value, want.Value) || f.Available != want.Available || input.Definitions[f.Method] != want.Method || input.Definitions[f.Limitation] != want.Limitation {
							t.Fatal("fact changed", k)
						}
					}
					after, _ := json.Marshal(results)
					if string(before) != string(after) {
						t.Fatal("original report mutated")
					}
				})
			}
		}
	}
}

func TestCompactPromptPrivateMeasurement(t *testing.T) {
	path := os.Getenv("EASY_STOCK_SCORING_EFFICIENCY_AUDIT")
	if path == "" {
		t.Skip("optional private offline fixture")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err = json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	rules, _ := RulesFor(j.Request.TraderProfile)
	metrics := metricsForReport(j.Request, j.Results, rules)
	old, err := buildLegacyScoringPromptAt(j.Request, j.Results, metrics, rules, j.AggregationStartedAt)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := buildScoringPromptAt(j.Request, j.Results, metrics, rules, j.AggregationStartedAt)
	if err != nil {
		t.Fatal(err)
	}
	_, payload, _ := strings.Cut(compact, "[组合证据JSON]\n")
	var parts map[string]json.RawMessage
	_ = json.Unmarshal([]byte(payload), &parts)
	for _, k := range []string{"holdings", "sources", "facts", "definitions", "evidence_catalog", "action_catalog"} {
		t.Logf("compact_%s_bytes=%d", k, len(parts[k]))
	}
	var cards []map[string]json.RawMessage
	_ = json.Unmarshal(parts["holdings"], &cards)
	for i, card := range cards {
		for _, k := range []string{"counter", "trading_logic", "etf_judgments", "conditions", "main_conflict"} {
			t.Logf("card_%d_%s_bytes=%d", i, k, len(card[k]))
		}
	}
	t.Logf("frozen_holdings=%d legacy_bytes=%d compact_bytes=%d reduction=%.1f%%", len(j.Results), len(old), len(compact), 100*(1-float64(len(compact))/float64(len(old))))
	if len(compact) >= len(old) {
		t.Fatal("compact input failed to reduce duplication")
	}
}

func TestScoringPatchUsesOnlyTwoModelCalls(t *testing.T) {
	req, results, metrics, report, at := etfScoringFixture(5, 50)
	rules, _ := RulesFor(req.TraderProfile)
	report = fixtureContractReport(report, buildScoringCatalog(req, results, metrics, at))
	report.StyleMatch = "偏离"
	bad, _ := json.Marshal(report)
	report.StyleMatch = "明显偏离"
	good, _ := json.Marshal(report)
	g := &scoringReplayGateway{responses: []json.RawMessage{bad, good}}
	s := NewService(nil, g, nil, nil)
	got, err := s.scorePortfolio(context.Background(), req, results, metrics, rules, at)
	if err != nil || !got.ScoreAvailable || g.calls.Load() != 2 {
		t.Fatal(err, g.calls.Load())
	}
}

func TestCompactSelectorsRestoreCanonicalEvidenceAndActions(t *testing.T) {
	req, results, metrics, base, at := etfScoringFixture(5, 50)
	catalog := buildScoringCatalog(req, results, metrics, at)
	r := fixtureContractReport(base, compactCatalog(catalog))
	data, _ := json.Marshal(r)
	validated, issues := validateScoringOutput(string(data), req, results, metrics, at)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	actions := map[string]bool{}
	for _, a := range catalog.Actions {
		actions[a.ID] = true
	}
	for _, h := range validated.Holdings {
		if !actions[h.ActionID] {
			t.Fatal("short action alias leaked into report", h.ActionID)
		}
	}
	for _, id := range validated.AdjustmentActionIDs {
		if !actions[id] {
			t.Fatal("order alias leaked")
		}
	}
	for _, s := range validated.Scenarios {
		for _, id := range s.ActionIDs {
			if !actions[id] {
				t.Fatal("scenario alias leaked")
			}
		}
	}
	walkScoringRefs(&validated, func(_ string, ref *EvidenceRef) {
		if ref.EvidenceID != "" || ref.Fact == "" && ref.SourceID == "" {
			t.Fatal("reference was not expanded", ref)
		}
	})
}
