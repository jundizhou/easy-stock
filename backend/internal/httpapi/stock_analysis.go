package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
	"easy-stock/backend/internal/stockanalysis"
)

type stockAIAnalysisRequest struct {
	Symbol string `json:"symbol"`
}

func (s *Server) stockAIAnalysis(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request stockAIAnalysisRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	normalized, err := foundation.NormalizeSymbol(request.Symbol)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	dataCtx, cancelData := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancelData()
	benchmarkSymbol, benchmarkName := stockanalysis.BenchmarkForSymbol(normalized.Canonical)

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
		writeError(w, http.StatusBadGateway, "个股K线加载失败: "+lineErr.Error())
		return
	}

	concepts := make([]string, 0, 8)
	industry := ""
	for _, item := range catalog {
		if item.Symbol != normalized.Canonical {
			continue
		}
		concepts = append(concepts, item.Concepts...)
		industry = strings.TrimSpace(item.Industry)
		if strings.TrimSpace(quote.Name) == "" {
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
	if s.hermesGateway != nil {
		status := s.hermesGateway.Status()
		if status.Available && status.Configured && (len(announcements) > 0 || len(news) > 0) {
			themeCtx, cancelTheme := context.WithTimeout(r.Context(), s.modelResponseTimeout())
			modelEvidence, modelErr := stockanalysis.ExtractThemeEvidence(themeCtx, s.hermesGateway, analysisInput)
			cancelTheme()
			if modelErr == nil {
				analysisInput.ModelThemeEvidence = modelEvidence
			} else {
				analysisInput.CollectionGaps = append(analysisInput.CollectionGaps, "大模型题材抽取不可用，已使用本地证据规则: "+modelErr.Error())
			}
		}
	}
	analysis, err := stockanalysis.Analyze(analysisInput)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if s.hermesGateway == nil {
		analysis.AI.Status = "unavailable"
		analysis.AI.Message = "Hermes未启用，当前展示本地结构化研判"
	} else {
		status := s.hermesGateway.Status()
		if !status.Available || !status.Configured {
			analysis.AI.Status = "unavailable"
			analysis.AI.Message = firstNonEmpty(status.Message, "请先在系统设置中配置AI模型")
		} else {
			methodologyContext := ""
			if s.masteryLibrary != nil {
				knowledgeCtx, cancelKnowledge := context.WithTimeout(r.Context(), 6*time.Second)
				methodologyContext, _ = s.masteryLibrary.ContextForPrompt(
					knowledgeCtx,
					fmt.Sprintf("结合游资心法、情绪周期、趋势与风险控制分析%s %s", analysis.Name, analysis.Symbol),
					6_000,
				)
				cancelKnowledge()
			}
			aiCtx, cancelAI := context.WithTimeout(r.Context(), s.modelResponseTimeout())
			if aiErr := stockanalysis.EnrichWithAI(aiCtx, s.hermesGateway, &analysis, methodologyContext); aiErr != nil {
				analysis.AI.Status = "error"
				analysis.AI.Message = aiErr.Error() + "；已保留本地结构化研判"
			}
			cancelAI()
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": analysis})
}
