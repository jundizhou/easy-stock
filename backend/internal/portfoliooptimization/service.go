package portfoliooptimization

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/foundation"
	pi "easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type CollectFunc func(context.Context, pi.Report, Request, time.Time) (Universe, error)
type Dependencies struct {
	Collect  CollectFunc
	Research pi.ResearchResolver
	Quotes   pi.QuoteRefresher
}
type Service struct {
	store     *pi.Store
	gateway   agent.Gateway
	deps      Dependencies
	mu        sync.Mutex
	active    map[string]context.CancelFunc
	wg        sync.WaitGroup
	closed    bool
	initError error
}

func NewService(store *pi.Store, gateway agent.Gateway, deps Dependencies) *Service {
	s := &Service{store: store, gateway: gateway, deps: deps, active: map[string]context.CancelFunc{}}
	if store == nil {
		s.initError = errors.New("持仓优化存储不可用")
		return s
	}
	s.initError = store.InitOptimizations(context.Background())
	if s.initError != nil {
		return s
	}
	jobs, err := store.RunningOptimizations(context.Background())
	if err != nil {
		s.initError = err
		return s
	}
	for i := range jobs {
		var j Job
		if err := json.Unmarshal(jobs[i], &j); err != nil {
			s.initError = err
			return s
		}
		if j.Status == "running" {
			settleInterruptedUsage(&j, time.Now().UTC())
			j.Status = "interrupted"
			j.ResumeAvailable = checkpointCanResume(j)
			j.Message = "服务曾重启，已保存研究和方案，可恢复缺失阶段"
			if !j.ResumeAvailable {
				j.Message = "服务重启后核对预算已耗尽，保留结果，可重新开始优化"
			}
			if err := s.save(&j); err != nil {
				s.initError = err
			}
		}
	}
	return s
}
func fingerprint(v any) string {
	data, _ := json.Marshal(v)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
func baselineHash(hs []pi.Holding) string {
	copyHoldings := append([]pi.Holding{}, hs...)
	sort.Slice(copyHoldings, func(i, j int) bool { return copyHoldings[i].Symbol < copyHoldings[j].Symbol })
	return fingerprint(copyHoldings)
}
func (s *Service) save(job *Job) error {
	now := time.Now().UTC()
	if !job.executionTick.IsZero() {
		job.ExecutionDurationMS += max(int64(0), now.Sub(job.executionTick).Milliseconds())
		job.executionTick = now
	}
	updateCheckpointProgress(job)
	job.UpdatedAt = now
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return s.store.SaveOptimization(context.Background(), job.ID, job.SourceID, job.Fingerprint, data, job.UpdatedAt.Format(time.RFC3339Nano))
}
func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	var job Job
	if s.initError != nil {
		return job, s.initError
	}
	raw, err := s.store.GetOptimization(ctx, id)
	if err != nil {
		return job, err
	}
	err = json.Unmarshal(raw, &job)
	return job, err
}
func (s *Service) List(ctx context.Context, source string) ([]Job, error) {
	raw, err := s.store.ListOptimizations(ctx, source)
	if err != nil {
		return nil, err
	}
	out := []Job{}
	for _, r := range raw {
		var j Job
		if err := json.Unmarshal(r, &j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}
func (s *Service) ready() error {
	if s.initError != nil {
		return s.initError
	}
	if s.gateway == nil || s.deps.Research == nil || s.deps.Quotes == nil || s.deps.Collect == nil {
		return errors.New("AI持仓优化服务未配置")
	}
	status := s.gateway.Status()
	if !status.Available || !status.Configured {
		return errors.New("请先在设置配置可用AI模型")
	}
	return nil
}
func normalizeCandidates(req Request) (Request, error) {
	if len(req.CandidateSymbols) > MaxCandidates {
		return req, errors.New("用户候选最多8只")
	}
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range req.CandidateSymbols {
		sym, err := foundation.NormalizeSymbol(raw)
		if err != nil {
			return req, err
		}
		if !seen[sym.Canonical] {
			seen[sym.Canonical] = true
			out = append(out, sym.Canonical)
		}
	}
	sort.Strings(out)
	req.CandidateSymbols = out
	return req, nil
}
func (s *Service) Start(ctx context.Context, sourceID string, req Request) (Job, error) {
	if err := s.ready(); err != nil {
		return Job{}, err
	}
	req, err := normalizeCandidates(req)
	if err != nil {
		return Job{}, err
	}
	source, err := s.store.Get(ctx, sourceID)
	if err != nil {
		return Job{}, err
	}
	if source.Status != "succeeded" || source.Report == nil || !source.Report.Conclusion.ScoreAvailable || len(source.Report.Conclusion.Dimensions) != 4 || source.Report.AlgorithmVersion != pi.AlgorithmVersion {
		return Job{}, errors.New("请先补齐或刷新持仓AI巡检，完整四维评分后再优化")
	}
	if req.RestartFrom != "" {
		previous, e := s.Get(ctx, req.RestartFrom)
		if e != nil {
			return Job{}, e
		}
		if previous.SourceID != sourceID || previous.Status == "running" || previous.ResumeAvailable || previous.Status == "succeeded" && previous.Outcome != "review_invalid" {
			return Job{}, errors.New("该任务仍可恢复或已有完整结果，无需重新开始")
		}
	}
	report := *source.Report
	report.Request = source.Request
	now := time.Now().UTC()
	if report.GeneratedAt.IsZero() || now.Sub(report.GeneratedAt) > 24*time.Hour {
		return Job{}, errors.New("原巡检已超过24小时，请先刷新巡检再优化")
	}
	for _, r := range report.Holdings {
		if !pi.ValidOptimizationResearch(r) || r.ReportCompletedAt.IsZero() || now.Sub(r.ReportCompletedAt) >= 24*time.Hour {
			return Job{}, errors.New("原个股研究已过期或未完成，请先刷新巡检")
		}
	}
	baseline := report.Request.Holdings
	root := sourceID
	if report.Request.SourceOptimizationID != "" {
		parent, err := s.Get(ctx, report.Request.SourceOptimizationID)
		if err != nil {
			return Job{}, fmt.Errorf("初始优化链不可用：%w", err)
		}
		if err := s.ValidateApplication(ctx, report.Request); err != nil {
			return Job{}, err
		}
		baseline = parent.Baseline
		root = parent.RootSourceID
	}
	if _, _, err := weights(baseline, false); err != nil {
		return Job{}, err
	}
	// Source generation/report IDs freeze a meaningful input fingerprint. A retry
	// uses resume; opening a completed result never triggers another analysis.
	fp := fingerprint(struct {
		Source   string
		Version  string
		Prompts  string
		Request  Request
		Baseline string
	}{sourceID, Version, ModelPromptVersion, req, baselineHash(baseline)})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Job{}, errors.New("优化服务正在关闭")
	}
	raw, err := s.store.FindOptimization(ctx, fp)
	if err == nil {
		var existing Job
		err = json.Unmarshal(raw, &existing)
		return existing, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Job{}, err
	}
	job := Job{ID: fmt.Sprintf("po-%d", now.UnixNano()), SourceID: sourceID, RootSourceID: root, Source: report, Baseline: baseline, BaselineFingerprint: baselineHash(baseline), Fingerprint: fp, Version: Version, Request: req, Status: "running", Stage: "preparing", Message: "冻结原组合、研究与变动口径，检查有限候选池", StartedAt: now, AsOf: now, Results: append([]pi.HoldingResult{}, report.Holdings...), Candidates: []Candidate{}, Eligibility: []Eligibility{}, Plans: []Plan{}, Limitations: []string{"建议仅为目标比例，不生成实际成交股数；入场、资金配对、T+1及费用须实际执行前核验"}}
	for i := range job.Results {
		job.Results[i].ResearchOrigin = "reused"
	}
	if err := s.save(&job); err != nil {
		return Job{}, err
	}
	s.launch(job)
	return job, nil
}
func (s *Service) launch(job Job) {
	raw, _ := json.Marshal(job)
	_ = json.Unmarshal(raw, &job)

	ctx, cancel := context.WithTimeout(context.Background(), remainingExecution(job))
	s.active[job.ID] = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.active, job.ID); s.mu.Unlock() }()
		job.executionTick = time.Now().UTC()
		s.run(ctx, job)
	}()
}
func (s *Service) Resume(ctx context.Context, id string) (Job, error) {
	if err := s.ready(); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Job{}, errors.New("优化服务正在关闭")
	}
	if _, ok := s.active[id]; ok {
		return s.Get(ctx, id)
	}
	job, err := s.Get(ctx, id)
	if err != nil {
		return job, err
	}
	if !job.ResumeAvailable {
		return job, errors.New("优化任务无需恢复")
	}
	// New validation rules, a new session, or aged research invalidate the quote
	// and proposal checkpoint. Successful stock research remains reusable.
	now := time.Now().UTC()
	if now.Sub(job.Source.GeneratedAt) > stockanalysis.ResearchReuseWindow {
		return job, errors.New("原巡检已过期，请先刷新巡检后再优化；已保存的优化研究仍保留")
	}
	for _, r := range job.Source.Holdings {
		if now.Sub(r.ReportCompletedAt) >= stockanalysis.ResearchReuseWindow {
			return job, errors.New("原持仓个股研究已过期，请先刷新巡检再优化")
		}
	}
	stale := job.Version != Version || (job.ModelPromptVersion != "" && job.ModelPromptVersion != ModelPromptVersion) || LatestSession(now) != job.LatestTradeDate
	job.Version = Version
	for _, candidate := range job.Candidates {
		if candidate.Selected && candidate.Screening != nil && candidate.Screening.CompletedSession != LatestCompletedSession(now) {
			stale = true
		}
	}
	for _, r := range job.Results {
		if now.Sub(r.ReportCompletedAt) >= stockanalysis.ResearchReuseWindow {
			stale = true
		}
	}
	if stale {
		job.InvestmentBaseline = nil
		job.SnapshotAt = time.Time{}
		job.Proposal = nil
		job.Plans = []Plan{}
		job.SelectedPlan = nil
		job.FallbackPlan = nil
		job.RevisionCount = 0
		job.RevisionHistory = nil
		job.Outcome = ""
		job.OutcomeReason = ""
		job.UnionFacts = nil
		job.Eligibility = nil
		job.AsOf = now
		job.ModelLoops = nil
		job.ProposalCheckpoint = nil
		job.CheckpointProgress = nil
		job.RangeRepairUsed = false
		job.ExecutionDurationMS = 0
		job.ModelStageDurationMS = map[string]int64{}
		job.ModelDurationMS = 0
		job.ModelStartedAt = time.Time{}
		job.Limitations = append(job.Limitations, "恢复时校验规则、交易日或研究时效变化，重新冻结资料并复评")
	}
	if !stale && !checkpointCanResume(job) {
		return job, errors.New("本次优化已达到时间或纠错上限，请重新开始；有效研究仍可复用")
	}
	job.ModelProgress = agent.PromptProgress{}
	job.ModelPromptBytes = 0
	// Older jobs marked a repair used before the size check, even when the
	// request was never sent. Restore the allowance only for that exact case.
	if strings.Contains(job.Error, "当前阶段输入") && strings.Contains(job.Error, "未发送模型") {
		stageCalls := 0
		for _, attempt := range job.ModelAttempts {
			if attempt.Stage == job.Stage {
				stageCalls++
			}
		}
		if stageCalls == 1 {
			limitations := []string{}
			for _, limitation := range job.Limitations {
				if limitation != "已使用一次模型格式/一致性修复" {
					limitations = append(limitations, limitation)
				}
			}
			job.Limitations = limitations
		}
	}
	job.Status = "running"
	job.ResumeAvailable = false
	job.Error = ""
	job.CompletedAt = time.Time{}
	job.Message = "恢复缺失阶段，已成功研究不重复执行"
	if err := s.save(&job); err != nil {
		return job, err
	}
	s.launch(job)
	return job, nil
}
func (s *Service) Cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancel, ok := s.active[id]
	if ok {
		cancel()
	}
	return ok
}
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	for _, cancel := range s.active {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *Service) ValidateApplication(ctx context.Context, req pi.Request) error {
	if req.SourceOptimizationID == "" {
		return nil
	}
	j, err := s.Get(ctx, req.SourceOptimizationID)
	if err != nil {
		return err
	}
	if j.Version != Version {
		return errors.New("该优化使用旧版交易资格校验，请重新优化后再使用目标方案")
	}
	if j.SelectedPlan == nil || *j.SelectedPlan < 0 || *j.SelectedPlan >= len(j.Plans) || j.Status != "succeeded" {
		return errors.New("优化没有可使用的目标方案")
	}
	p := j.Plans[*j.SelectedPlan]
	if !currentComparison(j, p) {
		return errors.New("该方案未通过当前独立复评与评分采纳要求，请重新优化后再使用目标方案；有效个股研究会继续复用")
	}
	if p.Status != "conditional" && p.Status != "accepted" {
		return errors.New("该方案未通过优化复评")
	}
	if req.TraderProfile != j.Source.Request.TraderProfile || req.Horizon != j.Source.Request.Horizon || !equalWeights(req.Holdings, p.Target) {
		return errors.New("新巡检须沿用优化目标、风格与周期；修改实际持仓请新建基准")
	}
	return nil
}
func equalWeights(a, b []pi.Holding) bool {
	m, _, err := weights(a, false)
	if err != nil {
		return false
	}
	n, _, err := weights(b, false)
	if err != nil || len(m) != len(n) {
		return false
	}
	for s, w := range m {
		if n[s] != w {
			return false
		}
	}
	return true
}
func (s *Service) run(ctx context.Context, job Job) {
	err := s.execute(ctx, &job)
	if err != nil {
		job.Status = "incomplete"
		job.Outcome = "incomplete"
		job.ResumeAvailable = checkpointCanResume(job)
		job.Error = err.Error()
		job.Message = "优化未完成，已保存研究与检查点，可恢复"
		if !job.ResumeAvailable {
			job.Message = "本次优化已达到时间或纠错上限，已保留结果，可重新开始"
		}
		if errors.Is(err, context.Canceled) {
			job.Status = "cancelled"
			job.Message = "已停止优化，共享个股研究继续独立运行"
		}
		job.CompletedAt = time.Now().UTC()
		_ = s.save(&job)
	}
}
func (s *Service) execute(ctx context.Context, job *Job) error {
	ctx, release, err := agent.BindTask(ctx, s.gateway)
	if err != nil {
		return err
	}
	defer release()
	job.Model = agent.BoundModel(ctx)
	if job.SnapshotAt.IsZero() {
		if err := s.prepare(ctx, job); err != nil {
			return err
		}
	}
	err = s.executeFrozen(ctx, job)
	if err != nil && !errors.Is(err, context.Canceled) && finishFallbackAfterError(job, err) {
		return s.save(job)
	}
	return err
}

// A substantive revision shares the original task binding, deadline, research
// and baseline. It cannot launch a new screening or reset the repair allowance.
func (s *Service) executeFrozen(ctx context.Context, job *Job) error {
	if job.Proposal == nil {
		job.Stage = "proposing"
		job.Message = "比较个股投资价值，程序搜索盈利、风险和结构更合理的整数配仓"
		if job.RevisionCount > 0 {
			job.Message = "候选配仓尚未通过全部采纳检查，按具体原因调整投资范围后重新求解（复用全部研究）"
		}
		if err := s.save(job); err != nil {
			return err
		}
		var p Proposal
		var frozenWeights *Proposal
		var frozenReferences *Proposal
		var frozenRanges *Proposal
		var frozenParts *proposalPartsError
		prompt, err := proposalPrompt(*job)
		if err != nil {
			return err
		}
		if err := s.model(ctx, job, prompt, func(content string) error {
			if job.RevisionCount > 0 && job.InvestmentBaseline != nil {
				var err error
				p, err = refineAllocationRanges(ctx, *job, content)
				return err
			}
			if frozenRanges != nil {
				var err error
				p, err = repairInitialRanges(ctx, *job, *frozenRanges, content)
				return err
			}
			if frozenParts != nil {
				var err error
				p, err = repairProposalParts(ctx, *job, frozenParts, content)
				var pending *proposalPartsError
				if errors.As(err, &pending) {
					frozenParts = pending
				}
				var rangeError *programRangeRepairError
				if errors.As(err, &rangeError) {
					copy := p
					frozenRanges = &copy
				}
				return err
			}
			if frozenReferences != nil {
				var err error
				p, err = repairComparisonReferences(*job, *frozenReferences, content)
				return err
			}
			if frozenWeights != nil {
				var patch struct {
					Weights map[string]int `json:"preferred_weights"`
				}
				if err := jsonContent(content, &patch); err != nil {
					return err
				}
				var err error
				p, err = patchPreferredWeights(*job, *frozenWeights, patch.Weights)
				if err == nil {
					corrections := []string{}
					for _, a := range p.Alternatives[0].Allocations {
						if patch.Weights[a.Symbol] != a.Preferred {
							corrections = append(corrections, fmt.Sprintf("%s %d%%→%d%%", a.Symbol, patch.Weights[a.Symbol], a.Preferred))
						}
					}
					if len(corrections) > 0 {
						job.Limitations = append(job.Limitations, "完整权重补丁按原范围、交易权限及预算仅搬移1个百分点："+strings.Join(corrections, "；")+"；投资结论不变")
					}
				}
				return err
			}
			p = Proposal{}
			content = trimJSONFence(content)
			content, normalized := normalizeEarlyClosedAllocations(content)
			if normalized {
				job.Limitations = append(job.Limitations, "模型股票列表提前闭合，程序仅校正嵌套位置；原始内容、数值及投资结论不变")
			}
			if corrected, extraBrace := normalizeExtraAllocationBraces(content); extraBrace {
				content = corrected
				job.Limitations = append(job.Limitations, "模型股票行引用结尾多出闭合符，程序仅移除可确定的多余符号；字段、数值及投资结论不变")
			}
			if expanded, compact := normalizeCompactProposal(content); compact {
				content = expanded
				job.Limitations = append(job.Limitations, "模型修复沿用已声明列格式，程序无损恢复股票与投资命名对象；缺少字段不补猜")
			}
			var decodeErr error
			p, decodeErr = decodeInitialProposal(ctx, *job, content)
			if decodeErr == nil {
				decodeErr = validateProposal(*job, p)
			}
			if err := decodeErr; err != nil {
				var partsError *proposalPartsError
				if errors.As(err, &partsError) {
					frozenParts = partsError
				}
				var rangeError *programRangeRepairError
				if errors.As(err, &rangeError) {
					copy := p
					frozenRanges = &copy
				}
				var evidenceError *comparisonEvidenceError
				if errors.As(err, &evidenceError) {
					copy := p
					frozenReferences = &copy
					evidenceError.proposal = &copy
					for _, c := range p.InvestmentComparisons {
						var missing *comparisonEvidenceError
						if errors.As(validateInvestmentComparison(*job, c), &missing) {
							evidenceError.needed = append(evidenceError.needed, c)
						}
					}
				}
				var weightError *preferredWeightTotalError
				if errors.As(err, &weightError) {
					copy := p
					frozenWeights = &copy
				}
				return err
			}
			return nil
		}); err != nil {
			return err
		}
		for i := range p.RiskGroups {
			p.RiskGroups[i].Weight = 0
		}

		job.Proposal = &p
		job.ProposalCheckpoint = &ProposalCheckpoint{Draft: p}
		job.Plans = []Plan{}
		for _, alt := range p.Alternatives {
			solutions, err := SearchAllocations(ctx, *job, alt)
			if err != nil {
				job.Plans = append(job.Plans, Plan{Name: alt.Name, Allocations: alt.Allocations, Status: "rejected", Error: err.Error()})
				continue
			}
			for _, solution := range solutions {
				i := len(job.Plans)
				plan := Plan{Name: alt.Name, Allocations: alt.Allocations, Status: "rejected", AssessmentOrder: "original_first", Target: solution.Target, Search: &solution.Search}
				if len(solutions) > 1 {
					plan.Name += " · " + solution.Search.Method
				}
				if (int(job.Fingerprint[0])+i)%2 == 0 {
					plan.AssessmentOrder = "target_first"
				}
				plan.Funding, plan.TradeSold, plan.TradeBought = FundingFlowsCompared(job.Source.Request.Holdings, plan.Target, p.InvestmentComparisons)
				plan.Checks = Check(job.Baseline, plan.Target)
				plan.Improvements = measureImprovements(*job, plan.Target)
				req := job.Source.Request
				plan.Original = pi.OptimizationReport(req, job.Results)
				req.Holdings = plan.Target
				plan.Proposed = pi.OptimizationReport(req, job.Results)
				if equalWeights(job.Source.Request.Holdings, plan.Target) {
					plan.Status = "unchanged"
					plan.Error = "未生成仓位变化，独立复评未执行"
				} else if len(plan.Improvements) == 0 {
					plan.Error = "只有配置变化，没有具体投资比较或可复算的结构改善"
				} else {
					plan.Status = "pending_review"
				}
				job.Plans = append(job.Plans, plan)
			}
		}
		if err := s.save(job); err != nil {
			return err
		}
	}
	selectReviewPlan(job)
	for i := range job.Plans {
		plan := &job.Plans[i]
		if plan.Status != "pending_review" {
			continue
		}
		tooSimilar := false
		for _, previous := range job.Plans[:i] {
			if previous.Assessment != nil && !materiallyDifferent(previous.Target, plan.Target) {
				tooSimilar = true
			}
		}
		for _, previous := range job.RevisionHistory {
			if !materiallyDifferent(previous.Target, plan.Target) {
				tooSimilar = true
			}
		}
		if tooSimilar {
			plan.Status = "not_reviewed"
			plan.Error = "与已复评组合仅微小调权，不重复取分；继续寻找有实质变化的配置"
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		job.Stage = "assessing"
		job.Message = "程序已选定本轮配置，正在独立复评"
		if job.RevisionCount > 0 {
			job.Message = "改进配仓已生成，正在独立复评；优先寻找70分以上的合理组合"
		}
		if err := s.save(job); err != nil {
			return err
		}
		prompt, err := pairedPrompt(*job, *plan)
		if err != nil {
			return err
		}
		plan.Original.Conclusion.RiskGroups = frozenGroups(job.Proposal.RiskGroups, plan.Original.Request.Holdings)
		plan.Proposed.Conclusion.RiskGroups = frozenGroups(job.Proposal.RiskGroups, plan.Proposed.Request.Holdings)
		var a, b pi.AIReport
		var review Assessment
		checkpoint := &ReviewCheckpoint{}
		plan.ReviewCheckpoint = checkpoint
		err = s.model(ctx, job, prompt, func(content string) error {
			var err error
			a, b, review, err = checkpoint.validate(*job, *plan, content)
			return err
		})
		if err != nil {
			var invalid *modelValidationError
			if errors.As(err, &invalid) {
				plan.Status = "invalid_review"
				plan.Error = "独立复评未通过一致性检查，未采用评分：" + invalid.Error()
				job.Limitations = append(job.Limitations, plan.Name+"复评无效，保留已通过的部分；本轮不尝试其他配置取分")
				if saveErr := s.save(job); saveErr != nil {
					return saveErr
				}
				continue
			}
			return err
		}
		if plan.AssessmentOrder == "target_first" {
			a, b = b, a
		}
		plan.Original.Model = job.Model
		plan.Proposed.Model = job.Model
		plan.Original.GeneratedAt = job.SnapshotAt
		plan.Proposed.GeneratedAt = job.SnapshotAt
		plan.Original.Conclusion = a
		plan.Proposed.Conclusion = b
		plan.Original.Conclusion.RiskGroups = frozenGroups(job.Proposal.RiskGroups, job.Source.Request.Holdings)
		plan.Proposed.Conclusion.RiskGroups = frozenGroups(job.Proposal.RiskGroups, plan.Target)

		if plan.Proposed.Metrics.MaxSinglePercent > plan.Proposed.Profile.MaxSinglePercent {
			review.ResidualRisks = append(review.ResidualRisks, fmt.Sprintf("目标最大单票仍为%d%%，超过风格参考%d%%，仅为局部改善", plan.Proposed.Metrics.MaxSinglePercent, plan.Proposed.Profile.MaxSinglePercent))
		}
		plan.Assessment = &review
		classifyReviewedPlan(*job, plan)
		if plan.Status == "qualified_alternative" {
			rememberFallback(job, *plan)
		}
		if err := s.save(job); err != nil {
			return err
		}
		if plan.Status == "conditional" {
			for k := i + 1; k < len(job.Plans); k++ {
				if job.Plans[k].Status == "pending_review" {
					job.Plans[k].Status = "not_reviewed"
					job.Plans[k].Error = "已找到独立复评达标的可采纳组合，停止其他方案复评"
				}
			}
			break
		}
	}
	selected := -1
	job.SelectedPlan = nil
	for i, p := range job.Plans {
		if (p.Status == "conditional" || p.Status == "accepted") && currentComparison(*job, p) {
			if selected < 0 || p.Checks.Sold < job.Plans[selected].Checks.Sold {
				selected = i
			}
		}
	}
	if selected < 0 && shouldRevise(*job) {
		if err := ctx.Err(); err != nil {
			return err
		}
		archiveRejectedRound(job)
		job.RevisionCount++
		job.Proposal = nil
		job.ProposalCheckpoint = nil
		job.Plans = []Plan{}
		job.Outcome, job.OutcomeReason = "", ""
		job.Stage = "proposing"
		job.Message = "方案尚未通过全部采纳检查，按具体原因调整投资范围并再次求解，不重做个股研究"
		if err := s.save(job); err != nil {
			return err
		}
		return s.executeFrozen(ctx, job)
	}
	if selected < 0 && job.FallbackPlan != nil && currentComparison(*job, *job.FallbackPlan) {
		// All allowed searches finished. Preserve the original score and review
		// when selecting the best valid alternative from either round.
		fallback := *job.FallbackPlan
		selected = len(job.Plans)
		for i, p := range job.Plans {
			if equalWeights(p.Target, fallback.Target) {
				selected = i
				break
			}
		}
		if selected == len(job.Plans) {
			job.Plans = append(job.Plans, fallback)
		} else {
			job.Plans[selected] = fallback
		}
	}
	job.Outcome = "unchanged"
	job.OutcomeReason = ""
	if selected >= 0 {
		job.SelectedPlan = &selected
		job.Outcome = job.Plans[selected].Status
		job.OutcomeReason = job.Plans[selected].Assessment.Reason
		if !meetsScoreTarget(job.Plans[selected]) {
			job.OutcomeReason = fallbackOutcomeReason(job.Plans[selected])
		}
	} else if len(job.Plans) > 0 {
		job.Outcome = "no_feasible_plan"
		for _, p := range job.Plans {
			if p.Status == "unchanged" {
				job.Outcome = "unchanged"
				break
			}
		}
	}
	if job.OutcomeReason == "" {
		for _, plan := range job.Plans {
			if plan.Error != "" {
				job.OutcomeReason += plan.Name + "：" + plan.Error + "；"
			}
		}
	}
	if job.OutcomeReason == "" {
		for _, round := range job.RevisionHistory {
			if round.Error != "" {
				job.OutcomeReason += fmt.Sprintf("第%d轮%s：%s；", round.Round, round.Name, round.Error)
			}
		}
	}
	if job.OutcomeReason == "" {
		job.OutcomeReason = job.Proposal.KeepReason
	} else if selected < 0 && job.Proposal.KeepReason != "" {
		job.OutcomeReason += "后续配仓说明：" + job.Proposal.KeepReason
	}
	if selected < 0 {
		invalidReviews, validReviews := 0, 0
		for _, p := range job.Plans {
			if p.Status == "invalid_review" {
				invalidReviews++
			}
			if p.Assessment != nil {
				validReviews++
			}
		}
		if invalidReviews > 0 && validReviews == 0 && len(job.RevisionHistory) == 0 {
			job.Outcome = "review_invalid"
			job.OutcomeReason = "可行配仓已生成，但独立复评均未通过一致性检查，不能给出可信评分或采纳结论；原持仓未调整。" + job.OutcomeReason
		}
		if score, ok := bestBelowTarget(*job); ok && !hasCurrentQualifiedScore(*job) {
			job.Outcome = "below_target_score"
			job.OutcomeReason = fmt.Sprintf("未达标：独立复评已验证的最高目标评分为%d分。70分为优化目标；65–69分须提升至少5分且四维均不低于50分。已按具体缺陷改进%d次，仍未形成同时通过独立偏好、风险与资金约束的方案，保留原持仓。", score, job.RevisionCount)
			if job.Proposal.KeepReason != "" {
				job.OutcomeReason += job.Proposal.KeepReason + "；"
			}
			for _, p := range job.Plans {
				if p.Error != "" {
					job.OutcomeReason += p.Error + "；"
				}
			}
		}
		receivers := 0
		blocked := []string{}
		elig := map[string]Eligibility{}
		for _, e := range job.Eligibility {
			elig[e.Symbol] = e
		}
		for _, r := range job.Results {
			reason := ""
			switch {
			case elig[r.Holding.Symbol].Locked || !elig[r.Holding.Symbol].CanIncrease:
				reason = elig[r.Holding.Symbol].Reason
			case !pi.ValidOptimizationResearch(r):
				reason = "个股研究未完成或数据结构无效"
			default:
				receivers++
			}
			if reason != "" {
				blocked = append(blocked, holdingName(r.Holding)+"："+shortText(reason, 180))
			}
		}
		if receivers == 0 {
			job.Outcome = "no_feasible_plan"
			job.OutcomeReason = "固定总仓位下没有通过交易资格与数据检查的资金接收标的，未生成可行调整方案；原持仓暂未调整，不代表通过优化推荐。" + strings.Join(blocked, "；")
		}
	}
	job.Status = "succeeded"
	job.FallbackPlan = nil
	job.Stage = "completed"
	job.Message = "持仓优化评估完成，原报告与持仓保持可追溯"
	job.ResumeAvailable = false
	job.CompletedAt = time.Now().UTC()
	return s.save(job)
}
func (s *Service) prepare(ctx context.Context, job *Job) error {
	job.Needs = PortfolioNeeds(job.Source)
	job.Stage = "screening"
	job.Message = "纯代码最多检查80只、3分钟，保留8只合格候选，最多6只不同行业进入AI"
	if err := s.save(job); err != nil {
		return err
	}
	dataCtx, cancel := context.WithTimeout(ctx, ScreeningTimeout+15*time.Second)
	u, err := s.deps.Collect(dataCtx, job.Source, job.Request, job.AsOf)
	cancel()
	if err != nil {
		return err
	}
	if len(u.Candidates) > MaxCandidates {
		u.Candidates = u.Candidates[:MaxCandidates]
	}
	job.Candidates = u.Candidates
	job.ScreeningAudit = u.ScreeningAudit
	job.Limitations = append(job.Limitations, u.Limitations...)
	catalog := map[string]foundation.StockCatalogEntry{}
	for _, c := range u.Catalog {
		symbol, err := foundation.NormalizeSymbol(c.Symbol)
		if err == nil {
			catalog[symbol.Canonical] = c
		}
	}
	all := []string{}
	seen := map[string]bool{}
	for _, h := range job.Source.Request.Holdings {
		seen[h.Symbol] = true
		all = append(all, h.Symbol)
	}
	for _, c := range job.Candidates {
		if !seen[c.Symbol] {
			seen[c.Symbol] = true
			all = append(all, c.Symbol)
		}
	}
	quotes := u.Quotes
	if len(quotes) == 0 {
		quoteCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		quotes, err = s.deps.Quotes(quoteCtx, all)
		cancel()
		if err != nil {
			return err
		}
	}
	qm := map[string]foundation.Quote{}
	for _, q := range quotes {
		sym, err := foundation.NormalizeSymbol(q.Symbol)
		if err == nil {
			q.Symbol = sym.Canonical
			qm[q.Symbol] = q
		}
	}
	job.Eligibility = []Eligibility{}
	for _, symbol := range all {
		q := qm[symbol]
		q.Symbol = symbol
		job.Eligibility = append(job.Eligibility, TradingEligibility(catalog[symbol], q, time.Now()))
	}
	elig := map[string]Eligibility{}
	for _, e := range job.Eligibility {
		elig[e.Symbol] = e
	}
	// Only code-qualified candidates can enter AI research. The collector ranks
	// all eight by industry startup and disclosed growth; no AI is called here.
	eligibleCandidates := []Candidate{}
	for i := range job.Candidates {
		c := &job.Candidates[i]
		c.Selected = false
		CandidateIndustry(c, catalog[c.Symbol].Industry)
		if seenHolding(job.Source.Request.Holdings, c.Symbol) {
			c.Reason = "已在原组合中"
			continue
		}
		if c.Screening == nil || !c.Screening.Qualified {
			continue
		}
		if !elig[c.Symbol].CanIncrease {
			c.Reason = elig[c.Symbol].Reason
			continue
		}
		if c.IndustryGroup == "" || c.CatalogIndustryGroup == "" {
			c.Reason += "；行业未知，不能核验行业分散，未进入AI研究"
			continue
		}
		eligibleCandidates = append(eligibleCandidates, *c)
	}
	selected := map[string]bool{}
	for _, i := range DiverseCandidates(eligibleCandidates, MaxCandidateResearch) {
		selected[eligibleCandidates[i].Symbol] = true
	}
	chosen := len(selected)
	for i := range job.Candidates {
		c := &job.Candidates[i]
		if selected[c.Symbol] {
			c.Selected = true
			c.Reason += "；通过行业分散进入候选AI研究，最终由AI决定是否纳入组合"
		} else if c.Screening != nil && c.Screening.Qualified {
			c.Reason += "；行业重复、交易核验未通过或排序未入前六，保留为代码筛查记录"
		}
	}
	if chosen < MaxCandidateResearch {
		job.Limitations = append(job.Limitations, fmt.Sprintf("在代码检查预算内，按交易资格及行业分散后仅%d只候选，未凑足6只；不补同一行业、不降低门槛", chosen))
	}
	// Only the current bounded selection enters this research union. Reports
	// excluded after a new-session resume remain in the stock research library.
	activeCandidates := map[string]bool{}
	for _, c := range job.Candidates {
		if c.Selected {
			activeCandidates[c.Symbol] = true
		}
	}
	retainedResults := []pi.HoldingResult{}
	for _, r := range job.Results {
		if seenHolding(job.Source.Request.Holdings, r.Holding.Symbol) || activeCandidates[r.Holding.Symbol] {
			retainedResults = append(retainedResults, r)
		}
	}
	job.Results = retainedResults
	request := job.Source.Request
	request.ResearchLevel = stockanalysis.ResearchLevelStandard
	for _, c := range job.Candidates {
		if c.Selected && !hasResult(job.Results, c.Symbol) {
			job.Results = append(job.Results, pi.HoldingResult{Holding: pi.Holding{Symbol: c.Symbol, Name: c.Name}, Status: "queued"})
		}
	}
	// At most two candidate studies wait concurrently. Mutations/checkpoint
	// serialization happen under one lock; each stock still uses the shared library.
	var researchMu sync.Mutex
	var researchWG sync.WaitGroup
	researchSem := make(chan struct{}, 2)
	researchErrors := make(chan error, len(job.Results))
	researchResults := append([]pi.HoldingResult(nil), job.Results...)
	for i, r := range researchResults {
		if pi.ValidOptimizationResearch(r) && job.AsOf.Sub(r.ReportCompletedAt) < 24*time.Hour && !stockanalysis.NeedsResearchRevalidation(r.Analysis.ResearchReport) {
			continue
		}
		researchWG.Add(1)
		go func(i int, r pi.HoldingResult) {
			defer researchWG.Done()
			select {
			case researchSem <- struct{}{}:
			case <-ctx.Done():
				researchErrors <- ctx.Err()
				return
			}
			defer func() { <-researchSem }()
			researchMu.Lock()
			job.Stage = "researching"
			job.Message = fmt.Sprintf("补齐%s的AI研究，候选最多6只、同时最多2只", r.Holding.Symbol)
			saveErr := s.save(job)
			researchMu.Unlock()
			if saveErr != nil {
				researchErrors <- saveErr
				return
			}
			result, err := s.deps.Research(ctx, r.Holding, request, job.AsOf, false, r.AnalysisID, func(progress pi.HoldingResult) {
				researchMu.Lock()
				defer researchMu.Unlock()
				progress.Holding = r.Holding
				job.Results[i] = progress
				_ = s.save(job)
			})
			researchMu.Lock()
			defer researchMu.Unlock()
			result.Holding = r.Holding
			if err != nil {
				result.Status = "failed"
				result.Error = err.Error()
				job.Results[i] = result
				if saveErr := s.save(job); saveErr != nil {
					researchErrors <- saveErr
					return
				}
				if seenHolding(job.Source.Request.Holdings, r.Holding.Symbol) || ctx.Err() != nil {
					researchErrors <- err
					return
				}
				job.Limitations = append(job.Limitations, r.Holding.Symbol+"候选研究失败，已排除")
				for n := range job.Candidates {
					if job.Candidates[n].Symbol == r.Holding.Symbol {
						job.Candidates[n].Reason = "候选研究失败，已排除：" + err.Error()
					}
				}
				return
			}
			job.Results[i] = result
			if saveErr := s.save(job); saveErr != nil {
				researchErrors <- saveErr
			}
		}(i, r)
	}
	researchWG.Wait()
	close(researchErrors)
	for err := range researchErrors {
		if err != nil {
			return err
		}
	}
	successful := []pi.HoldingResult{}
	job.NewStocks = 0
	job.ReusedStocks = 0
	for _, r := range job.Results {
		if !pi.ValidOptimizationResearch(r) {
			continue
		}
		successful = append(successful, r)
		if r.ResearchOrigin == "new" {
			job.NewStocks++
		} else {
			job.ReusedStocks++
		}
	}
	job.Results = successful
	symbols := []string{}
	for _, r := range job.Results {
		symbols = append(symbols, r.Holding.Symbol)
	}
	// Freeze one final quote snapshot for both comparisons after research completes.
	quoteCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	quotes, err = s.deps.Quotes(quoteCtx, symbols)
	cancel()
	if err != nil {
		return err
	}
	qm = map[string]foundation.Quote{}
	for _, q := range quotes {
		sym, err := foundation.NormalizeSymbol(q.Symbol)
		if err == nil {
			q.Symbol = sym.Canonical
			qm[q.Symbol] = q
		}
	}
	job.Eligibility = []Eligibility{}
	for i := range job.Results {
		r := &job.Results[i]
		q := qm[r.Holding.Symbol]
		if !QuoteCurrent(q, time.Now()) {
			return fmt.Errorf("%s最近有效交易日行情不可用，不能确认优化", r.Holding.Symbol)
		}
		r.CurrentQuote = &q
		r.QuoteStatus = "refreshed"
		r.QuoteMessage = "本次优化统一行情快照"
		job.Eligibility = append(job.Eligibility, TradingEligibility(catalog[r.Holding.Symbol], q, time.Now()))
	}
	job.SnapshotAt = time.Now().UTC()
	job.LatestTradeDate = LatestSession(job.SnapshotAt)
	job.UnionFacts = pi.OptimizationUnionReport(job.Source.Request, job.Results).Facts
	return s.save(job)
}
func hasResult(rs []pi.HoldingResult, s string) bool {
	for _, r := range rs {
		if r.Holding.Symbol == s {
			return true
		}
	}
	return false
}
func seenHolding(hs []pi.Holding, s string) bool {
	for _, h := range hs {
		if h.Symbol == s {
			return true
		}
	}
	return false
}

// ApplyRequest preserves the optimization chain and clears cost for new or
// increased allocations; recording a blended cost requires actual user input.
func ApplyRequest(j Job) (pi.Request, error) {
	if j.Version != Version {
		return pi.Request{}, errors.New("该优化使用旧版交易资格校验，请重新优化后再使用目标方案")
	}
	if j.Status != "succeeded" || j.SelectedPlan == nil || *j.SelectedPlan < 0 || *j.SelectedPlan >= len(j.Plans) {
		return pi.Request{}, errors.New("没有合格目标方案")
	}
	if !currentComparison(j, j.Plans[*j.SelectedPlan]) {
		return pi.Request{}, errors.New("该方案未通过当前独立复评与评分采纳要求，请重新优化后再使用目标方案")
	}
	req := j.Source.Request
	req.Holdings = append([]pi.Holding{}, j.Plans[*j.SelectedPlan].Target...)
	req.SourceOptimizationID = j.ID
	req.ForceSymbols = nil
	return req, nil
}

func currentComparison(j Job, p Plan) bool {
	if j.ModelPromptVersion != ModelPromptVersion || (p.Status != "conditional" && p.Status != "accepted") {
		return false
	}
	return len(acceptanceReasons(j, p)) == 0
}

func holdingName(h pi.Holding) string {
	if h.Name != "" {
		return h.Name
	}
	return h.Symbol
}
