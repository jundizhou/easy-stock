package portfoliooptimization

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func programProposalJSON(t *testing.T, p Proposal) string {
	t.Helper()
	data, _ := json.Marshal(p)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["weight_mode"] = "program"
	for _, alt := range raw["alternatives"].([]any) {
		for _, value := range alt.(map[string]any)["allocations"].([]any) {
			row := value.(map[string]any)
			for _, key := range []string{"preferred_weight", "suitable_for_increase", "suitability_reason"} {
				delete(row, key)
			}
		}
	}
	data, _ = json.Marshal(raw)
	return string(data)
}

func TestProgramProposalSolvesWeightsWithoutChangingInvestment(t *testing.T) {
	j, p := fixtureJob(), fixtureProposal()
	p.Alternatives[0].Allocations[0].Investment.Action = "hold"
	decoded, err := decodeInitialProposal(context.Background(), j, programProposalJSON(t, p))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateProposal(j, decoded); err != nil {
		t.Fatal(err)
	}
	alt := decoded.Alternatives[0]
	total := 0
	for k, a := range alt.Allocations {
		total += a.Preferred
		if !reflect.DeepEqual(a.Investment, p.Alternatives[0].Allocations[k].Investment) || !reflect.DeepEqual(a.Conditions, p.Alternatives[0].Allocations[k].Conditions) {
			t.Fatal("materialization changed investment judgment or conditions")
		}
	}
	if total != 80 || alt.Allocations[0].Suitable || alt.Allocations[0].Preferred > j.Source.Request.Holdings[0].Weight || !alt.Allocations[1].Suitable {
		t.Fatal("wrong total or program granted funds to hold", alt)
	}
	data, _ := json.Marshal(decoded)
	var restored Proposal
	if err := json.Unmarshal(data, &restored); err != nil || validateProposal(j, restored) != nil {
		t.Fatal("canonical historical output must remain usable", err)
	}
}

func TestProgramProposalRejectsConflictingPermissionAndInvalidJudgment(t *testing.T) {
	j, p := fixtureJob(), fixtureProposal()
	for _, mutate := range []func(map[string]any){
		func(r map[string]any) { r["preferred_weight"] = 100 },
		func(r map[string]any) { r["suitable_for_increase"] = true },
		func(r map[string]any) { r["investment"].(map[string]any)["action"] = "unknown" },
		func(r map[string]any) {
			r["allocation_conditions"] = []any{}
			r["confirmation_ids"] = []any{}
			r["invalidation_ids"] = []any{}
		},
	} {
		var raw map[string]any
		_ = json.Unmarshal([]byte(programProposalJSON(t, p)), &raw)
		row := raw["alternatives"].([]any)[0].(map[string]any)["allocations"].([]any)[1].(map[string]any)
		mutate(row)
		data, _ := json.Marshal(raw)
		if _, err := decodeInitialProposal(context.Background(), j, string(data)); err == nil {
			t.Fatal("conflicting or incomplete investment accepted", string(data))
		}
	}
	// Older outputs retain their own fields, including contradictions that the
	// strict validator rejects; the new decoder cannot silently repair them.
	p.Alternatives[0].Allocations[0].Investment.Action = "hold"
	p.Alternatives[0].Allocations[0].Suitable = true
	data, _ := json.Marshal(p)
	decoded, err := decodeInitialProposal(context.Background(), j, string(data))
	if err != nil || !reflect.DeepEqual(decoded, p) || validateProposal(j, decoded) == nil {
		t.Fatal("legacy permissions changed or weakened", decoded, err)
	}
}
