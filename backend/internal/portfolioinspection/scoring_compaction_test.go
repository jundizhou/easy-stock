package portfolioinspection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/stockanalysis"
)

type compactTestGateway struct {
	*scoreGateway
	prompts []string
	failure string
}

func (g *compactTestGateway) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	g.prompts = append(g.prompts, prompt)
	if strings.Contains(prompt, "[研究片段JSON]") {
		switch g.failure {
		case "provider":
			return agent.PromptResult{}, errors.New("provider failed")
		case "invalid":
			return agent.PromptResult{Content: `{}`}, nil
		case "invalid_once":
			if len(g.prompts) == 1 {
				return agent.PromptResult{Content: `{}`}, nil
			}
		case "oversized":
			data, _ := json.Marshal(map[string]string{"summary": strings.Repeat("风", compactionSummaryBytes)})
			return agent.PromptResult{Content: string(data)}, nil
		case "large_summaries":
			if !strings.Contains(prompt, `"fragment_is_summary":true`) {
				data, _ := json.Marshal(map[string]string{"summary": strings.Repeat("风", 1800)})
				return agent.PromptResult{Content: string(data)}, nil
			}
		case "budget_summaries":
			limit := compactionSummaryBytes
			if _, budget, ok := strings.Cut(prompt, "summary字符串的JSON编码不得超过"); ok {
				_, _ = fmt.Sscanf(budget, "%d字节", &limit)
			}
			data, _ := json.Marshal(map[string]string{"summary": strings.Repeat("风险须核验。", (min(limit, 3602)-2)/18)})
			return agent.PromptResult{Content: string(data)}, nil
		}
		return agent.PromptResult{Content: `{"summary":"已核验原报告片段；风险及来源待查原研究。"}`}, nil
	}
	return g.scoreGateway.Prompt(ctx, prompt)
}

func TestCompactionBudgetsSummariesAcrossTenHoldings(t *testing.T) {
	for _, n := range []int{6, 10} {
		t.Run(fmt.Sprintf("holdings_%d", n), func(t *testing.T) {
			req, results, metrics, rules, at := sourceRichResearchFixture(n)
			before, _ := json.Marshal(results)
			original := buildCompactScoringInput(req, results, metrics, rules, at)
			g := &compactTestGateway{scoreGateway: &scoreGateway{}, failure: "budget_summaries"}
			s := NewService(nil, g, nil, nil)
			defer s.Close()
			prompt, compressed, err := s.prepareScoringPrompt(context.Background(), req, results, metrics, rules, at, aggregationBudgetFor(time.Minute, ""))
			if err != nil || !compressed || len(prompt) > targetScoringPromptBytes {
				t.Fatalf("portfolio summary budget failed: bytes=%d compressed=%v err=%v", len(prompt), compressed, err)
			}
			_, payload, _ := strings.Cut(prompt, "[组合证据JSON]\n")
			var got compactScoringInput
			if err := json.Unmarshal([]byte(payload), &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Holdings) != n || !reflect.DeepEqual(got.Evidence, original.Evidence) || !reflect.DeepEqual(got.Actions, original.Actions) || !reflect.DeepEqual(got.Definitions, original.Definitions) {
				t.Fatal("compaction changed holdings or protected catalogs")
			}
			wantFacts, _ := json.Marshal(original.Facts)
			gotFacts, _ := json.Marshal(got.Facts)
			if string(wantFacts) != string(gotFacts) {
				t.Fatal("compaction changed numeric facts")
			}
			for i, card := range got.Holdings {
				if !reflect.DeepEqual(compactionCardSkeleton(card), compactionCardSkeleton(original.Holdings[i])) {
					t.Fatal("compaction changed conditions or execution constraints")
				}
			}
			for i, source := range got.Sources {
				want, _ := json.Marshal(sourceProvenance(original.Sources[i]))
				actual, _ := json.Marshal(sourceProvenance(source))
				if string(want) != string(actual) {
					t.Fatal("compaction changed source provenance")
				}
			}
			after, _ := json.Marshal(results)
			if string(before) != string(after) {
				t.Fatal("compaction mutated original reports")
			}
			_, budget, _ := strings.Cut(g.prompts[0], "summary字符串的JSON编码不得超过")
			var summaryBytes int
			_, _ = fmt.Sscanf(budget, "%d字节", &summaryBytes)
			t.Logf("holdings=%d prompt_bytes=%d summary_budget=%d calls=%d", n, len(prompt), summaryBytes, len(g.prompts))
		})
	}
}

func TestEightHoldingsFitWhenLegacyProtectedInputExceededBudget(t *testing.T) {
	req, results, _, rules, at := sourceRichResearchFixture(8)
	for i := range results {
		// Simulate the reported eight-report case with about 197 KiB of protected input.
		// Keep the trading prerequisite intact; only the wire encoding may shrink.
		rr := results[i].Analysis.ResearchReport
		rr.Decision.ExecutionPrerequisites = append(rr.Decision.ExecutionPrerequisites, strings.Repeat("交易状态及限制必须逐项核验。", 148))
	}
	metrics := CalculateMetrics(req, results, rules)
	instructions, original, _, err := buildScoringPromptParts(req, results, metrics, rules, at)
	if err != nil {
		t.Fatal(err)
	}
	minimum := original
	minimum.Holdings = make([]PortfolioAssetCard, len(original.Holdings))
	for i, card := range original.Holdings {
		minimum.Holdings[i] = compactionCardSkeleton(card)
	}
	minimum.Sources = make([]map[string]any, len(original.Sources))
	for i, source := range original.Sources {
		minimum.Sources[i] = sourceProvenance(source)
	}
	newMinimum, err := renderCompactScoringPrompt(instructions+compactionInstructions, minimum)
	if err != nil {
		t.Fatal(err)
	}
	type objectFact compactFact // Historical field-name encoding without MarshalJSON.
	legacyFacts := make(map[string]objectFact, len(minimum.Facts))
	for key, fact := range minimum.Facts {
		legacyFacts[key] = objectFact(fact)
	}
	legacyEvidence := make([]scoringEvidence, 0, len(minimum.Evidence))
	for id, ref := range minimum.Evidence {
		legacyEvidence = append(legacyEvidence, scoringEvidence{ID: id, Ref: ref})
	}
	newFactsJSON, _ := json.Marshal(minimum.Facts)
	legacyFactsJSON, _ := json.Marshal(legacyFacts)
	newEvidenceJSON, _ := json.Marshal(minimum.Evidence)
	legacyEvidenceJSON, _ := json.Marshal(legacyEvidence)
	legacyProtectedBytes := len(newMinimum) + len(legacyFactsJSON) - len(newFactsJSON) + len(legacyEvidenceJSON) - len(newEvidenceJSON)
	if legacyProtectedBytes <= targetScoringPromptBytes || len(newMinimum) >= targetScoringPromptBytes-len(original.Holdings)*(compactionMinSummaryBytes+len(`,"compacted_research":`)) {
		t.Fatalf("fixture misses the protected-input boundary: legacy=%d compact=%d", legacyProtectedBytes, len(newMinimum))
	}
	g := &compactTestGateway{scoreGateway: &scoreGateway{}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	prompt, compressed, err := s.prepareScoringPrompt(context.Background(), req, results, metrics, rules, at, aggregationBudgetFor(time.Minute, ""))
	if err != nil || !compressed || len(prompt) > targetScoringPromptBytes {
		t.Fatalf("eight-holding scoring input failed: bytes=%d compressed=%v err=%v", len(prompt), compressed, err)
	}
	_, payload, _ := strings.Cut(prompt, "[组合证据JSON]\n")
	var got compactScoringInput
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Evidence, original.Evidence) || !reflect.DeepEqual(got.Actions, original.Actions) || !reflect.DeepEqual(got.Definitions, original.Definitions) {
		t.Fatal("protected evidence or action catalog changed")
	}
	wantFacts, _ := json.Marshal(original.Facts)
	gotFacts, _ := json.Marshal(got.Facts)
	if string(wantFacts) != string(gotFacts) {
		t.Fatal("facts changed after compact encoding")
	}
	for i, card := range got.Holdings {
		if !reflect.DeepEqual(compactionCardSkeleton(card), compactionCardSkeleton(original.Holdings[i])) {
			t.Fatalf("holding %d lost trading constraints", i)
		}
	}
	for i, source := range got.Sources {
		want, _ := json.Marshal(sourceProvenance(original.Sources[i]))
		actual, _ := json.Marshal(sourceProvenance(source))
		if string(want) != string(actual) {
			t.Fatalf("source %d lost provenance", i)
		}
	}
	t.Logf("legacy_protected_bytes=%d compact_protected_bytes=%d prompt_bytes=%d", legacyProtectedBytes, len(newMinimum), len(prompt))
}

func sourceRichResearchFixture(n int) (Request, []HoldingResult, Metrics, ProfileRules, time.Time) {
	req, results, _, rules, at := oversizedResearchFixture(n)
	for i := range results {
		rr := results[i].Analysis.ResearchReport
		results[i].Holding.Weight = 90 / n
		req.Holdings[i] = results[i].Holding
		for j := 0; j < 24; j++ {
			id := fmt.Sprintf("source-%02d", j)
			rr.Sources = append(rr.Sources, stockanalysis.ResearchSource{ID: id, Kind: "announcement", Title: strings.Repeat("公司经营情况与风险提示", 12), Content: "须核验披露时点和原文。"})
			rr.Counter[0].SourceIDs = append(rr.Counter[0].SourceIDs, id)
		}
	}
	return req, results, CalculateMetrics(req, results, rules), rules, at
}

func TestLargeSummariesAreCompactedAgain(t *testing.T) {
	req, results, metrics, rules, at := oversizedResearchFixture(8)
	g := &compactTestGateway{scoreGateway: &scoreGateway{}, failure: "large_summaries"}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	prompt, compressed, err := s.prepareScoringPrompt(context.Background(), req, results, metrics, rules, at, aggregationBudgetFor(time.Minute, ""))
	if err != nil || !compressed || len(prompt) > maxScoringPromptBytes {
		t.Fatalf("multi-level compaction failed: %v", err)
	}
	merged := false
	for _, p := range g.prompts {
		merged = merged || strings.Contains(p, `"fragment_is_summary":true`)
	}
	if !merged {
		t.Fatal("large summaries were not compacted again")
	}
}

func oversizedResearchFixture(n int) (Request, []HoldingResult, Metrics, ProfileRules, time.Time) {
	req, results, _, _ := scoreFixture()
	for i := 1; i < n; i++ {
		data, _ := json.Marshal(results[0])
		var r HoldingResult
		_ = json.Unmarshal(data, &r)
		r.Holding.Symbol = fmt.Sprintf("600%03d.SH", i)
		r.Analysis.Symbol = r.Holding.Symbol
		r.AnalysisID = fmt.Sprintf("report-%d", i)
		req.Holdings = append(req.Holdings, r.Holding)
		results = append(results, r)
	}
	for i := range results {
		rr := results[i].Analysis.ResearchReport
		rr.Counter = []stockanalysis.ResearchClaim{{Text: strings.Repeat("风险资料。", 2500), SourceIDs: []string{"s1"}}}
		rr.Conditions = []stockanalysis.ResearchCondition{{ID: "c1", Text: "跌破时复核", Metric: "close"}}
		rr.InvalidationIDs = []string{"c1"}
		rr.Decision.ExecutionPrerequisites = []string{"核验交易状态"}
	}
	rules, _ := RulesFor(req.TraderProfile)
	return req, results, CalculateMetrics(req, results, rules), rules, time.Now().UTC()
}

func TestScoringCompactsEightOversizedHoldingsAndPreservesCatalogs(t *testing.T) {
	req, results, metrics, rules, at := oversizedResearchFixture(8)
	before, _ := json.Marshal(results)
	original := buildCompactScoringInput(req, results, metrics, rules, at)
	g := &compactTestGateway{scoreGateway: &scoreGateway{}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	prompt, compressed, err := s.prepareScoringPrompt(context.Background(), req, results, metrics, rules, at, aggregationBudgetFor(time.Minute, ""))
	if err != nil || !compressed || len(prompt) > maxScoringPromptBytes {
		t.Fatalf("compaction failed: %d %v %v", len(prompt), compressed, err)
	}
	if len(g.prompts) == 0 {
		t.Fatal("expected compaction calls")
	}
	_, payload, _ := strings.Cut(prompt, "[组合证据JSON]\n")
	var got compactScoringInput
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Evidence, original.Evidence) || !reflect.DeepEqual(got.Actions, original.Actions) {
		t.Fatal("evidence or action selectors changed")
	}
	compacted := 0
	for i, card := range got.Holdings {
		if card.CompactedResearch != "" {
			compacted++
		}
		if card.Symbol != results[i].Holding.Symbol || !reflect.DeepEqual(card.Conditions, original.Holdings[i].Conditions) || !reflect.DeepEqual(card.InvalidationIDs, original.Holdings[i].InvalidationIDs) || !reflect.DeepEqual(card.Prerequisites, original.Holdings[i].Prerequisites) {
			t.Fatalf("holding %d lost identity, conditions or prerequisites", i)
		}
	}
	if compacted == 0 || compacted == len(got.Holdings) {
		t.Fatalf("did not stop after fitting input: compacted %d holdings", compacted)
	}
	after, _ := json.Marshal(results)
	if string(before) != string(after) {
		t.Fatal("original research was modified")
	}
	for _, p := range g.prompts {
		if len(p) > maxScoringPromptBytes {
			t.Fatal("oversized compaction request")
		}
	}
}

func TestCompactionFailureDoesNotPublishScore(t *testing.T) {
	for _, mode := range []string{"provider", "invalid", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			req, results, metrics, rules, at := oversizedResearchFixture(8)
			g := &compactTestGateway{scoreGateway: &scoreGateway{}, failure: mode}
			s := NewService(nil, g, nil, nil)
			defer s.Close()
			report, err := s.scorePortfolio(context.Background(), req, results, metrics, rules, at)
			if err == nil || report.ScoreAvailable || g.calls.Load() != 0 {
				t.Fatalf("compaction failure published score: %+v %v", report, err)
			}
		})
	}
}

func TestCompactionScoreReportsSummaryLimitation(t *testing.T) {
	req, results, metrics, rules, at := oversizedResearchFixture(1)
	results[0].Analysis.ResearchReport.Counter[0].Text = strings.Repeat("必须检查风险。", 30000)
	g := &compactTestGateway{scoreGateway: &scoreGateway{content: scoreContent(t, scoreJSON(t))}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	report, err := s.scorePortfolio(context.Background(), req, results, metrics, rules, at)
	if err != nil || !report.ScoreAvailable || len(report.DataLimitations) == 0 || report.DataLimitations[0] != compactionNotice {
		t.Fatalf("missing summary limitation: %+v %v", report, err)
	}
}

type compactRepairGateway struct {
	*compactTestGateway
	valid, invalid string
	scorePrompts   []string
}

func (g *compactRepairGateway) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	if strings.Contains(prompt, "[研究片段JSON]") {
		return g.compactTestGateway.Prompt(ctx, prompt)
	}
	g.scorePrompts = append(g.scorePrompts, prompt)
	if len(g.scorePrompts) == 1 {
		return agent.PromptResult{Content: g.invalid}, nil
	}
	return agent.PromptResult{Content: fixtureRepairResponse(g.valid, prompt)}, nil
}

func TestCompactedEvidenceIsReusedByScoringRepair(t *testing.T) {
	req, results, metrics, rules, at := oversizedResearchFixture(1)
	results[0].Analysis.ResearchReport.Counter[0].Text = strings.Repeat("必须检查风险。", 30000)
	_, _, _, base := scoreFixture()
	base = fixtureContractReport(base, buildScoringCatalog(req, results, metrics, at))
	good, _ := json.Marshal(base)
	base.Holdings[0].ActionPriority = "优先复核"
	bad, _ := json.Marshal(base)
	g := &compactRepairGateway{compactTestGateway: &compactTestGateway{scoreGateway: &scoreGateway{}}, valid: string(good), invalid: string(bad)}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	report, err := s.scorePortfolio(context.Background(), req, results, metrics, rules, at)
	if err != nil || !report.ScoreAvailable || len(g.scorePrompts) != 2 {
		t.Fatalf("repair failed: %v, calls=%d", err, len(g.scorePrompts))
	}
	for _, prompt := range g.scorePrompts {
		if !strings.Contains(prompt, "compacted_research") || !strings.Contains(prompt, "已核验原报告片段") {
			t.Fatal("repair did not use frozen compacted research")
		}
		if len(prompt) > maxScoringPromptBytes {
			t.Fatalf("oversized score/repair prompt: %d", len(prompt))
		}
	}
	if len(report.DataLimitations) == 0 || report.DataLimitations[0] != compactionNotice {
		t.Fatal("summary limitation lost after repair")
	}
}

func TestRepairInputOverBudgetRestartsFromFrozenEvidence(t *testing.T) {
	req, results, metrics, base := scoreFixture()
	at := time.Now().UTC()
	rules, _ := RulesFor(req.TraderProfile)
	base = fixtureContractReport(base, buildScoringCatalog(req, results, metrics, at))
	good, _ := json.Marshal(base)
	base.Holdings[0].ActionPriority = "优先复核"
	base.ExecutiveSummary = strings.Repeat("研究", 50000)
	bad, _ := json.Marshal(base)
	g := &compactRepairGateway{compactTestGateway: &compactTestGateway{scoreGateway: &scoreGateway{}}, valid: string(good), invalid: string(bad)}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	report, err := s.scorePortfolio(context.Background(), req, results, metrics, rules, at)
	if err != nil || !report.ScoreAvailable || len(g.scorePrompts) != 2 {
		t.Fatalf("oversized repair did not recover: %v calls=%d", err, len(g.scorePrompts))
	}
	if len(g.scorePrompts[1]) > maxScoringPromptBytes || strings.Contains(g.scorePrompts[1], strings.Repeat("研究", 100)) || !strings.Contains(g.scorePrompts[1], "[校验反馈JSON]") {
		t.Fatal("fresh retry retained the oversized prior answer")
	}
}

func TestCompactionStopsAtCallLimit(t *testing.T) {
	req, results, metrics, rules, at := oversizedResearchFixture(1)
	results[0].Analysis.ResearchReport.Counter[0].Text = strings.Repeat("风", (compactionChunkBytes*(compactionMaxCalls+1))/3)
	g := &compactTestGateway{scoreGateway: &scoreGateway{}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	_, compressed, err := s.prepareScoringPrompt(context.Background(), req, results, metrics, rules, at, aggregationBudgetFor(time.Minute, ""))
	if err == nil || compressed || !strings.Contains(err.Error(), "调用上限") || len(g.prompts) != compactionMaxCalls {
		t.Fatalf("compaction did not respect call limit: %v, calls=%d", err, len(g.prompts))
	}
}

func TestCompactionKeepsSmallHoldingAndOriginalInputUnchanged(t *testing.T) {
	req, results, metrics, rules, at := oversizedResearchFixture(2)
	results[0].Analysis.ResearchReport.Counter[0].Text = strings.Repeat("经营风险。", 15000)
	results[1].Analysis.ResearchReport.Counter[0].Text = "短报告中的反证也必须保留"
	instructions, input, _, err := buildScoringPromptParts(req, results, metrics, rules, at)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(input)
	g := &compactTestGateway{scoreGateway: &scoreGateway{}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	got, err := s.compactScoringEvidence(context.Background(), input, aggregationBudgetFor(time.Minute, ""), instructions+compactionInstructions)
	if err != nil {
		t.Fatal(err)
	}
	if got.Holdings[0].CompactedResearch == "" || !reflect.DeepEqual(got.Holdings[1], input.Holdings[1]) {
		t.Fatal("small holding was unnecessarily summarized")
	}
	for i, source := range input.Sources {
		if source["report_id"] == input.Holdings[1].ReportID && !reflect.DeepEqual(source, got.Sources[i]) {
			t.Fatal("small holding source was changed")
		}
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("compaction mutated its caller's input")
	}
	wantFacts, _ := json.Marshal(input.Facts)
	gotFacts, _ := json.Marshal(got.Facts)
	if string(wantFacts) != string(gotFacts) || !reflect.DeepEqual(input.Definitions, got.Definitions) {
		t.Fatal("numeric facts or their definitions changed")
	}
}

func TestStagedScoringProcessesOversizedTradingConditions(t *testing.T) {
	req, results, metrics, rules, at := oversizedResearchFixture(1)
	input := buildCompactScoringInput(req, results, metrics, rules, at)
	input.Holdings[0].Conditions[0].Text = strings.Repeat("关键条件", 30000)
	before, _ := json.Marshal(input)
	g := &compactTestGateway{scoreGateway: &scoreGateway{}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	got, err := s.compactScoringEvidence(context.Background(), input, aggregationBudgetFor(time.Minute, ""), "评估规则")
	if err != nil || len(g.prompts) == 0 || got.Holdings[0].CompactedResearch == "" || len(got.StageCoverage) != 1 {
		t.Fatalf("staged processing failed: %v calls=%d", err, len(g.prompts))
	}
	prompt, err := renderCompactScoringPrompt("评估规则", got)
	if err != nil || len(prompt) > targetScoringPromptBytes || !reflect.DeepEqual(got.Actions, input.Actions) {
		t.Fatalf("staged portfolio view lost actions or exceeded budget: %v bytes=%d", err, len(prompt))
	}
	for _, modelPrompt := range g.prompts {
		if len(modelPrompt) > maxScoringPromptBytes {
			t.Fatal("oversized per-holding stage request")
		}
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("staging mutated the original report view")
	}
}

func TestStagedScoringKeepsTenHoldingCoverageAndBackendSelectors(t *testing.T) {
	req, results, _, rules, at := sourceRichResearchFixture(10)
	for i := range results {
		rr := results[i].Analysis.ResearchReport
		rr.Decision.ExecutionPrerequisites = append(rr.Decision.ExecutionPrerequisites, strings.Repeat("交易限制须核验。", 900))
	}
	metrics := CalculateMetrics(req, results, rules)
	before := buildCompactScoringInput(req, results, metrics, rules, at)
	g := &compactTestGateway{scoreGateway: &scoreGateway{}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	prompt, compressed, err := s.prepareScoringPrompt(context.Background(), req, results, metrics, rules, at, aggregationBudgetFor(time.Minute, ""))
	if err != nil || !compressed || len(prompt) > targetScoringPromptBytes {
		t.Fatalf("staged portfolio failed: bytes=%d compressed=%v err=%v", len(prompt), compressed, err)
	}
	_, payload, _ := strings.Cut(prompt, "[组合证据JSON]\n")
	var got compactScoringInput
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Holdings) != 10 || len(got.StageCoverage) != 10 || !reflect.DeepEqual(got.Actions, before.Actions) {
		t.Fatal("staged input lost holdings, coverage or backend actions")
	}
	for i, card := range got.Holdings {
		if card.CompactedResearch == "" || got.StageCoverage[i].SourcesRead != 25 || got.StageCoverage[i].SourceRefs != 25 || got.StageCoverage[i].FactsRead == 0 {
			t.Fatalf("holding %d was not fully staged: %+v", i, got.StageCoverage[i])
		}
	}
	for id, ref := range got.Evidence {
		if original, ok := before.Evidence[id]; !ok || original != ref {
			t.Fatalf("evidence selector %s no longer maps to its original source", id)
		}
	}
	if len(g.prompts) == 0 || len(g.prompts) > compactionMaxCalls {
		t.Fatalf("unexpected staged model call count: %d", len(g.prompts))
	}
}

func TestStagedScoreRetainsOriginalActionGuardAndDisclosesSummaryLimit(t *testing.T) {
	req, results, _, rules, at := oversizedResearchFixture(1)
	rr := results[0].Analysis.ResearchReport
	rr.Conditions[0].Text = strings.Repeat("关键失效条件须核验。", 12000)
	rr.Decision.ExecutionPrerequisites = []string{"核验实际交易状态与成交限制"}
	metrics := CalculateMetrics(req, results, rules)
	raw := scoreJSON(t)
	raw["confidence_level"] = "高"
	g := &compactTestGateway{scoreGateway: &scoreGateway{content: scoreContent(t, raw)}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	report, err := s.scorePortfolio(context.Background(), req, results, metrics, rules, at)
	if err != nil || !report.ScoreAvailable || len(report.DataLimitations) == 0 || report.DataLimitations[0] != compactionNotice {
		t.Fatalf("staged score missing validated output or limitation: %+v %v", report, err)
	}
	if len(report.Actions) == 0 || !reflect.DeepEqual(report.Actions[0].ExecutionPrerequisites, rr.Decision.ExecutionPrerequisites) {
		t.Fatal("staged score lost the original backend action guard")
	}
	if report.ConfidenceLevel != "中" || !strings.Contains(report.ConfidenceReason, "逐标的摘要") {
		t.Fatal("staged score overstated evidence confidence")
	}
}

func TestStagedScoringDistributesCitationBudgetAcrossHoldings(t *testing.T) {
	req, results, metrics, rules, at := sourceRichResearchFixture(2)
	input := buildCompactScoringInput(req, results, metrics, rules, at)
	first := input.Holdings[0].ReportID
	for i := 0; i < 4000; i++ {
		id := fmt.Sprintf("extra-%04d", i)
		input.Sources = append(input.Sources, map[string]any{"report_id": first, "id": id, "title": "资料", "kind": "announcement", "excerpt": "经营资料待核验"})
		input.Evidence["ev-"+id] = EvidenceRef{ReportID: first, SourceID: id}
	}
	g := &compactTestGateway{scoreGateway: &scoreGateway{}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	runner := &compactionRunner{service: s, ctx: context.Background(), budget: aggregationBudgetFor(time.Minute, "")}
	got, err := s.stageScoringEvidence(input, runner, "评估规则")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := renderCompactScoringPrompt("评估规则", got)
	if err != nil || len(prompt) > targetScoringPromptBytes || len(got.Evidence) >= len(input.Evidence) {
		t.Fatalf("citation budget was not bounded: bytes=%d err=%v", len(prompt), err)
	}
	for _, coverage := range got.StageCoverage {
		if coverage.SourceRefs == 0 || coverage.ChunksRead == 0 {
			t.Fatalf("holding lost source coverage: %+v", coverage)
		}
	}
	for id, ref := range got.Evidence {
		if original, ok := input.Evidence[id]; !ok || original != ref {
			t.Fatalf("unexpected citation: %s", id)
		}
	}
}

func TestStagedScoringRejectsUnboundedSharedFactsBeforeModel(t *testing.T) {
	req, results, metrics, rules, at := oversizedResearchFixture(1)
	input := buildCompactScoringInput(req, results, metrics, rules, at)
	input.Definitions["shared"] = strings.Repeat("无法分片的组合事实", 30000)
	input.Facts["global_unbounded"] = compactFact{Value: 1, Available: true, Method: "shared"}
	g := &compactTestGateway{scoreGateway: &scoreGateway{}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	_, err := s.compactScoringEvidence(context.Background(), input, aggregationBudgetFor(time.Minute, ""), "评估规则")
	if err == nil || len(g.prompts) != 0 || !strings.Contains(err.Error(), "共享事实与动作目录") {
		t.Fatalf("unbounded shared facts should stop before model: %v calls=%d", err, len(g.prompts))
	}
}

func TestResumeOversizedDraftCanRestartWithinExistingAttempt(t *testing.T) {
	req, results, metrics, base := scoreFixture()
	at := time.Now().UTC()
	rules, _ := RulesFor(req.TraderProfile)
	base.ExecutiveSummary = strings.Repeat("旧结果", 50000)
	old, _ := json.Marshal(base)
	g := &compactTestGateway{scoreGateway: &scoreGateway{content: scoreContent(t, scoreJSON(t))}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	report, err := s.scorePortfolioWithAudit(context.Background(), req, results, metrics, rules, at, &scoringAttempt{PromptVersion: PromptVersion, ContractVersion: scoringContractVersion, Response: string(old)}, nil)
	if err != nil || !report.ScoreAvailable || g.calls.Load() != 1 {
		t.Fatalf("resume stuck on oversized draft: %v", err)
	}
	if len(g.prompts[0]) > maxScoringPromptBytes || strings.Contains(g.prompts[0], strings.Repeat("旧结果", 100)) {
		t.Fatal("resume resent oversized draft")
	}
}

func TestInvalidSummaryGetsOnlyOneFormatRepair(t *testing.T) {
	for _, mode := range []string{"invalid_once", "invalid", "provider", "large_summaries"} {
		t.Run(mode, func(t *testing.T) {
			g := &compactTestGateway{scoreGateway: &scoreGateway{}, failure: mode}
			s := NewService(nil, g, nil, nil)
			defer s.Close()
			r := &compactionRunner{service: s, ctx: context.Background(), budget: aggregationBudgetFor(time.Minute, ""), summaryBytes: 1024}
			_, err := r.summarize(PortfolioAssetCard{Symbol: "600519.SH"}, map[string]any{"counter": "现金流恶化"})
			wantCalls := 2
			if mode == "provider" {
				wantCalls = 1
			}
			if len(g.prompts) != wantCalls || (err == nil) != (mode == "invalid_once") {
				t.Fatalf("wrong bounded repair behavior: %v calls=%d", err, len(g.prompts))
			}
		})
	}
}
