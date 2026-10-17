package portfolioinspection

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"easy-stock/backend/internal/stockanalysis"
)

func TestETFConditionalActionsRetainExecutionPrerequisites(t *testing.T) {
	for _, tc := range []struct {
		name, action string
		allowed      bool
	}{
		{"saved_action", "持有但严格执行0.953元有效失守即减仓/离场，收盘站上1.013元前不追加；任何交易前核验实时IOPV与折溢价、盘口深度及逐产品交易规则。", true},
		{"conditional_size", "执行跌破0.953元则减仓50%的计划；任何交易前核验执行条件。", true},
		{"prerequisite_before_plan", "每次交易前必须核实交易规则；建议收复1.013元后按纪律加仓。", true},
		{"mandatory_prerequisite", "执行失守0.953元即减仓的纪律；交易前须重新核验盘口和申赎限制。", true},
		{"local_verified_plan", "核验交易规则通过后再执行减仓计划", true},
		{"verification_object_before_verb", "在执行前提核验完成后按纪律减仓/离场", true},
		{"verification_object_before_verb_with_size", "交易规则核实通过后再执行减仓50%的计划", true},
		{"unverified_object_before_verb", "在执行前提核验完成前应当减仓", false},
		{"skipped_object_before_verb", "无需执行前提核验完成后立即减仓", false},
		{"local_check_after_trade", "立即买入50%后核验交易规则", false},
		{"local_unrelated_check", "核验公司财务通过后立即买入50%", false},
		{"local_check_before_trade", "核验交易规则之前立即买入50%", false},
		{"local_guard_does_not_waive_later_trade", "核验交易规则通过后执行减仓并立即买入50%", false},
		{"skipped_local_check", "无需核验交易规则后立即买入50%", false},
		{"forbid_immediate", "不得立即买入；任何交易前核验交易规则", true},
		{"forbid_execution", "执行前提核验完成前不执行任何ETF调仓指令：先人工核验实时IOPV与折溢价、盘口深度、逐产品交易规则及申赎状态；核验通过后按原研究价格锚条件化处理——收盘有效跌破0.953再评估减仓/离场，收盘收复0.996并站稳（进阶1.013）前不追加", true},
		{"forbid_execution_in_scenario", "ETF在0.953-0.996区间震荡时维持观察、不执行调仓，触发失效线或披露负面的单票降权重估", true},
		{"forbid_allocation", "核验前不减仓50%", true},
		{"forbid_immediate_allocation", "核验前不要立即减仓50%", true},
		{"forbid_first_not_second", "不建议减仓但应当买入", false},
		{"forbid_execution_not_second", "不执行调仓但立即买入50%", false},
		{"forbid_execution_next_clause", "不执行调仓；立即买入50%", false},
		{"forbid_allocation_not_second", "不得立即买入50%并减仓50%", false},
		{"forbid_unrelated_action", "不要追高但立即减仓", false},
		{"not_forbidden", "不是不建议立即买入", false},
		{"double_negative", "不能不执行调仓", false},
		{"double_negative_allocation", "不得不减仓50%", false},
		{"immediate", "立即减仓50%", false},
		{"guard_does_not_waive_immediate", "立即减仓50%；任何交易前核验交易规则", false},
		{"unconditional_size", "减仓50%；任何交易前核验交易规则", false},
		{"price_only", "持有但严格执行0.953元有效失守即减仓/离场", false},
		{"unrelated_wait", "等待核验；立即买入", false},
		{"unrelated_check", "执行跌破0.953元则减仓；核验公司财务后观察", false},
		{"unrelated_global_check", "执行失守0.953元即减仓；任何交易前核验公司财务", false},
		{"unrelated_global_object", "执行失守0.953元即减仓；任何交易前核验财务；盘口和交易规则以后再说", false},
		{"skipped_global_check", "执行失守0.953元即减仓；无需任何交易前核验交易规则", false},
		{"separate_immediate_clause", "执行失守0.953元即减仓；任何交易前核验交易规则；现在买入50%", false},
		{"same_clause_immediate", "执行失守0.953元即减仓并立即加仓50%；任何交易前核验交易规则", false},
		{"same_clause_unconditional_size", "执行失守0.953元即减仓并加仓50%；任何交易前核验交易规则", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, results, _, _, _ := etfScoringFixture(.2, 50)
			err := validateETFActionText(tc.action, results[1])
			if (err == nil) != tc.allowed {
				t.Fatal("execution prerequisite classification", tc.action, err)
			}
		})
	}
}

func TestETFProhibitionsAreScopedAcrossActionOutputs(t *testing.T) {
	for _, where := range []string{"holdings[1]", "adjustment_order[0]", "scenarios[0].portfolio_action", "optimization"} {
		t.Run(where, func(t *testing.T) {
			for _, allowed := range []bool{true, false} {
				req, results, metrics, report, at := etfScoringFixture(.2, 50)
				action := "ETF执行前提核验完成前不执行任何ETF调仓指令"
				if !allowed {
					action += "但立即买入50%"
				}
				switch where {
				case "holdings[1]", "optimization":
					report.Holdings[1].Action = action
				case "adjustment_order[0]":
					report.AdjustmentOrder = []string{action}
				case "scenarios[0].portfolio_action":
					report.Scenarios[0].PortfolioAction = action
				}
				var err error
				if where == "optimization" {
					err = ValidateOptimizationInvestmentAction(report.Holdings[1], results[1])
				} else {
					err = validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at)
				}
				if (err == nil) != allowed {
					t.Fatalf("allowed=%v: %v", allowed, err)
				}
				if err != nil && (!strings.Contains(err.Error(), "立即买入") || where != "optimization" && !strings.Contains(err.Error(), where)) {
					t.Fatalf("validation error lost field/directive context: %v", err)
				}
			}
		})
	}
}

func TestETFScoringRepairsLegacyDirectiveWithCatalogAction(t *testing.T) {
	req, results, metrics, report, at := etfScoringFixture(.2, 50)
	// A prerequisite limited to additions does not qualify a reduction.
	report.AdjustmentOrder = []string{"ETF严格执行0.953有效失守的减仓/离场纪律；加仓前核验交易规则"}
	initial, _ := json.Marshal(report)
	report.Holdings[1].Action = "执行前提核验完成前不执行任何ETF调仓指令；先人工核验实时IOPV与交易规则"
	report.AdjustmentOrder = []string{"ETF在执行前提核验完成后按纪律减仓/离场"}
	report.Scenarios[0].PortfolioAction = "ETF维持观察、不执行调仓"
	report = fixtureContractReport(report, buildScoringCatalog(req, results, metrics, at))
	repaired, _ := json.Marshal(report)
	g := &scoringReplayGateway{responses: []json.RawMessage{initial, repaired}}
	s := NewService(nil, g, nil, nil)
	rules, _ := RulesFor(req.TraderProfile)
	validated, err := s.scorePortfolio(context.Background(), req, results, metrics, rules, at)
	if err != nil || !validated.ScoreAvailable || g.calls.Load() != 2 {
		t.Fatal("failed to reject initial directive and accept repair", err, g.calls.Load())
	}
	diagnostics := g.prompts[1]
	if !strings.Contains(diagnostics, `"path":"adjustment_order"`) || !strings.Contains(diagnostics, "adjustment_action_ids") {
		t.Fatal("repair prompt missing actionable validation diagnostic")
	}
	if len(results[1].Analysis.ETF.ExecutionBlockers) == 0 {
		t.Fatal("accepting a conditional action cleared the frozen execution blockers")
	}
}

func TestETFConditionalActionsWorkInInspectionAndOptimization(t *testing.T) {
	for _, where := range []string{"holding", "order", "scenario", "optimization"} {
		t.Run(where, func(t *testing.T) {
			req, results, metrics, report, at := etfScoringFixture(.2, 50)
			rr := results[1].Analysis.ResearchReport
			rr.Sources = append(rr.Sources, stockanalysis.ResearchSource{ID: "m-price", Kind: "calculation"})
			rr.Anchors = []stockanalysis.PriceAnchor{{ID: "entry", Price: 1.013, SourceID: "m-price"}}
			rr.Decision.Status = "conditional"
			rr.Decision.PricePlan = &stockanalysis.AnchoredPricePlan{EntryAnchor: "entry", SourceIDs: []string{"m-price"}}
			rr.Decision.ExecutionPrerequisites = append([]string(nil), results[1].Analysis.ETF.ExecutionBlockers...)
			action := "510300.SH 建议在收复1.013元后加仓；任何交易前核验交易规则与盘口条件。"
			switch where {
			case "holding", "optimization":
				report.Holdings[1].Action = action
			case "order":
				report.AdjustmentOrder = []string{action}
			case "scenario":
				report.Scenarios[0].PortfolioAction = action
			}
			var err error
			if where == "optimization" {
				err = ValidateOptimizationInvestmentAction(report.Holdings[1], results[1])
			} else {
				err = validateScoringReportFactsAt(&report, req, results, metrics, true, nil, at)
			}
			if err != nil {
				t.Fatal("conditional plan rejected", where, err)
			}
			if !strings.Contains(action, "任何交易前核验") || len(results[1].Analysis.ETF.ExecutionBlockers) == 0 {
				t.Fatal("test lost frozen execution prerequisites")
			}
			// Execution verification does not authorize a new price unsupported
			// by the frozen plan, in either inspection or investment selection.
			invented := HoldingConclusion{Action: "建议在买入价7.123元触发后加仓；任何交易前核验交易规则"}
			if err := validateHoldingPriceDirectives(invented, results[1]); err == nil {
				t.Fatal("inspection accepted an invented price")
			}
			if err := ValidateOptimizationInvestmentAction(invented, results[1]); err == nil {
				t.Fatal("optimization accepted an invented price")
			}
		})
	}
}
