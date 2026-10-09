package portfoliooptimization

import (
	"context"
	pi "easy-stock/backend/internal/portfolioinspection"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type rangeAdjustment struct {
	Symbol  string `json:"symbol"`
	Minimum int    `json:"min_weight"`
	Maximum int    `json:"max_weight"`
	Reason  string `json:"reason"`
}

func (r *rangeAdjustment) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if len(fields) != 4 {
		return errors.New("每个范围补丁只含symbol/min_weight/max_weight/reason四项")
	}
	for _, key := range []string{"symbol", "min_weight", "max_weight", "reason"} {
		if fields[key] == nil || string(fields[key]) == "null" {
			return fmt.Errorf("范围补丁缺少%s", key)
		}
	}
	type named rangeAdjustment
	var v named
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*r = rangeAdjustment(v)
	return nil
}

// Refinement changes investment size, never identity, funding permission, facts,
// roles, conditions or frozen risks. Code supplies a feasible reference.
func refineAllocationRanges(ctx context.Context, job Job, content string) (Proposal, error) {
	if job.InvestmentBaseline == nil || len(job.InvestmentBaseline.Alternatives) != 1 {
		return Proposal{}, errors.New("缺少已校验的投资范围基准")
	}
	var patch struct {
		Ranges     []rangeAdjustment `json:"allocation_ranges"`
		KeepReason string            `json:"keep_reason"`
	}
	if err := jsonContent(content, &patch); err != nil {
		return Proposal{}, err
	}
	data, _ := json.Marshal(job.InvestmentBaseline)
	var p Proposal
	if err := json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if len(patch.Ranges) == 0 && strings.TrimSpace(patch.KeepReason) != "" {
		p.Alternatives = nil
		p.KeepReason = patch.KeepReason
		return p, validateProposal(job, p)
	}
	alt := &p.Alternatives[0]
	p.RangeAdjustments = patch.Ranges
	if len(patch.Ranges) != len(alt.Allocations) {
		return p, errors.New("范围改进须包含全部已研究股票，不能增删股票")
	}
	rows := map[string]rangeAdjustment{}
	for _, row := range patch.Ranges {
		if _, ok := rows[row.Symbol]; ok {
			return p, errors.New("范围改进股票重复")
		}
		if row.Minimum < 0 || row.Maximum > 100 || row.Minimum > row.Maximum || strings.TrimSpace(row.Reason) == "" || len([]rune(row.Reason)) > 180 {
			return p, errors.New("范围改进上下限及依据无效")
		}
		if err := validateProfitInterpretation(row.Reason); err != nil {
			return p, err
		}
		rows[row.Symbol] = row
	}
	original, _, _ := weights(job.Source.Request.Holdings, false)
	rules, _ := pi.RulesFor(job.Source.Request.TraderProfile)
	elig := map[string]Eligibility{}
	for _, e := range job.Eligibility {
		elig[e.Symbol] = e
	}
	for i, a := range alt.Allocations {
		row, ok := rows[a.Symbol]
		if !ok {
			return p, fmt.Errorf("范围改进缺少%s", a.Symbol)
		}
		limit := max(original[a.Symbol], rules.MaxSinglePercent)
		if !a.Suitable || !elig[a.Symbol].CanIncrease {
			limit = original[a.Symbol]
		}
		if row.Minimum > limit || (elig[a.Symbol].Locked && (row.Minimum != original[a.Symbol] || row.Maximum != original[a.Symbol])) {
			return p, fmt.Errorf("%s范围改进超过原资金权限或交易锁定", a.Symbol)
		}
		if row.Maximum > limit {
			p.RangeBoundCorrections = append(p.RangeBoundCorrections, fmt.Sprintf("%s建议上限%d%%与原资金权限取交集为%d%%；未授予新资金权限", a.Symbol, row.Maximum, limit))
			row.Maximum = limit
		}
		a.Minimum, a.Maximum = row.Minimum, row.Maximum
		a.Preferred = max(a.Minimum, min(a.Maximum, a.Preferred))
		alt.Allocations[i] = a
	}
	frozenJob := job
	frozenJob.Proposal = &p
	// Feasible reference construction is not a review. Rejected-target filtering
	// remains in materialization, where lack of a new solution is a normal outcome.
	frozenJob.RevisionHistory = nil
	solutions, err := SearchAllocations(ctx, frozenJob, *alt)
	if err != nil {
		return p, err
	}
	w, _, _ := weights(solutions[0].Target, false)
	for i, a := range alt.Allocations {
		alt.Allocations[i].Preferred = w[a.Symbol]
	}
	return p, validateProposal(job, p)
}

func rangeRefinementPrompt(job Job) (string, error) {
	frontier, err := permittedRangeFrontier(job)
	if err != nil {
		return "", err
	}
	return boundedModelPromptWithLimit(MaxRevisionModelPromptBytes, func(detail int) (string, error) {
		data := commonDossier(job, job.Results, detail)
		data["previous_review"] = revisionFeedback(job)
		data["minimum_portfolio_score"] = TargetPortfolioScore
		baseline, _, _ := weights(job.Baseline, false)
		data["initial_baseline"] = baseline
		data["permitted_range_frontier"] = frontier
		_, total, _ := weights(job.Source.Request.Holdings, false)
		data["required_stock_total"] = total
		data["required_cash"] = 100 - total
		original, _, _ := weights(job.Source.Request.Holdings, false)
		rules, _ := pi.RulesFor(job.Source.Request.TraderProfile)
		data["top_three_hard_limit"] = max(topWeight(job.Source.Request.Holdings), rules.MaxTopThreePercent)
		elig := map[string]Eligibility{}
		for _, e := range job.Eligibility {
			elig[e.Symbol] = e
		}
		rows := []any{}
		for _, a := range job.InvestmentBaseline.Alternatives[0].Allocations {
			limit := max(original[a.Symbol], rules.MaxSinglePercent)
			if !a.Suitable || !elig[a.Symbol].CanIncrease {
				limit = original[a.Symbol]
			}
			rows = append(rows, []any{a.Symbol, original[a.Symbol], a.Minimum, a.Maximum, limit, a.Suitable, elig[a.Symbol].Locked, a.Investment.Role, a.Investment.Action})
		}
		data["range_columns"] = []string{"symbol", "original_weight", "old_min", "old_max", "allowed_max", "funding_allowed", "locked", "role", "action"}
		data["investment_ranges"] = rows
		payload, err := modelJSON(data)
		if err != nil {
			return "", err
		}
		return `你是持仓方案研究员。程序已搜索配仓，但仍有采纳检查未通过。根据previous_review.rejection_reason及复评缺陷、冻结事实与investment_ranges，调整可接受权重范围，让程序一次优化内继续求解综合评分≥70的合理组合。不是重新评分，不重做个股研究。
` + dossierNote + `只调min/max，投资判断、角色、资金权限、条件及risk_groups冻结。funding_allowed=false不增持，locked上下限等于original_weight，max≤allowed_max。permitted_range_frontier是程序在资金权限内算出的参考（不是评分）：核验盈利、风险和机会成本，合理时放开范围使该配置可达，不能仅重述旧上限、强制选票或为凑分增风险。
固定required_stock_total与required_cash，初始组合累计替换≤70%、重叠≥30%，新增≤2、持仓≤10。前三大不加重原超限，硬上限见top_three_hard_limit；风格参考继续独立评价。每股min/max覆盖可行总仓位即可，不输出preferred_weight或计算最终权重，程序精确求解。
逐股列齐，清仓/未选列0，reason说明范围变化怎样解决缺陷，不能虚构财务、估值或已满足条件。不重写13项投资报告、问题列表或股票计划。仅完整JSON≤2KiB、无代码块或附加核对文字：{"allocation_ranges":[{"symbol":"代码","min_weight":0,"max_weight":35,"reason":"范围依据"}],"keep_reason":""}。无合理范围改进则allocation_ranges:[]并解释keep_reason，不抬分。
[资料JSON]
` + payload, nil
	})
}

// Show attainable structural improvements before the planner chooses ranges.
// This preview widens only existing investment permissions, never a hold or
// trading lock. Its objective and diagnostics are not independent AI scores.
func permittedRangeFrontier(job Job) (map[string]any, error) {
	if job.InvestmentBaseline == nil || len(job.InvestmentBaseline.Alternatives) != 1 {
		return nil, errors.New("范围参考缺少已校验投资基准")
	}
	data, _ := json.Marshal(job.InvestmentBaseline)
	var p Proposal
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	original, _, _ := weights(job.Source.Request.Holdings, false)
	rules, _ := pi.RulesFor(job.Source.Request.TraderProfile)
	elig := map[string]Eligibility{}
	for _, e := range job.Eligibility {
		elig[e.Symbol] = e
	}
	symbols := []string{}
	for i := range p.Alternatives[0].Allocations {
		a := &p.Alternatives[0].Allocations[i]
		symbols = append(symbols, a.Symbol)
		a.Minimum = 0
		a.Maximum = max(original[a.Symbol], rules.MaxSinglePercent)
		if !a.Suitable || !elig[a.Symbol].CanIncrease {
			a.Maximum = original[a.Symbol]
		}
		if elig[a.Symbol].Locked {
			a.Minimum, a.Maximum = original[a.Symbol], original[a.Symbol]
		}
		a.Preferred = max(a.Minimum, min(a.Maximum, a.Preferred))
	}
	job.Proposal, job.RevisionHistory = &p, nil
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	solutions, err := SearchAllocations(ctx, job, p.Alternatives[0])
	if err != nil {
		return map[string]any{"unavailable": err.Error()}, nil
	}
	return allocationReferenceTable(symbols, solutions), nil
}

func allocationReferenceTable(symbols []string, solutions []qualitySolution) map[string]any {
	rows := []any{}
	for _, s := range solutions {
		w, _, _ := weights(s.Target, false)
		values := []int{}
		for _, symbol := range symbols {
			values = append(values, w[symbol])
		}
		rows = append(rows, []any{values, s.Search.ProfitablePercent, s.Search.LossPercent, s.Search.VolatilityProxy})
	}
	return map[string]any{"symbols": symbols, "columns": []string{"weights", "profitable_weight", "deducted_loss_weight", "volatility_proxy"}, "rows": rows}
}

func initialAllocationReferences(job Job) (map[string]any, error) {
	original, _, _ := weights(job.Source.Request.Holdings, false)
	rules, _ := pi.RulesFor(job.Source.Request.TraderProfile)
	alt := Alternative{Name: "投资判断前的代码配仓参考"}
	symbols := []string{}
	for _, r := range job.Results {
		symbols = append(symbols, r.Holding.Symbol)
		alt.Allocations = append(alt.Allocations, Allocation{Symbol: r.Holding.Symbol, Maximum: max(original[r.Holding.Symbol], rules.MaxSinglePercent), Preferred: original[r.Holding.Symbol]})
	}
	// These are research questions for the planner, not executable allocations.
	job.Proposal = nil
	m, err := buildQualityAllocationModel(job, alt, false)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	solutions, err := searchAllocationModel(ctx, job, alt, m)
	if err != nil {
		return map[string]any{"unavailable": err.Error()}, nil
	}
	return allocationReferenceTable(symbols, solutions), nil
}
