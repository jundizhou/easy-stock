package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
	"easy-stock/backend/internal/review"
)

type dailyValidationRequest struct {
	SummaryDate string `json:"summary_date"`
	Force       bool   `json:"force"`
}

func (s *Server) reviewDailyValidation(w http.ResponseWriter, r *http.Request) {
	if s.reviewAutomation == nil {
		writeError(w, http.StatusServiceUnavailable, "昨日验证服务不可用")
		return
	}
	summaryDate, err := s.reviewValidationSummaryDate(r.Context(), strings.TrimSpace(r.URL.Query().Get("summary_date")))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"data": nil, "summary_date": "", "job": review.DailyValidationJob{Status: "idle", Stage: "idle", Message: "还没有可验证的昨日 AI 总结"}})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	validation, validationErr := s.reviewAutomation.GetDailyValidation(r.Context(), summaryDate)
	if validationErr != nil && !errors.Is(validationErr, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, validationErr.Error())
		return
	}
	job, jobErr := s.reviewAutomation.GetDailyValidationJob(r.Context(), summaryDate)
	if jobErr != nil {
		writeError(w, http.StatusInternalServerError, jobErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": validation, "summary_date": summaryDate, "job": job})
}

func (s *Server) reviewDailyValidationStatus(w http.ResponseWriter, r *http.Request) {
	if s.reviewAutomation == nil {
		writeError(w, http.StatusServiceUnavailable, "昨日验证服务不可用")
		return
	}
	summaryDate, err := s.reviewValidationSummaryDate(r.Context(), strings.TrimSpace(r.URL.Query().Get("summary_date")))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"data": review.DailyValidationJob{Status: "idle", Stage: "idle", Message: "还没有可验证的昨日 AI 总结"}})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	job, err := s.reviewAutomation.GetDailyValidationJob(r.Context(), summaryDate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": job})
}

func (s *Server) reviewDailyValidationCreate(w http.ResponseWriter, r *http.Request) {
	if s.reviewAutomation == nil {
		writeError(w, http.StatusServiceUnavailable, "昨日验证服务不可用")
		return
	}
	request := dailyValidationRequest{}
	if r.ContentLength != 0 {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "无效的昨日验证请求: "+err.Error())
			return
		}
		if err := ensureJSONEOF(decoder); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	summaryDate, err := s.reviewValidationSummaryDate(r.Context(), strings.TrimSpace(request.SummaryDate))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "还没有可验证的昨日 AI 总结")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	job, err := s.reviewAutomation.StartDailyValidation(r.Context(), summaryDate, request.Force, s.collectDailyValidationSnapshot)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "采集器") || strings.Contains(err.Error(), "存储") {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": job})
}

func (s *Server) reviewValidationSummaryDate(ctx context.Context, requested string) (string, error) {
	if requested != "" {
		if _, err := time.Parse("2006-01-02", requested); err != nil {
			return "", errors.New("summary_date 必须是 YYYY-MM-DD")
		}
		return requested, nil
	}
	before := time.Now().In(shanghaiLocation).Format("2006-01-02")
	summary, err := s.reviewAutomation.LatestDailySummaryBefore(ctx, before)
	if err != nil {
		return "", err
	}
	return summary.TradeDate, nil
}

func (s *Server) collectDailyValidationSnapshot(ctx context.Context, summary review.DailySummary) (review.DailyValidationSnapshot, error) {
	snapshot := review.DailyValidationSnapshot{CapturedAt: time.Now().UTC(), DataQuality: []string{}}
	var (
		indexes          []foundation.MarketIndexSnapshot
		industries       []foundation.MarketIndustryMomentum
		flows            []foundation.MarketFundFlow
		themes           []foundation.ThemeOverview
		ladder           limitUpLadderData
		emotion          *marketEmotionResult
		catalog          []foundation.StockCatalogEntry
		indexErr         error
		industryErr      error
		flowErr          error
		themeErr         error
		ladderErr        error
		emotionErr       error
		catalogErr       error
		emotionTradeDate string
	)
	var wg sync.WaitGroup
	if s.marketOverview != nil {
		wg.Add(3)
		go func() { defer wg.Done(); indexes, _, indexErr = s.marketOverview.MarketIndexes(ctx, "core") }()
		go func() { defer wg.Done(); industries, _, industryErr = s.marketOverview.IndustryMomentum(ctx, 40) }()
		go func() { defer wg.Done(); flows, _, flowErr = s.marketOverview.MarketFundFlows(ctx, "theme", "net", 40) }()
	} else {
		snapshot.DataQuality = append(snapshot.DataQuality, "市场总览服务不可用")
	}
	if s.themeOverview != nil {
		wg.Add(1)
		go func() { defer wg.Done(); themes, _, themeErr = s.themeOverview.Overviews(ctx) }()
	} else {
		snapshot.DataQuality = append(snapshot.DataQuality, "题材总览服务不可用")
	}
	if s.limitUpSnapshots != nil && s.limitUpProvider != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ladder, ladderErr = s.limitUpSnapshots.load(ctx, s.limitUpProvider, s.stockConcepts, s.realtimeProvider)
		}()
	} else {
		snapshot.DataQuality = append(snapshot.DataQuality, "涨停梯队服务不可用")
	}
	if s.marketEmotion != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			history, err := s.marketEmotion.load(ctx)
			if err != nil {
				emotionErr = err
				return
			}
			if s.marketEmotionIntraday != nil && s.limitUpSnapshots != nil && s.limitUpProvider != nil {
				intraday, intradayErr := s.marketEmotionIntraday.load(ctx, func(loadCtx context.Context) (marketemotion.IntradaySnapshot, error) {
					ladder, loadErr := s.limitUpSnapshots.load(loadCtx, s.limitUpProvider, s.stockConcepts, s.realtimeProvider)
					if loadErr != nil {
						return marketemotion.IntradaySnapshot{}, loadErr
					}
					return buildMarketEmotionIntraday(ladder, history.Latest), nil
				})
				if intradayErr == nil {
					history.Intraday = &intraday
				}
			}
			emotion = &marketEmotionResult{history: history}
		}()
	} else {
		snapshot.DataQuality = append(snapshot.DataQuality, "市场情绪服务不可用")
	}
	if s.stockDirectory != nil {
		wg.Add(1)
		go func() { defer wg.Done(); catalog, catalogErr = s.stockDirectory.StockCatalog(ctx) }()
	}
	wg.Wait()

	if indexErr != nil {
		snapshot.DataQuality = append(snapshot.DataQuality, "指数数据不可用："+indexErr.Error())
	}
	if industryErr != nil {
		snapshot.DataQuality = append(snapshot.DataQuality, "行业强度不可用："+industryErr.Error())
	}
	if flowErr != nil {
		snapshot.DataQuality = append(snapshot.DataQuality, "题材资金流不可用："+flowErr.Error())
	}
	if themeErr != nil {
		snapshot.DataQuality = append(snapshot.DataQuality, "题材数据不可用："+themeErr.Error())
	}
	if ladderErr != nil {
		snapshot.DataQuality = append(snapshot.DataQuality, "涨停梯队不可用："+ladderErr.Error())
	}
	if emotionErr != nil {
		snapshot.DataQuality = append(snapshot.DataQuality, "市场情绪不可用："+emotionErr.Error())
	}
	if catalogErr != nil {
		snapshot.DataQuality = append(snapshot.DataQuality, "股票目录不可用："+catalogErr.Error())
	}

	for _, item := range indexes {
		snapshot.Indexes = append(snapshot.Indexes, review.DailyValidationIndex{ID: item.ID, Name: item.Name, ChangePercent: item.ChangePercent, TradeDate: item.Meta.TradeDate, Source: item.Meta.Source})
	}
	for _, item := range industries {
		snapshot.Industries = append(snapshot.Industries, review.DailyValidationIndustry{Name: item.Name, ChangePercent: item.ChangePercent, NetInflow: item.MainNetInflow, RisingCount: item.RisingCount, FallingCount: item.FallingCount, Score: item.Score, LeaderName: item.LeaderName})
	}
	for _, item := range flows {
		snapshot.Flows = append(snapshot.Flows, review.DailyValidationFlow{Dimension: item.Dimension, Name: item.Name, ChangePercent: item.ChangePercent, NetInflow: item.NetInflow, MainNetInflow: item.MainNetInflow, LeaderName: item.LeaderName})
	}
	for _, item := range themes {
		snapshot.Themes = append(snapshot.Themes, review.DailyValidationTheme{Name: firstNonEmpty(item.Name, item.Theme), ChangePercent: item.ChangePercent, NetInflow: item.MainNetInflow, RisingCount: item.RisingNodes, FallingCount: item.FallingNodes, LimitUpCount: item.LimitUpCount, MaxStreak: item.MaxStreak, TrendScore: item.TrendScore, Stage: item.TrendStage, Leaders: append([]string(nil), item.Leaders...), Source: item.Source})
	}
	sort.SliceStable(snapshot.Themes, func(i, j int) bool {
		if snapshot.Themes[i].ChangePercent != snapshot.Themes[j].ChangePercent {
			return snapshot.Themes[i].ChangePercent > snapshot.Themes[j].ChangePercent
		}
		return snapshot.Themes[i].NetInflow > snapshot.Themes[j].NetInflow
	})
	if emotion != nil {
		if latest := emotion.history.Latest; latest != nil {
			raw := latest.Raw
			emotionTradeDate = latest.TradeDate
			snapshot.Emotion = &review.DailyValidationEmotion{Phase: latest.Phase, EmotionScore: latest.EmotionScore, Heat: latest.Scores.Heat, Profit: latest.Scores.Profit, Structure: latest.Scores.Structure, LimitUpCount: raw.LimitUpCount, LimitDownCount: raw.LimitDownCount, BrokenCount: raw.BrokenCount, FirstBoardCount: raw.FirstBoardCount, BoardCount: raw.BoardCount, MaxStreak: raw.MaxStreak, PreviousLimitUpRet: raw.PreviousLimitUpRet, FinalBreakRate: raw.FinalBreakRate, AdvanceRate: raw.AdvanceRate, HighAverageReturn: raw.HighAverageReturn, HighAdvanceRate: raw.HighAdvanceRate, HeightCollapse: raw.HeightCollapse, HighRiskScore: raw.HighRiskScore, QuoteCoverage: raw.QuoteCoverage, Confidence: latest.Confidence}
		}
		if emotion.history.Intraday != nil {
			item := emotion.history.Intraday
			snapshot.Intraday = &review.DailyValidationIntraday{TradeDate: item.TradeDate, Status: item.Status, Breadth: item.Breadth, RiskScore: item.RiskScore, CurrentMaxStreak: item.Metrics.CurrentMaxStreak, HeightCollapse: item.Metrics.HeightCollapse, HighAverageReturn: item.Metrics.HighAverageReturn, HighDownRate: item.Metrics.HighDownRate, HighAdvanceRate: item.Metrics.HighAdvanceRate, LimitUpCount: item.Metrics.LimitUpCount, BoardCount: item.Metrics.BoardCount, FirstBoardCount: item.Metrics.FirstBoardCount, Confidence: item.Confidence}
		}
	}
	snapshot.LimitUp = review.DailyValidationLimitUp{CurrentTradeDate: ladder.Current.TradeDate, PreviousTradeDate: ladder.Previous.TradeDate, CurrentCount: ladder.Current.LimitUpCount, PreviousCount: ladder.Previous.LimitUpCount, CurrentBoardCount: ladder.Current.BoardCount, PreviousBoardCount: ladder.Previous.BoardCount, CurrentMaxStreak: ladder.Current.MaxStreak, PreviousMaxStreak: ladder.Previous.MaxStreak}
	for _, item := range ladder.ConceptHeat {
		snapshot.LimitUp.Concepts = append(snapshot.LimitUp.Concepts, review.DailyValidationTheme{Name: item.Name, LimitUpCount: item.Count, MaxStreak: item.MaxStreak, TrendScore: int(item.Heat), Leaders: append([]string(nil), item.Leaders...)})
	}
	// Prefer a date from the same-day/ladder snapshot. The historical emotion
	// cache can still point at the previous session while intraday providers are
	// already serving today's data.
	if snapshot.Intraday != nil && snapshot.Intraday.TradeDate != "" {
		snapshot.TradeDate = snapshot.Intraday.TradeDate
	}
	if snapshot.TradeDate == "" {
		snapshot.TradeDate = ladder.Current.TradeDate
	}
	if snapshot.TradeDate == "" {
		snapshot.TradeDate = emotionTradeDate
	}
	if snapshot.TradeDate == "" && len(snapshot.Indexes) > 0 {
		snapshot.TradeDate = snapshot.Indexes[0].TradeDate
	}
	if snapshot.TradeDate == "" {
		snapshot.TradeDate = time.Now().In(shanghaiLocation).Format("2006-01-02")
	}

	symbols := resolveValidationSymbols(summary, catalog)
	if len(symbols) > 0 && s.realtimeProvider != nil {
		quotes, quoteErr := s.realtimeProvider.Realtime(ctx, symbols)
		if quoteErr != nil {
			snapshot.DataQuality = append(snapshot.DataQuality, "关注个股行情不可用："+quoteErr.Error())
		} else {
			for _, quote := range quotes {
				snapshot.Stocks = append(snapshot.Stocks, review.DailyValidationStock{Name: quote.Name, Symbol: quote.Symbol, Open: quote.Open, High: quote.High, Low: quote.Low, Price: quote.Price, PreviousClose: quote.PreviousClose, ChangePercent: quote.ChangePercent, TradeDate: quote.Meta.TradeDate, Source: quote.Meta.Source, Matched: true})
			}
		}
	} else if len(symbols) == 0 {
		snapshot.DataQuality = append(snapshot.DataQuality, "昨日明日关注没有可解析的股票代码")
	} else {
		snapshot.DataQuality = append(snapshot.DataQuality, "实时行情服务不可用")
	}
	if snapshot.Emotion == nil && len(snapshot.Themes) == 0 && len(snapshot.Indexes) == 0 && len(snapshot.Stocks) == 0 {
		return review.DailyValidationSnapshot{}, errors.New("没有采集到足够的验证日盘面数据")
	}
	return snapshot, nil
}

type marketEmotionResult struct {
	history marketemotion.History
}

func resolveValidationSymbols(summary review.DailySummary, catalog []foundation.StockCatalogEntry) []string {
	requested := map[string]string{}
	for _, item := range append(append([]review.DailyStockView{}, summary.TomorrowFocus...), summary.TodaySurprises...) {
		if strings.TrimSpace(item.Symbol) != "" {
			requested[item.Symbol] = item.Symbol
		}
	}
	if len(requested) == 0 {
		for _, item := range append(append([]review.DailyStockView{}, summary.TomorrowFocus...), summary.TodaySurprises...) {
			for _, candidate := range catalog {
				if strings.TrimSpace(item.Name) != "" && item.Name == candidate.Name {
					requested[candidate.Symbol] = candidate.Symbol
					break
				}
			}
		}
	}
	result := make([]string, 0, len(requested))
	for symbol := range requested {
		result = append(result, symbol)
	}
	sort.Strings(result)
	if len(result) > 30 {
		result = result[:30]
	}
	return result
}
