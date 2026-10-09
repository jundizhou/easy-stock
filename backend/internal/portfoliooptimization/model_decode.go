package portfoliooptimization

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Compact model responses reduce repeated keys. Persisted/public reports keep
// the named object shape, so history and the frontend remain compatible.
func (i *InvestmentJudgment) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '[' {
		var v []string
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		if len(v) != 13 {
			return fmt.Errorf("投资判断数组须按固定13列返回")
		}
		*i = InvestmentJudgment{Role: v[0], Action: v[1], Horizon: v[2], Business: v[3], Growth: v[4], Valuation: v[5], Timing: v[6], PortfolioFit: v[7], Risk: v[8], Exit: v[9], OpportunityCost: v[10], PriorOpinion: v[11], PeriodSuitability: v[12]}
		return nil
	}
	type named InvestmentJudgment
	var v named
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*i = InvestmentJudgment(v)
	return nil
}

var extraAllocationBrace = regexp.MustCompile(`\}\]\}\}(\s*,\s*\{\s*"symbol"\s*:)`)

// Confirmed output defect: a stock's final evidence_refs and allocation are
// followed by one extra '}' before the next stock. Remove only that token when
// the complete result becomes the one-alternative/multi-stock proposal shape.
// No scalar or field is changed, and normal investment validation still runs.
func normalizeExtraAllocationBraces(content string) (string, bool) {
	if json.Valid([]byte(content)) {
		return content, false
	}
	matches := extraAllocationBrace.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return content, false
	}
	quoted, escaped := false, false
	out := strings.Builder{}
	next := 0
	for i := 0; i < len(content); i++ {
		if next < len(matches) && i == matches[next][0]+3 && !quoted {
			next++
			continue
		}
		c := content[i]
		out.WriteByte(c)
		if escaped {
			escaped = false
		} else if quoted && c == '\\' {
			escaped = true
		} else if c == '"' {
			quoted = !quoted
		}
	}
	if next != len(matches) {
		return content, false
	}
	candidate := out.String()
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(candidate), &root) != nil {
		return content, false
	}
	var alternatives []map[string]json.RawMessage
	if json.Unmarshal(root["alternatives"], &alternatives) != nil || len(alternatives) != 1 {
		return content, false
	}
	var rows []map[string]json.RawMessage
	if alternatives[0]["name"] == nil || json.Unmarshal(alternatives[0]["allocations"], &rows) != nil || len(rows) < 2 {
		return content, false
	}
	for _, row := range rows {
		if row["symbol"] == nil || row["investment"] == nil || row["evidence_refs"] == nil || row["preferred_weight"] == nil {
			return content, false
		}
	}
	return candidate, true
}

// Some models close allocations after its first row, leaving the other stock
// objects in alternatives and the final keep_reason outside the root. This
// exact, identifiable shape can be reparented without changing any values.
// Real alternatives, unknown trailers and incomplete rows are never guessed.
func normalizeEarlyClosedAllocations(content string) (string, bool) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(content)))
	var root map[string]json.RawMessage
	if decoder.Decode(&root) != nil {
		return content, false
	}
	input := strings.TrimSpace(content)
	tail := strings.TrimSpace(input[decoder.InputOffset():])
	if tail != "" {
		if !strings.HasPrefix(tail, `],"keep_reason":`) || root["keep_reason"] != nil {
			return content, false
		}
		var trailing map[string]json.RawMessage
		if json.Unmarshal([]byte("{"+tail[2:]), &trailing) != nil || len(trailing) != 1 {
			return content, false
		}
		var reason string
		if json.Unmarshal(trailing["keep_reason"], &reason) != nil {
			return content, false
		}
		root["keep_reason"] = trailing["keep_reason"]
	}
	var alternatives []json.RawMessage
	if json.Unmarshal(root["alternatives"], &alternatives) != nil || len(alternatives) < 2 {
		return content, false
	}
	var first map[string]json.RawMessage
	if json.Unmarshal(alternatives[0], &first) != nil || len(first) != 2 || first["name"] == nil || first["allocations"] == nil {
		return content, false
	}
	var rows []json.RawMessage
	if json.Unmarshal(first["allocations"], &rows) != nil || len(rows) != 1 {
		return content, false
	}
	for _, raw := range alternatives[1:] {
		var row map[string]json.RawMessage
		if json.Unmarshal(raw, &row) != nil || row["symbol"] == nil || row["investment"] == nil || row["min_weight"] == nil || row["max_weight"] == nil || row["preferred_weight"] == nil || row["name"] != nil || row["allocations"] != nil {
			return content, false
		}
		rows = append(rows, raw)
	}
	first["allocations"], _ = json.Marshal(rows)
	root["alternatives"], _ = json.Marshal([]any{first})
	encoded, err := json.Marshal(root)
	return string(encoded), err == nil
}
