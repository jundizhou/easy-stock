package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
	"easy-stock/backend/internal/runtimelog"
	"easy-stock/backend/internal/stockanalysis"
)

type stockAIAnalysisRequest struct {
	Symbol string `json:"symbol"`
	Mode   string `json:"mode,omitempty"`
}

const (
	stockAnalysisModeQuick = "quick"
	stockAnalysisModeFull  = "full"
)

func (s *Server) stockAIAnalysis(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request stockAIAnalysisRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode == "" {
		mode = stockAnalysisModeFull
	}
	if mode != stockAnalysisModeQuick && mode != stockAnalysisModeFull {
		writeError(w, http.StatusBadRequest, "mode must be quick or full")
		return
	}
	normalized, err := foundation.NormalizeSymbol(request.Symbol)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	timeout := s.stockAnalysisTimeout()
	if mode == stockAnalysisModeQuick {
		timeout = s.stockAnalysisQuickTimeout()
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	analysis, err := s.analyzeStockWithMode(ctx, normalized.Canonical, mode)
	if err != nil {
		status := http.StatusBadGateway
		var runError *stockAnalysisRunError
		if errors.As(err, &runError) {
			status = runError.status
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": analysis})
}

type stockAnalysisRunError struct {
	status int
	err    error
}

func (e *stockAnalysisRunError) Error() string { return e.err.Error() }
func (e *stockAnalysisRunError) Unwrap() error { return e.err }

func (s *Server) logStockAnalysisStage(symbol, stage, status string, startedAt time.Time, itemCount int, err error) {
	if s == nil || s.logger == nil {
		return
	}
	durationMS := int64(0)
	if !startedAt.IsZero() {
		durationMS = time.Since(startedAt).Milliseconds()
	}
	level := "info"
	if status == "failed" {
		level = "warn"
	}
	if err != nil {
		s.logger.Printf(
			"level=%s event=stock_analysis_stage feature=stock-analysis symbol=%q stage=%q status=%q duration_ms=%d item_count=%d error=%q",
			level, symbol, stage, status, durationMS, itemCount, runtimelog.Redact(err.Error()),
		)
		return
	}
	s.logger.Printf(
		"level=%s event=stock_analysis_stage feature=stock-analysis symbol=%q stage=%q status=%q duration_ms=%d item_count=%d",
		level, symbol, stage, status, durationMS, itemCount,
	)
}

func (s *Server) logStockResearchModelCall(symbol string, level stockanalysis.ResearchLevel, stage string, startedAt time.Time, promptBytes, responseBytes int, err error) {
	if s == nil || s.logger == nil {
		return
	}
	if err != nil {
		s.logger.Printf("level=warn event=stock_research_model_call feature=stock-analysis symbol=%q analysis_level=%q stage=%q duration_ms=%d prompt_bytes=%d response_bytes=%d error=%q", symbol, level, stage, time.Since(startedAt).Milliseconds(), promptBytes, responseBytes, runtimelog.Redact(err.Error()))
		return
	}
	s.logger.Printf("level=info event=stock_research_model_call feature=stock-analysis symbol=%q analysis_level=%q stage=%q duration_ms=%d prompt_bytes=%d response_bytes=%d", symbol, level, stage, time.Since(startedAt).Milliseconds(), promptBytes, responseBytes)
}

func (s *Server) logThemeEvidencePrompt(symbol string, stats stockanalysis.ThemeEvidencePromptStats) {
	if s == nil || s.logger == nil {
		return
	}
	s.logger.Printf(
		"level=info event=stock_analysis_theme_candidates feature=stock-analysis symbol=%q announcements_input=%d announcements_candidates=%d news_input=%d news_candidates=%d themes_input=%d themes_candidates=%d prompt_bytes=%d tool_mode=%q model_mode=%q",
		symbol, stats.AnnouncementInput, stats.AnnouncementCandidates, stats.NewsInput, stats.NewsCandidates,
		stats.ThemeInput, stats.ThemeCandidates, stats.PromptBytes, stats.ToolMode, "current_selected",
	)
}

func (s *Server) logThemeEvidenceAttempts(symbol string, attempts []stockanalysis.ThemeEvidenceAttempt) {
	if s == nil || s.logger == nil {
		return
	}
	for _, attempt := range attempts {
		if strings.TrimSpace(attempt.Error) != "" {
			s.logger.Printf(
				"level=warn event=stock_analysis_theme_attempt feature=stock-analysis symbol=%q attempt=%d duration_ms=%d prompt_bytes=%d response_bytes=%d tool_mode=%q error=%q",
				symbol, attempt.Number, attempt.DurationMS, attempt.PromptBytes, attempt.ResponseBytes, "disabled", runtimelog.Redact(attempt.Error),
			)
			continue
		}
		s.logger.Printf(
			"level=info event=stock_analysis_theme_attempt feature=stock-analysis symbol=%q attempt=%d duration_ms=%d prompt_bytes=%d response_bytes=%d tool_mode=%q",
			symbol, attempt.Number, attempt.DurationMS, attempt.PromptBytes, attempt.ResponseBytes, "disabled",
		)
	}
}

func (s *Server) analyzeStock(ctx context.Context, canonicalSymbol string) (stockanalysis.Analysis, error) {
	return s.awaitStockResearch(ctx, stockanalysis.ResearchRequest{Symbol: canonicalSymbol, Purpose: "holding"})
}

func (s *Server) analyzeStockWithMode(ctx context.Context, canonicalSymbol, mode string) (stockanalysis.Analysis, error) {
	if mode == stockAnalysisModeFull {
		return s.awaitStockResearch(ctx, stockanalysis.ResearchRequest{Symbol: canonicalSymbol})
	}
	analysis, _, err := s.collectStockResearch(ctx, canonicalSymbol)
	return analysis, err
}

func (s *Server) collectStockResearch(ctx context.Context, canonicalSymbol string) (stockanalysis.Analysis, *stockanalysis.ResearchSnapshot, error) {
	normalized, err := foundation.NormalizeSymbol(canonicalSymbol)
	if err != nil {
		return stockanalysis.Analysis{}, nil, err
	}
	dataCtx, cancelData := context.WithTimeout(ctx, 35*time.Second)
	defer cancelData()
	benchmarkSymbol, benchmarkName := stockanalysis.BenchmarkForSymbol(normalized.Canonical)
	dataStartedAt := time.Now()
	s.logStockAnalysisStage(normalized.Canonical, "data_collection", "started", time.Time{}, 0, nil)

	var (
		quote           foundation.Quote
		lines           []foundation.KLine
		benchmarkLines  []foundation.KLine
		limitUps        []foundation.LimitUpEvent
		cachedThemes    []foundation.StockThemeAttribution
		catalog         []foundation.StockCatalogEntry
		themes          []foundation.ThemeOverview
		news            []foundation.NewsItem
		business        foundation.StockBusinessProfile
		fundamentals    foundation.StockFundamentals
		reports         []foundation.MarketResearchItem
		announcements   []foundation.MarketResearchItem
		quoteErr        error
		lineErr         error
		benchmarkErr    error
		limitUpErr      error
		cachedThemeErr  error
		catalogErr      error
		themeErr        error
		newsErr         error
		businessErr     error
		fundamentalErr  error
		reportErr       error
		announcementErr error
		collectionWG    sync.WaitGroup
	)

	collectionWG.Add(3)
	go func() {
		defer collectionWG.Done()
		quotes, loadErr := s.realtimeProvider.Realtime(dataCtx, []string{normalized.Canonical})
		if loadErr != nil {
			quoteErr = loadErr
			return
		}
		if len(quotes) == 0 {
			quoteErr = fmt.Errorf("实时行情未返回%s", normalized.Canonical)
			return
		}
		quote = quotes[0]
	}()
	go func() {
		defer collectionWG.Done()
		lines, lineErr = s.loadKLine(dataCtx, normalized.Canonical, "day", 300)
	}()
	go func() {
		defer collectionWG.Done()
		benchmarkLines, benchmarkErr = s.loadKLine(dataCtx, benchmarkSymbol, "day", 300)
	}()

	if s.limitUpProvider != nil {
		collectionWG.Add(1)
		go func() {
			defer collectionWG.Done()
			limitUps, limitUpErr = s.limitUpProvider.RecentLimitUps(dataCtx, 40)
		}()
		if provider, ok := s.limitUpProvider.(StockThemeAttributionProvider); ok {
			collectionWG.Add(1)
			go func() {
				defer collectionWG.Done()
				cachedThemes, cachedThemeErr = provider.StockThemes(dataCtx, normalized.Canonical, 40)
			}()
		}
	}
	if s.stockConcepts != nil {
		collectionWG.Add(1)
		go func() {
			defer collectionWG.Done()
			catalog, catalogErr = s.stockConcepts.StockCatalog(dataCtx)
		}()
	}
	if s.stockBusiness != nil {
		collectionWG.Add(2)
		go func() {
			defer collectionWG.Done()
			business, businessErr = s.stockBusiness.StockBusinessProfile(dataCtx, normalized.Canonical)
		}()
		go func() {
			defer collectionWG.Done()
			fundamentals, fundamentalErr = s.stockBusiness.StockFundamentals(dataCtx, normalized.Canonical)
		}()
	}
	if s.marketOverview != nil {
		collectionWG.Add(2)
		go func() {
			defer collectionWG.Done()
			reports, _, reportErr = s.marketOverview.MarketReports(dataCtx, "stock", "", normalized.Canonical, "", 8)
		}()
		go func() {
			defer collectionWG.Done()
			announcements, _, announcementErr = s.marketOverview.MarketAnnouncements(dataCtx, "", normalized.Canonical, "all", 24)
		}()
	}
	if s.themeOverview != nil {
		collectionWG.Add(1)
		go func() {
			defer collectionWG.Done()
			themes, _, themeErr = s.themeOverview.Overviews(dataCtx)
		}()
	}
	if s.newsProvider != nil {
		collectionWG.Add(1)
		go func() {
			defer collectionWG.Done()
			news, newsErr = s.newsProvider.LatestNews(dataCtx, 120)
		}()
	}
	collectionWG.Wait()
	if lineErr != nil {
		s.logStockAnalysisStage(normalized.Canonical, "data_collection", "failed", dataStartedAt, len(lines), lineErr)
		return stockanalysis.Analysis{}, nil, &stockAnalysisRunError{status: http.StatusBadGateway, err: fmt.Errorf("个股K线加载失败: %w", lineErr)}
	}

	concepts := make([]string, 0, 8)
	industry := ""
	for _, item := range catalog {
		if item.Symbol != normalized.Canonical {
			continue
		}
		concepts = append(concepts, item.Concepts...)
		industry = strings.TrimSpace(item.Industry)
		if strings.TrimSpace(item.Name) != "" {
			quote.Name = item.Name
		}
		break
	}
	var emotion *marketemotion.Snapshot
	if s.marketEmotion != nil && s.marketEmotion.store != nil {
		points, loadErr := s.marketEmotion.store.List(dataCtx, 1)
		if loadErr == nil && len(points) > 0 {
			latest := points[len(points)-1]
			emotion = &latest
		}
	}

	gaps := make([]string, 0, 6)
	if quoteErr != nil {
		gaps = append(gaps, "实时行情降级为最新日K: "+quoteErr.Error())
	}
	if limitUpErr != nil {
		gaps = append(gaps, "精确涨停事件不可用: "+limitUpErr.Error())
	}
	if cachedThemeErr != nil {
		gaps = append(gaps, "开盘啦个股题材缓存不可用: "+cachedThemeErr.Error())
	}
	if catalogErr != nil {
		gaps = append(gaps, "个股概念目录不可用: "+catalogErr.Error())
	}
	if themeErr != nil {
		gaps = append(gaps, "题材趋势数据不可用: "+themeErr.Error())
	}
	if newsErr != nil {
		gaps = append(gaps, "近期新闻不可用: "+newsErr.Error())
	}
	if benchmarkErr != nil {
		gaps = append(gaps, "基准指数数据不可用: "+benchmarkErr.Error())
	}
	if businessErr != nil {
		gaps = append(gaps, "主营业务资料不可用: "+businessErr.Error())
	}
	if fundamentalErr != nil {
		gaps = append(gaps, "基本面资料不可用: "+fundamentalErr.Error())
	}
	if reportErr != nil {
		gaps = append(gaps, "机构研报不可用: "+reportErr.Error())
	}
	if announcementErr != nil {
		gaps = append(gaps, "公司公告不可用: "+announcementErr.Error())
	}
	dataStatus := "completed"
	if len(gaps) > 0 {
		dataStatus = "degraded"
	}
	s.logStockAnalysisStage(normalized.Canonical, "data_collection", dataStatus, dataStartedAt, len(lines), nil)

	analysisInput := stockanalysis.Input{
		Symbol:          normalized.Canonical,
		Quote:           quote,
		KLines:          lines,
		BenchmarkSymbol: benchmarkSymbol,
		BenchmarkName:   benchmarkName,
		BenchmarkKLines: benchmarkLines,
		LimitUps:        limitUps,
		Catalog:         catalog,
		Concepts:        concepts,
		Industry:        industry,
		Business:        business.MainBusiness,
		BusinessDetail:  business.Description,
		BusinessSource:  business.Meta.Source,
		Fundamentals:    &fundamentals,
		Reports:         reports,
		Announcements:   announcements,
		CachedThemes:    cachedThemes,
		Themes:          themes,
		MarketEmotion:   emotion,
		News:            news,
		CollectionGaps:  gaps,
	}
	localStartedAt := time.Now()
	s.logStockAnalysisStage(normalized.Canonical, "local_analysis", "started", time.Time{}, len(lines), nil)
	analysis, err := stockanalysis.Analyze(analysisInput)
	if err != nil {
		s.logStockAnalysisStage(normalized.Canonical, "local_analysis", "failed", localStartedAt, len(lines), err)
		return stockanalysis.Analysis{}, nil, &stockAnalysisRunError{status: http.StatusUnprocessableEntity, err: err}
	}
	s.logStockAnalysisStage(normalized.Canonical, "local_analysis", "completed", localStartedAt, len(analysis.Evidence), nil)
	analysis.AI.Status = "rules"
	analysis.AI.Message = "快速分析已完成，当前为量化基线，尚未形成AI研究结论"
	snapshot := stockanalysis.BuildResearchSnapshot(analysisInput, analysis, time.Now().UTC())
	analysis.SnapshotID = snapshot.ID
	return analysis, &snapshot, nil
}
