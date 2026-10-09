package portfoliooptimization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	pi "easy-stock/backend/internal/portfolioinspection"
)

// This error is available only after program-mode investment validation. A
// range repair is a bounded change of sizes, not another investment or score.
type programRangeRepairError struct {
	proposal Proposal
	cause    error
}

func (e *programRangeRepairError) Error() string { return e.cause.Error() }
func (e *programRangeRepairError) Unwrap() error { return e.cause }

func feasibilityLimits(job Job, p Proposal) map[string][2]int {
	original, _, _ := weights(job.Source.Request.Holdings, false)
	rules, _ := pi.RulesFor(job.Source.Request.TraderProfile)
	eligibility := map[string]Eligibility{}
	for _, e := range job.Eligibility {
		eligibility[e.Symbol] = e
	}
	limits := map[string][2]int{}
	for _, alt := range p.Alternatives {
		for _, a := range alt.Allocations {
			e := eligibility[a.Symbol]
			hi := max(original[a.Symbol], rules.MaxSinglePercent)
			if !a.Suitable || !e.CanIncrease {
				hi = min(a.Maximum, original[a.Symbol])
			}
			// Do not turn an explicit exit or excluded watch stock into a purchase
			// merely to fill a numeric gap. Locked original holdings stay locked.
			if a.Maximum == 0 {
				hi = 0
			}
			lo := 0
			if e.Locked {
				lo, hi = original[a.Symbol], original[a.Symbol]
			}
			limits[a.Symbol] = [2]int{lo, hi}
		}
	}
	return limits
}

func repairInitialRanges(ctx context.Context, job Job, frozen Proposal, content string) (Proposal, error) {
	var fields map[string]json.RawMessage
	if err := jsonContent(content, &fields); err != nil {
		return Proposal{}, err
	}
	for key := range fields {
		if key != "allocation_ranges" && key != "keep_reason" {
			return Proposal{}, fmt.Errorf("仓位范围修复不能改写%s", key)
		}
	}
	if fields["allocation_ranges"] == nil || string(fields["allocation_ranges"]) == "null" {
		return Proposal{}, errors.New("仓位范围修复须明确allocation_ranges")
	}
	var patch struct {
		Ranges     []rangeAdjustment `json:"allocation_ranges"`
		KeepReason string            `json:"keep_reason"`
	}
	if err := jsonContent(content, &patch); err != nil {
		return Proposal{}, err
	}
	if len(frozen.Alternatives) != 1 {
		return Proposal{}, errors.New("缺少冻结的投资方案")
	}
	limits := feasibilityLimits(job, frozen)
	for _, row := range patch.Ranges {
		limit, known := limits[row.Symbol]
		if !known || row.Minimum < limit[0] || row.Maximum > limit[1] {
			return Proposal{}, fmt.Errorf("%s范围修复超出允许区间[%d,%d]；不新增资金权限或重启已排除股票", row.Symbol, limit[0], limit[1])
		}
	}
	// The existing strict range patch handler copies the baseline, validates
	// every row, solves all constraints and supplies exact reference weights.
	job.InvestmentBaseline = &frozen
	p, err := refineAllocationRanges(ctx, job, content)
	if err != nil {
		return p, fmt.Errorf("仓位范围修复仍不可行：%w", err)
	}
	if len(p.Alternatives) > 0 {
		p.RangeBoundCorrections = append(p.RangeBoundCorrections, "初始权重范围不可行，AI仅修正min/max；投资判断、资金权限和条件保持，最终权重由程序求解并须独立复评")
	}
	return p, nil
}

func feasibilityRepairPrompt(ctx context.Context, job Job, p Proposal, cause error) (string, error) {
	if len(p.Alternatives) != 1 {
		return "", errors.New("仓位范围修复缺少单一方案")
	}
	limits := feasibilityLimits(job, p)
	// Show a feasible numerical reference under the original permissions, rather
	// than asking the model to do portfolio arithmetic again. Never auto-adopt it.
	raw, _ := json.Marshal(p)
	var reference Proposal
	if err := json.Unmarshal(raw, &reference); err != nil {
		return "", err
	}
	for i, a := range reference.Alternatives[0].Allocations {
		limit := limits[a.Symbol]
		a.Minimum, a.Maximum = limit[0], limit[1]
		a.Preferred = max(a.Minimum, min(a.Maximum, a.Preferred))
		reference.Alternatives[0].Allocations[i] = a
	}
	searchJob := job
	searchJob.Proposal = &reference
	searchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	solutions, searchErr := SearchAllocations(searchCtx, searchJob, reference.Alternatives[0])
	cancel()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	symbols := []string{}
	for _, a := range p.Alternatives[0].Allocations {
		symbols = append(symbols, a.Symbol)
	}
	frontier := allocationReferenceTable(symbols, solutions)
	if searchErr != nil {
		frontier = map[string]any{"unavailable": searchErr.Error()}
	}
	return boundedModelPromptWithLimit(MaxRevisionModelPromptBytes, func(detail int) (string, error) {
		data := commonDossier(job, job.Results, detail)
		original, total, _ := weights(job.Source.Request.Holdings, false)
		baseline, _, _ := weights(job.Baseline, false)
		rules, _ := pi.RulesFor(job.Source.Request.TraderProfile)
		data["required_stock_total"], data["required_cash"] = total, 100-total
		data["initial_baseline"] = baseline
		data["top_three_hard_limit"] = max(topWeight(job.Source.Request.Holdings), rules.MaxTopThreePercent)
		data["failure"] = cause.Error()
		data["feasible_reference"] = frontier
		data["range_columns"] = []string{"symbol", "original", "old_min", "old_max", "allowed_min", "allowed_max", "action", "role", "range_reason", "portfolio_fit", "risk"}
		rows := []any{}
		for _, a := range p.Alternatives[0].Allocations {
			if a.Investment == nil {
				return "", errors.New("缺少冻结的投资判断")
			}
			limit := limits[a.Symbol]
			rows = append(rows, []any{a.Symbol, original[a.Symbol], a.Minimum, a.Maximum, limit[0], limit[1], a.Investment.Action, a.Investment.Role, shortText(a.Reason, 80), shortText(a.Investment.PortfolioFit, 80), shortText(a.Investment.Risk, 80)})
		}
		data["investment_ranges"] = rows
		hard := []any{}
		searchJob.Proposal = &p
		for _, check := range groupRiskChecks(searchJob, job.Source.Request.Holdings) {
			if check.Hard {
				hard = append(hard, []any{check.Symbols, check.Limit})
			}
		}
		data["linked_symbols_and_limit"] = hard
		payload, err := modelJSON(data)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(`你是持仓方案的仓位范围修复员。投资判断已校验并冻结，但上下限无法形成可行配仓。只修正min_weight/max_weight，原股票、投资行动、理由、引用、风险分组和条件不变，不重新选股、不评分。
`+compactDossierNote+`按照failure及investment_ranges，在allowed_min/allowed_max内修正范围。清仓和零仓排除股不可恢复，hold/reduce不能新增资金，locked上下限固定。min合计≤required_stock_total≤max合计仅为必要条件，新增≤2、持仓≤10，固定现金/总仓位，初始累计替换≤70%，前三及实际风险边界仍由程序检查。
feasible_reference是这些资金权限下的代码参考，不是AI结论或评分。结合原投资理由核对可行配仓；合理时令新范围覆盖参考，不能只重复不可能的旧上下限。列齐所有原股票，reason说明具体范围变更的投资取舍；不要改写全部投资报告，不输出最终权重。仅返回JSON：{"allocation_ranges":[{"symbol":"原代码","min_weight":0,"max_weight":35,"reason":"范围取舍"}],"keep_reason":""}。无合理范围则allocation_ranges:[]，说明keep_reason；不以新增现金或强制买股凑总数。`) + "\n[资料JSON]\n" + payload, nil
	})
}
