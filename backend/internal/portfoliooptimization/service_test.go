package portfoliooptimization

import (
	"context"
	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/foundation"
	pi "easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureJob() Job {
	now := time.Now().UTC()
	cost := 100.0
	hs := holds(60, 20)
	hs[1].CostPrice = &cost
	req := pi.Request{TraderProfile: pi.ProfileBalanced, Horizon: "swing", ResearchLevel: stockanalysis.ResearchLevelStandard, Holdings: hs}
	rs := []pi.HoldingResult{}
	elig := []Eligibility{}
	for _, h := range hs {
		rr := &stockanalysis.ResearchReport{ResearchSynthesis: stockanalysis.ResearchSynthesis{Headline: "测试逻辑", Thesis: stockanalysis.ResearchClaim{Text: "可核验逻辑", SourceIDs: []string{"s1"}}, EvidenceLevel: "sufficient", Conditions: []stockanalysis.ResearchCondition{{ID: "confirm", Text: "趋势确认", SourceIDs: []string{"s1"}}, {ID: "invalid", Text: "逻辑失效", SourceIDs: []string{"s1"}}}, InvalidationIDs: []string{"invalid"}, Decision: stockanalysis.ResearchDecision{Status: "conditional", Horizon: "swing"}}, Sources: []stockanalysis.ResearchSource{{ID: "s1", Title: "测试数据", Content: "事实"}}, Request: stockanalysis.ResearchRequest{Symbol: h.Symbol, Horizon: "swing"}, GeneratedAt: now, CutoffAt: now, Validation: "references_checked"}
		a := &stockanalysis.Analysis{Symbol: h.Symbol, Name: h.Symbol, AI: stockanalysis.AISynthesisStatus{Status: "ready"}, ResearchReport: rr}
		rs = append(rs, pi.HoldingResult{Holding: h, Analysis: a, AnalysisID: "report-" + h.Symbol, Status: "succeeded", ResearchOrigin: "reused", ReportCompletedAt: now, ResearchCutoffAt: now})
		elig = append(elig, Eligibility{Symbol: h.Symbol, CanIncrease: true, Conditional: true})
	}
	source := pi.OptimizationReport(req, rs)
	source.ID = "source"
	source.GeneratedAt = now
	source.Conclusion = fixtureScore(hs)
	source.Conclusion.ScoreAvailable = true
	score := 70
	source.Conclusion.TotalScore = &score
	return Job{Source: source, SourceID: "source", RootSourceID: "source", Baseline: hs, Results: rs, Eligibility: elig, AsOf: now, Fingerprint: "ab"}
}
func fixtureProposal() Proposal {
	p := Proposal{Issues: []string{"单票集中"}}
	as := []Allocation{}
	for i, s := range []string{"600519.SH", "000858.SZ"} {
		w := 45
		if i == 1 {
			w = 35
		}
		as = append(as, Allocation{Symbol: s, Minimum: w, Maximum: w, Preferred: w, Reason: "研究支持调整集中", Funding: "由另一只减持配对", Suitable: true, SuitabilityReason: "同周期证据支持新增资金", ConfirmationIDs: []string{"confirm"}, InvalidationIDs: []string{"invalid"}, EvidenceRefs: []pi.EvidenceRef{{ReportID: "report-" + s, SourceID: "s1"}}, Investment: fixtureInvestment()})
	}
	p.Alternatives = []Alternative{{Name: "测试小幅调权", Allocations: as}}
	return p
}
func fixtureInvestment() *InvestmentJudgment {
	return &InvestmentJudgment{Role: "盈利兑现", Action: "allocate", Horizon: "swing", Business: "主营盈利可持续", Growth: "本周期经营增长", Valuation: "估值未知，主要比较组合角色", Timing: "按量价与经营变化管理", PortfolioFit: "降低原集中风险", Risk: "仍有行业波动", Exit: "盈利逻辑恶化时减仓", OpportunityCost: "比较另一旧股及原配置", PriorOpinion: "保留原意见；本次基于组合需求重新判断", PeriodSuitability: "仅复用经营事实，不将短线价格结论证明波段走势"}
}
func fixtureScore(holdings []pi.Holding) pi.AIReport {
	r := pi.AIReport{RiskLevel: "中", RiskReason: "集中风险待核验", StyleMatch: "部分偏离", ExecutiveSummary: "按条件管理", ConfidenceLevel: "中", ConfidenceReason: "部分条件待验证", Scenarios: []pi.Scenario{{Name: "震荡", Condition: "趋势走弱", PortfolioAction: "复核逻辑"}}}
	for _, key := range []string{"holding_logic", "portfolio_structure", "risk_capacity", "strategy_fit"} {
		score := 70
		r.Dimensions = append(r.Dimensions, pi.ScoreDimension{Key: key, Score: &score, Reason: "共同证据有限判断", EvidenceRefs: []pi.EvidenceRef{{Fact: "concentration_hhi"}}})
	}
	for _, h := range holdings {
		r.Holdings = append(r.Holdings, pi.HoldingConclusion{Symbol: h.Symbol, Conclusion: "按条件持有", ActionPriority: "观察", Action: "等待确认", Confirmation: "趋势确认", Invalidation: "逻辑失效"})
	}
	return r
}

type testGateway struct {
	calls        atomic.Int32
	fail         atomic.Bool
	block        atomic.Bool
	entered      chan struct{}
	pairedPrompt string
}

func (g *testGateway) Status() agent.Status                         { return agent.Status{Available: true, Configured: true} }
func (g *testGateway) ModelAPIKey() (string, error)                 { return "", nil }
func (g *testGateway) SyncLLM(appsettings.LLM, *string) error       { return nil }
func (g *testGateway) Start(context.Context) (agent.Process, error) { return nil, errors.New("unused") }
func (g *testGateway) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	g.calls.Add(1)
	if g.block.Load() {
		select {
		case g.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return agent.PromptResult{}, ctx.Err()
	}
	if g.fail.Load() {
		return agent.PromptResult{}, errors.New("test timeout")
	}
	var v any
	if strings.Contains(prompt, "独立组合复评员") {
		g.pairedPrompt = prompt
		var input struct {
			A, B struct {
				Weights map[string]float64 `json:"weights"`
			}
		}
		if err := json.Unmarshal([]byte(strings.Split(prompt, "[资料JSON]\n")[1]), &input); err != nil {
			return agent.PromptResult{}, err
		}
		a := fixtureScore(holds(int(input.A.Weights["600519.SH"]), int(input.A.Weights["000858.SZ"])))
		b := fixtureScore(holds(int(input.B.Weights["600519.SH"]), int(input.B.Weights["000858.SZ"])))
		a.Holdings, b.Holdings, a.Scenarios, b.Scenarios = nil, nil, nil, nil
		preferred := "b"
		if input.A.Weights["600519.SH"] < input.B.Weights["600519.SH"] {
			preferred = "a"
		}
		v = map[string]any{"a": a, "b": b, "assessment": Assessment{Preferred: preferred, Reason: "较低单票仓位更合理，现金不变但集中缺口仍在", Issue: "单票集中", EvidenceRefs: []pi.EvidenceRef{{Fact: "max_single_percent"}}}}
	} else {
		v = fixtureProposal()
	}
	data, _ := json.Marshal(v)
	return agent.PromptResult{Content: string(data)}, nil
}
func setupService(t *testing.T, g *testGateway) (*Service, *pi.Store, *atomic.Int32) {
	t.Helper()
	store, err := pi.OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	j := fixtureJob()
	_, err = store.Save(context.Background(), pi.Job{ID: "source", Status: "succeeded", Request: j.Source.Request, Report: &j.Source})
	if err != nil {
		t.Fatal(err)
	}
	calls := &atomic.Int32{}
	deps := Dependencies{Collect: func(context.Context, pi.Report, Request, time.Time) (Universe, error) {
		u := Universe{}
		for _, h := range j.Baseline {
			u.Catalog = append(u.Catalog, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: h.Symbol, Name: "测试股", Volume: 100, Amount: 1000, Meta: foundation.SourceMeta{TradeDate: LatestSession(time.Now())}}})
		}
		return u, nil
	}, Research: func(_ context.Context, h pi.Holding, _ pi.Request, _ time.Time, _ bool, _ string, _ func(pi.HoldingResult)) (pi.HoldingResult, error) {
		calls.Add(1)
		for _, r := range j.Results {
			if r.Holding.Symbol == h.Symbol {
				return r, nil
			}
		}
		return pi.HoldingResult{}, errors.New("unknown")
	}, Quotes: func(_ context.Context, symbols []string) ([]foundation.Quote, error) {
		date, _ := time.ParseInLocation("2006-01-02", LatestSession(time.Now()), time.FixedZone("Asia/Shanghai", 8*3600))
		out := []foundation.Quote{}
		for _, s := range symbols {
			out = append(out, foundation.Quote{Symbol: s, Name: "测试", Price: 100, TradeTime: date})
		}
		return out, nil
	}}
	svc := NewService(store, g, deps)
	t.Cleanup(func() { svc.Close(); store.Close() })
	return svc, store, calls
}
func waitDone(t *testing.T, s *Service, id string) Job {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		j, err := s.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status != "running" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}
func TestEndToEndDedupResearchReuseAndSafeApplication(t *testing.T) {
	g := &testGateway{}
	s, store, calls := setupService(t, g)
	j, err := s.Start(context.Background(), "source", Request{})
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.Start(context.Background(), "source", Request{})
	if err != nil || same.ID != j.ID {
		t.Fatal("dedup", err)
	}
	done := waitDone(t, s, j.ID)
	if done.Status != "succeeded" || done.SelectedPlan == nil || done.Outcome != "conditional" {
		t.Fatalf("%+v", done)
	}
	if calls.Load() != 0 || g.calls.Load() != 2 {
		t.Fatal("successful research repeated or extra assessments")
	}
	plan := done.Plans[0]
	if plan.Checks.Total != 80 || plan.Checks.Cash != 20 || plan.Checks.Replacement != 18.75 {
		t.Fatal(plan.Checks)
	}
	if strings.Contains(g.pairedPrompt, "preferred_weight") || strings.Contains(g.pairedPrompt, "funding_reason") {
		t.Fatal("optimizer marketing leaked to independent review")
	}
	request, err := ApplyRequest(done)
	if err != nil || request.SourceOptimizationID != done.ID || request.Holdings[1].CostPrice != nil {
		t.Fatal("unsafe cost/provenance", err)
	}
	if err := s.ValidateApplication(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	request.Holdings[0].Weight++
	if s.ValidateApplication(context.Background(), request) == nil {
		t.Fatal("modified target bypass")
	}
	source, _ := store.Get(context.Background(), "source")
	if source.Request.Holdings[1].CostPrice == nil || source.Request.Holdings[0].Weight != 60 {
		t.Fatal("source changed")
	}
	legacy := done
	legacy.ID, legacy.Version, legacy.Fingerprint = "legacy-plan", "portfolio-optimization-v1", "legacy-plan-input"
	if err := s.save(&legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRequest(legacy); err == nil {
		t.Fatal("legacy plan applied")
	}
	request.SourceOptimizationID = legacy.ID
	request.Holdings = append([]pi.Holding{}, legacy.Plans[*legacy.SelectedPlan].Target...)
	if s.ValidateApplication(context.Background(), request) == nil {
		t.Fatal("legacy application API bypass")
	}
	legacy.ID, legacy.Version, legacy.ModelPromptVersion, legacy.Fingerprint = "legacy-review", Version, "", "legacy-review-input"
	if err := s.save(&legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRequest(legacy); err == nil {
		t.Fatal("old anonymous review applied")
	}
	request.SourceOptimizationID = legacy.ID
	if s.ValidateApplication(context.Background(), request) == nil {
		t.Fatal("old anonymous review bypassed API validation")
	}
}

func TestAnonymousComparisonMapsPreferredPortfolioInBothOrders(t *testing.T) {
	for _, order := range []string{"original_first", "target_first"} {
		t.Run(order, func(t *testing.T) {
			g := &testGateway{}
			s, _, researchCalls := setupService(t, g)
			j := fixtureJob()
			j.ID = "anonymous-" + order
			j.SnapshotAt = time.Now().UTC()
			p := fixtureProposal()
			j.Proposal = &p
			target := holds(45, 35)
			req := j.Source.Request
			req.Holdings = target
			j.Plans = []Plan{{Name: "调权", Status: "pending_review", Allocations: p.Alternatives[0].Allocations, Target: target, Original: j.Source, Proposed: pi.OptimizationReport(req, j.Results), Checks: Check(j.Baseline, target), Improvements: measureImprovements(j, target), AssessmentOrder: order}}
			if err := s.execute(context.Background(), &j); err != nil {
				t.Fatal(err)
			}
			if j.Outcome != "conditional" || j.SelectedPlan == nil || !j.Plans[0].Assessment.Accepted || g.calls.Load() != 1 || researchCalls.Load() != 0 {
				t.Fatal("anonymous direction was reversed or research repeated", j.Outcome, j.Plans[0].Assessment)
			}
			if strings.Contains(g.pairedPrompt, "target_first") || strings.Contains(g.pairedPrompt, "original_first") {
				t.Fatal("order leaked into blind review")
			}
		})
	}
	for _, tc := range []struct {
		preferred, order string
		accepted         bool
	}{{"a", "original_first", false}, {"b", "target_first", false}, {"neither", "original_first", false}, {"neither", "target_first", false}} {
		got, err := comparisonTargetsProposal(tc.preferred, tc.order)
		if err != nil || got != tc.accepted {
			t.Fatal(tc, got, err)
		}
	}
	if _, err := comparisonTargetsProposal("", "target_first"); err == nil {
		t.Fatal("legacy accepted flag must not infer direction")
	}
}

func TestNewValidationVersionDoesNotReuseLegacyUnchangedResult(t *testing.T) {
	for _, oldVersion := range []string{"portfolio-optimization-v1", Version} {
		t.Run(oldVersion, func(t *testing.T) {
			g := &testGateway{}
			s, store, calls := setupService(t, g)
			legacy := fixtureJob()
			legacy.ID = "legacy-unchanged"
			legacy.Version = oldVersion
			legacy.Status, legacy.Outcome = "succeeded", "unchanged"
			legacy.Fingerprint = fingerprint(struct {
				Source   string
				Version  string
				Request  Request
				Baseline string
			}{"source", legacy.Version, Request{}, baselineHash(legacy.Baseline)})
			for i := range legacy.Eligibility {
				legacy.Eligibility[i].Locked = true
				legacy.Eligibility[i].CanIncrease = false
			}
			if err := s.save(&legacy); err != nil {
				t.Fatal(err)
			}
			fresh, err := s.Start(context.Background(), "source", Request{})
			if err != nil {
				t.Fatal(err)
			}
			done := waitDone(t, s, fresh.ID)
			if done.ID == legacy.ID || done.Version != Version || done.Outcome != "conditional" || calls.Load() != 0 {
				t.Fatalf("legacy result reused or successful research repeated: %s %s %s calls=%d", done.ID, done.Version, done.Outcome, calls.Load())
			}
			raw, err := store.GetOptimization(context.Background(), legacy.ID)
			if err != nil {
				t.Fatal(err)
			}
			var retained Job
			if err := json.Unmarshal(raw, &retained); err != nil {
				t.Fatal(err)
			}
			if retained.Version != legacy.Version || retained.Outcome != "unchanged" || !retained.Eligibility[0].Locked {
				t.Fatal("legacy history was modified")
			}
		})
	}
}

func TestResumeLegacyVersionRefreshesLockedSnapshotAndProposal(t *testing.T) {
	g := &testGateway{}
	s, _, calls := setupService(t, g)
	legacy := fixtureJob()
	legacy.ID, legacy.Version = "legacy-resume", "portfolio-optimization-v1"
	legacy.Status, legacy.ResumeAvailable = "incomplete", true
	legacy.LatestTradeDate = LatestSession(time.Now())
	legacy.SnapshotAt = time.Now().UTC()
	legacy.Proposal = &Proposal{KeepReason: "old missing amount locked every stock"}
	for i := range legacy.Eligibility {
		legacy.Eligibility[i].Locked = true
		legacy.Eligibility[i].CanIncrease = false
	}
	if err := s.save(&legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resume(context.Background(), legacy.ID); err != nil {
		t.Fatal(err)
	}
	done := waitDone(t, s, legacy.ID)
	if done.Version != Version || done.Outcome != "conditional" || done.Eligibility[0].Locked ||
		!done.SnapshotAt.After(legacy.SnapshotAt) || calls.Load() != 0 || g.calls.Load() != 2 {
		t.Fatalf("legacy checkpoint not refreshed: %s %s locked=%v research=%d models=%d", done.Version, done.Outcome, done.Eligibility[0].Locked, calls.Load(), g.calls.Load())
	}
}
func TestCancelResumeAndRestartPreserveResearchAndStage(t *testing.T) {
	g := &testGateway{entered: make(chan struct{}, 1)}
	g.block.Store(true)
	s, store, calls := setupService(t, g)
	j, err := s.Start(context.Background(), "source", Request{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-g.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("not entered")
	}
	if !s.Cancel(j.ID) {
		t.Fatal("not cancelled")
	}
	done := waitDone(t, s, j.ID)
	if done.Status != "cancelled" || !done.ResumeAvailable {
		t.Fatal(done.Status)
	}
	s.Close()
	g.block.Store(false)
	s2 := NewService(store, g, s.deps)
	defer s2.Close()
	if _, err := s2.Resume(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	done = waitDone(t, s2, j.ID)
	if done.Status != "succeeded" || calls.Load() != 0 {
		t.Fatalf("recovery %s %s %d", done.Status, done.Error, calls.Load())
	}
	// Persisted snapshot is still usable even if source history disappears.
	saved, err := s2.Get(context.Background(), j.ID)
	if err != nil || len(saved.Source.Holdings) != 2 {
		t.Fatal("snapshot dependency")
	}
}
func TestRefAndConditionForgeryRejected(t *testing.T) {
	j := fixtureJob()
	p := fixtureProposal()
	p.Alternatives[0].Allocations[1].EvidenceRefs[0].SourceID = "invented"
	if validateProposal(j, p) == nil {
		t.Fatal("invented source")
	}
	p = fixtureProposal()
	p.Alternatives[0].Allocations[1].ConfirmationIDs = []string{"invented"}
	if validateProposal(j, p) == nil {
		t.Fatal("invented condition")
	}
	p = fixtureProposal()
	p.Alternatives[0].Allocations[1].Reason = "买入价格999元"
	if validateProposal(j, p) == nil {
		t.Fatal("unanchored trade price")
	}
}

func TestChainedInspectionUsesInitialBaseline(t *testing.T) {
	g := &testGateway{}
	s, store, _ := setupService(t, g)
	j, err := s.Start(context.Background(), "source", Request{})
	if err != nil {
		t.Fatal(err)
	}
	done := waitDone(t, s, j.ID)
	req, err := ApplyRequest(done)
	if err != nil {
		t.Fatal(err)
	}
	fresh := done.Plans[0].Proposed
	fresh.Request = req
	fresh.ID = "next-inspection"
	fresh.GeneratedAt = time.Now().UTC()
	_, err = store.Save(context.Background(), pi.Job{ID: fresh.ID, Status: "succeeded", Request: req, Report: &fresh})
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.Start(context.Background(), fresh.ID, Request{})
	if err != nil {
		t.Fatal(err)
	}
	if !equalWeights(next.Baseline, holds(60, 20)) || next.RootSourceID != "source" {
		t.Fatal("chain rebased", next.Baseline, next.RootSourceID)
	}
	s.Cancel(next.ID)
	waitDone(t, s, next.ID)
}
func TestInterruptedPersistenceAndManualModelRecovery(t *testing.T) {
	g := &testGateway{}
	s, store, _ := setupService(t, g)
	j := fixtureJob()
	j.ID = "interrupted"
	j.Fingerprint = "unique"
	j.Status = "running"
	j.Version = Version
	j.ModelDurationMS = ModelTimeout.Milliseconds()
	if err := s.save(&j); err != nil {
		t.Fatal(err)
	}
	s.Close()
	fresh := NewService(store, g, s.deps)
	defer fresh.Close()
	stored, err := fresh.Get(context.Background(), j.ID)
	if err != nil || stored.Status != "interrupted" || !stored.ResumeAvailable {
		t.Fatal("interruption not recovered", err)
	}
	if _, err := fresh.Resume(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	done := waitDone(t, fresh, j.ID)
	if done.Status != "succeeded" {
		t.Fatal("exhausted checkpoint never recoverable", done.Error)
	}
}

func TestNoProposalAndNoFactImprovementAreValidConclusions(t *testing.T) {
	j := fixtureJob()
	p := Proposal{KeepReason: "约束下应保持"}
	if err := validateProposal(j, p); err != nil {
		t.Fatal(err)
	}
	p = fixtureProposal()
	p.Alternatives[0].Allocations[0].Minimum = 60
	p.Alternatives[0].Allocations[0].Maximum = 60
	p.Alternatives[0].Allocations[0].Preferred = 60
	p.Alternatives[0].Allocations[1].Minimum = 20
	p.Alternatives[0].Allocations[1].Maximum = 20
	p.Alternatives[0].Allocations[1].Preferred = 20
	target, err := Solve(j, p.Alternatives[0])
	if err != nil || !equalWeights(target, j.Source.Request.Holdings) {
		t.Fatal("unchanged invalid", err)
	}
	if len(measureImprovements(j, target)) != 0 {
		t.Fatal("score-only marketing marked improvement")
	}
}

func TestBlockedFundingIsNotReportedAsRecommendationAndDoesNotRunReview(t *testing.T) {
	for _, tc := range []struct {
		name, evidence, status, outcome string
	}{
		{"no-trading-plan", "sufficient", "no_plan", "unchanged"},
		{"limited-evidence", "limited", "observe", "unchanged"},
		{"eligible-observation", "sufficient", "observe", "unchanged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := &testGateway{}
			service, _, _ := setupService(t, gateway)
			job := fixtureJob()
			job.ID = "funding-" + tc.name
			job.SnapshotAt = job.AsOf
			job.Proposal = &Proposal{KeepReason: "暂不调整"}
			for i := range job.Results {
				report := job.Results[i].Analysis.ResearchReport
				report.EvidenceLevel = tc.evidence
				report.EvidenceReasons = []string{"财务关键字段仍需核实"}
				report.Decision.Status = tc.status
				report.Decision.Reason = "行情时效不足"
			}
			if err := service.execute(context.Background(), &job); err != nil {
				t.Fatal(err)
			}
			if job.Status != "succeeded" || job.Outcome != tc.outcome || job.SelectedPlan != nil {
				t.Fatalf("unexpected outcome: %s/%s", job.Status, job.Outcome)
			}
			if gateway.calls.Load() != 0 {
				t.Fatal("independent review ran without a changed portfolio")
			}
			if tc.outcome == "no_feasible_plan" && (!strings.Contains(job.OutcomeReason, "不代表通过优化推荐") || !strings.Contains(job.OutcomeReason, job.Results[0].Holding.Symbol)) {
				t.Fatalf("blocked result hides funding reason: %s", job.OutcomeReason)
			}
		})
	}
}

func TestCodeScreeningSelectsSixAndResearchRunsAtMostTwoConcurrently(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	base := fixtureJob()
	var active, maximum, calls atomic.Int32
	s.deps.Collect = func(context.Context, pi.Report, Request, time.Time) (Universe, error) {
		u := Universe{}
		for _, h := range base.Baseline {
			u.Catalog = append(u.Catalog, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: h.Symbol, Name: "原持仓", Volume: 100, Amount: 10000, Meta: foundation.SourceMeta{TradeDate: LatestSession(time.Now())}}})
		}
		for i := 0; i < MaxCandidates; i++ {
			symbol := fmt.Sprintf("600%03d.SH", i+1)
			u.Catalog = append(u.Catalog, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: symbol, Name: symbol, Volume: 100, Amount: 10000, Meta: foundation.SourceMeta{TradeDate: LatestSession(time.Now())}}, Industry: []string{"银行", "国有大型银行Ⅱ", "城商行Ⅱ", "机械设备", "电力设备", "食品饮料", "计算机", "通信"}[i]})
			u.Candidates = append(u.Candidates, Candidate{Symbol: symbol, Name: symbol, Screening: &CandidateScreening{Qualified: i != 0, Score: float64(100 - i)}})
		}
		return u, nil
	}
	s.deps.Research = func(_ context.Context, h pi.Holding, _ pi.Request, _ time.Time, _ bool, _ string, notify func(pi.HoldingResult)) (pi.HoldingResult, error) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		if h.Symbol == "600001.SH" {
			return pi.HoldingResult{}, errors.New("unqualified candidate reached AI")
		}
		raw, _ := json.Marshal(base.Results[0])
		var r pi.HoldingResult
		_ = json.Unmarshal(raw, &r)
		r.Holding = h
		r.Analysis.Symbol = h.Symbol
		r.Analysis.ResearchReport.Request.Symbol = h.Symbol
		r.AnalysisID = "report-" + h.Symbol
		r.ResearchOrigin = "new"
		notify(pi.HoldingResult{Holding: h, Status: "running"})
		time.Sleep(25 * time.Millisecond)
		return r, nil
	}
	j := base
	j.ID = "code-screening"
	j.Version = Version
	if err := s.prepare(context.Background(), &j); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 6 || maximum.Load() != 2 || g.calls.Load() != 0 || j.NewStocks != 6 || len(j.Results) != 8 {
		t.Fatalf("calls=%d max=%d AI decisions before research=%d new=%d results=%d", calls.Load(), maximum.Load(), g.calls.Load(), j.NewStocks, len(j.Results))
	}
	for i, c := range j.Candidates {
		if c.Selected != (i == 1 || i >= 3) {
			t.Fatal("code ranking not respected", i, c)
		}
	}
}
func TestPairedPromptSharesEvidenceOnceAndFreezesGroups(t *testing.T) {
	j := fixtureJob()
	p := fixtureProposal()
	j.Proposal = &p
	before := pi.OptimizationReport(j.Source.Request, j.Results)
	req := j.Source.Request
	req.Holdings = holds(45, 35)
	after := pi.OptimizationReport(req, j.Results)
	text, err := pairedPrompt(j, Plan{Original: before, Proposed: after, AssessmentOrder: "original_first"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(text, "可核验逻辑") != 2 || !strings.Contains(text, "不再输出risk_groups") || len(text) > 30000 {
		t.Fatalf("duplicated or unbounded evidence prompt: %d", len(text))
	}
}

func TestQualifiedCandidatesDoNotFillQuotaWithSameIndustry(t *testing.T) {
	g := &testGateway{}
	s, _, _ := setupService(t, g)
	base := fixtureJob()
	s.deps.Collect = func(context.Context, pi.Report, Request, time.Time) (Universe, error) {
		u := Universe{}
		for _, h := range base.Baseline {
			u.Catalog = append(u.Catalog, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: h.Symbol, Name: "原持仓", Volume: 100, Amount: 10000, Meta: foundation.SourceMeta{TradeDate: LatestSession(time.Now())}}})
		}
		for i := 0; i < 4; i++ {
			symbol := fmt.Sprintf("600%03d.SH", i+1)
			industry := []string{"国有大型银行Ⅱ", "城商行Ⅱ", "股份制银行Ⅱ", ""}[i]
			u.Catalog = append(u.Catalog, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: symbol, Name: symbol, Volume: 100, Amount: 10000, Meta: foundation.SourceMeta{TradeDate: LatestSession(time.Now())}}, Industry: industry})
			u.Candidates = append(u.Candidates, Candidate{Symbol: symbol, Screening: &CandidateScreening{Qualified: true, Score: float64(100 - i)}})
		}
		return u, nil
	}
	calls := 0
	s.deps.Research = func(_ context.Context, h pi.Holding, _ pi.Request, _ time.Time, _ bool, _ string, _ func(pi.HoldingResult)) (pi.HoldingResult, error) {
		calls++
		if h.Symbol != "600001.SH" {
			return pi.HoldingResult{}, errors.New("same/unknown industry started AI")
		}
		data, _ := json.Marshal(base.Results[0])
		var r pi.HoldingResult
		_ = json.Unmarshal(data, &r)
		r.Holding = h
		r.AnalysisID = "report-" + h.Symbol
		r.ResearchOrigin = "new"
		return r, nil
	}
	j := base
	j.ID = "single-industry"
	j.Fingerprint = "single-industry"
	if err := s.prepare(context.Background(), &j); err != nil {
		t.Fatal(err)
	}
	selected := 0
	for _, c := range j.Candidates {
		if c.Selected {
			selected++
		}
	}
	if calls != 1 || selected != 1 || !strings.Contains(strings.Join(j.Limitations, ";"), "不补同一行业") {
		t.Fatal("filled quota despite industry constraint", calls, selected, j.Limitations)
	}
}
