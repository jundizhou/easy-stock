package portfolioinspection

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/runtimelog"
	"easy-stock/backend/internal/stockanalysis"
)

var ErrJobRunning = errors.New("已有持仓巡检正在运行，请等待完成后再开始新的巡检")

type StockAnalyzer func(context.Context, string) (stockanalysis.Analysis, error)

type Service struct {
	store       *Store
	gateway     hermes.Gateway
	analyze     StockAnalyzer
	logger      *log.Logger
	concurrency int
	mu          sync.Mutex
	runningID   string
}

func NewService(store *Store, gateway hermes.Gateway, analyze StockAnalyzer, logger *log.Logger) *Service {
	service := &Service{store: store, gateway: gateway, analyze: analyze, logger: logger, concurrency: DefaultConcurrency}
	if store != nil {
		_ = store.MarkInterrupted(context.Background())
	}
	return service
}

func (s *Service) Start(ctx context.Context, request Request) (Job, error) {
	normalized, err := normalizeRequest(request)
	if err != nil {
		return Job{}, err
	}
	if s == nil || s.store == nil || s.analyze == nil {
		return Job{}, errors.New("持仓巡检服务不可用")
	}
	if s.gateway == nil {
		return Job{}, errors.New("AI分析底座不可用，请先在系统设置中配置模型")
	}
	status := s.gateway.Status()
	if !status.Available || !status.Configured {
		return Job{}, errors.New(firstNonEmpty(status.Message, "请先在系统设置中配置AI模型"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runningID != "" {
		return Job{}, ErrJobRunning
	}
	now := time.Now().UTC()
	job := Job{
		ID: newID(), Status: "running", Stage: "queued", Request: normalized,
		Results: make([]HoldingResult, len(normalized.Holdings)), TotalStocks: len(normalized.Holdings),
		Message: "任务已提交，将逐只完成个股分析后生成持仓总报告", StartedAt: now, UpdatedAt: now,
	}
	for index, holding := range normalized.Holdings {
		job.Results[index] = HoldingResult{Holding: holding, Status: "queued"}
	}
	if _, err := s.store.Save(ctx, job); err != nil {
		return Job{}, err
	}
	s.runningID = job.ID
	if s.logger != nil {
		s.logger.Printf("level=info event=portfolio_inspection_start feature=portfolio-inspection job_id=%q profile=%s stocks=%d total_position=%d", job.ID, normalized.TraderProfile, len(normalized.Holdings), totalPosition(normalized.Holdings))
	}
	go s.run(job)
	return job, nil
}

func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	if s == nil || s.store == nil {
		return Job{}, errors.New("持仓巡检服务不可用")
	}
	return s.store.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, limit int) ([]Job, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("持仓巡检服务不可用")
	}
	return s.store.List(ctx, limit)
}

func (s *Service) run(job Job) {
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
	type event struct {
		index    int
		started  bool
		analysis stockanalysis.Analysis
		err      error
	}
	work := make(chan int)
	events := make(chan event)
	workers := min(s.concurrency, len(job.Results))
	for worker := 0; worker < workers; worker++ {
		go func() {
			for index := range work {
				events <- event{index: index, started: true}
				analysis, err := s.analyze(ctx, job.Results[index].Holding.Symbol)
				events <- event{index: index, analysis: analysis, err: err}
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
	stockStartedAt := map[int]time.Time{}
	completed := 0
	for completed < len(job.Results) {
		item := <-events
		if item.started {
			active[item.index] = struct{}{}
			stockStartedAt[item.index] = time.Now()
			job.Results[item.index].Status = "running"
			job.Stage = "analyzing_stocks"
			job.Message = fmt.Sprintf("正在分析持仓个股，已完成 %d/%d", completed, len(job.Results))
			job.CurrentSymbols = activeSymbols(job.Results, active)
			s.persist(job)
			continue
		}
		delete(active, item.index)
		completed++
		job.Results[item.index].CompletedAt = time.Now().UTC()
		if item.err != nil {
			job.Results[item.index].Status = "failed"
			job.Results[item.index].Error = item.err.Error()
		} else {
			job.Results[item.index].Status = "succeeded"
			job.Results[item.index].Analysis = &item.analysis
			job.Results[item.index].Holding.Name = item.analysis.Name
			job.Request.Holdings[item.index].Name = item.analysis.Name
		}
		if s.logger != nil {
			status := job.Results[item.index].Status
			duration := time.Since(stockStartedAt[item.index]).Milliseconds()
			if item.err != nil {
				s.logger.Printf("level=warn event=portfolio_stock_analysis_complete feature=portfolio-inspection job_id=%q stock_index=%d status=%s duration_ms=%d error=%q", job.ID, item.index+1, status, duration, runtimelog.Redact(item.err.Error()))
			} else {
				s.logger.Printf("level=info event=portfolio_stock_analysis_complete feature=portfolio-inspection job_id=%q stock_index=%d status=%s duration_ms=%d", job.ID, item.index+1, status, duration)
			}
		}
		job.CompletedStocks = completed
		job.CoveragePercent = coverage(job.Results, job.Request)
		job.CurrentSymbols = activeSymbols(job.Results, active)
		job.Message = fmt.Sprintf("正在分析持仓个股，已完成 %d/%d", completed, len(job.Results))
		s.persist(job)
	}

	rules, _ := RulesFor(job.Request.TraderProfile)
	metrics := CalculateMetrics(job.Request, job.Results, rules)
	job.Stage = "aggregating"
	job.CurrentSymbols = nil
	job.CoveragePercent = metrics.CoveragePercent
	job.Message = "个股分析已完成，正在生成组合巡检报告"
	s.persist(job)
	if s.logger != nil {
		s.logger.Printf("level=info event=portfolio_aggregation_start feature=portfolio-inspection job_id=%q coverage=%.1f succeeded=%d total=%d", job.ID, metrics.CoveragePercent, succeededCount(job.Results), len(job.Results))
	}

	conclusion := localReport(job.Request, job.Results, metrics, rules)
	aiErr := error(nil)
	if metrics.CoveragePercent >= MinimumAICoverage {
		conclusion, aiErr = s.generateAIReport(ctx, job.Request, job.Results, metrics, rules)
	} else {
		aiErr = fmt.Errorf("有效个股分析仅覆盖 %.1f%% 持仓，低于组合结论所需的 %d%%", metrics.CoveragePercent, MinimumAICoverage)
	}
	if aiErr != nil {
		conclusion = localReport(job.Request, job.Results, metrics, rules)
		conclusion.DataLimitations = append(conclusion.DataLimitations, aiErr.Error())
		if s.logger != nil {
			s.logger.Printf("level=warn event=portfolio_aggregation_degraded feature=portfolio-inspection job_id=%q error=%q", job.ID, runtimelog.Redact(aiErr.Error()))
		}
	}
	completedAt := time.Now().UTC()
	job.Report = &Report{ID: job.ID, PromptVersion: PromptVersion, AlgorithmVersion: AlgorithmVersion, Profile: rules, Holdings: job.Results, Metrics: metrics, Conclusion: conclusion, GeneratedAt: completedAt}
	job.ReportAvailable = true
	job.Stage = "completed"
	job.UpdatedAt = completedAt
	job.CompletedAt = completedAt
	failedCount := len(job.Results) - succeededCount(job.Results)
	if failedCount == 0 && aiErr == nil {
		job.Status = "succeeded"
		job.Message = "持仓 AI 巡检已完成，报告已保存在本机"
		job.Error = ""
	} else {
		job.Status = "partial"
		job.Message = "巡检报告已生成，部分分析使用降级结果"
		job.Error = firstNonEmpty(errorText(aiErr), fmt.Sprintf("%d 只股票分析失败", failedCount))
	}
	s.persist(job)
	if s.logger != nil {
		s.logger.Printf("level=info event=portfolio_inspection_complete feature=portfolio-inspection job_id=%q status=%s stocks=%d coverage=%.1f duration_ms=%d", job.ID, job.Status, len(job.Results), metrics.CoveragePercent, time.Since(started).Milliseconds())
	}
}

func (s *Service) generateAIReport(ctx context.Context, request Request, results []HoldingResult, metrics Metrics, rules ProfileRules) (AIReport, error) {
	prompt, err := buildPrompt(request, results, metrics, rules)
	if err != nil {
		return AIReport{}, err
	}
	response, err := s.gateway.Prompt(ctx, prompt)
	if err != nil {
		return AIReport{}, fmt.Errorf("持仓组合AI分析失败: %w", err)
	}
	var report AIReport
	if err := decodeJSONObject(response.Content, &report); err != nil {
		return AIReport{}, fmt.Errorf("持仓组合AI未返回有效JSON: %w", err)
	}
	if strings.TrimSpace(report.ExecutiveSummary) == "" || strings.TrimSpace(report.RiskLevel) == "" {
		return AIReport{}, errors.New("持仓组合AI返回缺少必要字段")
	}
	report.HealthScore = metrics.HealthScore
	report.RiskLevel = riskLevelForMetrics(metrics, rules)
	report.StyleMatch = styleMatchLabel(metrics.StyleMatchScore)
	report.Confidence = math.Max(0, math.Min(1, report.Confidence))
	report.Source = "hermes-ai"
	report.PrimaryRisks = limitStrings(report.PrimaryRisks, 8)
	report.ConcentrationFinding = limitStrings(report.ConcentrationFinding, 8)
	report.AdjustmentOrder = limitStrings(report.AdjustmentOrder, 10)
	report.NextChecklist = limitStrings(report.NextChecklist, 10)
	report.DataLimitations = limitStrings(report.DataLimitations, 10)
	if len(report.Holdings) > len(request.Holdings) {
		report.Holdings = report.Holdings[:len(request.Holdings)]
	}
	return report, nil
}

func normalizeRequest(request Request) (Request, error) {
	if _, ok := RulesFor(request.TraderProfile); !ok {
		return Request{}, errors.New("请选择有效的交易风格")
	}
	if len(request.Holdings) == 0 {
		return Request{}, errors.New("请至少添加一只持仓股票")
	}
	if len(request.Holdings) > MaxHoldings {
		return Request{}, fmt.Errorf("持仓股票最多支持 %d 只", MaxHoldings)
	}
	seen := map[string]struct{}{}
	total := 0
	normalized := Request{TraderProfile: request.TraderProfile, Holdings: make([]Holding, 0, len(request.Holdings))}
	for _, holding := range request.Holdings {
		symbol, err := foundation.NormalizeSymbol(holding.Symbol)
		if err != nil {
			return Request{}, fmt.Errorf("无效股票代码 %q: %w", holding.Symbol, err)
		}
		if _, exists := seen[symbol.Canonical]; exists {
			return Request{}, fmt.Errorf("股票 %s 重复添加", symbol.Canonical)
		}
		if holding.Weight <= 0 || holding.Weight > 100 {
			return Request{}, fmt.Errorf("%s 的持仓占比必须在 1%% 到 100%% 之间", symbol.Canonical)
		}
		if holding.CostPrice != nil && *holding.CostPrice <= 0 {
			return Request{}, fmt.Errorf("%s 的持仓成本必须大于 0", symbol.Canonical)
		}
		total += holding.Weight
		seen[symbol.Canonical] = struct{}{}
		holding.Symbol = symbol.Canonical
		holding.Name = strings.TrimSpace(holding.Name)
		normalized.Holdings = append(normalized.Holdings, holding)
	}
	if total > 100 {
		return Request{}, errors.New("持仓总占比不能超过 100%")
	}
	return normalized, nil
}

func activeSymbols(results []HoldingResult, active map[int]struct{}) []string {
	symbols := make([]string, 0, len(active))
	for index := range active {
		symbols = append(symbols, results[index].Holding.Symbol)
	}
	sort.Strings(symbols)
	return symbols
}

func coverage(results []HoldingResult, request Request) float64 {
	total, succeeded := 0, 0
	for index, holding := range request.Holdings {
		total += holding.Weight
		if index < len(results) && results[index].Status == "succeeded" {
			succeeded += holding.Weight
		}
	}
	if total == 0 {
		return 0
	}
	return round(float64(succeeded)/float64(total)*100, 1)
}

func totalPosition(holdings []Holding) int {
	total := 0
	for _, holding := range holdings {
		total += holding.Weight
	}
	return total
}

func succeededCount(results []HoldingResult) int {
	count := 0
	for _, result := range results {
		if result.Status == "succeeded" {
			count++
		}
	}
	return count
}

func (s *Service) persist(job Job) {
	job.UpdatedAt = time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.store.Save(ctx, job); err != nil && s.logger != nil {
		s.logger.Printf("level=warn event=portfolio_inspection_persist_error feature=portfolio-inspection job_id=%q error=%q", job.ID, runtimelog.Redact(err.Error()))
	}
}

func newID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	return fmt.Sprintf("portfolio-%d", time.Now().UnixNano())
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func decodeJSONObject(content string, target any) error {
	content = strings.TrimSpace(content)
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return errors.New("JSON object not found")
	}
	return json.Unmarshal([]byte(content[start:end+1]), target)
}

func limitStrings(values []string, limit int) []string {
	result := make([]string, 0, min(len(values), limit))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, value)
		if len(result) >= limit {
			break
		}
	}
	return result
}
