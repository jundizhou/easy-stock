package portfoliooptimization

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	pi "easy-stock/backend/internal/portfolioinspection"
)

// This gateway has no network implementation. Replaying an observed failure
// must never silently turn into another sample from the model.
type recordedConversation struct {
	*testGateway
	dir   string
	calls int
}

func (g *recordedConversation) Status() agent.Status {
	return agent.Status{Available: true, Configured: true}
}
func (g *recordedConversation) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	if err := ctx.Err(); err != nil {
		return agent.PromptResult{}, err
	}
	g.calls++
	base := filepath.Join(g.dir, fmt.Sprintf("call-%02d", g.calls))
	input, err := os.ReadFile(base + "-input.txt")
	if err != nil {
		return agent.PromptResult{}, err
	}
	if strings.Contains(prompt, "独立组合复评员") != strings.Contains(string(input), "独立组合复评员") {
		return agent.PromptResult{}, fmt.Errorf("recorded stage mismatch at call %d", g.calls)
	}
	if strings.Contains(prompt, "独立组合复评员") {
		type pair struct {
			A, B struct {
				Weights map[string]float64 `json:"weights"`
			}
		}
		if !strings.Contains(string(input), `"scoring_version":"`+pi.AlgorithmVersion+`"`) {
			return agent.PromptResult{}, fmt.Errorf("recorded review uses an older scoring contract; cannot reuse its scores under %s", pi.AlgorithmVersion)
		}
		var before, after pair
		for _, item := range []struct {
			text   string
			target *pair
		}{{string(input), &before}, {prompt, &after}} {
			_, payload, ok := strings.Cut(item.text, "[资料JSON]\n")
			if !ok || json.NewDecoder(strings.NewReader(payload)).Decode(item.target) != nil {
				return agent.PromptResult{}, fmt.Errorf("recorded review lacks A/B weights")
			}
		}
		if !reflect.DeepEqual(before, after) {
			return agent.PromptResult{}, fmt.Errorf("recorded review belongs to different weights at call %d", g.calls)
		}
	}
	data, err := os.ReadFile(base + "-output.json")
	if err != nil {
		return agent.PromptResult{}, err
	}
	var saved struct {
		Result agent.PromptResult `json:"result"`
		Error  string             `json:"error"`
	}
	if err = json.Unmarshal(data, &saved); err != nil {
		return agent.PromptResult{}, err
	}
	if saved.Error != "" {
		return agent.PromptResult{}, fmt.Errorf("recorded transport failure: %s", saved.Error)
	}
	saved.Result.Usage = agent.TokenUsage{}
	return saved.Result, nil
}

func TestSavedConversationCompletesWithOriginalScores(t *testing.T) {
	dir := os.Getenv("EASY_STOCK_OPTIMIZATION_TRANSCRIPT_DIR")
	if dir == "" {
		t.Skip("explicit private real-model transcript required")
	}
	data, err := os.ReadFile(os.Getenv("EASY_STOCK_OPTIMIZATION_AUDIT"))
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err = json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	if j.SnapshotAt.IsZero() || len(j.Results) == 0 {
		t.Fatal("frozen research required")
	}
	j.Version, j.ModelPromptVersion = Version, ModelPromptVersion
	j.Proposal, j.InvestmentBaseline, j.FallbackPlan = nil, nil, nil
	j.Plans, j.RevisionHistory, j.SelectedPlan = nil, nil, nil
	j.RevisionCount, j.ModelDurationMS = 0, 0
	j.ModelAttempts, j.ModelStageDurationMS = nil, nil
	j.ModelStartedAt = time.Time{}
	j.Status, j.Stage, j.Outcome, j.OutcomeReason, j.Error = "running", "proposing", "", "", ""
	limits := []string{}
	for _, l := range j.Limitations {
		if l != "已使用一次模型格式/一致性修复" {
			limits = append(limits, l)
		}
	}
	j.Limitations = limits
	store, err := pi.OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	g := &recordedConversation{testGateway: &testGateway{}, dir: dir}
	s := NewService(store, g, Dependencies{})
	defer s.Close()
	if err = s.execute(context.Background(), &j); err != nil {
		t.Fatal(err)
	}
	if dest := os.Getenv("EASY_STOCK_REPLAY_OUTPUT"); dest != "" {
		data, _ := json.MarshalIndent(j, "", "  ")
		if err = os.WriteFile(dest, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if j.SelectedPlan == nil {
		t.Fatal("recorded conversation has no qualified result", j.OutcomeReason)
	}
	p := j.Plans[*j.SelectedPlan]
	if !currentComparison(j, p) || !meetsScoreMinimum(p) || !p.Checks.Valid || j.RevisionCount > MaxRevisionRounds {
		t.Fatal("recorded result failed qualification")
	}
	if _, err = ApplyRequest(j); err != nil {
		t.Fatal(err)
	}
	t.Logf("production flow replayed %d real recorded outputs; score %d -> %d; zero live calls; replay duration is not a model latency measurement", g.calls, *p.Original.Conclusion.TotalScore, *p.Proposed.Conclusion.TotalScore)
}
