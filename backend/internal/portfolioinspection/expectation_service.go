package portfolioinspection

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/review"
	"easy-stock/backend/internal/runtimelog"
)

var ErrExpectationRunning = errors.New("已有持仓明日预期正在运行，请等待完成后再开始")

type DailySummaryStore interface {
	GetDailySummary(context.Context, string) (review.DailySummary, error)
}

type ExpectationService struct {
	store       *Store
	reviews     DailySummaryStore
	gateway     hermes.Gateway
	analyze     StockAnalyzer
	logger      *log.Logger
	concurrency int
	mu          sync.Mutex
	runningID   string
}

func NewExpectationService(store *Store, reviews DailySummaryStore, gateway hermes.Gateway, analyze StockAnalyzer, logger *log.Logger) *ExpectationService {
	service := &ExpectationService{store: store, reviews: reviews, gateway: gateway, analyze: analyze, logger: logger, concurrency: DefaultConcurrency}
	if store != nil {
		_ = store.MarkExpectationsInterrupted(context.Background())
	}
	return service
}

func (s *ExpectationService) Start(ctx context.Context, request ExpectationRequest) (ExpectationJob, error) {
	request.SummaryDate = strings.TrimSpace(request.SummaryDate)
	if request.SummaryDate == "" {
		return ExpectationJob{}, errors.New("缺少复盘交易日期")
	}
	normalized, err := normalizeRequest(Request{TraderProfile: request.TraderProfile, Holdings: request.Holdings})
	if err != nil {
		return ExpectationJob{}, err
	}
	request.TraderProfile, request.Holdings = normalized.TraderProfile, normalized.Holdings
	if s == nil || s.store == nil || s.reviews == nil || s.analyze == nil {
		return ExpectationJob{}, errors.New("持仓明日预期服务不可用")
	}
	summary, err := s.reviews.GetDailySummary(ctx, request.SummaryDate)
	if errors.Is(err, sql.ErrNoRows) {
		return ExpectationJob{}, errors.New("未找到对应日期的大V综合复盘，请先完成每日复盘")
	}
	if err != nil {
		return ExpectationJob{}, fmt.Errorf("读取每日复盘失败: %w", err)
	}
	if s.gateway == nil {
		return ExpectationJob{}, errors.New("AI分析底座不可用，请先在系统设置中配置模型")
	}
	status := s.gateway.Status()
	if !status.Available || !status.Configured {
		return ExpectationJob{}, errors.New(firstNonEmpty(status.Message, "请先在系统设置中配置AI模型"))
	}
	portfolioHash, err := stableHash(normalized)
	if err != nil {
		return ExpectationJob{}, err
	}
	summaryHash, err := stableHash(summary)
	if err != nil {
		return ExpectationJob{}, err
	}
	if !request.Force {
		cached, cacheErr := s.store.FindExpectation(ctx, request.SummaryDate, portfolioHash, summaryHash, ExpectationPromptVersion)
		if cacheErr == nil && (cached.Status == "running" || cached.ReportAvailable) {
			cached.Cached = cached.ReportAvailable
			return cached, nil
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runningID != "" {
		return ExpectationJob{}, ErrExpectationRunning
	}
	now := time.Now().UTC()
	job := ExpectationJob{
		ID: newID(), Status: "running", Stage: "queued", PromptVersion: ExpectationPromptVersion,
		PortfolioHash: portfolioHash, SummaryHash: summaryHash, Request: request,
		Results: make([]HoldingResult, len(normalized.Holdings)), TotalStocks: len(normalized.Holdings),
		Message: "任务已提交，将刷新持仓个股分析并结合今日复盘生成明日预期", StartedAt: now, UpdatedAt: now,
	}
	for index, holding := range normalized.Holdings {
		job.Results[index] = HoldingResult{Holding: holding, Status: "queued"}
	}
	if _, err := s.store.SaveExpectation(ctx, job); err != nil {
		return ExpectationJob{}, err
	}
	s.runningID = job.ID
	if s.logger != nil {
		s.logger.Printf("level=info event=portfolio_expectation_start feature=portfolio-expectation job_id=%q trade_date=%s stocks=%d", job.ID, request.SummaryDate, len(request.Holdings))
	}
	go s.run(job, summary)
	return job, nil
}

func (s *ExpectationService) Get(ctx context.Context, id string) (ExpectationJob, error) {
	if s == nil || s.store == nil {
		return ExpectationJob{}, errors.New("持仓明日预期服务不可用")
	}
	return s.store.GetExpectation(ctx, id)
}

func (s *ExpectationService) Latest(ctx context.Context, tradeDate string) (ExpectationJob, error) {
	if s == nil || s.store == nil {
		return ExpectationJob{}, errors.New("持仓明日预期服务不可用")
	}
	return s.store.LatestExpectation(ctx, tradeDate)
}

func (s *ExpectationService) run(job ExpectationJob, summary review.DailySummary) {
	started := time.Now()
	defer func() {
		s.mu.Lock()
		if s.runningID == job.ID {
			s.runningID = ""
		}
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	work := make(chan int)
	type analysisEvent struct {
		index          int
		started        bool
		analysisResult HoldingResult
	}
	events := make(chan analysisEvent)
	workers := min(s.concurrency, len(job.Results))
	for worker := 0; worker < workers; worker++ {
		go func() {
			for index := range work {
				events <- analysisEvent{index: index, started: true}
				analysis, err := s.analyze(ctx, job.Results[index].Holding.Symbol)
				result := HoldingResult{Holding: job.Results[index].Holding, CompletedAt: time.Now().UTC()}
				if err != nil {
					result.Status, result.Error = "failed", err.Error()
				} else {
					result.Status, result.Analysis = "succeeded", &analysis
					result.Holding.Name = analysis.Name
				}
				events <- analysisEvent{index: index, analysisResult: result}
			}
		}()
	}
	go func() {
		for index := range job.Results {
			work <- index
		}
		close(work)
	}()
	active := map[int]struct{}{}
	completed := 0
	for completed < len(job.Results) {
		item := <-events
		if item.started {
			active[item.index] = struct{}{}
			job.Results[item.index].Status = "running"
			job.Stage = "analyzing_stocks"
			job.Message = fmt.Sprintf("正在刷新持仓个股分析，已完成 %d/%d", completed, len(job.Results))
			job.CurrentSymbols = expectationActiveSymbols(job.Results, active)
			s.persistExpectation(job)
			continue
		}
		delete(active, item.index)
		completed++
		job.Results[item.index] = item.analysisResult
		job.Request.Holdings[item.index].Name = item.analysisResult.Holding.Name
		job.CompletedStocks = completed
		job.CoveragePercent = coverage(job.Results, Request{TraderProfile: job.Request.TraderProfile, Holdings: job.Request.Holdings})
		job.CurrentSymbols = expectationActiveSymbols(job.Results, active)
		job.Message = fmt.Sprintf("正在刷新持仓个股分析，已完成 %d/%d", completed, len(job.Results))
		s.persistExpectation(job)
	}

	request := Request{TraderProfile: job.Request.TraderProfile, Holdings: job.Request.Holdings}
	rules, _ := RulesFor(request.TraderProfile)
	metrics := CalculateMetrics(request, job.Results, rules)
	job.Stage = "synthesizing"
	job.CurrentSymbols = nil
	job.CoveragePercent = metrics.CoveragePercent
	job.Message = "个股分析已完成，正在把今日复盘映射到真实持仓"
	s.persistExpectation(job)

	conclusion := localExpectationReport(summary, request, job.Results, metrics)
	aiErr := error(nil)
	if metrics.CoveragePercent >= MinimumAICoverage {
		prompt, promptErr := buildExpectationPrompt(summary, request, job.Results, metrics, rules)
		if promptErr != nil {
			aiErr = promptErr
		} else if response, err := s.gateway.Prompt(ctx, prompt); err != nil {
			aiErr = fmt.Errorf("持仓明日预期AI分析失败: %w", err)
		} else if err := decodeJSONObject(response.Content, &conclusion); err != nil {
			aiErr = fmt.Errorf("持仓明日预期AI未返回有效JSON: %w", err)
		} else if err := normalizeExpectationConclusion(&conclusion, request); err != nil {
			aiErr = err
		} else {
			conclusion.Source = "hermes-ai"
		}
	} else {
		aiErr = fmt.Errorf("有效个股分析仅覆盖 %.1f%% 持仓，低于完整预期所需的 %d%%", metrics.CoveragePercent, MinimumAICoverage)
	}
	if aiErr != nil {
		conclusion = localExpectationReport(summary, request, job.Results, metrics)
		conclusion.DataLimitations = limitStrings(append(conclusion.DataLimitations, aiErr.Error()), 10)
	}
	completedAt := time.Now().UTC()
	job.Report = &ExpectationReport{ID: job.ID, TradeDate: summary.TradeDate, PromptVersion: ExpectationPromptVersion, Profile: rules, Holdings: job.Results, Metrics: metrics, Conclusion: conclusion, GeneratedAt: completedAt}
	job.ReportAvailable = true
	job.Stage = "completed"
	job.UpdatedAt, job.CompletedAt = completedAt, completedAt
	if aiErr == nil && succeededCount(job.Results) == len(job.Results) {
		job.Status, job.Message, job.Error = "succeeded", "持仓明日预期已完成并保存到本机", ""
	} else {
		job.Status, job.Message, job.Error = "partial", "持仓明日预期已生成，部分内容使用降级结果", errorText(aiErr)
	}
	s.persistExpectation(job)
	if s.logger != nil {
		s.logger.Printf("level=info event=portfolio_expectation_complete feature=portfolio-expectation job_id=%q status=%s coverage=%.1f duration_ms=%d", job.ID, job.Status, metrics.CoveragePercent, time.Since(started).Milliseconds())
	}
}

func (s *ExpectationService) persistExpectation(job ExpectationJob) {
	job.UpdatedAt = time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.store.SaveExpectation(ctx, job); err != nil && s.logger != nil {
		s.logger.Printf("level=warn event=portfolio_expectation_persist_error feature=portfolio-expectation job_id=%q error=%q", job.ID, runtimelog.Redact(err.Error()))
	}
}

func expectationActiveSymbols(results []HoldingResult, active map[int]struct{}) []string {
	symbols := make([]string, 0, len(active))
	for index := range active {
		symbols = append(symbols, results[index].Holding.Symbol)
	}
	sort.Strings(symbols)
	return symbols
}

func stableHash(value any) (string, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}
