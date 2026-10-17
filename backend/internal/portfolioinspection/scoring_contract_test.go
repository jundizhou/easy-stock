package portfolioinspection

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/stockanalysis"
)

// Old scoring fixtures describe economic judgments using canonical refs. The
// fake model now selects IDs from its actual prompt, just like the live model.
// Unknown refs are retained so invalid-evidence tests still fail validation.
func fixtureContractReport(report AIReport, catalog scoringCatalog) AIReport {
	data, _ := json.Marshal(report)
	var cloned AIReport
	_ = json.Unmarshal(data, &cloned)
	report = cloned
	report.ContractVersion = scoringContractVersion
	walkScoringRefs(&report, func(_ string, ref *EvidenceRef) {
		for _, e := range catalog.Evidence {
			if *ref == e.Ref {
				*ref = EvidenceRef{EvidenceID: e.ID}
				return
			}
		}
	})
	bySymbol := map[string]string{}
	for _, a := range catalog.Actions {
		if a.Kind == "observe" {
			bySymbol[a.Symbol] = a.ID
		}
	}
	var all []string
	for i := range report.Holdings {
		h := &report.Holdings[i]
		h.ActionID = bySymbol[h.Symbol]
		h.Action = ""
		h.Confirmation = ""
		h.Invalidation = ""
		all = append(all, h.ActionID)
	}
	report.AdjustmentOrder = nil
	report.AdjustmentActionIDs = all
	for i := range report.Scenarios {
		report.Scenarios[i].PortfolioAction = ""
		report.Scenarios[i].ActionIDs = all
	}
	return report
}

func TestScoringContractPreservesMixedPortfolioAndPremiumScores(t *testing.T) {
	for _, premium := range []float64{.2, 5, 12} {
		req, results, metrics, base, at := etfScoringFixture(premium, 50)
		structured := fixtureContractReport(base, buildScoringCatalog(req, results, metrics, at))
		if err := validateScoringReportFactsAt(&base, req, results, metrics, true, nil, at); err != nil {
			t.Fatal(err)
		}
		if err := validateScoringReportFactsAt(&structured, req, results, metrics, true, nil, at); err != nil {
			t.Fatal(err)
		}
		if *structured.TotalScore != *base.TotalScore || structured.ETFScoring.PremiumPenalty != base.ETFScoring.PremiumPenalty || structured.ETFScoring.DiversificationBonus != base.ETFScoring.DiversificationBonus {
			t.Fatal("output contract changed scoring policy", premium)
		}
	}
}

func fixtureModelScoringResponse(content, prompt string) string {
	_, payload, ok := strings.Cut(prompt, "[组合证据JSON]\n")
	if !ok {
		_, repairPayload, repairing := strings.Cut(prompt, "[局部修复JSON]\n")
		if !repairing {
			return content
		}
		var repair struct {
			Input json.RawMessage `json:"evidence"`
		}
		if json.Unmarshal([]byte(repairPayload), &repair) != nil {
			return content
		}
		payload = string(repair.Input)
	}
	var catalog scoringCatalog
	if json.NewDecoder(strings.NewReader(payload)).Decode(&catalog) != nil {
		return content
	}

	report, err := decodeScoringReport(content)
	if err != nil || report.ContractVersion != "" {
		return content
	}
	data, _ := json.Marshal(fixtureContractReport(report, catalog))
	return string(data)
}

func TestScoringContractResolvesOnlyCatalogEvidence(t *testing.T) {
	req, results, metrics, base, at := etfScoringFixture(.2, 50)
	catalog := buildScoringCatalog(req, results, metrics, at)
	for _, kind := range []string{"valid", "invented", "mixed_reference", "wrong_action_owner", "fake_permission", "free_action"} {
		t.Run(kind, func(t *testing.T) {
			data, _ := json.Marshal(base)
			var report AIReport
			_ = json.Unmarshal(data, &report)
			report = fixtureContractReport(report, catalog)
			switch kind {
			case "invented":
				report.Dimensions[0].EvidenceRefs[0].EvidenceID = "ev-does-not-exist"
			case "mixed_reference":
				report.Dimensions[0].EvidenceRefs[0].ReportID = "another-report"
			case "wrong_action_owner":
				report.Holdings[1].ActionID = report.Holdings[0].ActionID
			case "fake_permission":
				report.Actions = []ScoringAction{{ExecutionStatus: "ready"}}
			case "free_action":
				report.Scenarios[0].PortfolioAction = "立即买入ETF"
			}
			before, _ := json.Marshal(report)
			err := validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at)
			if (err == nil) != (kind == "valid") {
				t.Fatal(kind, err)
			}
			if err != nil {
				after, _ := json.Marshal(report)
				if string(before) != string(after) {
					t.Fatal("failed validation mutated candidate")
				}
			}
			if err == nil && (report.Dimensions[0].EvidenceRefs[0].Fact != "concentration_hhi" || !report.ScoreAvailable) {
				t.Fatal("catalog reference not resolved")
			}
		})
	}
}

func TestScoringContractActionsCarryFrozenPrerequisitesAtEveryOutlet(t *testing.T) {
	req, results, metrics, base, at := etfScoringFixture(.2, 50)
	r := &results[1]
	rr := r.Analysis.ResearchReport
	r.Analysis.ETF.Bars = make([]foundation.KLine, 20)
	rr.Sources = append(rr.Sources, stockanalysis.ResearchSource{ID: "m-price", Kind: "calculation"})
	rr.Anchors = []stockanalysis.PriceAnchor{{ID: "stop", Price: .957, SourceID: "m-price"}}
	rr.Conditions = []stockanalysis.ResearchCondition{{ID: "c2", Text: "收盘有效跌破原失效线", Metric: "close", Operator: "lte", AnchorID: "stop", Window: "1d", SourceIDs: []string{"m-price"}}}
	rr.InvalidationIDs = []string{"c2"}
	rr.Decision.Status = "conditional"
	rr.Decision.PricePlan = &stockanalysis.AnchoredPricePlan{StopAnchor: "stop", SourceIDs: []string{"m-price"}}
	catalog := buildScoringCatalog(req, results, metrics, at)
	var reduction ScoringAction
	for _, a := range catalog.Actions {
		if a.Symbol == r.Holding.Symbol && a.Kind == "conditional_reduce" {
			reduction = a
		}
	}
	if reduction.ID == "" {
		t.Fatal("missing supported conditional action")
	}
	for _, condition := range []string{"市场走弱", "ETF触发失效条件；执行前核验", "核验完成后观察，其他股票仍待确认"} {
		data, _ := json.Marshal(base)
		var report AIReport
		_ = json.Unmarshal(data, &report)
		report = fixtureContractReport(report, catalog)
		report.Holdings[1].ActionID = reduction.ID
		report.AdjustmentActionIDs = []string{reduction.ID}
		report.Scenarios[0].ActionIDs = []string{reduction.ID}
		report.Scenarios[0].Condition = condition
		if err := validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at); err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{report.Holdings[1].Action, report.AdjustmentOrder[0], report.Scenarios[0].PortfolioAction} {
			if text != reduction.Text || !strings.Contains(text, "交易规则须核验") || !strings.Contains(text, "0.9570") {
				t.Fatal("action outlet lost frozen conditions", text)
			}
		}
		if reduction.ExecutionStatus != "pending_verification" || len(results[1].Analysis.ETF.ExecutionBlockers) == 0 {
			t.Fatal("execution was authorized")
		}
	}
	rr.Decision.Status = "no_plan"
	for _, a := range scoringActionsFor(*r) {
		if strings.HasPrefix(a.Kind, "conditional_") {
			t.Fatal("invented plan for no_plan")
		}
	}
}

func TestScoringCollectsEvidenceAndActionErrorsBeforeRepair(t *testing.T) {
	req, results, metrics, base, at := etfScoringFixture(.2, 50)
	base.Dimensions[0].EvidenceRefs = []EvidenceRef{{ReportID: results[1].AnalysisID, SourceID: "s1"}, {Fact: "688220.SZ.financial.deducted_net_profit"}}
	base.Scenarios[0].PortfolioAction = "执行ETF失效减仓"
	before, _ := json.Marshal(base)
	err := validateScoringReportFactsAt(&base, req, results, metrics, true, nil, at)
	for _, want := range []string{"source_id=s1", "688220.SZ", "scenarios[0].portfolio_action"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal("lost independent error", want, err)
		}
	}
	after, _ := json.Marshal(base)
	if string(before) != string(after) {
		t.Fatal("invalid report changed")
	}

	_, _, _, valid, _ := etfScoringFixture(.2, 50)
	valid = fixtureContractReport(valid, buildScoringCatalog(req, results, metrics, at))
	repaired, _ := json.Marshal(valid)
	g := &scoringReplayGateway{responses: []json.RawMessage{before, repaired}}
	s := NewService(nil, g, nil, nil)
	rules, _ := RulesFor(req.TraderProfile)
	result, err := s.scorePortfolio(context.Background(), req, results, metrics, rules, at)
	if err != nil || !result.ScoreAvailable || g.calls.Load() != 2 {
		t.Fatal(err, g.calls.Load())
	}
	for _, want := range []string{"source_id=s1", "688220.SZ", "scenarios[0].portfolio_action"} {
		if !strings.Contains(g.prompts[1], want) {
			t.Fatal("repair prompt omitted", want)
		}
	}
}

func TestScoringCollectsIndependentFormatEvidenceAndActionErrors(t *testing.T) {
	req, results, metrics, base, at := etfScoringFixture(.2, 50)
	base = fixtureContractReport(base, buildScoringCatalog(req, results, metrics, at))
	base.Dimensions[0].EvidenceRefs = []EvidenceRef{{EvidenceID: "ev-missing"}}
	base.Scenarios[0].ActionIDs = []string{"act-missing"}
	data, _ := json.Marshal(base)
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	raw["primary_risks"] = []any{map[string]any{"unknown": true}}
	data, _ = json.Marshal(raw)
	report, issues := validateScoringOutput(string(data), req, results, metrics, at)
	if report.ScoreAvailable || report.TotalScore != nil || report.ETFScoring != nil {
		t.Fatal("invalid partial response exposed a score")
	}
	for _, code := range []string{"format", "evidence", "action"} {
		found := false
		for _, issue := range issues {
			found = found || issue.Code == code
		}
		if !found {
			t.Fatal("missing independent problem", code, issues)
		}
	}
}

func TestScoringContractDoesNotOfferActionsWithoutValidOriginalPlan(t *testing.T) {
	for _, mode := range []string{"valid", "missing_source", "invalid_price", "planning_blocker", "decision_blocker", "wrong_stop", "entry_condition"} {
		t.Run(mode, func(t *testing.T) {
			_, results, _, _, _ := etfScoringFixture(.2, 50)
			r := &results[1]
			rr := r.Analysis.ResearchReport
			r.Analysis.ETF.Bars = make([]foundation.KLine, 20)
			rr.Anchors = []stockanalysis.PriceAnchor{{ID: "stop", Price: .957, SourceID: "e-profile"}}
			rr.Conditions = []stockanalysis.ResearchCondition{{ID: "c2", Text: "收盘触及原失效线", Metric: "close", Operator: "lte", AnchorID: "stop", Window: "1d"}}
			rr.InvalidationIDs = []string{"c2"}
			rr.Decision.Status = "conditional"
			rr.Decision.PricePlan = &stockanalysis.AnchoredPricePlan{StopAnchor: "stop"}
			switch mode {
			case "missing_source":
				rr.Anchors[0].SourceID = "missing"
			case "invalid_price":
				rr.Anchors[0].Price = 0
			case "planning_blocker":
				r.Analysis.ETF.Bars = nil
			case "decision_blocker":
				rr.Decision.Blockers = []string{"原研究证据不足"}
			case "wrong_stop":
				rr.Decision.PricePlan.StopAnchor = "another-stop"
			case "entry_condition":
				rr.InvalidationIDs = nil
				rr.Conditions[0].Operator = "gte"
			}
			conditional := 0
			for _, a := range scoringActionsFor(*r) {
				if strings.HasPrefix(a.Kind, "conditional_") {
					conditional++
				}
			}
			if (conditional > 0) != (mode == "valid") {
				t.Fatal("action escaped original plan constraints", conditional)
			}
		})
	}
}
