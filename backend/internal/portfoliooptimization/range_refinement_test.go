package portfoliooptimization

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRangeRefinementFreezesInvestmentAndPermission(t *testing.T) {
	j, alt := searchFixture(t)
	p := Proposal{Alternatives: []Alternative{alt}}
	j.Proposal = &p
	j.InvestmentBaseline = &p
	j.RevisionCount = 1
	j.RevisionHistory = []RevisionRound{{RiskGroups: nil, Assessment: &Assessment{Reason: "真实缺陷"}}}
	rows := []rangeAdjustment{}
	for _, a := range alt.Allocations {
		rows = append(rows, rangeAdjustment{Symbol: a.Symbol, Minimum: 0, Maximum: a.Maximum, Reason: "盈利质量与风险权衡"})
	}
	raw, _ := json.Marshal(map[string]any{"allocation_ranges": rows, "keep_reason": ""})
	revised, err := refineAllocationRanges(context.Background(), j, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range revised.Alternatives[0].Allocations {
		original := alt.Allocations[i]
		if !reflect.DeepEqual(a.Investment, original.Investment) || !reflect.DeepEqual(a.Conditions, original.Conditions) || a.Suitable != original.Suitable || !reflect.DeepEqual(a.EvidenceRefs, original.EvidenceRefs) {
			t.Fatal("range-only revision rewrote investment", a)
		}
	}
	before, _ := json.Marshal(p)
	if string(before) == "" || !reflect.DeepEqual(j.InvestmentBaseline, &p) {
		t.Fatal("baseline mutated")
	}
	prompt, err := proposalPrompt(j)
	if err != nil || !strings.Contains(prompt, "allocation_ranges") || strings.Contains(prompt, "investment_comparisons最多8") || len(prompt) > MaxRevisionModelPromptBytes {
		t.Fatal("revision still regenerates investment reports", err)
	}
	// A range can never unlock a stock or grant a hold/reduce new buying permission.
	j.Eligibility[0].Locked = true
	if _, err := refineAllocationRanges(context.Background(), j, string(raw)); err == nil {
		t.Fatal("range edit unlocked trading")
	}
	j.Eligibility[0].Locked = false
	j.InvestmentBaseline.Alternatives[0].Allocations[0].Suitable = false
	j.InvestmentBaseline.Alternatives[0].Allocations[0].Investment.Action = "hold"
	rows[0].Maximum = 41
	raw, _ = json.Marshal(map[string]any{"allocation_ranges": rows})
	bounded, err := refineAllocationRanges(context.Background(), j, string(raw))
	if err != nil || bounded.Alternatives[0].Allocations[0].Maximum != 40 || len(bounded.RangeBoundCorrections) != 1 {
		t.Fatal("range intersection failed", bounded, err)
	}
	rows[0].Minimum = 41
	raw, _ = json.Marshal(map[string]any{"allocation_ranges": rows})
	if _, err := refineAllocationRanges(context.Background(), j, string(raw)); err == nil {
		t.Fatal("incompatible range granted new funding")
	}
}

func TestRangePatchMissingZeroIsNotAssumed(t *testing.T) {
	for _, raw := range []string{
		`{"symbol":"600519.SH","max_weight":35,"reason":"范围"}`,
		`{"symbol":"600519.SH","min_weight":null,"max_weight":35,"reason":"范围"}`,
		`{"symbol":"600519.SH","min_weight":0,"max_weight":35,"reason":"范围","action":"allocate"}`,
	} {
		var a rangeAdjustment
		if json.Unmarshal([]byte(raw), &a) == nil {
			t.Fatal("partial or permission-changing range accepted", raw)
		}
	}
}

func TestIssueDisplayShapeNormalizationIsLossless(t *testing.T) {
	for _, raw := range []string{`{"issues":"亏损暴露"}`, `{"issues":["亏损暴露"]}`, `{"issues":[{"name":"亏损","detail":"扩大","evidence_refs":[{"fact":"cash_percent"}]}]}`, `{"issues":[{"text":"亏损","evidence_refs":[{"fact":"cash_percent"}]}]}`} {
		var p Proposal
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Issues) != 1 {
			t.Fatal("issue text lost")
		}
		encoded, _ := json.Marshal(p)
		var back Proposal
		if err := json.Unmarshal(encoded, &back); err != nil || !reflect.DeepEqual(back, p) {
			t.Fatal("issue text or citations not retained", string(encoded), err)
		}
	}
	for _, raw := range []string{`{"issues":[{"text":"亏损","weight":30}]}`, `{"issues":[{"name":123}]}`, `{"issues":[{}]}`} {
		var p Proposal
		if json.Unmarshal([]byte(raw), &p) == nil {
			t.Fatal("unknown issue content discarded")
		}
	}
}
