package portfoliooptimization

import (
	"encoding/json"
	"fmt"
	"strings"

	pi "easy-stock/backend/internal/portfolioinspection"
)

// A/B scores and every investment comparison are independent durable blocks.
// Only failed blocks can be patched. No valid score is sampled a second time.
type ReviewCheckpoint struct {
	Blocks    map[string]json.RawMessage `json:"blocks"`
	Pending   []reviewPart               `json:"pending"`
	Keys      []string                   `json:"keys"`
	Preferred string                     `json:"preferred,omitempty"`
}
type reviewPart struct {
	Key     string          `json:"key"`
	Problem string          `json:"problem"`
	Value   json.RawMessage `json:"value,omitempty"`
}
type reviewPartsError struct{ state *ReviewCheckpoint }

func (e *reviewPartsError) Error() string {
	parts := []string{}
	for _, p := range e.state.Pending {
		parts = append(parts, p.Key+"："+p.Problem)
	}
	return "复评局部校验失败；" + strings.Join(parts, "；")
}
func (e *reviewPartsError) prompt(original string) string {
	pending, _ := modelJSON(e.state.Pending)
	return original + `
仅修复以下失败块，已通过的其他块由程序冻结，禁止重写或对调A/B，不为过关改分。key为a/b时返回该侧完整评分对象；assessment仅返回评价本身，不含investment_comparisons；comparison:N返回原来的单条投资比较，不能更换股票或维度。修正引用时同时核对本组实际持仓，另一组机会成本放assessment，不得虚构未知事实。仅返回{"repairs":[{"key":"待补key","value":完整修复对象}]}。
[待补项]
` + pending
}
func reviewSide(job Job, raw json.RawMessage, ra, rb pi.Report, side string) (pi.AIReport, error) {
	var a pi.AIReport
	report := ra
	if side == "b" {
		report = rb
	}
	if err := validateProfitInterpretation(string(raw)); err != nil {
		return a, err
	}
	var err error
	if side == "a" {
		err = checkReviewScopes(raw, nil, ra, rb)
	} else {
		err = checkReviewScopes(nil, raw, ra, rb)
	}
	if err != nil {
		return a, err
	}
	a, err = pi.DecodeNamedOptimizationComparisonScore(raw, report, side)
	if err != nil {
		return a, err
	}
	if err = validateWholePortfolioIndustryClaim(job, report, a); err != nil {
		return a, err
	}
	if len(a.RiskGroups) > 0 {
		return a, fmt.Errorf("复评risk_groups必须为空，分组已冻结由程序计算")
	}
	return a, nil
}
func (c *ReviewCheckpoint) validate(job Job, plan Plan, content string) (pi.AIReport, pi.AIReport, Assessment, error) {
	var a, b pi.AIReport
	var assessment Assessment
	ra, rb := plan.Original, plan.Proposed
	if plan.AssessmentOrder == "target_first" {
		ra, rb = rb, ra
	}
	rows := map[string]json.RawMessage{}
	if c.Blocks == nil {
		var result struct {
			A          json.RawMessage `json:"a"`
			B          json.RawMessage `json:"b"`
			Assessment json.RawMessage `json:"assessment"`
		}
		if err := jsonContent(content, &result); err != nil {
			return a, b, assessment, err
		}
		var review struct {
			InvestmentComparisons []json.RawMessage `json:"investment_comparisons"`
		}
		// Preserve identifiable scores even when assessment is missing or malformed.
		assessmentParsed := json.Unmarshal(result.Assessment, &review) == nil
		if assessmentParsed && len(review.InvestmentComparisons) > 8 {
			return a, b, assessment, fmt.Errorf("复评投资比较最多8项")
		}
		var core map[string]json.RawMessage
		rows["a"], rows["b"] = result.A, result.B
		if assessmentParsed && json.Unmarshal(result.Assessment, &core) == nil && core != nil {
			delete(core, "investment_comparisons")
			rows["assessment"], _ = json.Marshal(core)
		} else {
			rows["assessment"] = result.Assessment
			review.InvestmentComparisons = nil
		}
		c.Keys = []string{"a", "b", "assessment"}
		for i, v := range review.InvestmentComparisons {
			key := fmt.Sprintf("comparison:%d", i)
			c.Keys = append(c.Keys, key)
			rows[key] = v
		}
		c.Blocks = map[string]json.RawMessage{}
		for _, key := range c.Keys {
			c.Pending = append(c.Pending, reviewPart{Key: key})
		}
	} else {
		var patch struct {
			Repairs []struct {
				Key   string          `json:"key"`
				Value json.RawMessage `json:"value"`
			} `json:"repairs"`
		}
		if err := decodeStrictPart(content, &patch); err != nil {
			return a, b, assessment, &reviewPartsError{c}
		}
		expected := map[string]bool{}
		for _, part := range c.Pending {
			expected[part.Key] = true
		}
		for _, row := range patch.Repairs {
			if !expected[row.Key] || rows[row.Key] != nil {
				return a, b, assessment, &reviewPartsError{c}
			}
			rows[row.Key] = row.Value
		}
	}
	pending := []reviewPart{}
	for _, part := range c.Pending {
		raw, ok := rows[part.Key]
		if !ok {
			pending = append(pending, part)
			continue
		}
		var err error
		switch part.Key {
		case "a":
			if err = preserveReviewScores(part.Value, raw); err != nil {
				break
			}
			_, err = reviewSide(job, raw, ra, rb, "a")
		case "b":
			if err = preserveReviewScores(part.Value, raw); err != nil {
				break
			}
			_, err = reviewSide(job, raw, ra, rb, "b")
		case "assessment":
			var r Assessment
			err = decodeStrictPart(string(raw), &r)
			if err == nil {
				err = validateProfitInterpretation(string(raw))
			}
			if err == nil {
				_, err = comparisonTargetsProposal(r.Preferred, plan.AssessmentOrder)
			}
			if err == nil && c.Preferred != "" && r.Preferred != c.Preferred {
				err = fmt.Errorf("修复不能更换已经明确的偏好方向")
			}
			if in(r.Preferred, "a", "b", "neither") && c.Preferred == "" {
				c.Preferred = r.Preferred
			}
			if err == nil && (r.Reason == "" || r.Issue == "" || len(r.InvestmentComparisons) > 0) {
				err = fmt.Errorf("评价需原因与原问题，比较条目由程序独立保留")
			}
			if err == nil {
				err = checkComparisonRefs(job, &plan, ra, rb, r.EvidenceRefs)
			}
		default:
			var row ReviewedInvestmentComparison
			err = decodeStrictPart(string(raw), &row)
			if err == nil {
				err = validateProfitInterpretation(string(raw))
			}
			if err == nil && len(part.Value) > 0 {
				var old ReviewedInvestmentComparison
				if json.Unmarshal(part.Value, &old) == nil && (old.PreferredSymbol != row.PreferredSymbol || old.OtherSymbol != row.OtherSymbol || old.Dimension != row.Dimension) {
					err = fmt.Errorf("局部修复不能更换比较股票或维度")
				}
			}
			if err == nil {
				if !in(c.Preferred, "a", "b", "neither") {
					err = fmt.Errorf("评价偏好尚未明确")
				} else {
					err = validateReviewedComparisons(job, plan, Assessment{Preferred: c.Preferred, InvestmentComparisons: []ReviewedInvestmentComparison{row}})
				}
			}
		}
		if err != nil {
			part.Problem = err.Error()
			if len(part.Value) == 0 {
				part.Value = raw
			}
			pending = append(pending, part)
		} else {
			c.Blocks[part.Key] = raw
		}
	}
	c.Pending = pending
	if len(pending) > 0 {
		return a, b, assessment, &reviewPartsError{c}
	}
	var err error
	a, err = reviewSide(job, c.Blocks["a"], ra, rb, "a")
	if err != nil {
		return a, b, assessment, err
	}
	b, err = reviewSide(job, c.Blocks["b"], ra, rb, "b")
	if err != nil {
		return a, b, assessment, err
	}
	_ = json.Unmarshal(c.Blocks["assessment"], &assessment)
	assessment.Accepted, err = comparisonTargetsProposal(assessment.Preferred, plan.AssessmentOrder)
	if err != nil {
		return a, b, assessment, err
	}
	for _, key := range c.Keys[3:] {
		var row ReviewedInvestmentComparison
		_ = json.Unmarshal(c.Blocks[key], &row)
		assessment.InvestmentComparisons = append(assessment.InvestmentComparisons, row)
	}
	return a, b, assessment, validateReviewedComparisons(job, plan, assessment)
}

// A factual/narrative repair cannot resample already explicit, valid dimension
// scores, even when the containing A/B block has not yet passed all checks.
func preserveReviewScores(old, next json.RawMessage) error {
	if len(old) == 0 {
		return nil
	}
	scores := func(raw json.RawMessage) map[string]int {
		var v struct {
			Dimensions []struct {
				Key   string
				Score json.RawMessage
			}
		}
		out := map[string]int{}
		counts := map[string]int{}
		if json.Unmarshal(raw, &v) != nil {
			return out
		}
		for _, d := range v.Dimensions {
			counts[d.Key]++
			var score int
			if string(d.Score) != "null" && json.Unmarshal(d.Score, &score) == nil && score >= 0 && score <= 100 {
				out[d.Key] = score
			}
		}
		for key, n := range counts {
			if n != 1 {
				delete(out, key)
			}
		}
		return out
	}
	a, b := scores(old), scores(next)
	for _, key := range []string{"holding_logic", "portfolio_structure", "risk_capacity", "strategy_fit"} {
		value, ok := a[key]
		if !ok {
			continue
		}
		updated, exists := b[key]
		if !exists || updated != value {
			return fmt.Errorf("局部修复不得改写已明确的%s评分%d", key, value)
		}
	}
	return nil
}
