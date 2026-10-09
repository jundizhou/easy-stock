package portfoliooptimization

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	pi "easy-stock/backend/internal/portfolioinspection"
)

type reviewScopeIssue struct {
	Side        string `json:"side"`
	Path        string `json:"path"`
	Reference   string `json:"reference"`
	AvailableIn string `json:"available_in"`
}

type reviewScopeError struct {
	Issues   []reviewScopeIssue  `json:"cross_side_references"`
	Holdings map[string][]string `json:"actual_holdings"`
}

func (e *reviewScopeError) Error() string {
	b, _ := json.Marshal(e)
	return "复评分组引用错误：引用属于另一组持股，不是财务字段缺失；不能替换成同一只股票的另一字段。" + string(b)
}

// Collect ALL opposite-side references before ordinary score validation. The
// shared dossier is not a license to treat the union as either portfolio.
// No citations, scores or A/B identities are rewritten by this check.
func checkReviewScopes(a, b json.RawMessage, ra, rb pi.Report) error {
	err := &reviewScopeError{Holdings: map[string][]string{"a": {}, "b": {}}}
	reports := []pi.Report{ra, rb}
	facts := []map[string]pi.Fact{pi.OptimizationComparisonFacts(ra), pi.OptimizationComparisonFacts(rb)}
	sources := []map[string]bool{{}, {}}
	for i, r := range reports {
		side := []string{"a", "b"}[i]
		for _, h := range r.Request.Holdings {
			if h.Weight > 0 {
				err.Holdings[side] = append(err.Holdings[side], h.Symbol)
			}
		}
		for _, h := range r.Holdings {
			if h.Holding.Weight <= 0 || !pi.ValidOptimizationResearch(h) {
				continue
			}
			for _, src := range h.Analysis.ResearchReport.Sources {
				sources[i][h.AnalysisID+"/"+src.ID] = true
			}
		}
	}
	for i, raw := range []json.RawMessage{a, b} {
		var value any
		if json.Unmarshal(raw, &value) != nil {
			continue
		} // normal decoder owns shape errors
		side, other := []string{"a", "b"}[i], []string{"b", "a"}[i]
		var walk func(any, string)
		walk = func(value any, path string) {
			switch v := value.(type) {
			case map[string]any:
				if ref, ok := v["fact"].(string); ok {
					key := pi.OptimizationFactAlias(strings.TrimPrefix(ref, side+"."), facts[i])
					otherKey := pi.OptimizationFactAlias(strings.TrimPrefix(ref, other+"."), facts[1-i])
					if !facts[i][key].Available && facts[1-i][otherKey].Available {
						err.Issues = append(err.Issues, reviewScopeIssue{side, path, ref, other})
					}
				}
				if id, ok := v["report_id"].(string); ok {
					source, _ := v["source_id"].(string)
					ref := id + "/" + source
					if !sources[i][ref] && sources[1-i][ref] {
						err.Issues = append(err.Issues, reviewScopeIssue{side, path, ref, other})
					}
				}
				keys := []string{}
				for key := range v {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					walk(v[key], path+"."+key)
				}
			case []any:
				for n, item := range v {
					walk(item, fmt.Sprintf("%s[%d]", path, n))
				}
			}
		}
		walk(value, side)
	}
	if len(err.Issues) > 0 {
		return err
	}
	return nil
}

func addReviewScopes(data map[string]any, ra, rb pi.Report, results []pi.HoldingResult) {
	scopes := map[string]string{}
	for i, r := range []pi.Report{ra, rb} {
		for _, h := range r.Request.Holdings {
			if h.Weight > 0 {
				scopes[h.Symbol] += []string{"a", "b"}[i]
			}
		}
	}
	columns := append([]string(nil), data["stock_columns"].([]string)...)
	data["stock_columns"] = append(columns, "score_scope")
	rows := data["stocks"].([]any)
	for i, r := range results {
		rows[i] = append(rows[i].([]any), scopes[r.Holding.Symbol])
	}
}
