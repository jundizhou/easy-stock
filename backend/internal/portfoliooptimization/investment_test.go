package portfoliooptimization

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	pi "easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
)

func TestCitationOnlyRepairFreezesEveryInvestmentField(t *testing.T) {
	j, p := fixtureJob(), fixtureProposal()
	c := InvestmentComparison{FromSymbol: "600519.SH", ToSymbol: "000858.SZ", Dimension: "business", Reason: "接收资金方经营更适合本周期", Tradeoff: "放弃原弹性", EvidenceRefs: []pi.EvidenceRef{{ReportID: "report-000858.SZ", SourceID: "s1"}}}
	p.InvestmentComparisons = []InvestmentComparison{c}
	var missing *comparisonEvidenceError
	if !errors.As(validateProposal(j, p), &missing) {
		t.Fatal("missing peer citation was accepted")
	}
	before, _ := json.Marshal(p)
	patch := `{"comparison_refs":[{"from_symbol":"600519.SH","to_symbol":"000858.SZ","dimension":"business","evidence_refs":[{"report_id":"report-600519.SH","source_id":"s1"},{"report_id":"report-000858.SZ","source_id":"s1"}]}]}`
	updated, err := repairComparisonReferences(j, p, patch)
	if err != nil {
		t.Fatal(err)
	}
	updated.InvestmentComparisons[0].EvidenceRefs = p.InvestmentComparisons[0].EvidenceRefs
	if !reflect.DeepEqual(updated, p) {
		t.Fatal("citation patch changed investment judgments or constraints")
	}
	after, _ := json.Marshal(p)
	if string(before) != string(after) {
		t.Fatal("original proposal was mutated")
	}
	for _, bad := range []string{
		strings.Replace(patch, `"dimension":"business"`, `"dimension":"business","max_weight":100`, 1),
		strings.Replace(patch, `"dimension":"business"`, `"dimension":"growth"`, 1),
		strings.Replace(patch, `"source_id":"s1"`, `"source_id":"invented"`, 1),
		`{"comparison_refs":[]}`,
		patch + " trailing",
	} {
		if _, err := repairComparisonReferences(j, p, bad); err == nil {
			t.Fatal("unsafe or incomplete citation patch accepted", bad)
		}
	}
	missing.proposal = &p
	missing.needed = []InvestmentComparison{c}
	prompt, _ := proposalPrompt(j)
	data := strings.Split(prompt, "[资料JSON]\n")[1]
	repair := modelRepairPrompt(prompt, strings.Repeat("oversized proposal", 10000), missing)
	if len(repair) > MaxModelPromptBytes || !strings.Contains(repair, "[资料JSON]\n"+data) || !strings.Contains(repair, "comparison_refs") || strings.Contains(repair, "oversized proposal") {
		t.Fatal("citation repair repeated full proposal or lost facts", len(repair))
	}
}

func semanticExit(a *Allocation) {
	a.ConfirmationIDs, a.InvalidationIDs = nil, nil
	a.Conditions = []AllocationCondition{{Kind: "exit", Text: "主营盈利持续恶化时退出", Verification: "核对下一期披露与原经营逻辑", Status: "pending", EvidenceRefs: a.EvidenceRefs}}
}

func TestProfitTerminologyCannotInventCoreProfit(t *testing.T) {
	j, p := fixtureJob(), fixtureProposal()
	a := &p.Alternatives[0].Allocations[0]
	a.Investment.Business = "归母利润全为主营盈利"
	if err := validateProposal(j, p); err == nil || !strings.Contains(err.Error(), "口径不同") {
		t.Fatal("unproven core profit claim accepted", err)
	}
	a.Investment.Business = "归母不是全为主营，须分别核对扣非"
	if err := validateProposal(j, p); err != nil {
		t.Fatal("explicit caveat rejected", err)
	}
	c := InvestmentComparison{FromSymbol: "600519.SH", ToSymbol: "000858.SZ", Dimension: "business", Reason: "归母全为经营利润，比其他股确定", Tradeoff: "牺牲弹性", EvidenceRefs: []pi.EvidenceRef{{ReportID: "report-600519.SH", SourceID: "s1"}, {ReportID: "report-000858.SZ", SourceID: "s1"}}}
	if validateInvestmentComparison(j, c) == nil {
		t.Fatal("same invented claim hidden in investment comparison")
	}
}

func TestReducingLossMakingVolatileStockDoesNotMakeItsRoleDefensive(t *testing.T) {
	j, p := fixtureJob(), fixtureProposal()
	a := &p.Alternatives[0].Allocations[0]
	r := &j.Results[0]
	r.Analysis.Trend.ATR14Percent = 6.24
	cutoff := r.Analysis.ResearchReport.CutoffAt
	payload, _ := json.Marshal(map[string]any{"data": map[string]any{"report_date": cutoff.Format("2006-01-02"), "net_profit": -100, "deducted_net_profit": -120, "deducted_net_profit_available": true}})
	r.Analysis.ResearchReport.Sources = append(r.Analysis.ResearchReport.Sources, stockanalysis.ResearchSource{ID: "f-financial", CapturedAt: cutoff, Content: string(payload)})
	a.Investment.Role, a.Investment.Action, a.Suitable = "防守", "reduce", false
	if err := validateProposal(j, p); err == nil || !strings.Contains(err.Error(), "减仓操作不代表") {
		t.Fatal("reduction confused with defensive asset", err)
	}
	a.Investment.Role = "进攻"
	if err := validateProposal(j, p); err != nil {
		t.Fatal("role correction must not prohibit keeping/reducing stock", err)
	}
}

func TestMeasuredPairCorrelationCoversBothStocksOnlyForRiskAndFit(t *testing.T) {
	j := fixtureJob()
	for _, r := range j.Results {
		for i := 0; i < 25; i++ {
			r.Analysis.Chart = append(r.Analysis.Chart, stockanalysis.TrendPoint{Date: j.AsOf.AddDate(0, 0, i-30).Format("2006-01-02"), Close: 100 + float64(i*i+i%3)})
		}
	}
	c := InvestmentComparison{FromSymbol: "600519.SH", ToSymbol: "000858.SZ", Dimension: "portfolio_fit", Reason: "两股已计算的共同波动", Tradeoff: "历史关系不保证未来", EvidenceRefs: []pi.EvidenceRef{{Fact: "correlation.600519.SH.000858.SZ"}}}
	if err := validateInvestmentComparison(j, c); err != nil {
		t.Fatal("real pair fact was treated as unrelated", err)
	}
	c.Dimension = "business"
	if validateInvestmentComparison(j, c) == nil {
		t.Fatal("correlation used to prove business quality")
	}
	c.Dimension = "risk"
	c.EvidenceRefs[0].Fact = "correlation.600519.SH.unknown"
	if validateInvestmentComparison(j, c) == nil {
		t.Fatal("unmeasured or unrelated pair accepted")
	}
	c.EvidenceRefs[0].Fact = "correlation.600519.SH.000858.SZ"
	for _, r := range j.Results {
		r.Analysis.Chart = r.Analysis.Chart[:10]
	}
	if validateInvestmentComparison(j, c) == nil {
		t.Fatal("insufficient samples treated as known correlation")
	}
}

func TestSharedBenchmarkCorrelationsSupportExposureComparison(t *testing.T) {
	j, _ := searchFixture(t)
	for _, r := range j.Results {
		for i := 0; i < 25; i++ {
			r.Analysis.Chart = append(r.Analysis.Chart, stockanalysis.TrendPoint{Date: j.AsOf.AddDate(0, 0, i-30).Format("2006-01-02"), Close: 100 + float64(i*i+i%3)})
		}
	}
	c := InvestmentComparison{FromSymbol: "600519.SH", ToSymbol: "000858.SZ", Dimension: "portfolio_fit", Reason: "两股与同一原持股的真实相关性支持比较共同驱动", Tradeoff: "历史相关性不保证未来", EvidenceRefs: []pi.EvidenceRef{{Fact: "correlation.600519.SH.000001.SZ"}, {Fact: "correlation.000858.SZ.000001.SZ"}}}
	if err := validateInvestmentComparison(j, c); err != nil {
		t.Fatal("valid shared-reference exposure rejected", err)
	}
	c.Dimension = "growth"
	if validateInvestmentComparison(j, c) == nil {
		t.Fatal("correlation used as earnings evidence")
	}
	c.Dimension = "risk"
	c.EvidenceRefs = c.EvidenceRefs[:1]
	if validateInvestmentComparison(j, c) == nil {
		t.Fatal("second stock not covered")
	}
	c.EvidenceRefs = append(c.EvidenceRefs, pi.EvidenceRef{Fact: "correlation.000858.SZ.invented"})
	if validateInvestmentComparison(j, c) == nil {
		t.Fatal("invented correlation accepted")
	}
}

func TestUnselectedWaitingCandidateCannotReceiveFunds(t *testing.T) {
	j, p := fixtureJob(), fixtureProposal()
	j.Source.Request.Holdings = j.Source.Request.Holdings[:1]
	j.Source.Request.Holdings[0].Weight = 80
	j.Results[1].Holding.Weight = 0
	a, b := &p.Alternatives[0].Allocations[0], &p.Alternatives[0].Allocations[1]
	a.Minimum, a.Maximum, a.Preferred = 80, 80, 80
	b.Minimum, b.Maximum, b.Preferred = 0, 0, 0
	b.Investment.Action, b.Suitable = "wait", false
	semanticExit(b)
	b.Conditions = append(b.Conditions, AllocationCondition{Kind: "entry", Text: "价格回到合适位置再评估", Verification: "核对量价与经营", Status: "pending", EvidenceRefs: b.EvidenceRefs})
	if err := validateProposal(j, p); err != nil {
		t.Fatal("waiting zero candidate blocked the whole portfolio", err)
	}
	if canIncrease(j, *b) {
		t.Fatal("zero candidate received funding permission")
	}
	b.Maximum = 1
	if validateProposal(j, p) == nil {
		t.Fatal("nonzero waiting range bypassed conditional funding permission")
	}
}

func TestInvestmentAllocationIndependentOfReportLabels(t *testing.T) {
	for _, level := range []string{"sufficient", "limited", "insufficient"} {
		for _, status := range []string{"conditional", "observe", "no_plan"} {
			t.Run(level+"/"+status, func(t *testing.T) {
				j, p := fixtureJob(), fixtureProposal()
				for _, r := range j.Results {
					r.Analysis.ResearchReport.EvidenceLevel = level
					r.Analysis.ResearchReport.Decision.Status = status
					r.Analysis.ResearchReport.Decision.PricePlan = nil
				}
				for i := range p.Alternatives[0].Allocations {
					semanticExit(&p.Alternatives[0].Allocations[i])
				}
				if err := validateProposal(j, p); err != nil {
					t.Fatal(err)
				}
				target, err := Solve(j, p.Alternatives[0])
				if err != nil || !equalWeights(target, holds(45, 35)) {
					t.Fatal(target, err)
				}
			})
		}
	}
}

func TestInvestmentActionConditionsAndPriceIntegrity(t *testing.T) {
	for _, action := range []string{"hold", "reduce"} {
		j, p := fixtureJob(), fixtureProposal()
		a := &p.Alternatives[0].Allocations[1]
		a.Investment.Action, a.Suitable = action, false
		if err := validateProposal(j, p); err != nil {
			t.Fatal(err)
		}
		if _, err := Solve(j, p.Alternatives[0]); err == nil {
			t.Fatal("hold/reduce received additional funds", action)
		}
		a.Suitable = true
		if validateProposal(j, p) == nil {
			t.Fatal("permission contradicted action", action)
		}
	}
	j, p := fixtureJob(), fixtureProposal()
	a := &p.Alternatives[0].Allocations[1]
	semanticExit(a)
	a.Investment.Action = "wait"
	if validateProposal(j, p) == nil {
		t.Fatal("wait accepted without entry observation")
	}
	a.Conditions = append(a.Conditions, AllocationCondition{Kind: "entry", Text: "价格回到可接受位置后重新评估", Verification: "核对当时行情与估值", Status: "pending", EvidenceRefs: a.EvidenceRefs})
	if err := validateProposal(j, p); err != nil {
		t.Fatal(err)
	}
	rr := j.Results[1].Analysis.ResearchReport
	rr.Decision.Status = "no_plan"
	rr.Anchors = []stockanalysis.PriceAnchor{{ID: "actual-price", Price: 100, SourceID: "s1"}}
	price := 100.0
	c := &a.Conditions[1]
	c.AnchorID, c.Operator, c.Threshold = "actual-price", "lte", &price
	if err := validateProposal(j, p); err != nil {
		t.Fatal("actual anchor must work without original plan", err)
	}
	price = 99
	if validateProposal(j, p) == nil {
		t.Fatal("fabricated typed price accepted")
	}
	price = 100
	c.Verification = "买入价99"
	if validateProposal(j, p) == nil {
		t.Fatal("fabricated textual price accepted")
	}
	c.Verification, c.Status = "观察实际行情", "satisfied"
	if validateProposal(j, p) == nil {
		t.Fatal("condition falsely marked satisfied")
	}
	c.Status = "pending"
	a.Investment.Valuation = "买入价99"
	if validateProposal(j, p) == nil {
		t.Fatal("fabricated price hidden in investment judgment")
	}
}

func investmentOnlyFixture() (Job, Proposal) {
	j, p := fixtureJob(), fixtureProposal()
	j.ID, j.Fingerprint, j.Version = "investment-only", "ab", Version
	j.Source.Request.Holdings = holds(30, 30)
	j.Baseline = j.Source.Request.Holdings
	for i := range j.Results {
		j.Results[i].Holding = j.Baseline[i]
	}
	j.Source = pi.OptimizationReport(j.Source.Request, j.Results)
	j.SnapshotAt = time.Now().UTC()
	for i, w := range []int{25, 35} {
		a := &p.Alternatives[0].Allocations[i]
		a.Minimum, a.Maximum, a.Preferred = w, w, w
		semanticExit(a)
	}
	p.InvestmentComparisons = []InvestmentComparison{{FromSymbol: "600519.SH", ToSymbol: "000858.SZ", Dimension: "business", Reason: "接收资金方主营盈利质量更适合本周期", Tradeoff: "减少另一股的经营弹性", EvidenceRefs: []pi.EvidenceRef{{ReportID: "report-600519.SH", SourceID: "s1"}, {ReportID: "report-000858.SZ", SourceID: "s1"}}}}
	return j, p
}

type investmentGateway struct {
	*testGateway
	proposal Proposal
	confirm  bool
}

func (g *investmentGateway) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	if strings.Contains(prompt, "allocation_ranges") {
		g.calls.Add(1)
		return agent.PromptResult{Content: `{"allocation_ranges":[],"keep_reason":"当前固定范围没有其他合理配置"}`}, nil
	}
	if !strings.Contains(prompt, "独立组合复评员") {
		g.calls.Add(1)
		b, _ := json.Marshal(g.proposal)
		return agent.PromptResult{Content: string(b)}, nil
	}
	response, err := g.testGateway.Prompt(ctx, prompt)
	if err != nil {
		return response, err
	}
	var decoded struct {
		A, B       pi.AIReport
		Assessment Assessment
	}
	if err := json.Unmarshal([]byte(response.Content), &decoded); err != nil {
		return response, err
	}
	// Accept the investment tradeoff even with a one-point lower risk score.
	chosen := &decoded.B
	if decoded.Assessment.Preferred == "a" {
		chosen = &decoded.A
	}
	for i := range chosen.Dimensions {
		if chosen.Dimensions[i].Key == "risk_capacity" {
			score := 69
			chosen.Dimensions[i].Score = &score
		}
	}
	if g.confirm {
		decoded.Assessment.InvestmentComparisons = []ReviewedInvestmentComparison{{PreferredSymbol: "000858.SZ", OtherSymbol: "600519.SH", Dimension: "business", Reason: "独立比较后盈利角色更合适", Tradeoff: "仍有经营波动", EvidenceRefs: g.proposal.InvestmentComparisons[0].EvidenceRefs}}
	}
	b, _ := json.Marshal(decoded)
	response.Content = string(b)
	return response, nil
}

func TestInvestmentOnlyChangesRequireIndependentConfirmation(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		for _, order := range []string{"original_first", "target_first"} {
			t.Run(order+"/"+map[bool]string{false: "score-only", true: "confirmed"}[confirm], func(t *testing.T) {
				base := &testGateway{}
				svc, _, _ := setupService(t, base)
				j, p := investmentOnlyFixture()
				svc.gateway = &investmentGateway{testGateway: base, proposal: p, confirm: confirm}
				// Opposite leading byte parities produce opposite blind A/B orders.
				if order == "target_first" {
					j.Fingerprint = "bb"
				}
				if err := svc.execute(context.Background(), &j); err != nil {
					t.Fatal(err)
				}
				var plan Plan
				if confirm {
					if len(j.Plans) != 1 || j.Plans[0].Assessment == nil || base.calls.Load() != 2 {
						t.Fatal("confirmed review missing", j.Outcome)
					}
					plan = j.Plans[0]
				} else {
					if len(j.RevisionHistory) != 1 || base.calls.Load() != 3 || j.RevisionCount != 1 {
						t.Fatal("rejected high score did not trigger one bounded revision")
					}
					prior := j.RevisionHistory[0]
					plan = Plan{AssessmentOrder: order, Improvements: prior.Improvements, Assessment: prior.Assessment, Proposed: pi.Report{Conclusion: prior.Conclusion}}
					if !strings.Contains(prior.Error, "改善依据") || !strings.Contains(j.OutcomeReason, prior.Error) {
						t.Fatal("specific rejection missing", prior.Error)
					}
				}
				if plan.AssessmentOrder != order || len(plan.Improvements) != 1 || plan.Improvements[0].Kind != "investment" {
					t.Fatal("unexpected structural improvement or review order", plan)
				}
				if (j.SelectedPlan != nil) != confirm {
					t.Fatal("score alone accepted or independent confirmation ignored", j.Outcome)
				}
				if confirm && (plan.Proposed.Conclusion.Dimensions[2].Score == nil || *plan.Proposed.Conclusion.Dimensions[2].Score != 69) {
					t.Fatal("risk score decrease not retained")
				}
				if strings.Contains(base.pairedPrompt, "接收资金方主营盈利质量更适合本周期") {
					t.Fatal("optimizer comparison leaked into blind review")
				}
			})
		}
	}
}

func TestInvestmentComparisonsMatchActualFundingAndBlindDirection(t *testing.T) {
	j, p := investmentOnlyFixture()
	j.Proposal = &p
	target := holds(25, 35)
	plan := Plan{Target: target, Original: j.Source, AssessmentOrder: "original_first", Improvements: measureImprovements(j, target)}
	if len(plan.Improvements) != 1 || plan.Improvements[0].Weight != 5 {
		t.Fatal(plan.Improvements)
	}
	c := ReviewedInvestmentComparison{PreferredSymbol: "000858.SZ", OtherSymbol: "600519.SH", Dimension: "business", Reason: "主营质量比较", Tradeoff: "减少弹性", EvidenceRefs: p.InvestmentComparisons[0].EvidenceRefs}
	r := Assessment{Preferred: "b", InvestmentComparisons: []ReviewedInvestmentComparison{c}}
	if err := validateReviewedComparisons(j, plan, r); err != nil || !reviewConfirmsImprovement(j, plan, r) {
		t.Fatal(err)
	}
	r.Preferred = "a"
	if validateReviewedComparisons(j, plan, r) == nil {
		t.Fatal("reversed funding direction accepted")
	}
	r.Preferred = "b"
	r.InvestmentComparisons[0].Dimension = "growth"
	if !reviewConfirmsImprovement(j, plan, r) {
		t.Fatal("valid independent dimension rejected")
	}
	r.InvestmentComparisons[0].EvidenceRefs = []pi.EvidenceRef{{ReportID: "invented", SourceID: "s1"}}
	if validateReviewedComparisons(j, plan, r) == nil {
		t.Fatal("invented references accepted")
	}
	p.InvestmentComparisons[0].FromSymbol, p.InvestmentComparisons[0].ToSymbol = "000858.SZ", "600519.SH"
	if len(measureImprovements(j, target)) != 0 {
		t.Fatal("unrelated funding comparison accepted")
	}
	j.Proposal.RiskGroups = []pi.RiskGroup{{Name: "共同风险", Symbols: []string{"000858.SZ"}}}
	plan.Proposed = pi.OptimizationReport(pi.Request{TraderProfile: pi.ProfileBalanced, Horizon: "swing", Holdings: target}, j.Results)
	plan.Proposed.Metrics.HighRiskPercent = 100
	if riskAcceptable(j, plan) {
		t.Fatal("real high-risk exposure limit bypassed")
	}
}

func TestCandidateDossierPreservesOpinionsAndFactsWithoutMutatingReport(t *testing.T) {
	j := fixtureJob()
	r := &j.Results[1]
	r.Holding.Weight = 0
	rr := r.Analysis.ResearchReport
	rr.EvidenceLevel, rr.Decision.Status, rr.Decision.NewPosition = "insufficient", "no_plan", "原研究反对新仓"
	rr.Support = []stockanalysis.ResearchClaim{{Text: "主营仍可核验", SourceIDs: []string{"s1"}}}
	rr.Counter = []stockanalysis.ResearchClaim{{Text: "业务有负面事实", SourceIDs: []string{"s1"}}}
	before, _ := json.Marshal(rr)
	prompt, err := proposalPrompt(j)
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"主营仍可核验", "业务有负面事实", "原研究反对新仓", "000858.SZ", `"price"`} {
		if !strings.Contains(prompt, word) {
			t.Fatal("candidate input lost", word)
		}
	}
	after, _ := json.Marshal(rr)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("original report mutated")
	}
}

func TestCandidateFitUsesKnownIndustriesOnly(t *testing.T) {
	j := fixtureJob()
	catalog := map[string]string{"600519.SH": "白酒", "000858.SZ": "白酒"}
	if bonus, _ := CandidateFit(j.Source, catalog, "银行"); bonus != 10 {
		t.Fatal(bonus)
	}
	if bonus, _ := CandidateFit(j.Source, catalog, "白酒"); bonus != -10 {
		t.Fatal(bonus)
	}
	delete(catalog, "000858.SZ")
	if bonus, _ := CandidateFit(j.Source, catalog, "银行"); bonus != 0 {
		t.Fatal("unknown assumed diversified")
	}
}

func TestUnchosenResearchMustHaveExplicitInvestmentReason(t *testing.T) {
	j, p := fixtureJob(), fixtureProposal()
	encoded, _ := json.Marshal(j.Results[0])
	var r pi.HoldingResult
	if err := json.Unmarshal(encoded, &r); err != nil {
		t.Fatal(err)
	}
	r.Holding = pi.Holding{Symbol: "000001.SZ", Name: "未选候选"}
	r.AnalysisID = "report-000001.SZ"
	r.Analysis.Symbol = r.Holding.Symbol
	j.Results = append(j.Results, r)
	if validateProposal(j, p) == nil {
		t.Fatal("candidate omitted without investment explanation")
	}
	a := p.Alternatives[0].Allocations[0]
	a.Symbol, a.Minimum, a.Maximum, a.Preferred = r.Holding.Symbol, 0, 0, 0
	a.Investment = fixtureInvestment()
	a.Investment.Action = "hold"
	a.Suitable = false
	a.Reason = "可核验但本次配置用途不如其他股票，不纳入"
	a.EvidenceRefs = []pi.EvidenceRef{{ReportID: r.AnalysisID, SourceID: "s1"}}
	p.Alternatives[0].Allocations = append(p.Alternatives[0].Allocations, a)
	if err := validateProposal(j, p); err != nil {
		t.Fatal(err)
	}
	target, err := Solve(j, p.Alternatives[0])
	if err != nil || !equalWeights(target, holds(45, 35)) {
		t.Fatal(target, err)
	}
}

func TestWholePortfolioIndustryClaimUsesActualFrozenMembers(t *testing.T) {
	j := fixtureJob()
	j.Proposal = &Proposal{RiskGroups: []pi.RiskGroup{{Name: "半导体板块贝塔", Symbols: []string{"600519.SH"}}}}
	score := pi.AIReport{ExecutiveSummary: "全为芯片设计"}
	if validateWholePortfolioIndustryClaim(j, j.Source, score) == nil {
		t.Fatal("mixed holdings relabelled as all chips")
	}
	for _, text := range []string{"芯片暴露75%，仍有其他业务", "并非全为芯片设计", "不是全为半导体资产", "不全为芯片设计"} {
		score.ExecutiveSummary = text
		if err := validateWholePortfolioIndustryClaim(j, j.Source, score); err != nil {
			t.Fatal(err)
		}
	}
	j.Proposal.RiskGroups[0].Symbols = append(j.Proposal.RiskGroups[0].Symbols, "000858.SZ")
	score.ExecutiveSummary = "全为芯片设计"
	if err := validateWholePortfolioIndustryClaim(j, j.Source, score); err != nil {
		t.Fatal("true full exposure rejected", err)
	}
}

func TestCurrentDeductedProfitClaimUsesAmountNotGrowthRate(t *testing.T) {
	symbol := "301536.SZ"
	facts := map[string]pi.Fact{symbol + ".financial.deducted_net_profit": {Value: 711600000.0, Available: true}}
	for _, good := range []string{"中报扣非7.12亿，营收增长", "扣非净利润约7.116亿元", "扣非+644%", "去年扣非6.44亿，当前增长"} {
		if err := validateCurrentDeductedProfit(symbol, good, facts); err != nil {
			t.Fatal(good, err)
		}
	}
	if validateCurrentDeductedProfit(symbol, "中报扣非6.44亿、毛利率55.36%", facts) == nil {
		t.Fatal("growth percentage interpreted as profit amount")
	}
	facts[symbol+".financial.deducted_net_profit"] = pi.Fact{Value: -58700000.0, Available: true}
	if err := validateCurrentDeductedProfit(symbol, "扣非亏损0.59亿", facts); err != nil {
		t.Fatal(err)
	}
	if validateCurrentDeductedProfit(symbol, "扣非盈利0.59亿", facts) == nil {
		t.Fatal("loss described as profit")
	}
	facts[symbol+".financial.deducted_net_profit"] = pi.Fact{Available: false}
	if err := validateCurrentDeductedProfit(symbol, "扣非6.44亿", facts); err != nil {
		t.Fatal("missing fact became an evidence permission gate", err)
	}
}
