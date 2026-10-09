package portfolioinspection

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/stockanalysis"
)

func scoreFixture() (Request, []HoldingResult, Metrics, AIReport) {
	req := Request{TraderProfile: ProfileBalanced, Horizon: "swing", ResearchLevel: stockanalysis.ResearchLevelStandard, Holdings: []Holding{{Symbol: "600519.SH", Weight: 60}}}
	a := stockanalysis.Analysis{Symbol: "600519.SH", AnalysisID: "report1", AI: stockanalysis.AISynthesisStatus{Status: "ready"}, ResearchReport: &stockanalysis.ResearchReport{ResearchSynthesis: stockanalysis.ResearchSynthesis{Headline: "测试", Thesis: stockanalysis.ResearchClaim{Text: "逻辑", SourceIDs: []string{"s1"}}}, Validation: "references_checked", Sources: []stockanalysis.ResearchSource{{ID: "s1", Title: "测试公告"}}}}
	results := []HoldingResult{{Holding: req.Holdings[0], Status: "succeeded", Analysis: &a, AnalysisID: "report1", ResearchOrigin: "reused"}}
	rules, _ := RulesFor(req.TraderProfile)
	metrics := CalculateMetrics(req, results, rules)
	report := AIReport{RiskLevel: "中", RiskReason: "仓位较集中", StyleMatch: "部分偏离", ExecutiveSummary: "按条件管理持仓", ConfidenceLevel: "中", ConfidenceReason: "部分条件待验证", Holdings: []HoldingConclusion{{Symbol: "600519.SH", Conclusion: "保持观察", ActionPriority: "观察", Action: "确认前观察", Confirmation: "趋势延续", Invalidation: "趋势破坏"}}, Scenarios: []Scenario{{Name: "震荡分化", Condition: "趋势走弱", PortfolioAction: "复核持有逻辑"}}}
	for i, r := range scoreRubric {
		score := 70 + i
		report.Dimensions = append(report.Dimensions, ScoreDimension{Key: r.Key, Score: &score, Reason: "证据支持的有限判断", EvidenceRefs: []EvidenceRef{{Fact: "concentration_hhi"}}})
	}
	return req, results, metrics, report
}
func TestScoringWithoutStaticStopsAndChecksReferences(t *testing.T) {
	req, results, metrics, report := scoreFixture()
	if metrics.StopLossCoveragePercent != 0 {
		t.Fatal("fixture stop")
	}
	if err := validateScoringReport(&report, req, results, metrics); err != nil {
		t.Fatal(err)
	}
	if !report.ScoreAvailable || *report.TotalScore != 71 || len(report.Dimensions) != 4 {
		t.Fatalf("AI score incorrectly gated %+v", report)
	}
	report.Dimensions[0].EvidenceRefs = []EvidenceRef{{ReportID: "report1", SourceID: "invented"}}
	if err := validateScoringReport(&report, req, results, metrics); err == nil {
		t.Fatal("invented evidence accepted")
	}
	_, _, _, report = scoreFixture()
	report.Dimensions[0].EvidenceRefs = []EvidenceRef{{Fact: "known_stop_loss_risk_percent"}}
	if err := validateScoringReport(&report, req, results, metrics); err == nil {
		t.Fatal("unknown stop loss used as zero risk")
	}
	_, _, _, report = scoreFixture()
	report.Holdings[0].Symbol = "000001.SZ"
	if err := validateScoringReport(&report, req, results, metrics); err == nil {
		t.Fatal("foreign holding accepted")
	}
	_, _, _, report = scoreFixture()
	for i := 0; i < 2; i++ {
		report.Dimensions[i].Adjustments = []ScoreAdjustment{{RiskID: "集中", Reason: "集中风险", Points: -10}}
	}
	if err := validateScoringReport(&report, req, results, metrics); err == nil {
		t.Fatal("same risk penalized twice")
	}
}
func TestPromptUsesResearchAndBoundsInputWithoutMutatingReports(t *testing.T) {
	req, results, metrics, _ := scoreFixture()
	results[0].Analysis.ResearchReport.Thesis.Text = strings.Repeat("事实", 40000)
	before := results[0].Analysis.ResearchReport.Thesis.Text
	rules, _ := RulesFor(req.TraderProfile)
	prompt, err := buildPrompt(req, results, metrics, rules)
	if err != nil || len(prompt) > 25000 {
		t.Fatalf("oversized prompt %d %v", len(prompt), err)
	}
	if strings.Contains(prompt, "deterministic_summary") || !strings.Contains(prompt, "不得调用工具") || !strings.Contains(prompt, "\"source_id\":\"来源编号\"") {
		t.Fatal("bad scoring prompt")
	}
	if results[0].Analysis.ResearchReport.Thesis.Text != before {
		t.Fatal("cached source rewritten")
	}
}

type scoreGateway struct {
	calls   atomic.Int32
	fail    atomic.Bool
	content string
}

func (g *scoreGateway) Status() agent.Status { return agent.Status{Available: true, Configured: true} }
func (g *scoreGateway) Prompt(context.Context, string) (agent.PromptResult, error) {
	g.calls.Add(1)
	if g.fail.Load() {
		return agent.PromptResult{}, errors.New("test provider timeout")
	}
	return agent.PromptResult{Content: g.content}, nil
}
func (g *scoreGateway) Warmup(context.Context) error { return nil }

func (g *scoreGateway) ModelAPIKey() (string, error)           { return "", nil }
func (g *scoreGateway) SyncLLM(appsettings.LLM, *string) error { return nil }
func (g *scoreGateway) Start(context.Context) (agent.Process, error) {
	return nil, errors.New("unused")
}

func waitPortfolio(t *testing.T, s *Service, id string) Job {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		j, err := s.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status != "running" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("portfolio did not finish")
	return Job{}
}
func TestPortfolioRecoveryOnlyRetriesAggregation(t *testing.T) {
	req, results, _, report := scoreFixture()
	encoded, _ := json.Marshal(report)
	gateway := &scoreGateway{content: string(encoded)}
	gateway.fail.Store(true)
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var researchCalls atomic.Int32
	service := NewService(store, gateway, func(context.Context, string) (stockanalysis.Analysis, error) { return stockanalysis.Analysis{}, nil }, nil)
	defer service.Close()
	service.ConfigureResearch(func(_ context.Context, h Holding, _ Request, _ time.Time, _ bool, _ string, _ func(HoldingResult)) (HoldingResult, error) {
		researchCalls.Add(1)
		r := results[0]
		r.Holding = h
		return r, nil
	}, nil)
	job, err := service.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	done := waitPortfolio(t, service, job.ID)
	if done.Status != "partial" || done.Report.Conclusion.ScoreAvailable || !done.ResumeAvailable {
		t.Fatalf("partial %+v", done)
	}
	gateway.fail.Store(false)
	resumed, err := service.Resume(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	done = waitPortfolio(t, service, resumed.ID)
	if done.Status != "succeeded" || researchCalls.Load() != 1 || !done.Report.Conclusion.ScoreAvailable {
		t.Fatalf("aggregation recovery repeated stocks %+v calls=%d", done, researchCalls.Load())
	}
}

func TestRecoveryOnlyCompletesMissingStocks(t *testing.T) {
	req, results, _, report := scoreFixture()
	req.Holdings = append(req.Holdings, Holding{Symbol: "000001.SZ", Weight: 20})
	report.Holdings = append(report.Holdings, HoldingConclusion{Symbol: "000001.SZ", Conclusion: "等待验证", ActionPriority: "观察", Action: "等待确认", Confirmation: "趋势延续", Invalidation: "趋势破坏"})
	data, _ := json.Marshal(report)
	gateway := &scoreGateway{content: string(data)}
	store, _ := OpenStore("")
	defer store.Close()
	service := NewService(store, gateway, func(context.Context, string) (stockanalysis.Analysis, error) { return stockanalysis.Analysis{}, nil }, nil)
	defer service.Close()
	var callsA, callsB atomic.Int32
	var fixed atomic.Bool
	service.ConfigureResearch(func(_ context.Context, h Holding, _ Request, _ time.Time, _ bool, _ string, _ func(HoldingResult)) (HoldingResult, error) {
		r := results[0]
		r.Holding = h
		if h.Symbol == "600519.SH" {
			callsA.Add(1)
			return r, nil
		}
		callsB.Add(1)
		if !fixed.Load() {
			return HoldingResult{Holding: h}, errors.New("provider failed")
		}
		a := *r.Analysis
		a.Symbol = h.Symbol
		a.AnalysisID = "report2"
		r.Analysis = &a
		r.AnalysisID = a.AnalysisID
		return r, nil
	}, nil)
	job, err := service.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	done := waitPortfolio(t, service, job.ID)
	if done.Status != "partial" || done.Report.Metrics.AIResearchCoveragePercent != 75 || done.Report.Conclusion.ScoreAvailable {
		t.Fatalf("missing stock misrepresented %+v", done)
	}
	fixed.Store(true)
	job, err = service.Resume(context.Background(), done.ID)
	if err != nil {
		t.Fatal(err)
	}
	done = waitPortfolio(t, service, job.ID)
	if done.Status != "succeeded" || callsA.Load() != 1 || callsB.Load() != 2 {
		t.Fatalf("successful stock repeated %d/%d %s", callsA.Load(), callsB.Load(), done.Status)
	}
}

func TestScoringCannotInventPricePlanOrTreatIntradayQuoteAsClose(t *testing.T) {
	req, results, metrics, report := scoreFixture()
	report.Holdings[0].Action = "止损价 123.45 元"
	if err := validateScoringReport(&report, req, results, metrics); err == nil {
		t.Fatal("unsupported price accepted")
	}
	a := results[0].Analysis.ResearchReport
	a.CutoffAt = time.Now().Add(-time.Hour)
	a.Anchors = []stockanalysis.PriceAnchor{{ID: "p1", Price: 10}}
	a.Conditions = []stockanalysis.ResearchCondition{{ID: "c1", Text: "收盘不低于锚点", Metric: "close", Operator: "gte", AnchorID: "p1"}, {ID: "c2", Metric: "disclosure"}}
	results[0].CurrentQuote = &foundation.Quote{Price: 11, TradeTime: time.Now()}
	facts := portfolioFacts(req, results, metrics)
	c := facts["600519.SH.condition.c1"]
	if !c.Available || c.Value.(map[string]any)["status"] != "quote_reference_only" || facts["600519.SH.condition.c2"].Available {
		t.Fatal("unverified research conditions claimed confirmed")
	}
}
