package httpapi

import (
	"context"
	"database/sql"
	"easy-stock/backend/internal/foundation"
	pi "easy-stock/backend/internal/portfolioinspection"
	po "easy-stock/backend/internal/portfoliooptimization"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

func (s *Server) portfolioOptimizationCreate(w http.ResponseWriter, r *http.Request) {
	var req po.Request
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	job, err := s.portfolioOptimization.Start(r.Context(), r.PathValue("id"), req)
	if err != nil {
		optimizationError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": job})
}
func (s *Server) portfolioOptimizationList(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.portfolioStore.OptimizationSummaries(r.Context(), r.PathValue("id"))
	if err != nil {
		optimizationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": jobs})
}
func (s *Server) portfolioOptimizationGet(w http.ResponseWriter, r *http.Request) {
	job, err := s.portfolioOptimization.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		optimizationError(w, err)
		return
	}
	if r.URL.Query().Get("view") == "progress" {
		completed := 0
		for _, result := range job.Results {
			if result.Status == "succeeded" {
				completed++
			}
		}
		var audit *po.ScreeningAudit
		if job.ScreeningAudit != nil {
			summary := *job.ScreeningAudit
			summary.Records = nil
			audit = &summary
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
			"id": job.ID, "status": job.Status, "stage": job.Stage, "message": job.Message, "error": job.Error,
			"resume_available": job.ResumeAvailable, "updated_at": job.UpdatedAt, "candidates": job.Candidates,
			"model_duration_ms": job.ModelDurationMS, "reused_stocks": job.ReusedStocks, "new_stocks": job.NewStocks,
			"model_progress":        job.ModelProgress,
			"checkpoint_progress":   job.CheckpointProgress,
			"execution_duration_ms": job.ExecutionDurationMS,
			"portfolio_needs":       job.Needs,
			"screening_audit":       audit,
			"revision_count":        job.RevisionCount,
			"model_prompt_bytes":    job.ModelPromptBytes, "model_prompt_version": job.ModelPromptVersion,
			"model_stage_duration_ms": job.ModelStageDurationMS, "model_attempts": job.ModelAttempts,
			"completed_research": completed, "total_research": len(job.Results),
		}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": job})
}
func (s *Server) portfolioOptimizationResume(w http.ResponseWriter, r *http.Request) {
	job, err := s.portfolioOptimization.Resume(r.Context(), r.PathValue("id"))
	if err != nil {
		optimizationError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": job})
}
func (s *Server) portfolioOptimizationCancel(w http.ResponseWriter, r *http.Request) {
	if !s.portfolioOptimization.Cancel(r.PathValue("id")) {
		writeError(w, http.StatusConflict, "优化任务已结束")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"message": "已停止优化，不取消共享个股研究"})
}
func optimizationError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, sql.ErrNoRows) {
		status = http.StatusNotFound
	}
	writeError(w, status, err.Error())
}

func (s *Server) collectOptimizationUniverse(parent context.Context, report pi.Report, req po.Request, asOf time.Time) (po.Universe, error) {
	return s.collectOptimizationUniverseWithin(parent, report, req, asOf, po.ScreeningTimeout)
}

func (s *Server) collectOptimizationUniverseWithin(parent context.Context, report pi.Report, req po.Request, asOf time.Time, budget time.Duration) (po.Universe, error) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	audit := &po.ScreeningAudit{CheckLimit: po.MaxScreeningStocks, BudgetMS: budget.Milliseconds(), StopReason: "pool_exhausted"}
	defer func() { audit.DurationMS = time.Since(started).Milliseconds() }()
	u := po.Universe{ScreeningAudit: audit, Candidates: []po.Candidate{}, Limitations: []string{"纯代码筛选：最多检查80只、耗时3分钟，保留最多8只合格候选、最多6只分散行业进入AI研究；初筛不调用AI"}}
	var catalog []foundation.StockCatalogEntry
	var err error
	if s.stockDirectory != nil {
		catalog, err = s.stockDirectory.StockCatalog(ctx)
	} else if s.stockConcepts != nil {
		catalog, err = s.stockConcepts.StockCatalog(ctx)
	} else {
		err = fmt.Errorf("股票目录不可用")
	}
	if err != nil {
		return u, fmt.Errorf("无法确认真实股票目录：%w", err)
	}
	entries := map[string]foundation.StockCatalogEntry{}
	for _, e := range catalog {
		if sym, err := foundation.NormalizeSymbol(e.Symbol); err == nil {
			e.Symbol = sym.Canonical
			entries[e.Symbol] = e
		}
	}
	industries := map[string]string{}
	for symbol, e := range entries {
		industries[symbol] = e.Industry
	}
	original := map[string]bool{}
	for _, h := range report.Request.Holdings {
		original[h.Symbol] = true
	}
	type pool struct {
		signal po.IndustrySignal
		stocks []foundation.BoardStock
	}
	pools := []pool{}
	if s.marketOverview != nil && s.industryStocks != nil {
		momentum, _, loadErr := s.marketOverview.IndustryMomentum(ctx, 150)
		if loadErr != nil {
			u.Limitations = append(u.Limitations, "行业动量不可用，未自动增加未经筛选的股票")
		} else {
			type rankedIndustry struct {
				item   foundation.MarketIndustryMomentum
				signal po.IndustrySignal
			}
			ranked := []rankedIndustry{}
			for _, m := range momentum {
				signal := po.ScreenIndustry(m)
				if signal.Qualified {
					ranked = append(ranked, rankedIndustry{m, signal})
				}
			}
			u.Limitations = append(u.Limitations, fmt.Sprintf("已用代码检查%d个行业，%d个符合温和初启条件；本次取前16个初启行业的真实成分，分批检查并在淘汰后补选", len(momentum), len(ranked)))
			sort.SliceStable(ranked, func(i, j int) bool {
				if ranked[i].signal.Score != ranked[j].signal.Score {
					return ranked[i].signal.Score > ranked[j].signal.Score
				}
				return ranked[i].item.Code < ranked[j].item.Code
			})
			// Bounded verified boards, with round-robin inspection of their members.
			if len(ranked) > po.MaxScreeningIndustries {
				ranked = ranked[:po.MaxScreeningIndustries]
			}
			pools = make([]pool, len(ranked))
			var wg sync.WaitGroup
			membershipSem := make(chan struct{}, 4)
			for i, r := range ranked {
				wg.Add(1)
				go func(i int, r rankedIndustry) {
					defer wg.Done()
					select {
					case membershipSem <- struct{}{}:
					case <-ctx.Done():
						return
					}
					defer func() { <-membershipSem }()
					loadCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
					defer cancel()
					stocks, _, err := s.industryStocks.IndustryStocks(loadCtx, r.item.Code, 50)
					pools[i].signal = r.signal
					if err != nil {
						return
					}
					for _, h := range stocks {
						sym, err := foundation.NormalizeSymbol(h.Symbol)
						if err != nil || original[sym.Canonical] || riskyOptimizationName(h.Name) {
							continue
						}
						h.Symbol = sym.Canonical
						if e, ok := entries[h.Symbol]; ok && !riskyOptimizationName(e.Name) {
							pools[i].stocks = append(pools[i].stocks, h)
						}
					}
					sort.SliceStable(pools[i].stocks, func(a, b int) bool {
						if pools[i].stocks[a].Amount != pools[i].stocks[b].Amount {
							return pools[i].stocks[a].Amount > pools[i].stocks[b].Amount
						}
						return pools[i].stocks[a].Symbol < pools[i].stocks[b].Symbol
					})
				}(i, r)
			}
			wg.Wait()
		}
	}
	// Build a bounded inspection queue, not an eight-stock pre-financial pool.
	queue := []po.Candidate{}
	seen := map[string]bool{}
	add := func(symbol string, signal po.IndustrySignal, source string) {
		if seen[symbol] || original[symbol] || len(queue) >= po.MaxScreeningStocks {
			return
		}
		e, ok := entries[symbol]
		if !ok || riskyOptimizationName(e.Name) {
			return
		}
		seen[symbol] = true
		c := po.Candidate{Symbol: symbol, Name: e.Name, Source: source, Industry: signal.Name, Screening: &po.CandidateScreening{Industry: signal}}
		po.CandidateIndustry(&c, e.Industry)
		if c.IndustryGroup == "" || c.CatalogIndustryGroup == "" {
			return
		}
		queue = append(queue, c)
	}
	for _, symbol := range req.CandidateSymbols {
		for _, p := range pools {
			for _, h := range p.stocks {
				if h.Symbol == symbol {
					add(symbol, p.signal, "user_industry_startup")
					break
				}
			}
			if seen[symbol] {
				break
			}
		}
		if !seen[symbol] && !original[symbol] {
			u.Limitations = append(u.Limitations, symbol+"未在已核验的初启行业成分中或目录行业无效，不能绕过代码筛选")
		}
	}
	// Take one stock per verified source board in each pass. Do not split one
	// board into several industries merely because another catalog disagrees.
	for round := 0; len(queue) < po.MaxScreeningStocks; round++ {
		any := false
		for _, p := range pools {
			if round < len(p.stocks) {
				add(p.stocks[round].Symbol, p.signal, "industry_startup:"+p.signal.Name)
				any = true
			}
		}
		if !any {
			break
		}
	}
	// Originals are needed even if no new stock qualifies.
	potential := make([]po.Candidate, len(queue))
	for i, c := range queue {
		potential[i] = c
		screen := *c.Screening
		screen.Qualified = true // Industry capacity only; never a qualification decision.
		potential[i].Screening = &screen
	}
	possibleIndustries := len(po.DiverseCandidates(potential, po.MaxCandidateResearch))
	symbols := []string{}
	for _, h := range report.Request.Holdings {
		symbols = append(symbols, h.Symbol)
	}
	quoteCtx, quoteCancel := context.WithTimeout(ctx, 12*time.Second)
	u.Quotes, err = s.refreshOptimizationQuotes(quoteCtx, symbols)
	quoteCancel()
	if err != nil {
		return u, fmt.Errorf("原持仓行情核验失败：%w", err)
	}
	financial, financialOK := s.stockBusiness.(StockFinancialHistoryProvider)
	qualified := []po.Candidate{}
	for offset := 0; offset < len(queue); offset += 8 {
		if ctx.Err() != nil {
			break
		}
		batch := queue[offset:min(offset+8, len(queue))]
		batchSymbols := []string{}
		for _, c := range batch {
			batchSymbols = append(batchSymbols, c.Symbol)
		}
		quoteCtx, quoteCancel := context.WithTimeout(ctx, 12*time.Second)
		quotes, quoteErr := s.refreshOptimizationQuotes(quoteCtx, batchSymbols)
		quoteCancel()
		qm := map[string]foundation.Quote{}
		for _, q := range quotes {
			qm[q.Symbol] = q
		}
		checked := make([]bool, len(batch))
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		for i := range batch {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()
				if ctx.Err() != nil {
					return
				}
				checked[i] = true
				c := &batch[i]
				if quoteErr != nil {
					c.Reason = "行情获取失败，未通过代码检查"
					c.Screening.Reasons = []string{c.Reason}
					return
				}
				e := po.TradingEligibility(entries[c.Symbol], qm[c.Symbol], asOf)
				if !e.CanIncrease {
					c.Reason = e.Reason
					c.Screening.Reasons = []string{e.Reason}
					return
				}
				bars, barErr := s.optimizationScreeningBars(ctx, c.Symbol)
				var reports []foundation.StockFundamentals
				var finErr error
				if financialOK {
					loadCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
					reports, finErr = financial.StockFinancialHistory(loadCtx, c.Symbol, 4)
					cancel()
				} else {
					finErr = fmt.Errorf("财务历史接口不可用")
				}
				screening := po.ScreenCandidate(c.Symbol, c.Name, c.Screening.Industry, bars, reports, asOf, qm[c.Symbol].Valuation)
				if barErr != nil {
					screening.Qualified = false
					screening.Reasons = append(screening.Reasons, "价格历史获取失败")
				}
				if finErr != nil {
					screening.Qualified = false
					screening.Reasons = append(screening.Reasons, "财务历史获取失败")
				}
				c.FitBonus, c.FitReason = po.CandidateFit(report, industries, c.CatalogIndustryGroup)
				c.Screening = &screening
				c.Reason = strings.Join(screening.Reasons, "；")
			}(i)
		}
		wg.Wait()
		for i, c := range batch {
			if !checked[i] {
				continue
			}
			audit.Checked++
			audit.Records = append(audit.Records, po.ScreeningRecord{Symbol: c.Symbol, Name: c.Name, Industry: c.IndustryGroup, Qualified: c.Screening.Qualified, Reason: c.Reason})
			if c.Screening.Qualified {
				qualified = append(qualified, c)
				u.Quotes = append(u.Quotes, qm[c.Symbol])
			}
		}
		u.Candidates = po.QualifiedCandidatePool(qualified)
		audit.Diverse = len(po.DiverseCandidates(u.Candidates, po.MaxCandidateResearch))
		if len(u.Candidates) == po.MaxCandidates && audit.Diverse >= possibleIndustries {
			audit.StopReason = "candidates_ready"
			if audit.Diverse < po.MaxCandidateResearch {
				audit.StopReason = "industry_limit"
			}
			break
		}
	}
	if parent.Err() != nil {
		return u, parent.Err()
	}
	if ctx.Err() != nil {
		audit.StopReason = "time_limit"
	} else if audit.StopReason != "candidates_ready" && audit.StopReason != "industry_limit" && audit.Checked >= po.MaxScreeningStocks {
		audit.StopReason = "check_limit"
	}
	audit.Qualified, audit.Retained = len(qualified), len(u.Candidates)
	// Persist only retained candidate quotes and catalog entries. The full
	// rejection audit remains outside the research union and model prompts.
	needed := map[string]bool{}
	for _, symbol := range symbols {
		needed[symbol] = true
	}
	for _, c := range u.Candidates {
		needed[c.Symbol] = true
	}
	quotes := []foundation.Quote{}
	for _, q := range u.Quotes {
		if needed[q.Symbol] {
			quotes = append(quotes, q)
		}
	}
	u.Quotes = quotes
	for symbol := range needed {
		if e, ok := entries[symbol]; ok {
			u.Catalog = append(u.Catalog, e)
		}
	}
	sort.Slice(u.Catalog, func(i, j int) bool { return u.Catalog[i].Symbol < u.Catalog[j].Symbol })
	stopLabel := map[string]string{"candidates_ready": "候选已充足", "industry_limit": "本次可检查范围的行业分散已达上限", "check_limit": "达到80只检查上限", "time_limit": "达到代码筛查时间上限", "pool_exhausted": "可用队列已检查完"}[audit.StopReason]
	u.Limitations = append(u.Limitations, fmt.Sprintf("代码检查%d只，%d只合格，保留%d只；行业分散后最多%d只进入AI，停止原因：%s", audit.Checked, audit.Qualified, audit.Retained, audit.Diverse, stopLabel))
	return u, nil
}

func riskyOptimizationName(name string) bool {
	upper := strings.ToUpper(name)
	return strings.Contains(upper, "ST") || strings.Contains(upper, "退")
}
func (s *Server) optimizationScreeningBars(ctx context.Context, symbol string) ([]foundation.KLine, error) {
	var lastErr error
	for _, p := range []KLineProvider{s.kLinePrimary, s.kLineFallback} {
		if p == nil {
			continue
		}
		loadCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		bars, err := p.KLine(loadCtx, symbol, "day", 35)
		cancel()
		if err == nil && len(bars) >= po.CandidatePolicy.ReturnSessions+1 {
			return bars, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("日线样本不足")
	}
	return nil, lastErr
}

// Fetch one bounded valuation batch alongside prices. A failed optional source
// leaves valuation unknown; it must not block the original growth pathway.
func (s *Server) refreshOptimizationQuotes(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	var valuations []foundation.StockValuation
	var wg sync.WaitGroup
	if s.stockValuation != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loadCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			values, err := s.stockValuation.StockValuations(loadCtx, symbols)
			if err == nil {
				valuations = values
			}
		}()
	}
	quotes, err := s.refreshPortfolioQuotes(ctx, symbols)
	wg.Wait()
	for i := range quotes {
		// Never carry an older optional valuation across a new price snapshot.
		quotes[i].Valuation = nil
		for _, v := range valuations {
			if v.Symbol == quotes[i].Symbol {
				value := v
				quotes[i].Valuation = &value
				break
			}
		}
	}
	return quotes, err
}
