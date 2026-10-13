package portfolioinspection

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/etfanalysis"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/stockanalysis"
)

func etfScoringFixture(premium float64, etfWeight int) (Request, []HoldingResult, Metrics, AIReport, time.Time) {
	req, stocks, _, report := scoreFixture()
	at := time.Date(2026, 10, 13, 10, 0, 0, 0, foundation.AStockLocation)
	h := Holding{Symbol: "510300.SH", Name: "测试宽基ETF", Weight: etfWeight}
	req.Holdings[0].Weight = 100 - etfWeight
	stocks[0].Holding = req.Holdings[0]
	value := func(v float64) *float64 { return &v }
	profile := foundation.ETFProfile{Instrument: foundation.Instrument{Symbol: h.Symbol, InstrumentType: "etf"}, AssetClass: "equity", TrackingTarget: "沪深300指数", Meta: foundation.SourceMeta{Source: "fixture", FetchedAt: at}}
	data := etfanalysis.Analysis{Profile: profile, ExecutionBlockers: []string{"交易规则须核验"}, Direction: &etfanalysis.DirectionAnalysis{Classification: etfanalysis.ClassifyDirection(profile)}}
	data.Execution.Snapshot = foundation.ETFExecution{Symbol: h.Symbol, Price: value(1 + premium/100), IOPV: value(1), Bid: value(1), Ask: value(1.001), BidUnits: value(100), AskUnits: value(100), QuoteAt: at, IOPVAt: at, QuoteStatus: "normal_quote"}
	research := &stockanalysis.ResearchReport{Validation: "references_checked", CutoffAt: at, ResearchSynthesis: stockanalysis.ResearchSynthesis{ETFJudgments: &etfanalysis.Judgments{}, Thesis: stockanalysis.ResearchClaim{Text: "ETF研究", SourceIDs: []string{"e-profile"}}}, Sources: []stockanalysis.ResearchSource{{ID: "e-profile", Kind: "fund_profile", Content: "冻结基金档案"}}}
	stocks = append(stocks, HoldingResult{Holding: h, Status: "succeeded", AnalysisID: "etf-report", ResearchCutoffAt: at, Analysis: &stockanalysis.Analysis{Symbol: h.Symbol, InstrumentType: "etf", ETF: &data, AI: stockanalysis.AISynthesisStatus{Status: "ready"}, ResearchReport: research}})
	req.Holdings = append(req.Holdings, h)
	report.Holdings = append(report.Holdings, HoldingConclusion{Symbol: h.Symbol, Conclusion: "观察", ActionPriority: "观察", Action: "核验交易条件", Confirmation: "趋势延续", Invalidation: "趋势失效"})
	rules, _ := RulesFor(req.TraderProfile)
	return req, stocks, CalculateMetrics(req, stocks, rules), report, at
}

func TestETFPremiumBoundariesAndWeightedScores(t *testing.T) {
	for _, tc := range []struct {
		p       float64
		penalty int
	}{{-.5, 0}, {0, 0}, {.49999, 0}, {.5, 3}, {.99999, 3}, {1, 6}, {2.99999, 6}, {3, 12}, {4.99999, 12}, {5, 20}, {9.99999, 20}, {10, 35}} {
		req, results, metrics, report, at := etfScoringFixture(tc.p, 50)
		if err := validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at); err != nil {
			t.Fatal(tc, err)
		}
		e := report.ETFScoring
		want := int(math.Round(72 + .5*(8-float64(tc.penalty))))
		if e == nil || e.BaseRiskScore != 72 || e.FinalRiskScore != want || e.PremiumPenalty != float64(tc.penalty)/2 || e.DiversificationBonus != 4 {
			t.Fatalf("p=%v: %+v want risk %d", tc.p, e, want)
		}
		if _, valid := VerifiedOptimizationScore(report); !valid {
			t.Fatal("adjusted total disagrees with persisted dimensions")
		}
	}
}

func TestETFScoreDependsOnInternalWeightsNotCashOrCost(t *testing.T) {
	req, results, metrics, report, at := etfScoringFixture(5, 50)
	if err := validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at); err != nil {
		t.Fatal(err)
	}
	want := *report.TotalScore
	for _, scale := range []float64{.2, .6} {
		req, results, _, report, at := etfScoringFixture(5, 50)
		for i := range req.Holdings {
			req.Holdings[i].Weight = int(float64(req.Holdings[i].Weight) * scale)
			results[i].Holding = req.Holdings[i]
		}
		cost := .1
		results[1].Holding.CostPrice = &cost
		rules, _ := RulesFor(req.TraderProfile)
		metrics := CalculateMetrics(req, results, rules)
		if err := validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at); err != nil {
			t.Fatal(err)
		}
		if *report.TotalScore != want || report.ETFScoring.PremiumPenalty != 10 {
			t.Fatal("cash or floating profit changed ETF risk score", report)
		}
	}
}

func TestETFUnverifiedPremiumCannotUseNewPriceOrStaleFlag(t *testing.T) {
	for _, kind := range []string{"old", "cross_border", "money", "stale", "carry", "future", "wrong_symbol", "invalid_iopv"} {
		req, results, metrics, report, at := etfScoringFixture(10, 50)
		a := results[1].Analysis.ETF
		a.Execution.Comparable = true
		oldPremium := 10.0
		a.Execution.PremiumPercent = &oldPremium
		switch kind {
		case "old":
			at = at.Add(2 * time.Minute)
		case "cross_border":
			a.Profile.CrossBorder = true
		case "money":
			a.Profile.AssetClass = "money"
		case "stale":
			a.Execution.Snapshot.Meta.Stale = true
		case "carry":
			a.Execution.Snapshot.Meta.CarryForward = true
		case "future":
			a.Execution.Snapshot.IOPVAt = at.Add(time.Minute)
		case "wrong_symbol":
			a.Execution.Snapshot.Symbol = "159915.SZ"
		case "invalid_iopv":
			a.Execution.Snapshot.IOPV = nil
		}
		results[1].CurrentQuote = &foundation.Quote{Price: 99, TradeTime: at}
		if err := validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at); err != nil {
			t.Fatal(err)
		}
		item := report.ETFScoring.Adjustments[0]
		if item.PremiumEligible || item.PremiumPercent != nil || report.ETFScoring.PremiumPenalty != 0 || len(report.DataLimitations) == 0 {
			t.Fatal(kind, item)
		}
		f := portfolioFactsAt(req, results, metrics, at)["510300.SH.etf.premium_assessment"]
		if f.Available {
			t.Fatal("unverified premium made citable", kind)
		}
	}
}

func TestETFObservationExpiresAtNextContinuousSession(t *testing.T) {
	_, results, metrics, _, at := etfScoringFixture(5, 50)
	snapshot := &results[1].Analysis.ETF.Execution.Snapshot
	snapshot.QuoteAt = time.Date(2026, 10, 13, 15, 1, 0, 0, foundation.AStockLocation)
	snapshot.IOPVAt = snapshot.QuoteAt
	for _, tc := range []struct {
		at       time.Time
		eligible bool
	}{{snapshot.QuoteAt.Add(time.Hour), true}, {time.Date(2026, 10, 14, 10, 0, 0, 0, foundation.AStockLocation), false}} {
		item := etfScoreAdjustment(results[1], metrics.TotalPositionPercent, tc.at)
		if item.PremiumEligible != tc.eligible {
			t.Fatal(at, tc, item)
		}
	}
}

func TestETFRiskFloorAndProductSplitting(t *testing.T) {
	for _, tc := range []struct {
		p      float64
		weight int
		floor  string
	}{{5, 29, "低"}, {5, 30, "中"}, {10, 49, "中"}, {10, 50, "高"}} {
		req, results, metrics, report, at := etfScoringFixture(tc.p, tc.weight)
		report.RiskLevel = "低"
		if err := validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at); err != nil {
			t.Fatal(err)
		}
		if report.RiskLevel != tc.floor {
			t.Fatal(tc, report.RiskLevel)
		}
	}
	_, results, _, _, at := etfScoringFixture(.2, 50)
	one := etfScoreSummary(results, 100, at)
	fund := results[1]
	fund.Holding.Weight = 25
	results[1] = fund
	copyData, _ := json.Marshal(fund)
	var second HoldingResult
	_ = json.Unmarshal(copyData, &second)
	second.Holding.Symbol, second.Analysis.Symbol, second.Analysis.ETF.Profile.Symbol, second.Analysis.ETF.Execution.Snapshot.Symbol = "159919.SZ", "159919.SZ", "159919.SZ", "159919.SZ"
	results = append(results, second)
	two := etfScoreSummary(results, 100, at)
	if one.DiversificationBonus != two.DiversificationBonus || one.PremiumPenalty != two.PremiumPenalty {
		t.Fatal("splitting products manufactured diversification")
	}
}

func TestETFScoringRejectsDuplicateAdjustmentsAndImmediateActions(t *testing.T) {
	for _, where := range []string{"dimension", "holding", "order", "scenario", "price"} {
		req, results, metrics, report, at := etfScoringFixture(5, 50)
		switch where {
		case "dimension":
			report.Dimensions[2].Adjustments = []ScoreAdjustment{{RiskID: "etf_premium", Reason: "溢价扣分", Points: -20}}
		case "holding":
			report.Holdings[1].Action = "立即减仓50%"
		case "order":
			report.AdjustmentOrder = []string{"510300.SH 立即减仓50%"}
		case "scenario":
			report.Scenarios[0].PortfolioAction = "立即卖出所有ETF"
		case "price":
			report.AdjustmentOrder = []string{"510300.SH 止损价3.456"}
		}
		if err := validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at); err == nil {
			t.Fatal("accepted", where)
		}
	}
}

func TestETFDiversificationNeedsMandateNotMarketingName(t *testing.T) {
	_, results, _, _, at := etfScoringFixture(.2, 50)
	a := *results[1].Analysis.ETF
	a.Profile.TrackingTarget = ""
	a.Profile.Name = "沪深300ETF"
	if b, _ := etfDiversification(a, at); b != 0 {
		t.Fatal("name earned bonus")
	}
	a.Profile.TrackingTarget = "沪深300指数"
	a.Holdings = []foundation.ETFHoldings{{Symbol: a.Profile.Symbol, AssetType: "equity", ReportDate: "2026-09-30", Items: []foundation.ETFHolding{{Symbol: "600519.SH", WeightPercent: 25}}}}
	if b, _ := etfDiversification(a, at); b != 0 {
		t.Fatal("known concentration ignored")
	}
}

func TestETFDirectionBoundsAndNotification(t *testing.T) {
	req, results, metrics, report, at := etfScoringFixture(5, 50)
	a := results[1].Analysis.ETF
	a.Direction.Technical = &etfanalysis.DirectionTechnical{Symbol: "000300.SH", Relationship: "tracking_index", AsOf: "2026-10-13", Bars: make([]foundation.KLine, 120)}
	a.Direction.News = []etfanalysis.DirectionNews{{SourceID: "news1", Item: foundation.NewsItem{Content: strings.Repeat("消息", 10000)}}}
	before, _ := json.Marshal(a)
	f := portfolioFactsAt(req, results, metrics, at)
	if !f["510300.SH.etf.direction.technical"].Available {
		t.Fatal("missing direction fact")
	}
	d := a.PortfolioDirection()
	if len(d.Technical.Bars) != 0 || len([]rune(d.News[0].Item.Content)) > 401 {
		t.Fatal("unbounded summary")
	}
	after, _ := json.Marshal(a)
	if string(before) != string(after) {
		t.Fatal("original ETF snapshot mutated")
	}
	if err := validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at); err != nil {
		t.Fatal(err)
	}
	text := NotificationMarkdown(Job{Status: "succeeded", Request: req, Report: &Report{AlgorithmVersion: AlgorithmVersion, Holdings: results, Conclusion: report}})
	for _, expected := range []string{"股票与ETF组合", "ETF风险评分调整", "参考溢价5.00%"} {
		if !strings.Contains(text, expected) {
			t.Fatal(expected, text)
		}
	}
}

func TestPureETFAndOldDirectionReportsCanBeScored(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		req, results, _, report, at := etfScoringFixture(.2, 100)
		req.Holdings, results, report.Holdings = req.Holdings[1:], results[1:], report.Holdings[1:]
		if legacy {
			results[0].Analysis.ETF.Direction = nil
		}
		rules, _ := RulesFor(req.TraderProfile)
		metrics := CalculateMetrics(req, results, rules)
		if err := validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at); err != nil {
			t.Fatal(err)
		}
		if metrics.StockScoreCoveragePercent != 0 || metrics.HealthScoreAvailable || metrics.AIResearchCoveragePercent != 100 || !report.ScoreAvailable || report.ETFScoring.FinalRiskScore != 80 {
			t.Fatal(legacy, metrics, report)
		}
		encoded, _ := json.Marshal(report)
		var restored AIReport
		if err := json.Unmarshal(encoded, &restored); err != nil {
			t.Fatal(err)
		}
		if score, ok := VerifiedOptimizationScore(restored); !ok || score != *report.TotalScore {
			t.Fatal("persisted ETF score lost compatibility")
		}
	}
}

func TestETFPipelinePersistsTheSameFrozenFactsAsScoring(t *testing.T) {
	req, results, _, base, _ := etfScoringFixture(5, 50)
	now := time.Now().UTC()
	results[1].Analysis.ETF.Profile.Meta.FetchedAt = now.Add(-time.Hour)
	results[1].Analysis.ETF.Execution.Snapshot = foundation.ETFExecution{} // Missing paired estimate is explicit, not zero premium.
	results[1].ResearchCutoffAt = now
	results[1].Analysis.ResearchReport.CutoffAt = now
	data, _ := json.Marshal(base)
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gateway := &scoreGateway{content: string(data)}
	service := NewService(store, gateway, nil, nil)
	defer service.Close()
	service.ConfigureResearch(func(_ context.Context, h Holding, _ Request, _ time.Time, _ bool, _ string, _ func(HoldingResult)) (HoldingResult, error) {
		for _, r := range results {
			if r.Holding.Symbol == h.Symbol {
				return r, nil
			}
		}
		return HoldingResult{}, nil
	}, nil)
	job, err := service.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	done := waitPortfolio(t, service, job.ID)
	if done.Status != "succeeded" || done.Report.Conclusion.ETFScoring == nil {
		t.Fatal(done.Error)
	}
	e := done.Report.Conclusion.ETFScoring
	if e.DiversificationBonus != 4 || e.PremiumPenalty != 0 || !e.EvaluatedAt.Equal(done.Report.EvaluatedAt) || !e.EvaluatedAt.Equal(done.AggregationStartedAt) {
		t.Fatal(e, done.Report.EvaluatedAt)
	}
	f := done.Report.Facts["510300.SH.etf.premium_scoring_eligible"]
	if !f.AsOf.Equal(e.EvaluatedAt) || f.Value != false {
		t.Fatal("fact table used a different scoring time", f)
	}
}

func TestOptimizationUsesTheSameETFRulesAndFrozenTime(t *testing.T) {
	req, results, metrics, base, at := etfScoringFixture(10, 50)
	data, _ := json.Marshal(base)
	inspection := base
	if err := validateScoringReportFactsAt(&inspection, req, results, metrics, true, nil, at); err != nil {
		t.Fatal(err)
	}
	r := OptimizationReport(req, results, at)
	comparison, err := DecodeOptimizationComparisonScore(data, r)
	if err != nil {
		t.Fatal(err)
	}
	if *comparison.TotalScore != *inspection.TotalScore || comparison.ETFScoring.PremiumPenalty != inspection.ETFScoring.PremiumPenalty || !comparison.ETFScoring.EvaluatedAt.Equal(at) {
		t.Fatal("inspection and review used different ETF scoring", comparison, inspection)
	}
}

func TestBroadETFIsNotPenalizedAsOneConcentratedCompany(t *testing.T) {
	for _, target := range []string{"沪深300指数", "中证半导体指数"} {
		req, results, _, report, at := etfScoringFixture(.2, 100)
		req.Holdings, results, report.Holdings = req.Holdings[1:], results[1:], report.Holdings[1:]
		results[0].Analysis.ETF.Profile.TrackingTarget = target
		score := 85
		report.Dimensions[1].Score = &score
		rules, _ := RulesFor(req.TraderProfile)
		metrics := CalculateMetrics(req, results, rules)
		err := validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at)
		if target == "沪深300指数" && err != nil {
			t.Fatal("broad ETF treated as one company", err)
		}
		if target != "沪深300指数" && (err == nil || !strings.Contains(err.Error(), "集中度")) {
			t.Fatal("unverified sector concentration ignored", err)
		}
	}
}
