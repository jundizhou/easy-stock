package portfolioinspection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/stockanalysis"
)

const scoringContractVersion = "portfolio-contract-v1"

type scoringEvidence struct {
	ID  string      `json:"evidence_id"`
	Ref EvidenceRef `json:"reference"`
}

// ScoringAction is authored by the backend from frozen research. The model
// selects its ID; it cannot supply prices, clear blockers or verify conditions.
type ScoringAction struct {
	ID                     string   `json:"action_id"`
	Symbol                 string   `json:"symbol"`
	ReportID               string   `json:"report_id"`
	Kind                   string   `json:"kind"`
	ConditionIDs           []string `json:"condition_ids,omitempty"`
	AnchorIDs              []string `json:"anchor_ids,omitempty"`
	ExecutionPrerequisites []string `json:"execution_prerequisites,omitempty"`
	ExecutionStatus        string   `json:"execution_status"`
	Text                   string   `json:"text"`
	Confirmation           string   `json:"confirmation"`
	Invalidation           string   `json:"invalidation"`
}

type scoringCatalog struct {
	Evidence []scoringEvidence `json:"evidence_catalog"`
	Actions  []ScoringAction   `json:"action_catalog"`
}

func scoringID(prefix string, value any) string {
	data, _ := json.Marshal(value)
	hash := sha256.Sum256(data)
	return prefix + hex.EncodeToString(hash[:12])
}

func buildScoringCatalog(request Request, results []HoldingResult, metrics Metrics, at time.Time) scoringCatalog {
	c := scoringCatalog{}
	facts := ScoringFacts(portfolioFactsAt(request, results, metrics, at))
	keys := make([]string, 0, len(facts))
	for key, fact := range facts {
		if fact.Available {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		ref := EvidenceRef{Fact: key}
		c.Evidence = append(c.Evidence, scoringEvidence{ID: scoringID("ev-", []any{ref, facts[key]}), Ref: ref})
	}
	for _, r := range results {
		if !validHoldingResearch(r) {
			continue
		}
		rr := r.Analysis.ResearchReport
		used := scoringSourceIDs(r, rr.ResearchSynthesis)
		seen := map[string]bool{}
		for _, source := range rr.Sources {
			if !used[source.ID] || seen[source.ID] {
				continue
			}
			seen[source.ID] = true
			ref := EvidenceRef{ReportID: r.AnalysisID, SourceID: source.ID}
			c.Evidence = append(c.Evidence, scoringEvidence{ID: scoringID("ev-", []any{ref, source}), Ref: ref})
		}
		c.Actions = append(c.Actions, scoringActionsFor(r)...)
	}
	return c
}

func scoringActionsFor(r HoldingResult) []ScoringAction {
	rr := r.Analysis.ResearchReport
	prerequisites := append([]string(nil), rr.Decision.ExecutionPrerequisites...)
	if isETF(r.Analysis) {
		prerequisites = append(prerequisites, r.Analysis.ETF.ExecutionBlockers...)
	}
	prerequisites = uniqueScoringStrings(prerequisites)
	var confirmations, invalidations []string
	for _, condition := range rr.Conditions {
		if oneOf(condition.ID, rr.InvalidationIDs...) {
			invalidations = append(invalidations, condition.Text)
		} else {
			confirmations = append(confirmations, condition.Text)
		}
	}
	confirmation := strings.Join(confirmations, "；")
	if confirmation == "" {
		confirmation = "原研究尚无已核验的交易确认条件，继续核验持有依据"
	}
	invalidation := strings.Join(invalidations, "；")
	if invalidation == "" {
		invalidation = "原研究未提供明确失效条件，出现新反证时重新评估，不另造退出价格"
	}
	actions := []ScoringAction{}
	add := func(kind, text string, conditionIDs, anchorIDs []string) {
		a := ScoringAction{Symbol: r.Holding.Symbol, ReportID: r.AnalysisID, Kind: kind, Text: r.Holding.Symbol + "：" + text, ConditionIDs: conditionIDs, AnchorIDs: anchorIDs, Confirmation: confirmation, Invalidation: invalidation, ExecutionStatus: "observation_only", ExecutionPrerequisites: prerequisites}
		if strings.HasPrefix(kind, "conditional_") {
			a.ExecutionStatus = "pending_verification"
		}
		a.ID = scoringID("act-", a)
		actions = append(actions, a)
	}
	add("observe", "保持观察，核验原研究的确认与失效条件，当前不据此执行买卖", nil, nil)
	add("hold", "已有持仓继续观察；跟踪原研究失效条件，此项不提供新增或减仓执行许可", nil, nil)
	add("reassess", "优先复核持有依据及原研究反证；完成核验前不据此执行买卖", nil, nil)
	plan := rr.Decision.PricePlan
	if rr.Decision.Status != "conditional" || plan == nil || len(rr.Decision.Blockers) > 0 || isETF(r.Analysis) && len(r.Analysis.ETF.PlanningBlockers()) > 0 {
		return actions
	}
	sources := map[string]bool{}
	for _, source := range rr.Sources {
		sources[source.ID] = true
	}
	anchors := map[string]stockanalysis.PriceAnchor{}
	for _, anchor := range rr.Anchors {
		if anchor.Price > 0 && !math.IsInf(anchor.Price, 0) && !math.IsNaN(anchor.Price) && sources[anchor.SourceID] {
			anchors[anchor.ID] = anchor
		}
	}
	for _, condition := range rr.Conditions {
		anchor, ok := anchors[condition.AnchorID]
		if !ok || condition.ID == "" || condition.Text == "" || condition.Metric != "close" {
			continue
		}
		isExit := oneOf(condition.ID, rr.InvalidationIDs...) && condition.AnchorID == plan.StopAnchor && condition.Operator == "lte"
		// Entry decisions can require several conditions simultaneously. Without
		// an explicit AND/OR contract, do not turn one price condition into a buy.
		if !isExit {
			continue
		}
		guard := "实际交易前须核验实时执行条件"
		if len(prerequisites) > 0 {
			guard += "（" + strings.Join(prerequisites, "；") + "）"
		}
		trigger := fmt.Sprintf("；全部前提核验通过且原研究条件 %s（%s；收盘不高于 %.4f，观察窗口 %s）成立后，才可考虑", condition.ID, condition.Text, anchor.Price, condition.Window)
		add("conditional_reduce", guard+trigger+"减仓；当前仍为待核验条件计划", []string{condition.ID}, []string{condition.AnchorID})
		add("conditional_exit", guard+trigger+"离场；当前仍为待核验条件计划", []string{condition.ID}, []string{condition.AnchorID})
	}
	return actions
}

func uniqueScoringStrings(values []string) []string {
	var result []string
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func walkScoringRefs(report *AIReport, visit func(string, *EvidenceRef)) {
	for i := range report.Dimensions {
		for j := range report.Dimensions[i].EvidenceRefs {
			visit(fmt.Sprintf("dimensions[%d].evidence_refs[%d]", i, j), &report.Dimensions[i].EvidenceRefs[j])
		}
	}
	for i := range report.RiskGroups {
		for j := range report.RiskGroups[i].EvidenceRefs {
			visit(fmt.Sprintf("risk_groups[%d].evidence_refs[%d]", i, j), &report.RiskGroups[i].EvidenceRefs[j])
		}
	}
	keys := make([]string, 0, len(report.ExplanationDetails))
	for key := range report.ExplanationDetails {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		detail := report.ExplanationDetails[key]
		for j := range detail.EvidenceRefs {
			visit(fmt.Sprintf("explanation_details.%s.evidence_refs[%d]", key, j), &detail.EvidenceRefs[j])
		}
		report.ExplanationDetails[key] = detail
	}
}

// Compile selectors before validating scores. No model prose is used as an
// execution permission, and no prerequisite is borrowed from another holding.
func compileScoringContract(report *AIReport, catalog scoringCatalog) scoringIssues {
	var issues scoringIssues
	if report.ContractVersion != scoringContractVersion {
		issues.add("contract", "contract_version", "必须使用本次输入的组合输出协议 "+scoringContractVersion)
	}
	evidence := map[string]EvidenceRef{}
	for i, e := range catalog.Evidence {
		evidence[e.ID] = e.Ref
		evidence[fmt.Sprintf("ev-%d", i+1)] = e.Ref
	}
	walkScoringRefs(report, func(path string, ref *EvidenceRef) {
		canonical, ok := evidence[ref.EvidenceID]
		if !ok || ref.ReportID != "" || ref.SourceID != "" || ref.Fact != "" {
			issues.add("evidence", path, "只允许选择 evidence_catalog 中的 evidence_id，不得拼接 report_id/source_id/fact")
			return
		}
		*ref = canonical
	})
	actions := map[string]ScoringAction{}
	for i, a := range catalog.Actions {
		actions[a.ID] = a
		actions[fmt.Sprintf("act-%d", i+1)] = a
	}
	if len(report.Actions) > 0 {
		issues.add("action", "actions", "actions 为后端生成字段，只能选择 action_id")
	}
	report.Actions = nil
	selected := map[string]bool{}
	selectAction := func(id, symbol, path string) (ScoringAction, bool) {
		a, ok := actions[id]
		if !ok || symbol != "" && a.Symbol != symbol {
			issues.add("action", path, "动作编号不存在或不属于该标的，请从 action_catalog 选择")
			return ScoringAction{}, false
		}
		if !selected[a.ID] {
			report.Actions = append(report.Actions, a)
			selected[a.ID] = true
		}
		return a, true
	}
	for i := range report.Holdings {
		h := &report.Holdings[i]
		path := fmt.Sprintf("holdings[%d]", i)
		if h.Action != "" || h.Confirmation != "" || h.Invalidation != "" {
			issues.add("action", path, "动作、确认和失效文字由后端生成，只返回 action_id")
		}
		if a, ok := selectAction(h.ActionID, h.Symbol, path+".action_id"); ok {
			h.ActionID = a.ID
			h.Action = a.Text
			h.Confirmation = a.Confirmation
			h.Invalidation = a.Invalidation
		}
	}
	selectMany := func(ids []string, path string) []string {
		var texts []string
		seenSymbols := map[string]bool{}
		if len(ids) == 0 || len(ids) > MaxHoldings {
			issues.add("action", path, "请选择1至10个动作编号，每个标的最多一个")
		}
		for i, id := range ids {
			if a, ok := selectAction(id, "", fmt.Sprintf("%s[%d]", path, i)); ok {
				ids[i] = a.ID
				if seenSymbols[a.Symbol] {
					issues.add("action", path, "同一动作出口不能为同一标的选择重复或冲突动作")
				}
				seenSymbols[a.Symbol] = true
				texts = append(texts, a.Text)
			}
		}
		return texts
	}
	if len(report.AdjustmentOrder) > 0 {
		issues.add("action", "adjustment_order", "处理顺序由后端生成，只返回 adjustment_action_ids")
	}
	report.AdjustmentOrder = selectMany(report.AdjustmentActionIDs, "adjustment_action_ids")
	for i := range report.Scenarios {
		scenario := &report.Scenarios[i]
		path := fmt.Sprintf("scenarios[%d]", i)
		if scenario.PortfolioAction != "" {
			issues.add("action", path+".portfolio_action", "情景动作由后端生成，只返回 action_ids；condition 只描述市场观察，不解除执行前提")
		}
		scenario.PortfolioAction = strings.Join(selectMany(scenario.ActionIDs, path+".action_ids"), "\n")
	}
	return issues
}
