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

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/runtimelog"
	"easy-stock/backend/internal/stockanalysis"
)

var ErrJobRunning = errors.New("已有持仓巡检正在运行，请等待完成后再开始新的巡检")

type StockAnalyzer func(context.Context, string) (stockanalysis.Analysis, error)
type HoldingAnalyzer func(context.Context, Holding) (stockanalysis.Analysis, error)
type ResearchResolver func(context.Context, Holding, Request, time.Time, bool, string, func(HoldingResult)) (HoldingResult, error)
type QuoteRefresher func(context.Context, []string) ([]foundation.Quote, error)

type Service struct {
	store          *Store
	gateway        agent.Gateway
	analyze        StockAnalyzer
	analyzeHolding HoldingAnalyzer
	logger         *log.Logger
	concurrency    int
	mu             sync.Mutex
	runningID      string
	resolver       ResearchResolver
	quotes         QuoteRefresher
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	closed         bool
	onComplete     func(Job)
}

// ConfigureCompletion must be called before starting jobs.
func (s *Service) ConfigureCompletion(handler func(Job)) {
	s.onComplete = handler
}

func NewService(store *Store, gateway agent.Gateway, analyze StockAnalyzer, logger *log.Logger, holdingAnalyzers ...HoldingAnalyzer) *Service {
	service := &Service{store: store, gateway: gateway, analyze: analyze, logger: logger, concurrency: DefaultConcurrency}
	if len(holdingAnalyzers) > 0 {
		service.analyzeHolding = holdingAnalyzers[0]
	}
	if store != nil {
		_ = store.MarkInterrupted(context.Background())
	}
	return service
}

func (s *Service) ConfigureResearch(resolver ResearchResolver, quotes QuoteRefresher) {
	s.resolver, s.quotes = resolver, quotes
}

func (s *Service) Start(ctx context.Context, request Request) (Job, error) {
	return s.start(ctx, request, nil)
}

func (s *Service) Resume(ctx context.Context, id string) (Job, error) {
	previous, err := s.Get(ctx, id)
	if err != nil {
		return Job{}, err
	}
	if !previous.ResumeAvailable {
		return Job{}, errors.New("该持仓报告无需恢复，请重新分析组合")
	}
	previous.Request.ForceSymbols = nil
	return s.start(ctx, previous.Request, &previous)
}

func (s *Service) start(ctx context.Context, request Request, previous *Job) (Job, error) {
	normalized, err := normalizeRequest(request)
	if err != nil {
		return Job{}, err
	}
	if s == nil || s.store == nil || (s.resolver == nil && s.analyze == nil) {
		return Job{}, errors.New("持仓分析服务不可用")
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
	if s.closed {
		return Job{}, errors.New("持仓分析服务正在关闭")
	}
	if s.runningID != "" {
		return Job{}, ErrJobRunning
	}
	now := time.Now().UTC()
	job := Job{ID: newID(), Status: "running", Stage: "resolving_reports", Request: normalized, Results: make([]HoldingResult, len(normalized.Holdings)), TotalStocks: len(normalized.Holdings), Message: "正在查找24小时内个股AI报告，仅补齐缺失股票", StartedAt: now, UpdatedAt: now}
	for i, h := range normalized.Holdings {
		job.Results[i] = HoldingResult{Holding: h, Status: "queued"}
	}
	if previous != nil {
		job.ResumedFrom = previous.ID
		for i, old := range previous.Results {
			if i >= len(job.Results) {
				break
			}
			if validHoldingResearch(old) {
				old.ResearchOrigin = "reused"
				job.Results[i] = old
				job.CompletedStocks++
			} else {
				job.Results[i].AnalysisID = old.AnalysisID
			}
		}
	}
	if _, err := s.store.Save(ctx, job); err != nil {
		return Job{}, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	s.runningID, s.cancel = job.ID, cancel
	s.wg.Add(1)
	initial := job
	initial.Results = append([]HoldingResult(nil), job.Results...)
	go s.run(runCtx, job)
	return initial, nil
}

func (s *Service) Cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runningID != id || s.cancel == nil {
		return false
	}
	s.cancel()
	return true
}
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func decorateJob(job Job) Job {
	job.ResumeAvailable = job.Status != "running" && job.Status != "succeeded" && len(job.Results) > 0 && (job.Report == nil || job.Report.AlgorithmVersion == AlgorithmVersion)
	return job
}

func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	if s == nil || s.store == nil {
		return Job{}, errors.New("持仓巡检服务不可用")
	}
	job, err := s.store.Get(ctx, id)
	return decorateJob(job), err
}

func (s *Service) List(ctx context.Context, limit int) ([]Job, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("持仓巡检服务不可用")
	}
	jobs, err := s.store.List(ctx, limit)
	for i := range jobs {
		jobs[i] = decorateJob(jobs[i])
	}
	return jobs, err
}

func (s *Service) run(ctx context.Context, job Job) {
	started := time.Now()
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		if s.runningID == job.ID {
			s.cancel()
			s.runningID = ""
			s.cancel = nil
		}
		s.mu.Unlock()
	}()
	bound, release, err := agent.BindTask(ctx, s.gateway)
	if err != nil {
		job.Status, job.Stage, job.Error = "failed", "failed", err.Error()
		job.CompletedAt = time.Now().UTC()
		s.persist(job)
		return
	}
	ctx = bound
	defer release()
	responseWait := time.Duration(0)
	if llm, ok := agent.BoundLLM(ctx); ok {
		responseWait = time.Duration(appsettings.NormalizeLLMResponseTimeoutSeconds(llm.ResponseTimeoutSeconds)) * time.Second
	}
	effort, _ := agent.BoundReasoningEffort(ctx)
	researchBudget := stockanalysis.ResearchBudgetFor(stockanalysis.ResearchRequest{AnalysisLevel: job.Request.ResearchLevel}, responseWait, effort)
	budget := time.Duration((len(job.Results)+DefaultConcurrency-1)/DefaultConcurrency)*researchBudget.TotalTimeout() + AggregationTimeout + 2*time.Minute
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	type event struct {
		index  int
		result HoldingResult
		done   bool
		err    error
	}
	events := make(chan event, len(job.Results)*4)
	send := func(e event) {
		select {
		case events <- e:
		case <-ctx.Done():
		}
	}
	inputs := append([]HoldingResult(nil), job.Results...)
	request := job.Request
	request.Holdings = append([]Holding(nil), job.Request.Holdings...)
	asOf := job.StartedAt
	pending := make([]int, 0)
	for i, r := range inputs {
		if !validHoldingResearch(r) {
			pending = append(pending, i)
		}
	}
	work := make(chan int)
	for worker := 0; worker < min(s.concurrency, len(pending)); worker++ {
		go func() {
			for index := range work {
				base := inputs[index]
				result := base
				result.Status = "resolving"
				send(event{index: index, result: result})
				var err error
				if s.resolver != nil {
					force := false
					for _, symbol := range request.ForceSymbols {
						if symbol == base.Holding.Symbol {
							force = true
						}
					}
					result, err = s.resolver(ctx, base.Holding, request, asOf, force, base.AnalysisID, func(r HoldingResult) { send(event{index: index, result: r}) })
				} else {
					var analysis stockanalysis.Analysis
					if s.analyzeHolding != nil {
						analysis, err = s.analyzeHolding(ctx, base.Holding)
					} else {
						analysis, err = s.analyze(ctx, base.Holding.Symbol)
					}
					result = HoldingResult{Holding: base.Holding, Analysis: &analysis, AnalysisID: analysis.AnalysisID, ResearchOrigin: "new", Status: "succeeded"}
					if !validHoldingResearch(result) && err == nil {
						err = errors.New("个股AI研究未成功，量化快照不算成功报告")
					}
				}
				send(event{index: index, result: result, done: true, err: err})
			}
		}()
	}
	go func() {
		defer close(work)
		for _, index := range pending {
			select {
			case work <- index:
			case <-ctx.Done():
				return
			}
		}
	}()
	completed := len(job.Results) - len(pending)
	for completed < len(job.Results) {
		select {
		case <-ctx.Done():
			for i := range job.Results {
				if !validHoldingResearch(job.Results[i]) {
					job.Results[i].Status = "failed"
					job.Results[i].Error = "组合等待已结束，已启动的个股研究仍可独立完成"
				}
			}
			s.finishPartial(&job, ctx.Err())
			return
		case item := <-events:
			r := item.result
			r.Holding = inputs[item.index].Holding
			if r.Analysis != nil && r.Analysis.Name != "" {
				r.Holding.Name = r.Analysis.Name
				job.Request.Holdings[item.index].Name = r.Analysis.Name
			}
			if item.done {
				completed++
				r.CompletedAt = time.Now().UTC()
				if item.err != nil {
					r.Status = "failed"
					r.Error = item.err.Error()
				} else if !validHoldingResearch(r) {
					r.Status = "failed"
					r.Error = "个股AI报告未通过完整性检查"
				} else {
					r.Status = "succeeded"
				}
			}
			job.Results[item.index] = r
			job.CompletedStocks = completed
			job.CoveragePercent = coverage(job.Results, job.Request)
			updateResearchCounts(&job)
			job.CurrentSymbols = nil
			for _, r := range job.Results {
				if oneOf(r.Status, "running", "queued", "resolving") {
					job.CurrentSymbols = append(job.CurrentSymbols, r.Holding.Symbol)
				}
			}
			job.Stage = "analyzing_stocks"
			job.Message = fmt.Sprintf("已完成 %d/%d 只 · 复用 %d 份 · 新研究 %d 只 · 共享任务 %d 只", completed, len(job.Results), job.ReusedStocks, job.NewStocks, job.SharedStocks)
			s.persist(job)
		}
	}
	if ctx.Err() != nil {
		s.finishPartial(&job, ctx.Err())
		return
	}
	s.refreshQuotes(ctx, &job)
	if succeededCount(job.Results) != len(job.Results) {
		s.finishPartial(&job, errors.New("部分个股AI研究未完成，请补齐失败个股；已有成功报告已保留"))
		return
	}
	updateResearchCounts(&job)
	rules, _ := RulesFor(job.Request.TraderProfile)
	metrics := metricsForReport(job.Request, job.Results, rules)
	job.Stage = "aggregating"
	job.CurrentSymbols = nil
	job.CoveragePercent = metrics.AIResearchCoveragePercent
	job.AggregationStartedAt = time.Now().UTC()
	job.Message = "个股AI报告已就绪，正在综合评分和评估组合风险"
	s.persist(job)
	if s.logger != nil {
		s.logger.Printf("level=info event=portfolio_aggregation_start feature=portfolio-inspection job_id=%q reused=%d new=%d shared=%d", job.ID, job.ReusedStocks, job.NewStocks, job.SharedStocks)
	}
	conclusion, err := s.scorePortfolio(ctx, job.Request, job.Results, metrics, rules)
	job.AggregationDurationMS = time.Since(job.AggregationStartedAt).Milliseconds()
	if err != nil {
		s.finishPartial(&job, err)
		return
	}
	now := time.Now().UTC()
	job.Report = &Report{ID: job.ID, PromptVersion: PromptVersion, AlgorithmVersion: AlgorithmVersion, Profile: rules, Holdings: job.Results, Metrics: metrics, Conclusion: conclusion, GeneratedAt: now, Request: job.Request, Facts: portfolioFacts(job.Request, job.Results, metrics), Model: agent.BoundModel(ctx)}
	job.ReportAvailable = true
	job.Status = "succeeded"
	job.Stage = "completed"
	job.CompletedAt = now
	job.Message = "持仓AI分析已完成，组合评分与报告已保存"
	job.Error = ""
	s.persist(job)
	if s.logger != nil {
		s.logger.Printf("level=info event=portfolio_inspection_complete feature=portfolio-inspection job_id=%q status=%s duration_ms=%d aggregation_duration_ms=%d", job.ID, job.Status, time.Since(started).Milliseconds(), job.AggregationDurationMS)
	}
}

func updateResearchCounts(job *Job) {
	job.ReusedStocks, job.NewStocks, job.SharedStocks = 0, 0, 0
	for _, r := range job.Results {
		switch r.ResearchOrigin {
		case "reused":
			job.ReusedStocks++
		case "new":
			job.NewStocks++
		case "shared_running":
			job.SharedStocks++
		}
	}
}
func metricsForReport(request Request, results []HoldingResult, rules ProfileRules) Metrics {
	copies := append([]HoldingResult(nil), results...)
	for i, r := range copies {
		if r.Analysis != nil && r.CurrentQuote != nil {
			a := *r.Analysis
			a.Quote = *r.CurrentQuote
			copies[i].Analysis = &a
		}
	}
	return CalculateMetrics(request, copies, rules)
}
func (s *Service) refreshQuotes(ctx context.Context, job *Job) {
	var symbols []string
	for _, r := range job.Results {
		symbols = append(symbols, r.Holding.Symbol)
	}
	var quotes []foundation.Quote
	var err error
	if s.quotes != nil {
		loadCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		quotes, err = s.quotes(loadCtx, symbols)
		cancel()
	}
	bySymbol := map[string]foundation.Quote{}
	for _, q := range quotes {
		symbol, e := foundation.NormalizeSymbol(q.Symbol)
		if e == nil && q.Price > 0 && !math.IsNaN(q.Price) && !math.IsInf(q.Price, 0) {
			q.Symbol = symbol.Canonical
			bySymbol[q.Symbol] = q
		}
	}
	for i, r := range job.Results {
		q, ok := bySymbol[r.Holding.Symbol]
		if ok {
			job.Results[i].CurrentQuote = &q
			job.Results[i].QuoteStatus = "refreshed"
			job.Results[i].QuoteMessage = "行情已刷新，原研究证据未更新"
			if q.Meta.Stale || q.Meta.CarryForward || q.TradeTime.IsZero() {
				job.Results[i].QuoteStatus = "dated"
				job.Results[i].QuoteMessage = "取得行情快照，需核对交易时间；不代表实时研究"
			}
		} else {
			job.Results[i].QuoteStatus = "original"
			job.Results[i].QuoteMessage = "行情刷新不可用，使用原研究快照与原时点"
			if err != nil {
				job.Results[i].QuoteMessage += "：" + clip(err.Error(), 160)
			}
		}
	}
}
func (s *Service) finishPartial(job *Job, err error) {
	rules, _ := RulesFor(job.Request.TraderProfile)
	metrics := metricsForReport(job.Request, job.Results, rules)
	// Do not publish old rule-based actions or zero-risk scores as completed AI output.
	conclusion := AIReport{Source: "incomplete", RiskLevel: "待评估", StyleMatch: "待评估", ExecutiveSummary: fmt.Sprintf("已完成%d/%d只个股AI研究，覆盖%.1f%%持仓。组合评估尚未完成，已有报告已保留，可恢复任务。", succeededCount(job.Results), len(job.Results), metrics.AIResearchCoveragePercent), PrimaryRisks: []string{}, ConcentrationFinding: []string{}, Holdings: []HoldingConclusion{}, AdjustmentOrder: []string{}, Scenarios: []Scenario{}, NextChecklist: []string{}, DataLimitations: []string{err.Error()}}
	for _, r := range job.Results {
		if !validHoldingResearch(r) {
			conclusion.DataLimitations = append(conclusion.DataLimitations, r.Holding.Symbol+"："+r.Error)
		}
	}
	now := time.Now().UTC()
	job.Report = &Report{ID: job.ID, PromptVersion: PromptVersion, AlgorithmVersion: AlgorithmVersion, Profile: rules, Holdings: job.Results, Metrics: metrics, Conclusion: conclusion, GeneratedAt: now, Request: job.Request, Facts: portfolioFacts(job.Request, job.Results, metrics)}
	job.Status = "partial"
	if errors.Is(err, context.Canceled) {
		job.Status = "cancelled"
	}
	job.Stage = "completed"
	job.CompletedAt = now
	job.ReportAvailable = true
	job.CurrentSymbols = nil
	job.CoveragePercent = metrics.AIResearchCoveragePercent
	job.Message = "已有个股报告已保存，可恢复缺失研究或重试组合评估"
	job.Error = err.Error()
	updateResearchCounts(job)
	s.persist(*job)
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
	if request.Horizon == "" {
		request.Horizon = "swing"
	}
	if !oneOf(request.Horizon, "short", "swing", "medium") {
		return Request{}, errors.New("请选择有效持有周期")
	}
	if request.ResearchLevel == "" {
		request.ResearchLevel = stockanalysis.ResearchLevelStandard
	}
	if !oneOf(string(request.ResearchLevel), "standard", "deep") {
		return Request{}, errors.New("补齐个股研究请选择标准或深度")
	}
	normalized := Request{SourceOptimizationID: strings.TrimSpace(request.SourceOptimizationID), TraderProfile: request.TraderProfile, Horizon: request.Horizon, ResearchLevel: request.ResearchLevel, Holdings: make([]Holding, 0, len(request.Holdings))}
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
		if holding.CostPrice != nil && (*holding.CostPrice <= 0 || math.IsNaN(*holding.CostPrice) || math.IsInf(*holding.CostPrice, 0)) {
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
	for _, symbol := range request.ForceSymbols {
		n, err := foundation.NormalizeSymbol(symbol)
		if err != nil {
			return Request{}, err
		}
		if _, ok := seen[n.Canonical]; !ok {
			return Request{}, errors.New("重新研究的股票必须属于本次持仓")
		}
		normalized.ForceSymbols = append(normalized.ForceSymbols, n.Canonical)
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
	if _, err := s.store.Save(ctx, job); err != nil {
		if s.logger != nil {
			s.logger.Printf("level=warn event=portfolio_inspection_persist_error feature=portfolio-inspection job_id=%q error=%q", job.ID, runtimelog.Redact(err.Error()))
		}
	} else if !job.CompletedAt.IsZero() && s.onComplete != nil {
		s.onComplete(job)
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
