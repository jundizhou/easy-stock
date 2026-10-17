package stockanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"easy-stock/backend/internal/agent"
)

var ErrResearchBusy = errors.New("研究任务已达并发上限，请稍后再试")

type ResearchPublisher func(string, string, *Analysis, *ResearchSnapshot) error
type ResearchRunner func(context.Context, ResearchRequest, ResearchPublisher) (Analysis, *ResearchSnapshot, error)
type ResearchPreparation func(context.Context, ResearchRequest) (context.Context, ResearchBudget, func(), error)
type activeResearch struct {
	id     string
	symbol string
	cancel context.CancelFunc
}

type ResearchService struct {
	store               *ResearchStore
	run                 ResearchRunner
	prepare             ResearchPreparation
	mu                  sync.Mutex
	active              map[string]activeResearch
	workers             chan struct{}
	wg                  sync.WaitGroup
	closed              bool
	initializationError error
	onComplete          func(ResearchJob)
}

func NewResearchService(store *ResearchStore, run ResearchRunner, onComplete ...func(ResearchJob)) *ResearchService {
	return NewResearchServiceWithPreparation(store, run, nil, onComplete...)
}

func NewResearchServiceWithPreparation(store *ResearchStore, run ResearchRunner, prepare ResearchPreparation, onComplete ...func(ResearchJob)) *ResearchService {
	s := &ResearchService{store: store, run: run, prepare: prepare, active: map[string]activeResearch{}, workers: make(chan struct{}, 2)}
	if len(onComplete) > 0 {
		s.onComplete = onComplete[0]
	}
	if store != nil {
		s.initializationError = store.MarkInterrupted(context.Background())
	}
	return s
}

func (s *ResearchService) Start(ctx context.Context, request ResearchRequest) (ResearchJob, error) {
	return s.start(ctx, request, nil)
}

func (s *ResearchService) Resume(ctx context.Context, id string) (ResearchJob, error) {
	if s == nil || s.store == nil {
		return ResearchJob{}, fmt.Errorf("研究任务服务不可用")
	}
	job, err := s.store.Get(ctx, id)
	if err != nil {
		return ResearchJob{}, err
	}
	if !job.Public().ResumeAvailable || job.Analysis == nil {
		return ResearchJob{}, fmt.Errorf("该研究没有可继续的阶段，请重新分析")
	}
	return s.start(ctx, job.Request, &job)
}

func (s *ResearchService) start(ctx context.Context, request ResearchRequest, resume *ResearchJob) (ResearchJob, error) {
	request, err := NormalizeResearchRequest(request)
	if err != nil {
		return ResearchJob{}, err
	}
	if s == nil || s.store == nil || s.run == nil {
		return ResearchJob{}, fmt.Errorf("研究任务服务不可用")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked(ctx, request, resume)
}

func (s *ResearchService) startLocked(ctx context.Context, request ResearchRequest, resume *ResearchJob) (ResearchJob, error) {
	keyBytes, _ := json.Marshal(request)
	key := string(keyBytes)
	if s.initializationError != nil {
		return ResearchJob{}, s.initializationError
	}
	if s.closed {
		return ResearchJob{}, fmt.Errorf("研究服务正在关闭")
	}
	if active, ok := s.active[key]; ok {
		job, err := s.store.Get(ctx, active.id)
		if err != nil || job.Status == "queued" || job.Status == "running" {
			return job, err
		}
		delete(s.active, key)
	}
	if len(s.active) >= 8 {
		return ResearchJob{}, ErrResearchBusy
	}
	now := time.Now().UTC()
	job := ResearchJob{ID: NewResearchID(), Request: request, Status: "queued", Stage: "queued", Message: "研究已提交", StartedAt: now, UpdatedAt: now}
	if resume != nil {
		job.Analysis, job.Snapshot, job.Checkpoint = resume.Analysis, resume.Snapshot, resume.Checkpoint
		job.ResumedFrom = resume.ID
		job.Message = "继续已保存的研究阶段"
	}
	runCtx, cancel := context.WithCancel(context.Background())
	trace := agent.CallTraceFromContext(ctx)
	if trace.RootJobID == "" {
		trace.RootJobID = job.ID
	}
	trace.SharedTaskID = job.ID
	trace.ExecutionID = fmt.Sprintf("%s-%d", job.ID, time.Now().UnixNano())
	runCtx = agent.WithCallTrace(runCtx, trace)
	budget := ResearchBudgetFor(request, 0, "")
	release := func() {}
	if s.prepare != nil {
		var err error
		runCtx, budget, release, err = s.prepare(runCtx, request)
		if err != nil {
			cancel()
			if release != nil {
				release()
			}
			return job, err
		}
	}
	job.Budget = &budget
	if err := s.store.Save(ctx, job); err != nil {
		cancel()
		release()
		return job, err
	}
	s.active[key] = activeResearch{id: job.ID, symbol: request.Symbol, cancel: cancel}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer release()
		s.execute(runCtx, key, job)
	}()
	return job, nil
}

func (s *ResearchService) execute(ctx context.Context, key string, job ResearchJob) {
	defer func() {
		s.mu.Lock()
		if item, ok := s.active[key]; ok && item.id == job.ID {
			item.cancel()
			delete(s.active, key)
		}
		s.mu.Unlock()
	}()
	execution := &researchExecution{checkpoint: job.Checkpoint}
	if job.ResumedFrom != "" {
		previous := job
		execution.resume = &previous
	}
	execution.save = func(cp *ResearchCheckpoint) error {
		job.Checkpoint = cp
		job.UpdatedAt = time.Now().UTC()
		// A cancellation must not discard the last completed model stage.
		saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.store.Save(saveCtx, job)
	}
	ctx = context.WithValue(ctx, researchExecutionKey{}, execution)
	publish := func(stage, message string, analysis *Analysis, snapshot *ResearchSnapshot) error {
		job.Stage = stage
		job.Message = message
		job.Status = "running"
		job.UpdatedAt = time.Now().UTC()
		if analysis != nil {
			analysis.AnalysisID = job.ID
			job.Analysis = analysis
		}
		if snapshot != nil {
			job.Snapshot = snapshot
		}
		return s.store.Save(ctx, job)
	}
	var analysis Analysis
	var snapshot *ResearchSnapshot
	var err error
	select {
	case s.workers <- struct{}{}:
		// Queued work retains cancellation and its frozen model, but starts
		// spending the execution budget only after acquiring a worker.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(WithResearchBudget(ctx, *job.Budget), job.Budget.TotalTimeout())
		defer cancel()
		func() {
			defer func() { <-s.workers }()
			defer func() {
				if recover() != nil {
					err = errors.New("研究任务意外中断，已保留已采集资料")
				}
			}()
			if ctx.Err() != nil {
				err = ctx.Err()
				return
			}
			analysis, snapshot, err = s.run(ctx, job.Request, publish)
		}()
	case <-ctx.Done():
		err = ctx.Err()
	}
	if analysis.Symbol != "" {
		analysis.AnalysisID = job.ID
		job.Analysis = &analysis
	}
	if snapshot != nil {
		job.Snapshot = snapshot
	}
	now := time.Now().UTC()
	job.UpdatedAt = now
	job.CompletedAt = &now
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		job.Status = "failed"
		job.Stage = "failed"
		job.Error = fmt.Sprintf("研究超过%s执行时限", formatResearchDuration(job.Budget.TotalTimeout()))
		job.Message = "研究超时，保留已完成的数据"
	} else if ctx.Err() != nil {
		job.Status = "cancelled"
		job.Stage = "cancelled"
		job.Message = "研究已停止，保留已完成的数据"
	} else if err != nil {
		job.Status = "failed"
		job.Stage = "failed"
		job.Error = err.Error()
		job.Message = "研究未完成，已保存可用数据"
	} else if job.Request.AnalysisLevel == ResearchLevelQuantitative && analysis.Symbol != "" {
		job.Status = "succeeded"
		job.Stage = "completed"
		job.Message = "量化速览完成，未调用AI"
	} else if analysis.AI.Status != "ready" {
		job.Status = "degraded"
		job.Stage = "completed"
		job.Message = analysis.AI.Message
	} else {
		job.Status = "succeeded"
		job.Stage = "completed"
		job.Message = "研究完成"
	}
	saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if saveErr := s.store.Save(saveCtx, job); saveErr != nil {
		// A persisted running state is marked interrupted at the next startup.
		s.mu.Lock()
		s.initializationError = fmt.Errorf("保存研究结果失败：%w", saveErr)
		s.mu.Unlock()
	} else if s.onComplete != nil {
		s.onComplete(job)
	}
}

func formatResearchDuration(duration time.Duration) string {
	if duration%time.Minute == 0 {
		return fmt.Sprintf("%d分钟", int(duration/time.Minute))
	}
	return duration.String()
}

func (s *ResearchService) Wait(ctx context.Context, id string) (ResearchJob, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		job, err := s.store.Get(ctx, id)
		if err != nil {
			return job, err
		}
		if job.Status != "running" && job.Status != "queued" {
			return job, nil
		}
		select {
		case <-ctx.Done():
			return job, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *ResearchService) Cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.active {
		if item.id == id {
			item.cancel()
			return true
		}
	}
	return false
}

func (s *ResearchService) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	for _, item := range s.active {
		item.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}
