package cockpit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFlowBoardEncodesEmptySidesAsArrays(t *testing.T) {
	board := BuildFlowBoard([]FlowItem{{Name: "A", NetInflow: -5}}, 3)
	encoded, err := json.Marshal(board)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"inflow":[]`) || !strings.Contains(string(encoded), `"outflow":[{`) {
		t.Fatalf("empty inflow must encode as [], got %s", encoded)
	}
}

func TestCollectFactsEncodesEmptySourcesAsArray(t *testing.T) {
	facts := CollectFacts(context.Background(), PhasePreOpen, Feed{})
	encoded, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Sources == nil || !strings.Contains(string(encoded), `"sources":[]`) {
		t.Fatalf("sources must be an empty array, got %s", encoded)
	}
}
