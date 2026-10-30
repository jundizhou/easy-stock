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
		}
		return agent.PromptResult{Content: `{"summary":"已核验原报告片段；风险及来源待查原研究。"}`}, nil
	}
	return g.scoreGateway.Prompt(ctx, prompt)
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

func TestCompactionPreflightStopsBeforeModelWhenProtectedDataCannotFit(t *testing.T) {
	req, results, metrics, rules, at := oversizedResearchFixture(1)
	input := buildCompactScoringInput(req, results, metrics, rules, at)
	input.Holdings[0].Conditions[0].Text = strings.Repeat("关键条件", 30000)
	g := &compactTestGateway{scoreGateway: &scoreGateway{}}
	s := NewService(nil, g, nil, nil)
	defer s.Close()
	_, err := s.compactScoringEvidence(context.Background(), input, aggregationBudgetFor(time.Minute, ""), "评估规则")
	if err == nil || len(g.prompts) != 0 || !strings.Contains(err.Error(), "无法仅靠摘要压缩") {
		t.Fatalf("unnecessary calls before protected-data failure: %v %d", err, len(g.prompts))
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
	for _, mode := range []string{"invalid_once", "invalid", "provider"} {
		t.Run(mode, func(t *testing.T) {
			g := &compactTestGateway{scoreGateway: &scoreGateway{}, failure: mode}
			s := NewService(nil, g, nil, nil)
			defer s.Close()
			r := &compactionRunner{service: s, ctx: context.Background(), budget: aggregationBudgetFor(time.Minute, "")}
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
