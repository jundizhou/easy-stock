package portfoliooptimization

import (
	"encoding/json"
	"reflect"
)

var repairInvestmentColumns = []string{"role", "action", "horizon", "business", "growth", "valuation", "timing", "portfolio_fit", "risk", "exit", "opportunity_cost", "prior_opinion", "period_suitability"}
var repairAllocationColumns = []string{"symbol", "min_weight", "max_weight", "preferred_weight", "reason", "funding_reason", "suitable_for_increase", "suitability_reason", "investment", "confirmation_ids", "invalidation_ids", "allocation_conditions", "evidence_refs"}

// RawMessage preserves every scalar/number exactly. A second tuple records
// absent field indices, preserving missing versus explicit null. Objects with
// unknown fields are left untouched; this never guesses missing judgments.
func compactRepairProposal(output string) (string, bool) {
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(output), &root) != nil || root["investment_columns"] != nil || root["allocation_columns"] != nil {
		return output, false
	}
	var alternatives []map[string]json.RawMessage
	if json.Unmarshal(root["alternatives"], &alternatives) != nil {
		return output, false
	}
	investmentChanged, allocationChanged := false, false
	for _, alt := range alternatives {
		var allocations []json.RawMessage
		if json.Unmarshal(alt["allocations"], &allocations) != nil {
			continue
		}
		for i, raw := range allocations {
			var a map[string]json.RawMessage
			if json.Unmarshal(raw, &a) != nil {
				continue
			}
			if encoded, ok := compactColumns(a["investment"], repairInvestmentColumns); ok {
				a["investment"] = encoded
				investmentChanged = true
			}
			updated, _ := json.Marshal(a)
			if encoded, ok := compactColumns(updated, repairAllocationColumns); ok {
				allocations[i] = encoded
				allocationChanged = true
			} else {
				allocations[i] = updated
			}
		}
		alt["allocations"], _ = json.Marshal(allocations)
	}
	if !investmentChanged && !allocationChanged {
		return output, false
	}
	root["alternatives"], _ = json.Marshal(alternatives)
	if investmentChanged {
		root["investment_columns"], _ = json.Marshal(repairInvestmentColumns)
	}
	if allocationChanged {
		root["allocation_columns"], _ = json.Marshal(repairAllocationColumns)
	}
	encoded, err := json.Marshal(root)
	if err != nil || len(encoded) >= len(output) {
		return output, false
	}
	return string(encoded), true
}
func compactColumns(raw json.RawMessage, columns []string) (json.RawMessage, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return raw, false
	}
	row, missing, known := []json.RawMessage{}, []int{}, 0
	for i, key := range columns {
		if object[key] == nil {
			row = append(row, json.RawMessage("null"))
			missing = append(missing, i)
		} else {
			row = append(row, object[key])
			known++
		}
	}
	encoded, _ := json.Marshal([]any{row, missing})
	return encoded, known == len(object) && len(encoded) < len(raw)
}

// Some repair responses retain the declared compact input representation. The
// fixed column declarations make it losslessly decodable; unknown mappings,
// missing-index conflicts and invented columns are never inferred.
func normalizeCompactProposal(output string) (string, bool) {
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(output), &root) != nil || (root["investment_columns"] == nil && root["allocation_columns"] == nil) {
		return output, false
	}
	for key, expected := range map[string][]string{"investment_columns": repairInvestmentColumns, "allocation_columns": repairAllocationColumns} {
		if root[key] != nil {
			var actual []string
			if json.Unmarshal(root[key], &actual) != nil || !reflect.DeepEqual(actual, expected) {
				return output, false
			}
		}
	}
	var alternatives []map[string]json.RawMessage
	if json.Unmarshal(root["alternatives"], &alternatives) != nil {
		return output, false
	}
	for _, alt := range alternatives {
		var rows []json.RawMessage
		if json.Unmarshal(alt["allocations"], &rows) != nil {
			return output, false
		}
		for i, row := range rows {
			a, ok := expandColumns(row, repairAllocationColumns, root["allocation_columns"] != nil)
			if !ok {
				return output, false
			}
			if a["investment"] != nil {
				investment, ok := expandColumns(a["investment"], repairInvestmentColumns, root["investment_columns"] != nil)
				if !ok {
					return output, false
				}
				a["investment"], _ = json.Marshal(investment)
			}
			rows[i], _ = json.Marshal(a)
		}
		alt["allocations"], _ = json.Marshal(rows)
	}
	root["alternatives"], _ = json.Marshal(alternatives)
	delete(root, "investment_columns")
	delete(root, "allocation_columns")
	encoded, err := json.Marshal(root)
	return string(encoded), err == nil
}

func expandColumns(raw json.RawMessage, columns []string, declared bool) (map[string]json.RawMessage, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil && object != nil {
		return object, true
	}
	var tuple []json.RawMessage
	if !declared || json.Unmarshal(raw, &tuple) != nil || len(tuple) != 2 {
		return nil, false
	}
	var values []json.RawMessage
	var missing []int
	if json.Unmarshal(tuple[0], &values) != nil || len(values) != len(columns) || json.Unmarshal(tuple[1], &missing) != nil {
		return nil, false
	}
	absent := map[int]bool{}
	for _, i := range missing {
		if i < 0 || i >= len(columns) || absent[i] || string(values[i]) != "null" {
			return nil, false
		}
		absent[i] = true
	}
	object = map[string]json.RawMessage{}
	for i, key := range columns {
		if !absent[i] {
			object[key] = values[i]
		}
	}
	return object, true
}
