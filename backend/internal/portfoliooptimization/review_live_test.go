package portfoliooptimization

import (
	"context"
	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
	pi "easy-stock/backend/internal/portfolioinspection"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Explicitly enabled replay of a saved proposal using the configured model.
// Copies credentials into a temporary sandbox; never writes the user's store,
// settings, reports or holdings, and never launches any stock research.
func TestLiveSavedOptimizationReview(t *testing.T) {
	if os.Getenv("EASY_STOCK_LIVE_OPTIMIZATION_REVIEW") != "1" {
		t.Skip("live model replay is opt-in")
	}
	data, err := os.ReadFile(os.Getenv("EASY_STOCK_OPTIMIZATION_AUDIT"))
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	settings, err := os.ReadFile(os.Getenv("EASY_STOCK_REPLAY_SETTINGS"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		LLM     appsettings.LLM `json:"llm"`
		Runtime string          `json:"agent_runtime"`
	}
	if err := json.Unmarshal(settings, &cfg); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	for _, name := range []string{"config.yaml", ".env", "model-capabilities.json"} {
		content, err := os.ReadFile(filepath.Join(os.Getenv("EASY_STOCK_REPLAY_HERMES_HOME"), name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	g := agent.NewService(agent.ServiceConfig{Hermes: agent.HermesConfig{RuntimeRoot: os.Getenv("EASY_STOCK_REPLAY_HERMES_ROOT"), Home: home, WorkDir: home}, Codex: agent.CodexConfig{RuntimeRoot: os.Getenv("EASY_STOCK_REPLAY_CODEX_ROOT"), Home: filepath.Join(home, "codex"), WorkDir: home}})
	if err := g.SyncLLM(cfg.LLM, nil); err != nil {
		t.Fatal(err)
	}
	g.RestoreSelection(cfg.Runtime)
	store, err := pi.OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	var gateway agent.Gateway = &savedEvaluationGateway{Service: g, dir: os.Getenv("EASY_STOCK_REPLAY_CALLS"), log: t.Logf}
	if path := os.Getenv("EASY_STOCK_OPTIMIZATION_PARTS_RESPONSE"); path != "" {
		if patch := os.Getenv("EASY_STOCK_OPTIMIZATION_PATCH"); patch != "" {
			gateway = &savedEvaluationGateway{Service: g, files: []string{patch}, log: t.Logf}
		}
		s := NewService(store, gateway, Dependencies{})
		defer func() { s.Close(); store.Close() }()
		testLiveProposalParts(t, s, j, path)
		return
	}
	proposalReplay, patchReplay := os.Getenv("EASY_STOCK_OPTIMIZATION_RESPONSE"), os.Getenv("EASY_STOCK_OPTIMIZATION_PATCH")
	continuing := os.Getenv("EASY_STOCK_OPTIMIZATION_CONTINUE") == "1"
	factRecovery := os.Getenv("EASY_STOCK_OPTIMIZATION_REVIEW_FACT_RECOVERY")
	if factRecovery != "" {
		if !continuing || j.Status != "succeeded" || (j.Outcome != "review_invalid" && j.Outcome != "below_target_score") || proposalReplay != "" {
			t.Fatal("fact recovery requires the exact completed checkpoint with rejected fact references")
		}
		j.Status = "incomplete"
	}
	if continuing {
		if j.SnapshotAt.IsZero() || len(j.Results) == 0 || j.Version != Version || j.Status != "incomplete" || patchReplay != "" {
			t.Fatal("continuation requires an incomplete current-policy checkpoint and no replay outputs")
		}
		if proposalReplay != "" && os.Getenv("EASY_STOCK_OPTIMIZATION_REVIEW_SHAPE_RECOVERY") != "1" {
			evidenceRecovery := os.Getenv("EASY_STOCK_OPTIMIZATION_PROPOSAL_EVIDENCE_RECOVERY") == "1"
			conditionRecovery := os.Getenv("EASY_STOCK_OPTIMIZATION_PROPOSAL_CONDITION_RECOVERY") == "1"
			if j.Stage != "proposing" || j.Proposal != nil || (!strings.Contains(j.Error, "未发送模型") && !strings.Contains(j.Error, "Proposal.issues") && !evidenceRecovery && !conditionRecovery) {
				t.Fatal("checkpoint replay is only for an unsent repair or lossless issue-shape recovery of the exact saved proposal")
			}
			if conditionRecovery {
				data, err := os.ReadFile(proposalReplay)
				if err != nil {
					t.Fatal(err)
				}
				var saved struct {
					Result agent.PromptResult `json:"result"`
				}
				if err := json.Unmarshal(data, &saved); err != nil {
					t.Fatal(err)
				}
				_, err = decodeInitialProposal(context.Background(), j, saved.Result.Content)
				if len(j.ModelAttempts) < 2 || !strings.Contains(j.ModelAttempts[0].Error, "等待买点须明确入场条件") || !strings.Contains(j.Error, "投资判断缺少该股真实事实引用") || err != nil {
					t.Fatal("exact original proposal did not pass corrected zero-watch / row-reference rules", err)
				}
				j.Limitations = append(j.Limitations, "精确恢复零仓观察及同股条件引用被误拒的原方案；投资内容不变，既有模型预算与格式修复次数保留")
			}
			if evidenceRecovery {
				data, err := os.ReadFile(proposalReplay)
				if err != nil {
					t.Fatal(err)
				}
				var saved struct {
					Result agent.PromptResult `json:"result"`
				}
				if err := json.Unmarshal(data, &saved); err != nil {
					t.Fatal(err)
				}
				var p Proposal
				if err := jsonContent(saved.Result.Content, &p); err != nil {
					t.Fatal(err)
				}
				if len(j.ModelAttempts) == 0 || !strings.Contains(j.ModelAttempts[0].Error, "须分别引用双方真实资料") || validateProposal(j, p) != nil {
					t.Fatal("exact original proposal does not pass corrected correlation evidence validation")
				}
				j.Limitations = append(j.Limitations, "真实原方案因共享基准相关性校验误拒；修正规则后精确恢复原始内容，已消耗预算与修复次数保持，复评仍真实调用")
			}
			gateway = &savedEvaluationGateway{Service: g, files: []string{proposalReplay}, dir: os.Getenv("EASY_STOCK_REPLAY_CALLS"), log: t.Logf}
		}
		t.Log("continuing the exact isolated checkpoint; no model, repair or revision budget reset")
	} else if os.Getenv("EASY_STOCK_OPTIMIZATION_NEW_PROPOSAL") == "1" {
		if j.SnapshotAt.IsZero() || len(j.Results) == 0 || proposalReplay != "" || patchReplay != "" {
			t.Fatal("new live proposal requires frozen research and no replay outputs")
		}
		j.ID = "live-quality-" + j.ID
		j.Version, j.ModelPromptVersion = Version, ModelPromptVersion
		j.Proposal, j.Plans, j.SelectedPlan = nil, nil, nil
		j.FallbackPlan = nil
		j.RevisionCount, j.RevisionHistory = 0, nil
		j.InvestmentBaseline = nil
		j.RangeRepairUsed = false
		j.ModelLoops, j.ProposalCheckpoint, j.CheckpointProgress = nil, nil, nil
		j.ExecutionDurationMS = 0
		j.Outcome, j.OutcomeReason = "", ""
		kept := []string{}
		for _, l := range j.Limitations {
			if l != "已使用一次模型格式/一致性修复" {
				kept = append(kept, l)
			}
		}
		j.Limitations = append(kept, "新规则隔离实测：固定已有研究与行情，方案和复评均真实调用；不修改用户持仓")
		t.Log("new quality-policy test: frozen research; all proposal and review calls are real")
	} else if proposalReplay != "" {
		if j.SnapshotAt.IsZero() {
			t.Fatal("frozen facts required")
		}
		// A new isolated regression replay consumes the exact two recorded outputs.
		// Production repair limits are unchanged; no new selection call is made.
		j.ID = "saved-regression-" + j.ID
		j.Proposal = nil
		j.Plans = nil
		j.SelectedPlan = nil
		j.FallbackPlan = nil
		kept := []string{}
		for _, l := range j.Limitations {
			if l != "已使用一次模型格式/一致性修复" {
				kept = append(kept, l)
			}
		}
		j.Limitations = append(kept, "隔离回归：精确回放已记录的方案及权重补丁，仅独立复评调用真实模型")
		files := []string{proposalReplay}
		if patchReplay != "" {
			files = append(files, patchReplay)
		}
		gateway = &savedEvaluationGateway{Service: g, files: files, dir: os.Getenv("EASY_STOCK_REPLAY_CALLS"), log: t.Logf}
		t.Log("isolated regression: replaying exact saved proposal/optional patch; later review is a real model call")
	} else if j.Proposal == nil || len(j.Plans) == 0 {
		t.Fatal("saved materialized plan required")
	}
	if os.Getenv("EASY_STOCK_OPTIMIZATION_REVIEW_SHAPE_RECOVERY") == "1" {
		if !continuing || (!strings.Contains(j.Error, "dimensions: json: cannot unmarshal object") && !strings.Contains(j.Error, "现金缺口仍在")) {
			t.Fatal("shape recovery requires the exact failed dimension-container checkpoint")
		}
		data, err := os.ReadFile(os.Getenv("EASY_STOCK_OPTIMIZATION_RESPONSE"))
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			Result agent.PromptResult `json:"result"`
		}
		if err = json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		var scored struct {
			A, B       json.RawMessage
			Assessment Assessment `json:"assessment"`
		}
		if err = jsonContent(saved.Result.Content, &scored); err != nil {
			t.Fatal(err)
		}
		pending := -1
		for i := range j.Plans {
			if j.Plans[i].Status == "pending_review" {
				pending = i
				break
			}
		}
		if pending < 0 {
			t.Fatal("no pending exact plan")
		}
		plan := &j.Plans[pending]
		a, b := plan.Original, plan.Proposed
		if plan.AssessmentOrder == "target_first" {
			a, b = b, a
		}
		if _, err = pi.DecodeOptimizationComparisonScore(scored.A, a); err != nil {
			t.Fatal(err)
		}
		if _, err = pi.DecodeOptimizationComparisonScore(scored.B, b); err != nil {
			t.Fatal(err)
		}
		gateway = &savedEvaluationGateway{Service: g, files: []string{os.Getenv("EASY_STOCK_OPTIMIZATION_RESPONSE")}, dir: os.Getenv("EASY_STOCK_REPLAY_CALLS"), log: t.Logf}
		t.Log("lossless dimension-container recovery of the exact recorded scores; budgets preserved, later model calls real")
	}
	s := NewService(store, gateway, Dependencies{})
	if factRecovery != "" {
		recoveryFiles := strings.Split(factRecovery, ",")
		for _, recoveryFile := range recoveryFiles {
			data, err := os.ReadFile(recoveryFile)
			if err != nil {
				t.Fatal(err)
			}
			var saved struct {
				Result agent.PromptResult `json:"result"`
			}
			if err := json.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			var scored struct{ A, B json.RawMessage }
			if err := jsonContent(saved.Result.Content, &scored); err != nil {
				t.Fatal(err)
			}
			input, err := os.ReadFile(strings.TrimSuffix(recoveryFile, "-output.json") + "-input.txt")
			if err != nil {
				t.Fatal(err)
			}
			_, payload, present := strings.Cut(string(input), "[资料JSON]\n")
			var supplied struct {
				A, B struct {
					Weights map[string]float64 `json:"weights"`
				}
			}
			if !present || json.NewDecoder(strings.NewReader(payload)).Decode(&supplied) != nil {
				t.Fatal("saved review has no frozen weights")
			}
			// A later completed round may have archived an earlier response whose
			// only failure was a genuine fact alias. Rebuild that exact plan from
			// its immutable target and original investment ranges, not a new plan.
			for _, round := range j.RevisionHistory {
				if !strings.Contains(round.Error, "不可用事实引用") || j.InvestmentBaseline == nil || len(j.InvestmentBaseline.Alternatives) != 1 {
					continue
				}
				ow := reviewTestWeights(j.Source.Request.Holdings)
				tw := reviewTestWeights(round.Target)
				order := ""
				if reflect.DeepEqual(supplied.A.Weights, ow) && reflect.DeepEqual(supplied.B.Weights, tw) {
					order = "original_first"
				}
				if reflect.DeepEqual(supplied.A.Weights, tw) && reflect.DeepEqual(supplied.B.Weights, ow) {
					order = "target_first"
				}
				if order == "" {
					continue
				}
				req := j.Source.Request
				original := pi.OptimizationReport(req, j.Results)
				req.Holdings = round.Target
				p := Plan{Name: round.Name, Status: "invalid_review", Target: round.Target, Checks: round.Checks, Allocations: j.InvestmentBaseline.Alternatives[0].Allocations, Funding: round.Funding, Improvements: round.Improvements, Original: original, Proposed: pi.OptimizationReport(req, j.Results), AssessmentOrder: order, Error: round.Error}
				j.Plans = append(j.Plans, p)
				break
			}
			found := false
			for i := range j.Plans {
				p := &j.Plans[i]
				if p.Status != "invalid_review" || !strings.Contains(p.Error, "不可用事实引用") {
					continue
				}
				a, b := p.Original, p.Proposed
				a.Conclusion.RiskGroups = frozenGroups(j.Proposal.RiskGroups, a.Request.Holdings)
				b.Conclusion.RiskGroups = frozenGroups(j.Proposal.RiskGroups, b.Request.Holdings)
				if p.AssessmentOrder == "target_first" {
					a, b = b, a
				}
				aw := reviewTestWeights(a.Request.Holdings)
				bw := reviewTestWeights(b.Request.Holdings)
				if !reflect.DeepEqual(supplied.A.Weights, aw) || !reflect.DeepEqual(supplied.B.Weights, bw) {
					continue
				}
				if _, err := pi.DecodeNamedOptimizationComparisonScore(scored.A, a, "a"); err != nil {
					t.Fatal(err)
				}
				if _, err := pi.DecodeNamedOptimizationComparisonScore(scored.B, b, "b"); err != nil {
					t.Fatal(err)
				}
				p.Status, p.Error = "pending_review", ""
				found = true
				break
			}
			if !found {
				t.Fatal("no invalid review matches the saved weights and fact references", recoveryFile)
			}
		}
		gateway = &savedEvaluationGateway{Service: g, files: recoveryFiles, dir: os.Getenv("EASY_STOCK_REPLAY_CALLS"), log: t.Logf}
		s.gateway = gateway
		j.Limitations = append(j.Limitations, "恢复原始评分对已提供事实的路径引用；四维评分和原时钟保持，不重采样该组合")
		t.Log("exact recorded scores matched to frozen A/B weights and supplied facts; original budgets preserved; later calls real")
	}
	// Recover only the exact invalid review from a saved checkpoint. This does
	// not resample scores or reset budgets; it applies production's new skip
	// behavior to a response that the previous implementation stopped on.
	if savedPath := os.Getenv("EASY_STOCK_OPTIMIZATION_INVALID_REVIEW_RECOVERY"); savedPath != "" {
		if !continuing || proposalReplay != "" || len(j.ModelAttempts) == 0 {
			t.Fatal("invalid-review recovery requires an exact continuation")
		}
		data, err := os.ReadFile(savedPath)
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			Result agent.PromptResult `json:"result"`
		}
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		var scored struct{ A, B json.RawMessage }
		if err := jsonContent(saved.Result.Content, &scored); err != nil {
			t.Fatal(err)
		}
		found := false
		for i := range j.Plans {
			p := &j.Plans[i]
			if p.Status != "pending_review" {
				continue
			}
			a, b := p.Original, p.Proposed
			if p.AssessmentOrder == "target_first" {
				a, b = b, a
			}
			_, invalid := pi.DecodeNamedOptimizationComparisonScore(scored.A, a, "a")
			prefix := "A复评："
			if invalid == nil {
				_, invalid = pi.DecodeNamedOptimizationComparisonScore(scored.B, b, "b")
				prefix = "B复评："
			}
			if invalid == nil || prefix+invalid.Error() != j.ModelAttempts[len(j.ModelAttempts)-1].Error || j.Error != prefix+invalid.Error() {
				t.Fatal("recorded invalid review does not reproduce checkpoint error")
			}
			p.Status = "invalid_review"
			p.Error = "独立复评未通过一致性检查，未采用评分：" + j.Error
			j.Limitations = append(j.Limitations, "精确恢复已记录的无效复评，未采用分数、未重复调用或重置预算")
			found = true
			break
		}
		if !found {
			t.Fatal("no exact pending invalid review")
		}
		t.Log("exact invalid review reproduced and skipped; original budgets preserved")
	}
	defer func() { s.Close(); store.Close() }()
	remaining := TotalTimeout
	if continuing {
		remaining = remainingExecution(j)
		if j.ModelStartedAt.IsZero() || remaining <= 0 {
			t.Fatal("original isolated evaluation deadline exhausted")
		}
	} else {
		j.ModelDurationMS = 0
		j.ModelStageDurationMS = map[string]int64{}
		j.ModelStartedAt = time.Time{}
		j.ModelAttempts = nil
	}
	j.Error = ""
	ctx, cancel := context.WithTimeout(context.Background(), remaining)
	defer cancel()
	started := time.Now()
	j.executionTick = started
	err = s.execute(ctx, &j)
	if err != nil {
		j.Error = err.Error()
		j.Status = "incomplete"
		j.Outcome = "incomplete"
		j.ResumeAvailable = true
	}
	t.Logf("live saved review: model=%s duration=%s status=%s outcome=%s", cfg.LLM.Model, time.Since(started), j.Status, j.Outcome)
	for _, a := range j.ModelAttempts {
		t.Logf("stage=%s prompt_bytes=%d response_bytes=%d duration_ms=%d budget_ms=%d", a.Stage, a.PromptBytes, a.ResponseBytes, a.DurationMS, a.BudgetMS)
	}
	if dest := os.Getenv("EASY_STOCK_REPLAY_OUTPUT"); dest != "" {
		data, _ := json.MarshalIndent(j, "", "  ")
		if writeErr := os.WriteFile(dest, data, 0600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != "succeeded" {
		t.Fatal("review did not complete")
	}
	if j.SelectedPlan != nil && !meetsScoreMinimum(j.Plans[*j.SelectedPlan]) {
		t.Fatal("selected target below the independently computed quality floor")
	}
	if os.Getenv("EASY_STOCK_OPTIMIZATION_REQUIRE_QUALIFIED") == "1" && j.SelectedPlan == nil {
		t.Fatal("one-invocation quality goal not achieved: " + j.OutcomeReason)
	}
	if j.RevisionCount > MaxRevisionRounds {
		t.Fatal("substantive revision limit exceeded")
	}
}

// Test-only provenance capture. Cached outputs are clearly identified, and their
// old token usage is not charged a second time.
type savedEvaluationGateway struct {
	*agent.Service
	files    []string
	dir      string
	sequence atomic.Int32
	log      func(string, ...any)
}

func (g *savedEvaluationGateway) PromptWithOptions(ctx context.Context, prompt string, opts agent.PromptOptions) (agent.PromptResult, error) {
	n := int(g.sequence.Add(1))
	replayed := n <= len(g.files)
	var result agent.PromptResult
	var err error
	if replayed {
		var data []byte
		data, err = os.ReadFile(g.files[n-1])
		var saved struct {
			Result agent.PromptResult `json:"result"`
		}
		if err == nil {
			err = json.Unmarshal(data, &saved)
		}
		result = saved.Result
		result.Usage = agent.TokenUsage{}
	} else {
		lastLog := time.Time{}
		originalProgress := opts.OnProgress
		opts.OnProgress = func(p agent.PromptProgress) {
			if originalProgress != nil {
				originalProgress(p)
			}
			if g.log != nil && (lastLog.IsZero() || time.Since(lastLog) >= 30*time.Second) {
				g.log("real model call=%d elapsed_ms=%d text=%d reasoning=%d", n, p.ElapsedMS, p.TextBytes, p.ReasoningBytes)
				lastLog = time.Now()
			}
		}
		result, err = g.Service.PromptWithOptions(ctx, prompt, opts)
	}
	if g.dir != "" {
		if e := os.MkdirAll(g.dir, 0700); e != nil {
			return result, e
		}
		if e := os.WriteFile(filepath.Join(g.dir, fmt.Sprintf("call-%02d-input.txt", n)), []byte(prompt), 0600); e != nil {
			return result, e
		}
		record := struct {
			Result   agent.PromptResult `json:"result"`
			Replayed bool               `json:"replayed"`
			Error    string             `json:"error,omitempty"`
		}{Result: result, Replayed: replayed}
		if err != nil {
			record.Error = err.Error()
		}
		data, e := json.MarshalIndent(record, "", "  ")
		if e != nil {
			return result, e
		}
		if e = os.WriteFile(filepath.Join(g.dir, fmt.Sprintf("call-%02d-output.json", n)), data, 0600); e != nil {
			return result, e
		}
	}
	return result, err
}

// Match the stock-relative scoring DTO, never account-relative trading weights.
func reviewTestWeights(holdings []pi.Holding) map[string]float64 {
	total := 0
	for _, h := range holdings {
		total += h.Weight
	}
	result := map[string]float64{}
	for _, h := range holdings {
		result[h.Symbol] = pi.EquityPercent(h.Weight, total)
	}
	return result
}
