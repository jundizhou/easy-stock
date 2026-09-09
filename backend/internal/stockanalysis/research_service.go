package stockanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrResearchBusy = errors.New("研究任务已达并发上限，请稍后再试")

type ResearchPublisher func(string, string, *Analysis, *ResearchSnapshot) error
type ResearchRunner func(context.Context, ResearchRequest, ResearchPublisher) (Analysis, *ResearchSnapshot, error)
type activeResearch struct {
	id     string
	cancel context.CancelFunc
}

type ResearchService struct {
	store               *ResearchStore
	run                 ResearchRunner
	mu                  sync.Mutex
	active              map[string]activeResearch
	workers             chan struct{}
	wg                  sync.WaitGroup
	closed              bool
	initializationError error
}

func NewResearchService(store *ResearchStore, run ResearchRunner) *ResearchService {
	s := &ResearchService{store: store, run: run, active: map[string]activeResearch{}, workers: make(chan struct{}, 2)}
	if store != nil {
		s.initializationError = store.MarkInterrupted(context.Background())
	}
	return s
}

func (s *ResearchService) Start(ctx context.Context, request ResearchRequest) (ResearchJob, error) {
	request, err := NormalizeResearchRequest(request)
	if err != nil {
		return ResearchJob{}, err
	}
	if s == nil || s.store == nil || s.run == nil {
		return ResearchJob{}, fmt.Errorf("研究任务服务不可用")
	}
	keyBytes, _ := json.Marshal(request)
	key := string(keyBytes)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initializationError != nil {
		return ResearchJob{}, s.initializationError
	}
	if s.closed {
		return ResearchJob{}, fmt.Errorf("研究服务正在关闭")
	}
	if active, ok := s.active[key]; ok {
		return s.store.Get(ctx, active.id)
	}
	if len(s.active) >= 8 {
		return ResearchJob{}, ErrResearchBusy
	}
	now := time.Now().UTC()
	job := ResearchJob{ID: NewResearchID(), Request: request, Status: "queued", Stage: "queued", Message: "研究已提交", StartedAt: now, UpdatedAt: now}
	if err := s.store.Save(ctx, job); err != nil {
		return job, err
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	s.active[key] = activeResearch{id: job.ID, cancel: cancel}
	s.wg.Add(1)
	go s.execute(runCtx, key, job)
	return job, nil
}

func (s *ResearchService) execute(ctx context.Context, key string, job ResearchJob) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		if item, ok := s.active[key]; ok {
			item.cancel()
			delete(s.active, key)
		}
		s.mu.Unlock()
	}()
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
		job.Error = "研究超过12分钟总时限"
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
	}
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
